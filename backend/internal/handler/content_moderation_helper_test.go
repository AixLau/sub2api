package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

func TestOpenAIContentModerationNilServiceFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	decision := (&OpenAIGatewayHandler{}).checkContentModeration(
		c,
		zap.NewNop(),
		nil,
		middleware2.AuthSubject{UserID: 42},
		service.ContentModerationProtocolOpenAIChat,
		"gpt-test",
		[]byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	)

	require.NotNil(t, decision)
	require.True(t, decision.Blocked)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}
