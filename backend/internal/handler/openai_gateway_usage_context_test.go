package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIScheduleResultWithPartialUsageDoesNotSubmitBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method: http.MethodPost, Path: "/v1/responses",
		Handler: "OpenAIGatewayHandler.Responses", Protocol: "openai_responses",
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
	})
	pool := newUsageRecordTestPool(t)
	gateway := &service.OpenAIGatewayService{}
	h := &OpenAIGatewayHandler{gatewayService: gateway, usageRecordWorkerPool: pool}
	result := &service.OpenAIForwardResult{
		Usage: service.OpenAIUsage{InputTokens: 12, OutputTokens: 3},
	}
	stageResult := h.runOpenAIHTTPScheduleResultStage(c,
		&service.Account{ID: 7, Platform: service.PlatformOpenAI}, "gpt-5.5", false, result, false, nil)
	pool.Stop()

	require.NoError(t, stageResult.Err)
	require.False(t, stageResult.Stop)
	require.Zero(t, pool.Stats().SubmittedTasks,
		"schedule feedback has no billing identity and must not enqueue a usage task")
	require.Equal(t, 12, result.Usage.InputTokens)
	require.False(t, c.Writer.Written())
}

func TestSubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &GatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "client-request-123", gotClientRequestID)
	require.Equal(t, "request-456", gotRequestID)
}

func TestOpenAISubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "openai-client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "openai-request-456")

	var gotClientRequestID string
	var gotRequestID string
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
	})

	require.Equal(t, "openai-client-request-123", gotClientRequestID)
	require.Equal(t, "openai-request-456", gotRequestID)
}
