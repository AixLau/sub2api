package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type adminUserUsageRepoStub struct {
	UsageLogRepository

	stats  *usagestats.UsageStats
	userID int64
	start  time.Time
	end    time.Time
	calls  int
}

func (r *adminUserUsageRepoStub) GetUserStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	r.userID = userID
	r.start = startTime
	r.end = endTime
	r.calls++
	return r.stats, nil
}

func TestAdminServiceGetUserUsageStatsUsesAggregatedUsageRepo(t *testing.T) {
	repo := &adminUserUsageRepoStub{
		stats: &usagestats.UsageStats{
			TotalRequests:            3,
			TotalInputTokens:         100,
			TotalOutputTokens:        50,
			TotalCacheCreationTokens: 7,
			TotalCacheReadTokens:     8,
			TotalCacheTokens:         15,
			TotalTokens:              165,
			TotalCost:                1.25,
			TotalActualCost:          0.75,
			AverageDurationMs:        321,
		},
	}
	svc := &adminServiceImpl{usageLogRepo: repo}

	result, err := svc.GetUserUsageStats(context.Background(), 42, "7d")
	require.NoError(t, err)

	require.Equal(t, int64(42), repo.userID)
	require.Equal(t, 1, repo.calls)
	require.True(t, repo.end.After(repo.start))

	var body map[string]any
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &body))

	require.Equal(t, "7d", body["period"])
	require.Equal(t, float64(3), body["total_requests"])
	require.Equal(t, float64(100), body["total_input_tokens"])
	require.Equal(t, float64(50), body["total_output_tokens"])
	require.Equal(t, float64(15), body["total_cache_tokens"])
	require.Equal(t, float64(7), body["total_cache_creation_tokens"])
	require.Equal(t, float64(8), body["total_cache_read_tokens"])
	require.Equal(t, float64(165), body["total_tokens"])
	require.Equal(t, 1.25, body["total_cost"])
	require.Equal(t, 0.75, body["total_actual_cost"])
	require.Equal(t, float64(321), body["average_duration_ms"])
}

func TestAdminServiceGetUserUsageStatsTodayStartsAtLocalDay(t *testing.T) {
	repo := &adminUserUsageRepoStub{stats: &usagestats.UsageStats{}}
	svc := &adminServiceImpl{usageLogRepo: repo}

	_, err := svc.GetUserUsageStats(context.Background(), 42, "today")
	require.NoError(t, err)

	require.Equal(t, timezone.StartOfDay(repo.end), repo.start)
}
