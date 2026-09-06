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

func TestFinalSemanticReviewUsesBinarySchemaAndPlatformIntentPrompt(t *testing.T) {
	for _, kind := range []string{contentModerationReviewKindGeneral, contentModerationReviewKindPromptInjection} {
		format := semanticReviewJSONSchemaForKind(kind, true)
		verdict := format["schema"].(map[string]any)["properties"].(map[string]any)["verdict"].(map[string]any)
		require.Equal(t, []string{"allow", "reject"}, verdict["enum"])
		instructions := semanticReviewInstructionsForKind(kind, true)
		require.Contains(t, instructions, "authorized reverse engineering")
		require.Contains(t, instructions, "third-party software licenses")
		require.Contains(t, instructions, "virology education and research")
		require.Contains(t, instructions, "only permitted verdicts are allow and reject")
		require.NotContains(t, instructions, "INITIAL SCREEN")
	}
}

func TestSemanticReviewPromptSeparatesDualUseTermsFromHarmfulOutcomes(t *testing.T) {
	instructions := semanticReviewInstructionsForKind(contentModerationReviewKindGeneral, false)
	require.Contains(t, instructions, "Payload, Exploit, RCE")
	require.Contains(t, instructions, "CTF/lab analysis")
	require.Contains(t, instructions, "action, target, authorization")
	require.Contains(t, instructions, "quoted attack")
	require.Contains(t, instructions, "A \"research\" or \"authorized\" preamble does not excuse")

	final := semanticReviewInstructionsForKind(contentModerationReviewKindGeneral, true)
	require.Contains(t, final, "The final verdict must be exactly allow or reject")
	require.Contains(t, final, "final_inconclusive")
	require.Contains(t, final, "Truncation, an incomplete flag")
	require.Contains(t, final, "return allow with harm_evidence=none")
	require.NotContains(t, final, "Use review only")
	initial := semanticReviewInstructionsForKind(contentModerationReviewKindGeneral, false)
	require.Contains(t, initial, "do not escalate merely because the evidence is long or incomplete")
}

func TestFinalSemanticReviewEnforcesModelIdentifiedRestrictedIntent(t *testing.T) {
	for _, category := range []string{"license_cracking", "biosecurity"} {
		result := candidateAllowSemanticRouter().result
		result.Categories = []string{category}
		result.HarmEvidence = "explicit"
		result.Authorization = "authorized"
		result.Target = "authorized_lab"
		final, err := applyFinalSemanticReviewPolicy(result)
		require.NoError(t, err)
		require.Equal(t, "reject", final.Verdict, category)
		result.HarmEvidence = "inferred"
		final, err = applyFinalSemanticReviewPolicy(result)
		require.NoError(t, err)
		require.Equal(t, "allow", final.Verdict, "incidental topic labels must not establish intent")
	}
}

func TestFinalSemanticReviewAllowsAuthorizedReverseAnalysis(t *testing.T) {
	result := candidateAllowSemanticRouter().result
	result.Categories = []string{"reverse_engineering"}
	result.HarmEvidence = "explicit"
	result.Intent = "defensive"
	result.Target = "self_owned"
	result.Authorization = "authorized"
	final, err := applyFinalSemanticReviewPolicy(result)
	require.NoError(t, err)
	require.Equal(t, "allow", final.Verdict)

	result.Verdict = "reject"
	result.Intent = "harmful"
	result.Authorization = "unauthorized"
	result.Target = "third_party"
	result.HarmMechanism = "credential_theft"
	result.Operationality = "actionable"
	result.Executability = "direct"
	final, err = applyFinalSemanticReviewPolicy(result)
	require.NoError(t, err)
	require.Equal(t, "reject", final.Verdict)
}

func TestCandidateSecondAuditHasOnlyTerminalModelDecisions(t *testing.T) {
	for _, verdict := range []string{"allow", "reject"} {
		t.Run(verdict, func(t *testing.T) {
			cfg := escalationCandidateConfig()
			selection := escalationCandidateSelection(cfg, true)
			final := candidateAllowSemanticRouter().result
			final.Verdict = verdict
			final.ReasonCodes = []string{"final_inconclusive"}
			router := &semanticReviewSequenceRouter{results: []ContentModerationSemanticReviewResult{
				semanticReviewResultForEscalation("review", "ambiguous", "unclear", 0.6), final,
			}}
			repo := &contentModerationTestRepo{}
			svc := candidateTestService(repo)
			svc.SetSemanticReviewRouter(router)
			outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, selection, "")
			require.Len(t, router.inputs, 2)
			require.False(t, router.inputs[0].FinalReview)
			require.True(t, router.inputs[1].FinalReview)
			require.Equal(t, contentModerationReviewKindGeneral, router.inputs[1].ReviewKind)
			require.Equal(t, verdict == "reject", outcome.Decision.Blocked)
			logs := repo.snapshotLogs()
			require.Len(t, logs, 1)
			require.Empty(t, logs[0].ReviewStatus)
			require.False(t, logs[0].UserViolationEligible)
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(logs[0].Metadata, &metadata))
			require.Equal(t, verdict, metadata["semantic_review_verdict"])
		})
	}
}

func TestFinalSemanticReviewRejectsNonTerminalUpstreamOutput(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"output_text":"{\"verdict\":\"review\"}"}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
	_, err := svc.ReviewSemanticContent(context.Background(), &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
	}, "review-model", ContentModerationSemanticReviewInput{Text: "classify this request", FinalReview: true})
	require.ErrorContains(t, err, "non-terminal verdict")
	var body map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
	format := body["text"].(map[string]any)["format"].(map[string]any)
	require.Equal(t, "semantic_review_final_v1", format["name"])
}

func TestFirstReviewVerdictAlwaysInvokesFinalModel(t *testing.T) {
	cfg := escalationCandidateConfig()
	selection := escalationCandidateSelection(cfg, false)
	selection.Source.Text = selection.Fragment
	selection.ReviewText = selection.Fragment
	selection.EvidenceComplete = true
	router := &semanticReviewSequenceRouter{results: []ContentModerationSemanticReviewResult{
		semanticReviewResultForEscalation("review", "harmful", "unauthorized", 0.99),
		candidateAllowSemanticRouter().result,
	}}
	svc := candidateTestService(&contentModerationTestRepo{})
	svc.SetSemanticReviewRouter(router)
	outcome := svc.runCandidateSemanticReview(context.Background(), ContentModerationCheckInput{UserID: 1}, cfg, selection, "")
	require.Len(t, router.inputs, 2)
	require.True(t, router.inputs[1].FinalReview)
	require.True(t, outcome.Decision.Allowed)
}

func TestSemanticReviewProviderFailureDoesNotBlockObserveMode(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModeObserve
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	svc := candidateTestService(&contentModerationTestRepo{})
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{Verdict: "review"}})
	decision, handled := svc.semanticReviewProviderFallback(context.Background(), ContentModerationCheckInput{}, cfg,
		ContentModerationInput{Text: "inspect a request"}, "input-hash", "", nil, false)
	require.True(t, handled)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
}

func TestFinalReviewEvidenceKeepsRestrictedIntentBeyondInputLimit(t *testing.T) {
	cfg := escalationCandidateConfig()
	cfg.SemanticReview.EscalationMaxInputRunes = 2000
	text := "outer task start " + strings.Repeat("ordinary context ", 1000) +
		"research virology and reverse engineering" + strings.Repeat(" later context", 600) + " final task end"
	selection := contentModerationCandidateSelectionFromRule(cfg, ContentModerationInputSource{
		Source: "responses.input", Role: "user", Text: text,
	}, contentModerationSourceOriginUserTurn, ContentModerationKeywordRule{
		Keyword: "virology", Category: ContentModerationKeywordCategoryBiosecurity, Enabled: true,
	}, contentModerationCandidateKindKeyword)
	input := contentModerationCandidateEscalationInput(cfg, selection, ContentModerationSemanticReviewInput{})
	require.LessOrEqual(t, len([]rune(input.Text)), cfg.SemanticReview.EscalationMaxInputRunes)
	require.True(t, json.Valid([]byte(input.Text)))
	require.Contains(t, input.Text, "outer task start")
	require.Contains(t, input.Text, "virology")
	require.Contains(t, input.Text, "reverse engineering")
	require.Contains(t, input.Text, "final task end")
	require.False(t, input.EvidenceComplete)
}

func TestFinalReviewRejectsModelIdentifiedUnauthorizedCyberIntent(t *testing.T) {
	result := semanticReviewResultForEscalation("allow", "harmful", "unauthorized", 0.99)
	final, err := applyFinalSemanticReviewPolicy(result)
	require.NoError(t, err)
	require.Equal(t, "reject", final.Verdict)
}
