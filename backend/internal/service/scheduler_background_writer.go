package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// 调度快照存放在多实例共享的 Redis 中。全量重建与 outbox 消费写的是同一份数据，
// 只需一个实例来做；其余实例只读快照。写入者通过 Redis 租约选出。
const (
	SchedulerBackgroundWriterModeAuto      = "auto"
	SchedulerBackgroundWriterModePreferred = "preferred"
	SchedulerBackgroundWriterModeDisabled  = "disabled"

	defaultSchedulerBackgroundWriterLease = 30 * time.Second
	minSchedulerBackgroundWriterLease     = 3 * time.Second
	schedulerBackgroundWriterOpTimeout    = 3 * time.Second
)

// SchedulerBackgroundWriterLease 是 SchedulerCache 的可选能力：选出唯一的后台写入者。
// 未实现该接口的缓存（如单测替身）保持旧行为：每个实例都执行后台写入。
type SchedulerBackgroundWriterLease interface {
	// TryAcquireBackgroundWriter 在租约空闲时以 owner 身份获取，返回是否获取成功。
	TryAcquireBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) (bool, error)
	// RenewBackgroundWriter 仅在租约仍归 owner 时续期，返回是否仍持有。
	RenewBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) (bool, error)
	// ReleaseBackgroundWriter 仅在租约仍归 owner 时释放。
	ReleaseBackgroundWriter(ctx context.Context, owner string) error
	// HeartbeatPreferredBackgroundWriter 声明 owner 是指定写入者（带 TTL）。
	HeartbeatPreferredBackgroundWriter(ctx context.Context, owner string, ttl time.Duration) error
	// ClearPreferredBackgroundWriter 仅在声明仍归 owner 时清除。
	ClearPreferredBackgroundWriter(ctx context.Context, owner string) error
	// PreferredBackgroundWriter 返回当前存活的指定写入者，没有则为空串。
	PreferredBackgroundWriter(ctx context.Context) (string, error)
}

// schedulerBackgroundWriter 维护本实例是否为后台写入者。
//
//   - auto：竞争租约；发现存活的指定写入者（preferred）时主动让出、不再竞争。
//   - preferred：持续声明自己是指定写入者，租约空闲即获取。
//   - disabled：从不写入（不在此结构中出现，由调用方直接判断）。
//
// 持有租约期间若续期一直失败（如 Redis 不可达），超过一个租约周期后自行视为失去写入权，
// 因为此时租约在 Redis 中也已过期，可能已被其它实例接手。
type schedulerBackgroundWriter struct {
	lease     SchedulerBackgroundWriterLease
	owner     string
	preferred bool
	ttl       time.Duration
	now       func() time.Time
	// onAcquired 在本实例从非写入者变为写入者时调用（不得阻塞）。
	onAcquired func()

	mu          sync.Mutex
	holding     bool
	lastRenewOK time.Time
}

func newSchedulerBackgroundWriter(lease SchedulerBackgroundWriterLease, owner string, preferred bool, ttl time.Duration, onAcquired func()) *schedulerBackgroundWriter {
	if ttl < minSchedulerBackgroundWriterLease {
		ttl = minSchedulerBackgroundWriterLease
	}
	return &schedulerBackgroundWriter{
		lease:      lease,
		owner:      owner,
		preferred:  preferred,
		ttl:        ttl,
		now:        time.Now,
		onAcquired: onAcquired,
	}
}

func (w *schedulerBackgroundWriter) renewInterval() time.Duration {
	return w.ttl / 3
}

// IsWriter 报告本实例当前是否持有写入权。
func (w *schedulerBackgroundWriter) IsWriter() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.holding {
		return false
	}
	if w.now().Sub(w.lastRenewOK) >= w.ttl {
		return false
	}
	return true
}

// tick 执行一轮选举：声明指定写入者、续期或获取租约、必要时让出。
func (w *schedulerBackgroundWriter) tick(ctx context.Context) {
	if w.preferred {
		if err := w.lease.HeartbeatPreferredBackgroundWriter(ctx, w.owner, w.ttl); err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer preferred heartbeat failed: owner=%s err=%v", w.owner, err)
		}
	}

	w.mu.Lock()
	holding := w.holding
	w.mu.Unlock()

	if holding {
		w.tickHolding(ctx)
		return
	}
	w.tickIdle(ctx)
}

func (w *schedulerBackgroundWriter) tickHolding(ctx context.Context) {
	if !w.preferred {
		other, err := w.lease.PreferredBackgroundWriter(ctx)
		if err == nil && other != "" && other != w.owner {
			if err := w.lease.ReleaseBackgroundWriter(ctx, w.owner); err != nil {
				logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer yield release failed: owner=%s err=%v", w.owner, err)
			}
			w.setHolding(false)
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer yielded to preferred: owner=%s preferred=%s", w.owner, other)
			return
		}
	}

	held, err := w.lease.RenewBackgroundWriter(ctx, w.owner, w.ttl)
	if err != nil {
		w.mu.Lock()
		expired := w.now().Sub(w.lastRenewOK) >= w.ttl
		if expired {
			w.holding = false
		}
		w.mu.Unlock()
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer renew failed: owner=%s expired=%t err=%v", w.owner, expired, err)
		return
	}
	if !held {
		w.setHolding(false)
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer lease lost: owner=%s", w.owner)
		// 租约可能只是过期而空闲，立即重新竞争；被他人持有时 SETNX 失败，无副作用。
		w.tickIdle(ctx)
		return
	}
	w.mu.Lock()
	w.lastRenewOK = w.now()
	w.mu.Unlock()
}

func (w *schedulerBackgroundWriter) tickIdle(ctx context.Context) {
	if !w.preferred {
		other, err := w.lease.PreferredBackgroundWriter(ctx)
		if err != nil {
			return
		}
		if other != "" && other != w.owner {
			return
		}
	}
	acquired, err := w.lease.TryAcquireBackgroundWriter(ctx, w.owner, w.ttl)
	if err != nil {
		logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer acquire failed: owner=%s err=%v", w.owner, err)
		return
	}
	if !acquired {
		return
	}
	w.mu.Lock()
	w.holding = true
	w.lastRenewOK = w.now()
	w.mu.Unlock()
	logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer acquired: owner=%s preferred=%t", w.owner, w.preferred)
	if w.onAcquired != nil {
		w.onAcquired()
	}
}

func (w *schedulerBackgroundWriter) setHolding(v bool) {
	w.mu.Lock()
	w.holding = v
	w.mu.Unlock()
}

// release 在实例退出时释放租约与指定写入者声明，让其它实例立即接手。
func (w *schedulerBackgroundWriter) release(ctx context.Context) {
	w.mu.Lock()
	holding := w.holding
	w.holding = false
	w.mu.Unlock()
	if holding {
		if err := w.lease.ReleaseBackgroundWriter(ctx, w.owner); err != nil {
			logger.LegacyPrintf("service.scheduler_snapshot", "[Scheduler] background writer release failed: owner=%s err=%v", w.owner, err)
		}
	}
	if w.preferred {
		_ = w.lease.ClearPreferredBackgroundWriter(ctx, w.owner)
	}
}

func (w *schedulerBackgroundWriter) run(stopCh <-chan struct{}) {
	w.runTick()
	ticker := time.NewTicker(w.renewInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.runTick()
		case <-stopCh:
			return
		}
	}
}

func (w *schedulerBackgroundWriter) runTick() {
	ctx, cancel := context.WithTimeout(context.Background(), schedulerBackgroundWriterOpTimeout)
	defer cancel()
	w.tick(ctx)
}

func schedulerBackgroundWriterMode(cfg *config.Config) string {
	if cfg == nil {
		return SchedulerBackgroundWriterModeAuto
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.Gateway.Scheduling.BackgroundWriterMode))
	switch mode {
	case SchedulerBackgroundWriterModePreferred, SchedulerBackgroundWriterModeDisabled:
		return mode
	default:
		return SchedulerBackgroundWriterModeAuto
	}
}

func schedulerBackgroundWriterLeaseTTL(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.Scheduling.BackgroundWriterLeaseSeconds <= 0 {
		return defaultSchedulerBackgroundWriterLease
	}
	return time.Duration(cfg.Gateway.Scheduling.BackgroundWriterLeaseSeconds) * time.Second
}

// schedulerBackgroundWriterOwner 返回本实例在租约中的身份：配置的 instance_id，
// 否则为 hostname 加随机后缀（同一主机重启后不会误认旧租约）。
func schedulerBackgroundWriterOwner(cfg *config.Config) string {
	suffix := randomSchedulerOwnerSuffix()
	if cfg != nil {
		if id := strings.TrimSpace(cfg.Gateway.Scheduling.InstanceID); id != "" {
			return id + "-" + suffix
		}
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s-%s", host, suffix)
}

func randomSchedulerOwnerSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
