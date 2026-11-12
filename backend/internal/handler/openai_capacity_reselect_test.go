package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAICapacityWaitSlice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		return c
	}
	now := time.Unix(1_800_000_000, 0)
	plan := &service.AccountWaitPlan{Timeout: 10 * time.Second, ReselectAfter: 3 * time.Second}

	t.Run("callers that did not opt in keep the legacy single wait", func(t *testing.T) {
		timeout, reselect := openAICapacityWaitSlice(newContext(), plan, 10*time.Second, now)
		require.Equal(t, 10*time.Second, timeout)
		require.False(t, reselect)
	})

	t.Run("plans without a reselect interval keep the legacy single wait", func(t *testing.T) {
		c := newContext()
		enableOpenAICapacityReselect(c)
		timeout, reselect := openAICapacityWaitSlice(c, &service.AccountWaitPlan{Timeout: 10 * time.Second}, 5*time.Second, now)
		require.Equal(t, 5*time.Second, timeout)
		require.False(t, reselect)
	})

	t.Run("total wait is bounded by the first plan timeout across reselects", func(t *testing.T) {
		c := newContext()
		enableOpenAICapacityReselect(c)
		clock := now
		total := time.Duration(0)
		rounds := 0
		for {
			rounds++
			require.Less(t, rounds, 20, "reselect loop must terminate")
			// A later selection may hand out a longer plan; it must not extend the budget.
			current := plan
			if rounds > 1 {
				current = &service.AccountWaitPlan{Timeout: 120 * time.Second, ReselectAfter: 3 * time.Second}
			}
			timeout, reselect := openAICapacityWaitSlice(c, current, current.Timeout, clock)
			require.LessOrEqual(t, timeout, 3*time.Second)
			total += timeout
			clock = clock.Add(timeout)
			if !reselect {
				break
			}
		}
		require.Equal(t, 4, rounds) // 3s + 3s + 3s + 1s
		require.Equal(t, 10*time.Second, total)

		// Budget exhausted: only a token wait remains and no further reselect.
		timeout, reselect := openAICapacityWaitSlice(c, plan, plan.Timeout, clock)
		require.Equal(t, openAICapacityReselectMinRemaining, timeout)
		require.False(t, reselect)
	})
}

// Reselecting splits the wait that would have happened anyway; it never makes
// a request wait longer than it did before the feature existed.
func TestOpenAICapacityWaitSlice_KeepsFastFailCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Unix(1_800_000_000, 0)
	run := func(legacyTimeouts ...time.Duration) (time.Duration, int) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		enableOpenAICapacityReselect(c)
		plan := &service.AccountWaitPlan{Timeout: 30 * time.Second, ReselectAfter: 3 * time.Second}
		clock, total, rounds := now, time.Duration(0), 0
		for {
			legacy := legacyTimeouts[len(legacyTimeouts)-1]
			if rounds < len(legacyTimeouts) {
				legacy = legacyTimeouts[rounds]
			}
			rounds++
			require.Less(t, rounds, 100)
			slice, reselect := openAICapacityWaitSlice(c, plan, legacy, clock)
			total += slice
			clock = clock.Add(slice)
			if !reselect {
				return total, rounds
			}
		}
	}

	// Several candidates: the pre-existing 5s fast-fail cap is the whole budget,
	// although the plan timeout is 30s. One reselect still happens at 3s.
	total, rounds := run(5 * time.Second)
	require.Equal(t, 5*time.Second, total)
	require.Equal(t, 2, rounds)

	// Single candidate: the plan timeout is the budget, as before.
	total, rounds = run(30 * time.Second)
	require.Equal(t, 30*time.Second, total)
	require.Equal(t, 10, rounds)

	// A later round with a shorter allowance pulls the deadline in...
	total, _ = run(30*time.Second, 5*time.Second)
	require.Equal(t, 5*time.Second, total)
	// ...and one with a longer allowance never pushes it out.
	total, _ = run(5*time.Second, 120*time.Second)
	require.Equal(t, 5*time.Second, total)
}

// Queueing is split into reselect slices; ops must see the sum, and a wait
// that ended (slot acquired) must not leak its deadline into a later failover.
func TestOpenAICapacityWaitAccountingAcrossReselects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	enableOpenAICapacityReselect(c)
	plan := &service.AccountWaitPlan{AccountID: 1, Timeout: 10 * time.Second, ReselectAfter: 3 * time.Second}
	start := time.Now()

	// Three slices on account 1, each followed by a reselect.
	for i := 0; i < 3; i++ {
		slice, reselect := openAICapacityWaitSlice(c, plan, 10*time.Second, start.Add(time.Duration(i)*3*time.Second))
		require.Equal(t, 3*time.Second, slice)
		require.True(t, reselect)
		// setOpsSelectedAccount zeroes the ops value on every (re)selection.
		service.SetOpsLatencyMs(c, service.OpsAccountQueueWaitMsKey, 0)
		total := addOpenAICapacityWait(c, 1, slice)
		require.Equal(t, time.Duration(i+1)*3*time.Second, total)
	}
	waited := service.OpsLatencyMs(c, service.OpsAccountQueueWaitMsKey)
	require.NotNil(t, waited)
	require.Equal(t, 9000, *waited, "ops wait must be the sum of all slices, not the last one")

	// The budget is shared: only 1s is left, and it is the final slice.
	slice, reselect := openAICapacityWaitSlice(c, plan, 10*time.Second, start.Add(9*time.Second))
	require.Equal(t, time.Second, slice)
	require.False(t, reselect)

	// A reselect then acquires a slot on another account without waiting.
	service.SetOpsLatencyMs(c, service.OpsAccountQueueWaitMsKey, 0)
	settleOpenAICapacityWait(c, service.OpenAIAccountWaitOutcomeAcquired, 2)
	waited = service.OpsLatencyMs(c, service.OpsAccountQueueWaitMsKey)
	require.NotNil(t, waited)
	require.Equal(t, 9000, *waited, "the queueing that preceded a spill must still be reported")

	// A later failover that has to queue again gets a fresh budget.
	slice, reselect = openAICapacityWaitSlice(c, plan, 10*time.Second, start.Add(60*time.Second))
	require.Equal(t, 3*time.Second, slice, "an exhausted deadline must not carry over once a slot was acquired")
	require.True(t, reselect)
	require.Equal(t, time.Second, addOpenAICapacityWait(c, 1, time.Second), "accumulated wait restarts after settle")

	// Settling with no accumulated wait reports nothing and is safe.
	settleOpenAICapacityWait(c, service.OpenAIAccountWaitOutcomeTimeout, 0)
	settleOpenAICapacityWait(c, service.OpenAIAccountWaitOutcomeTimeout, 0)
	settleOpenAICapacityWait(nil, service.OpenAIAccountWaitOutcomeTimeout, 0)
}
