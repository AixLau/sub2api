//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// waitSlotCache is an in-memory account slot gate that behaves like the Redis
// one: the slot is taken when the "script" runs, the reply may arrive later.
type waitSlotCache struct {
	fakeConcurrencyCache
	mu      sync.Mutex
	slots   map[string]struct{}
	waiting int
	maxWait int
	peak    int
	// freeAt: before this instant the account reports full.
	freeAt time.Time
	// replyDelay: time between the slot being taken and the reply arriving.
	replyDelay time.Duration
}

func (c *waitSlotCache) AcquireAccountSlot(ctx context.Context, _ int64, maxConcurrency int, requestID string) (bool, error) {
	c.mu.Lock()
	if time.Now().Before(c.freeAt) || len(c.slots) >= maxConcurrency {
		c.mu.Unlock()
		return false, nil
	}
	c.slots[requestID] = struct{}{}
	if len(c.slots) > c.peak {
		c.peak = len(c.slots)
	}
	delay := c.replyDelay
	c.mu.Unlock()
	if delay > 0 {
		// Redis already executed the script; only the reply is late. A caller
		// that gives up here never learns that it holds a slot.
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return true, nil
}

func (c *waitSlotCache) ReleaseAccountSlot(_ context.Context, _ int64, requestID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.slots, requestID)
	return nil
}

func (c *waitSlotCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waiting++
	if c.waiting > c.maxWait {
		c.maxWait = c.waiting
	}
	return true, nil
}

func (c *waitSlotCache) DecrementAccountWaitCount(context.Context, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.waiting--
	return nil
}

func (c *waitSlotCache) snapshot() (inUse, waiting, peak int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.slots), c.waiting, c.peak
}

type waitSlotFixture struct {
	h       *OpenAIGatewayHandler
	cache   *waitSlotCache
	account *service.Account
	w       *httptest.ResponseRecorder
	c       *gin.Context
	cancel  context.CancelFunc
}

func newWaitSlotFixture(t *testing.T, concurrency int) *waitSlotFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	account := &service.Account{
		ID: 11, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: concurrency,
	}
	cache := &waitSlotCache{slots: map[string]struct{}{}}
	concurrencyService := service.NewConcurrencyService(cache)
	gw := service.NewOpenAIGatewayService(nil,
		&openAIImagesFailoverAccountRepo{accounts: []service.Account{*account}}, nil, nil, nil, nil, nil, nil,
		&config.Config{RunMode: config.RunModeSimple}, nil, concurrencyService, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	enableOpenAICapacityReselect(c)
	return &waitSlotFixture{
		h: &OpenAIGatewayHandler{
			gatewayService:    gw,
			concurrencyHelper: NewConcurrencyHelper(concurrencyService, SSEPingFormatNone, 0),
		},
		cache: cache, account: account, w: w, c: c, cancel: cancel,
	}
}

// acquire runs the routing stage's loop: every openAISlotRetryReselect is a
// fresh selection that again returns a wait plan on the same account.
func (f *waitSlotFixture) acquire(t *testing.T, plan service.AccountWaitPlan, candidates int) (release func(), reason openAISlotRetryReason, acquired bool, reselects int) {
	t.Helper()
	groupID := int64(1)
	streamStarted := false
	for {
		require.Less(t, reselects, 200, "reselect loop must terminate")
		planCopy := plan
		planCopy.AccountID = f.account.ID
		planCopy.MaxConcurrency = f.account.Concurrency
		selection := &service.AccountSelectionResult{Account: f.account, WaitPlan: &planCopy, CandidateCount: candidates}
		release, _, acquired, reason = f.h.acquireResponsesAccountSlotForRequest(
			f.c, &groupID, "", selection, "gpt-5.1", false, "", "", false, &streamStarted, zap.NewNop())
		if reason != openAISlotRetryReselect {
			return release, reason, acquired, reselects
		}
		_, waiting, _ := f.cache.snapshot()
		require.Zero(t, waiting, "a reselect must leave the wait queue before selecting again")
		reselects++
	}
}

// Redis takes the slot but the reply only arrives after the wait slice has
// ended. The request must learn that it holds the slot (and use it); it must
// not walk away, reselect, and leave the slot behind.
func TestOpenAICapacityWait_SlotTakenWhileReplyIsLateIsNeverLeaked(t *testing.T) {
	f := newWaitSlotFixture(t, 3)
	f.cache.freeAt = time.Now().Add(220 * time.Millisecond)
	f.cache.replyDelay = 400 * time.Millisecond // far beyond one 150ms slice

	release, reason, acquired, _ := f.acquire(t, service.AccountWaitPlan{
		Timeout: 5 * time.Second, MaxWaiting: 10, ReselectAfter: 150 * time.Millisecond,
	}, 0)
	require.True(t, acquired, "reason=%v body=%s", reason, f.w.Body.String())
	require.NotNil(t, release)

	inUse, waiting, peak := f.cache.snapshot()
	require.Equal(t, 1, inUse, "exactly the slot this request uses")
	require.Equal(t, 1, peak, "repeated slices must not pile up slots for one request")
	require.Zero(t, waiting)

	release()
	release() // releasing is idempotent
	inUse, _, _ = f.cache.snapshot()
	require.Zero(t, inUse)
}

// Every account stays full: the request reselects each interval, never waits
// past the plan timeout, ends with a retryable capacity result, and leaves
// neither a wait-queue entry nor a slot behind.
func TestOpenAICapacityWait_AllFullWaitsAtMostThePlanTimeout(t *testing.T) {
	f := newWaitSlotFixture(t, 2)
	f.cache.freeAt = time.Now().Add(time.Hour)

	started := time.Now()
	release, reason, acquired, reselects := f.acquire(t, service.AccountWaitPlan{
		Timeout: 900 * time.Millisecond, MaxWaiting: 10, ReselectAfter: 200 * time.Millisecond,
	}, 0)
	elapsed := time.Since(started)

	require.False(t, acquired)
	require.Nil(t, release)
	require.Equal(t, openAISlotRetryCapacity, reason, "the caller answers 429")
	require.GreaterOrEqual(t, reselects, 3, "selection must be re-run while queueing")
	require.GreaterOrEqual(t, elapsed, 850*time.Millisecond)
	require.Less(t, elapsed, 900*time.Millisecond+openAICapacityReselectMinRemaining+400*time.Millisecond,
		"reselecting must not extend the total wait beyond the plan timeout")

	inUse, waiting, _ := f.cache.snapshot()
	require.Zero(t, inUse)
	require.Zero(t, waiting)
	waited := service.OpsLatencyMs(f.c, service.OpsAccountQueueWaitMsKey)
	require.NotNil(t, waited)
	require.GreaterOrEqual(t, *waited, 800, "ops must see the whole wait, not the last slice")
}

// The client goes away while queueing: no further reselect, nothing left in
// the wait queue, no slot held.
func TestOpenAICapacityWait_ClientCancelReleasesEverything(t *testing.T) {
	f := newWaitSlotFixture(t, 2)
	f.cache.freeAt = time.Now().Add(time.Hour)
	time.AfterFunc(250*time.Millisecond, f.cancel)

	started := time.Now()
	release, reason, acquired, _ := f.acquire(t, service.AccountWaitPlan{
		Timeout: 30 * time.Second, MaxWaiting: 10, ReselectAfter: 100 * time.Millisecond,
	}, 0)

	require.False(t, acquired)
	require.Nil(t, release)
	require.Equal(t, openAISlotRetryNone, reason, "a cancelled request is not retried")
	require.Less(t, time.Since(started), 2*time.Second)
	inUse, waiting, _ := f.cache.snapshot()
	require.Zero(t, inUse)
	require.Zero(t, waiting)
}

// The slot arrives at the very moment the client cancels: it is released
// through the request context instead of being held until the slot TTL.
func TestOpenAICapacityWait_SlotGrantedAfterCancelIsReleased(t *testing.T) {
	f := newWaitSlotFixture(t, 2)
	f.cache.freeAt = time.Now().Add(150 * time.Millisecond)
	f.cache.replyDelay = 300 * time.Millisecond
	time.AfterFunc(250*time.Millisecond, f.cancel) // while the reply is in flight

	release, _, acquired, _ := f.acquire(t, service.AccountWaitPlan{
		Timeout: 30 * time.Second, MaxWaiting: 10, ReselectAfter: 100 * time.Millisecond,
	}, 0)
	if acquired && release != nil {
		release()
	}
	require.Eventually(t, func() bool {
		inUse, waiting, _ := f.cache.snapshot()
		return inUse == 0 && waiting == 0
	}, 2*time.Second, 10*time.Millisecond, "a slot granted to a cancelled request must be released")
}
