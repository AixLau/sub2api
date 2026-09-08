package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	PublicTransitSchemaVersion = "ai-transit.v1"
	PublicTransitSystem        = "sub2api"
	PublicTransitWellKnownPath = "/.well-known/ai-transit.json"
	PublicTransitSnapshotPath  = "/api/public/transit/v1/snapshot"
)

type PublicTransitDiscovery struct {
	SchemaVersion string `json:"schema_version"`
	System        string `json:"system"`
	SnapshotURL   string `json:"snapshot_url"`
	GeneratedAt   string `json:"generated_at"`
}

// Public DTOs deliberately do not embed monitor DTOs: future internal fields must
// never become public implicitly.
type PublicTransitSnapshot struct {
	SchemaVersion string                  `json:"schema_version"`
	System        string                  `json:"system"`
	GeneratedAt   string                  `json:"generated_at"`
	Monitoring    PublicTransitMonitoring `json:"monitoring"`
	Groups        []PublicTransitGroup    `json:"groups"`
	Models        []PublicTransitModel    `json:"models"`
}

type PublicTransitMonitoring struct {
	Source   string                    `json:"source"`
	Range    string                    `json:"range"`
	Coverage PublicTransitCoverage     `json:"coverage"`
	Metrics  PublicTransitMetrics      `json:"metrics"`
	Health   PublicTransitHealth       `json:"health"`
	Trend    []PublicTransitTrendPoint `json:"trend"`
}

type PublicTransitCoverage struct {
	RequestedStart   time.Time `json:"requested_start"`
	RequestedEnd     time.Time `json:"requested_end"`
	CoverageStart    time.Time `json:"coverage_start"`
	DataThrough      time.Time `json:"data_through"`
	ComputedAt       time.Time `json:"computed_at"`
	CoverageComplete bool      `json:"coverage_complete"`
	BucketSeconds    int       `json:"bucket_seconds"`
}

type PublicTransitMetrics struct {
	// Rates use the same 0..1 units and ignored-error policy as channel monitor v2.
	ErrorRate   float64              `json:"error_rate"`
	SuccessRate float64              `json:"success_rate"`
	CacheRate   float64              `json:"cache_rate"`
	TTFT        PublicTransitLatency `json:"ttft"`
	Duration    PublicTransitLatency `json:"duration"`
}

type PublicTransitLatency struct {
	P50Ms *int64   `json:"p50_ms"`
	P90Ms *int64   `json:"p90_ms"`
	P95Ms *int64   `json:"p95_ms"`
	AvgMs *float64 `json:"avg_ms"`
}

type PublicTransitHealth struct {
	Overall   string   `json:"overall"`
	ErrorRate string   `json:"error_rate"`
	TTFT      string   `json:"ttft"`
	Cache     string   `json:"cache"`
	Score     *float64 `json:"score"`
}

type PublicTransitTrendPoint struct {
	BucketStart time.Time            `json:"bucket_start"`
	Metrics     PublicTransitMetrics `json:"metrics"`
	Health      PublicTransitHealth  `json:"health"`
}

type PublicTransitGroup struct {
	Name           string                    `json:"name"`
	Platform       string                    `json:"platform"`
	RateMultiplier float64                   `json:"rate_multiplier"`
	Metrics        PublicTransitMetrics      `json:"metrics"`
	Health         PublicTransitHealth       `json:"health"`
	Trend          []PublicTransitTrendPoint `json:"trend"`
}

type PublicTransitModel struct {
	Platform string               `json:"platform"`
	Model    string               `json:"model"`
	Metrics  PublicTransitMetrics `json:"metrics"`
	Health   PublicTransitHealth  `json:"health"`
}

type publicTransitMonitorReader interface {
	ParseFilter(string, []string, []string, []int64) (ChannelMonitorV2Filter, error)
	Snapshot(context.Context, ChannelMonitorV2Filter, bool) (*ChannelMonitorV2Snapshot, error)
	Models(context.Context, ChannelMonitorV2Filter, bool) (*ChannelMonitorV2List[ChannelMonitorV2ModelRow], error)
	Matrix(context.Context, ChannelMonitorV2Filter, ChannelMonitorV2GroupBy, bool) (*ChannelMonitorV2Matrix, error)
}

type publicTransitGroupReader interface {
	ListActive(context.Context) ([]Group, error)
}

type PublicTransitService struct {
	monitor publicTransitMonitorReader
	groups  publicTransitGroupReader
}

func (s *SettingService) PublicTransitEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return true
	}
	v, err := s.settingRepo.GetValue(ctx, SettingKeyPublicTransitEnabled)
	return err != nil || !isFalseSettingValue(v)
}

func NewPublicTransitService(monitor *ChannelMonitorV2Service, groups *GroupService) *PublicTransitService {
	if monitor == nil || groups == nil {
		return &PublicTransitService{}
	}
	return &PublicTransitService{monitor: monitor, groups: groups}
}

func (s *PublicTransitService) Discovery(context.Context, string) (*PublicTransitDiscovery, error) {
	return &PublicTransitDiscovery{
		SchemaVersion: PublicTransitSchemaVersion,
		System:        PublicTransitSystem,
		SnapshotURL:   PublicTransitSnapshotPath,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *PublicTransitService) Snapshot(ctx context.Context, rangeValue string) (*PublicTransitSnapshot, error) {
	if s.monitor == nil || s.groups == nil {
		return nil, errors.New("public transit dependencies unavailable")
	}
	if rangeValue == "" {
		rangeValue = "7d"
	}
	filter, err := s.monitor.ParseFilter(rangeValue, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	groups, err := s.groups.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("load public transit groups: %w", err)
	}
	publicGroups := make(map[int64]Group)
	filter.RestrictGroups = true
	filter.AllowedGroupIDs = make([]int64, 0, len(groups))
	for _, group := range groups {
		if group.Status != StatusActive || group.IsExclusive {
			continue
		}
		publicGroups[group.ID] = group
		filter.AllowedGroupIDs = append(filter.AllowedGroupIDs, group.ID)
	}

	snapshot, err := s.monitor.Snapshot(ctx, filter, false)
	if err != nil {
		return nil, err
	}
	models, err := s.monitor.Models(ctx, filter, false)
	if err != nil {
		return nil, err
	}
	matrix, err := s.monitor.Matrix(ctx, filter, ChannelMonitorV2GroupByPlatformGroup, false)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || models == nil || matrix == nil {
		return nil, errors.New("public transit monitor returned incomplete data")
	}

	result := &PublicTransitSnapshot{
		SchemaVersion: PublicTransitSchemaVersion,
		System:        PublicTransitSystem,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Monitoring: PublicTransitMonitoring{
			Source:   "channel-monitor-v2",
			Range:    filter.Range,
			Coverage: publicTransitCoverage(snapshot.Coverage),
			Metrics:  publicTransitMetrics(snapshot.Metrics),
			Health:   publicTransitHealth(snapshot.Health),
			Trend:    publicTransitTrend(snapshot.Trend),
		},
		Groups: make([]PublicTransitGroup, 0, len(matrix.Items)),
		Models: make([]PublicTransitModel, 0, len(models.Items)),
	}
	for _, row := range matrix.Items {
		if row.GroupID == nil {
			continue
		}
		group, public := publicGroups[*row.GroupID]
		if !public {
			continue
		}
		result.Groups = append(result.Groups, PublicTransitGroup{
			Name:           group.Name,
			Platform:       row.Platform,
			RateMultiplier: group.RateMultiplier,
			Metrics:        publicTransitMetrics(row.Metrics),
			Health:         publicTransitHealth(row.Health),
			Trend:          publicTransitTrend(row.Buckets),
		})
	}
	for _, row := range models.Items {
		result.Models = append(result.Models, PublicTransitModel{
			Platform: row.Platform,
			Model:    row.Model,
			Metrics:  publicTransitMetrics(row.Metrics),
			Health:   publicTransitHealth(row.Health),
		})
	}
	return result, nil
}

func publicTransitCoverage(v ChannelMonitorV2Coverage) PublicTransitCoverage {
	return PublicTransitCoverage{
		RequestedStart: v.RequestedStart, RequestedEnd: v.RequestedEnd,
		CoverageStart: v.CoverageStart, DataThrough: v.DataThrough,
		ComputedAt: v.ComputedAt, CoverageComplete: v.CoverageComplete,
		BucketSeconds: v.BucketSeconds,
	}
}

func publicTransitMetrics(v ChannelMonitorV2Metric) PublicTransitMetrics {
	// Match the channel-monitor-v2 dashboard's 7d success-rate card: it is the
	// complement of the scored error rate. The raw SuccessRate field tracks
	// absolute successful requests and intentionally differs when ignored error
	// categories or retry attempts are present.
	successRate := 1 - v.ErrorRate
	if successRate < 0 {
		successRate = 0
	} else if successRate > 1 {
		successRate = 1
	}
	return PublicTransitMetrics{
		ErrorRate: v.ErrorRate, SuccessRate: successRate, CacheRate: v.CacheRate,
		TTFT: publicTransitLatency(v.TTFT), Duration: publicTransitLatency(v.Duration),
	}
}

func publicTransitLatency(v ChannelMonitorV2Latency) PublicTransitLatency {
	return PublicTransitLatency{P50Ms: v.P50Ms, P90Ms: v.P90Ms, P95Ms: v.P95Ms, AvgMs: v.AvgMs}
}

func publicTransitHealth(v ChannelMonitorV2Health) PublicTransitHealth {
	overall, errorRate, ttft, cache := v.Overall, v.ErrorRate, v.TTFT, v.Cache
	if overall == "" {
		overall = "unknown"
	}
	if errorRate == "" {
		errorRate = "unknown"
	}
	if ttft == "" {
		ttft = "unknown"
	}
	if cache == "" {
		cache = "unknown"
	}
	return PublicTransitHealth{Overall: overall, ErrorRate: errorRate, TTFT: ttft, Cache: cache, Score: v.Score}
}

func publicTransitTrend(rows []ChannelMonitorV2TrendPoint) []PublicTransitTrendPoint {
	out := make([]PublicTransitTrendPoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, PublicTransitTrendPoint{
			BucketStart: row.BucketStart,
			Metrics:     publicTransitMetrics(row.Metrics),
			Health:      publicTransitHealth(row.Health),
		})
	}
	return out
}
