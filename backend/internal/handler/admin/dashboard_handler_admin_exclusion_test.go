package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newDashboardAdminExclusionTestRouter(repo *dashboardUsageRepoCapture, adminService service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil, adminService)
	router := gin.New()
	router.GET("/admin/dashboard/models", handler.GetModelStats)
	router.GET("/admin/dashboard/groups", handler.GetGroupStats)
	router.GET("/admin/dashboard/user-breakdown", handler.GetUserBreakdown)
	router.GET("/admin/dashboard/snapshot-v2", handler.GetSnapshotV2)
	return router
}

func newDashboardAdminExclusionTestService() *stubAdminService {
	return &stubAdminService{users: []service.User{
		{ID: 101, Role: service.RoleAdmin},
		{ID: 202, Role: service.RoleAdmin},
	}}
}

func TestDashboardModelStatsExcludesAdminsByDefault(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?start_date=2026-03-01&end_date=2026-03-02", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{101, 202}, repo.modelExcludeUserIDs)
}

func TestDashboardModelStatsMergesExplicitExclusions(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?start_date=2026-03-01&end_date=2026-03-02&exclude_user_ids=9,101", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{9, 101, 202}, repo.modelExcludeUserIDs)
}

func TestDashboardModelStatsKeepsExplicitAdminDrilldown(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?start_date=2026-03-01&end_date=2026-03-02&user_id=101", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, repo.modelExcludeUserIDs)
}

func TestDashboardGroupStatsExcludesAdminsByDefault(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/groups?start_date=2026-03-01&end_date=2026-03-02", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{101, 202}, repo.groupExcludeUserIDs)
}

func TestDashboardUserBreakdownExcludesAdminsByDefault(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/user-breakdown?start_date=2026-03-01&end_date=2026-03-02&model=gpt-6", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.userBreakdownCaptured)
	require.Equal(t, []int64{101, 202}, repo.userBreakdownDim.ExcludeUserIDs)
}

func TestDashboardUserBreakdownKeepsExplicitAdminDrilldown(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/user-breakdown?start_date=2026-03-01&end_date=2026-03-02&user_id=101", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.userBreakdownCaptured)
	require.Empty(t, repo.userBreakdownDim.ExcludeUserIDs)
}

func TestDashboardSnapshotExcludesAdminsByDefault(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/snapshot-v2?start_date=2026-03-01&end_date=2026-03-02&include_stats=false&include_trend=true&include_model_stats=true&include_group_stats=true&include_users_trend=false", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{101, 202}, repo.trendExcludeUserIDs)
	require.Equal(t, []int64{101, 202}, repo.modelExcludeUserIDs)
	require.Equal(t, []int64{101, 202}, repo.groupExcludeUserIDs)
}

func TestDashboardSnapshotKeepsExplicitAdminDrilldown(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, newDashboardAdminExclusionTestService())

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/snapshot-v2?start_date=2026-03-01&end_date=2026-03-02&user_id=101&include_stats=false&include_trend=true&include_model_stats=true&include_group_stats=false&include_users_trend=false", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Empty(t, repo.trendExcludeUserIDs)
	require.Empty(t, repo.modelExcludeUserIDs)
}

func TestDashboardModelStatsWithoutAdminServiceKeepsExplicitExclusions(t *testing.T) {
	repo := &dashboardUsageRepoCapture{}
	router := newDashboardAdminExclusionTestRouter(repo, nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/models?start_date=2026-03-01&end_date=2026-03-02&exclude_user_ids=9", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{9}, repo.modelExcludeUserIDs)
}
