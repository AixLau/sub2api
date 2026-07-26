package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeSemanticReviewTaxonomyBoundsLabels(t *testing.T) {
	result := normalizeSemanticReviewTaxonomy(ContentModerationSemanticReviewResult{
		Categories:  []string{" Financial—Research ", "credential attack", "credential_attack", "free form model explanation", "ＪＡＩＬＢＲＥＡＫ"},
		ReasonCodes: []string{"no authorization", "public_market_data", "a long prose explanation", "a long prose explanation"},
	})
	require.Equal(t, []string{"benign_context", "credential_theft", "other", "jailbreak"}, result.Categories)
	require.Equal(t, []string{"unauthorized_access", "benign_context", "model_reason_other"}, result.ReasonCodes)
	require.Equal(t, []string{"no authorization", "public_market_data", "a long prose explanation"}, result.ReasonDetails)
}

func TestNormalizeSemanticReviewTaxonomyEmptyAndUnknown(t *testing.T) {
	result := normalizeSemanticReviewTaxonomy(ContentModerationSemanticReviewResult{
		Categories:  []string{" ", "unknown-category"},
		ReasonCodes: []string{"", "未知原因"},
	})
	require.Equal(t, []string{"other"}, result.Categories)
	require.Equal(t, []string{"model_reason_other"}, result.ReasonCodes)
	require.Equal(t, []string{"未知原因"}, result.ReasonDetails)
}

func TestNormalizeSemanticReviewTaxonomyKeepsDetailsAcrossRepeatedNormalization(t *testing.T) {
	result := normalizeSemanticReviewResult(ContentModerationSemanticReviewResult{
		Categories:  []string{"prompt_policy_override_attempt", "benign_debugging"},
		ReasonCodes: []string{"Free Form Explanation"},
	})
	result = normalizeSemanticReviewResult(result)

	require.Equal(t, []string{"jailbreak", "benign_context"}, result.Categories)
	require.Equal(t, []string{"model_reason_other"}, result.ReasonCodes)
	require.Equal(t, []string{"Free Form Explanation"}, result.ReasonDetails)
}

func TestCanonicalSemanticReviewLabelNormalizesUnicodeAndSeparators(t *testing.T) {
	require.Equal(t, "credential_theft", canonicalSemanticReviewCategory("ＣＲＥＤＥＮＴＩＡＬ—ＴＨＥＦＴ"))
	require.Equal(t, "market_manipulation", canonicalSemanticReviewReasonCode("market/manipulation"))
	require.Equal(t, "semantic_policy_harmless_review", canonicalSemanticReviewReasonCode("semantic_policy_harmless_review"))
	require.Equal(t, "semantic_policy_unsubstantiated_fraud", canonicalSemanticReviewReasonCode("semantic_policy_unsubstantiated_fraud"))
	// ambiguous_context is the reason code the prompt instructs the model to
	// emit for review verdicts; it must survive normalization unchanged.
	require.Equal(t, "ambiguous_context", canonicalSemanticReviewReasonCode("ambiguous_context"))
}
