package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeModerationEngineModeMigratesLegacyValues(t *testing.T) {
	cases := map[string]string{
		"rule_only":       ContentModerationEngineModeRulesOnly,
		"api_only":        ContentModerationEngineModeModelOnly,
		"hybrid":          ContentModerationEngineModeRulesAndModel,
		"candidate_only":  ContentModerationEngineModeRulesAndModel,
		"rules_only":      ContentModerationEngineModeRulesOnly,
		"model_only":      ContentModerationEngineModeModelOnly,
		"rules_and_model": ContentModerationEngineModeRulesAndModel,
	}
	for input, want := range cases {
		if got := normalizeModerationEngineMode(input); got != want {
			t.Fatalf("normalizeModerationEngineMode(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUnifiedModelModeReviewsWithoutLocalCandidate(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeModelOnly
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "allow", Intent: "benign", Target: "unknown", Authorization: "unknown",
		InformationAccess: "unknown", HarmMechanism: "none", HarmEvidence: "none",
		Operationality: "none", Executability: "none", Severity: "low", Confidence: 1,
	}}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)
	content := ContentModerationInput{Text: "这是一个没有任何本地规则命中的普通请求"}

	decision, handled := svc.checkUnifiedReviewMode(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, content, "hash")

	require.True(t, handled)
	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.Equal(t, 1, router.calls)
	require.Contains(t, router.input.Text, "普通请求")
}

func TestUnifiedRulesAndModelDoesNotBlockKeywordBeforeModel(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRulesAndModel
	cfg.SemanticReview.Enabled = true
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword: "credential", Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh, Action: ContentModerationKeywordActionBlock, Enabled: true,
	}}
	cfg.normalize()
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "allow", Intent: "benign", Target: "unknown", Authorization: "unknown",
		InformationAccess: "unknown", HarmMechanism: "none", HarmEvidence: "none",
		Operationality: "none", Executability: "none", Severity: "low", Confidence: 1,
	}}
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, handled := svc.checkUnifiedReviewMode(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, ContentModerationInput{Text: "credential administration"}, "hash")

	require.True(t, handled)
	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.Equal(t, 1, router.calls)
}
