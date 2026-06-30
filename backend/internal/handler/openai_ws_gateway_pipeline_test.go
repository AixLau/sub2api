package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIWebSocketPipelineRunsInitialFrameStagesInOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var events []string
	pipeline := &OpenAIGatewayPipeline{
		wsInitialFrameStages: []openAIWebSocketGatewayStage{
			openAIWebSocketGatewayPipelineTestStage{
				name: "moderation",
				run: func(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
					events = append(events, "moderation:"+ctx.input.Model)
					return openAIWebSocketGatewayStageResult{}
				},
			},
			openAIWebSocketGatewayPipelineTestStage{
				name: "image_permission",
				run: func(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
					events = append(events, "image:"+string(ctx.input.Body))
					return openAIWebSocketGatewayStageResult{}
				},
			},
			openAIWebSocketGatewayPipelineTestStage{
				name: "cyber",
				run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
					events = append(events, "cyber")
					return openAIWebSocketGatewayStageResult{
						Result: openAIWebSocketPipelineResult{
							Blocked:       true,
							BlockReason:   openAIWebSocketPipelineBlockReasonCyberSession,
							Message:       cyberSessionBlockedClientMsg,
							CyberBlockKey: "ws-session-key",
						},
					}
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)

	result := pipeline.RunWebSocketInitialFrame(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIWebSocketPipelineInput{
		APIKey:  &service.APIKey{ID: 7},
		Model:   "gpt-5.1",
		Body:    []byte(`{"model":"gpt-5.1","input":"hello"}`),
		Subject: middleware.AuthSubject{UserID: 42, Concurrency: 1},
	})

	require.True(t, result.Blocked)
	require.Equal(t, openAIWebSocketPipelineBlockReasonCyberSession, result.BlockReason)
	require.Equal(t, "ws-session-key", result.CyberBlockKey)
	require.Equal(t, []string{
		"moderation:gpt-5.1",
		`image:{"model":"gpt-5.1","input":"hello"}`,
		"cyber",
	}, events)
	_, ok := moderationcoverage.PipelineAdmissionFromContext(c)
	require.False(t, ok)
}

func TestOpenAIWebSocketPipelineMarksAdmissionWhenStagesAllow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		source    string
		run       func(*OpenAIGatewayPipeline, *OpenAIGatewayHandler, *gin.Context, *zap.Logger, openAIWebSocketPipelineInput) openAIWebSocketPipelineResult
		configure func(*OpenAIGatewayPipeline)
	}{
		{
			name:   "initial frame",
			source: moderationcoverage.SourceOpenAIWebSocketInitialFrame,
			run:    (*OpenAIGatewayPipeline).RunWebSocketInitialFrame,
			configure: func(p *OpenAIGatewayPipeline) {
				p.wsInitialFrameStages = []openAIWebSocketGatewayStage{
					openAIWebSocketGatewayPipelineTestStage{name: "moderation", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
						return openAIWebSocketGatewayStageResult{}
					}},
				}
			},
		},
		{
			name:   "followup frame",
			source: moderationcoverage.SourceOpenAIWebSocketFollowupFrame,
			run:    (*OpenAIGatewayPipeline).RunWebSocketFollowupFrame,
			configure: func(p *OpenAIGatewayPipeline) {
				p.wsFollowupFrameStages = []openAIWebSocketGatewayStage{
					openAIWebSocketGatewayPipelineTestStage{name: "followup_moderation", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
						return openAIWebSocketGatewayStageResult{}
					}},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := &OpenAIGatewayPipeline{}
			tt.configure(pipeline)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)

			result := tt.run(pipeline, &OpenAIGatewayHandler{}, c, zap.NewNop(), openAIWebSocketPipelineInput{
				APIKey:  &service.APIKey{ID: 9, Group: &service.Group{AllowImageGeneration: true}},
				Model:   "gpt-5.1",
				Body:    []byte(`{"model":"gpt-5.1","input":"hello"}`),
				Subject: middleware.AuthSubject{UserID: 44, Concurrency: 1},
			})

			require.False(t, result.Blocked)
			admission, ok := moderationcoverage.PipelineAdmissionFromContext(c)
			require.True(t, ok)
			require.True(t, admission.Admitted)
			require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, admission.Pipeline)
			require.Equal(t, moderationcoverage.StagePreForward, admission.Stage)
			require.Equal(t, tt.source, admission.Source)
			require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
		})
	}
}

func TestOpenAIWebSocketPipelineRunsFollowupFrameStages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var calls []openAIWebSocketPipelineInput
	pipeline := &OpenAIGatewayPipeline{
		wsFollowupFrameStages: []openAIWebSocketGatewayStage{
			openAIWebSocketGatewayPipelineTestStage{
				name: "followup_moderation",
				run: func(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
					calls = append(calls, ctx.input)
					return openAIWebSocketGatewayStageResult{
						Result: openAIWebSocketPipelineResult{
							Blocked:     true,
							BlockReason: openAIWebSocketPipelineBlockReasonModeration,
							ModerationDecision: &service.ContentModerationDecision{
								Blocked:    true,
								StatusCode: http.StatusForbidden,
								Message:    "followup blocked by pipeline",
								Action:     service.ContentModerationActionBlock,
							},
						},
					}
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	body := []byte(`{"type":"response.create","model":"gpt-5.1","input":"risk"}`)

	result := pipeline.RunWebSocketFollowupFrame(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIWebSocketPipelineInput{
		APIKey:  &service.APIKey{ID: 8},
		Model:   "gpt-5.1",
		Body:    body,
		Subject: middleware.AuthSubject{UserID: 43, Concurrency: 1},
	})

	require.True(t, result.Blocked)
	require.Equal(t, openAIWebSocketPipelineBlockReasonModeration, result.BlockReason)
	require.NotNil(t, result.ModerationDecision)
	require.Equal(t, "followup blocked by pipeline", result.ModerationDecision.Message)
	require.Len(t, calls, 1)
	require.Equal(t, body, calls[0].Body)
	require.Equal(t, "gpt-5.1", calls[0].Model)
}

func TestOpenAIResponsesWebSocketInitialFrameUsesPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stage := &openAIWebSocketGatewayPipelineBlockingStage{
		reason:  openAIWebSocketPipelineBlockReasonImagePermission,
		message: "blocked by injected websocket pipeline",
	}
	h := &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{}), SSEPingFormatNone, time.Second),
		pipeline: &OpenAIGatewayPipeline{
			wsInitialFrameStages: []openAIWebSocketGatewayStage{stage},
		},
	}
	wsServer := newOpenAIWSHandlerTestServer(t, h, middleware.AuthSubject{UserID: 1, Concurrency: 1})
	defer wsServer.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(wsServer.URL, "http")+"/openai/v1/responses", nil)
	cancelDial()
	require.NoError(t, err)
	defer func() {
		_ = clientConn.CloseNow()
	}()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":false}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 3*time.Second)
	_, _, err = clientConn.Read(readCtx)
	cancelRead()
	require.Error(t, err)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	require.Contains(t, closeErr.Reason, "blocked by injected websocket pipeline")
	require.Len(t, stage.calls, 1)
	require.Equal(t, "gpt-5.1", stage.calls[0].Model)
	require.Contains(t, string(stage.calls[0].Body), `"model":"gpt-5.1"`)
}

type openAIWebSocketGatewayPipelineTestStage struct {
	name string
	run  func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult
}

func (s openAIWebSocketGatewayPipelineTestStage) Name() string {
	return s.name
}

func (s openAIWebSocketGatewayPipelineTestStage) Run(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
	return s.run(ctx)
}

type openAIWebSocketGatewayPipelineBlockingStage struct {
	reason  openAIWebSocketPipelineBlockReason
	message string
	calls   []openAIWebSocketPipelineInput
}

func (s *openAIWebSocketGatewayPipelineBlockingStage) Name() string {
	return "blocking_test_stage"
}

func (s *openAIWebSocketGatewayPipelineBlockingStage) Run(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
	s.calls = append(s.calls, ctx.input)
	return openAIWebSocketGatewayStageResult{
		Result: openAIWebSocketPipelineResult{
			Blocked:     true,
			BlockReason: s.reason,
			Message:     s.message,
		},
	}
}
