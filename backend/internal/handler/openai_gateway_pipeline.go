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
	moderationGuard     moderationGuard
	cyberSessionChecker openAIGatewayCyberSessionChecker
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

func (p *OpenAIGatewayPipeline) CheckModeration(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	guard := newContentModerationGuard(nil)
	if p != nil && p.moderationGuard != nil {
		guard = p.moderationGuard
	}
	return guard.Check(c, reqLog, input)
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
