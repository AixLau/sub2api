package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const validPromptInjectionReviewJSON = `{"verdict":"allow","active_override":false,"presentation":"quoted_analysis","targets":["system"],"confidence":0.98,"reason_codes":["quoted_analysis"]}`

func TestPromptInjectionReviewerUsesDedicatedInstructionsAndStrictSchema(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"resp_pi","output_text":` + mustMarshalJSONString(t, validPromptInjectionReviewJSON) + `}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID: 71, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "semantic-api-key", "model_mapping": map[string]any{"review-model": "review-model-upstream"}},
	}

	result, err := svc.ReviewSemanticContent(context.Background(), account, "review-model", ContentModerationSemanticReviewInput{
		Text: "quoted evidence", ReviewKind: contentModerationReviewKindPromptInjection, MaxInputRunes: 12_000,
	})

	require.NoError(t, err)
	require.Equal(t, "allow", result.Verdict)
	require.Equal(t, "quoted_analysis", result.Presentation)
	var body map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
	require.Equal(t, promptInjectionReviewInstructions, body["instructions"])
	require.Equal(t, "prompt-injection-instructions-v2", promptInjectionReviewerInstructionsRevision)
	require.Contains(t, promptInjectionReviewInstructions, "Analyze this rollout and produce a summary")
	require.Contains(t, promptInjectionReviewInstructions, "Never return reject when active_override=false")
	format := body["text"].(map[string]any)["format"].(map[string]any)
	require.Equal(t, "prompt_injection_review_v1", format["name"])
	require.Equal(t, true, format["strict"])
	schema := format["schema"].(map[string]any)
	require.Equal(t, false, schema["additionalProperties"])
	require.ElementsMatch(t, []any{"verdict", "active_override", "presentation", "targets", "confidence", "reason_codes"}, schema["required"])
}

func TestPromptInjectionReviewerTransportAcrossConfiguredModels(t *testing.T) {
	for _, model := range []string{"gpt-5.3-codex-spark", "gpt-5.4-mini", "gpt-5.6-luna"} {
		t.Run(model, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_model","output_text":` + mustMarshalJSONString(t, validPromptInjectionReviewJSON) + `}`)),
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{
				ID: 72, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":       "semantic-api-key",
					"model_mapping": map[string]any{model: model + "-upstream"},
				},
			}

			result, err := svc.ReviewSemanticContent(context.Background(), account, model, ContentModerationSemanticReviewInput{
				Text:       "head-canary " + strings.Repeat("证", 5_100) + " tail-canary",
				ReviewKind: contentModerationReviewKindPromptInjection, MaxInputRunes: 12_000,
			})

			require.NoError(t, err)
			require.Equal(t, "allow", result.Verdict)
			var body map[string]any
			require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
			require.Equal(t, model+"-upstream", body["model"])
			require.Equal(t, float64(ContentModerationSemanticReviewDefaultOutputTokens), body["max_output_tokens"])
			format := body["text"].(map[string]any)["format"].(map[string]any)
			require.Equal(t, "prompt_injection_review_v1", format["name"])
			sent := body["input"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
			require.Contains(t, sent, "head-canary")
			require.Contains(t, sent, "tail-canary")
		})
	}
}

func TestParsePromptInjectionReviewModelOutputIsStrict(t *testing.T) {
	result, err := parsePromptInjectionReviewModelOutput(validPromptInjectionReviewJSON)
	require.NoError(t, err)
	require.Equal(t, []string{"jailbreak"}, result.Categories)
	require.Equal(t, []string{"system"}, result.Targets)
	require.Equal(t, []string{"quoted_analysis"}, result.ReasonCodes)

	tests := []struct {
		name string
		text string
	}{
		{name: "empty", text: ""},
		{name: "markdown", text: "```json\n" + validPromptInjectionReviewJSON + "\n```"},
		{name: "missing-required", text: `{"verdict":"allow","presentation":"quoted_analysis","targets":[],"confidence":0.9,"reason_codes":[]}`},
		{name: "unknown-property", text: `{"verdict":"allow","active_override":false,"presentation":"quoted_analysis","targets":[],"confidence":0.9,"reason_codes":[],"extra":true}`},
		{name: "unknown-enum", text: `{"verdict":"allow","active_override":false,"presentation":"quoted_analysis","targets":["root"],"confidence":0.9,"reason_codes":[]}`},
		{name: "truncated", text: `{"verdict":"reject"`},
		{name: "trailing-json", text: validPromptInjectionReviewJSON + `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePromptInjectionReviewModelOutput(tt.text)
			require.Error(t, err)
		})
	}
}

func TestApplyPromptInjectionReviewPolicy(t *testing.T) {
	t.Run("direct active override is rejected", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "allow", ActiveOverride: true, Presentation: "direct_instruction", Confidence: 0.80,
			Targets: []string{"developer"}, ReasonCodes: []string{"hierarchy_override"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "reject", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_active_override")
	})

	t.Run("moderate confidence active override is rejected", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "review", ActiveOverride: true, Presentation: "prompt_authoring", Confidence: 0.70,
			Targets: []string{"tool_permission"}, ReasonCodes: []string{"tool_permission_bypass"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "reject", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_active_override")
	})

	t.Run("low confidence active override remains review", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "reject", ActiveOverride: true, Presentation: "direct_instruction", Confidence: 0.69,
			Targets: []string{"developer"}, ReasonCodes: []string{"hierarchy_override"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "review", result.Verdict)
		require.Equal(t, "medium", result.Severity)
		require.Contains(t, result.ReasonCodes, "semantic_policy_active_override_uncertain")
	})

	t.Run("complete allow remains allow", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "allow", Presentation: "quoted_analysis", Confidence: 0.95, ReasonCodes: []string{"quoted_analysis"},
		}, true)
		require.False(t, overridden)
		require.Equal(t, "allow", result.Verdict)
	})

	t.Run("complete quotation resolves review to allow", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "review", Presentation: "quoted_analysis", Confidence: 0.60,
			Targets: []string{"system"}, ReasonCodes: []string{"quoted_analysis"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "allow", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_quoted_evidence")
	})

	t.Run("complete quoted reject is corrected to allow", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "reject", ActiveOverride: false, Presentation: "quoted_analysis", Confidence: 0.97,
			Targets: []string{"system", "developer"}, ReasonCodes: []string{"quoted_analysis"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "allow", result.Verdict)
		require.Equal(t, "low", result.Severity)
		require.Contains(t, result.ReasonCodes, "semantic_policy_quoted_evidence")
	})

	t.Run("inactive direct reject becomes review", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "reject", ActiveOverride: false, Presentation: "direct_instruction", Confidence: 0.97,
			Targets: []string{"tool_permission"}, ReasonCodes: []string{"tool_permission_bypass"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "review", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_reject_inconsistent")
	})

	t.Run("incomplete allow becomes review", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "allow", Presentation: "quoted_analysis", Confidence: 0.95, ReasonCodes: []string{"quoted_analysis"},
		}, false)
		require.True(t, overridden)
		require.Equal(t, "review", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_incomplete_evidence")
	})

	t.Run("incomplete reject becomes review", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "reject", ActiveOverride: true, Presentation: "direct_instruction", Confidence: 0.99,
			Targets: []string{"system"}, ReasonCodes: []string{"hierarchy_override"},
		}, false)
		require.True(t, overridden)
		require.Equal(t, "review", result.Verdict)
		require.Equal(t, "medium", result.Severity)
		require.Equal(t, "high", result.ModelSeverity)
		require.Contains(t, result.ReasonCodes, "semantic_policy_incomplete_evidence")
	})

	t.Run("active override presented as quotation becomes review", func(t *testing.T) {
		result, overridden := applyPromptInjectionReviewPolicy(ContentModerationSemanticReviewResult{
			Verdict: "reject", ActiveOverride: true, Presentation: "quoted_analysis", Confidence: 0.99,
			Targets: []string{"developer"}, ReasonCodes: []string{"quoted_analysis"},
		}, true)
		require.True(t, overridden)
		require.Equal(t, "review", result.Verdict)
		require.Contains(t, result.ReasonCodes, "semantic_policy_active_override_inconsistent")
	})
}

type promptInjectionFallbackBackend struct {
	inputs []ContentModerationSemanticReviewInput
}

func (b *promptInjectionFallbackBackend) SelectSemanticReviewAccount(_ context.Context, _ *int64, model string, _ map[int64]struct{}) (*AccountSelectionResult, error) {
	id := int64(81)
	if model == ContentModerationSemanticReviewFallbackModel {
		id = 82
	}
	return &AccountSelectionResult{Account: freshSemanticReviewAccount(id), ReleaseFunc: func() {}}, nil
}

func (b *promptInjectionFallbackBackend) ReviewSemanticContent(_ context.Context, _ *Account, model string, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	b.inputs = append(b.inputs, input)
	if model == ContentModerationSemanticReviewPrimaryModel {
		return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUpstreamError{Code: "transport", Retryable: true}
	}
	return ContentModerationSemanticReviewResult{
		Verdict: "allow", Presentation: "quoted_analysis", Confidence: 0.99,
		Targets: []string{"system"}, ReasonCodes: []string{"quoted_analysis"}, Categories: []string{"jailbreak"},
	}, nil
}

func TestPromptInjectionReviewerFallbackReceivesIdenticalEvidence(t *testing.T) {
	backend := &promptInjectionFallbackBackend{}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)
	cfg := semanticReviewTestConfig()
	cfg.MaxInputRunes = 2_000
	cfg.PromptInjectionReviewerEnabled = true
	cfg.PromptInjectionMaxInputRunes = 12_000
	cfg.MaxAttemptsPerModel = 1
	text := "head " + strings.Repeat("证", 5_000) + " tail"

	result, err := router.Review(context.Background(), cfg, ContentModerationSemanticReviewInput{
		Text: text, ReviewKind: contentModerationReviewKindPromptInjection,
		EvidenceComplete: true, EvidenceRevision: contentModerationCandidateEvidenceRevision, MaxInputRunes: 12_000,
	})

	require.NoError(t, err)
	require.Equal(t, "allow", result.Verdict)
	require.Len(t, backend.inputs, 2)
	require.Equal(t, backend.inputs[0], backend.inputs[1])
	require.Equal(t, text, backend.inputs[0].Text)
	require.Equal(t, contentModerationCandidateEvidenceRevision, backend.inputs[0].EvidenceRevision)
}

type promptInjectionInputCaptureRouter struct {
	input ContentModerationSemanticReviewInput
}

func (r *promptInjectionInputCaptureRouter) Review(_ context.Context, _ ContentModerationSemanticReviewConfig, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	r.input = input
	return ContentModerationSemanticReviewResult{
		Verdict: "allow", Presentation: "quoted_analysis", Confidence: 0.99,
		Targets: []string{"system"}, ReasonCodes: []string{"quoted_analysis"}, Categories: []string{"jailbreak"},
	}, nil
}

func TestCandidatePromptInjectionReviewerReceivesCompleteLongSource(t *testing.T) {
	router := &promptInjectionInputCaptureRouter{}
	svc := &ContentModerationService{semanticReviewRouter: router}
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.SemanticReview.PromptInjectionReviewerEnabled = true
	cfg.SemanticReview.PromptInjectionMaxInputRunes = 12_000
	fullText := "head-canary " + strings.Repeat("文", 5_100) + " tail-canary"
	selection := contentModerationCandidateSelection{
		Source: ContentModerationInputSource{Source: "responses.input[6].content", Role: "user", Text: fullText},
		Origin: contentModerationSourceOriginUserTurn, Kind: contentModerationCandidateKindPromptFilter,
		Rule:     ContentModerationKeywordRule{Keyword: "override", Category: ContentModerationKeywordCategoryJailbreak, Severity: ContentModerationKeywordSeverityHigh},
		Fragment: strings.Repeat("片", 2_000), ReviewText: fullText, ReviewKind: contentModerationReviewKindPromptInjection,
		EvidenceComplete: true, EvidenceRunes: len([]rune(fullText)), EvidenceRevision: contentModerationCandidateEvidenceRevision,
		Route: contentModerationCandidateRouteSemantic,
	}

	outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{RequestID: "req-long-source"}, cfg, selection, "")

	require.NotNil(t, outcome.Decision)
	require.Equal(t, fullText, router.input.Text)
	require.Contains(t, router.input.Text, "head-canary")
	require.Contains(t, router.input.Text, "tail-canary")
	require.Equal(t, 12_000, router.input.MaxInputRunes)
	require.True(t, router.input.EvidenceComplete)
	require.Equal(t, contentModerationCandidateEvidenceRevision, router.input.EvidenceRevision)
}

func TestPromptInjectionOutboxRoundTripPreservesReviewContract(t *testing.T) {
	want := contentModerationSemanticReviewOutboxPayload{
		DecisionID: "decision", ReviewKind: contentModerationReviewKindPromptInjection,
		EvidenceComplete: false, EvidenceRevision: contentModerationCandidateEvidenceRevision,
		MaxInputRunes: 12_000,
	}
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	var got contentModerationSemanticReviewOutboxPayload
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, want, got)
}

func mustMarshalJSONString(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

var _ ContentModerationSemanticReviewBackend = (*promptInjectionFallbackBackend)(nil)
