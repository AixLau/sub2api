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

func TestContentModerationCheckErrorDecisionAllowsRequest(t *testing.T) {
	decision := contentModerationCheckErrorDecision()

	require.NotNil(t, decision)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.False(t, decision.Flagged)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
	require.Empty(t, decision.Message)
}

func TestOpenAIContentModerationNilServiceFailsOpen(t *testing.T) {
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
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)
	require.Zero(t, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}
