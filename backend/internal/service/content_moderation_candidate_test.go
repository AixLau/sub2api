package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/promptfilter"
	"github.com/stretchr/testify/require"
)

type candidateRetryDedupeRepo struct {
	contentModerationTestRepo
	duplicateRetries atomic.Int64
}

func (r *candidateRetryDedupeRepo) IncrementDuplicateRetryCount(context.Context, string) error {
	r.duplicateRetries.Add(1)
	return nil
}

func candidateTestConfig() *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	cfg.RecordNonHits = true
	cfg.DecisionCacheEnabled = true
	cfg.DecisionCacheTTLSeconds = 600
	cfg.CandidateFragmentRunes = maxContentModerationCandidateRunes
	cfg.PromptFilterMode = promptfilter.ModeOff
	cfg.normalize()
	return cfg
}

func candidateTestService(repo ContentModerationRepository) *ContentModerationService {
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.SetDecisionCacheKey(bytes.Repeat([]byte{0x5a}, 32))
	return svc
}

func TestCandidateModeEnforcesBoundedReviewerInvariants(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.EngineMode = ContentModerationEngineModeCandidateOnly
	cfg.AuditScope = ContentModerationAuditScopeAllContext
	cfg.RecordNonHits = true
	cfg.CandidateFragmentRunes = 12_000
	cfg.SemanticReview.Enabled = false
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.SemanticReview.MaxInputRunes = 12_000

	normalizeContentModerationCandidateOnlyInvariants(cfg)

	require.Equal(t, ContentModerationAuditScopeUserOnly, cfg.AuditScope)
	require.False(t, cfg.RecordNonHits)
	require.Equal(t, maxContentModerationCandidateRunes, cfg.CandidateFragmentRunes)
	require.True(t, cfg.SemanticReview.Enabled)
	require.Equal(t, ContentModerationSemanticReviewTriggerLocalReview, cfg.SemanticReview.Trigger)
	require.Equal(t, maxContentModerationCandidateRunes, cfg.SemanticReview.MaxInputRunes)
}

func TestCandidateAdaptiveWindowUsesPreferredSizeForOrdinaryContext(t *testing.T) {
	text := strings.Repeat("前文", 700) + " danger-marker " + strings.Repeat("后文", 700)

	got := contentModerationCandidateAdaptiveRunes(text, maxContentModerationCandidateRunes)

	require.Equal(t, contentModerationCandidatePreferredRunes, got)
}

func TestCandidateAdaptiveWindowKeepsFullBudgetForAuthorizationContext(t *testing.T) {
	text := strings.Repeat("前文", 700) + " 已获授权的测试环境 danger-marker " + strings.Repeat("后文", 700)

	got := contentModerationCandidateAdaptiveRunes(text, maxContentModerationCandidateRunes)

	require.Equal(t, maxContentModerationCandidateRunes, got)
}

func TestCandidateSelectionUsesMatchedUserSourceOnly(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	content := ContentModerationInput{Sources: []ContentModerationInputSource{
		{Source: "responses.system", Role: "system", Text: "danger-marker from a system prompt"},
		{Source: "responses.input[0].role=user.content", Role: "user", Text: "history asks about danger-marker"},
		{Source: "responses.input[1].tool_result", Role: "tool", Text: "danger-marker from a tool"},
		{Source: "responses.input[2].role=developer.content", Role: "developer", Text: "# AGENTS.md\ndanger-marker from platform wrapper"},
		{Source: "responses.input[3].role=user.content", Role: "user", Text: "latest user turn is harmless"},
	}}

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)

	require.True(t, found)
	require.Equal(t, "responses.input[0].role=user.content", selection.Source.Source)
	require.Equal(t, "history asks about danger-marker", selection.Fragment)
	require.NotContains(t, selection.Fragment, "system prompt")
	require.NotContains(t, selection.Fragment, "tool")
	require.NotContains(t, selection.Fragment, "AGENTS.md")
}

func TestCandidateSelectionDoesNotLetWrapperMarkersBypassUserContent(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   "# AGENTS.md\ndanger-marker in an actual user request",
	}}}

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)

	require.True(t, found)
	require.Equal(t, "responses.input[0].role=user.content", selection.Source.Source)
	require.Contains(t, selection.Fragment, "danger-marker")
}

func TestCandidateModeIgnoresLargeResponsesToolOutputInUserOnlyScope(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.EngineMode = ContentModerationEngineModeCandidateOnly
	cfg.AuditScope = ContentModerationAuditScopeUserOnly
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	body := []byte(`{"input":[{"type":"function_call_output","output":"` + strings.Repeat("danger-marker ", maxModerationInputRunes) + `"},{"type":"message","role":"user","content":"safe request"}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)
	repo := &contentModerationTestRepo{}
	svc := candidateTestService(repo)

	decision := svc.checkCandidateOnly(context.Background(), ContentModerationCheckInput{Protocol: ContentModerationProtocolOpenAIResponses, Body: body}, cfg, content)

	require.True(t, decision.Allowed)
	require.Empty(t, repo.snapshotLogs())
}

func TestCandidateExtractionFailureStoresSelectedSourceAndReasons(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	repo := &contentModerationTestRepo{}
	svc := candidateTestService(repo)
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source:          "responses.input[0].role=user.content",
		Role:            "user",
		Text:            "danger-marker request",
		Truncated:       true,
		TruncateReasons: []string{"max_total_runes"},
	}}}

	decision := svc.checkCandidateOnly(context.Background(), ContentModerationCheckInput{Protocol: ContentModerationProtocolOpenAIResponses}, cfg, content)

	require.True(t, decision.Allowed)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, "candidate_extraction_incomplete", logs[0].Error)
	require.Equal(t, []string{"max_total_runes"}, logs[0].TruncateReasons)
	require.Equal(t, "responses.input[0].role=user.content", logs[0].SelectedSource)
	require.Equal(t, "user", logs[0].SelectedSourceRole)
	require.Equal(t, "danger-marker request", logs[0].InputExcerpt)
}

func TestCandidateExtractionFailureCollapsesRepeatedRetriesIntoOneAudit(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source:          "responses.input[0].role=user.content",
		Role:            "user",
		Text:            "danger-marker request",
		Truncated:       true,
		TruncateReasons: []string{"unsupported_required_value"},
	}}}
	input := ContentModerationCheckInput{UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Protocol: ContentModerationProtocolOpenAIResponses}

	first := svc.checkCandidateOnly(context.Background(), input, cfg, content)
	second := svc.checkCandidateOnly(context.Background(), input, cfg, content)

	require.True(t, first.Allowed)
	require.True(t, second.Allowed)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, "candidate_extraction_incomplete", logs[0].Error)
	require.Equal(t, int64(1), repo.duplicateRetries.Load())
}

func TestCandidateExtractionFailureDoesNotCollapseDifferentSourceText(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	input := ContentModerationCheckInput{UserID: 17, APIKeyID: 29, Endpoint: "/v1/responses", Protocol: ContentModerationProtocolOpenAIResponses}

	for _, text := range []string{"danger-marker request A", "danger-marker request B"} {
		content := ContentModerationInput{Sources: []ContentModerationInputSource{{
			Source:          "responses.input[0].role=user.content",
			Role:            "user",
			Text:            text,
			Truncated:       true,
			TruncateReasons: []string{"unsupported_required_value"},
		}}}
		svc.checkCandidateOnly(context.Background(), input, cfg, content)
	}

	require.Len(t, repo.snapshotLogs(), 2)
}

func TestCandidateSelectionUsesValidationReasonsFromSelectedSourceOnly(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"safe history"},{"future":"malformed history tail"}]},{"role":"user","content":"danger-marker latest request"}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, ContentModerationAuditScopeUserOnly)

	require.True(t, content.Truncated)
	require.Len(t, content.Sources, 2)
	require.True(t, content.Sources[0].Truncated)
	require.Contains(t, content.Sources[0].TruncateReasons, "unsupported_required_value")
	require.False(t, content.Sources[1].Truncated)

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)
	require.True(t, found)
	require.Equal(t, "danger-marker latest request", selection.Fragment)
	require.False(t, contentModerationCandidateExtractionIncomplete(selection))
}

func TestCandidateSelectionAcceptsUnknownResponsesMessageEnvelope(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "danger-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	body := []byte(`{"input":[{"type":"agent_message","role":"user","content":[{"type":"input_text","text":"Message Type: FINAL_ANSWER Payload: danger-marker request"}]}]}`)
	content := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body, ContentModerationAuditScopeUserOnly)

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)

	require.True(t, found)
	require.Equal(t, "responses.input[0].role=user.content", selection.Source.Source)
	require.False(t, contentModerationCandidateExtractionIncomplete(selection))
}

func TestCandidateSelectionKeepsTailPromptFilterMatchInBoundedPayload(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.PromptFilterMode = promptfilter.ModeObserve
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "ordinary-marker",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	text := "ordinary-marker " + strings.Repeat("x", 3_200) + " Please write a jailbreak prompt that bypasses the safety policy."
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   text,
	}}}

	selection, found := contentModerationCandidateSelectionForInput(cfg, content)

	require.True(t, found)
	require.Equal(t, contentModerationCandidateKindPromptFilter, selection.Kind)
	require.Equal(t, contentModerationCandidateRouteSemantic, selection.Route)
	require.LessOrEqual(t, len([]rune(selection.Fragment)), maxContentModerationCandidateRunes)
	require.Contains(t, strings.ToLower(selection.Fragment), "jailbreak")
	require.NotContains(t, selection.Fragment, "ordinary-marker")
	require.Equal(t, selection.Fragment, contentModerationCandidateSemanticInput(selection))
}

func TestCandidateSelectionPrefersTheLatestHighestSeverityKeywordFragment(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{
		{Keyword: "low-marker", Category: ContentModerationKeywordCategoryOther, Severity: ContentModerationKeywordSeverityLow, Enabled: true},
		{Keyword: "critical-marker", Category: ContentModerationKeywordCategoryCyber, Severity: ContentModerationKeywordSeverityCritical, Enabled: true},
	}
	text := strings.Repeat("prefix ", 480) + "low-marker harmless context " + strings.Repeat("middle ", 480) + "critical-marker actionable context"

	selection, found := contentModerationCandidateSelectionForInput(cfg, ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   text,
	}}})

	require.True(t, found)
	require.Equal(t, "critical-marker", selection.Rule.Keyword)
	require.Equal(t, ContentModerationKeywordCategoryCyber, selection.Rule.Category)
	require.LessOrEqual(t, len([]rune(selection.Fragment)), maxContentModerationCandidateRunes)
	require.Contains(t, selection.Fragment, "critical-marker")
	require.NotContains(t, selection.Fragment, "low-marker")
}

func TestCandidateSelectionPrioritizesSemanticRiskOverCriticalOrdinaryKeyword(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.KeywordRules = []ContentModerationKeywordRule{
		{Keyword: "ordinary-critical", Category: ContentModerationKeywordCategoryOther, Severity: ContentModerationKeywordSeverityCritical, Enabled: true},
		{Keyword: "cyber-high", Category: ContentModerationKeywordCategoryCyber, Severity: ContentModerationKeywordSeverityHigh, Enabled: true},
	}

	selection, found := contentModerationCandidateSelectionForInput(cfg, ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   "ordinary-critical content with cyber-high operational request",
	}}})

	require.True(t, found)
	require.Equal(t, "cyber-high", selection.Rule.Keyword)
	require.Equal(t, contentModerationCandidateRouteSemantic, selection.Route)
}

func TestCandidateRouteUsesOrdinaryAPIOnlyForSupportedCategories(t *testing.T) {
	openAI := candidateTestConfig()
	openAI.Provider = "openai"
	zhipu := candidateTestConfig()
	zhipu.Provider = "zhipu"

	require.Equal(t, contentModerationCandidateRouteOrdinary, contentModerationCandidateRouteFor(openAI, ContentModerationKeywordCategoryOther))
	require.Equal(t, contentModerationCandidateRouteOrdinary, contentModerationCandidateRouteFor(zhipu, ContentModerationKeywordCategoryPolitical))
	require.Equal(t, contentModerationCandidateRouteSemantic, contentModerationCandidateRouteFor(openAI, ContentModerationKeywordCategoryPolitical))
	require.Equal(t, contentModerationCandidateRouteSemantic, contentModerationCandidateRouteFor(openAI, "prompt_injection"))
	require.Equal(t, contentModerationCandidateRouteSemantic, contentModerationCandidateRouteFor(openAI, ContentModerationKeywordCategoryCyber))
}

func TestCandidateDecisionCacheSuppressesRepeatedModelExecutionAndSideEffects(t *testing.T) {
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	cfg := candidateTestConfig()
	selection := contentModerationCandidateSelection{
		Source:   ContentModerationInputSource{Source: "responses.input", Role: "user", Text: "candidate"},
		Origin:   contentModerationSourceOriginUserTurn,
		Kind:     contentModerationCandidateKindKeyword,
		Rule:     ContentModerationKeywordRule{Keyword: "candidate", Category: ContentModerationKeywordCategoryOther},
		Fragment: "candidate",
		Route:    contentModerationCandidateRouteOrdinary,
	}
	var executions atomic.Int64
	run := func(context.Context) contentModerationCandidateOutcome {
		executions.Add(1)
		return contentModerationCandidateOutcome{
			Decision:   &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow},
			DecisionID: "candidate-decision-1",
			Cacheable:  true,
		}
	}
	input := ContentModerationCheckInput{UserID: 17, APIKeyID: 29}

	first := svc.executeCandidateDecision(context.Background(), cfg, input, selection, run)
	second := svc.executeCandidateDecision(context.Background(), cfg, input, selection, run)

	require.True(t, first.Decision.Allowed)
	require.True(t, second.Decision.Allowed)
	require.True(t, second.CacheHit)
	require.Equal(t, int64(1), executions.Load())
	require.Equal(t, int64(1), repo.duplicateRetries.Load())
}

func TestCandidateDecisionCoordinatorCollapsesConcurrentRetries(t *testing.T) {
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	cfg := candidateTestConfig()
	selection := contentModerationCandidateSelection{
		Source:   ContentModerationInputSource{Source: "responses.input", Role: "user", Text: "candidate"},
		Origin:   contentModerationSourceOriginUserTurn,
		Kind:     contentModerationCandidateKindKeyword,
		Rule:     ContentModerationKeywordRule{Keyword: "candidate", Category: ContentModerationKeywordCategoryOther},
		Fragment: "candidate",
		Route:    contentModerationCandidateRouteOrdinary,
	}
	var executions atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	run := func(context.Context) contentModerationCandidateOutcome {
		if executions.Add(1) == 1 {
			close(started)
		}
		<-release
		return contentModerationCandidateOutcome{
			Decision:   &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow},
			DecisionID: "candidate-decision-concurrent",
			Cacheable:  true,
		}
	}
	input := ContentModerationCheckInput{UserID: 17, APIKeyID: 29}

	const callers = 5
	gate := make(chan struct{})
	results := make(chan contentModerationCandidateOutcome, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			results <- svc.executeCandidateDecision(context.Background(), cfg, input, selection, run)
		}()
	}
	close(gate)
	<-started
	require.Eventually(t, func() bool { return executions.Load() == 1 }, time.Second, 5*time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	for result := range results {
		require.NotNil(t, result.Decision)
		require.True(t, result.Decision.Allowed)
	}
	require.Equal(t, int64(1), executions.Load())
	require.Equal(t, int64(callers-1), repo.duplicateRetries.Load())
}

func TestCandidateAuditDoesNotApplyObserveModeSideEffects(t *testing.T) {
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	cfg := candidateTestConfig()
	cfg.Mode = ContentModerationModeObserve
	selection := contentModerationCandidateSelection{
		Source:   ContentModerationInputSource{Source: "responses.input", Role: "user", Text: "candidate"},
		Origin:   contentModerationSourceOriginUserTurn,
		Kind:     contentModerationCandidateKindKeyword,
		Rule:     ContentModerationKeywordRule{Keyword: "candidate", Category: ContentModerationKeywordCategoryOther},
		Fragment: "candidate",
		Route:    contentModerationCandidateRouteOrdinary,
	}
	log := svc.buildCandidateLog(
		ContentModerationCheckInput{UserID: 17},
		cfg,
		selection,
		contentModerationDecisionSourceOrdinaryAPI,
		ContentModerationActionAllow,
		true,
		"violence",
		1,
		map[string]float64{"violence": 1},
		nil,
		"",
	)

	svc.persistCandidateAudit(context.Background(), ContentModerationCheckInput{UserID: 17}, cfg, selection, log, false)

	require.Len(t, repo.logs, 1)
	require.Zero(t, repo.logs[0].ViolationCount)
	require.False(t, repo.logs[0].AutoBanned)
	require.False(t, repo.logs[0].EmailSent)
}

type candidateEvidenceStore struct {
	mu        sync.Mutex
	snapshots map[int64]ContentModerationEvidenceSnapshot
}

func (s *candidateEvidenceStore) CreateEvidenceSnapshot(_ context.Context, snapshot *ContentModerationEvidenceSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshots == nil {
		s.snapshots = make(map[int64]ContentModerationEvidenceSnapshot)
	}
	copy := *snapshot
	copy.ID = int64(len(s.snapshots) + 1)
	if copy.CreatedAt.IsZero() {
		copy.CreatedAt = time.Now()
	}
	s.snapshots[copy.LogID] = copy
	snapshot.ID = copy.ID
	snapshot.CreatedAt = copy.CreatedAt
	return nil
}

func (s *candidateEvidenceStore) GetEvidenceSnapshotByLogID(_ context.Context, logID int64) (*ContentModerationEvidenceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.snapshots[logID]
	if !ok {
		return nil, context.Canceled
	}
	return &snapshot, nil
}

func TestCandidateAuditStoresExactReviewerPayloadEvidence(t *testing.T) {
	repo := &candidateRetryDedupeRepo{}
	svc := candidateTestService(repo)
	evidence := &candidateEvidenceStore{}
	svc.SetEvidenceStore(evidence, contentModerationTestEncryptor{})
	cfg := candidateTestConfig()
	selection := contentModerationCandidateSelection{
		Source:   ContentModerationInputSource{Source: "responses.input[2].role=user.content", Role: "user", Text: "candidate context"},
		Origin:   contentModerationSourceOriginUserTurn,
		Kind:     contentModerationCandidateKindKeyword,
		Rule:     ContentModerationKeywordRule{Keyword: "candidate", Category: ContentModerationKeywordCategoryCyber, Severity: ContentModerationKeywordSeverityHigh},
		Fragment: "candidate context",
		Route:    contentModerationCandidateRouteSemantic,
	}
	log := svc.buildCandidateLog(
		ContentModerationCheckInput{RequestID: "req-evidence", UserID: 17},
		cfg,
		selection,
		contentModerationDecisionSourceSemantic,
		ContentModerationActionSemanticReviewAllow,
		false,
		"semantic_review",
		0.9,
		map[string]float64{"semantic_review": 0.9},
		nil,
		"",
	)

	svc.persistCandidateAudit(context.Background(), ContentModerationCheckInput{RequestID: "req-evidence", UserID: 17}, cfg, selection, log, false)
	view, err := svc.GetEvidenceSnapshot(context.Background(), log.ID)

	require.NoError(t, err)
	require.Equal(t, selection.Fragment, view.Payload)
	require.Equal(t, len([]rune(selection.Fragment)), view.PayloadRunes)
	require.Equal(t, selection.Source.Source, view.Selection["selected_source"])
	metadata, err := json.Marshal(view.Selection)
	require.NoError(t, err)
	require.NotContains(t, string(metadata), "system")
}

func TestCandidateAccountRetryReusesPriorCandidateDecision(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.Enabled = true
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "candidate-cyber",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityHigh,
		Enabled:  true,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &candidateRetryDedupeRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo, nil, nil, nil, nil, nil,
	)
	svc.SetDecisionCacheKey(bytes.Repeat([]byte{0x5a}, 32))
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{Verdict: "allow"}}
	svc.SetSemanticReviewRouter(router)
	input := ContentModerationCheckInput{
		UserID:      17,
		APIKeyID:    29,
		AccountID:   42,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"candidate-cyber request"}]}`),
	}

	first, err := svc.CheckAccountAttempt(context.Background(), input, nil)
	require.NoError(t, err)
	require.NotNil(t, first.NextState)
	second, err := svc.CheckAccountAttempt(context.Background(), input, first.NextState)

	require.NoError(t, err)
	require.True(t, second.Reused)
	require.Equal(t, 1, router.calls)
	require.Equal(t, int64(1), repo.duplicateRetries.Load())
}

func TestCandidateCheckAllowsMissWithoutCallingReviewers(t *testing.T) {
	var ordinaryCalls atomic.Int64
	ordinary := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		ordinaryCalls.Add(1)
	}))
	defer ordinary.Close()

	cfg := candidateTestConfig()
	cfg.Enabled = true
	cfg.BaseURL = ordinary.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "candidate-marker",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &candidateRetryDedupeRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo, nil, nil, nil, nil, nil,
	)
	svc.SetDecisionCacheKey(bytes.Repeat([]byte{0x5a}, 32))
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{Verdict: "allow"}}
	svc.SetSemanticReviewRouter(router)

	body, err := json.Marshal(map[string]any{"messages": []map[string]string{
		{"role": "system", "content": "candidate-marker from system context"},
		{"role": "user", "content": "a harmless user request"},
	}})
	require.NoError(t, err)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   17,
		APIKeyID: 29,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
	require.Zero(t, ordinaryCalls.Load())
	require.Zero(t, router.calls)
	require.Empty(t, repo.snapshotLogs())
}

func TestCandidateCheckUsesOneBoundedOrdinaryPayloadAndCachesRetry(t *testing.T) {
	var ordinaryCalls atomic.Int64
	var requestMu sync.Mutex
	var reviewerInput string
	ordinary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ordinaryCalls.Add(1)
		var request moderationAPIRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		input, ok := request.Input.(string)
		require.True(t, ok)
		requestMu.Lock()
		reviewerInput = input
		requestMu.Unlock()
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{
			CategoryScores: map[string]float64{"sexual": 0.01},
		}}})
	}))
	defer ordinary.Close()

	cfg := candidateTestConfig()
	cfg.Enabled = true
	cfg.BaseURL = ordinary.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "ordinary-marker",
		Category: ContentModerationKeywordCategoryOther,
		Severity: ContentModerationKeywordSeverityHigh,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &candidateRetryDedupeRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo, nil, nil, nil, nil, nil,
	)
	svc.SetDecisionCacheKey(bytes.Repeat([]byte{0x5a}, 32))

	userText := strings.Repeat("prefix ", 260) + "ordinary-marker relevant user context " + strings.Repeat("suffix ", 260)
	body, err := json.Marshal(map[string]any{"messages": []map[string]string{
		{"role": "system", "content": "system-only context must not be sent"},
		{"role": "user", "content": userText},
	}})
	require.NoError(t, err)
	input := ContentModerationCheckInput{
		UserID:   17,
		APIKeyID: 29,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	}

	first, err := svc.Check(context.Background(), input)
	require.NoError(t, err)
	second, err := svc.Check(context.Background(), input)

	require.NoError(t, err)
	require.True(t, first.Allowed)
	require.True(t, second.Allowed)
	require.Equal(t, int64(1), ordinaryCalls.Load())
	require.Equal(t, int64(1), repo.duplicateRetries.Load())
	require.Len(t, repo.snapshotLogs(), 1)
	requestMu.Lock()
	defer requestMu.Unlock()
	require.Contains(t, reviewerInput, "ordinary-marker")
	require.NotContains(t, reviewerInput, "system-only context")
	require.LessOrEqual(t, len([]rune(reviewerInput)), maxContentModerationCandidateRunes)
}

func TestCandidateCheckRoutesCyberCandidateToSemanticReview(t *testing.T) {
	var ordinaryCalls atomic.Int64
	ordinary := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		ordinaryCalls.Add(1)
	}))
	defer ordinary.Close()

	cfg := candidateTestConfig()
	cfg.Enabled = true
	cfg.BaseURL = ordinary.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.KeywordRules = []ContentModerationKeywordRule{{
		Keyword:  "cyber-marker",
		Category: ContentModerationKeywordCategoryCyber,
		Severity: ContentModerationKeywordSeverityCritical,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &candidateRetryDedupeRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo, nil, nil, nil, nil, nil,
	)
	svc.SetDecisionCacheKey(bytes.Repeat([]byte{0x5a}, 32))
	router := &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict:        "review",
		Categories:     []string{"cyber"},
		Confidence:     0.8,
		Operationality: "actionable",
	}}
	svc.SetSemanticReviewRouter(router)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:   17,
		APIKeyID: 29,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"cyber-marker request"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSemanticReviewReview, decision.Action)
	require.Equal(t, 1, router.calls)
	require.Zero(t, ordinaryCalls.Load())
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
	require.Equal(t, "platform_openai", logs[0].ModerationProvider)

	status, err := svc.GetStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), status.PreBlockChecked)
	require.Equal(t, int64(1), status.PreBlockAllowed)
	require.Zero(t, status.PreBlockBlocked)
}
