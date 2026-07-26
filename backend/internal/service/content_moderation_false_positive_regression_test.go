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
			name: "authorized private external service operation is allowed",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "benign", Target: "external_service", Authorization: "authorized",
				InformationAccess: "private", HarmMechanism: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"other"},
			},
			wantVerdict: "allow",
		},
		{
			name: "harmless operational review with unclear authorization is allowed",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "defensive", Target: "external_service", Authorization: "unclear",
				InformationAccess: "restricted", HarmMechanism: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"other"},
			},
			wantVerdict: "allow",
		},
		{
			// Prompt v5 authorized-scope exemption: an allow for authorized
			// CTF/lab work may carry a non-none harm mechanism and must not be
			// flipped to review as allow_inconsistent.
			name: "authorized lab exploit delivery allow is preserved",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "defensive", Target: "authorized_lab", Authorization: "authorized",
				InformationAccess: "provided_by_user", HarmMechanism: "exploit_delivery", Operationality: "actionable",
				Executability: "direct", Categories: []string{"cyber"},
			},
			wantVerdict: "allow",
		},
		{
			name: "self owned target with credential theft mechanism allow is preserved",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "benign", Target: "self_owned", Authorization: "authorized",
				InformationAccess: "provided_by_user", HarmMechanism: "credential_theft", Operationality: "actionable",
				Executability: "direct", Categories: []string{"cyber"},
			},
			wantVerdict: "allow",
		},
		{
			name: "explicit unauthorized signal remains reviewable",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "benign", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "restricted", HarmMechanism: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"unauthorized_access"},
			},
			wantVerdict: "review",
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
		{
			name: "benign form automation with inferred deception is allowed",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "benign", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "inferred", DeceptionType: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"cyber"},
			},
			wantVerdict: "allow",
		},
		{
			name: "ambiguous form automation with inferred deception stays reviewable",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "ambiguous", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "inferred", DeceptionType: "none", Operationality: "actionable",
				Executability: "direct", Categories: []string{"cyber"},
			},
			wantVerdict: "review",
		},
		{
			name: "legacy fraud result without evidence fields remains reviewable",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "ambiguous", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict: "review",
		},
		{
			name: "explicit deception remains reviewable when intent is ambiguous",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "ambiguous", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "explicit", DeceptionType: "unauthorized_submission",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict: "review",
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
