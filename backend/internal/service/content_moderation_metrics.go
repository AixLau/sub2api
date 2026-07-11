package service

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ContentModerationMetrics struct {
	registry          *prometheus.Registry
	requests          *prometheus.CounterVec
	requestLatency    *prometheus.HistogramVec
	chunks            prometheus.Histogram
	cache             *prometheus.CounterVec
	providerCalls     *prometheus.CounterVec
	batch             *prometheus.CounterVec
	correlation       *prometheus.CounterVec
	forwardedEvidence *prometheus.CounterVec
	forcedFresh       *prometheus.CounterVec
	pendingReviewAge  prometheus.Gauge
	highSeverityMiss  prometheus.Counter
}

func NewContentModerationMetrics() *ContentModerationMetrics {
	registry := prometheus.NewRegistry()
	m := &ContentModerationMetrics{
		registry:          registry,
		requests:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_requests_total", Help: "Completed moderation requests."}, []string{"mode", "cache_state", "aggregate"}),
		requestLatency:    prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sub2api_moderation_request_duration_seconds", Help: "End-to-end moderation request latency.", Buckets: []float64{.05, .1, .25, .5, 1, 1.5, 3, 6, 8}}, []string{"mode", "cache_state"}),
		chunks:            prometheus.NewHistogram(prometheus.HistogramOpts{Name: "sub2api_moderation_chunks", Help: "Chunks required per moderation request.", Buckets: []float64{1, 2, 4, 8, 16, 32, 64}}),
		cache:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_cache_operations_total", Help: "Moderation cache operations."}, []string{"operation", "result"}),
		providerCalls:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_provider_calls_total", Help: "Fresh moderation provider calls and results."}, []string{"result"}),
		batch:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_batch_events_total", Help: "Bounded batch lifecycle events."}, []string{"event"}),
		correlation:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_correlation_total", Help: "Cyber-policy correlation outcomes."}, []string{"result"}),
		forwardedEvidence: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_forwarded_evidence_total", Help: "Audit evidence state for forwarded requests."}, []string{"state"}),
		forcedFresh:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_forced_fresh_total", Help: "Observe-mode forced-fresh comparisons."}, []string{"result"}),
		pendingReviewAge:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "sub2api_moderation_oldest_pending_review_seconds", Help: "Age of the oldest pending correlated review."}),
		highSeverityMiss:  prometheus.NewCounter(prometheus.CounterOpts{Name: "sub2api_moderation_confirmed_high_severity_misses_total", Help: "Confirmed high-severity correlated misses."}),
	}
	registry.MustRegister(m.requests, m.requestLatency, m.chunks, m.cache, m.providerCalls, m.batch, m.correlation, m.forwardedEvidence, m.forcedFresh, m.pendingReviewAge, m.highSeverityMiss)
	return m
}

func (m *ContentModerationMetrics) Handler() http.Handler {
	if m == nil || m.registry == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

func (m *ContentModerationMetrics) observeRequest(mode, cacheState string, level ModerationLevel, started time.Time, chunks int) {
	if m == nil {
		return
	}
	mode = boundedModerationMode(mode)
	cacheState = boundedModerationCacheState(cacheState)
	aggregate := boundedModerationAggregate(level)
	m.requests.WithLabelValues(mode, cacheState, aggregate).Inc()
	m.requestLatency.WithLabelValues(mode, cacheState).Observe(time.Since(started).Seconds())
	m.chunks.Observe(float64(chunks))
}

func boundedModerationMode(value string) string {
	if value == ContentModerationModeObserve {
		return "observe"
	}
	return "pre_block"
}
func boundedModerationCacheState(value string) string {
	switch value {
	case "all_hit", "incremental":
		return value
	default:
		return "cold"
	}
}
func boundedModerationAggregate(level ModerationLevel) string {
	switch level {
	case ModerationLevelPass:
		return "pass"
	case ModerationLevelReview:
		return "review"
	case ModerationLevelReject:
		return "reject"
	default:
		return "error"
	}
}
