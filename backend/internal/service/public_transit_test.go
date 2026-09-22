package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type publicTransitGroupStub struct {
	groups []Group
	err    error
}

func (s publicTransitGroupStub) ListActive(context.Context) ([]Group, error) { return s.groups, s.err }

type publicTransitMonitorStub struct {
	snapshot *ChannelMonitorV2Snapshot
	models   *ChannelMonitorV2List[ChannelMonitorV2ModelRow]
	matrix   *ChannelMonitorV2Matrix
	filters  []ChannelMonitorV2Filter
	admins   []bool
	fail     string
}

func (s *publicTransitMonitorStub) ParseFilter(r string, platforms, models []string, ids []int64) (ChannelMonitorV2Filter, error) {
	return NewChannelMonitorV2Service(nil).ParseFilter(r, platforms, models, ids)
}
func (s *publicTransitMonitorStub) Snapshot(_ context.Context, f ChannelMonitorV2Filter, admin bool) (*ChannelMonitorV2Snapshot, error) {
	s.filters = append(s.filters, f)
	s.admins = append(s.admins, admin)
	if s.fail == "snapshot" {
		return nil, ErrChannelMonitorDisabled
	}
	return s.snapshot, nil
}
func (s *publicTransitMonitorStub) Models(_ context.Context, f ChannelMonitorV2Filter, admin bool) (*ChannelMonitorV2List[ChannelMonitorV2ModelRow], error) {
	s.filters = append(s.filters, f)
	s.admins = append(s.admins, admin)
	if s.fail == "models" {
		return nil, errors.New("models failed")
	}
	return s.models, nil
}
func (s *publicTransitMonitorStub) Matrix(_ context.Context, f ChannelMonitorV2Filter, groupBy ChannelMonitorV2GroupBy, admin bool) (*ChannelMonitorV2Matrix, error) {
	if groupBy != ChannelMonitorV2GroupByPlatformGroup {
		panic("unexpected grouping")
	}
	s.filters = append(s.filters, f)
	s.admins = append(s.admins, admin)
	if s.fail == "matrix" {
		return nil, errors.New("matrix failed")
	}
	return s.matrix, nil
}

func newPublicTransitFixture() (*PublicTransitService, *publicTransitMonitorStub) {
	p50, p90, p95, average, score := int64(120), int64(180), int64(210), 135.5, 98.0
	groupID, privateID, actorID := int64(10), int64(11), int64(999)
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	metrics := ChannelMonitorV2Metric{
		RequestCount: 10000, SuccessRequests: 9900, ErrorRequests: 100,
		ErrorRate: 0.01, SuccessRate: 0.99, CacheRate: 0.4,
		InputTokens: 900000, TokenCount: 1000000, CacheRateNumerator: 4000, CacheRateDenominator: 10000,
		RPM: 300, TPM: 50000,
		UpstreamAffectedRequests: &actorID, UpstreamAttemptCount: &actorID,
		TTFT:     ChannelMonitorV2Latency{SampleCount: 9000, P50Ms: &p50, P90Ms: &p90, P95Ms: &p95, AvgMs: &average},
		Duration: ChannelMonitorV2Latency{SampleCount: 9500, AvgMs: &average},
	}
	health := ChannelMonitorV2Health{Overall: "healthy", ErrorRate: "healthy", TTFT: "healthy", Cache: "healthy", Score: &score, MinimumSample: 10}
	point := ChannelMonitorV2TrendPoint{BucketStart: now, Metrics: metrics, Health: health}
	monitor := &publicTransitMonitorStub{
		snapshot: &ChannelMonitorV2Snapshot{
			Config: ChannelMonitorV2Config{GroupIDs: []int64{groupID, privateID}, UpdatedBy: &actorID},
			Coverage: ChannelMonitorV2Coverage{
				RequestedStart: now.Add(-7 * 24 * time.Hour), RequestedEnd: now,
				CoverageStart: now.Add(-3 * 24 * time.Hour), DataThrough: now,
				ComputedAt: now, CoverageComplete: false, BucketSeconds: 43200,
				Bootstrap: &ChannelMonitorV2Bootstrap{Active: true},
			},
			Metrics: metrics, Health: health, Trend: []ChannelMonitorV2TrendPoint{point},
		},
		models: &ChannelMonitorV2List[ChannelMonitorV2ModelRow]{Items: []ChannelMonitorV2ModelRow{{
			Platform: "openai", Model: "gpt-5", Metrics: metrics, Health: health,
		}}},
		matrix: &ChannelMonitorV2Matrix{Items: []ChannelMonitorV2MatrixRow{
			{Platform: "openai", GroupID: &groupID, GroupName: "internal-group-label", Metrics: metrics, Health: health, Buckets: []ChannelMonitorV2TrendPoint{point}},
			{Platform: "openai", GroupID: &privateID, GroupName: "private-vip"},
		}},
	}
	groups := publicTransitGroupStub{groups: []Group{
		{ID: groupID, Name: "public-pro", Platform: "openai", RateMultiplier: 1.25, Status: StatusActive},
		{ID: privateID, Name: "private-vip", Platform: "openai", Status: StatusActive, IsExclusive: true},
		{ID: 12, Name: "disabled-group", Platform: "openai", Status: StatusDisabled},
	}}
	return &PublicTransitService{monitor: monitor, groups: groups}, monitor
}

func TestPublicTransitSnapshotProjectsV2AndRestrictsPublicGroups(t *testing.T) {
	svc, monitor := newPublicTransitFixture()
	result, err := svc.Snapshot(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, PublicTransitSchemaVersion, result.SchemaVersion)
	require.Equal(t, "channel-monitor-v2", result.Monitoring.Source)
	require.Equal(t, "7d", result.Monitoring.Range)
	require.False(t, result.Monitoring.Coverage.CoverageComplete)
	require.Equal(t, monitor.snapshot.Coverage.CoverageStart, result.Monitoring.Coverage.CoverageStart)
	require.Equal(t, 0.99, result.Monitoring.Metrics.SuccessRate)
	require.Equal(t, 0.4, result.Monitoring.Metrics.CacheRate)
	require.Equal(t, int64(120), *result.Monitoring.Metrics.TTFT.P50Ms)
	require.Equal(t, 135.5, *result.Monitoring.Metrics.Duration.AvgMs)
	require.Equal(t, "healthy", result.Monitoring.Health.Overall)
	require.Equal(t, 98.0, *result.Monitoring.Health.Score)
	require.Len(t, result.Monitoring.Trend, 1)
	require.Equal(t, result.Monitoring.Metrics, result.Monitoring.Trend[0].Metrics)
	require.Len(t, result.Groups, 1)
	require.Equal(t, "public-pro", result.Groups[0].Name)
	require.Equal(t, 1.25, result.Groups[0].RateMultiplier)
	require.Len(t, result.Groups[0].Trend, 1)
	require.Len(t, result.Models, 1)
	require.Equal(t, "gpt-5", result.Models[0].Model)
	require.Equal(t, result.Monitoring.Metrics, result.Models[0].Metrics)
	require.Len(t, monitor.filters, 3)
	for i, filter := range monitor.filters {
		require.True(t, filter.RestrictGroups)
		require.Equal(t, []int64{10}, filter.AllowedGroupIDs)
		require.Empty(t, filter.GroupIDs)
		require.False(t, monitor.admins[i])
	}
}

func TestPublicTransitSnapshotNeverSerializesInternalFields(t *testing.T) {
	svc, _ := newPublicTransitFixture()
	result, err := svc.Snapshot(context.Background(), "24h")
	require.NoError(t, err)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	for _, field := range []string{
		"config", "group_ids", "group_id", "channel_id", "account_id", "user_id", "updated_by",
		"api_key", "token", "request_count", "success_requests", "error_requests", "sample_count",
		"input_tokens", "output_tokens", "token_count", "rpm", "tpm", "cache_rate_numerator",
		"cache_rate_denominator", "upstream_affected_requests", "upstream_attempt_count", "bootstrap",
		"minimum_sample", "thresholds",
	} {
		require.NotContains(t, string(body), `"`+field+`"`)
	}
	require.NotContains(t, string(body), "private-vip")
	require.NotContains(t, string(body), "internal-group-label")
	require.NotContains(t, string(body), "disabled-group")
}

func TestPublicTransitSnapshotEmptyPublicScopeDoesNotBecomeUnrestricted(t *testing.T) {
	svc, monitor := newPublicTransitFixture()
	svc.groups = publicTransitGroupStub{}
	monitor.snapshot = &ChannelMonitorV2Snapshot{}
	monitor.models = &ChannelMonitorV2List[ChannelMonitorV2ModelRow]{}
	monitor.matrix = &ChannelMonitorV2Matrix{}
	result, err := svc.Snapshot(context.Background(), "90m")
	require.NoError(t, err)
	require.NotNil(t, result.Groups)
	require.NotNil(t, result.Models)
	require.NotNil(t, result.Monitoring.Trend)
	require.Empty(t, result.Groups)
	require.Empty(t, result.Models)
	require.Equal(t, "unknown", result.Monitoring.Health.Overall)
	require.Nil(t, result.Monitoring.Metrics.TTFT.AvgMs)
	for _, filter := range monitor.filters {
		require.True(t, filter.RestrictGroups)
		require.Empty(t, filter.AllowedGroupIDs)
	}
}

func TestPublicTransitSnapshotRejectsUnavailableData(t *testing.T) {
	for _, failure := range []string{"snapshot", "models", "matrix", "groups", "nil_snapshot"} {
		t.Run(failure, func(t *testing.T) {
			svc, monitor := newPublicTransitFixture()
			monitor.fail = failure
			if failure == "groups" {
				svc.groups = publicTransitGroupStub{err: errors.New("groups failed")}
			}
			if failure == "nil_snapshot" {
				monitor.snapshot = nil
			}
			result, err := svc.Snapshot(context.Background(), "7d")
			require.Error(t, err)
			require.Nil(t, result)
		})
	}
}

func TestPublicTransitSnapshotRejectsUnsupportedRangeBeforeReadingData(t *testing.T) {
	svc, monitor := newPublicTransitFixture()
	_, err := svc.Snapshot(context.Background(), "15d")
	require.ErrorIs(t, err, ErrChannelMonitorV2InvalidRange)
	require.Empty(t, monitor.filters)
}

func TestPublicTransitSnapshotTrendResolution(t *testing.T) {
	for _, tc := range []struct {
		rangeValue string
		bucket     time.Duration
		points     int
	}{
		{"", 20 * time.Minute, 504},
		{"7d", 20 * time.Minute, 504},
		{"90m", 5 * time.Minute, 18},
		{"24h", time.Hour, 24},
		{"30d", 24 * time.Hour, 30},
	} {
		t.Run(tc.rangeValue, func(t *testing.T) {
			svc, monitor := newPublicTransitFixture()
			_, err := svc.Snapshot(context.Background(), tc.rangeValue)
			require.NoError(t, err)
			require.Len(t, monitor.filters, 3)
			for _, filter := range monitor.filters {
				require.Equal(t, tc.bucket, filter.Bucket)
				require.Equal(t, tc.points, int(filter.End.Sub(filter.Start)/filter.Bucket))
				require.LessOrEqual(t, tc.points, publicTransitMaxTrendPoints)
				require.Equal(t, filter.Start.Truncate(filter.Bucket), filter.Start)
				require.Equal(t, filter.End.Truncate(filter.Bucket), filter.End)
				require.Equal(t, monitor.filters[0], filter)
			}
		})
	}
}

func TestPublicTransitSnapshotRejectsMissingDependencies(t *testing.T) {
	result, err := NewPublicTransitService(nil, nil).Snapshot(context.Background(), "")
	require.Error(t, err)
	require.Nil(t, result)
}

func TestPublicTransitDiscovery(t *testing.T) {
	discovery, err := (&PublicTransitService{}).Discovery(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, PublicTransitSchemaVersion, discovery.SchemaVersion)
	require.Equal(t, PublicTransitSystem, discovery.System)
	require.Equal(t, PublicTransitSnapshotPath, discovery.SnapshotURL)
	_, err = time.Parse(time.RFC3339, discovery.GeneratedAt)
	require.NoError(t, err)
}
