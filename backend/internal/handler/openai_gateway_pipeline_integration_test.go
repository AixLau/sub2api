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

func TestOpenAIGatewayHandlerCheckWithModerationGuardUsesPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pipelineDecision := &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by pipeline",
		Action:     service.ContentModerationActionBlock,
	}
	pipelineGuard := &openAIGatewayPipelineIntegrationGuardSpy{decision: pipelineDecision}
	handlerGuard := &openAIGatewayPipelineIntegrationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	input := moderationGuardInput{
		Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","input":"pipeline-risk"}`),
	}
	reqLog := zap.NewNop()
	h := &OpenAIGatewayHandler{
		moderationGuard: handlerGuard,
		pipeline:        newOpenAIGatewayPipeline(pipelineGuard),
	}

	decision := h.checkWithModerationGuard(c, reqLog, input)

	require.Same(t, pipelineDecision, decision)
	require.Len(t, pipelineGuard.calls, 1)
	require.Equal(t, input, pipelineGuard.calls[0])
	require.Empty(t, handlerGuard.calls)
}

func TestOpenAIGatewayHandlerCheckWithModerationGuardNilPipelineFallsBackToHandlerGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expectedDecision := &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by fallback guard",
		Action:     service.ContentModerationActionBlock,
	}
	guard := &openAIGatewayPipelineIntegrationGuardSpy{decision: expectedDecision}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	input := moderationGuardInput{
		Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
		Protocol: service.ContentModerationProtocolOpenAIChat,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","messages":[{"role":"user","content":"risk"}]}`),
	}
	h := &OpenAIGatewayHandler{moderationGuard: guard}

	var decision *service.ContentModerationDecision
	require.NotPanics(t, func() {
		decision = h.checkWithModerationGuard(c, zap.NewNop(), input)
	})

	require.Same(t, expectedDecision, decision)
	require.Len(t, guard.calls, 1)
	require.Equal(t, input, guard.calls[0])
}

func TestOpenAIGatewayHandlerCheckWithModerationGuardNilPipelineFailsClosedWithoutGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	h := &OpenAIGatewayHandler{}

	var decision *service.ContentModerationDecision
	require.NotPanics(t, func() {
		decision = h.checkWithModerationGuard(c, zap.NewNop(), moderationGuardInput{
			Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
			Protocol: service.ContentModerationProtocolOpenAIEmbeddings,
			Model:    "text-embedding-3-small",
			Body:     []byte(`{"model":"text-embedding-3-small","input":"hello"}`),
		})
	})

	require.NotNil(t, decision)
	require.True(t, decision.Blocked)
	require.False(t, decision.Allowed)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}

type openAIGatewayPipelineIntegrationGuardSpy struct {
	decision *service.ContentModerationDecision
	calls    []moderationGuardInput
}

func (s *openAIGatewayPipelineIntegrationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.calls = append(s.calls, input)
	return s.decision
}
