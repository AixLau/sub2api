package service

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type ContentModerationMetrics struct {
	registry                  *prometheus.Registry
	requests                  *prometheus.CounterVec
	requestLatency            *prometheus.HistogramVec
	chunks                    prometheus.Histogram
	cache                     *prometheus.CounterVec
	providerCalls             *prometheus.CounterVec
	batch                     *prometheus.CounterVec
	correlation               *prometheus.CounterVec
	forwardedEvidence         *prometheus.CounterVec
	forcedFresh               *prometheus.CounterVec
	semanticReview            *prometheus.CounterVec
	semanticReviewAttempts    *prometheus.CounterVec
	semanticLatency           *prometheus.HistogramVec
	semanticTokens            *prometheus.CounterVec
	receipts                  *prometheus.CounterVec
	promptInjectionReviews    *prometheus.CounterVec
	promptInjectionFailClosed *prometheus.CounterVec
	promptInjectionEvidence   prometheus.Histogram
	forwardConflicts          *prometheus.CounterVec
	pendingReviewAge          prometheus.Gauge
	highSeverityMiss          prometheus.Counter
}

func NewContentModerationMetrics() *ContentModerationMetrics {
	registry := prometheus.NewRegistry()
	m := &ContentModerationMetrics{
		registry:                  registry,
		requests:                  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_requests_total", Help: "Completed moderation requests."}, []string{"mode", "cache_state", "aggregate"}),
		requestLatency:            prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sub2api_moderation_request_duration_seconds", Help: "End-to-end moderation request latency.", Buckets: []float64{.05, .1, .25, .5, 1, 1.5, 3, 6, 8}}, []string{"mode", "cache_state"}),
		chunks:                    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "sub2api_moderation_chunks", Help: "Chunks required per moderation request.", Buckets: []float64{1, 2, 4, 8, 16, 32, 64}}),
		cache:                     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_cache_operations_total", Help: "Moderation cache operations."}, []string{"operation", "result"}),
		providerCalls:             prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_provider_calls_total", Help: "Fresh moderation provider calls and results."}, []string{"result"}),
		batch:                     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_batch_events_total", Help: "Bounded batch lifecycle events."}, []string{"event"}),
		correlation:               prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_correlation_total", Help: "Cyber-policy correlation outcomes."}, []string{"result"}),
		forwardedEvidence:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_forwarded_evidence_total", Help: "Audit evidence state for forwarded requests."}, []string{"state"}),
		forcedFresh:               prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_forced_fresh_total", Help: "Observe-mode forced-fresh comparisons."}, []string{"result"}),
		semanticReview:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_semantic_review_total", Help: "Platform semantic review outcomes."}, []string{"model", "result"}),
		semanticReviewAttempts:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_semantic_review_attempts_total", Help: "Platform semantic review account attempts and fallback decisions."}, []string{"model", "outcome", "reason"}),
		semanticLatency:           prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sub2api_moderation_semantic_review_duration_seconds", Help: "Platform semantic review end-to-end latency.", Buckets: []float64{.25, .5, .75, 1, 1.5, 2, 3, 5, 8}}, []string{"model", "result"}),
		semanticTokens:            prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_semantic_review_tokens_total", Help: "Platform semantic review token usage."}, []string{"model", "direction"}),
		receipts:                  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_receipts_total", Help: "Moderation execution receipts."}, []string{"pipeline", "outcome"}),
		promptInjectionReviews:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_prompt_injection_reviews_total", Help: "Prompt-injection reviewer outcomes."}, []string{"verdict", "complete"}),
		promptInjectionFailClosed: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_prompt_injection_fail_closed_total", Help: "Prompt-injection fail-closed outcomes."}, []string{"reason"}),
		promptInjectionEvidence:   prometheus.NewHistogram(prometheus.HistogramOpts{Name: "sub2api_moderation_prompt_injection_evidence_runes", Help: "Prompt-injection evidence size in runes.", Buckets: []float64{500, 1_000, 2_000, 4_000, 6_000, 8_000, 12_000}}),
		forwardConflicts:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sub2api_moderation_forward_conflicts_total", Help: "Attempts to forward without a completed moderation receipt."}, []string{"decision"}),
		pendingReviewAge:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "sub2api_moderation_oldest_pending_review_seconds", Help: "Age of the oldest pending correlated review."}),
		highSeverityMiss:          prometheus.NewCounter(prometheus.CounterOpts{Name: "sub2api_moderation_confirmed_high_severity_misses_total", Help: "Confirmed high-severity correlated misses."}),
	}
	registry.MustRegister(m.requests, m.requestLatency, m.chunks, m.cache, m.providerCalls, m.batch, m.correlation, m.forwardedEvidence, m.forcedFresh, m.semanticReview, m.semanticReviewAttempts, m.semanticLatency, m.semanticTokens, m.receipts, m.promptInjectionReviews, m.promptInjectionFailClosed, m.promptInjectionEvidence, m.forwardConflicts, m.pendingReviewAge, m.highSeverityMiss)
	return m
}

func (m *ContentModerationMetrics) observePromptInjectionReview(verdict string, complete bool, evidenceRunes int) {
	if m == nil {
		return
	}
	switch verdict {
	case "allow", "review", "reject", "error":
	default:
		verdict = "error"
	}
	completeLabel := "false"
	if complete {
		completeLabel = "true"
	}
	m.promptInjectionReviews.WithLabelValues(verdict, completeLabel).Inc()
	if evidenceRunes < 0 {
		evidenceRunes = 0
	}
	m.promptInjectionEvidence.Observe(float64(evidenceRunes))
}

func (m *ContentModerationMetrics) observePromptInjectionFailClosed(reason string) {
	if m == nil {
		return
	}
	switch reason {
	case "review", "unavailable", "incomplete", "disabled":
	default:
		reason = "other"
	}
	m.promptInjectionFailClosed.WithLabelValues(reason).Inc()
}

func (s *ContentModerationService) RecordModerationReceipt(pipeline, outcome string) {
	if s == nil || s.metrics == nil {
		return
	}
	switch pipeline {
	case "openai_http", "openai_websocket", "gateway_pre_forward", "selected_account":
	default:
		pipeline = "other"
	}
	switch outcome {
	case "no_hit", "allow", "reject", "deferred", "error", "out_of_scope", "deferred_selected_account":
	default:
		outcome = "error"
	}
	s.metrics.receipts.WithLabelValues(pipeline, outcome).Inc()
}

func (s *ContentModerationService) RecordModerationForwardConflict(decision string) {
	if s == nil || s.metrics == nil {
		return
	}
	switch decision {
	case "missing", "deferred", "blocked":
	default:
		decision = "other"
	}
	s.metrics.forwardConflicts.WithLabelValues(decision).Inc()
}

func (m *ContentModerationMetrics) observeSemanticReview(model, result string, started time.Time, usage OpenAIUsage) {
	if m == nil {
		return
	}
	model = boundedSemanticReviewModel(model)
	switch result {
	case "allow", "review", "reject":
	default:
		result = "error"
	}
	m.semanticReview.WithLabelValues(model, result).Inc()
	m.semanticLatency.WithLabelValues(model, result).Observe(time.Since(started).Seconds())
	if usage.InputTokens > 0 {
		m.semanticTokens.WithLabelValues(model, "input").Add(float64(usage.InputTokens))
	}
	if usage.OutputTokens > 0 {
		m.semanticTokens.WithLabelValues(model, "output").Add(float64(usage.OutputTokens))
	}
}

func (m *ContentModerationMetrics) observeSemanticReviewAttempt(model, outcome, reason string) {
	if m == nil {
		return
	}
	model = boundedSemanticReviewModel(model)
	switch outcome {
	case "success", "error", "skipped", "no_account":
	default:
		outcome = "error"
	}
	switch reason {
	case "", "no_account", "quota_exhausted", "model_unsupported", "transport", "timeout", "read_response", "token_unavailable", "retryable_error":
	default:
		reason = "other"
	}
	m.semanticReviewAttempts.WithLabelValues(model, outcome, reason).Inc()
}

func boundedSemanticReviewModel(model string) string {
	switch model {
	case ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewFallbackModel:
		return model
	default:
		return "other"
	}
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
