package service

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationMetricsBoundedLabelsAndIndependentRegistries(t *testing.T) {
	first := NewContentModerationMetrics()
	second := NewContentModerationMetrics()
	first.observeRequest(ContentModerationModeObserve, "incremental", ModerationLevelPass, time.Now().Add(-time.Millisecond), 2)
	first.observePromptInjectionReview("reject", true, 5_100)
	first.observePromptInjectionFailClosed("incomplete")
	service := &ContentModerationService{metrics: first}
	service.RecordModerationReceipt("openai_http", "reject")
	service.RecordModerationForwardConflict("deferred")
	second.observeRequest(ContentModerationModePreBlock, "all_hit", ModerationLevelReject, time.Now().Add(-time.Millisecond), 1)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/metrics", nil)
	first.Handler().ServeHTTP(recorder, request)
	require.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	require.Contains(t, body, `mode="observe"`)
	require.Contains(t, body, `cache_state="incremental"`)
	require.Contains(t, body, `sub2api_moderation_prompt_injection_reviews_total{complete="true",verdict="reject"} 1`)
	require.Contains(t, body, `sub2api_moderation_receipts_total{outcome="reject",pipeline="openai_http"} 1`)
	require.Contains(t, body, `sub2api_moderation_forward_conflicts_total{decision="deferred"} 1`)
	for _, forbidden := range []string{"request_id", "user_id", "model=", "chunk_hmac", "api_key", "https://"} {
		require.False(t, strings.Contains(body, forbidden), body)
	}
}

func TestContentModerationForcedFreshSamplerIsDeterministicAndBounded(t *testing.T) {
	first := shouldForceFreshModeration("request-stable")
	require.Equal(t, first, shouldForceFreshModeration("request-stable"))
	require.False(t, shouldForceFreshModeration(""))
}
