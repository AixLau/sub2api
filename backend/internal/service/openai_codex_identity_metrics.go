package service

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Metrics deliberately omit user, account and identity labels. The number of
// time series is bounded even when every request introduces a new raw thread.
type codexIdentityMetrics struct {
	registry           *prometheus.Registry
	events             *prometheus.CounterVec
	indexedKeys        *prometheus.GaugeVec
	keyCountsAvailable prometheus.Gauge
	keyCountsUpdatedAt prometheus.Gauge
	scrapeMu           sync.Mutex
	providerMu         sync.RWMutex
	keyCountProvider   func(context.Context) (map[string]int64, error)
}

var codexIdentityTelemetry = newCodexIdentityMetrics()

func newCodexIdentityMetrics() *codexIdentityMetrics {
	m := &codexIdentityMetrics{
		registry: prometheus.NewRegistry(),
		events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sub2api_codex_identity_events_total", Help: "Codex HTTP identity operations and outcomes.",
		}, []string{"operation", "result"}),
		indexedKeys: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "sub2api_codex_identity_indexed_keys", Help: "Identity keys tracked by the ownership index; excludes legacy keys not yet adopted.",
		}, []string{"kind"}),
		keyCountsAvailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sub2api_codex_identity_key_counts_available", Help: "Whether the latest scrape obtained ownership index counts.",
		}),
		keyCountsUpdatedAt: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "sub2api_codex_identity_key_counts_updated_at_seconds", Help: "Unix time of the most recent successful ownership index count.",
		}),
	}
	m.registry.MustRegister(m.events, m.indexedKeys, m.keyCountsAvailable, m.keyCountsUpdatedAt)
	return m
}

func boundedCodexIdentityOperation(value string) string {
	switch value {
	case "period_session", "side_session", "thread_current", "thread_history", "side_fork", "current_parent", "fork_history", "identity_store", "ownership", "cleanup", "side_lifecycle":
		return value
	default:
		return "other"
	}
}

func boundedCodexIdentityResult(value string) string {
	switch value {
	case "created", "reused", "advanced", "stale_rejected", "contention", "pinned", "missing", "conflict", "error", "deleted", "observed", "registered":
		return value
	default:
		return "other"
	}
}

func boundedCodexIdentityKeyKind(value string) string {
	switch value {
	case "period_session", "thread_current", "thread_history", "side_session", "side_fork", "side_thread_current", "side_lifecycle", "legacy_v2", "legacy_v3", "ownership":
		return value
	default:
		return "other"
	}
}

// RecordCodexIdentityEvent records only bounded operation/result categories.
// Debug logging shares that redaction contract and excludes Redis keys, raw
// client identities, prompts and arbitrary error messages.
func RecordCodexIdentityEvent(operation, result string) {
	codexIdentityTelemetry.recordEvent(operation, result)
}

func (m *codexIdentityMetrics) recordEvent(operation, result string) {
	operation = boundedCodexIdentityOperation(operation)
	result = boundedCodexIdentityResult(result)
	m.events.WithLabelValues(operation, result).Inc()
	logger.L().Debug("codex.identity",
		zap.String("operation", operation), zap.String("result", result),
		zap.Bool(logger.OpsSystemLogSkipField, true),
	)
}

// SetCodexIdentityKeyCountProvider connects the existing Redis ownership index
// to monitoring. It is invoked only by authenticated metrics scrapes, never by
// normal requests, and does not scan the Redis keyspace or renew identities.
func SetCodexIdentityKeyCountProvider(provider func(context.Context) (map[string]int64, error)) {
	codexIdentityTelemetry.providerMu.Lock()
	codexIdentityTelemetry.keyCountProvider = provider
	codexIdentityTelemetry.providerMu.Unlock()
}

func (m *codexIdentityMetrics) handler() http.Handler {
	prometheusHandler := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.scrapeMu.Lock()
		defer m.scrapeMu.Unlock()
		m.providerMu.RLock()
		provider := m.keyCountProvider
		m.providerMu.RUnlock()
		if provider != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			counts, err := provider(ctx)
			cancel()
			if err != nil {
				m.keyCountsAvailable.Set(0)
				m.events.WithLabelValues("identity_store", "error").Inc()
			} else {
				// Reset removes vanished kinds instead of leaving stale gauges.
				m.indexedKeys.Reset()
				bounded := make(map[string]int64, len(counts))
				for kind, count := range counts {
					if count >= 0 {
						bounded[boundedCodexIdentityKeyKind(kind)] += count
					}
				}
				for kind, count := range bounded {
					m.indexedKeys.WithLabelValues(kind).Set(float64(count))
				}
				m.keyCountsAvailable.Set(1)
				m.keyCountsUpdatedAt.SetToCurrentTime()
			}
		} else {
			m.keyCountsAvailable.Set(0)
		}
		prometheusHandler.ServeHTTP(w, r)
	})
}

// CodexIdentityMetricsHandler serves the process counters and ownership-index
// gauges. Route registration must keep it behind the admin authentication chain.
func CodexIdentityMetricsHandler() http.Handler {
	return codexIdentityTelemetry.handler()
}
