//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestBillingRejectHoldLimiter(now *time.Time) *billingRejectHoldLimiter {
	return &billingRejectHoldLimiter{
		threshold:   2,
		window:      time.Minute,
		initialHold: 30 * time.Second,
		maxHold:     10 * time.Minute,
		idleReset:   30 * time.Minute,
		maxPerUser:  2,
		maxTotal:    3,
		capacity:    4,
		now:         func() time.Time { return *now },
		entries:     make(map[int64]*billingRejectHoldEntry),
	}
}

func TestBillingRejectHoldStartsAfterThresholdAndEscalates(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newTestBillingRejectHoldLimiter(&now)

	require.Nil(t, l.begin(1), "first rejection is immediate")
	require.Nil(t, l.begin(1), "second rejection is immediate")

	first := l.begin(1)
	require.NotNil(t, first)
	require.Equal(t, 30*time.Second, first.Deadline().Sub(now))
	first.Done(BillingRejectHoldExpired)

	// 持续重试 4 分钟后，挂起时长等于已持续的时间。
	now = now.Add(4 * time.Minute)
	second := l.begin(1)
	require.NotNil(t, second)
	require.Equal(t, 4*time.Minute, second.Deadline().Sub(now))
	second.Done(BillingRejectHoldExpired)

	// 超过上限后封顶 10 分钟。
	now = now.Add(20 * time.Minute)
	third := l.begin(1)
	require.NotNil(t, third)
	require.Equal(t, 10*time.Minute, third.Deadline().Sub(now))
	third.Done(BillingRejectHoldExpired)
	require.EqualValues(t, 0, l.heldTotal.Load())
}

func TestBillingRejectHoldWindowAndIdleReset(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newTestBillingRejectHoldLimiter(&now)

	require.Nil(t, l.begin(1))
	require.Nil(t, l.begin(1))
	// 窗口过期后重新计数。
	now = now.Add(61 * time.Second)
	require.Nil(t, l.begin(1))
	require.Nil(t, l.begin(1))
	hold := l.begin(1)
	require.NotNil(t, hold)
	hold.Done(BillingRejectHoldExpired)

	// 长时间没有再被拒绝：回到立即拒绝。
	now = now.Add(31 * time.Minute)
	require.Nil(t, l.begin(1))
}

func TestBillingRejectHoldCapsFallBackToImmediateReject(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := newTestBillingRejectHoldLimiter(&now)
	l.threshold = 0 // 测试里直接进入挂起模式

	a1, a2 := l.begin(1), l.begin(1)
	require.NotNil(t, a1)
	require.NotNil(t, a2)
	require.Nil(t, l.begin(1), "per-user cap")

	b1 := l.begin(2)
	require.NotNil(t, b1)
	require.Nil(t, l.begin(2), "global cap")
	require.EqualValues(t, 2, l.overflow.Load())

	a1.Done(BillingRejectHoldExpired)
	a1.Done(BillingRejectHoldExpired) // 幂等
	require.EqualValues(t, 2, l.heldTotal.Load())
	require.NotNil(t, l.begin(2))
}

func TestBillingRejectHoldWaitEvents(t *testing.T) {
	now := time.Now()
	l := newTestBillingRejectHoldLimiter(&now)
	l.threshold = 0
	l.now = time.Now

	hold := l.begin(1)
	require.NotNil(t, hold)

	// 没有信号时一直阻塞（不轮询）。
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	require.Equal(t, BillingRejectHoldCanceled, hold.Wait(ctx))
	cancel()

	// 解除信号唤醒一次。
	done := make(chan BillingRejectHoldEvent, 1)
	go func() { done <- hold.Wait(context.Background()) }()
	time.Sleep(20 * time.Millisecond)
	l.wake(1)
	select {
	case event := <-done:
		require.Equal(t, BillingRejectHoldProbe, event)
	case <-time.After(time.Second):
		t.Fatal("wake did not release the waiter")
	}

	// 唤醒后若仍被拒绝，按退避再重试一次，而不是立刻空转。
	started := time.Now()
	require.Equal(t, BillingRejectHoldProbe, hold.Wait(context.Background()))
	require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond)

	// 到期。
	hold.deadline = time.Now().Add(30 * time.Millisecond)
	hold.retries = 0
	require.Equal(t, BillingRejectHoldExpired, hold.Wait(context.Background()))
	hold.Done(BillingRejectHoldExpired)
	require.EqualValues(t, 1, l.expired.Load())
}

func TestBillingRejectHoldClearResetsCountAndWakes(t *testing.T) {
	now := time.Now()
	l := newTestBillingRejectHoldLimiter(&now)
	l.threshold = 0
	l.now = time.Now

	hold := l.begin(1)
	require.NotNil(t, hold)
	done := make(chan BillingRejectHoldEvent, 1)
	go func() { done <- hold.Wait(context.Background()) }()
	time.Sleep(20 * time.Millisecond)

	l.clear(1)
	require.Equal(t, BillingRejectHoldProbe, <-done)
	hold.Done(BillingRejectHoldProbe)
	require.EqualValues(t, 0, l.tracked.Load())
	require.EqualValues(t, 0, l.heldTotal.Load())

	l.threshold = 2
	require.Nil(t, l.begin(1), "count restarts after a successful billing check")
}

func TestHandleBillingRejectHoldReleaseMessage(t *testing.T) {
	now := time.Now()
	l := newTestBillingRejectHoldLimiter(&now)
	l.threshold = 0
	l.now = time.Now
	svc := &APIKeyService{billingRejectHold: l}

	require.False(t, svc.handleBillingRejectHoldRelease("0123abcd"), "ordinary cache keys are not release messages")

	hold := l.begin(42)
	require.NotNil(t, hold)
	done := make(chan BillingRejectHoldEvent, 1)
	go func() { done <- hold.Wait(context.Background()) }()
	time.Sleep(20 * time.Millisecond)

	require.True(t, svc.handleBillingRejectHoldRelease(billingRejectHoldReleasePrefix+"42"))
	select {
	case event := <-done:
		require.Equal(t, BillingRejectHoldProbe, event)
	case <-time.After(time.Second):
		t.Fatal("release message did not wake the waiter")
	}
	require.True(t, svc.handleBillingRejectHoldRelease(billingRejectHoldReleasePrefix+"not-a-number"))
}

func TestBillingRejectHoldWakeAllAfterSubscriberReconnect(t *testing.T) {
	now := time.Now()
	l := newTestBillingRejectHoldLimiter(&now)
	l.threshold = 0
	l.now = time.Now
	svc := &APIKeyService{billingRejectHold: l}

	holdA, holdB := l.begin(1), l.begin(2)
	require.NotNil(t, holdA)
	require.NotNil(t, holdB)
	// 没有挂起请求的用户不计入。
	l.threshold = 5
	require.Nil(t, l.begin(3))

	events := make(chan BillingRejectHoldEvent, 2)
	for _, hold := range []*BillingRejectHold{holdA, holdB} {
		go func(h *BillingRejectHold) { events <- h.Wait(context.Background()) }(hold)
	}
	time.Sleep(20 * time.Millisecond)

	require.Equal(t, 2, l.wakeAll())
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			require.Equal(t, BillingRejectHoldProbe, event)
		case <-time.After(time.Second):
			t.Fatal("wakeAll did not wake every held request")
		}
	}
	holdA.Done(BillingRejectHoldProbe)
	holdB.Done(BillingRejectHoldProbe)
	require.Equal(t, 0, l.wakeAll())
	svc.wakeAllBillingRejectHolds() // 无挂起时为空操作
}

type flakyReleasePublishCache struct {
	APIKeyCache
	failures int
	messages []string
}

func (c *flakyReleasePublishCache) PublishAuthCacheInvalidation(_ context.Context, cacheKey string) error {
	c.messages = append(c.messages, cacheKey)
	if len(c.messages) <= c.failures {
		return context.DeadlineExceeded
	}
	return nil
}

func TestBillingRejectHoldReleasePublishRetries(t *testing.T) {
	cache := &flakyReleasePublishCache{failures: 2}
	svc := &APIKeyService{cache: cache}
	svc.publishBillingRejectHoldRelease(9)
	require.Equal(t, []string{"billing-hold-release:9", "billing-hold-release:9", "billing-hold-release:9"}, cache.messages)

	// 一直失败时在固定次数后放弃。
	cache = &flakyReleasePublishCache{failures: 100}
	svc = &APIKeyService{cache: cache}
	svc.publishBillingRejectHoldRelease(9)
	require.Len(t, cache.messages, billingRejectHoldPublishAttempts)
}
