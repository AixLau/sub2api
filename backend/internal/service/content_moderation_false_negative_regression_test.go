package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSemanticReviewFalseNegativePolicyBoundaries pins verdicts that must not
// be silently downgraded by false-positive suppression rules. Each case is a
// previously observed or reviewed leniency risk.
func TestSemanticReviewFalseNegativePolicyBoundaries(t *testing.T) {
	tests := []struct {
		name         string
		result       ContentModerationSemanticReviewResult
		wantVerdict  string
		wantOverride bool
	}{
		{
			// A model reject must never be flipped to allow by the
			// unsubstantiated-fraud rule; it falls back to review instead.
			name: "ambiguous fraud reject without explicit evidence falls back to review",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "ambiguous", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "inferred", DeceptionType: "financial_fraud",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict:  "review",
			wantOverride: true,
		},
		{
			// Explicit-evidence deception types other than financial_fraud must
			// stay reject-eligible and be promoted from review.
			name: "explicit impersonation review is promoted to reject",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "restricted", HarmMechanism: "deception_fraud",
				HarmEvidence: "explicit", DeceptionType: "impersonation",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict:  "reject",
			wantOverride: true,
		},
		{
			// Case and whitespace noise in evidence fields must not defeat the
			// explicit-evidence gate and downgrade a consistent reject.
			name: "padded mixed-case explicit financial fraud reject is preserved",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "restricted", HarmMechanism: "deception_fraud",
				HarmEvidence: " Explicit ", DeceptionType: " Financial_Fraud",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict:  "reject",
			wantOverride: false,
		},
		{
			// Out-of-vocabulary evidence values normalize to unknown, which is
			// neither none nor inferred and must not trigger the
			// unsubstantiated-fraud downgrade. Intent is benign so the case
			// exercises the unknown-evidence gate itself, not the intent gate.
			name: "unknown evidence values keep an ambiguous fraud review",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "benign", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "maybe", DeceptionType: "phishing",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict:  "review",
			wantOverride: false,
		},
		{
			// An out-of-vocabulary deception_type alone (normalized to unknown)
			// must also block the unsubstantiated-fraud downgrade even when
			// harm_evidence is a valid inferred value.
			name: "unknown deception type keeps a benign fraud review",
			result: ContentModerationSemanticReviewResult{
				Verdict: "review", Intent: "benign", Target: "external_service", Authorization: "unclear",
				InformationAccess: "provided_by_user", HarmMechanism: "deception_fraud",
				HarmEvidence: "inferred", DeceptionType: "phishing",
				Operationality: "actionable", Executability: "direct", Categories: []string{"fraud"},
			},
			wantVerdict:  "review",
			wantOverride: false,
		},
		{
			// A complete reject tuple must stay reject when the attack is merely
			// decomposed into components (executability=indirect).
			name: "indirect executability with a complete reject tuple stays reject",
			result: ContentModerationSemanticReviewResult{
				Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				InformationAccess: "restricted", HarmMechanism: "credential_theft",
				Operationality: "actionable", Executability: "indirect", Categories: []string{"credential_theft"},
			},
			wantVerdict:  "reject",
			wantOverride: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, overridden := applySemanticReviewPolicy(tt.result)
			require.Equal(t, tt.wantVerdict, got.Verdict)
			require.Equal(t, tt.wantOverride, overridden)
		})
	}
}

// TestSemanticReviewHarmlessReviewDowngradeBoundary pins the current reach of
// the harmless_review downgrade: benign intent with harm_mechanism=none is
// allowed even against a restricted third-party target with a critical
// severity and a high-risk category label.
func TestSemanticReviewHarmlessReviewDowngradeBoundary(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict: "review", Intent: "benign", Target: "third_party", Authorization: "unclear",
		InformationAccess: "restricted", HarmMechanism: "none", Severity: "critical",
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	})

	require.True(t, overridden)
	require.Equal(t, "allow", result.Verdict)
	require.Equal(t, ContentModerationKeywordSeverityLow, result.Severity)
	require.Contains(t, result.ReasonCodes, "semantic_policy_harmless_review")
}
