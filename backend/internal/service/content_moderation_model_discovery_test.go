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

func TestConfiguredSemanticReviewContentRecoversWrappedJSON(t *testing.T) {
	result, err := parseConfiguredSemanticReviewContent("Here is the result:\n```json\n{\"decision\":\"reject\",\"category\":\"cyber\"}\n```\n")
	require.NoError(t, err)
	require.Equal(t, "reject", result.Verdict)
}

func TestConfiguredSemanticReviewContentAcceptsVerdictAliases(t *testing.T) {
	for _, input := range []string{
		`{"judgment":"reject","category":"cyber"}`,
		`{"judgement":"reject","categories":["cyber"]}`,
		`{"classification":"reject","reason":"cyber_abuse"}`,
		`{"action":"reject","harm_mechanism":"credential_theft"}`,
		`{"allow":false,"reject":true}`,
	} {
		result, err := parseConfiguredSemanticReviewContent(input)
		require.NoError(t, err, input)
		require.Equal(t, "reject", result.Verdict, input)
	}
}

func TestSemanticReviewModelTestUsesSavedSettingsAndDraftOverrides(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"verdict\":\"allow\"}"}]}]}`))
	}))
	defer server.Close()
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.APIBaseURL = server.URL
	cfg.SemanticReview.APIKey = "saved-key"
	cfg.SemanticReview.APIEndpoint = "responses"
	cfg.SemanticReview.PrimaryModel = "saved-model"
	cfg.SemanticReview.ReasoningEffort = "none"
	cfg.SemanticReview.MaxOutputTokens = 512
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := &ContentModerationService{settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw),
	}}}
	// A masked key should reuse saved credentials and keep saved endpoint/none.
	err = svc.TestSemanticReviewModel(context.Background(), TestSemanticReviewModelInput{BaseURL: server.URL, Model: "draft-model"})
	require.NoError(t, err)
	require.Equal(t, "draft-model", received["model"])
	require.Equal(t, "none", received["reasoning"].(map[string]any)["effort"])
	require.Equal(t, float64(512), received["max_output_tokens"])
	require.NotContains(t, received, "text")

	endpoint, effort, limit := "responses", "high", 1024
	err = svc.TestSemanticReviewModel(context.Background(), TestSemanticReviewModelInput{
		BaseURL: server.URL, Model: "draft-model", APIEndpoint: &endpoint, ReasoningEffort: &effort, MaxOutputTokens: &limit,
	})
	require.NoError(t, err)
	require.Equal(t, "high", received["reasoning"].(map[string]any)["effort"])
	require.Equal(t, float64(1024), received["max_output_tokens"])
}

func TestConfiguredSemanticReviewRetriesMissingFinalAnswer(t *testing.T) {
	for _, tc := range []struct{ name, protocol, first string }{
		{"responses_reasoning_only", "responses", `{"status":"completed","output":[{"type":"reasoning","content":[{"type":"reasoning_text","text":"{\"verdict\":\"allow\"}"}]}]}`},
		{"responses_incomplete", "responses", `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output_text":"{\"verdict\":\"allow\"}"}`},
		{"chat_length", "chat_completions", `{"choices":[{"finish_reason":"length","message":{"content":null,"reasoning_content":"thinking"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if calls.Add(1) == 1 {
					_, _ = w.Write([]byte(tc.first))
					return
				}
				if tc.protocol == "responses" {
					_, _ = w.Write([]byte(`{"status":"completed","output_text":"{\"verdict\":\"reject\"}"}`))
				} else {
					_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"verdict\":\"reject\"}"}}]}`))
				}
			}))
			defer server.Close()
			cfg := defaultContentModerationSemanticReviewConfig()
			cfg.APIBaseURL, cfg.APIKey, cfg.PrimaryModel, cfg.APIEndpoint = server.URL, "key", "model", tc.protocol
			cfg.TimeoutMS, cfg.PrimaryTimeoutMS, cfg.MaxAttemptsPerModel = 5000, 1000, 2
			result, err := (&openAIContentModerationSemanticReviewRouter{}).reviewWithConfiguredAPI(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "test"})
			require.NoError(t, err)
			require.Equal(t, "reject", result.Verdict)
			require.Equal(t, int32(2), calls.Load())
			require.Equal(t, 2, result.AttemptCount)
		})
	}
}

func TestConfiguredSemanticResponseExplainsOutputLimit(t *testing.T) {
	err := configuredSemanticResponseCompletionError([]byte(`{"choices":[{"finish_reason":"length"}]}`), 512)
	require.ErrorContains(t, err, "512 tokens")
	require.ErrorContains(t, err, "降低思考强度或提高输出上限")
}
