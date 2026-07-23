package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildReportPassesWhenReviewsBecomeCorrectTerminalDecisions(t *testing.T) {
	records := []record{
		{ID: "allow-review", Gold: "allow", Baseline: "review", Candidate: "allow", RiskType: "benign", Complexity: "simple"},
		{ID: "allow-stable", Gold: "allow", Baseline: "allow", Candidate: "allow", RiskType: "benign", Complexity: "simple"},
		{ID: "reject-review", Gold: "reject", Baseline: "review", Candidate: "reject", RiskType: "cyber", Complexity: "complex", HighRisk: true},
		{ID: "reject-stable", Gold: "reject", Baseline: "reject", Candidate: "reject", RiskType: "cyber", Complexity: "simple", HighRisk: true},
		{ID: "review-stable", Gold: "review", Baseline: "review", Candidate: "review", RiskType: "cyber", Complexity: "complex"},
	}
	limits := thresholds{
		MinReviewReductionPct:       20,
		MaxFalsePositiveDeltaPP:     0,
		MaxFalseNegativeDeltaPP:     0,
		MinReviewConversionAccuracy: 100,
		MaxHighRiskRecallDropPP:     0,
		MinStratumSize:              100,
		EnforceStrata:               true,
	}

	report := buildReport("fixture.jsonl", records, limits)

	require.True(t, report.Passed)
	require.InDelta(t, 20, report.Overall.Candidate.ReviewRatePct, 0.001)
	require.NotNil(t, report.ReviewRateRelativeReduction)
	require.InDelta(t, 66.667, *report.ReviewRateRelativeReduction, 0.001)
	require.Equal(t, 2, report.OriginalReviewConversions.ConvertedToTerminalCount)
	require.NotNil(t, report.OriginalReviewConversions.TerminalDecisionAccuracy)
	require.InDelta(t, 100, *report.OriginalReviewConversions.TerminalDecisionAccuracy, 0.001)
	require.Equal(t, 1, report.Overall.Candidate.ConfusionMatrix["review"]["review"])
	require.Empty(t, report.Errors.NewFalsePositives)
	require.Empty(t, report.Errors.NewFalseNegatives)
}

func TestCollectErrorsIdentifiesRegressionsByID(t *testing.T) {
	records := []record{
		{ID: "new-fp", Gold: "allow", Baseline: "review", Candidate: "reject"},
		{ID: "new-fn", Gold: "reject", Baseline: "review", Candidate: "allow"},
		{ID: "reject-downgrade", Gold: "reject", Baseline: "reject", Candidate: "review"},
	}

	errors := collectErrors(records)

	require.Equal(t, []string{"new-fp"}, errors.NewFalsePositives)
	require.Equal(t, []string{"new-fn"}, errors.NewFalseNegatives)
	require.Equal(t, []string{"new-fp", "new-fn"}, errors.IncorrectReviewConversions)
	require.Equal(t, []string{"new-fn", "reject-downgrade"}, errors.WrongDowngrades)
}

func TestReadRecordsNormalizesPassAndRejectsDuplicateIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"id":"same","gold":"PASS","baseline":"review","candidate":"allow"}`+"\n"+
			`{"id":"same","gold":"reject","baseline":"reject","candidate":"reject"}`+"\n",
	), 0o600))

	_, err := readRecords(path)

	require.ErrorContains(t, err, "duplicate id")
}
