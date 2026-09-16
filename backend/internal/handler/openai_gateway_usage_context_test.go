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

type openAIWSScheduleHealthCache struct {
	accountIDs []int64
}

func (c *openAIWSScheduleHealthCache) RecordOpenAIAPIKeyHealthFailure(_ context.Context, accountID int64, _, _ int) (int64, bool, error) {
	c.accountIDs = append(c.accountIDs, accountID)
	return int64(len(c.accountIDs)), false, nil
}

func TestOpenAIWebSocketScheduleFailureDoesNotCompleteTurnOrSubmitBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, partialUsage := range []bool{false, true} {
		name := "without_usage"
		if partialUsage {
			name = "partial_usage"
		}
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method: http.MethodGet, Path: "/v1/responses",
				Handler: "OpenAIGatewayHandler.ResponsesWebSocket", Protocol: "openai_responses",
				Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
			})
			account := &service.Account{
				ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"pool_mode": true},
			}
			accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: *account}
			settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{
				service.SettingKeyOpenAIAPIKeyHealthBreakerSettings: `{"enabled":true,"window_minutes":1,"failure_threshold":3,"cooldown_minutes":5}`,
			}}, nil)
			healthCache := &openAIWSScheduleHealthCache{}
			rateLimit := service.NewRateLimitService(accountRepo, nil, nil, nil, nil)
			rateLimit.SetSettingService(settings)
			rateLimit.SetOpenAIAPIKeyHealthCache(healthCache)
			gateway := service.NewOpenAIGatewayService(nil,
				accountRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				nil, rateLimit, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
			)
			pool := newUsageRecordTestPool(t)
			h := &OpenAIGatewayHandler{gatewayService: gateway, usageRecordWorkerPool: pool}
			service.MarkOpsCyberPolicy(c, service.CyberPolicyMark{Message: "pending failover", UpstreamStatus: http.StatusBadGateway})
			c.Set(cyberPolicyRecordedKey, true)
			turnSlotsReleased := 0
			turnErr := &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}
			var stageResult ExecutableStageResult
			if partialUsage {
				success := false
				stageResult = h.runOpenAIWebSocketUsageStage(c, OpenAIWebSocketUsageStage{
					Handler: h, Account: account, Model: "gpt-5.5", TurnErr: turnErr,
					Result:           &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 12, OutputTokens: 3}},
					ScheduleSuccess:  &success,
					ReleaseTurnSlots: func() { turnSlotsReleased++ },
				})
			} else {
				stageResult = h.runOpenAIWebSocketScheduleResultStage(c, account, "gpt-5.5", turnErr)
			}
			pool.Stop()

			require.NoError(t, stageResult.Err)
			require.False(t, stageResult.Stop)
			require.Equal(t, []int64{account.ID}, healthCache.accountIDs,
				"schedule feedback must reach account health even when the failed attempt has no billable result")
			require.Zero(t, pool.Stats().SubmittedTasks, "schedule feedback has no billing identity")
			require.Zero(t, turnSlotsReleased, "transport feedback must not complete the logical turn")
			require.NotNil(t, service.GetOpsCyberPolicy(c))
			require.True(t, c.GetBool(cyberPolicyRecordedKey), "failover must retain the turn's cyber record guard")
			executions := moderationcoverage.PipelineStageExecutionsFromContext(c)
			require.Len(t, executions, 1)
			require.Equal(t, moderationcoverage.StageUsage, executions[0].Stage)
			require.False(t, c.Writer.Written())
		})
	}
}

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
