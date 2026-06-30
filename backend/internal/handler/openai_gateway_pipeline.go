package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type OpenAIGatewayPipeline struct {
	moderationGuard      moderationGuard
	cyberSessionChecker  openAIGatewayCyberSessionChecker
	httpPreForwardStages []openAIHTTPGatewayStage
}

func newOpenAIGatewayPipeline(guard moderationGuard, cyberSessionChecker ...openAIGatewayCyberSessionChecker) *OpenAIGatewayPipeline {
	if guard == nil {
		guard = newContentModerationGuard(nil)
	}
	var checker openAIGatewayCyberSessionChecker
	if len(cyberSessionChecker) > 0 {
		checker = cyberSessionChecker[0]
	}
	return &OpenAIGatewayPipeline{
		moderationGuard:     guard,
		cyberSessionChecker: checker,
	}
}

func (p *OpenAIGatewayPipeline) withCyberSessionChecker(checker openAIGatewayCyberSessionChecker) *OpenAIGatewayPipeline {
	if p == nil {
		return newOpenAIGatewayPipeline(nil, checker)
	}
	return &OpenAIGatewayPipeline{
		moderationGuard:      p.moderationGuard,
		cyberSessionChecker:  checker,
		httpPreForwardStages: p.httpPreForwardStages,
	}
}

func (p *OpenAIGatewayPipeline) CheckModeration(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	guard := newContentModerationGuard(nil)
	if p != nil && p.moderationGuard != nil {
		guard = p.moderationGuard
	}
	return guard.Check(c, reqLog, input)
}

type openAIHTTPGatewayStage interface {
	Name() string
	Run(*openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult
}

type openAIHTTPGatewayStageContext struct {
	handler  *OpenAIGatewayHandler
	pipeline *OpenAIGatewayPipeline
	c        *gin.Context
	reqLog   *zap.Logger
	input    openAIHTTPPreForwardPipelineInput
}

type openAIHTTPGatewayStageResult struct {
	Blocked bool
	Cleanup func()
}

// OpenAIHTTPModerationStage runs the OpenAI content moderation pre-forward check.
type OpenAIHTTPModerationStage struct{}

func (OpenAIHTTPModerationStage) Name() string {
	return "moderation"
}

func (OpenAIHTTPModerationStage) Run(ctx *openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
	if ctx == nil {
		return openAIHTTPGatewayStageResult{}
	}
	pipeline := ctx.pipeline
	if pipeline == nil {
		pipeline = newOpenAIGatewayPipeline(nil)
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
		return openAIHTTPGatewayStageResult{}
	}
	if ctx.handler != nil {
		ctx.handler.errorResponse(ctx.c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	}
	return openAIHTTPGatewayStageResult{Blocked: true}
}

// OpenAIHTTPImagePermissionStage enforces image generation permission for
// OpenAI HTTP routes that can produce generated images.
type OpenAIHTTPImagePermissionStage struct{}

func (OpenAIHTTPImagePermissionStage) Name() string {
	return "image_permission"
}

func (OpenAIHTTPImagePermissionStage) Run(ctx *openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
	if ctx == nil || !ctx.input.EnableImageStage || !openAIHTTPGatewayImageIntent(ctx.input) {
		return openAIHTTPGatewayStageResult{}
	}
	if ctx.handler == nil {
		return openAIHTTPGatewayStageResult{Blocked: true}
	}
	var group *service.Group
	if ctx.input.APIKey != nil {
		group = ctx.input.APIKey.Group
	}
	if !service.GroupAllowsImageGeneration(group) {
		ctx.handler.errorResponse(ctx.c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return openAIHTTPGatewayStageResult{Blocked: true}
	}
	return openAIHTTPGatewayStageResult{}
}

// OpenAIHTTPImageSlotStage acquires the generated-image concurrency slot after
// moderation has allowed the request to continue.
type OpenAIHTTPImageSlotStage struct{}

func (OpenAIHTTPImageSlotStage) Name() string {
	return "image_slot"
}

func (OpenAIHTTPImageSlotStage) Run(ctx *openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
	if ctx == nil || !ctx.input.EnableImageStage || !openAIHTTPGatewayImageIntent(ctx.input) {
		return openAIHTTPGatewayStageResult{}
	}
	if ctx.handler == nil {
		return openAIHTTPGatewayStageResult{Blocked: true}
	}
	imageReleaseFunc, imageAcquired := ctx.handler.acquireImageGenerationSlot(ctx.c, ctx.input.StreamStarted)
	if !imageAcquired {
		return openAIHTTPGatewayStageResult{Blocked: true}
	}
	return openAIHTTPGatewayStageResult{Cleanup: imageReleaseFunc}
}

func openAIHTTPGatewayImageIntent(input openAIHTTPPreForwardPipelineInput) bool {
	imageEndpoint := input.ImageEndpoint
	if imageEndpoint == "" {
		imageEndpoint = "/v1/responses"
	}
	return service.IsImageGenerationIntent(imageEndpoint, input.Model, input.Body)
}

// OpenAIHTTPCyberStage rejects requests for sessions blocked by cyber policy.
type OpenAIHTTPCyberStage struct{}

func (OpenAIHTTPCyberStage) Name() string {
	return "cyber"
}

func (OpenAIHTTPCyberStage) Run(ctx *openAIHTTPGatewayStageContext) openAIHTTPGatewayStageResult {
	if ctx == nil || ctx.input.SkipCyberStage {
		return openAIHTTPGatewayStageResult{}
	}
	input := ctx.input
	cyberBody := input.CyberBody
	if cyberBody == nil {
		cyberBody = input.Body
	}
	pipeline := ctx.pipeline
	if pipeline == nil {
		pipeline = newOpenAIGatewayPipeline(nil)
	}
	result := pipeline.CheckCyberSession(ctx.c, ctx.reqLog, openAIGatewayCyberSessionInput{
		APIKey:   input.APIKey,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     cyberBody,
		Format:   input.CyberFormat,
	})
	if result == nil || !result.Blocked {
		return openAIHTTPGatewayStageResult{}
	}
	if ctx.handler != nil {
		ctx.handler.enqueueCyberSessionBlockedOpsEntry(ctx.c, input.APIKey, input.Model, result.BlockKey)
	}
	return openAIHTTPGatewayStageResult{Blocked: true}
}

// RunHTTPPreForward runs the OpenAI HTTP pre-forward stages in order.
func (p *OpenAIGatewayPipeline) RunHTTPPreForward(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIHTTPPreForwardPipelineInput) openAIHTTPPreForwardPipelineResult {
	var cleanup func()
	ctx := &openAIHTTPGatewayStageContext{
		handler:  h,
		pipeline: p,
		c:        c,
		reqLog:   reqLog,
		input:    input,
	}
	for _, stage := range p.httpPreForwardPipelineStages(input) {
		if stage == nil {
			continue
		}
		stageResult := stage.Run(ctx)
		cleanup = combineOpenAIHTTPGatewayCleanup(cleanup, stageResult.Cleanup)
		if stageResult.Blocked {
			if cleanup != nil {
				cleanup()
			}
			return openAIHTTPPreForwardPipelineResult{Blocked: true}
		}
	}
	return openAIHTTPPreForwardPipelineResult{ImageReleaseFunc: cleanup}
}

func (p *OpenAIGatewayPipeline) httpPreForwardPipelineStages(input openAIHTTPPreForwardPipelineInput) []openAIHTTPGatewayStage {
	if p != nil && len(p.httpPreForwardStages) > 0 {
		return p.httpPreForwardStages
	}
	if input.ImagePermissionBeforeModeration {
		return []openAIHTTPGatewayStage{
			OpenAIHTTPImagePermissionStage{},
			OpenAIHTTPModerationStage{},
			OpenAIHTTPImageSlotStage{},
			OpenAIHTTPCyberStage{},
		}
	}
	return []openAIHTTPGatewayStage{
		OpenAIHTTPModerationStage{},
		OpenAIHTTPImagePermissionStage{},
		OpenAIHTTPImageSlotStage{},
		OpenAIHTTPCyberStage{},
	}
}

func combineOpenAIHTTPGatewayCleanup(current func(), next func()) func() {
	if next == nil {
		return current
	}
	if current == nil {
		return next
	}
	return func() {
		next()
		current()
	}
}

type openAIGatewayCyberSessionChecker interface {
	CyberSessionBlockRuntime(ctx context.Context) (bool, time.Duration)
	IsCyberSessionBlocked(ctx context.Context, key string) bool
}

type openAIGatewayCyberSessionInput struct {
	APIKey   *service.APIKey
	Protocol string
	Model    string
	Body     []byte
	Format   cyberSessionBlockFormat
}

type OpenAICyberStageResult struct {
	Blocked  bool
	BlockKey string
}

func (p *OpenAIGatewayPipeline) CheckCyberSession(c *gin.Context, reqLog *zap.Logger, input openAIGatewayCyberSessionInput) *OpenAICyberStageResult {
	result := &OpenAICyberStageResult{}
	if p == nil || p.cyberSessionChecker == nil || c == nil || input.APIKey == nil {
		return result
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	enabled, _ := p.cyberSessionChecker.CyberSessionBlockRuntime(ctx)
	if !enabled {
		return result
	}
	key := service.CyberSessionBlockKey(input.APIKey.ID, c, input.Body)
	if key == "" {
		return result
	}
	result.BlockKey = key
	if !p.cyberSessionChecker.IsCyberSessionBlocked(ctx, key) {
		return result
	}
	result.Blocked = true
	writeOpenAICyberSessionBlockedResponse(c, input.Format)
	return result
}

func writeOpenAICyberSessionBlockedResponse(c *gin.Context, format cyberSessionBlockFormat) {
	switch format {
	case cyberBlockFormatResponses, cyberBlockFormatChat:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": cyberSessionBlockedClientMsg,
		}})
	default:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": cyberSessionBlockedClientMsg,
		}})
	}
}
