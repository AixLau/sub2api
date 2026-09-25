package handler

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type upstreamAuditCounter struct {
	calls    int
	protocol string
}

func (a *upstreamAuditCounter) Check(_ context.Context, req securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	a.calls++
	a.protocol = req.Protocol
	return &securityaudit.LegacyDecision{Allowed: true}, nil
}
func TestUpstreamContentAuditHTTPChecksOnceBeforeForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	engine := &upstreamAuditCounter{}
	h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(engine, nil)}
	input := openAIHTTPPreForwardPipelineInput{Subject: middleware2.AuthSubject{UserID: 1}, Protocol: GatewayProtocolOpenAIMessages, Model: "test", Body: []byte("{}")}
	result := (OpenAIHTTPModerationStage{}).Run(&openAIHTTPGatewayStageContext{handler: h, c: c, input: input})
	require.False(t, result.Blocked)
	require.Nil(t, h.checkSecurityAudit(c, nil, nil, input.Subject, input.Protocol, input.Model, input.Body))
	require.Equal(t, 1, engine.calls)
	require.Equal(t, service.ContentModerationProtocolAnthropicMessages, engine.protocol)
}
func TestUpstreamContentAuditWebSocketAuditsEachTurnOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	engine := &upstreamAuditCounter{}
	h := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(engine, nil)}
	input := openAIWebSocketPipelineInput{Subject: middleware2.AuthSubject{UserID: 1}, Protocol: service.ContentModerationProtocolOpenAIResponses, Model: "test", Body: []byte("{}")}
	stageContext := &openAIWebSocketGatewayStageContext{handler: h, c: c, input: input}
	for turn := 1; turn <= 2; turn++ {
		c.Set(securityAuditWSTurnContextKey, turn)
		first := (OpenAIWebSocketModerationStage{}).Run(stageContext)
		repeated := (OpenAIWebSocketModerationStage{}).Run(stageContext)
		require.False(t, first.Result.Blocked)
		require.False(t, repeated.Result.Blocked)
		require.Equal(t, turn, engine.calls)
	}
}
