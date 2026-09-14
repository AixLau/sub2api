package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfiguredSemanticReviewRetriesPrimaryBeforeFallback(t *testing.T) {
	var primaryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.Header.Get("x-codex-window-id"))
		require.NotEmpty(t, r.Header.Get("x-codex-installation-id"))
		require.NotEmpty(t, r.Header.Get("originator"))
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model == "primary" && primaryCalls.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"allow\",\"severity\":\"low\",\"confidence\":1,\"operationality\":\"none\",\"categories\":[],\"reason_codes\":[]}"}}]}`))
	}))
	defer server.Close()
	router := &openAIContentModerationSemanticReviewRouter{}
	cfg := defaultContentModerationSemanticReviewConfig()
	cfg.APIBaseURL, cfg.APIKey, cfg.PrimaryModel = server.URL, "test-key", "primary"
	cfg.FallbackModels = []string{"fallback"}
	cfg.MaxAttemptsPerModel = 3
	cfg.TimeoutMS, cfg.PrimaryTimeoutMS, cfg.FallbackTimeoutMS = 5000, 1000, 1000
	result, err := router.reviewWithConfiguredAPI(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, "primary", result.Model)
	require.Equal(t, int32(3), primaryCalls.Load())
}

func TestConfiguredSemanticReviewSwitchesAfterPrimaryRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls.Add(1)
		if body.Model == "primary" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"verdict\":\"allow\",\"severity\":\"low\",\"confidence\":1,\"operationality\":\"none\",\"categories\":[],\"reason_codes\":[]}"}}]}`))
	}))
	defer server.Close()
	router := &openAIContentModerationSemanticReviewRouter{}
	cfg := defaultContentModerationSemanticReviewConfig()
	cfg.APIBaseURL, cfg.APIKey, cfg.PrimaryModel = server.URL, "test-key", "primary"
	cfg.FallbackModels = []string{"fallback"}
	cfg.MaxAttemptsPerModel = 2
	cfg.TimeoutMS, cfg.PrimaryTimeoutMS, cfg.FallbackTimeoutMS = 5000, 1000, 1000
	result, err := router.reviewWithConfiguredAPI(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, "fallback", result.Model)
	require.Equal(t, "primary", result.FallbackFrom)
	require.Equal(t, int32(3), calls.Load())
}

func TestConfiguredResponsesRequestMatchesCompatibleEndpointAndSupportsNoneReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "primary", body["model"])
		require.Equal(t, "none", body["reasoning"].(map[string]any)["effort"])
		require.NotContains(t, body, "text")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"content":[{"type":"output_text","text":"{\"decision\":\"reject\",\"category\":\"license_cracking\",\"reason_code\":\"third_party_license_circumvention\"}"}]}]}`))
	}))
	defer server.Close()
	router := &openAIContentModerationSemanticReviewRouter{}
	cfg := defaultContentModerationSemanticReviewConfig()
	cfg.APIBaseURL, cfg.APIKey, cfg.APIEndpoint, cfg.PrimaryModel = server.URL, "test-key", "responses", "primary"
	cfg.ReasoningEffort = "none"
	cfg.MaxAttemptsPerModel = 1
	cfg.TimeoutMS, cfg.PrimaryTimeoutMS = 5000, 1000
	result, err := router.reviewWithConfiguredAPI(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "test"})
	require.NoError(t, err)
	require.Equal(t, "reject", result.Verdict)
}

func TestSemanticReviewResponseCapturesProviderReasoningSummary(t *testing.T) {
	response, err := parseSemanticReviewResponse([]byte(`{"reasoning":{"summary":[{"type":"summary_text","text":"判断为许可证绕过请求"}]},"output_text":"{\"verdict\":\"reject\"}"}`), "application/json")
	require.NoError(t, err)
	require.Equal(t, "判断为许可证绕过请求", response.ReasoningSummary)
}

func TestSemanticReviewResponseDoesNotTreatJSONDataFieldAsSSE(t *testing.T) {
	response, err := parseSemanticReviewResponse([]byte(`{"output_text":"{\"verdict\":\"allow\",\"reason_code\":\"data: benign_context\"}"}`), "application/json")
	require.NoError(t, err)
	require.Contains(t, response.Text, "data: benign_context")
}
