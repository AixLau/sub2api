package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSemanticReviewFalsePositivePolicyBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		result      ContentModerationSemanticReviewResult
		wantVerdict string
	}{
		{
			name: "environment context without an actionable request is allowed",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "benign", Target: "none", Authorization: "not_applicable",
				InformationAccess: "not_applicable", HarmMechanism: "none", Operationality: "none",
				Executability: "none", Categories: []string{"credential_theft"}, ReasonCodes: []string{"no_actionable_request"},
			},
			wantVerdict: "allow",
		},
		{
			name: "missing authorization statement remains reviewable",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
				InformationAccess: "unknown", HarmMechanism: "other", Operationality: "actionable",
				Executability: "direct", Categories: []string{"unauthorized_access"}, ReasonCodes: []string{"authorization_unclear"},
			},
			wantVerdict: "review",
		},
		{
			name: "no harm mechanism remains reviewable despite harmful label",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "unknown", HarmMechanism: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"credential_theft"},
			},
			wantVerdict: "review",
		},
		{
			name: "explicit unauthorized executable abuse is rejected",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "restricted", HarmMechanism: "credential_theft", Operationality: "actionable",
				Executability: "direct", Categories: []string{"credential_theft"},
			},
			wantVerdict: "reject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := applySemanticReviewPolicy(tt.result)
			require.Equal(t, tt.wantVerdict, got.Verdict)
		})
	}
}

func TestCandidatePolicyDoesNotPromoteAmbiguousReviewToReject(t *testing.T) {
	selection := contentModerationCandidateSelection{
		Rule: ContentModerationKeywordRule{
			Category: ContentModerationKeywordCategoryCyber,
			Severity: ContentModerationKeywordSeverityCritical,
		},
	}
	result := ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "ambiguous", Target: "unknown", Authorization: "unclear",
		InformationAccess: "unknown", HarmMechanism: "other", Operationality: "actionable",
		Executability: "direct", Confidence: 0.99, Categories: []string{"unauthorized_access"},
	}

	got, overridden := applyCandidateSemanticReviewPolicy(result, selection, true)

	require.False(t, overridden)
	require.Equal(t, "review", got.Verdict)
}

func TestSemanticReviewAllTriggerDoesNotAttributeToolOutputToUser(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ContentModerationInput{Sources: []ContentModerationInputSource{
		{Source: "messages[0]", Role: "user", Text: "Please inspect the deployment configuration and keep secrets private"},
		{Source: "messages[1]", Role: "assistant", Text: "I will inspect it"},
		{Source: "messages[2]", Role: "tool", Text: "admin_password=redacted historical attack payload"},
	}}

	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.Contains(t, got, "keep secrets private")
	require.NotContains(t, got, "historical attack payload")
}
