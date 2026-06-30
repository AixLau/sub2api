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

func TestOpenAIGatewayPipelineCheckModerationCallsGuardWithInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expectedDecision := &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}
	guard := &openAIGatewayPipelineModerationGuardSpy{decision: expectedDecision}
	pipeline := newOpenAIGatewayPipeline(guard)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	apiKey := &service.APIKey{ID: 7, UserID: 42, Name: "gateway-key"}
	input := moderationGuardInput{
		APIKey:   apiKey,
		Subject:  middleware2.AuthSubject{UserID: 42, Concurrency: 3},
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","input":"hello"}`),
	}
	reqLog := zap.NewNop()

	decision := pipeline.CheckModeration(c, reqLog, input)

	require.Same(t, expectedDecision, decision)
	require.Len(t, guard.calls, 1)
	require.Same(t, c, guard.contexts[0])
	require.Same(t, reqLog, guard.loggers[0])
	require.Same(t, apiKey, guard.calls[0].APIKey)
	require.Equal(t, input.Subject, guard.calls[0].Subject)
	require.Equal(t, input.Protocol, guard.calls[0].Protocol)
	require.Equal(t, input.Model, guard.calls[0].Model)
	require.Equal(t, input.Body, guard.calls[0].Body)
}

func TestOpenAIGatewayPipelineCheckModerationReturnsBlockedDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	blockedDecision := &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by moderation",
		Action:     service.ContentModerationActionBlock,
	}
	guard := &openAIGatewayPipelineModerationGuardSpy{decision: blockedDecision}
	pipeline := newOpenAIGatewayPipeline(guard)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	decision := pipeline.CheckModeration(c, zap.NewNop(), moderationGuardInput{
		Protocol: service.ContentModerationProtocolOpenAIChat,
		Model:    "gpt-5.1",
		Body:     []byte(`{"messages":[{"role":"user","content":"risk"}]}`),
	})

	require.Same(t, blockedDecision, decision)
	require.True(t, decision.Blocked)
	require.Equal(t, service.ContentModerationActionBlock, decision.Action)
	require.Len(t, guard.calls, 1)
}

func TestOpenAIGatewayPipelineCheckModerationNilGuardFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pipeline := newOpenAIGatewayPipeline(nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	require.NotPanics(t, func() {
		decision := pipeline.CheckModeration(c, zap.NewNop(), moderationGuardInput{
			Protocol: service.ContentModerationProtocolOpenAIResponses,
			Model:    "gpt-5.1",
			Body:     []byte(`{"input":"hello"}`),
		})

		require.NotNil(t, decision)
		require.True(t, decision.Blocked)
		require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
		require.Equal(t, service.ContentModerationActionError, decision.Action)
	})
}

type openAIGatewayPipelineModerationGuardSpy struct {
	decision *service.ContentModerationDecision
	contexts []*gin.Context
	loggers  []*zap.Logger
	calls    []moderationGuardInput
}

func (s *openAIGatewayPipelineModerationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.contexts = append(s.contexts, c)
	s.loggers = append(s.loggers, reqLog)
	s.calls = append(s.calls, input)
	return s.decision
}
