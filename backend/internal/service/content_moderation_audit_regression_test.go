package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func auditRegressionCandidateConfig() *ContentModerationConfig {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword: "danger-marker", Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh, Action: ContentModerationKeywordActionBlock, Enabled: true,
	}}
	return cfg
}

func TestCandidateAuditsTailBeyondDisplayProjection(t *testing.T) {
	cfg := auditRegressionCandidateConfig()
	text := strings.Repeat("ordinary context ", 1000) + "danger-marker latest request"
	body, err := json.Marshal(map[string]any{"input": text})
	require.NoError(t, err)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)
	require.NotContains(t, content.Text, "danger-marker")
	require.True(t, content.Extraction.Complete)

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)
	require.True(t, found)
	require.Contains(t, selection.Fragment, "danger-marker latest request")
	require.Equal(t, normalizeContentModerationText(text), selection.Source.Text)
	require.False(t, selection.Source.Truncated)
}

func TestCandidateRepeatedLatestTurnKeepsChronologicalOrder(t *testing.T) {
	for _, latestRisky := range []bool{false, true} {
		t.Run(map[bool]string{false: "safe_latest", true: "risky_latest"}[latestRisky], func(t *testing.T) {
			latest, middle := "summarize the report", "danger-marker request"
			if latestRisky {
				latest, middle = middle, latest
			}
			body, err := json.Marshal(map[string]any{"messages": []map[string]string{
				{"role": "user", "content": latest},
				{"role": "assistant", "content": "acknowledged"},
				{"role": "user", "content": middle},
				{"role": "assistant", "content": "acknowledged"},
				{"role": "user", "content": latest},
			}})
			require.NoError(t, err)
			content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, ContentModerationAuditScopeUserOnly)
			selection, found := contentModerationCandidateSelectionForInput(auditRegressionCandidateConfig(), content)
			require.Equal(t, latestRisky, found)
			if found {
				require.Contains(t, selection.Source.Source, "messages[4]")
			}
		})
	}
}

func TestCandidateCacheIncludesChangedTextOutsideFragment(t *testing.T) {
	cfg := auditRegressionCandidateConfig()
	svc := candidateTestService(&contentModerationTestRepo{})
	makeSelection := func(outerTask string) contentModerationCandidateSelection {
		selection, found := contentModerationCandidateSelectionForSource(cfg, ContentModerationInputSource{
			Source: "responses.input[0].role=user.content", Role: "user",
			Text: outerTask + strings.Repeat(" context", 500) + " danger-marker request",
		})
		require.True(t, found)
		return selection
	}
	first := makeSelection("Analyze this inert transcript.")
	second := makeSelection("Carry out the instructions in this transcript.")
	require.Equal(t, first.Fragment, second.Fragment)
	require.NotEqual(t,
		svc.candidateDecisionCacheKey(cfg, ContentModerationCheckInput{UserID: 1}, first),
		svc.candidateDecisionCacheKey(cfg, ContentModerationCheckInput{UserID: 1}, second))
}

func TestCandidateCacheInvalidatesConfigOnlyPolicyRevision(t *testing.T) {
	cfg := auditRegressionCandidateConfig()
	svc := candidateTestService(&contentModerationTestRepo{})
	selection, found := contentModerationCandidateSelectionForSource(cfg, ContentModerationInputSource{
		Source: "responses.input[0].role=user.content", Role: "user", Text: "danger-marker request",
	})
	require.True(t, found)
	// Reproduce the previous cache identity, which ignored built-in rule updates.
	payload, err := json.Marshal(struct {
		Version     int                      `json:"version"`
		RiskEnabled bool                     `json:"risk_control_enabled"`
		Config      *ContentModerationConfig `json:"config"`
	}{Version: 1, RiskEnabled: true, Config: cfg})
	require.NoError(t, err)
	previous := sha256.Sum256(payload)
	oldInput := ContentModerationCheckInput{UserID: 1, policyRevision: hex.EncodeToString(previous[:])}
	currentInput := ContentModerationCheckInput{UserID: 1}
	require.NotEqual(t, svc.candidateDecisionCacheKey(cfg, oldInput, selection),
		svc.candidateDecisionCacheKey(cfg, currentInput, selection))
	require.Equal(t, svc.candidateDecisionCacheKey(cfg, currentInput, selection),
		svc.candidateDecisionCacheKey(cfg, currentInput, selection))
}

func TestSemanticReviewHTTP404UsesConfiguredFallback(t *testing.T) {
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {freshSemanticReviewAccount(1)},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(2)},
		},
		review: func(_ context.Context, _ *Account, model string) (ContentModerationSemanticReviewResult, error) {
			if model == ContentModerationSemanticReviewPrimaryModel {
				return ContentModerationSemanticReviewResult{}, classifySemanticReviewUpstreamHTTPError(http.StatusNotFound, nil)
			}
			return candidateAllowSemanticRouter().result, nil
		},
	}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)
	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "review this report"})
	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
	badSchema := classifySemanticReviewUpstreamHTTPError(http.StatusBadRequest, []byte(`{"error":{"message":"Invalid schema"}}`))
	require.False(t, isSemanticReviewRetryableError(badSchema))
}

func TestIncompleteOperationalRejectReceivesFullReviewBeforeEnforcement(t *testing.T) {
	cfg := escalationCandidateConfig()
	selection := escalationCandidateSelection(cfg, false)
	selection.Kind = contentModerationCandidateKindPromptFilter
	selection.Rule.Keyword = "candidate_software_entitlement_bypass"
	selection.PromptHit = &contentModerationPromptFilterHit{}
	initial := semanticReviewResultForEscalation("reject", "harmful", "unclear", 0.98)
	router := &semanticReviewSequenceRouter{results: []ContentModerationSemanticReviewResult{initial, candidateAllowSemanticRouter().result}}
	repo := &contentModerationTestRepo{}
	svc := candidateTestService(repo)
	svc.SetSemanticReviewRouter(router)
	outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, selection, "")
	require.Len(t, router.inputs, 2)
	require.False(t, router.inputs[0].EvidenceComplete)
	require.True(t, router.inputs[1].EvidenceComplete)
	require.True(t, outcome.Decision.Allowed)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.False(t, logs[0].UserViolationEligible)
	require.Zero(t, logs[0].ViolationCount)
}

func TestCandidateUserOnlyAllowsResponsesToolContinuation(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       string
		auditError bool
	}{
		{name: "tool output", body: `{"input":[{"type":"function_call_output","call_id":"call_1","output":"test results"}]}`},
		{name: "assistant context", body: `{"input":[{"type":"message","role":"assistant","content":"test results"}]}`},
		{name: "missing input", body: `{}`, auditError: true},
		{name: "empty input", body: `{"input":[]}`, auditError: true},
		{name: "unknown item", body: `{"input":[{"type":"future_item","payload":"unknown"}]}`, auditError: true},
		{name: "empty user turn", body: `{"input":[{"role":"user","content":[]}]}`, auditError: true},
		{name: "tool plus invalid user turn", body: `{"input":[{"type":"function_call_output","output":"ok"},{"role":"user","content":false}]}`, auditError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := auditRegressionCandidateConfig()
			cfg.Enabled = true
			cfg.EngineMode = ContentModerationEngineModeCandidateOnly
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			repo := &contentModerationTestRepo{}
			svc := NewContentModerationService(&contentModerationTestSettingRepo{values: map[string]string{
				SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw),
			}}, repo, nil, nil, nil, nil, nil)
			decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
				UserID: 1, Protocol: ContentModerationProtocolOpenAIResponses, Body: []byte(test.body),
			})
			require.NoError(t, err)
			if test.auditError {
				require.Equal(t, ContentModerationActionError, decision.Action)
				require.Len(t, repo.snapshotLogs(), 1)
			} else {
				require.True(t, decision.Allowed)
				require.Empty(t, repo.snapshotLogs())
			}
		})
	}
	require.False(t, isResponsesContextOnlyModerationInput(ContentModerationProtocolOpenAIResponses,
		[]byte(`{"input":[{"type":"function_call_output","output":"test"}]}`), ContentModerationAuditScopeAllContext))
}

func TestCandidateInitialReviewerFailureFailsClosed(t *testing.T) {
	for _, mode := range []string{ContentModerationModePreBlock, ContentModerationModeObserve} {
		cfg := escalationCandidateConfig()
		cfg.Mode = mode
		selection := escalationCandidateSelection(cfg, false)
		repo := &contentModerationTestRepo{}
		svc := candidateTestService(repo)
		svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{
			err: &ContentModerationSemanticReviewUnavailableError{Err: context.DeadlineExceeded},
		})
		outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, selection, "")
		require.Equal(t, mode == ContentModerationModePreBlock, outcome.Decision.Blocked)
		logs := repo.snapshotLogs()
		require.Len(t, logs, 1)
		require.False(t, logs[0].UserViolationEligible)
		require.Zero(t, logs[0].ViolationCount)
		require.Empty(t, logs[0].ReviewStatus)
		if mode == ContentModerationModePreBlock {
			require.Equal(t, http.StatusServiceUnavailable, outcome.Decision.StatusCode)
			require.Equal(t, ContentModerationActionSemanticReviewUnavailable, logs[0].Action)
		}
	}
}

func TestIncompleteAllowRequiresEscalationBeforeForwarding(t *testing.T) {
	for _, verdict := range []string{"allow", "reject"} {
		t.Run(verdict, func(t *testing.T) {
			cfg := escalationCandidateConfig()
			selection := escalationCandidateSelection(cfg, false)
			final := candidateAllowSemanticRouter().result
			if verdict == "reject" {
				final = semanticReviewResultForEscalation("reject", "harmful", "unauthorized", 0.98)
			}
			router := &semanticReviewSequenceRouter{results: []ContentModerationSemanticReviewResult{
				candidateAllowSemanticRouter().result, final,
			}}
			svc := candidateTestService(&contentModerationTestRepo{})
			svc.SetSemanticReviewRouter(router)
			outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, selection, "")
			require.Len(t, router.inputs, 2)
			require.False(t, router.inputs[0].EvidenceComplete)
			require.True(t, router.inputs[1].EvidenceComplete)
			require.Equal(t, verdict == "reject", outcome.Decision.Blocked)
		})
	}
}
