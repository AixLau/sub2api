package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type OpenAIGatewayPipeline struct {
	moderationGuard       moderationGuard
	cyberSessionChecker   openAIGatewayCyberSessionChecker
	httpPreForwardStages  []openAIHTTPGatewayStage
	wsInitialFrameStages  []openAIWebSocketGatewayStage
	wsFollowupFrameStages []openAIWebSocketGatewayStage
}

// OpenAIWebSocketPipeline is the WebSocket-facing surface of the OpenAI gateway pipeline.
type OpenAIWebSocketPipeline = OpenAIGatewayPipeline

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
		moderationGuard:       p.moderationGuard,
		cyberSessionChecker:   checker,
		httpPreForwardStages:  p.httpPreForwardStages,
		wsInitialFrameStages:  p.wsInitialFrameStages,
		wsFollowupFrameStages: p.wsFollowupFrameStages,
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

type openAIHTTPModerationErrorFormat int

const (
	openAIHTTPModerationErrorOpenAI openAIHTTPModerationErrorFormat = iota
	openAIHTTPModerationErrorAnthropic
)

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
		ctx.handler.writeOpenAIHTTPModerationError(ctx.c, input.ModerationErrorFormat, decision)
	}
	return openAIHTTPGatewayStageResult{Blocked: true}
}

func (h *OpenAIGatewayHandler) writeOpenAIHTTPModerationError(c *gin.Context, format openAIHTTPModerationErrorFormat, decision *service.ContentModerationDecision) {
	markOpsContentModerationDiagnostic(c, decision)
	switch format {
	case openAIHTTPModerationErrorAnthropic:
		h.anthropicErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	default:
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	}
}

// OpenAIHTTPImagePermissionStage enforces image generation permission for
// OpenAI HTTP routes that can produce generated images.
type OpenAIHTTPImagePermissionStage struct{}

// openAIResponsesImageIntentForPlatform keeps Responses classification shared
// by route-owned and direct-invocation compatibility paths. Passive tool
// namespace declarations do not constitute an image-generation request.
func openAIResponsesImageIntentForPlatform(_ *service.APIKey, model string, body []byte) bool {
	return service.IsExplicitImageGenerationIntent("/v1/responses", model, body)
}

// rejectDirectOpenAIResponsesImagePermission is a compatibility guard for
// internal callers that invoke Responses without the registrar. Production
// routes carry route metadata and remain owned exclusively by the pre-forward
// pipeline, including slot acquisition and cleanup.
func (h *OpenAIGatewayHandler) rejectDirectOpenAIResponsesImagePermission(c *gin.Context, apiKey *service.APIKey, imageIntent bool) bool {
	if _, registered := moderationcoverage.RouteMetaFromContext(c); registered || !imageIntent {
		return false
	}
	var group *service.Group
	if apiKey != nil {
		group = apiKey.Group
	}
	if service.GroupAllowsImageGeneration(group) {
		return false
	}
	h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
	return true
}

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
	if imageEndpoint == "/v1/responses" {
		return service.IsExplicitImageGenerationIntent(imageEndpoint, input.Model, input.Body)
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
		ctx.handler.recordCyberSessionBlockedRiskEvent(ctx.c, input.APIKey, input.Model, result.BlockKey, cyberBody)
	}
	return openAIHTTPGatewayStageResult{Blocked: true}
}

// RunHTTPPreForward runs the OpenAI HTTP pre-forward stages in order.
func (p *OpenAIGatewayPipeline) RunHTTPPreForward(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIHTTPPreForwardPipelineInput) openAIHTTPPreForwardPipelineResult {
	var cleanup func()
	blocked := false
	ctx := &openAIHTTPGatewayStageContext{
		handler:  h,
		pipeline: p,
		c:        c,
		reqLog:   reqLog,
		input:    input,
	}
	result := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPPreForward,
		Stages:   openAIHTTPPreForwardExecutableStages(ctx, p.httpPreForwardPipelineStages(input), &cleanup, &blocked),
	}.Run(c)
	if result.Stop || result.Err != nil || blocked {
		if cleanup != nil {
			cleanup()
		}
		return openAIHTTPPreForwardPipelineResult{Blocked: true}
	}
	return openAIHTTPPreForwardPipelineResult{ImageReleaseFunc: cleanup}
}

func openAIHTTPPreForwardExecutableStages(ctx *openAIHTTPGatewayStageContext, stages []openAIHTTPGatewayStage, cleanup *func(), blocked *bool) []ExecutableStage {
	executableStages := make([]ExecutableStage, 0, len(stages)+1)
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		stage := stage
		executableStages = append(executableStages, ExecutableStage{
			Name: openAIHTTPPreForwardExecutableStageName(stage.Name()),
			Run: func() ExecutableStageResult {
				receiptCountBefore := 0
				if ctx != nil {
					receiptCountBefore = len(moderationcoverage.ModerationReceiptsFromContext(ctx.c))
				}
				stageResult := stage.Run(ctx)
				if !stageResult.Blocked && openAIHTTPPreForwardExecutableStageName(stage.Name()) == moderationcoverage.StageModeration && ctx != nil {
					ensureContentModerationReceipt(ctx.c, ctx.input.Protocol, receiptCountBefore)
				}
				if cleanup != nil {
					*cleanup = combineOpenAIHTTPGatewayCleanup(*cleanup, stageResult.Cleanup)
				}
				if stageResult.Blocked {
					if blocked != nil {
						*blocked = true
					}
					return ExecutableStageResult{Stop: true}
				}
				return ExecutableStageResult{}
			},
		})
	}
	executableStages = append(executableStages, ExecutableStage{
		Name: moderationcoverage.StagePreForward,
		Run: func() ExecutableStageResult {
			if ctx != nil {
				moderationcoverage.MarkPipelineAdmittedAfterModeration(ctx.c, moderationcoverage.PipelineOpenAIHTTP, moderationcoverage.StagePreForward, moderationcoverage.SourceOpenAIHTTPPreForward)
			}
			return ExecutableStageResult{}
		},
	})
	return executableStages
}

func openAIHTTPPreForwardExecutableStageName(stage string) string {
	switch stage {
	case "moderation":
		return moderationcoverage.StageModeration
	case "image_permission", "image_slot", "image":
		return moderationcoverage.StageImage
	case "cyber":
		return moderationcoverage.StageCyber
	default:
		return moderationcoverage.NormalizeStage(stage)
	}
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

type openAIWebSocketGatewayStage interface {
	Name() string
	Run(*openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult
}

type openAIWebSocketGatewayStageContext struct {
	handler  *OpenAIGatewayHandler
	pipeline *OpenAIGatewayPipeline
	c        *gin.Context
	reqLog   *zap.Logger
	input    openAIWebSocketPipelineInput
}

type openAIWebSocketGatewayStageResult struct {
	Result openAIWebSocketPipelineResult
}

// OpenAIWebSocketModerationStage runs the Responses WebSocket content moderation check.
type OpenAIWebSocketModerationStage struct{}

func (OpenAIWebSocketModerationStage) Name() string {
	return "moderation"
}

func (OpenAIWebSocketModerationStage) Run(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
	if ctx == nil {
		return openAIWebSocketGatewayStageResult{}
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
		return openAIWebSocketGatewayStageResult{}
	}
	return openAIWebSocketGatewayStageResult{Result: openAIWebSocketPipelineResult{
		Blocked:            true,
		BlockReason:        openAIWebSocketPipelineBlockReasonModeration,
		ModerationDecision: decision,
		Message:            decision.Message,
	}}
}

// OpenAIWebSocketImagePermissionStage enforces group image generation permission
// for the initial Responses WebSocket frame before any upstream connection is used.
type OpenAIWebSocketImagePermissionStage struct{}

func (OpenAIWebSocketImagePermissionStage) Name() string {
	return "image_permission"
}

func (OpenAIWebSocketImagePermissionStage) Run(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
	if ctx == nil || !openAIWebSocketGatewayImageIntent(ctx.input) {
		return openAIWebSocketGatewayStageResult{}
	}
	var group *service.Group
	if ctx.input.APIKey != nil {
		group = ctx.input.APIKey.Group
	}
	if service.GroupAllowsImageGeneration(group) {
		return openAIWebSocketGatewayStageResult{}
	}
	return openAIWebSocketGatewayStageResult{Result: openAIWebSocketPipelineResult{
		Blocked:     true,
		BlockReason: openAIWebSocketPipelineBlockReasonImagePermission,
		Message:     service.ImageGenerationPermissionMessage(),
	}}
}

func openAIWebSocketGatewayImageIntent(input openAIWebSocketPipelineInput) bool {
	imageEndpoint := input.ImageEndpoint
	if imageEndpoint == "" {
		imageEndpoint = "/v1/responses"
	}
	if imageEndpoint == "/v1/responses" {
		return service.IsExplicitImageGenerationIntent(imageEndpoint, input.Model, input.Body)
	}
	return service.IsImageGenerationIntent(imageEndpoint, input.Model, input.Body)
}

// OpenAIWebSocketCyberStage rejects WebSocket sessions that are already blocked
// by the cyber session policy. It intentionally does not write an HTTP response;
// the WebSocket handler preserves the existing error-frame and close behavior.
type OpenAIWebSocketCyberStage struct{}

func (OpenAIWebSocketCyberStage) Name() string {
	return "cyber"
}

func (OpenAIWebSocketCyberStage) Run(ctx *openAIWebSocketGatewayStageContext) openAIWebSocketGatewayStageResult {
	if ctx == nil {
		return openAIWebSocketGatewayStageResult{}
	}
	pipeline := ctx.pipeline
	if pipeline == nil {
		pipeline = newOpenAIGatewayPipeline(nil)
	}
	input := ctx.input
	cyberResult := pipeline.checkCyberSessionBlock(ctx.c, openAIGatewayCyberSessionInput{
		APIKey:   input.APIKey,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     input.CyberBody,
		Format:   cyberBlockFormatResponses,
	})
	result := openAIWebSocketPipelineResult{}
	if cyberResult != nil {
		result.CyberBlockKey = cyberResult.BlockKey
	}
	if cyberResult == nil || !cyberResult.Blocked {
		return openAIWebSocketGatewayStageResult{Result: result}
	}
	result.Blocked = true
	result.BlockReason = openAIWebSocketPipelineBlockReasonCyberSession
	result.Message = cyberSessionBlockedClientMessage(cyberSessionBlockPlatform(input.APIKey, input.Protocol, cyberBlockFormatResponses))
	return openAIWebSocketGatewayStageResult{Result: result}
}

// RunWebSocketInitialFrame runs the Responses WebSocket initial-frame stages in order.
func (p *OpenAIGatewayPipeline) RunWebSocketInitialFrame(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	return p.runWebSocketStages(h, c, reqLog, input, p.webSocketInitialFramePipelineStages(input), moderationcoverage.SourceOpenAIWebSocketInitialFrame)
}

// RunWebSocketFollowupFrame runs the Responses WebSocket follow-up-frame stages in order.
func (p *OpenAIGatewayPipeline) RunWebSocketFollowupFrame(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	return p.runWebSocketStages(h, c, reqLog, input, p.webSocketFollowupFramePipelineStages(input), moderationcoverage.SourceOpenAIWebSocketFollowupFrame)
}

func (p *OpenAIGatewayPipeline) runWebSocketStages(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput, stages []openAIWebSocketGatewayStage, source string) openAIWebSocketPipelineResult {
	ctx := &openAIWebSocketGatewayStageContext{
		handler:  h,
		pipeline: p,
		c:        c,
		reqLog:   reqLog,
		input:    input,
	}
	result := openAIWebSocketPipelineResult{}
	executableResult := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   source,
		Stages:   openAIWebSocketExecutableStages(ctx, stages, source, &result),
	}.Run(c)
	if executableResult.Stop || executableResult.Err != nil {
		return result
	}
	return result
}

func openAIWebSocketExecutableStages(ctx *openAIWebSocketGatewayStageContext, stages []openAIWebSocketGatewayStage, source string, result *openAIWebSocketPipelineResult) []ExecutableStage {
	executableStages := make([]ExecutableStage, 0, len(stages)+1)
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		stage := stage
		executableStages = append(executableStages, ExecutableStage{
			Name: openAIWebSocketExecutableStageName(stage.Name()),
			Run: func() ExecutableStageResult {
				receiptCountBefore := 0
				if ctx != nil {
					receiptCountBefore = len(moderationcoverage.ModerationReceiptsFromContext(ctx.c))
				}
				stageResult := stage.Run(ctx).Result
				if !stageResult.Blocked && openAIWebSocketExecutableStageName(stage.Name()) == moderationcoverage.StageModeration && ctx != nil {
					ensureContentModerationReceipt(ctx.c, ctx.input.Protocol, receiptCountBefore)
				}
				if result != nil && stageResult.CyberBlockKey != "" {
					result.CyberBlockKey = stageResult.CyberBlockKey
				}
				if stageResult.Blocked {
					if result != nil && stageResult.CyberBlockKey == "" {
						stageResult.CyberBlockKey = result.CyberBlockKey
					}
					if result != nil {
						*result = stageResult
					}
					return ExecutableStageResult{Stop: true}
				}
				return ExecutableStageResult{}
			},
		})
	}
	executableStages = append(executableStages, ExecutableStage{
		Name: moderationcoverage.StagePreForward,
		Run: func() ExecutableStageResult {
			if ctx != nil {
				moderationcoverage.MarkPipelineAdmittedAfterModeration(ctx.c, moderationcoverage.PipelineOpenAIWebSocket, moderationcoverage.StagePreForward, source)
			}
			return ExecutableStageResult{}
		},
	})
	return executableStages
}

func openAIWebSocketExecutableStageName(stage string) string {
	switch stage {
	case "moderation":
		return moderationcoverage.StageModeration
	case "image_permission":
		return moderationcoverage.StageImage
	case "cyber":
		return moderationcoverage.StageCyber
	default:
		return moderationcoverage.NormalizeStage(stage)
	}
}

func (p *OpenAIGatewayPipeline) webSocketInitialFramePipelineStages(input openAIWebSocketPipelineInput) []openAIWebSocketGatewayStage {
	if p != nil && len(p.wsInitialFrameStages) > 0 {
		return p.wsInitialFrameStages
	}
	return []openAIWebSocketGatewayStage{
		OpenAIWebSocketModerationStage{},
		OpenAIWebSocketImagePermissionStage{},
		OpenAIWebSocketCyberStage{},
	}
}

func (p *OpenAIGatewayPipeline) webSocketFollowupFramePipelineStages(input openAIWebSocketPipelineInput) []openAIWebSocketGatewayStage {
	if p != nil && len(p.wsFollowupFrameStages) > 0 {
		return p.wsFollowupFrameStages
	}
	return []openAIWebSocketGatewayStage{
		OpenAIWebSocketModerationStage{},
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
	Platform string
}

type OpenAICyberStageResult struct {
	Blocked  bool
	BlockKey string
}

func (p *OpenAIGatewayPipeline) CheckCyberSession(c *gin.Context, reqLog *zap.Logger, input openAIGatewayCyberSessionInput) *OpenAICyberStageResult {
	result := p.checkCyberSessionBlock(c, input)
	if result == nil || !result.Blocked {
		return result
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = cyberSessionBlockPlatform(input.APIKey, input.Protocol, input.Format)
	}
	writeOpenAICyberSessionBlockedResponse(c, input.Format, platform)
	return result
}

func (p *OpenAIGatewayPipeline) checkCyberSessionBlock(c *gin.Context, input openAIGatewayCyberSessionInput) *OpenAICyberStageResult {
	result := &OpenAICyberStageResult{}
	if p == nil || p.cyberSessionChecker == nil || c == nil || input.APIKey == nil {
		return result
	}
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	key := service.CyberSessionBlockKey(input.APIKey.ID, c, input.Body)
	if key == "" {
		return result
	}
	result.BlockKey = key
	enabled, _ := p.cyberSessionChecker.CyberSessionBlockRuntime(ctx)
	if !enabled {
		return result
	}
	if !p.cyberSessionChecker.IsCyberSessionBlocked(ctx, key) {
		return result
	}
	result.Blocked = true
	return result
}

func writeOpenAICyberSessionBlockedResponse(c *gin.Context, format cyberSessionBlockFormat, platform string) {
	message := cyberSessionBlockedClientMessage(platform)
	switch format {
	case cyberBlockFormatAnthropic:
		c.JSON(http.StatusForbidden, gin.H{"type": "error", "error": gin.H{
			"type":    "permission_error",
			"message": message,
		}})
	case cyberBlockFormatResponses, cyberBlockFormatChat:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": message,
		}})
	default:
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": message,
		}})
	}
}
