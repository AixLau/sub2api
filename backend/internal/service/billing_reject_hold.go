package service

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 余额不足后仍持续重试的客户端，每次请求都会把完整请求体传到本机才被拒绝，
// 白白消耗入站流量。billingRejectHoldLimiter 在同一用户短时间内被连续拒绝后，
// 把后续请求挂起一段时间再拒绝，用降低重试频率的方式减少流量。
//
// 挂起按用户（而非 IP）生效，不影响网站登录和充值。挂起不做轮询：充值、兑换、
// 分配订阅等代码路径调用 ReleaseBillingRejectHold，本机直接唤醒该用户挂起中的
// 请求，其他实例通过鉴权缓存失效的 Pub/Sub 频道收到同一条解除消息后唤醒。
// 被唤醒的请求重跑一次计费检查，通过就按正常请求继续处理。

const (
	billingRejectHoldSweepInterval  = time.Minute
	billingRejectHoldReleasePrefix  = "billing-hold-release:"
	billingRejectHoldPublishTimeout = 2 * time.Second
)

// 解除消息可能先于本机订阅缓存过期（L1 TTL 约 10 秒）到达，唤醒后若计费检查
// 仍不通过，按这个退避再重试几次，之后不再主动检查，只等下一次解除或到期。
var billingRejectHoldRetryBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}

// BillingRejectHoldEvent 是 BillingRejectHold.Wait 的返回原因。
type BillingRejectHoldEvent int

const (
	// BillingRejectHoldProbe 表示应重跑一次计费检查。
	BillingRejectHoldProbe BillingRejectHoldEvent = iota
	// BillingRejectHoldExpired 表示挂起到期，应返回拒绝响应。
	BillingRejectHoldExpired
	// BillingRejectHoldCanceled 表示客户端已断开。
	BillingRejectHoldCanceled
)

type billingRejectHoldEntry struct {
	rejects     int
	windowStart time.Time
	// holdSince 非零表示该用户已进入挂起模式；挂起时长随它持续的时间加长。
	holdSince time.Time
	lastSeen  time.Time
	held      int
	// wake 在收到解除信号时关闭并换新，唤醒该用户全部挂起中的请求。
	wake chan struct{}
}

type billingRejectHoldLimiter struct {
	threshold   int
	window      time.Duration
	initialHold time.Duration
	maxHold     time.Duration
	idleReset   time.Duration
	maxPerUser  int
	maxTotal    int
	capacity    int
	now         func() time.Time

	mu        sync.RWMutex
	entries   map[int64]*billingRejectHoldEntry
	nextSweep time.Time

	tracked   atomic.Int64
	heldTotal atomic.Int64
	started   atomic.Uint64
	expired   atomic.Uint64
	released  atomic.Uint64
	canceled  atomic.Uint64
	overflow  atomic.Uint64
}

// BillingRejectHoldHealth 暴露给运维健康接口的计数。
type BillingRejectHoldHealth struct {
	Enabled  bool   `json:"enabled"`
	Tracked  int64  `json:"tracked"`
	Held     int64  `json:"held"`
	Started  uint64 `json:"started"`
	Expired  uint64 `json:"expired"`
	Released uint64 `json:"released"`
	Canceled uint64 `json:"canceled"`
	Overflow uint64 `json:"overflow"`
}

// BillingRejectHold 代表一个正被挂起的请求。
type BillingRejectHold struct {
	limiter  *billingRejectHoldLimiter
	entry    *billingRejectHoldEntry
	deadline time.Time
	// retries 是唤醒后剩余的退避重试次数。
	retries int
	done    bool
}

func newBillingRejectHoldLimiter(cfg *config.Config) *billingRejectHoldLimiter {
	if cfg == nil || !cfg.APIKeyAuth.BillingRejectHold.Enabled {
		return nil
	}
	c := cfg.APIKeyAuth.BillingRejectHold
	if c.Threshold <= 0 || c.WindowSeconds <= 0 || c.InitialHoldSeconds <= 0 || c.MaxHoldSeconds < c.InitialHoldSeconds ||
		c.IdleResetSeconds <= 0 || c.MaxHeldPerUser <= 0 || c.MaxHeldTotal <= 0 || c.Capacity <= 0 {
		return nil
	}
	return &billingRejectHoldLimiter{
		threshold:   c.Threshold,
		window:      time.Duration(c.WindowSeconds) * time.Second,
		initialHold: time.Duration(c.InitialHoldSeconds) * time.Second,
		maxHold:     time.Duration(c.MaxHoldSeconds) * time.Second,
		idleReset:   time.Duration(c.IdleResetSeconds) * time.Second,
		maxPerUser:  c.MaxHeldPerUser,
		maxTotal:    c.MaxHeldTotal,
		capacity:    c.Capacity,
		now:         time.Now,
		entries:     make(map[int64]*billingRejectHoldEntry),
	}
}

// BeginBillingRejectHold 记录一次计费拒绝。返回 nil 表示应立即拒绝；
// 返回非 nil 表示该请求应被挂起，调用方必须最终调用 Done。
func (s *APIKeyService) BeginBillingRejectHold(userID int64) *BillingRejectHold {
	if s == nil || s.billingRejectHold == nil || userID <= 0 {
		return nil
	}
	return s.billingRejectHold.begin(userID)
}

// ClearBillingRejectHold 在用户通过计费检查时调用：清除其拒绝计数，
// 并唤醒该用户所有挂起中的请求。
func (s *APIKeyService) ClearBillingRejectHold(userID int64) {
	if s == nil || s.billingRejectHold == nil || userID <= 0 {
		return
	}
	s.billingRejectHold.clear(userID)
}

// ReleaseBillingRejectHold 在用户的计费状态可能由“不可用”变为“可用”时调用
// （充值、兑换、管理员调余额、分配或续期订阅等）：唤醒本机该用户挂起中的请求，
// 并通知其他实例做同样的事。不阻塞调用方，可在未启用挂起时安全调用。
func (s *APIKeyService) ReleaseBillingRejectHold(userID int64) {
	if s == nil || userID <= 0 {
		return
	}
	if s.billingRejectHold != nil {
		s.billingRejectHold.wake(userID)
	}
	if s.cache == nil || s.cfg == nil || !s.cfg.APIKeyAuth.BillingRejectHold.Enabled {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), billingRejectHoldPublishTimeout)
		defer cancel()
		if err := s.cache.PublishAuthCacheInvalidation(ctx, billingRejectHoldReleasePrefix+strconv.FormatInt(userID, 10)); err != nil {
			slog.Warn("publish billing reject hold release failed", "user_id", userID, "error", err)
		}
	}()
}

// handleBillingRejectHoldRelease 处理来自 Pub/Sub 的消息；返回 true 表示这是一条
// 解除消息（已处理），而不是鉴权缓存 key。
func (s *APIKeyService) handleBillingRejectHoldRelease(message string) bool {
	rest, ok := strings.CutPrefix(message, billingRejectHoldReleasePrefix)
	if !ok {
		return false
	}
	if userID, err := strconv.ParseInt(rest, 10, 64); err == nil && userID > 0 && s.billingRejectHold != nil {
		s.billingRejectHold.wake(userID)
	}
	return true
}

func (s *APIKeyService) BillingRejectHoldHealth() BillingRejectHoldHealth {
	if s == nil || s.billingRejectHold == nil {
		return BillingRejectHoldHealth{}
	}
	l := s.billingRejectHold
	return BillingRejectHoldHealth{
		Enabled:  true,
		Tracked:  l.tracked.Load(),
		Held:     l.heldTotal.Load(),
		Started:  l.started.Load(),
		Expired:  l.expired.Load(),
		Released: l.released.Load(),
		Canceled: l.canceled.Load(),
		Overflow: l.overflow.Load(),
	}
}

func (l *billingRejectHoldLimiter) begin(userID int64) *BillingRejectHold {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepLocked(now, false)
	entry := l.entries[userID]
	if entry != nil && entry.held == 0 && !now.Before(entry.lastSeen.Add(l.idleReset)) {
		// 长时间没有再被拒绝：按新用户重新计数。
		*entry = billingRejectHoldEntry{windowStart: now, wake: entry.wake}
	}
	if entry == nil {
		if len(l.entries) >= l.capacity {
			l.sweepLocked(now, true)
			if len(l.entries) >= l.capacity {
				l.overflow.Add(1)
				return nil
			}
		}
		entry = &billingRejectHoldEntry{windowStart: now, wake: make(chan struct{})}
		l.entries[userID] = entry
		l.tracked.Add(1)
	}
	entry.lastSeen = now

	if entry.holdSince.IsZero() {
		if !now.Before(entry.windowStart.Add(l.window)) {
			entry.windowStart = now
			entry.rejects = 0
		}
		entry.rejects++
		if entry.rejects <= l.threshold {
			return nil
		}
		entry.holdSince = now
		slog.Warn("billing reject hold engaged", "user_id", userID, "rejects", entry.rejects, "window", l.window.String())
	}

	if entry.held >= l.maxPerUser || l.heldTotal.Load() >= int64(l.maxTotal) {
		l.overflow.Add(1)
		return nil
	}

	// 挂起时长 = 已持续重试的时间，夹在 [initialHold, maxHold] 之间：
	// 重试得越久挂得越久，且与客户端并发数无关。
	duration := now.Sub(entry.holdSince)
	if duration < l.initialHold {
		duration = l.initialHold
	}
	if duration > l.maxHold {
		duration = l.maxHold
	}
	entry.held++
	l.heldTotal.Add(1)
	l.started.Add(1)
	return &BillingRejectHold{limiter: l, entry: entry, deadline: now.Add(duration)}
}

func (l *billingRejectHoldLimiter) clear(userID int64) {
	if l.tracked.Load() == 0 {
		return
	}
	l.mu.RLock()
	_, ok := l.entries[userID]
	l.mu.RUnlock()
	if !ok {
		return
	}
	l.mu.Lock()
	entry, ok := l.entries[userID]
	if ok {
		delete(l.entries, userID)
		l.tracked.Add(-1)
		entry.signalLocked()
		if entry.held > 0 {
			slog.Info("billing reject hold released", "user_id", userID, "held", entry.held)
		}
	}
	l.mu.Unlock()
}

// wake 唤醒该用户挂起中的请求去重跑计费检查，但保留拒绝计数：
// 如果检查仍不通过（例如充的钱不够），请求继续挂起。
func (l *billingRejectHoldLimiter) wake(userID int64) {
	if l.tracked.Load() == 0 {
		return
	}
	l.mu.RLock()
	entry, ok := l.entries[userID]
	waiting := ok && entry.held > 0
	l.mu.RUnlock()
	if !waiting {
		return
	}
	l.mu.Lock()
	if entry, ok := l.entries[userID]; ok {
		entry.signalLocked()
	}
	l.mu.Unlock()
}

func (e *billingRejectHoldEntry) signalLocked() {
	close(e.wake)
	e.wake = make(chan struct{})
}

// sweepLocked 清理空闲过期的条目。force=false 时最多每分钟执行一次。
func (l *billingRejectHoldLimiter) sweepLocked(now time.Time, force bool) {
	if !force && now.Before(l.nextSweep) {
		return
	}
	l.nextSweep = now.Add(billingRejectHoldSweepInterval)
	for userID, entry := range l.entries {
		if entry.held == 0 && !now.Before(entry.lastSeen.Add(l.idleReset)) {
			delete(l.entries, userID)
			l.tracked.Add(-1)
		}
	}
}

// Deadline 返回本次挂起的到期时间。
func (h *BillingRejectHold) Deadline() time.Time {
	if h == nil {
		return time.Time{}
	}
	return h.deadline
}

// Wait 阻塞到收到解除信号、挂起到期或客户端断开为止。不做周期轮询。
func (h *BillingRejectHold) Wait(ctx context.Context) BillingRejectHoldEvent {
	if h == nil || h.done {
		return BillingRejectHoldExpired
	}
	l := h.limiter
	remaining := h.deadline.Sub(l.now())
	if remaining <= 0 {
		return BillingRejectHoldExpired
	}
	l.mu.RLock()
	wake := h.entry.wake
	l.mu.RUnlock()

	deadline := time.NewTimer(remaining)
	defer deadline.Stop()
	var retry <-chan time.Time
	if h.retries > 0 {
		backoff := billingRejectHoldRetryBackoff[len(billingRejectHoldRetryBackoff)-h.retries]
		timer := time.NewTimer(backoff)
		defer timer.Stop()
		retry = timer.C
	}
	select {
	case <-ctx.Done():
		return BillingRejectHoldCanceled
	case <-deadline.C:
		return BillingRejectHoldExpired
	case <-wake:
		h.retries = len(billingRejectHoldRetryBackoff)
		return BillingRejectHoldProbe
	case <-retry:
		h.retries--
		return BillingRejectHoldProbe
	}
}

// Done 结束挂起并释放名额，可重复调用。
func (h *BillingRejectHold) Done(event BillingRejectHoldEvent) {
	if h == nil || h.done {
		return
	}
	h.done = true
	l := h.limiter
	l.mu.Lock()
	h.entry.held--
	h.entry.lastSeen = l.now()
	l.mu.Unlock()
	l.heldTotal.Add(-1)
	switch event {
	case BillingRejectHoldExpired:
		l.expired.Add(1)
	case BillingRejectHoldCanceled:
		l.canceled.Add(1)
	default:
		l.released.Add(1)
	}
}
