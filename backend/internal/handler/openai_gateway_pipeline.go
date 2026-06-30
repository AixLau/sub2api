package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type OpenAIGatewayPipeline struct {
	moderationGuard moderationGuard
}

func newOpenAIGatewayPipeline(guard moderationGuard) *OpenAIGatewayPipeline {
	if guard == nil {
		guard = newContentModerationGuard(nil)
	}
	return &OpenAIGatewayPipeline{
		moderationGuard: guard,
	}
}

func (p *OpenAIGatewayPipeline) CheckModeration(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	guard := newContentModerationGuard(nil)
	if p != nil && p.moderationGuard != nil {
		guard = p.moderationGuard
	}
	return guard.Check(c, reqLog, input)
}
