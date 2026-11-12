package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// spillLoadErrSlotCache makes the batched load query fail, forcing the
// priority + LRU fallback order.
type spillLoadErrSlotCache struct {
	*spillSlotCache
}

func (c spillLoadErrSlotCache) GetAccountsLoadBatch(context.Context, []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	return nil, errors.New("load batch unavailable")
}

// spillBarrierCache holds every reader of the secondary binding until `need`
// requests have read it, so all of them act on the same (soon stale) view.
// That is the interleaving a real deployment hits when one session overflows
// from several connections at once.
type spillBarrierCache struct {
	*spillSessionCache
	mu      sync.Mutex
	need    int
	arrived int
	release chan struct{}
}

func (c *spillBarrierCache) arm(need int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.need, c.arrived, c.release = need, 0, make(chan struct{})
}

func (c *spillBarrierCache) disarm() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.need = 0
}

func (c *spillBarrierCache) GetSessionAccountID(ctx context.Context, groupID int64, key string) (int64, error) {
	id, err := c.spillSessionCache.GetSessionAccountID(ctx, groupID, key)
	if !strings.HasPrefix(key, openAISpilloverSessionKeyPrefix) {
		return id, err
	}
	c.mu.Lock()
	if c.need == 0 {
		c.mu.Unlock()
		return id, err
	}
	c.arrived++
	if c.arrived == c.need {
		close(c.release)
	}
	release := c.release
	c.mu.Unlock()
	select {
	case <-release:
	case <-time.After(5 * time.Second):
	}
	return id, err
}

func newSpillBarrierEnv(t testing.TB, advanced bool, accounts ...Account) (*spillTestEnv, *spillBarrierCache) {
	t.Helper()
	env := newSpillTestEnv(t, true, accounts...)
	if advanced {
		enableAdvancedSpillScheduler(t, env)
	}
	barrier := &spillBarrierCache{spillSessionCache: env.sessions}
	env.svc.cache = barrier
	return env, barrier
}

func spillSelectConcurrently(t testing.TB, env *spillTestEnv, barrier *spillBarrierCache, advanced bool, session string, workers int) []*AccountSelectionResult {
	t.Helper()
	barrier.arm(workers)
	defer barrier.disarm()
	selections := make([]*AccountSelectionResult, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			selections[i] = spillSelect(t, env, advanced, session)
		}(i)
	}
	wg.Wait()
	return selections
}

func spillSelect(t testing.TB, env *spillTestEnv, advanced bool, session string) *AccountSelectionResult {
	t.Helper()
	var selection *AccountSelectionResult
	var err error
	if advanced {
		selection, _, err = env.svc.SelectAccountWithScheduler(context.Background(), &env.groupID, "", session, "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	} else {
		selection, err = env.svc.SelectAccountWithLoadAwareness(context.Background(), &env.groupID, session, "gpt-5.1", nil)
	}
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	return selection
}

func enableAdvancedSpillScheduler(t testing.TB, env *spillTestEnv) {
	t.Helper()
	env.svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
}

// ---------------------------------------------------------------------------
// 1. Primary/secondary binding consistency
// ---------------------------------------------------------------------------

// Concurrent requests of ONE session overflow to DIFFERENT accounts. Every
// request is served, but only one of them may claim the secondary; the primary
// is never rewritten by a first-time spill.
func TestOpenAIStickySpillover_SameSessionConcurrentSpillToDifferentAccounts(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("advanced=%v", advanced), func(t *testing.T) {
			const peers = 6
			for round := 0; round < 30; round++ {
				accounts := []Account{spillTestAccount(1, 1, 1, 0)}
				for id := int64(2); id < 2+peers; id++ {
					accounts = append(accounts, spillTestAccount(id, 1, 1, 0))
				}
				env, barrier := newSpillBarrierEnv(t, advanced, accounts...)
				env.sessions.bind(env.groupID, "s", 1)
				env.slots.fill(1, 1)
				before := SnapshotOpenAIStickySpilloverMetrics()

				selections := spillSelectConcurrently(t, env, barrier, advanced, "s", peers)

				served := map[int64]bool{}
				for _, selection := range selections {
					require.True(t, selection.Acquired, "every peer has a free slot, nobody may queue")
					require.NotEqual(t, int64(1), selection.Account.ID)
					require.False(t, served[selection.Account.ID], "account %d served two requests with one slot", selection.Account.ID)
					require.Equal(t, OpenAISchedulingRouteSpillNew, selection.spillRoute)
					served[selection.Account.ID] = true
				}
				require.Len(t, served, peers, "concurrent overflow must land on different accounts")

				primary := env.sessions.primary(env.groupID, "s")
				secondary := env.sessions.secondary(env.groupID, "s")
				require.Equal(t, int64(1), primary, "a first-time spill never rewrites the primary")
				require.True(t, served[secondary], "the secondary must be one of the accounts that served the session")

				after := SnapshotOpenAIStickySpilloverMetrics()
				require.Equal(t, int64(peers), after.NewAccountTotal-before.NewAccountTotal)
				require.Equal(t, int64(peers-1), after.BindingConflictTotal-before.BindingConflictTotal,
					"exactly one concurrent spill may claim the secondary")
				require.Zero(t, after.SwapTotal-before.SwapTotal)

				// The surviving pair is stable: the next overflow reuses the secondary.
				for _, selection := range selections {
					selection.ReleaseFunc()
				}
				env.slots.fill(1, 1)
				next := spillSelect(t, env, advanced, "s")
				require.True(t, next.Acquired)
				require.Equal(t, secondary, next.Account.ID)
				require.Equal(t, OpenAISchedulingRouteSpillSecondary, next.spillRoute)
				require.Equal(t, secondary, env.sessions.primary(env.groupID, "s"))
				require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"))
			}
		})
	}
}

// All requests read primary=1 / secondary=2. One of them reuses the secondary
// (and wants to swap) while the others overflow to account 3. Whatever the
// write order, the stored pair is one of the two self-consistent outcomes;
// the old non-atomic writes could leave primary=2 / secondary=3, silently
// dropping the old primary and spreading the session over three accounts.
func TestOpenAIStickySpillover_SameSessionConcurrentSwapAndNewSpillStayConsistent(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("advanced=%v", advanced), func(t *testing.T) {
			outcomes := map[[2]int64]int{}
			for round := 0; round < 60; round++ {
				env, barrier := newSpillBarrierEnv(t, advanced,
					spillTestAccount(1, 1, 1, 0), spillTestAccount(2, 1, 1, 0), spillTestAccount(3, 50, 1, 0))
				env.sessions.bind(env.groupID, "s", 1)
				env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)
				env.slots.fill(1, 1)

				selections := spillSelectConcurrently(t, env, barrier, advanced, "s", 6)

				reused := 0
				for _, selection := range selections {
					require.True(t, selection.Acquired)
					switch selection.Account.ID {
					case 2:
						reused++
						require.Equal(t, OpenAISchedulingRouteSpillSecondary, selection.spillRoute)
					case 3:
						require.Equal(t, OpenAISchedulingRouteSpillNew, selection.spillRoute)
					default:
						t.Fatalf("unexpected account %d", selection.Account.ID)
					}
				}
				require.Equal(t, 1, reused, "the secondary has a single free slot")

				pair := [2]int64{env.sessions.primary(env.groupID, "s"), env.sessions.secondary(env.groupID, "s")}
				require.Contains(t, [][2]int64{{2, 1}, {1, 3}}, pair,
					"a swap writes both halves or neither, and never mixes with a concurrent spill")
				outcomes[pair]++

				for _, selection := range selections {
					selection.ReleaseFunc()
				}
				for _, id := range []int64{2, 3} {
					require.Zero(t, env.slots.inUse(id), "account %d leaked a slot", id)
				}
			}
			t.Logf("outcomes: %v", outcomes)
		})
	}
}

func TestOpenAIStickySpillover_StaleViewNeverOverwritesNewerBinding(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 2, 1, 0), spillTestAccount(3, 2, 1, 0))
	ctx := context.Background()
	env.sessions.bind(env.groupID, "s", 1)

	// Two requests both read primary=1 / no secondary, then spill to 2 and 3.
	require.Equal(t, OpenAISchedulingRouteSpillNew, env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 0, 2, true, time.Hour))
	require.Equal(t, OpenAISchedulingRouteSpillNew, env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 0, 3, true, time.Hour))
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"), "the first spill keeps the secondary")

	// A request reuses secondary 2 and swaps.
	require.Equal(t, OpenAISchedulingRouteSpillSecondary, env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 2, 2, true, time.Hour))
	require.Equal(t, int64(2), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"))

	// Requests that still hold the pre-swap view must not touch the new pair.
	env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 2, 3, true, time.Hour)
	env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 1, 2, 2, true, time.Hour)
	require.Equal(t, int64(2), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"))

	// A deleted primary (account became unhealthy) is never recreated by a swap.
	require.NoError(t, env.svc.deleteStickySessionAccountID(ctx, &env.groupID, "s"))
	env.svc.recordOpenAIStickySpillover(ctx, &env.groupID, "s", 2, 1, 1, true, time.Hour)
	require.Zero(t, env.sessions.primary(env.groupID, "s"))
}

// ---------------------------------------------------------------------------
// 3. Scheduling path completeness
// ---------------------------------------------------------------------------

func TestOpenAIStickySpillover_LoadBatchDisabledStillSpills(t *testing.T) {
	env := newSpillTestEnv(t, true,
		spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 10, 1, 0), spillTestAccount(3, 500, 1, 0))
	env.svc.cfg.Gateway.Scheduling.LoadBatchEnabled = false
	ctx := context.Background()
	env.sessions.bind(env.groupID, "s", 1)

	// Primary with a free slot is used as before.
	selection := env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired)
	require.Equal(t, int64(1), selection.Account.ID)
	selection.ReleaseFunc()

	// Primary full: borrow a peer instead of queueing.
	env.slots.fill(1, 2)
	selection = env.selectLegacy(t, "s", nil)
	require.True(t, selection.Acquired, "load_batch_enabled=false must not fall back to queueing on the full sticky account")
	require.NotEqual(t, int64(1), selection.Account.ID)
	require.Equal(t, OpenAISchedulingRouteSpillNew, selection.spillRoute)
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
	require.Equal(t, selection.Account.ID, env.sessions.secondary(env.groupID, "s"))
	selection.ReleaseFunc()

	// A recorded secondary is preferred and swapped in, exactly like the load-batch path.
	for _, secondary := range []int64{2, 3} {
		env.sessions.bind(env.groupID, "s", 1)
		env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", secondary)
		selection = env.selectLegacy(t, "s", nil)
		require.True(t, selection.Acquired)
		require.Equal(t, secondary, selection.Account.ID)
		require.Equal(t, OpenAISchedulingRouteSpillSecondary, selection.spillRoute)
		require.Equal(t, secondary, env.sessions.primary(env.groupID, "s"))
		require.Equal(t, int64(1), env.sessions.secondary(env.groupID, "s"))
		selection.ReleaseFunc()
	}
}

func TestOpenAIStickySpillover_LoadBatchDisabledAllFullWaitsOnPrimaryWithReselect(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 3, 1, 0))
	env.svc.cfg.Gateway.Scheduling.LoadBatchEnabled = false
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)
	env.slots.fill(2, 3)

	selection := env.selectLegacy(t, "s", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(1), selection.Account.ID)
	require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
	require.Equal(t, 3*time.Second, selection.WaitPlan.ReselectAfter)
	require.Equal(t, 2, env.slots.inUse(1))
	require.Equal(t, 3, env.slots.inUse(2), "probing peers must not leak a slot")

	// A session without a binding queues on the fallback plan, also with reselect.
	selection = env.selectLegacy(t, "fresh", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, 30*time.Second, selection.WaitPlan.Timeout)
	require.Equal(t, 3*time.Second, selection.WaitPlan.ReselectAfter)
}

func TestOpenAIStickySpillover_LoadBatchDisabledAndSpilloverOffKeepsLegacyWait(t *testing.T) {
	env := newSpillTestEnv(t, false, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
	env.svc.cfg.Gateway.Scheduling.LoadBatchEnabled = false
	env.sessions.bind(env.groupID, "s", 1)
	env.slots.fill(1, 2)

	selection := env.selectLegacy(t, "s", nil)
	require.False(t, selection.Acquired)
	require.NotNil(t, selection.WaitPlan)
	require.Equal(t, int64(1), selection.Account.ID)
	require.Equal(t, 45*time.Second, selection.WaitPlan.Timeout)
	require.Zero(t, selection.WaitPlan.ReselectAfter)
	require.Zero(t, env.sessions.secondary(env.groupID, "s"))
}

func TestOpenAIStickySpillover_LoadBatchErrorStillPrefersSecondary(t *testing.T) {
	env := newSpillTestEnv(t, true,
		spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 10, 1, 0), spillTestAccount(3, 500, 1, 0))
	concurrency := NewConcurrencyService(spillLoadErrSlotCache{env.slots})
	concurrency.SetAccountLoadBatchCacheTTL(0)
	env.svc.concurrencyService = concurrency
	ctx := context.Background()
	env.slots.fill(1, 2)

	for _, secondary := range []int64{2, 3} {
		env.sessions.bind(env.groupID, "s", 1)
		env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", secondary)
		selection := env.selectLegacy(t, "s", nil)
		require.True(t, selection.Acquired)
		require.Equal(t, secondary, selection.Account.ID, "the secondary must be tried first when load info is unavailable")
		selection.ReleaseFunc()
	}
}

// Both schedulers must make the same decisions for the same situation.
func TestOpenAIStickySpillover_LegacyAndAdvancedSchedulersAgree(t *testing.T) {
	type outcome struct {
		acquired  bool
		accountID int64
		route     string
		primary   int64
		secondary int64
		reselect  time.Duration
	}
	run := func(t *testing.T, advanced bool, setup func(env *spillTestEnv)) outcome {
		env := newSpillTestEnv(t, true,
			spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 4, 1, 0), spillTestAccount(3, 4, 2, 0))
		if advanced {
			enableAdvancedSpillScheduler(t, env)
		}
		env.sessions.bind(env.groupID, "s", 1)
		setup(env)
		selection := spillSelect(t, env, advanced, "s")
		out := outcome{
			acquired: selection.Acquired, accountID: selection.Account.ID, route: selection.spillRoute,
			primary: env.sessions.primary(env.groupID, "s"), secondary: env.sessions.secondary(env.groupID, "s"),
		}
		if selection.WaitPlan != nil {
			out.reselect = selection.WaitPlan.ReselectAfter
		}
		return out
	}
	ctx := context.Background()
	cases := map[string]func(env *spillTestEnv){
		"primary has room": func(env *spillTestEnv) {},
		"primary full, secondary recorded": func(env *spillTestEnv) {
			env.slots.fill(1, 2)
			env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", 3)
		},
		"primary full, secondary full": func(env *spillTestEnv) {
			env.slots.fill(1, 2)
			env.slots.fill(3, 4)
			env.svc.setOpenAISpilloverAccountID(ctx, &env.groupID, "s", 3)
		},
		"everything full": func(env *spillTestEnv) {
			env.slots.fill(1, 2)
			env.slots.fill(2, 4)
			env.slots.fill(3, 4)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			legacy := run(t, false, setup)
			advanced := run(t, true, setup)
			require.Equal(t, legacy, advanced)
		})
	}
}

// ---------------------------------------------------------------------------
// 2 / 5. Metrics: account reuse is not an upstream cache hit
// ---------------------------------------------------------------------------

func TestOpenAISchedulingRouteOf(t *testing.T) {
	require.Equal(t, OpenAISchedulingRouteLoadBalance, OpenAISchedulingRouteOf(nil, OpenAIAccountScheduleDecision{}))
	require.Equal(t, OpenAISchedulingRouteStickyPrimary, OpenAISchedulingRouteOf(&AccountSelectionResult{}, OpenAIAccountScheduleDecision{StickySessionHit: true}))
	require.Equal(t, OpenAISchedulingRoutePreviousResponse, OpenAISchedulingRouteOf(&AccountSelectionResult{}, OpenAIAccountScheduleDecision{StickyPreviousHit: true}))
	require.Equal(t, OpenAISchedulingRouteSpillSecondary,
		OpenAISchedulingRouteOf(&AccountSelectionResult{spillRoute: OpenAISchedulingRouteSpillSecondary}, OpenAIAccountScheduleDecision{}))
}

func TestOpenAISchedulingRouteUsage_SeparatesAccountReuseFromUpstreamCacheHits(t *testing.T) {
	before := SnapshotOpenAIStickySpilloverMetrics()

	// The secondary was reused twice, but upstream only served one request from cache.
	ObserveOpenAISchedulingRouteUsage(OpenAISchedulingRouteSpillSecondary, 1000, 800)
	ObserveOpenAISchedulingRouteUsage(OpenAISchedulingRouteSpillSecondary, 1000, 0)
	ObserveOpenAISchedulingRouteUsage(OpenAISchedulingRouteStickyPrimary, 500, 9999) // clamped to input
	ObserveOpenAISchedulingRouteUsage(OpenAISchedulingRouteSpillNew, 0, 0)           // no upstream usage: ignored
	ObserveOpenAISchedulingRouteUsage("unknown-route", 100, 100)                     // unbounded label: ignored

	after := SnapshotOpenAIStickySpilloverMetrics()
	delta := func(route string) OpenAISchedulingRouteUsageSnapshot {
		a, b := after.RouteUsage[route], before.RouteUsage[route]
		return OpenAISchedulingRouteUsageSnapshot{
			Requests: a.Requests - b.Requests, CacheHitRequests: a.CacheHitRequests - b.CacheHitRequests,
			InputTokens: a.InputTokens - b.InputTokens, CachedTokens: a.CachedTokens - b.CachedTokens,
		}
	}
	secondary := delta(OpenAISchedulingRouteSpillSecondary)
	require.Equal(t, int64(2), secondary.Requests)
	require.Equal(t, int64(1), secondary.CacheHitRequests)
	require.InDelta(t, 0.4, secondary.CachedTokenRatio(), 1e-9)
	primary := delta(OpenAISchedulingRouteStickyPrimary)
	require.Equal(t, int64(500), primary.CachedTokens)
	require.Zero(t, delta(OpenAISchedulingRouteSpillNew).Requests)

	snapshot := OpenAIStickySpilloverMetricsSnapshot{SecondaryReuseTotal: 3, NewAccountTotal: 1}
	require.Equal(t, int64(4), snapshot.SpillTotal())
	require.InDelta(t, 0.75, snapshot.SecondaryReuseRate(), 1e-9)
	require.Zero(t, OpenAIStickySpilloverMetricsSnapshot{}.SecondaryReuseRate())
}

func TestOpenAISchedulingMetricsHandler_ExposesEveryRequiredSeries(t *testing.T) {
	RecordOpenAIAccountWaitReselect()
	RecordOpenAIAccountWaitQueueFull()
	ObserveOpenAIAccountWait(OpenAIAccountWaitOutcomeAcquiredOtherAccount, 4*time.Second)
	ObserveOpenAIAccountWait("anything-else", time.Second)
	ObserveOpenAIAccountWait(OpenAIAccountWaitOutcomeTimeout, 0) // no wait: not observed

	recorder := httptest.NewRecorder()
	OpenAISchedulingMetricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	for _, series := range []string{
		`sub2api_openai_scheduling_events_total{event="primary_wait"}`,
		`sub2api_openai_scheduling_events_total{event="spill_secondary"}`,
		`sub2api_openai_scheduling_events_total{event="spill_new"}`,
		`sub2api_openai_scheduling_events_total{event="primary_swap"}`,
		`sub2api_openai_scheduling_events_total{event="binding_conflict"}`,
		`sub2api_openai_scheduling_events_total{event="wait_reselect"}`,
		`sub2api_openai_scheduling_events_total{event="wait_queue_full"}`,
		`sub2api_openai_scheduling_route_requests_total{route="spill_secondary"}`,
		`sub2api_openai_scheduling_route_cache_hit_requests_total{route="sticky_primary"}`,
		`sub2api_openai_scheduling_route_input_tokens_total{route="spill_new"}`,
		`sub2api_openai_scheduling_route_cached_tokens_total{route="load_balance"}`,
		`sub2api_openai_scheduling_account_wait_seconds_count{outcome="acquired_other_account"}`,
		`sub2api_openai_scheduling_account_wait_seconds_count{outcome="timeout"}`,
		`sub2api_openai_scheduling_account_wait_seconds_count{outcome="error"}`,
	} {
		require.Contains(t, body, series)
	}
}

// ---------------------------------------------------------------------------
// Secondary association lifetime: idle expiry, not a fixed lifetime
// ---------------------------------------------------------------------------

// A session that stays active for far longer than the spillover TTL keeps its
// secondary: the next overflow reuses it instead of moving to a third account.
func TestOpenAIStickySpillover_ActiveSessionKeepsSecondaryBeyondTTL(t *testing.T) {
	for _, mode := range []string{"legacy", "legacy_no_load_batch", "advanced"} {
		t.Run(mode, func(t *testing.T) {
			env := newSpillTestEnv(t, true,
				spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 4, 1, 0), spillTestAccount(3, 500, 1, 0))
			advanced := mode == "advanced"
			if advanced {
				enableAdvancedSpillScheduler(t, env)
			}
			if mode == "legacy_no_load_batch" {
				env.svc.cfg.Gateway.Scheduling.LoadBatchEnabled = false
			}
			env.sessions.bind(env.groupID, "s", 1)
			env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)

			// 30 minutes of traffic served by the primary, one request every 2 minutes.
			for i := 0; i < 15; i++ {
				env.sessions.advance(2 * time.Minute)
				selection := spillSelect(t, env, advanced, "s")
				require.True(t, selection.Acquired)
				require.Equal(t, int64(1), selection.Account.ID)
				selection.ReleaseFunc()
			}
			require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"),
				"an active session must not lose its secondary after the 10m TTL")

			// The overflow that follows stays on the same two accounts.
			env.slots.fill(1, 2)
			selection := spillSelect(t, env, advanced, "s")
			require.True(t, selection.Acquired)
			require.Equal(t, int64(2), selection.Account.ID, "no switch to a third account")
			require.Equal(t, OpenAISchedulingRouteSpillSecondary, selection.spillRoute)
		})
	}
}

// Once the session itself has been idle for longer than the TTL the secondary
// is forgotten, while the primary binding (1h) is unaffected.
func TestOpenAIStickySpillover_IdleSessionForgetsSecondaryButKeepsPrimary(t *testing.T) {
	env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 4, 1, 0))
	env.sessions.bind(env.groupID, "s", 1)
	env.svc.setOpenAISpilloverAccountID(context.Background(), &env.groupID, "s", 2)

	env.sessions.advance(9 * time.Minute)
	require.Equal(t, int64(2), env.sessions.secondary(env.groupID, "s"))
	env.sessions.advance(2 * time.Minute)
	require.Zero(t, env.sessions.secondary(env.groupID, "s"))
	require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))
}

// ---------------------------------------------------------------------------
// Staged rollout
// ---------------------------------------------------------------------------

func TestOpenAIStickySpillover_GroupAllowlistLimitsRollout(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(fmt.Sprintf("advanced=%v", advanced), func(t *testing.T) {
			env := newSpillTestEnv(t, true, spillTestAccount(1, 2, 1, 0), spillTestAccount(2, 50, 1, 0))
			if advanced {
				enableAdvancedSpillScheduler(t, env)
			}
			env.sessions.bind(env.groupID, "s", 1)
			env.slots.fill(1, 2)

			// This group is not in the rollout: it keeps queueing on the sticky account.
			env.svc.cfg.Gateway.Scheduling.StickySessionSpilloverGroupIDs = []int64{env.groupID + 1}
			selection := spillSelect(t, env, advanced, "s")
			if advanced {
				// Without spillover the advanced scheduler keeps its pre-existing
				// sticky escape (one-off move to a peer, no secondary recorded).
				require.Empty(t, selection.spillRoute)
				if selection.Acquired {
					selection.ReleaseFunc()
				}
			} else {
				require.False(t, selection.Acquired)
				require.NotNil(t, selection.WaitPlan)
				require.Equal(t, int64(1), selection.Account.ID)
			}
			require.Zero(t, env.sessions.secondary(env.groupID, "s"))
			require.Equal(t, int64(1), env.sessions.primary(env.groupID, "s"))

			// Adding the group turns spillover on for it.
			env.svc.cfg.Gateway.Scheduling.StickySessionSpilloverGroupIDs = []int64{env.groupID + 1, env.groupID}
			selection = spillSelect(t, env, advanced, "s")
			require.True(t, selection.Acquired)
			require.Equal(t, int64(2), selection.Account.ID)
		})
	}
}
