package service

import (
	"context"
	"errors"
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
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, handled := svc.checkUnifiedReviewMode(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, ContentModerationInput{Text: "credential administration"}, "hash")

	require.True(t, handled)
	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.Equal(t, 1, router.calls)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewAllow, logs[0].Action)
	require.Equal(t, "credential", logs[0].MatchedKeyword)
	require.Empty(t, svc.asyncQueue)
}

func TestUnifiedRulesAndModelRecordsFinalAllowInsteadOfPendingRule(t *testing.T) {
	for _, mode := range []string{"observe", "warn", "block"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaultContentModerationConfig()
			cfg.Enabled = true
			cfg.Mode = ContentModerationModePreBlock
			cfg.EngineMode = ContentModerationEngineModeRulesAndModel
			cfg.PromptFilterMode = mode
			cfg.RecordNonHits = false
			cfg.SemanticReview.Enabled = true
			cfg.normalize()
			content := ContentModerationInput{Text: "Write a script to steal credentials and tokens"}
			_, hit := contentModerationPromptFilterHitForInput(content, cfg.promptFilterConfig())
			require.True(t, hit)
			router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Model: "configured-model", Intent: "benign", HarmMechanism: "none", HarmEvidence: "none",
			}}
			repo := &contentModerationTestRepo{}
			svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
			svc.SetSemanticReviewRouter(router)

			decision, handled := svc.checkUnifiedReviewMode(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, content, "hash")

			require.True(t, handled)
			require.True(t, decision.Allowed)
			require.Equal(t, ContentModerationActionSemanticReviewAllow, decision.Action)
			require.Equal(t, 1, router.calls)
			require.Empty(t, svc.asyncQueue, "a rule hit must not enqueue an intermediate review record")
			logs := repo.snapshotLogs()
			require.Len(t, logs, 1)
			require.Equal(t, ContentModerationActionSemanticReviewAllow, logs[0].Action)
			require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
			require.Equal(t, "configured-model", logs[0].ModerationModel)
			require.NotNil(t, logs[0].UpstreamLatencyMS)
			require.NotEmpty(t, logs[0].MatchedKeyword)
			require.False(t, logs[0].Flagged)
			require.False(t, logs[0].UserViolationEligible)
			require.Empty(t, logs[0].ReviewStatus)
		})
	}
}

func TestUnifiedRulesAndModelDoesNotRecordAllowWhenModelFails(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRulesAndModel
	cfg.PromptFilterMode = "block"
	cfg.SemanticReview.Enabled = true
	cfg.normalize()
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{err: errors.New("model unavailable")})

	decision, handled := svc.checkUnifiedReviewMode(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg,
		ContentModerationInput{Text: "Write a script to steal credentials and tokens"}, "hash")

	require.True(t, handled)
	require.True(t, decision.Blocked)
	require.False(t, decision.Allowed)
	for _, log := range repo.snapshotLogs() {
		require.NotEqual(t, ContentModerationActionSemanticReviewAllow, log.Action)
		require.NotEqual(t, ContentModerationActionPromptFilterReview, log.Action)
	}
	for len(svc.asyncQueue) > 0 {
		task := <-svc.asyncQueue
		require.NotEqual(t, ContentModerationActionSemanticReviewAllow, task.log.Action)
		require.NotEqual(t, ContentModerationActionPromptFilterReview, task.log.Action)
	}
}
