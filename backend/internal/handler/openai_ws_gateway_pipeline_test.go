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

func TestOpenAIResponsesWebSocketRequiresGatewayPipelineRegistrarEntrypoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Connection", "Upgrade")
	c.Request.Header.Set("Upgrade", "websocket")
	c.Request.Header.Set("Sec-WebSocket-Version", "13")
	c.Request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	setGatewayAuthContextForModerationTest(c)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodGet,
		Path:               "/v1/responses",
		Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIResponses,
		Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
	})

	h := &OpenAIGatewayHandler{}

	h.ResponsesWebSocket(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Contains(t, w.Body.String(), "OpenAI WebSocket pipeline entrypoint missing")
}

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
							Message:       cyberSessionBlockedClientMessage(service.PlatformOpenAI),
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

func TestOpenAIWebSocketDefaultPipelineStagesMatchCoverageMetadata(t *testing.T) {
	pipeline := newOpenAIGatewayPipeline(nil)
	input := openAIWebSocketPipelineInput{
		Protocol:      service.ContentModerationProtocolOpenAIResponses,
		ImageEndpoint: "/v1/responses",
	}

	initialStageNames := openAIWebSocketGatewayStageNames(pipeline.webSocketInitialFramePipelineStages(input))
	followupStageNames := openAIWebSocketGatewayStageNames(pipeline.webSocketFollowupFramePipelineStages(input))
	metadataStages := moderationcoverage.OpenAIWebSocketPipelineStagesForRoute(
		"OpenAIGatewayHandler.ResponsesWebSocket",
		service.ContentModerationProtocolOpenAIResponses,
	)

	require.Equal(t, []string{"moderation", "image_permission", "cyber"}, initialStageNames)
	require.Equal(t, []string{"moderation"}, followupStageNames)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageModeration)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageImage)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageCyber)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StagePreForward)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageBilling)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageRouting)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageForward)
	requireOpenAIWebSocketMetadataStageCovered(t, metadataStages, moderationcoverage.StageUsage)
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
					openAIWebSocketGatewayPipelineTestStage{name: "moderation", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
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

func TestOpenAIWebSocketDeferredReviewUsesRetryableCloseStatus(t *testing.T) {
	result := openAIWebSocketPipelineResult{
		Blocked:     true,
		BlockReason: openAIWebSocketPipelineBlockReasonModeration,
		ModerationDecision: &service.ContentModerationDecision{
			Blocked: true,
			Action:  service.ContentModerationActionSemanticReviewDeferred,
		},
	}
	require.Equal(t, coderws.StatusTryAgainLater, openAIWebSocketPipelineCloseStatus(result))

	result.ModerationDecision.Action = service.ContentModerationActionSemanticReviewReject
	require.Equal(t, coderws.StatusPolicyViolation, openAIWebSocketPipelineCloseStatus(result))
}

func TestOpenAIWebSocketPipelineRecordsPreForwardStageExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		source     string
		run        func(*OpenAIGatewayPipeline, *OpenAIGatewayHandler, *gin.Context, *zap.Logger, openAIWebSocketPipelineInput) openAIWebSocketPipelineResult
		configure  func(*OpenAIGatewayPipeline)
		wantStages []string
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
					openAIWebSocketGatewayPipelineTestStage{name: "image_permission", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
						return openAIWebSocketGatewayStageResult{}
					}},
					openAIWebSocketGatewayPipelineTestStage{name: "cyber", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
						return openAIWebSocketGatewayStageResult{}
					}},
				}
			},
			wantStages: []string{
				moderationcoverage.StageModeration,
				moderationcoverage.StageCyber,
				moderationcoverage.StageImage,
				moderationcoverage.StagePreForward,
			},
		},
		{
			name:   "followup frame",
			source: moderationcoverage.SourceOpenAIWebSocketFollowupFrame,
			run:    (*OpenAIGatewayPipeline).RunWebSocketFollowupFrame,
			configure: func(p *OpenAIGatewayPipeline) {
				p.wsFollowupFrameStages = []openAIWebSocketGatewayStage{
					openAIWebSocketGatewayPipelineTestStage{name: "moderation", run: func(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
						return openAIWebSocketGatewayStageResult{}
					}},
				}
			},
			wantStages: []string{
				moderationcoverage.StageModeration,
				moderationcoverage.StagePreForward,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := &OpenAIGatewayPipeline{}
			tt.configure(pipeline)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:             http.MethodGet,
				Path:               "/v1/responses",
				Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           service.ContentModerationProtocolOpenAIResponses,
				Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
				Status:             moderationcoverage.StatusCovered,
			})

			result := tt.run(pipeline, &OpenAIGatewayHandler{}, c, zap.NewNop(), openAIWebSocketPipelineInput{
				APIKey:  &service.APIKey{ID: 9, Group: &service.Group{AllowImageGeneration: true}},
				Model:   "gpt-5.1",
				Body:    []byte(`{"model":"gpt-5.1","input":"hello"}`),
				Subject: middleware.AuthSubject{UserID: 44, Concurrency: 1},
			})

			require.False(t, result.Blocked)
			executions := moderationcoverage.PipelineStageExecutionsFromContext(c)
			require.Len(t, executions, len(tt.wantStages))
			for i, stage := range tt.wantStages {
				require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, executions[i].Pipeline)
				require.Equal(t, stage, executions[i].Stage)
				require.Equal(t, tt.source, executions[i].Source)
				require.Equal(t, http.MethodGet, executions[i].Method)
				require.Equal(t, "/v1/responses", executions[i].Path)
				require.Equal(t, "OpenAIGatewayHandler.ResponsesWebSocket", executions[i].Handler)
				require.Equal(t, service.ContentModerationProtocolOpenAIResponses, executions[i].Protocol)
			}
		})
	}
}

func openAIWebSocketGatewayStageNames(stages []openAIWebSocketGatewayStage) []string {
	names := make([]string, 0, len(stages))
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		names = append(names, stage.Name())
	}
	return names
}

func requireOpenAIWebSocketMetadataStageCovered(t *testing.T, stages []moderationcoverage.PipelineStageCoverage, stageName string) {
	t.Helper()
	for _, stage := range stages {
		if stage.Stage == stageName {
			require.True(t, stage.Required, "websocket metadata stage %s should be required", stageName)
			require.True(t, stage.Covered, "websocket metadata stage %s should be covered", stageName)
			return
		}
	}
	t.Fatalf("websocket metadata missing stage %s", stageName)
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
