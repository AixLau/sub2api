package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicTransitSettingRepo struct {
	service.SettingRepository
	value string
}

func (s *publicTransitSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyPublicTransitEnabled {
		panic("unexpected setting")
	}
	return s.value, nil
}

type publicTransitReaderStub struct {
	result     *service.PublicTransitSnapshot
	err        error
	calls      int
	rangeValue string
}

func (s *publicTransitReaderStub) Snapshot(_ context.Context, rangeValue string) (*service.PublicTransitSnapshot, error) {
	s.calls++
	s.rangeValue = rangeValue
	return s.result, s.err
}

func (s *publicTransitReaderStub) Discovery(context.Context, string) (*service.PublicTransitDiscovery, error) {
	s.calls++
	return &service.PublicTransitDiscovery{
		SchemaVersion: service.PublicTransitSchemaVersion,
		System:        service.PublicTransitSystem,
		SnapshotURL:   service.PublicTransitSnapshotPath,
	}, s.err
}

func publicTransitTestRouter(svc publicTransitReader, settings *service.SettingService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := &PublicTransitHandler{svc: svc, settings: settings}
	r := gin.New()
	r.GET(service.PublicTransitWellKnownPath, h.Discovery)
	r.GET(service.PublicTransitSnapshotPath, h.Snapshot)
	r.GET("/api/v1/public/transit/snapshot", h.Snapshot)
	return r
}

func TestPublicTransitHandlerGateAppliesToEveryRouteAndChangesImmediately(t *testing.T) {
	for _, path := range []string{
		service.PublicTransitWellKnownPath, service.PublicTransitSnapshotPath, "/api/v1/public/transit/snapshot",
	} {
		t.Run(path, func(t *testing.T) {
			repo := &publicTransitSettingRepo{value: "false"}
			svc := &publicTransitReaderStub{result: &service.PublicTransitSnapshot{}}
			router := publicTransitTestRouter(svc, service.NewSettingService(repo, &config.Config{}))
			call := func() *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				return rec
			}
			rec := call()
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.Zero(t, svc.calls, "disabled public endpoints must not read monitoring data")

			repo.value = "true"
			rec = call()
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))
			require.Equal(t, 1, svc.calls)

			repo.value = "false"
			rec = call()
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.Equal(t, 1, svc.calls)
		})
	}
}

func TestPublicTransitHandlerDiscoveryUsesCanonicalRelativeURL(t *testing.T) {
	router := publicTransitTestRouter(&publicTransitReaderStub{}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, service.PublicTransitWellKnownPath, nil)
	req.Header.Set("X-Forwarded-Host", "attacker.invalid")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"schema_version":"ai-transit.v1","system":"sub2api","snapshot_url":"/api/public/transit/v1/snapshot","generated_at":""}`, rec.Body.String())
}

func TestPublicTransitHandlerBothSnapshotPathsHaveSamePublicContract(t *testing.T) {
	ttft := int64(125)
	snapshot := &service.PublicTransitSnapshot{
		SchemaVersion: service.PublicTransitSchemaVersion,
		System:        service.PublicTransitSystem,
		GeneratedAt:   "2026-09-07T00:00:00Z",
		Monitoring: service.PublicTransitMonitoring{
			Source: "channel-monitor-v2", Range: "7d",
			Metrics: service.PublicTransitMetrics{SuccessRate: 0.98, ErrorRate: 0.02, CacheRate: 0.4, TTFT: service.PublicTransitLatency{P50Ms: &ttft}},
			Health:  service.PublicTransitHealth{Overall: "healthy"},
			Trend:   []service.PublicTransitTrendPoint{},
		},
		Groups: []service.PublicTransitGroup{{Name: "public-pro", Platform: "openai", RateMultiplier: 1.25}},
		Models: []service.PublicTransitModel{{Platform: "openai", Model: "gpt-5"}},
	}
	svc := &publicTransitReaderStub{result: snapshot}
	router := publicTransitTestRouter(svc, nil)
	for _, path := range []string{service.PublicTransitSnapshotPath, "/api/v1/public/transit/snapshot"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path+"?range=7d&group_id=999&platform=private&model=secret&admin=true", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "7d", svc.rangeValue)
			require.Equal(t, "public, max-age=60", rec.Header().Get("Cache-Control"))
			var result map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Len(t, result, 6)
			require.Equal(t, "ai-transit.v1", result["schema_version"])
			require.Equal(t, "sub2api", result["system"])
			monitoring := result["monitoring"].(map[string]any)
			require.Equal(t, "channel-monitor-v2", monitoring["source"])
			require.Equal(t, 0.98, monitoring["metrics"].(map[string]any)["success_rate"])
			require.Len(t, result["groups"].([]any), 1)
			require.Len(t, result["models"].([]any), 1)
			for _, field := range []string{"config", "group_id", "channel_id", "account_id", "user_id", "api_key", "request_count", "sample_count", "rpm", "tpm", "updated_by"} {
				require.NotContains(t, rec.Body.String(), `"`+field+`"`)
			}
			require.NotContains(t, rec.Body.String(), "secret")
			require.NotContains(t, rec.Body.String(), "private")
		})
	}
}

func TestPublicTransitHandlerErrorsAreNotCachedAndDoNotExposeBackendDetails(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"monitor disabled", service.ErrChannelMonitorDisabled, http.StatusNotFound},
		{"invalid range", service.ErrChannelMonitorV2InvalidRange, http.StatusBadRequest},
		{"backend failure", errors.New("postgres://private-user:secret@internal-host failed"), http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := publicTransitTestRouter(&publicTransitReaderStub{err: test.err}, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, service.PublicTransitSnapshotPath, nil))
			require.Equal(t, test.status, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			require.NotContains(t, rec.Body.String(), "private-user")
			require.NotContains(t, rec.Body.String(), "secret")
			require.NotContains(t, rec.Body.String(), "internal-host")
		})
	}
}

type publicTransitV2Repo struct {
	service.ChannelMonitorV2Repository
	filters []service.ChannelMonitorV2Filter
}

func (s *publicTransitV2Repo) GetConfig(context.Context) (*service.ChannelMonitorV2Config, error) {
	return &service.ChannelMonitorV2Config{Enabled: true}, nil
}

func (s *publicTransitV2Repo) record(filter service.ChannelMonitorV2Filter, admin bool) {
	if admin {
		panic("public transit requested admin data")
	}
	s.filters = append(s.filters, filter)
}

func (s *publicTransitV2Repo) GetSnapshot(_ context.Context, filter service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2Snapshot, error) {
	s.record(filter, admin)
	actor := int64(12345)
	return &service.ChannelMonitorV2Snapshot{
		Config: service.ChannelMonitorV2Config{GroupIDs: []int64{10, 99}, UpdatedBy: &actor},
		// Public success rate complements scored errors, which can differ from
		// the raw success ratio when some error categories are excluded.
		Metrics: service.ChannelMonitorV2Metric{ErrorRate: 0.02, SuccessRate: 0.95, RequestCount: 12345, RPM: 6789},
		Health:  service.ChannelMonitorV2Health{Overall: "healthy"},
	}, nil
}

func (s *publicTransitV2Repo) GetModels(_ context.Context, filter service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2ModelRow], error) {
	s.record(filter, admin)
	return &service.ChannelMonitorV2List[service.ChannelMonitorV2ModelRow]{Items: []service.ChannelMonitorV2ModelRow{
		{Platform: "openai", Model: "gpt-5", Metrics: service.ChannelMonitorV2Metric{RequestCount: 12345}},
	}}, nil
}

func (s *publicTransitV2Repo) GetMatrix(_ context.Context, filter service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, groupBy service.ChannelMonitorV2GroupBy, admin bool) (*service.ChannelMonitorV2Matrix, error) {
	s.record(filter, admin)
	if groupBy != service.ChannelMonitorV2GroupByPlatformGroup {
		panic("unexpected matrix grouping")
	}
	id := int64(10)
	return &service.ChannelMonitorV2Matrix{Items: []service.ChannelMonitorV2MatrixRow{
		{Platform: "openai", GroupID: &id, GroupName: "internal-name", Metrics: service.ChannelMonitorV2Metric{RequestCount: 12345}},
	}}, nil
}

type publicTransitGroupsRepo struct {
	service.GroupRepository
}

func (s publicTransitGroupsRepo) ListActive(context.Context) ([]service.Group, error) {
	return []service.Group{
		{ID: 10, Name: "public-pro", Platform: "openai", Status: service.StatusActive},
		{ID: 99, Name: "private-vip", Platform: "openai", Status: service.StatusActive, IsExclusive: true},
	}, nil
}

func TestPublicTransitHandlerUsesRealV2ServiceWithOnlyPublicGroupScope(t *testing.T) {
	repo := &publicTransitV2Repo{}
	svc := service.NewPublicTransitService(
		service.NewChannelMonitorV2Service(repo),
		service.NewGroupService(publicTransitGroupsRepo{}, nil),
	)
	settings := service.NewSettingService(&publicTransitSettingRepo{value: "true"}, &config.Config{})
	router := publicTransitTestRouter(svc, settings)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, service.PublicTransitSnapshotPath+"?group_id=99&admin=true", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var result map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, "ai-transit.v1", result["schema_version"])
	require.Equal(t, 0.98, result["monitoring"].(map[string]any)["metrics"].(map[string]any)["success_rate"])
	require.Equal(t, "public-pro", result["groups"].([]any)[0].(map[string]any)["name"])
	require.Len(t, repo.filters, 3)
	for _, filter := range repo.filters {
		require.True(t, filter.RestrictGroups)
		require.Equal(t, []int64{10}, filter.AllowedGroupIDs)
		require.Empty(t, filter.GroupIDs)
	}
	for _, private := range []string{"12345", "6789", "private-vip", "internal-name", "group_ids", "group_id", "updated_by", "request_count", "sample_count"} {
		require.NotContains(t, rec.Body.String(), private)
	}
}
