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
	candidate := contentModerationSemanticGateCandidate{Keyword: "provider_unavailable", Category: "semantic_fallback"}
	content := ContentModerationInput{Text: "请分析这个普通请求", Sources: []ContentModerationInputSource{{Source: "responses.input", Role: "user", Text: "请分析这个普通请求"}}}
	input := contentModerationSemanticGateEscalationInput(ContentModerationCheckInput{}, cfg, content, candidate, ContentModerationSemanticReviewInput{})
	require.Contains(t, input.Text, content.Text)
	require.NotContains(t, input.Text, candidate.Keyword)
}

func TestSemanticOutboxDoesNotRetryDeterministicResponseErrors(t *testing.T) {
	require.True(t, isDeterministicSemanticReviewFailure(errors.New("semantic review models are unavailable: 模型返回内容无法解析")))
	require.True(t, isDeterministicSemanticReviewFailure(errors.New("模型输出达到上限（512 tokens）")))
	require.False(t, isDeterministicSemanticReviewFailure(errors.New("主模型请求超时")))
}
