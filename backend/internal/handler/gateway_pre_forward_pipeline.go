package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type gatewayPreForwardErrorFormat int

const (
	gatewayPreForwardErrorAnthropic gatewayPreForwardErrorFormat = iota
	gatewayPreForwardErrorGemini
	gatewayPreForwardErrorOpenAIChat
	gatewayPreForwardErrorOpenAIResponses
)

type gatewayPreForwardPipelineInput struct {
	APIKey      *service.APIKey
	Subject     middleware2.AuthSubject
	Protocol    string
	Model       string
	Body        []byte
	ErrorFormat gatewayPreForwardErrorFormat
}

type gatewayPreForwardPipelineResult struct {
	Blocked bool
}

type GatewayPreForwardPipeline struct {
	moderationGuard moderationGuard
	stages          []gatewayPreForwardStage
}

func newGatewayPreForwardPipeline(guard moderationGuard) *GatewayPreForwardPipeline {
	if guard == nil {
		guard = newContentModerationGuard(nil)
	}
	return &GatewayPreForwardPipeline{moderationGuard: guard}
}

func (p *GatewayPreForwardPipeline) CheckModeration(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	guard := newContentModerationGuard(nil)
	if p != nil && p.moderationGuard != nil {
		guard = p.moderationGuard
	}
	return guard.Check(c, reqLog, input)
}

func (h *GatewayHandler) runGatewayPreForwardPipeline(c *gin.Context, reqLog *zap.Logger, input gatewayPreForwardPipelineInput) gatewayPreForwardPipelineResult {
	pipeline := h.gatewayPreForwardPipeline()
	return pipeline.Run(h, c, reqLog, input)
}

func (h *GatewayHandler) runGatewayForwardStage(c *gin.Context, adapter ForwardStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Source:   moderationcoverage.SourceGatewayForwardStage,
		Stages: []ExecutableStage{
			executableForwardStageWithContext(c, adapter),
		},
	}.Run(c)
}

func (h *GatewayHandler) gatewayPreForwardPipeline() *GatewayPreForwardPipeline {
	if h == nil {
		return newGatewayPreForwardPipeline(nil)
	}
	if h.preForwardPipeline != nil {
		return h.preForwardPipeline
	}
	guard := h.moderationGuard
	if guard == nil {
		guard = newContentModerationGuard(h.contentModerationService)
	}
	return newGatewayPreForwardPipeline(guard)
}

type gatewayPreForwardStage interface {
	Name() string
	Run(*gatewayPreForwardStageContext) gatewayPreForwardStageResult
}

type gatewayPreForwardStageContext struct {
	handler  *GatewayHandler
	pipeline *GatewayPreForwardPipeline
	c        *gin.Context
	reqLog   *zap.Logger
	input    gatewayPreForwardPipelineInput
}

type gatewayPreForwardStageResult struct {
	Blocked bool
}

type GatewayPreForwardModerationStage struct{}

func (GatewayPreForwardModerationStage) Name() string {
	return moderationcoverage.StageModeration
}

func (GatewayPreForwardModerationStage) Run(ctx *gatewayPreForwardStageContext) gatewayPreForwardStageResult {
	if ctx == nil {
		return gatewayPreForwardStageResult{}
	}
	pipeline := ctx.pipeline
	if pipeline == nil {
		pipeline = newGatewayPreForwardPipeline(nil)
	}
	input := ctx.input
	decision := pipeline.CheckModeration(ctx.c, ctx.reqLog, moderationGuardInput{
		APIKey:   input.APIKey,
		Subject:  input.Subject,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     input.Body,
	})
	if decision == nil || !decision.Blocked {
		return gatewayPreForwardStageResult{}
	}
	if ctx.handler != nil {
		ctx.handler.writeGatewayPreForwardModerationError(ctx.c, input.ErrorFormat, decision)
	}
	return gatewayPreForwardStageResult{Blocked: true}
}

func (p *GatewayPreForwardPipeline) Run(h *GatewayHandler, c *gin.Context, reqLog *zap.Logger, input gatewayPreForwardPipelineInput) gatewayPreForwardPipelineResult {
	ctx := &gatewayPreForwardStageContext{
		handler:  h,
		pipeline: p,
		c:        c,
		reqLog:   reqLog,
		input:    input,
	}
	result := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Source:   moderationcoverage.SourceGatewayPreForward,
		Stages:   gatewayPreForwardExecutableStages(ctx, p.preForwardStages()),
	}.Run(c)
	if result.Stop || result.Err != nil {
		return gatewayPreForwardPipelineResult{Blocked: true}
	}
	return gatewayPreForwardPipelineResult{}
}

func gatewayPreForwardExecutableStages(ctx *gatewayPreForwardStageContext, stages []gatewayPreForwardStage) []ExecutableStage {
	executableStages := make([]ExecutableStage, 0, len(stages)+1)
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		stage := stage
		executableStages = append(executableStages, ExecutableStage{
			Name: stage.Name(),
			Run: func() ExecutableStageResult {
				result := stage.Run(ctx)
				return ExecutableStageResult{Stop: result.Blocked}
			},
		})
	}
	executableStages = append(executableStages, ExecutableStage{
		Name: moderationcoverage.StagePreForward,
		Run: func() ExecutableStageResult {
			if ctx != nil {
				moderationcoverage.MarkPipelineAdmitted(ctx.c, moderationcoverage.PipelineGatewayPreForward, moderationcoverage.StagePreForward, moderationcoverage.SourceGatewayPreForward)
			}
			return ExecutableStageResult{}
		},
	})
	return executableStages
}

func (p *GatewayPreForwardPipeline) preForwardStages() []gatewayPreForwardStage {
	if p != nil && len(p.stages) > 0 {
		return p.stages
	}
	return []gatewayPreForwardStage{GatewayPreForwardModerationStage{}}
}

func (h *GatewayHandler) writeGatewayPreForwardModerationError(c *gin.Context, format gatewayPreForwardErrorFormat, decision *service.ContentModerationDecision) {
	switch format {
	case gatewayPreForwardErrorGemini:
		googleError(c, contentModerationStatus(decision), decision.Message)
	case gatewayPreForwardErrorOpenAIChat:
		h.chatCompletionsErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	case gatewayPreForwardErrorOpenAIResponses:
		h.responsesErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	default:
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	}
}
