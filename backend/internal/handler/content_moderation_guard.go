package handler

import (
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type moderationGuard interface {
	Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision
}

type moderationGuardInput struct {
	APIKey   *service.APIKey
	Subject  middleware2.AuthSubject
	Protocol string
	Model    string
	Body     []byte
}

type openAIHTTPPreForwardPipelineInput struct {
	APIKey           *service.APIKey
	Subject          middleware2.AuthSubject
	Protocol         string
	Model            string
	Body             []byte
	CyberBody        []byte
	CyberFormat      cyberSessionBlockFormat
	EnableImageStage bool
	ImageEndpoint    string
	StreamStarted    bool
}

type openAIHTTPPreForwardPipelineResult struct {
	Blocked          bool
	ImageReleaseFunc func()
}

type contentModerationGuard struct {
	service *service.ContentModerationService
}

func newContentModerationGuard(svc *service.ContentModerationService) moderationGuard {
	return &contentModerationGuard{service: svc}
}

func (h *OpenAIGatewayHandler) checkWithModerationGuard(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	if h == nil {
		return newOpenAIGatewayPipeline(nil).CheckModeration(c, reqLog, input)
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		pipeline = newOpenAIGatewayPipeline(guard)
	}
	return pipeline.CheckModeration(c, reqLog, input)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPPreForwardPipeline(c *gin.Context, reqLog *zap.Logger, input openAIHTTPPreForwardPipelineInput) openAIHTTPPreForwardPipelineResult {
	result := openAIHTTPPreForwardPipelineResult{}
	if decision := h.checkWithModerationGuard(c, reqLog, moderationGuardInput{
		APIKey:   input.APIKey,
		Subject:  input.Subject,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     input.Body,
	}); decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		result.Blocked = true
		return result
	}

	if input.EnableImageStage {
		imageEndpoint := input.ImageEndpoint
		if imageEndpoint == "" {
			imageEndpoint = "/v1/responses"
		}
		imageIntent := service.IsImageGenerationIntent(imageEndpoint, input.Model, input.Body)
		if imageIntent && !service.GroupAllowsImageGeneration(input.APIKey.Group) {
			h.errorResponse(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
			result.Blocked = true
			return result
		}
		if imageIntent {
			imageReleaseFunc, imageAcquired := h.acquireImageGenerationSlot(c, input.StreamStarted)
			if !imageAcquired {
				result.Blocked = true
				return result
			}
			result.ImageReleaseFunc = imageReleaseFunc
		}
	}

	cyberBody := input.CyberBody
	if cyberBody == nil {
		cyberBody = input.Body
	}
	if h.checkCyberSessionWithPipeline(c, reqLog, openAIGatewayCyberSessionInput{
		APIKey:   input.APIKey,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     cyberBody,
		Format:   input.CyberFormat,
	}) {
		if result.ImageReleaseFunc != nil {
			result.ImageReleaseFunc()
			result.ImageReleaseFunc = nil
		}
		result.Blocked = true
		return result
	}

	return result
}

func (h *OpenAIGatewayHandler) checkCyberSessionWithPipeline(c *gin.Context, reqLog *zap.Logger, input openAIGatewayCyberSessionInput) bool {
	if h == nil {
		result := newOpenAIGatewayPipeline(nil).CheckCyberSession(c, reqLog, input)
		return result != nil && result.Blocked
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		pipeline = newOpenAIGatewayPipeline(guard, h.gatewayService)
	} else if pipeline.cyberSessionChecker == nil && h.gatewayService != nil {
		pipeline = &OpenAIGatewayPipeline{
			moderationGuard:     pipeline.moderationGuard,
			cyberSessionChecker: h.gatewayService,
		}
	}
	result := pipeline.CheckCyberSession(c, reqLog, input)
	if result == nil || !result.Blocked {
		return false
	}
	h.enqueueCyberSessionBlockedOpsEntry(c, input.APIKey, input.Model, result.BlockKey)
	return true
}

func (g *contentModerationGuard) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	if g == nil || g.service == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		return contentModerationCheckErrorDecision()
	}
	return runContentModeration(c, reqLog, g.service, input.APIKey, input.Subject, input.Protocol, input.Model, input.Body)
}
