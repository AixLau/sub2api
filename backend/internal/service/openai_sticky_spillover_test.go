package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mathrand "math/rand"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// spillSlotCache is an in-memory ConcurrencyCache that really counts slots, so
// tests can assert on oversubscription and slot leaks.
type spillSlotCache struct {
	ConcurrencyCache
	mu      sync.Mutex
	slots   map[int64]map[string]struct{}
	waiting map[int64]int
	maxSeen map[int64]int
	limits  map[int64]int
}

func newSpillSlotCache() *spillSlotCache {
	return &spillSlotCache{
		slots:   map[int64]map[string]struct{}{},
		waiting: map[int64]int{},
		maxSeen: map[int64]int{},
		limits:  map[int64]int{},
	}
}

func (c *spillSlotCache) AcquireAccountSlot(_ context.Context, accountID int64, maxConcurrency int, requestID string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slots[accountID] == nil {
		c.slots[accountID] = map[string]struct{}{}
	}
	c.limits[accountID] = maxConcurrency
	if len(c.slots[accountID]) >= maxConcurrency {
		return false, nil
	}
	c.slots[accountID][requestID] = struct{}{}
	if n := len(c.slots[accountID]); n > c.maxSeen[accountID] {
		c.maxSeen[accountID] = n
	}
	return true, nil
}

func (c *spillSlotCache) ReleaseAccountSlot(_ context.Context, accountID int64, requestID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.slots[accountID], requestID)
	return nil
}

func (c *spillSlotCache) GetAccountsLoadBatch(_ context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[int64]*AccountLoadInfo, len(accounts))
	for _, acc := range accounts {
		current := len(c.slots[acc.ID])
		waiting := c.waiting[acc.ID]
		rate := 0
		if acc.MaxConcurrency > 0 {
			rate = (current + waiting) * 100 / acc.MaxConcurrency
		}
		out[acc.ID] = &AccountLoadInfo{AccountID: acc.ID, CurrentConcurrency: current, WaitingCount: waiting, LoadRate: rate}
	}
	return out, nil
}

func (c *spillSlotCache) GetAccountWaitingCount(_ context.Context, accountID int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waiting[accountID], nil
}

func (c *spillSlotCache) inUse(accountID int64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.slots[accountID])
}

func (c *spillSlotCache) fill(accountID int64, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slots[accountID] == nil {
		c.slots[accountID] = map[string]struct{}{}
	}
	for i := 0; i < n; i++ {
		c.slots[accountID][fmt.Sprintf("filler-%d", i)] = struct{}{}
	}
}

func (c *spillSlotCache) drain(accountID int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slots[accountID] = map[string]struct{}{}
}

// spillSessionCache is a goroutine-safe, group-scoped sticky session cache.
type spillSessionCache struct {
	stubGatewayCache
	mu       sync.Mutex
	bindings map[string]int64
}

func newSpillSessionCache() *spillSessionCache {
	return &spillSessionCache{bindings: map[string]int64{}}
}

func spillCacheKey(groupID int64, key string) string { return fmt.Sprintf("%d|%s", groupID, key) }

func (c *spillSessionCache) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id, ok := c.bindings[spillCacheKey(groupID, sessionHash)]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (c *spillSessionCache) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bindings[spillCacheKey(groupID, sessionHash)] = accountID
	return nil
}

func (c *spillSessionCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *spillSessionCache) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.bindings, spillCacheKey(groupID, sessionHash))
	return nil
}

func (c *spillSessionCache) primary(groupID int64, sessionHash string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bindings[spillCacheKey(groupID, "openai:"+sessionHash)]
}

func (c *spillSessionCache) secondary(groupID int64, sessionHash string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bindings[spillCacheKey(groupID, openAISpilloverSessionKeyPrefix+sessionHash)]
}

func (c *spillSessionCache) bind(groupID int64, sessionHash string, accountID int64) {
	_ = c.SetSessionAccountID(context.Background(), groupID, "openai:"+sessionHash, accountID, time.Hour)
}

// spillAccountRepo hands out copies, like the real repository: Account caches
// derived data lazily, so sharing one struct across goroutines would race in
// the test double rather than in the scheduler.
type spillAccountRepo struct {
	stubOpenAIAccountRepo
}

func (r spillAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	account, err := r.stubOpenAIAccountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	clone := *account
	return &clone, nil
}

type spillTestEnv struct {
	svc      *OpenAIGatewayService
	slots    *spillSlotCache
	sessions *spillSessionCache
	groupID  int64
}

func spillTestAccount(id int64, concurrency int, priority int, groupID int64) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, Concurrency: concurrency, Priority: priority, GroupIDs: []int64{groupID},
	}
}

func newSpillTestEnv(t testing.TB, enabled bool, accounts ...Account) *spillTestEnv {
	t.Helper()
	groupID := int64(7)
	for i := range accounts {
		accounts[i].GroupIDs = []int64{groupID}
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 3
	cfg.Gateway.Scheduling.StickySessionWaitTimeout = 45 * time.Second
	cfg.Gateway.Scheduling.FallbackWaitTimeout = 30 * time.Second
	cfg.Gateway.Scheduling.FallbackMaxWaiting = 100
	if enabled {
		cfg.Gateway.Scheduling.StickySessionSpilloverEnabled = true
		cfg.Gateway.Scheduling.StickySessionSpilloverTTL = 10 * time.Minute
		cfg.Gateway.Scheduling.CapacityWeightedSelection = true
		cfg.Gateway.Scheduling.WaitReselectInterval = 3 * time.Second
	}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	slots := newSpillSlotCache()
	sessions := newSpillSessionCache()
	concurrency := NewConcurrencyService(slots)
	concurrency.SetAccountLoadBatchCacheTTL(0)
	svc := &OpenAIGatewayService{
		accountRepo:        spillAccountRepo{stubOpenAIAccountRepo{accounts: accounts}},
		cache:              sessions,
		cfg:                cfg,
		concurrencyService: concurrency,
	}
	return &spillTestEnv{svc: svc, slots: slots, sessions: sessions, groupID: groupID}
}

func (e *spillTestEnv) selectLegacy(t testing.TB, sessionHash string, excluded map[int64]struct{}) *AccountSelectionResult {
	t.Helper()
	selection, err := e.svc.SelectAccountWithLoadAwareness(context.Background(), &e.groupID, sessionHash, "gpt-5.1", excluded)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	return selection
}

// ---------------------------------------------------------------------------
// Legacy (load-aware) scheduler
// ---------------------------------------------------------------------------

func TestOpenAIStickySpillover_PrimaryWithFreeSlotIsAlwaysUsed(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	// A recorded secondary must not pull traffic off a primary that has room.
	env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)

	for i := 0; i < 2; i++ {
		selection := env.selectLegacy(t, "s", nil)
		require.True(t, selection.Acquired)
		require.Equal(t, int64(1), selection.Account.ID)
	}
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
}

func TestOpenAIStickySpillover_FullPrimaryBorrowsIdlePeerWithoutWaiting(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)

	selection := env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired, "a full sticky account must not queue while a peer is idle")
	require.Nil(t, selection.WaitPlan)
	require.Equal(t, int64(2), selection.Account.ID)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"), "first (cold) spill keeps the primary binding")
	require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"), "borrowed account is remembered as the secondary")
}

func TestOpenAIStickySpillover_ReusesWarmSecondaryAndSwapsRoles(t *testing.T) {
	// Account 3 is the emptiest and largest, yet the warm secondary (2) must win.
	env := newSpillTestEnv(t, true,
		spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 10, 1, 0), spillTestAccount(3, 500, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)
	env.slots.fill(1, 2)
	env.slots.fill(2, 5)

	for i := 0; i < 20; i++ {
		env.sessions.bind(env.groupID, "s", 1)
		env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)
		selection := env.selectLegacy(t, "s", nil)
		require.True(t, selection.Acquired)
		require.Equal(t, int64(2), selection.Account.ID, "warm secondary must be preferred over a colder, emptier peer")
		selection.ReleaseFunc()
	}
	require.Equal(t, int64(2), env.sessions.primary(env.groupID, "s"), "warm hit promotes the secondary to primary")
	require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"), "old primary stays as the warm fallback")

	// After the swap the session never touches a third account while either of
	// its two warm accounts has room.
	env.slots.drain(1)
	env.slots.fill(2, 10)
	selection := env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired)
	require.Equal(t, int64(1), selection.Account.ID)
}

func TestOpenAIStickySpillover_SecondaryFullFallsToAnotherPeer(t *testing.T) {
	env := newSpillTestEnv(t, true,
		spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 2, 1, 0), spillTestAccount(3, 5, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)
	env.slots.fill(1, 2)
	env.slots.fill(2, 2)

	selection := env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired)
	require.Equal(t, int64(3), selection.Account.ID)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"), "a cold spill never rewrites the primary")
	require.Equal(t, int64(3), env.sessions.secondary(env.groupID, "s"))
}

func TestOpenAIStickySpillover_AllFullWaitsOnPrimaryWithBoundedPlan(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 3, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)
	env.slots.fill(2, 3)

	selection := env.selectLegacy(t, "s", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(1), selection.Account.ID, "with no free capacity anywhere the cache-warm primary is the queue")
	require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
	require.Equal(t, 3, selection.WaitPlan.MaxWaiting)
	require.Equal(t, 3*time.Second, selection.WaitPlan.ReselectAfter)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Zero(t, env.sessions.secondary(env.groupID, "s"))

	// Primary queue is also full: fall back to the generic bounded wait.
	env.slots.waiting[1] = 3
	selection = env.selectLegacy(t, "s", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, 30*time.Second, selection.WaitPlan.Timeout)
	require.Equal(t, 100, selection.WaitPlan.MaxWaiting)
	require.Equal(t, 3*time.Second, selection.WaitPlan.ReselectAfter)
}

func TestOpenAIStickySpillover_DisabledKeepsLegacyStickyWait(t *testing.T) {
	env := newSpillTestEnv(t, false, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)

	selection := env.selectLegacy(t, "s", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(1), selection.Account.ID)
	require.Zero(t, selection.WaitPlan.ReselectAfter)
	require.Zero(t, env.sessions.secondary(env.groupID, "s"))
}

func TestOpenAIStickySpillover_SecondaryMustPassNormalEligibility(t *testing.T) {
	rateLimited := time.Now().Add(time.Hour)
	limited := spillTestAccount(2, 50, 1, 0)
	limited.RateLimitResetAt = &rateLimited
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), limited, spillTestAccount(3, 5, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)

	// Rate-limited secondary is skipped.
	env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)
	selection := env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired)
	require.Equal(t, int64(3), selection.Account.ID)
	selection.ReleaseFunc()

	// A secondary that failed over in this request (excluded) is skipped too,
	// and a secondary outside the group's pool is never selected.
	for _, secondary := range []int64{3, 999} {
		env.sessions.bind(env.groupID, "s", 1)
		env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", secondary)
		selection = env.selectLegacy(t, "s", map[int64]struct{}{3: {}})
		require.False(t, selection.Acquired, "no eligible peer: must queue instead of using an ineligible secondary")
		require.Equal(t, int64(1), selection.Account.ID)
	}
}

func TestOpenAIStickySpillover_BindingsAreScopedBySessionAndGroup(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	ctx := context.Background()
	otherGroup := env.groupID + 1
	env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", 2)

	require.Equal(t, int64(2), env.svc.getOpenAISpilloverAccountID(ctx, &env.groupID, "s"))
	require.Zero(t, env.svc.getOpenAISpilloverAccountID(ctx, &env.groupID, "other-session"))
	require.Zero(t, env.svc.getOpenAISpilloverAccountID(ctx, &otherGroup, "s"))
	require.Zero(t, env.svc.getOpenAISpilloverAccountID(ctx, &env.groupID, ""))
}

func TestOpenAIStickySpillover_RecordDoesNotRebindWhenNotAllowed(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	ctx := context.Background()
	env.sessions.bind(env.groupID, "s", 1)

	// Warm hit but rebinding is not allowed (profit gate defers binding).
	env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 2, 2, false)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"))

	// Selecting the primary itself is not a spill.
	env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 2, 1, true)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"))
}

// ---------------------------------------------------------------------------
// Capacity-weighted placement
// ---------------------------------------------------------------------------

func TestOrderOpenAIAccountsByFreeCapacity_PriorityTiersThenFreeSlots(t *testing.T) {
	rng := mathrand.New(mathrand.NewSource(1))
	build := func() []accountWithLoad {
		small := spillTestAccount(1, 5, 1, 0)
		big := spillTestAccount(2, 45, 1, 0)
		full := spillTestAccount(3, 10, 1, 0)
		preferred := spillTestAccount(4, 1, 0, 0)
		return []accountWithLoad{
			{account: &small, loadInfo: &AccountLoadInfo{AccountID: 1}},
			{account: &big, loadInfo: &AccountLoadInfo{AccountID: 2}},
			{account: &full, loadInfo: &AccountLoadInfo{AccountID: 3, CurrentConcurrency: 8, WaitingCount: 2, LoadRate: 100}},
			{account: &preferred, loadInfo: &AccountLoadInfo{AccountID: 4}},
		}
	}
	const rounds = 20000
	first := map[int64]int{}
	for i := 0; i < rounds; i++ {
		order := build()
		orderOpenAIAccountsByFreeCapacity(order, rng.Float64)
		require.Equal(t, int64(4), order[0].account.ID, "a better priority always goes first")
		require.Len(t, order, 4)
		first[order[1].account.ID]++
	}
	// Free slots are 5 : 45 : 0 → roughly 10% : 90% : ~0%.
	require.InDelta(t, 0.10, float64(first[1])/rounds, 0.02)
	require.InDelta(t, 0.90, float64(first[2])/rounds, 0.02)
	require.Less(t, first[3], rounds/100)
}

func TestOpenAICapacityWeightedSelection_NewSessionsFollowCapacity(t *testing.T) {
	run := func(enabled bool) map[int64]int {
		env := newSpillTestEnv(t, enabled,
			spillTestAccount(1, 5, 1, 0), spillTestAccount(2, 50, 1, 0), spillTestAccount(3, 50, 1, 0))
		bound := map[int64]int{}
		for i := 0; i < 2100; i++ {
			session := fmt.Sprintf("new-%d", i)
			selection := env.selectLegacy(t, session, nil)
			require.True(t, selection.Acquired)
			selection.ReleaseFunc() // sessions are idle between turns
			bound[env.sessions.primary(env.groupID, session)]++
		}
		return bound
	}

	uniform := run(false)
	weighted := run(true)
	t.Logf("sessions bound per account (5/50/50 slots): legacy=%v capacity_weighted=%v", uniform, weighted)

	// Legacy placement ignores capacity when accounts are idle: the 5-slot
	// account receives about a third of all sessions.
	require.Greater(t, uniform[1], 500)
	// Capacity-weighted placement gives it about 5/105 of them.
	require.InDelta(t, 2100*5/105, weighted[1], 60)
	require.InDelta(t, 1000, weighted[2], 150)
	require.InDelta(t, 1000, weighted[3], 150)
}

// ---------------------------------------------------------------------------
// Advanced scheduler
// ---------------------------------------------------------------------------

func newAdvancedSpillTestEnv(t *testing.T, accounts ...Account) *spillTestEnv {
	t.Helper()
	env := newSpillTestEnv(t, true, accounts...)
	env.svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	return env
}

func TestOpenAIStickySpillover_AdvancedSchedulerBorrowsWarmSecondary(t *testing.T) {
	env := newAdvancedSpillTestEnv(t,
		spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 10, 1, 0), spillTestAccount(3, 500, 1, 0))
	ctx := context.Background()
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)

	// Cold spill: some peer is borrowed, the primary binding is untouched.
	selection, decision, err := env.svc.SelectAccountWithScheduler(ctx, &env.groupID, "", "s", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.True(t, selection.Acquired)
	require.NotEqual(t, int64(1), selection.Account.ID)
	require.False(t, decision.StickySessionHit)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, selection.Account.ID, env.sessions.secondary(env.groupID, "s"))
	selection.ReleaseFunc()

	// Warm spill: the recorded secondary wins regardless of weighting.
	for i := 0; i < 20; i++ {
		env.sessions.bind(env.groupID, "s", 1)
		env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", 2)
		selection, _, err = env.svc.SelectAccountWithScheduler(ctx, &env.groupID, "", "s", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
		require.True(t, selection.Acquired)
		require.Equal(t, int64(2), selection.Account.ID)
		selection.ReleaseFunc()
	}
	require.Equal(t, int64(2), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"))
}

func TestOpenAIStickySpillover_AdvancedSchedulerAllFullWaitsOnPrimary(t *testing.T) {
	env := newAdvancedSpillTestEnv(t, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 3, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)
	env.slots.fill(2, 3)

	selection, decision, err := env.svc.SelectAccountWithScheduler(context.Background(), &env.groupID, "", "s", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(1), selection.Account.ID)
	require.Equal(t, 3*time.Second, selection.WaitPlan.ReselectAfter)
	require.True(t, decision.StickySessionHit)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, 3, env.slots.inUse(2), "a discarded spill attempt must not leak a slot")
}

// ---------------------------------------------------------------------------
// Concurrency
// ---------------------------------------------------------------------------

func TestOpenAIStickySpillover_ConcurrentRequestsNeverOversubscribeOrLeak(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("advanced=%v", advanced), func(t *testing.T) {
			accounts := []Account{
				spillTestAccount(1, 3, 1, 0), spillTestAccount(2, 4, 1, 0), spillTestAccount(3, 6, 1, 0),
			}
			env := newSpillTestEnv(t, true, accounts...)
			if advanced {
				env.svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
				resetOpenAIAdvancedSchedulerSettingCacheForTest()
				t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
			}
			const sessions = 4
			for i := 0; i < sessions; i++ {
				env.sessions.bind(env.groupID, fmt.Sprintf("s%d", i), 1)
			}

			var wg sync.WaitGroup
			var mu sync.Mutex
			acquired, queued := 0, 0
			for worker := 0; worker < 24; worker++ {
				wg.Add(1)
				go func(worker int) {
					defer wg.Done()
					session := fmt.Sprintf("s%d", worker%sessions)
					for i := 0; i < 40; i++ {
						var selection *AccountSelectionResult
						var err error
						if advanced {
							selection, _, err = env.svc.SelectAccountWithScheduler(context.Background(), &env.groupID, "", session, "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
						} else {
							selection, err = env.svc.SelectAccountWithLoadAwareness(context.Background(), &env.groupID, session, "gpt-5.1", nil)
						}
						if err != nil || selection == nil || selection.Account == nil {
							t.Errorf("selection failed: %v", err)
							return
						}
						mu.Lock()
						if selection.Acquired {
							acquired++
						} else {
							queued++
						}
						mu.Unlock()
						if selection.Acquired {
							time.Sleep(200 * time.Microsecond)
							selection.ReleaseFunc()
						} else if selection.WaitPlan == nil {
							t.Errorf("neither acquired nor wait plan")
							return
						}
					}
				}(worker)
			}
			wg.Wait()

			for _, account := range accounts {
				require.LessOrEqual(t, env.slots.maxSeen[account.ID], account.Concurrency, "account %d oversubscribed", account.ID)
				require.Zero(t, env.slots.inUse(account.ID), "account %d leaked a slot", account.ID)
			}
			require.Greater(t, acquired, 0)
			for i := 0; i < sessions; i++ {
				session := fmt.Sprintf("s%d", i)
				primary := env.sessions.primary(env.groupID, session)
				secondary := env.sessions.secondary(env.groupID, session)
				require.Contains(t, []int64{1, 2, 3}, primary)
				if secondary != 0 {
					require.Contains(t, []int64{1, 2, 3}, secondary)
				}
			}
			t.Logf("acquired=%d queued=%d", acquired, queued)
		})
	}
}

// ---------------------------------------------------------------------------
// Before / after simulation
// ---------------------------------------------------------------------------

type spillSimResult struct {
	requests      int
	queued        int // requests that could not get a slot at selection time
	waitTicks     int // total ticks spent queued
	maxWaitTicks  int
	timedOut      int
	cacheHits     int // served by an account that already served the session
	perAccount    map[int64]int
	peakInUse     map[int64]int
	idleWhileWait int // ticks where a request queued although a peer had a free slot
}

// runSpillSimulation replays one deterministic workload against the legacy
// scheduler: a small account and two large ones, many bursty sessions.
func runSpillSimulation(t *testing.T, enabled bool) spillSimResult {
	t.Helper()
	accounts := []Account{spillTestAccount(473, 10, 1, 0), spillTestAccount(487, 50, 1, 0), spillTestAccount(493, 50, 1, 0)}
	env := newSpillTestEnv(t, enabled, accounts...)
	capacity := map[int64]int{473: 10, 487: 50, 493: 50}
	rng := mathrand.New(mathrand.NewSource(20261112))

	const (
		sessions     = 90
		ticks        = 1500
		holdTicks    = 12 // request duration
		waitTimeout  = 45 // ticks a request may queue
		reselectTick = 3  // reselect interval in ticks
		burstChance  = 0.03
	)
	type inflight struct {
		release func()
		until   int
	}
	type waiter struct {
		session   string
		accountID int64
		since     int
		reselect  bool
	}
	result := spillSimResult{perAccount: map[int64]int{}, peakInUse: map[int64]int{}}
	served := map[string]map[int64]bool{}
	var running []inflight
	var waiting []waiter

	serve := func(session string, selection *AccountSelectionResult, release func(), tick int) {
		id := selection.Account.ID
		if served[session] == nil {
			served[session] = map[int64]bool{}
		}
		if served[session][id] {
			result.cacheHits++
		}
		served[session][id] = true
		result.perAccount[id]++
		running = append(running, inflight{release: release, until: tick + holdTicks})
	}
	peerHasFreeSlot := func(except int64) bool {
		for id, limit := range capacity {
			if id != except && env.slots.inUse(id) < limit {
				return true
			}
		}
		return false
	}

	for tick := 0; tick < ticks; tick++ {
		keep := running[:0]
		for _, r := range running {
			if r.until <= tick {
				r.release()
			} else {
				keep = append(keep, r)
			}
		}
		running = keep

		// Queued requests, emulating the handler: poll the planned account, and
		// with ReselectAfter re-run selection every reselectTick.
		stillWaiting := waiting[:0]
		for _, w := range waiting {
			waited := tick - w.since
			if waited >= waitTimeout {
				result.timedOut++
				result.waitTicks += waited
				if waited > result.maxWaitTicks {
					result.maxWaitTicks = waited
				}
				env.slots.waiting[w.accountID]--
				continue
			}
			var selection *AccountSelectionResult
			var release func()
			if w.reselect && waited > 0 && waited%reselectTick == 0 {
				env.slots.waiting[w.accountID]--
				selection = env.selectLegacy(t, w.session, nil)
				if selection.Acquired {
					release = selection.ReleaseFunc
				} else {
					w.accountID = selection.Account.ID
					env.slots.waiting[w.accountID]++
				}
			} else {
				acquire, err := env.svc.tryAcquireAccountSlot(context.Background(), w.accountID, capacity[w.accountID])
				require.NoError(t, err)
				if acquire.Acquired {
					env.slots.waiting[w.accountID]--
					release = acquire.ReleaseFunc
					account := accounts[0]
					for _, a := range accounts {
						if a.ID == w.accountID {
							account = a
						}
					}
					selection = &AccountSelectionResult{Account: &account, Acquired: true}
					// Handler binds the session to the account it waited on.
					env.sessions.bind(env.groupID, w.session, w.accountID)
				}
			}
			if release != nil {
				result.waitTicks += waited
				if waited > result.maxWaitTicks {
					result.maxWaitTicks = waited
				}
				serve(w.session, selection, release, tick)
				continue
			}
			if peerHasFreeSlot(w.accountID) {
				result.idleWhileWait++
			}
			stillWaiting = append(stillWaiting, w)
		}
		waiting = stillWaiting

		for i := 0; i < sessions; i++ {
			if rng.Float64() >= burstChance {
				continue
			}
			session := fmt.Sprintf("sim-%d", i)
			// A session turn fans out into 1-3 parallel requests.
			for n := 1 + rng.Intn(3); n > 0; n-- {
				result.requests++
				selection := env.selectLegacy(t, session, nil)
				if selection.Acquired {
					serve(session, selection, selection.ReleaseFunc, tick)
					continue
				}
				require.NotNil(t, selection.WaitPlan)
				result.queued++
				env.slots.waiting[selection.Account.ID]++
				waiting = append(waiting, waiter{
					session: session, accountID: selection.Account.ID, since: tick,
					reselect: selection.WaitPlan.ReselectAfter > 0,
				})
			}
		}
		for id := range capacity {
			if n := env.slots.inUse(id); n > result.peakInUse[id] {
				result.peakInUse[id] = n
			}
		}
	}
	return result
}

func TestOpenAIStickySpillover_SimulationBeforeAfter(t *testing.T) {
	before := runSpillSimulation(t, false)
	after := runSpillSimulation(t, true)
	report := func(name string, r spillSimResult) {
		t.Logf("%-6s requests=%d queued=%d (%.1f%%) wait_ticks=%d max_wait=%d timed_out=%d queued_while_peer_idle_ticks=%d cache_hit=%.1f%% per_account=%v peak_in_use=%v",
			name, r.requests, r.queued, 100*float64(r.queued)/float64(r.requests), r.waitTicks, r.maxWaitTicks,
			r.timedOut, r.idleWhileWait, 100*float64(r.cacheHits)/float64(r.requests), r.perAccount, r.peakInUse)
	}
	report("before", before)
	report("after", after)

	require.Equal(t, before.requests, after.requests, "both runs must replay the same workload")
	require.Greater(t, before.queued, 50, "the workload must reproduce sticky queueing with the legacy behaviour")
	require.Greater(t, before.idleWhileWait, 0, "legacy behaviour queues while peers are idle")
	require.Zero(t, after.idleWhileWait, "no request may stay queued while a peer has a free slot")
	require.Zero(t, after.waitTicks)
	require.Zero(t, after.queued, "group capacity is sufficient, so nothing should queue")
	require.Zero(t, after.timedOut)
	// Continuity must not be sacrificed: warm-account hit rate stays close to
	// the legacy behaviour.
	beforeHit := float64(before.cacheHits) / float64(before.requests)
	afterHit := float64(after.cacheHits) / float64(after.requests)
	require.Greater(t, afterHit, beforeHit-0.03)
	for id, limit := range map[int64]int{473: 10, 487: 50, 493: 50} {
		require.LessOrEqual(t, after.peakInUse[id], limit)
	}
}
