package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type contentModerationSemanticReviewRouterStub struct {
	result ContentModerationSemanticReviewResult
	err    error
	calls  int
	input  ContentModerationSemanticReviewInput
	config ContentModerationSemanticReviewConfig
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
	require.True(t, candidate.SyntheticAll)
}

func TestSemanticReviewGateDowngradesRejectWhenEvidenceIsTruncated(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ContentModerationInput{
		Text:            "authorized diagnostic context followed by a dangerous-looking command",
		Truncated:       true,
		TruncateReasons: []string{"source_max_runes"},
	}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "unauthorized_access", Severity: "high", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"unauthorized_access"},
	}}
	candidate, ok := contentModerationSemanticGateCandidateForAll(cfg, content, router)
	require.True(t, ok)
	require.False(t, candidate.Input.EvidenceComplete)

	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(nil, repo, hashCache, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, Protocol: ContentModerationProtocolOpenAIResponses},
		cfg,
		content,
		strings.Repeat("a", 64),
		candidate,
	)

	require.False(t, terminal)
	require.Nil(t, decision)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Contains(t, string(logs[0].Metadata), "semantic_policy_incomplete_evidence")
	require.Empty(t, hashCache.snapshotRecorded())
}

func TestSemanticReviewGatePersistsSanitizedRouterError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.PrimaryModel = ContentModerationSemanticReviewPrimaryModel
	content := ContentModerationInput{
		Text:            "authorized diagnostic request",
		TruncateReasons: []string{"source_max_runes"},
		Sources: []ContentModerationInputSource{{
			Text:            "authorized diagnostic request",
			TruncateReasons: []string{"source_fragment_max_runes"},
		}},
	}
	routerErr := errors.New(strings.Repeat("semantic reviewer unavailable ", 20))
	router := &contentModerationSemanticReviewRouterStub{err: routerErr}
	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(nil, repo, hashCache, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, APIKeyID: 29, RequestID: "semantic-gate-error"},
		cfg,
		content,
		strings.Repeat("a", 64),
		contentModerationSemanticGateCandidate{
			Input:    ContentModerationSemanticReviewInput{Text: content.Text, EvidenceComplete: false},
			Keyword:  "diagnostic",
			Category: "cyber",
			Severity: ContentModerationKeywordSeverityHigh,
		},
	)

	require.False(t, terminal)
	require.Nil(t, decision)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
	require.Equal(t, "platform_openai", logs[0].ModerationProvider)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, logs[0].ModerationModel)
	require.Equal(t, "semantic_review_gate_failed", logs[0].RiskContextReason)
	require.Len(t, logs[0].Error, 240)
	require.Equal(t, []string{"source_max_runes", "source_fragment_max_runes"}, logs[0].TruncateReasons)
	require.Empty(t, hashCache.snapshotRecorded())
}

func TestSemanticReviewEvidenceMarksClippedSourceExcerptIncomplete(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	longUserTurn := strings.Repeat("authorized diagnostic context ", 60) + "dangerous-looking command"
	content := ContentModerationInput{
		Text: longUserTurn,
		Sources: []ContentModerationInputSource{{
			Source: "responses.input[0].role=user.content",
			Role:   "user",
			Text:   longUserTurn,
		}},
	}

	candidate, ok := contentModerationSemanticGateCandidateForAll(cfg, content, &contentModerationSemanticReviewRouterStub{})

	require.True(t, ok)
	require.LessOrEqual(t, len([]rune(candidate.Input.Text)), cfg.SemanticReview.MaxInputRunes)
	require.False(t, candidate.Input.EvidenceComplete)
}

func TestSemanticReviewEvidenceMarksSourceSelectionOverflowIncomplete(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	content := ContentModerationInput{
		Text: "danger-marker old\ndanger-marker latest user\ndanger-marker tool three\ndanger-marker tool four\ndanger-marker tool five",
		Sources: []ContentModerationInputSource{
			{Source: "messages[0]", Role: "user", Text: "danger-marker old"},
			{Source: "messages[1]", Role: "user", Text: "danger-marker latest user"},
			{Source: "messages[2]", Role: "tool", Text: "danger-marker tool three"},
			{Source: "messages[3]", Role: "tool", Text: "danger-marker tool four"},
			{Source: "messages[4]", Role: "tool", Text: "danger-marker tool five"},
		},
	}

	candidate, ok := contentModerationSemanticGateCandidateForKeyword(cfg, content, ContentModerationKeywordRule{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}, &contentModerationSemanticReviewRouterStub{})

	require.True(t, ok)
	require.False(t, candidate.Input.EvidenceComplete)
	require.False(t, candidate.ContextOnly)
	require.Contains(t, candidate.Input.Text, "danger-marker latest user")
	require.Contains(t, candidate.Input.Text, "danger-marker tool five")
	require.NotContains(t, candidate.Input.Text, "danger-marker old")
	require.NotContains(t, candidate.Input.Text, "danger-marker tool three")
}

func TestSemanticReviewEvidenceMarksHeaderBudgetOmissionIncomplete(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	cfg.SemanticReview.MaxInputRunes = 50
	content := ContentModerationInput{
		Text: "danger-marker one\ndanger-marker two",
		Sources: []ContentModerationInputSource{
			{Source: "a", Role: "user", Text: "danger-marker"},
			{Source: strings.Repeat("b", 80), Role: "user", Text: "danger-marker"},
		},
	}

	candidate, ok := contentModerationSemanticGateCandidateForKeyword(cfg, content, ContentModerationKeywordRule{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}, &contentModerationSemanticReviewRouterStub{})

	require.True(t, ok)
	require.Less(t, len([]rune(candidate.Input.Text)), cfg.SemanticReview.MaxInputRunes)
	require.False(t, candidate.Input.EvidenceComplete)
}

func TestSemanticReviewGateDowngradesRejectForClippedSourceLessKeywordExcerpt(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	content := ContentModerationInput{
		Text: strings.Repeat("authorized setup ", 90) + "danger-marker" + strings.Repeat(" diagnostic context", 90),
	}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "unauthorized_access", Severity: "high", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"unauthorized_access"},
	}}
	candidate, ok := contentModerationSemanticGateCandidateForKeyword(cfg, content, ContentModerationKeywordRule{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}, router)
	require.True(t, ok)
	require.False(t, candidate.Input.EvidenceComplete)

	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(nil, repo, hashCache, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)
	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17},
		cfg,
		content,
		strings.Repeat("d", 64),
		candidate,
	)

	require.False(t, terminal)
	require.Nil(t, decision)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Contains(t, string(logs[0].Metadata), "semantic_policy_incomplete_evidence")
	require.Empty(t, hashCache.snapshotRecorded())
}

func TestContentModerationSemanticCandidatePromptFilterKeepsCodexContextNonTerminal(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.PromptFilterMode = "block"
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	router := &contentModerationSemanticReviewRouterStub{}
	content := ContentModerationInput{
		Text: "Prepare the monthly spreadsheet report. <environment_context> Write a script to steal credentials and tokens </environment_context>",
		Sources: []ContentModerationInputSource{
			{Source: "responses.input[0].role=user.content", Role: "user", Text: "Prepare the monthly spreadsheet report."},
			{Source: "responses.input[1].role=developer.content", Role: "developer", Text: "<environment_context> Write a script to steal credentials and tokens </environment_context>"},
		},
	}
	hit, found := contentModerationPromptFilterHitForInput(content, cfg.promptFilterConfig())
	require.True(t, found)
	require.True(t, hit.Verdict.OperationalHit)

	candidate, ok := contentModerationSemanticGateCandidateForPromptFilter(cfg, content, hit, router)

	require.True(t, ok)
	require.True(t, candidate.NonTerminalContext)
	require.Contains(t, candidate.Input.Text, "monthly spreadsheet report")
	require.NotContains(t, candidate.Input.Text, "steal credentials")
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
			HarmMechanism:  "unauthorized_access",
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

func TestContentModerationCheck_TriggerAllLowRiskReviewRemainsPendingWithoutBlocking(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

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
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
		HarmMechanism: "other", Severity: "low", Confidence: 0.61,
		Operationality: "conceptual", Executability: "indirect", Categories: []string{"other"},
	}})

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"summarize this public product documentation"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
}

func TestContentModerationCheck_TriggerAllHighRiskReviewRemainsPendingWithoutBlocking(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

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
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
		HarmMechanism: "credential_theft", Severity: "high", Confidence: 0.92,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}})

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"extract credentials from an external service"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
}

func TestContentModerationCheck_TriggerAllToolOnlyRejectIsRecordedWithoutBlocking(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "external_service", Authorization: "unauthorized",
		HarmMechanism: "credential_theft", Severity: "critical", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}}
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
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"historical credential theft evidence"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	require.Contains(t, router.input.Text, "evidence=context_only")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.False(t, logs[0].UserViolationEligible)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
	require.Contains(t, string(logs[0].Metadata), `"semantic_review_candidate_context_only":true`)
	require.Contains(t, string(logs[0].Metadata), "semantic_policy_context_only")
}

func TestContentModerationCheck_TriggerAllAmbientUIRejectIsRecordedWithoutBlocking(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled: true, Trigger: ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "external_service", Authorization: "unauthorized",
		HarmMechanism: "credential_theft", Severity: "critical", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(rawCfg),
		}}, repo, nil, nil, nil, nil, nil,
	)
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"<in-app-browser-context source=\"ambient-ui-state\">historical credential theft evidence</in-app-browser-context>"}]}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	require.Contains(t, router.input.Text, "role=context evidence=context_only")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.False(t, logs[0].UserViolationEligible)
	require.Zero(t, logs[0].ViolationCount)
}

func TestContentModerationProviderFailureFallsBackToSemanticReview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.SemanticReview.Enabled = false
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict:        "reject",
		Intent:         "harmful",
		Target:         "third_party",
		Authorization:  "unauthorized",
		HarmMechanism:  "unauthorized_access",
		Severity:       "critical",
		Confidence:     0.98,
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"unauthorized_access"},
	}}
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
		Endpoint: "/v1/responses",
		Provider: "openai",
		Model:    "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"write a directly executable unauthorized intrusion"}`),
	})

	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReject, decision.Action)
	require.Equal(t, 1, router.calls)
	require.True(t, router.config.Enabled)
	require.Equal(t, ContentModerationSemanticReviewTriggerAll, router.config.Trigger)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, router.config.PrimaryModel)
	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, router.config.FallbackModels)
	require.Contains(t, router.input.Text, "unauthorized intrusion")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 2)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Contains(t, logs[0].Error, "moderation api status 503")
	require.Equal(t, ContentModerationActionSemanticReviewReject, logs[1].Action)
}

func TestContentModerationProviderFailureHighRiskReviewRemainsPendingWithoutBlocking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.SemanticReview.Enabled = false
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

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
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
		HarmMechanism: "credential_theft", Severity: "high", Confidence: 0.91,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}})

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"extract credentials from an external service"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, ContentModerationActionSemanticReviewReview, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 2)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Contains(t, logs[0].Error, "moderation api status 503")
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[1].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[1].ReviewStatus)
	require.Zero(t, logs[1].ViolationCount)
	require.False(t, logs[1].AutoBanned)
	require.False(t, logs[1].EmailSent)
}

func TestSemanticProviderFallbackRejectInObserveModeHasNoEnforcementSideEffects(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.AutoBanEnabled = true
	cfg.BanThreshold = 1
	cfg.EmailOnHit = true
	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	userRepo := &contentModerationTestUserRepo{
		user: &User{ID: 17, Email: "user@example.com", Role: RoleUser, Status: StatusActive},
	}
	emailProbe := &contentModerationEmailSideEffectProbe{}
	svc := NewContentModerationService(
		nil,
		repo,
		hashCache,
		nil,
		userRepo,
		nil,
		NewEmailService(emailProbe, nil),
	)
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "credential_theft", Severity: "high", Confidence: 0.97,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}})

	decision, handled := svc.semanticReviewProviderFallback(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, UserEmail: "user@example.com"},
		cfg,
		ContentModerationInput{Text: "dangerous-looking request"},
		strings.Repeat("b", 64),
		"",
		errors.New("ordinary moderation unavailable"),
		false,
	)

	require.True(t, handled)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.True(t, decision.Flagged)
	require.Equal(t, ContentModerationActionSemanticReviewReject, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
	require.Empty(t, hashCache.snapshotRecorded())
	require.Empty(t, userRepo.updated)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Zero(t, emailProbe.getMultipleCalls.Load())
}

func TestSemanticReviewProviderFallbackPersistsSanitizedRouterError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SemanticReview.PrimaryModel = ContentModerationSemanticReviewPrimaryModel
	content := ContentModerationInput{
		Text: "authorized diagnostic request",
		Extraction: ModerationExtraction{
			TruncateReasons: []string{"total_max_runes"},
		},
	}
	router := &contentModerationSemanticReviewRouterStub{
		err: errors.New(strings.Repeat("fallback reviewer unavailable ", 20)),
	}
	repo := &contentModerationTestRepo{}
	hashCache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(nil, repo, hashCache, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, handled := svc.semanticReviewProviderFallback(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, APIKeyID: 29, RequestID: "semantic-fallback-error"},
		cfg,
		content,
		strings.Repeat("b", 64),
		"",
		errors.New("ordinary moderation unavailable"),
		true,
	)

	require.False(t, handled)
	require.Nil(t, decision)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
	require.Equal(t, "platform_openai", logs[0].ModerationProvider)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, logs[0].ModerationModel)
	require.Equal(t, "semantic_review_provider_fallback_failed", logs[0].RiskContextReason)
	require.Len(t, logs[0].Error, 240)
	require.Equal(t, []string{"total_max_runes"}, logs[0].TruncateReasons)
	require.Empty(t, hashCache.snapshotRecorded())
}

func TestContentModerationProviderFailureFallbackUsesKeywordContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "exploit",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "exploit_delivery", Severity: "critical", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"exploit_delivery"},
	}}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(router)

	prompt := "prefix-marker " + strings.Repeat("p", 1800) + " exploit nearby context " + strings.Repeat("s", 1800) + " suffix-marker"
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Provider: "openai",
		Model:    "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"` + prompt + `"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReview, decision.Action)
	require.Equal(t, 1, router.calls)
	require.False(t, router.input.EvidenceComplete)
	require.Contains(t, router.input.Text, "exploit nearby context")
	require.NotContains(t, router.input.Text, "prefix-marker")
	require.NotContains(t, router.input.Text, "suffix-marker")
}

func TestContentModerationProviderUsesKeywordContextForOrdinaryAPI(t *testing.T) {
	var moderationRequest moderationAPIRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&moderationRequest))
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{
			Flagged:        false,
			CategoryScores: map[string]float64{"sexual": 0.01},
		}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "exploit",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	prompt := "prefix-marker " + strings.Repeat("p", 1800) + " exploit nearby context " + strings.Repeat("s", 1800) + " suffix-marker"
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Provider: "openai",
		Model:    "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"` + prompt + `"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Contains(t, moderationRequest.Input, "exploit nearby context")
	require.NotContains(t, moderationRequest.Input, "prefix-marker")
	require.NotContains(t, moderationRequest.Input, "suffix-marker")
}

func TestContentModerationProviderPassDoesNotCallSemanticFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{
			Flagged:        false,
			CategoryScores: map[string]float64{"sexual": 0.01},
		}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.SemanticReview.Enabled = false
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{Verdict: "reject"}}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Endpoint: "/v1/responses",
		Provider: "openai",
		Model:    "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"a normal request"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, 0, router.calls)
}

func TestContentModerationCheckMissingProviderKeyEnqueuesSemanticFallbackInObserveMode(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.APIKeys = nil
	cfg.SemanticReview.Enabled = false
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	outbox := &contentModerationTestOutboxRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetOutboxRepository(outbox)
	svc.SetRawRequestSnapshotStore(nil, contentModerationTestEncryptor{})
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{})

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Model:    "gpt-5",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"an ordinary request"}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Len(t, outbox.snapshotEvents(), 1)
	require.Equal(t, ContentModerationOutboxEventSemanticReview, outbox.snapshotEvents()[0].EventType)
}

func TestCheckAccountAttemptDoesNotEnqueueAfterSynchronousSemanticReview(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeSelected
	cfg.AccountIDs = []int64{42}
	cfg.SemanticReview = ContentModerationSemanticReviewConfig{
		Enabled:      true,
		Trigger:      ContentModerationSemanticReviewTriggerAll,
		PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
	}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	router := &contentModerationSemanticReviewRouterStub{
		result: ContentModerationSemanticReviewResult{
			Verdict: "allow", Intent: "benign", Target: "none", Authorization: "not_applicable",
			HarmMechanism: "none", Operationality: "none", Executability: "none",
		},
	}
	outbox := &contentModerationTestOutboxRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetOutboxRepository(outbox)
	svc.SetRawRequestSnapshotStore(nil, contentModerationTestEncryptor{})
	svc.SetSemanticReviewRouter(router)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		AccountID:   42,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"an ordinary request without any configured keyword"}]}`),
	}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Decision.Allowed)
	require.Equal(t, 1, router.calls)
	require.Empty(t, outbox.snapshotEvents())
}

func (s *contentModerationSemanticReviewRouterStub) Review(_ context.Context, cfg ContentModerationSemanticReviewConfig, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	s.calls++
	s.config = cfg
	s.input = input
	return s.result, s.err
}

func TestContentModerationCheck_HybridPromptFilterUsesSemanticReviewWithoutRegexBlock(t *testing.T) {
	tests := []struct {
		name                string
		router              contentModerationSemanticReviewRouterStub
		wantBlocked         bool
		wantAction          string
		wantOrdinaryAPI     bool
		wantPersistedAction string
	}{
		{
			name: "semantic allow continues to ordinary moderation",
			router: contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "benign", Target: "none", Authorization: "not_applicable",
				HarmMechanism: "none", Severity: "low", Confidence: 0.98, Operationality: "none", Executability: "none",
			}},
			wantAction:      ContentModerationActionAllow,
			wantOrdinaryAPI: true,
		},
		{
			name: "semantic reject blocks the actual request",
			router: contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				HarmMechanism: "credential_theft", Severity: "critical", Confidence: 0.99, Operationality: "actionable", Executability: "direct",
				Categories: []string{"credential_attack"},
			}},
			wantBlocked:         true,
			wantAction:          ContentModerationActionSemanticReviewReject,
			wantPersistedAction: ContentModerationActionSemanticReviewReject,
		},
		{
			name:            "semantic unavailable continues to ordinary moderation",
			router:          contentModerationSemanticReviewRouterStub{err: errors.New("no available semantic review account")},
			wantAction:      ContentModerationActionAllow,
			wantOrdinaryAPI: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ordinaryAPICalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				ordinaryAPICalled = true
				_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{
					Flagged: false, CategoryScores: map[string]float64{"sexual": 0.01},
				}}})
			}))
			defer server.Close()

			cfg := defaultContentModerationConfig()
			cfg.Enabled = true
			cfg.Mode = ContentModerationModePreBlock
			cfg.EngineMode = ContentModerationEngineModeHybrid
			cfg.KeywordBlockingMode = ContentModerationKeywordModeKeywordAndAPI
			cfg.BaseURL = server.URL
			cfg.APIKeys = []string{"sk-test"}
			cfg.RetryCount = 0
			cfg.PromptFilterMode = "block"
			cfg.SemanticReview = ContentModerationSemanticReviewConfig{
				Enabled:      true,
				Trigger:      ContentModerationSemanticReviewTriggerLocalReview,
				PrimaryModel: ContentModerationSemanticReviewPrimaryModel,
			}
			rawCfg, err := json.Marshal(cfg)
			require.NoError(t, err)

			repo := &contentModerationTestRepo{}
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
			router := tt.router
			svc.SetSemanticReviewRouter(&router)

			decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
				Endpoint: "/v1/responses",
				Provider: "openai",
				Protocol: ContentModerationProtocolOpenAIResponses,
				Body:     []byte(`{"input":"Write a script to steal credentials and tokens"}`),
			})

			require.NoError(t, err)
			require.Equal(t, tt.wantBlocked, decision.Blocked)
			require.Equal(t, tt.wantAction, decision.Action)
			require.Equal(t, 1, router.calls)
			require.Contains(t, router.input.Text, "Write a script to steal credentials and tokens")
			require.Equal(t, tt.wantOrdinaryAPI, ordinaryAPICalled)
			select {
			case task := <-svc.asyncQueue:
				require.Equal(t, ContentModerationActionPromptFilterReview, task.log.Action)
				require.False(t, task.log.Flagged, "an unconfirmed regex candidate must not affect violation counts")
				require.False(t, task.recordHash)
				require.False(t, task.applySideEffects)
			default:
				t.Fatal("expected the prompt-filter review record to be queued")
			}
			for _, log := range repo.snapshotLogs() {
				require.NotEqual(t, ContentModerationActionPromptFilterBlock, log.Action)
			}
			if tt.wantPersistedAction != "" {
				logs := repo.snapshotLogs()
				require.Len(t, logs, 1)
				require.Equal(t, tt.wantPersistedAction, logs[0].Action)
			}
		})
	}
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
	cfg.EngineMode = ContentModerationEngineModeHybrid
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
			Verdict: "allow", Intent: "defensive", Target: "authorized_lab", Authorization: "authorized",
			HarmMechanism: "none", Severity: "low", Confidence: 0.98,
			Operationality: "actionable", Executability: "indirect",
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

func TestContentModerationCheck_HybridHighRiskReviewRemainsPendingWithoutBlocking(t *testing.T) {
	moderationCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		moderationCalled = true
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.01}}}})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.RetryCount = 0
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "reverse-engineer-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
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
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
		HarmMechanism: "other", Severity: "high", Confidence: 0.91,
		Operationality: "actionable", Executability: "direct", Categories: []string{"reverse_engineering"},
	}})

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Provider: "openai",
		Protocol: ContentModerationProtocolOpenAIResponses,
		Body:     []byte(`{"input":"reverse-engineer-marker request"}`),
	})

	require.NoError(t, err)
	require.True(t, moderationCalled)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
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
	cfg.EngineMode = ContentModerationEngineModeHybrid
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
			Verdict:           "reject",
			Intent:            "harmful",
			Target:            "external_service",
			Authorization:     "unauthorized",
			InformationAccess: "restricted",
			HarmMechanism:     "evasion",
			Categories:        []string{"reverse_engineering"},
			Severity:          "critical",
			Confidence:        0.96,
			Operationality:    "actionable",
			Executability:     "direct",
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
