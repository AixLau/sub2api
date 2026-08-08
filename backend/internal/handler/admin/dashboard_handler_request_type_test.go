package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dashboardUsageRepoCapture struct {
	service.UsageLogRepository
	trendRequestType      *int16
	trendStream           *bool
	trendStart            time.Time
	trendEnd              time.Time
	modelRequestType      *int16
	modelStream           *bool
	modelStart            time.Time
	modelEnd              time.Time
	trendMismatch         *bool
	modelMismatch         *bool
	groupMismatch         *bool
	rankingLimit          int
	ranking               []usagestats.UserSpendingRankingItem
	rankingTotal          float64
	dashboardStats        *usagestats.DashboardStats
	globalStatsCalls      int
	rangeStatsCalls       int
	rangeStatsStart       time.Time
	rangeStatsEnd         time.Time
	userBreakdownDim      usagestats.UserBreakdownDimension
	userBreakdownLimit    int
	userBreakdownCaptured bool
}

func (s *dashboardUsageRepoCapture) GetDashboardStats(ctx context.Context) (*usagestats.DashboardStats, error) {
	s.globalStatsCalls++
	if s.dashboardStats != nil {
		return s.dashboardStats, nil
	}
	return &usagestats.DashboardStats{}, nil
}

func (s *dashboardUsageRepoCapture) GetDashboardStatsWithRange(ctx context.Context, start, end time.Time) (*usagestats.DashboardStats, error) {
	s.rangeStatsCalls++
	s.rangeStatsStart, s.rangeStatsEnd = start, end
	if s.dashboardStats != nil {
		return s.dashboardStats, nil
	}
	return &usagestats.DashboardStats{}, nil
}

func (s *dashboardUsageRepoCapture) GetUsageTrendWithUsageFilters(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	filters usagestats.UsageLogFilters,
) ([]usagestats.TrendDataPoint, error) {
	s.trendRequestType = filters.RequestType
	s.trendStream = filters.Stream
	s.trendMismatch = filters.UpstreamModelMismatch
	return []usagestats.TrendDataPoint{}, nil
}

func (s *dashboardUsageRepoCapture) GetUsageTrendWithFilters(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	userID, apiKeyID, accountID, groupID int64,
	model string,
	requestType *int16,
	stream *bool,
	billingType *int8,
	excludeUserIDs ...int64,
) ([]usagestats.TrendDataPoint, error) {
	s.trendStart = startTime
	s.trendEnd = endTime
	s.trendRequestType = requestType
	s.trendStream = stream
	return []usagestats.TrendDataPoint{}, nil
}

func (s *dashboardUsageRepoCapture) GetModelStatsWithUsageFiltersBySource(
	ctx context.Context,
	startTime, endTime time.Time,
	filters usagestats.UsageLogFilters,
	source string,
) ([]usagestats.ModelStat, error) {
	s.modelRequestType = filters.RequestType
	s.modelStream = filters.Stream
	s.modelMismatch = filters.UpstreamModelMismatch
	return []usagestats.ModelStat{}, nil
}

func (s *dashboardUsageRepoCapture) GetGroupStatsWithUsageFilters(
	ctx context.Context,
	startTime, endTime time.Time,
	filters usagestats.UsageLogFilters,
) ([]usagestats.GroupStat, error) {
	s.groupMismatch = filters.UpstreamModelMismatch
	return []usagestats.GroupStat{}, nil
}

func (s *dashboardUsageRepoCapture) GetModelStatsWithFilters(
	ctx context.Context,
	startTime, endTime time.Time,
	userID, apiKeyID, accountID, groupID int64,
	requestType *int16,
	stream *bool,
	billingType *int8,
	excludeUserIDs ...int64,
) ([]usagestats.ModelStat, error) {
	s.modelStart = startTime
	s.modelEnd = endTime
	s.modelRequestType = requestType
	s.modelStream = stream
	return []usagestats.ModelStat{}, nil
}

func (s *dashboardUsageRepoCapture) GetUserSpendingRanking(
	ctx context.Context,
	startTime, endTime time.Time,
	limit int,
) (*usagestats.UserSpendingRankingResponse, error) {
	s.rankingLimit = limit
	return &usagestats.UserSpendingRankingResponse{
		Ranking:         s.ranking,
		TotalActualCost: s.rankingTotal,
		TotalRequests:   44,
		TotalTokens:     1234,
	}, nil
}

func (s *dashboardUsageRepoCapture) GetUserBreakdownStats(
	ctx context.Context,
	startTime, endTime time.Time,
	dim usagestats.UserBreakdownDimension,
	limit int,
) ([]usagestats.UserBreakdownItem, error) {
	s.userBreakdownDim = dim
	s.userBreakdownLimit = limit
	s.userBreakdownCaptured = true
	return []usagestats.UserBreakdownItem{}, nil
}

func newDashboardRequestTypeTestRouter(repo *dashboardUsageRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/trend", handler.GetUsageTrend)
	router.GET("/admin/dashboard/models", handler.GetModelStats)
	router.GET("/admin/dashboard/groups", handler.GetGroupStats)

	router.GET("/admin/dashboard/users-ranking", handler.GetUserSpendingRanking)
	return router
}

func TestDashboardTrendRequestTypePriority(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/trend?request_type=ws_v2&stream=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.trendRequestType)
	require.Equal(t, int16(service.RequestTypeWSV2), *repo.trendRequestType)
	require.Nil(t, repo.trendStream)
}

func TestDashboardTrendInvalidRequestType(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/trend?request_type=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDashboardTrendInvalidStream(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/trend?stream=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDashboardModelStatsRequestTypePriority(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?request_type=sync&stream=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.modelRequestType)
	require.Equal(t, int16(service.RequestTypeSync), *repo.modelRequestType)
	require.Nil(t, repo.modelStream)
}

func TestDashboardModelStatsInvalidRequestType(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?request_type=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDashboardSnapshotRoundsOnlyHourlyTrendStart(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	start := time.Date(2026, 7, 22, 12, 50, 0, 0, time.UTC)
	end := time.Date(2026, 7, 23, 12, 50, 0, 0, time.UTC)
	url := "/admin/dashboard/snapshot-v2?include_stats=false&include_trend=true&include_model_stats=true&include_group_stats=false&granularity=hour" +
		"&start_time=" + start.Format(time.RFC3339) + "&end_time=" + end.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.trendStart.Equal(start.Add(-50*time.Minute)))
	require.Equal(t, end, repo.trendEnd)
	require.Equal(t, start, repo.modelStart)
	require.Equal(t, end, repo.modelEnd)
}

func TestDashboardSnapshotUsesGlobalStatsForAccumulatedSummary(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()
	repo := &dashboardUsageRepoCapture{
		dashboardStats: &usagestats.DashboardStats{
			TotalTokens:      78_580_000_000,
			TotalActualCost:  5_960,
			TotalAccountCost: 2_540,
			TotalCost:        3_440,
		},
	}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet,
		"/admin/dashboard/snapshot-v2?include_stats=true&include_trend=false&include_model_stats=false"+
			"&start_time=2026-07-22T12:00:00Z&end_time=2026-07-23T12:00:00Z",
		nil,
	)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	// With aggregation disabled in this test service, global stats use the
	// complete raw-log retention window instead of the request's chart range.
	require.Zero(t, repo.globalStatsCalls)
	require.Equal(t, 1, repo.rangeStatsCalls)
	require.True(t, repo.rangeStatsStart.Before(time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)))
	require.True(t, repo.rangeStatsEnd.After(time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)))
	require.Contains(t, rec.Body.String(), `"total_tokens":78580000000`)
	require.Contains(t, rec.Body.String(), `"total_actual_cost":5960`)
	require.Contains(t, rec.Body.String(), `"total_account_cost":2540`)
	require.Contains(t, rec.Body.String(), `"total_cost":3440`)
}

func TestDashboardUserBreakdownExcludeUserIDs(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/user-breakdown?exclude_user_ids=10,20%203", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.userBreakdownCaptured)
	require.Equal(t, []int64{10, 20, 3}, repo.userBreakdownDim.ExcludeUserIDs)
	require.Equal(t, 50, repo.userBreakdownLimit)
}

func TestDashboardModelStatsInvalidStream(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?stream=bad", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDashboardModelStatsInvalidModelSource(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?model_source=invalid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDashboardModelStatsValidModelSource(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?model_source=upstream", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestDashboardModelAuditFilterPropagatesToTrendModelAndGroupQueries(t *testing.T) {
	resetDashboardReadCachesForTest()
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	for _, path := range []string{
		"/admin/dashboard/trend?upstream_model_mismatch=true",
		"/admin/dashboard/models?upstream_model_mismatch=true",
		"/admin/dashboard/groups?upstream_model_mismatch=true",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, path)
	}

	require.NotNil(t, repo.trendMismatch)
	require.True(t, *repo.trendMismatch)
	require.NotNil(t, repo.modelMismatch)
	require.True(t, *repo.modelMismatch)
	require.NotNil(t, repo.groupMismatch)
	require.True(t, *repo.groupMismatch)
}

func TestDashboardModelAuditFilterRejectsInvalidBoolean(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardRequestTypeTestRouter(repo)

	for _, path := range []string{
		"/admin/dashboard/trend?upstream_model_mismatch=invalid",
		"/admin/dashboard/models?upstream_model_mismatch=invalid",
		"/admin/dashboard/groups?upstream_model_mismatch=invalid",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
}

func TestDashboardUsersRankingLimitAndCache(t *testing.T) {
	dashboardUsersRankingCache = newSnapshotCache(5 * time.Minute)
	repo := &dashboardUsageRepoCapture{
		ranking: []usagestats.UserSpendingRankingItem{
			{UserID: 7, Email: "rank@example.com", ActualCost: 10.5, Requests: 3, Tokens: 300},
		},
		rankingTotal: 88.8,
	}
	router := newDashboardRequestTypeTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/users-ranking?limit=100&start_date=2025-01-01&end_date=2025-01-02", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 50, repo.rankingLimit)
	require.Contains(t, rec.Body.String(), "\"total_actual_cost\":88.8")
	require.Contains(t, rec.Body.String(), "\"total_requests\":44")
	require.Contains(t, rec.Body.String(), "\"total_tokens\":1234")
	require.Equal(t, "miss", rec.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/admin/dashboard/users-ranking?limit=100&start_date=2025-01-01&end_date=2025-01-02", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
}
