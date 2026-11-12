package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lateReplySlotCache takes the slot when the "script" runs and then loses or
// delays the reply, like Redis executing a command whose response never
// reaches the client in time.
type lateReplySlotCache struct {
	ConcurrencyCache
	mu         sync.Mutex
	account    map[string]struct{}
	user       map[string]struct{}
	calls      int
	replyDelay time.Duration
	loseReply  bool
}

func newLateReplySlotCache() *lateReplySlotCache {
	return &lateReplySlotCache{account: map[string]struct{}{}, user: map[string]struct{}{}}
}

func (c *lateReplySlotCache) acquire(ctx context.Context, set map[string]struct{}, limit int, requestID string) (bool, error) {
	c.mu.Lock()
	c.calls++
	if len(set) >= limit {
		c.mu.Unlock()
		return false, nil
	}
	set[requestID] = struct{}{}
	delay, lose := c.replyDelay, c.loseReply
	c.mu.Unlock()
	if lose {
		return false, errors.New("i/o timeout")
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return true, nil
}

func (c *lateReplySlotCache) AcquireAccountSlot(ctx context.Context, _ int64, limit int, requestID string) (bool, error) {
	return c.acquire(ctx, c.account, limit, requestID)
}

func (c *lateReplySlotCache) AcquireUserSlot(ctx context.Context, _ int64, limit int, requestID string) (bool, error) {
	return c.acquire(ctx, c.user, limit, requestID)
}

func (c *lateReplySlotCache) ReleaseAccountSlot(_ context.Context, _ int64, requestID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.account, requestID)
	return nil
}

func (c *lateReplySlotCache) ReleaseUserSlot(_ context.Context, _ int64, requestID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.user, requestID)
	return nil
}

func (c *lateReplySlotCache) held() (account, user, calls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.account), len(c.user), c.calls
}

// The caller's deadline (one wait slice) ends while the reply is in flight.
// The acquire must still report the slot it holds, so it can be used and
// released; over many rounds nothing accumulates.
func TestConcurrencyService_AcquireConfirmsSlotWhenReplyOutlivesCallerDeadline(t *testing.T) {
	cache := newLateReplySlotCache()
	cache.replyDelay = 80 * time.Millisecond
	svc := NewConcurrencyService(cache)

	for round := 0; round < 5; round++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		result, err := svc.AcquireAccountSlot(ctx, 1, 3)
		cancel()
		require.NoError(t, err, "a slot Redis already granted must not surface as a deadline error")
		require.True(t, result.Acquired)
		account, _, _ := cache.held()
		require.Equal(t, 1, account, "round %d: slots must not pile up across wait slices", round)
		result.ReleaseFunc()
		result.ReleaseFunc() // idempotent
		account, _, _ = cache.held()
		require.Zero(t, account)

		ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
		userResult, err := svc.AcquireUserSlot(ctx, 9, 3)
		cancel()
		require.NoError(t, err)
		require.True(t, userResult.Acquired)
		userResult.ReleaseFunc()
		_, user, _ := cache.held()
		require.Zero(t, user)
	}
}

// The reply is lost altogether: the outcome is unknown, so the request id is
// released defensively and the caller gets an error.
func TestConcurrencyService_AcquireReleasesSlotWhenReplyIsLost(t *testing.T) {
	cache := newLateReplySlotCache()
	cache.loseReply = true
	svc := NewConcurrencyService(cache)

	for round := 0; round < 10; round++ {
		result, err := svc.AcquireAccountSlot(context.Background(), 1, 3)
		require.Error(t, err)
		require.Nil(t, result)
		userResult, err := svc.AcquireUserSlot(context.Background(), 9, 3)
		require.Error(t, err)
		require.Nil(t, userResult)
		account, user, _ := cache.held()
		require.Zero(t, account, "round %d: an unconfirmed slot must be released at once", round)
		require.Zero(t, user)
	}
}

// A caller that is already cancelled never sends the acquire.
func TestConcurrencyService_AcquireSkipsCancelledCaller(t *testing.T) {
	cache := newLateReplySlotCache()
	svc := NewConcurrencyService(cache)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.AcquireAccountSlot(ctx, 1, 3)
	require.ErrorIs(t, err, context.Canceled)
	_, err = svc.AcquireUserSlot(ctx, 9, 3)
	require.ErrorIs(t, err, context.Canceled)
	account, user, calls := cache.held()
	require.Zero(t, account)
	require.Zero(t, user)
	require.Zero(t, calls)
}
