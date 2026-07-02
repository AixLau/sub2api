package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
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

type GatewayMessagesGeminiForwardStage struct {
	GeminiCompatService       *service.GeminiMessagesCompatService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	Model                     string
	Action                    string
	Stream                    bool
	Body                      []byte
	HasBoundSession           bool
	SessionGroupID            int64
	SessionKey                string
	Result                    **service.ForwardResult
}

func (GatewayMessagesGeminiForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayMessagesGeminiForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity {
		result, err = s.AntigravityGatewayService.ForwardGemini(
			ctx,
			c,
			s.Account,
			s.Model,
			s.Action,
			s.Stream,
			s.Body,
			s.HasBoundSession,
			service.WithForwardGeminiSession(s.SessionGroupID, s.SessionKey),
		)
	} else {
		result, err = s.GeminiCompatService.Forward(ctx, c, s.Account, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayMessagesForwardStage struct {
	GatewayService            *service.GatewayService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	ParsedRequest             *service.ParsedRequest
	Body                      []byte
	HasBoundSession           bool
	Result                    **service.ForwardResult
}

func (GatewayMessagesForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayMessagesForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity && s.Account.Type != service.AccountTypeAPIKey {
		result, err = s.AntigravityGatewayService.Forward(ctx, c, s.Account, s.Body, s.HasBoundSession)
	} else {
		result, err = s.GatewayService.Forward(ctx, c, s.Account, s.ParsedRequest)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayGeminiV1BetaForwardStage struct {
	GeminiCompatService       *service.GeminiMessagesCompatService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	Model                     string
	Action                    string
	Stream                    bool
	Body                      []byte
	HasBoundSession           bool
	SessionGroupID            int64
	SessionKey                string
	Result                    **service.ForwardResult
}

func (GatewayGeminiV1BetaForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayGeminiV1BetaForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity && s.Account.Type != service.AccountTypeAPIKey {
		result, err = s.AntigravityGatewayService.ForwardGemini(
			ctx,
			c,
			s.Account,
			s.Model,
			s.Action,
			s.Stream,
			s.Body,
			s.HasBoundSession,
			service.WithForwardGeminiSession(s.SessionGroupID, s.SessionKey),
		)
	} else {
		result, err = s.GeminiCompatService.ForwardNative(ctx, c, s.Account, s.Model, s.Action, s.Stream, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayCountTokensForwardStage struct {
	GatewayService *service.GatewayService
	Account        *service.Account
	ParsedRequest  *service.ParsedRequest
}

func (GatewayCountTokensForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayCountTokensForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.GatewayService == nil {
		return ExecutableStageResult{}
	}
	return ExecutableStageResult{
		Err: s.GatewayService.ForwardCountTokens(c.Request.Context(), c, s.Account, s.ParsedRequest),
	}
}

func (h *GatewayHandler) runGatewayBillingStage(c *gin.Context, run func() ExecutableStageResult) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Source:   moderationcoverage.SourceGatewayBillingStage,
		Stages: []ExecutableStage{
			{Name: moderationcoverage.StageBilling, Run: run},
		},
	}.Run(c)
}

func (h *GatewayHandler) runGatewayRoutingStage(c *gin.Context, run func() ExecutableStageResult) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Source:   moderationcoverage.SourceGatewayRoutingStage,
		Stages: []ExecutableStage{
			{Name: moderationcoverage.StageRouting, Run: run},
		},
	}.Run(c)
}

func (h *GatewayHandler) runGatewayUsageStage(c *gin.Context, adapter UsageStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Source:   moderationcoverage.SourceGatewayUsageStage,
		Stages: []ExecutableStage{
			executableUsageStageWithContext(c, adapter),
		},
	}.Run(c)
}

type GatewayUsageStage struct {
	Handler               *GatewayHandler
	RequestContext        context.Context
	Result                *service.ForwardResult
	QuotaPlatform         string
	APIKey                *service.APIKey
	Account               *service.Account
	Subscription          *service.UserSubscription
	InboundEndpoint       string
	UpstreamEndpoint      string
	UserAgent             string
	ClientIP              string
	RequestPayloadHash    string
	ForceCacheBilling     bool
	APIKeyService         *service.APIKeyService
	ChannelUsageFields    service.ChannelUsageFields
	LongContext           bool
	LongContextThreshold  int
	LongContextMultiplier float64
	LogComponent          string
	LogMessage            string
	LogUserID             int64
	LogModel              string
}

func (GatewayUsageStage) StageName() string {
	return moderationcoverage.StageUsage
}

func (s GatewayUsageStage) RunUsage(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	record := func(taskCtx context.Context) {
		var err error
		if s.LongContext {
			err = h.gatewayService.RecordUsageWithLongContext(taskCtx, &service.RecordUsageLongContextInput{
				Result:                s.Result,
				QuotaPlatform:         s.QuotaPlatform,
				APIKey:                s.APIKey,
				User:                  s.APIKey.User,
				Account:               s.Account,
				Subscription:          s.Subscription,
				InboundEndpoint:       s.InboundEndpoint,
				UpstreamEndpoint:      s.UpstreamEndpoint,
				UserAgent:             s.UserAgent,
				IPAddress:             s.ClientIP,
				RequestPayloadHash:    s.RequestPayloadHash,
				LongContextThreshold:  s.LongContextThreshold,
				LongContextMultiplier: s.LongContextMultiplier,
				ForceCacheBilling:     s.ForceCacheBilling,
				APIKeyService:         s.APIKeyService,
				ChannelUsageFields:    s.ChannelUsageFields,
			})
		} else {
			err = h.gatewayService.RecordUsage(taskCtx, &service.RecordUsageInput{
				Result:             s.Result,
				QuotaPlatform:      s.QuotaPlatform,
				APIKey:             s.APIKey,
				User:               s.APIKey.User,
				Account:            s.Account,
				Subscription:       s.Subscription,
				InboundEndpoint:    s.InboundEndpoint,
				UpstreamEndpoint:   s.UpstreamEndpoint,
				UserAgent:          s.UserAgent,
				IPAddress:          s.ClientIP,
				RequestPayloadHash: s.RequestPayloadHash,
				ForceCacheBilling:  s.ForceCacheBilling,
				APIKeyService:      s.APIKeyService,
				ChannelUsageFields: s.ChannelUsageFields,
			})
		}
		if err != nil {
			logger.L().With(
				zap.String("component", s.LogComponent),
				zap.Int64("user_id", s.LogUserID),
				zap.Int64("api_key_id", s.APIKey.ID),
				zap.Any("group_id", s.APIKey.GroupID),
				zap.String("model", s.LogModel),
				zap.Int64("account_id", s.Account.ID),
			).Error(s.LogMessage, zap.Error(err))
		}
	}
	h.submitUsageRecordTask(ctx, record)
	return ExecutableStageResult{}
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
