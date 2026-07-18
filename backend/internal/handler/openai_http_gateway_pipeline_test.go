package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIHTTPGatewayPipelineRunsStagesInOrderAndReleasesCleanupWhenLaterStageBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var events []string
	releaseCalls := 0
	pipeline := &OpenAIGatewayPipeline{
		httpPreForwardStages: []openAIHTTPGatewayStage{
			openAIHTTPGatewayPipelineTestStage{
				name: "moderation",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					events = append(events, "moderation")
					return openAIHTTPGatewayStageResult{}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "image",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					events = append(events, "image")
					return openAIHTTPGatewayStageResult{Cleanup: func() {
						releaseCalls++
						events = append(events, "image-release")
					}}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "cyber",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					events = append(events, "cyber")
					return openAIHTTPGatewayStageResult{Blocked: true}
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	result := pipeline.RunHTTPPreForward(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{})

	require.True(t, result.Blocked)
	require.Nil(t, result.ImageReleaseFunc)
	require.Equal(t, 1, releaseCalls)
	require.Equal(t, []string{"moderation", "image", "cyber", "image-release"}, events)
	_, ok := moderationcoverage.PipelineAdmissionFromContext(c)
	require.False(t, ok)
}

func TestOpenAIHTTPGatewayPipelineReturnsCleanupWhenAllStagesAllow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	releaseCalls := 0
	pipeline := &OpenAIGatewayPipeline{
		httpPreForwardStages: []openAIHTTPGatewayStage{
			openAIHTTPGatewayPipelineTestStage{
				name: "moderation",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "image",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{Cleanup: func() { releaseCalls++ }}
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	result := pipeline.RunHTTPPreForward(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{})

	require.False(t, result.Blocked)
	require.NotNil(t, result.ImageReleaseFunc)
	require.Zero(t, releaseCalls)
	admission, ok := moderationcoverage.PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.True(t, admission.Admitted)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, admission.Pipeline)
	require.Equal(t, moderationcoverage.StagePreForward, admission.Stage)
	require.Equal(t, moderationcoverage.SourceOpenAIHTTPPreForward, admission.Source)
	require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))

	result.ImageReleaseFunc()
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAIHTTPGatewayPipelineRecordsPreForwardStageExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	pipeline := &OpenAIGatewayPipeline{
		httpPreForwardStages: []openAIHTTPGatewayStage{
			openAIHTTPGatewayPipelineTestStage{
				name: "moderation",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "image_permission",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "image_slot",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{}
				},
			},
			openAIHTTPGatewayPipelineTestStage{
				name: "cyber",
				run: func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
					return openAIHTTPGatewayStageResult{}
				},
			},
		},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/responses",
		Handler:            "OpenAIGatewayHandler.Responses",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIResponses,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
		Status:             moderationcoverage.StatusCovered,
	})

	result := pipeline.RunHTTPPreForward(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{
		APIKey:   &service.APIKey{ID: 9, Group: &service.Group{AllowImageGeneration: true}},
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","input":"hello"}`),
	})

	require.False(t, result.Blocked)
	executions := moderationcoverage.PipelineStageExecutionsFromContext(c)
	require.Len(t, executions, 4)
	for i, stage := range []string{
		moderationcoverage.StageModeration,
		moderationcoverage.StageCyber,
		moderationcoverage.StageImage,
		moderationcoverage.StagePreForward,
	} {
		require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, executions[i].Pipeline)
		require.Equal(t, stage, executions[i].Stage)
		require.Equal(t, moderationcoverage.SourceOpenAIHTTPPreForward, executions[i].Source)
		require.Equal(t, http.MethodPost, executions[i].Method)
		require.Equal(t, "/v1/responses", executions[i].Path)
		require.Equal(t, "OpenAIGatewayHandler.Responses", executions[i].Handler)
		require.Equal(t, service.ContentModerationProtocolOpenAIResponses, executions[i].Protocol)
	}
}

func TestOpenAIHTTPGatewayPipelineModerationStageCanWriteAnthropicErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"risk"}]}`)
	guard := &openAIGatewayPipelineModerationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by pipeline",
		Action:     service.ContentModerationActionBlock,
	}}
	pipeline := newOpenAIGatewayPipeline(guard)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result := pipeline.RunHTTPPreForward(&OpenAIGatewayHandler{}, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{
		Protocol:              service.ContentModerationProtocolAnthropicMessages,
		Model:                 "claude-sonnet-4-5",
		Body:                  body,
		ModerationErrorFormat: openAIHTTPModerationErrorAnthropic,
	})

	require.True(t, result.Blocked)
	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"type":"error"`)
	require.Contains(t, recorder.Body.String(), `"type":"content_policy_violation"`)
	require.Contains(t, recorder.Body.String(), "blocked by pipeline")
}

func TestOpenAIHTTPGatewayPipelineDefaultStagesReleaseImageSlotWhenCyberStageBlocks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.1","prompt_cache_key":"pipeline-image-session","input":"draw","tools":[{"type":"image_generation"}]}`)
	apiKey := &service.APIKey{ID: 77, Group: &service.Group{AllowImageGeneration: true}}
	c, w := newOpenAIGatewayPipelineCyberContext(http.MethodPost, "/v1/responses", body)
	blockKey := service.CyberSessionBlockKey(apiKey.ID, c, body)
	require.NotEmpty(t, blockKey)

	guard := &openAIGatewayPipelineModerationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	checker := &openAIGatewayPipelineCyberCheckerStub{
		enabled: true,
		blocked: map[string]bool{blockKey: true},
	}
	h := &OpenAIGatewayHandler{
		pipeline: newOpenAIGatewayPipeline(guard, checker),
		cfg: &config.Config{Gateway: config.GatewayConfig{ImageConcurrency: config.ImageConcurrencyConfig{
			Enabled:               true,
			MaxConcurrentRequests: 1,
			OverflowMode:          config.ImageConcurrencyOverflowModeReject,
		}}},
		imageLimiter: &imageConcurrencyLimiter{},
	}

	result := h.pipeline.RunHTTPPreForward(h, c, zap.NewNop(), openAIHTTPPreForwardPipelineInput{
		APIKey:           apiKey,
		Protocol:         service.ContentModerationProtocolOpenAIResponses,
		Model:            "gpt-5.1",
		Body:             body,
		CyberFormat:      cyberBlockFormatResponses,
		EnableImageStage: true,
		ImageEndpoint:    "/v1/responses",
	})

	require.True(t, result.Blocked)
	require.Nil(t, result.ImageReleaseFunc)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, []string{blockKey}, checker.checkedKeys)

	nextRecorder := httptest.NewRecorder()
	nextContext, _ := gin.CreateTestContext(nextRecorder)
	nextContext.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	release, acquired := h.acquireImageGenerationSlot(nextContext, false)
	require.True(t, acquired)
	require.NotNil(t, release)
	release()
}

type openAIHTTPGatewayPipelineTestStage struct {
	name string
	run  func(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult
}

func (s openAIHTTPGatewayPipelineTestStage) Name() string {
	return s.name
}

func (s openAIHTTPGatewayPipelineTestStage) Run(ctx *openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
	return s.run(ctx)
}
