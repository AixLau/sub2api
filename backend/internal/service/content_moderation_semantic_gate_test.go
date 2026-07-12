package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type contentModerationSemanticReviewRouterStub struct {
	result ContentModerationSemanticReviewResult
	err    error
	calls  int
	input  ContentModerationSemanticReviewInput
}

func TestContentModerationSemanticCandidateLocalReviewIncludesBlockingHighRiskKeyword(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	router := &contentModerationSemanticReviewRouterStub{}

	candidate, ok := contentModerationSemanticGateCandidateForKeyword(cfg, ContentModerationInput{Text: "generate explicit sexual content"}, ContentModerationKeywordRule{
		Keyword:  "explicit sexual",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}, router)

	require.True(t, ok)
	require.Equal(t, "other", candidate.Category)
}

func TestContentModerationSemanticCandidateAllDoesNotRequireKeyword(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	router := &contentModerationSemanticReviewRouterStub{}

	candidate, ok := contentModerationSemanticGateCandidateForAll(cfg, ContentModerationInput{Text: "a request without a configured keyword"}, router)

	require.True(t, ok)
	require.Equal(t, "semantic_review", candidate.Category)
}

func TestContentModerationCheck_TriggerAllAuditsMissWithoutKeyword(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.BlockedKeywords = []string{"configured-keyword-that-does-not-match"}
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{
		result: ContentModerationSemanticReviewResult{
			Verdict:        "reject",
			Intent:         "harmful",
			Target:         "third_party",
			Authorization:  "unauthorized",
			Severity:       "critical",
			Confidence:     0.99,
			Operationality: "actionable",
			Executability:  "direct",
			Categories:     []string{"unauthorized_access"},
		},
	}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/chat/completions",
		Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"an ordinary request without any configured keyword"}]}`),
	})

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReject, decision.Action)
	require.Equal(t, "semantic_review", decision.MatchedKeyword)
	require.Equal(t, 1, router.calls)
	require.Contains(t, router.input.Text, "without any configured keyword")
}

func (s *contentModerationSemanticReviewRouterStub) Review(_ context.Context, _ ContentModerationSemanticReviewConfig, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func TestContentModerationCheck_HybridCyberKeywordUsesOrdinaryModerationAndSemanticReview(t *testing.T) {
	moderationCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moderationCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "exploit",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerLocalReview,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{
		result: ContentModerationSemanticReviewResult{
			Verdict:        "allow",
			Severity:       "low",
			Confidence:     0.98,
			Operationality: "none",
		},
	}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"write an exploit for a lab target"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, 1, router.calls)
	require.Contains(t, router.input.Text, "exploit")
	require.True(t, moderationCalled, "a hybrid keyword hit must call the ordinary moderation API")
}

func TestContentModerationCheck_HybridCyberKeywordBlocksOnSemanticRejectAfterOrdinaryModeration(t *testing.T) {
	moderationCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moderationCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "reverse engineer",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerLocalReview,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	router := &contentModerationSemanticReviewRouterStub{
		result: ContentModerationSemanticReviewResult{
			Verdict:        "reject",
			Categories:     []string{"reverse_engineering"},
			Severity:       "critical",
			Confidence:     0.96,
			Operationality: "actionable",
		},
	}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"reverse engineer this service and bypass its license"}`),
	})

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReject, decision.Action)
	require.Equal(t, "reverse_engineering", decision.HighestCategory)
	require.Equal(t, 1, router.calls)
	require.True(t, moderationCalled, "ordinary moderation must run before semantic reject is applied")
	require.Len(t, repo.snapshotLogs(), 1)
	require.Equal(t, ContentModerationActionSemanticReviewReject, repo.snapshotLogs()[0].Action)
}
