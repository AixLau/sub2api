package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// submittedTextBackend captures the input handed to the upstream reviewer so a
// test can assert the audit record matches the actual model request.
type submittedTextBackend struct {
	lastInput ContentModerationSemanticReviewInput
	calls     int
}

func (b *submittedTextBackend) SelectSemanticReviewAccount(_ context.Context, _ *int64, _ string, _ map[int64]struct{}) (*AccountSelectionResult, error) {
	return &AccountSelectionResult{
		Account:     &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key"}},
		ReleaseFunc: func() {},
	}, nil
}

func (b *submittedTextBackend) ReviewSemanticContent(_ context.Context, _ *Account, _ string, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	b.calls++
	b.lastInput = input
	return ContentModerationSemanticReviewResult{Verdict: "allow", Confidence: 0.99, Model: ContentModerationSemanticReviewPrimaryModel}, nil
}

func newSubmittedTextRouter(backend ContentModerationSemanticReviewBackend) ContentModerationSemanticReviewRouter {
	return NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)
}

func TestSemanticReviewSubmitBudgetGovernsEvidenceAndModelRequest(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.MaxInputRunes = 10_000
	cfg.MaxSubmitRunes = 40
	cfg.PrimaryModel = ContentModerationSemanticReviewPrimaryModel
	cfg.FallbackModels = nil

	body := "head-marker " + strings.Repeat("界", 200) + " tail-marker"
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   body,
	}}}

	text, complete := buildContentModerationSemanticReviewEvidence(cfg, content, "")
	require.False(t, complete)
	require.LessOrEqual(t, len([]rune(text)), 40)

	backend := &submittedTextBackend{}
	router := newSubmittedTextRouter(backend)
	input := contentModerationSemanticReviewInputForCheck(ContentModerationCheckInput{}, text, "")
	input.MaxInputRunes = cfg.effectiveSubmitRunes()

	result, err := router.Review(context.Background(), cfg, input)
	require.NoError(t, err)
	require.Equal(t, text, backend.lastInput.Text)
	require.Equal(t, text, result.SubmittedText)
	require.Equal(t, len([]rune(text)), len([]rune(result.SubmittedText)))
	require.Equal(t, 40, result.SubmittedMaxRunes)
	require.LessOrEqual(t, len([]rune(result.SubmittedText)), 40)
}

func TestSemanticReviewSubmittedTextDefaultsToLegacyMaxInputRunes(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.MaxInputRunes = 123
	// MaxSubmitRunes intentionally unset: legacy configurations must keep
	// submitting up to MaxInputRunes.
	require.Zero(t, cfg.MaxSubmitRunes)
	require.Equal(t, 123, cfg.effectiveSubmitRunes())
}

func TestSemanticReviewSubmittedTextLogsExactSentText(t *testing.T) {
	cfg := candidateTestConfig()
	cfg.SemanticReview.Enabled = true
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.SemanticReview.MaxInputRunes = 10_000
	cfg.SemanticReview.MaxSubmitRunes = 32

	repo := &contentModerationTestRepo{}
	svc := candidateTestService(repo)
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "allow", Intent: "benign", Target: "none", Authorization: "not_applicable",
		HarmMechanism: "none", Severity: "low", Confidence: 0.98, Operationality: "none", Executability: "none",
		Model:             ContentModerationSemanticReviewPrimaryModel,
		SubmittedText:     "danger-marker request",
		SubmittedMaxRunes: 32,
	}}

	source := ContentModerationInputSource{
		Source: "responses.input[0].role=user.content", Role: "user",
		Text: "danger-marker request",
	}
	selection := contentModerationCandidateSelectionFromRule(cfg, source, contentModerationSourceOriginUserTurn,
		ContentModerationKeywordRule{Keyword: "danger-marker", Category: ContentModerationKeywordCategoryCyber,
			Severity: ContentModerationKeywordSeverityHigh, Action: ContentModerationKeywordActionBlock, Enabled: true},
		contentModerationCandidateKindKeyword)

	svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 17}, cfg, selection, "")

	logs := repo.snapshotLogs()
	require.NotEmpty(t, logs)
	log := logs[len(logs)-1]
	require.Equal(t, "danger-marker request", log.SubmittedText)
	require.Equal(t, len([]rune("danger-marker request")), log.SubmittedRunes)
	require.Equal(t, 32, log.SubmittedMaxRunes)
	require.False(t, log.SubmittedTruncated)
	// The 240-rune summary field now mirrors the submitted text instead of an
	// unrelated fragment.
	require.Equal(t, "danger-marker request", log.InputExcerpt)
}

func TestSemanticReviewSubmittedTextMarksTruncation(t *testing.T) {
	text, maxRunes, truncated, reasons := contentModerationSemanticSubmittedText(strings.Repeat("界", 50), 20)
	require.Equal(t, 20, len([]rune(text)))
	require.Equal(t, 20, maxRunes)
	require.True(t, truncated)
	require.Equal(t, []string{"submit_max_runes"}, reasons)
}

func TestBuildSemanticReviewInputUsesSubmitBudgetNotLegacyBudget(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.MaxInputRunes = 4_000
	cfg.MaxSubmitRunes = 50
	content := ContentModerationInput{Sources: []ContentModerationInputSource{{
		Source: "responses.input[0].role=user.content",
		Role:   "user",
		Text:   "head " + strings.Repeat("界", 500) + " tail",
	}}}

	text, _ := buildContentModerationSemanticReviewEvidence(cfg, content, "")

	require.LessOrEqual(t, len([]rune(text)), 50)
}
