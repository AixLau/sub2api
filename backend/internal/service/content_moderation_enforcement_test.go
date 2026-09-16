package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func contentModerationEnforcementTestMetadata(t *testing.T, log ContentModerationLog) map[string]any {
	t.Helper()
	metadata := map[string]any{}
	require.NoError(t, json.Unmarshal(log.Metadata, &metadata))
	return metadata
}

// TestSemanticReviewGatePreBlockRejectRecordsBlockedEnforcement pins the pair the
// audit record has to state explicitly: the content verdict and what the gateway
// did with the request.
func TestSemanticReviewGatePreBlockRejectRecordsBlockedEnforcement(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	content := ContentModerationInput{Text: "unauthorized intrusion plan for a third-party service"}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "unauthorized_access", HarmEvidence: "explicit", Severity: "high", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"unauthorized_access"},
	}}
	candidate := contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{Text: content.Text, EvidenceComplete: true},
	}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, &contentModerationTestHashCache{}, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, Protocol: ContentModerationProtocolOpenAIResponses},
		cfg,
		content,
		strings.Repeat("a", 64),
		candidate,
	)

	require.True(t, terminal)
	require.True(t, decision.Blocked)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReject, logs[0].Action)
	require.Equal(t, ContentModerationEnforcementBlocked, logs[0].Enforcement)
	require.Equal(t, ContentModerationModePreBlock, logs[0].Mode)
}

// TestSemanticReviewGateObserveRejectRecordsAllowedEnforcement is the case the
// audit export could not previously distinguish: a reject verdict in observe mode
// leaves the request forwarded, so the record must not read as a block.
func TestSemanticReviewGateObserveRejectRecordsAllowedEnforcement(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	content := ContentModerationInput{Text: "unauthorized intrusion plan for a third-party service"}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "unauthorized_access", HarmEvidence: "explicit", Severity: "high", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"unauthorized_access"},
	}}
	candidate := contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{Text: content.Text, EvidenceComplete: true},
	}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, &contentModerationTestHashCache{}, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, Protocol: ContentModerationProtocolOpenAIResponses},
		cfg,
		content,
		strings.Repeat("a", 64),
		candidate,
	)

	require.True(t, terminal)
	require.False(t, decision.Blocked)
	require.True(t, decision.Allowed)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReject, logs[0].Action)
	require.Equal(t, ContentModerationEnforcementAllowed, logs[0].Enforcement)
	require.Equal(t, ContentModerationModeObserve, logs[0].Mode)
}

// TestSemanticReviewGateRecordsRawVerdictAlongsidePolicyVerdict covers the audit
// gap that made policy_override uninformative: it said something had changed but
// not what the reviewer originally decided.
func TestSemanticReviewGateRecordsRawVerdictAlongsidePolicyVerdict(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	content := ContentModerationInput{Text: "apply this patch so the paid subscription check always passes"}
	// The reviewer allows; the platform licence-circumvention rule promotes it.
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "allow", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "license_bypass", HarmEvidence: "explicit", Severity: "high", Confidence: 0.9,
		Operationality: "actionable", Executability: "direct", Categories: []string{"license_cracking"},
	}}
	candidate := contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{Text: content.Text, EvidenceComplete: true},
	}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, &contentModerationTestHashCache{}, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	_, _ = svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, Protocol: ContentModerationProtocolOpenAIResponses},
		cfg,
		content,
		strings.Repeat("a", 64),
		candidate,
	)

	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	metadata := contentModerationEnforcementTestMetadata(t, logs[0])
	require.Equal(t, "reject", metadata["semantic_review_verdict"], "the post-policy verdict")
	require.Equal(t, "allow", metadata["semantic_review_raw_verdict"], "the reviewer's own verdict")
	require.Equal(t, true, metadata["semantic_review_policy_override"])
	require.Contains(t, metadata["semantic_review_reason_codes"], "platform_license_circumvention")
}

// TestSemanticReviewProviderFallbackPreBlockOmitsKeywordAndBlocks covers the
// pre-block half of the fallback path; the observe half is asserted in
// TestSemanticReviewProviderFallbackRejectInObserveModeHasNoEnforcementSideEffects.
func TestSemanticReviewProviderFallbackPreBlockOmitsKeywordAndBlocks(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SemanticReview.PrimaryModel = ContentModerationSemanticReviewPrimaryModel
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, &contentModerationTestHashCache{}, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
		HarmMechanism: "credential_theft", HarmEvidence: "explicit", Severity: "high", Confidence: 0.97,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}})

	decision, handled := svc.semanticReviewProviderFallback(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, APIKeyID: 29, RequestID: "semantic-fallback-preblock"},
		cfg,
		ContentModerationInput{Text: "dangerous-looking request"},
		strings.Repeat("b", 64),
		"",
		errors.New("ordinary moderation unavailable"),
		true,
	)

	require.True(t, handled)
	require.True(t, decision.Blocked)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReject, logs[0].Action)
	require.Equal(t, ContentModerationEnforcementBlocked, logs[0].Enforcement)
	require.Empty(t, logs[0].MatchedKeyword)
	require.Empty(t, logs[0].KeywordCategory)
	require.Empty(t, logs[0].KeywordSeverity)
	metadata := contentModerationEnforcementTestMetadata(t, logs[0])
	require.Equal(t, true, metadata["semantic_review_candidate_provider_fallback"])
	require.NotContains(t, metadata, "semantic_review_candidate")
}

// TestSemanticReviewGateRecordsPlainReviewVerdictAsPendingReview reproduces the
// production shape that produced a false "final semantic reviewer is unavailable"
// record roughly ten times an hour: a reviewer that returns review, on a
// deployment with no final reviewer enabled. Nothing was unavailable, so the
// record must state the content outcome instead.
func TestSemanticReviewGateRecordsPlainReviewVerdictAsPendingReview(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.EscalationEnabled = false
	content := ContentModerationInput{Text: "an ordinary request the reviewer could not resolve"}
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "unclear", Severity: "medium", Confidence: 0.4,
		ReasonCodes: []string{"ambiguous_context"},
	}}
	candidate := contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{Text: content.Text, EvidenceComplete: true},
	}
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(nil, repo, &contentModerationTestHashCache{}, nil, nil, nil, nil)
	svc.SetSemanticReviewRouter(router)

	decision, terminal := svc.semanticReviewGate(
		context.Background(),
		ContentModerationCheckInput{UserID: 17, Protocol: ContentModerationProtocolOpenAIResponses},
		cfg,
		content,
		strings.Repeat("a", 64),
		candidate,
	)

	require.True(t, terminal)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReview, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Equal(t, ContentModerationEnforcementAllowed, logs[0].Enforcement)
	require.Empty(t, logs[0].Error, "no reviewer was unavailable")
	require.False(t, logs[0].UserViolationEligible)
	require.Zero(t, logs[0].ViolationCount)
}

// TestCandidateSemanticReviewRecordsPlainReviewVerdictAsPendingReview is the
// candidate-path half of the same production shape.
func TestCandidateSemanticReviewRecordsPlainReviewVerdictAsPendingReview(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.Mode = ContentModerationModeObserve
	cfg.SemanticReview.EscalationEnabled = false
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	repo := &contentModerationTestRepo{}
	svc := candidateTestService(repo)
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "unclear", Severity: "medium", Confidence: 0.4,
		ReasonCodes: []string{"ambiguous_context"},
	}}
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   "danger-marker request",
	}}}

	decision := svc.checkCandidateOnly(context.Background(), ContentModerationCheckInput{
		UserID: 17, APIKeyID: 29, Protocol: ContentModerationProtocolOpenAIResponses,
	}, cfg, content)

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReview, decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.Equal(t, ContentModerationEnforcementAllowed, logs[0].Enforcement)
	require.Empty(t, logs[0].Error, "no reviewer was unavailable")
}

func TestApplySemanticReviewSubmittedLogDigestsStoredText(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.StoreInputExcerpt = true
	log := &ContentModerationLog{}
	submitted := "审计送审文本 with redacted [已脱敏] content"
	applySemanticReviewSubmittedLog(log, cfg, ContentModerationSemanticReviewResult{SubmittedText: submitted, SubmittedMaxRunes: 4000})

	want := sha256.Sum256([]byte(submitted))
	require.Equal(t, hex.EncodeToString(want[:]), log.SubmittedTextSHA256)
	require.Equal(t, submitted, log.SubmittedText)
	require.Equal(t, len([]rune(submitted)), log.SubmittedRunes)
}

// TestApplySemanticReviewSubmittedLogRespectsPrivacyGate pins the deliberate
// choice that the digest sits behind the same gate as the text: with
// store_input_excerpt off, a hash of a short prompt is as identifying as the
// prompt, so neither is retained.
func TestApplySemanticReviewSubmittedLogRespectsPrivacyGate(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.StoreInputExcerpt = false
	log := &ContentModerationLog{}
	applySemanticReviewSubmittedLog(log, cfg, ContentModerationSemanticReviewResult{SubmittedText: "现在几点了？", SubmittedMaxRunes: 4000})

	require.Empty(t, log.SubmittedText)
	require.Empty(t, log.SubmittedTextSHA256)
	// Length and truncation state are still recorded; only the content and its
	// digest are withheld.
	require.Equal(t, len([]rune("现在几点了？")), log.SubmittedRunes)
	require.Equal(t, 4000, log.SubmittedMaxRunes)
}

// TestRecordPreBlockSyncMetricSeparatesReviewerOutages keeps a reviewer outage out
// of the content-blocking counter, where it used to look like the pipeline had
// blocked an actual violation.
func TestRecordPreBlockSyncMetricSeparatesReviewerOutages(t *testing.T) {
	svc := &ContentModerationService{}
	svc.recordPreBlockSyncMetric(1, ContentModerationActionSemanticReviewUnavailable)
	svc.recordPreBlockSyncMetric(1, ContentModerationActionSemanticReviewIncomplete)
	svc.recordPreBlockSyncMetric(1, ContentModerationActionSemanticReviewReject)
	svc.recordPreBlockSyncMetric(1, ContentModerationActionError)

	require.Equal(t, int64(1), svc.preBlockBlocked.Load())
	require.Equal(t, int64(2), svc.preBlockTechnicalFailures.Load())
	require.Equal(t, int64(1), svc.preBlockErrors.Load())
	require.Equal(t, int64(4), svc.preBlockChecked.Load())
}
