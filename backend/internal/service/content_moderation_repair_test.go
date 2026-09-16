package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationUsageAllowsCustomAPIWithoutAccount(t *testing.T) {
	require.NoError(t, (&UsageLog{Source: UsageSourceContentModeration}).ValidateActors())
	require.Error(t, (&UsageLog{Source: UsageSourceContentModeration, AccountID: -1}).ValidateActors())
	require.Error(t, (&UsageLog{Source: UsageSourceContentModeration, UserID: 1}).ValidateActors())
}

func TestSemanticReviewEscalationUsesOriginalEvidence(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview.EscalationMaxInputRunes = 1000
	// A provider-fallback candidate carries no matched keyword: the technical
	// marker is internal routing state, not user evidence. The rebuilt escalation
	// input must still be the real request, and none of the old marker vocabulary
	// may leak back in through the keyword slot.
	candidate := contentModerationSemanticGateCandidate{ProviderFallback: true}
	content := ContentModerationInput{Text: "请分析这个普通请求", Sources: []ContentModerationInputSource{{Source: "responses.input", Role: "user", Text: "请分析这个普通请求"}}}
	input := contentModerationSemanticGateEscalationInput(ContentModerationCheckInput{}, cfg, content, candidate, ContentModerationSemanticReviewInput{})
	require.Contains(t, input.Text, content.Text)
	require.Empty(t, candidate.Keyword)
	require.NotContains(t, input.Text, "provider_unavailable")
	require.NotContains(t, input.Text, "semantic_fallback")
}

func TestSemanticOutboxDoesNotRetryDeterministicResponseErrors(t *testing.T) {
	require.True(t, isDeterministicSemanticReviewFailure(errors.New("semantic review models are unavailable: 模型返回内容无法解析")))
	require.True(t, isDeterministicSemanticReviewFailure(errors.New("模型输出达到上限（512 tokens）")))
	require.False(t, isDeterministicSemanticReviewFailure(errors.New("主模型请求超时")))
}
