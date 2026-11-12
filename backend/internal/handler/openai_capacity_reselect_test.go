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
