package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestContentModerationCheckErrorDecisionBlocksRequest(t *testing.T) {
	decision := contentModerationCheckErrorDecision()

	require.NotNil(t, decision)
	require.False(t, decision.Allowed)
	require.True(t, decision.Blocked)
	require.True(t, decision.Flagged)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
	require.Equal(t, "内容安全模块暂时不可用，请稍后重试", decision.Message)
}
