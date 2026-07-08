package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *GatewayHandler) checkContentModeration(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentModerationDecision {
	if h == nil || h.contentModerationService == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		return contentModerationCheckErrorDecision()
	}
	return runContentModeration(c, reqLog, h.contentModerationService, apiKey, subject, protocol, model, body)
}

func contentModerationStatus(decision *service.ContentModerationDecision) int {
	if decision == nil || decision.StatusCode < 400 || decision.StatusCode > 599 {
		return http.StatusForbidden
	}
	return decision.StatusCode
}

func contentModerationErrorCode(decision *service.ContentModerationDecision) string {
	return "content_policy_violation"
}

func markOpsContentModerationDiagnostic(c *gin.Context, decision *service.ContentModerationDecision) {
	if c == nil || decision == nil {
		return
	}
	message := "内容审计拦截：命中风险规则"
	if keyword := strings.TrimSpace(decision.MatchedKeyword); keyword != "" {
		message = "内容审计拦截：命中关键词 " + keyword
	} else if category := strings.TrimSpace(decision.HighestCategory); category != "" {
		message = "内容审计拦截：最高风险分类 " + category
	}
	detail := map[string]string{
		"source":         "content_moderation",
		"code":           contentModerationErrorCode(decision),
		"action":         strings.TrimSpace(decision.Action),
		"client_message": strings.TrimSpace(decision.Message),
		"status_code":    fmt.Sprintf("%d", contentModerationStatus(decision)),
	}
	if decision.HighestCategory != "" {
		detail["highest_category"] = strings.TrimSpace(decision.HighestCategory)
	}
	if decision.HighestScore > 0 {
		detail["highest_score"] = fmt.Sprintf("%.4f", decision.HighestScore)
	}
	if decision.MatchedKeyword != "" {
		detail["matched_keyword"] = strings.TrimSpace(decision.MatchedKeyword)
	}
	if decision.KeywordCategory != "" {
		detail["keyword_category"] = strings.TrimSpace(decision.KeywordCategory)
	}
	if decision.KeywordSeverity != "" {
		detail["keyword_severity"] = strings.TrimSpace(decision.KeywordSeverity)
	}
	if decision.KeywordAction != "" {
		detail["keyword_action"] = strings.TrimSpace(decision.KeywordAction)
	}
	if decision.EffectiveKeywordAction != "" {
		detail["effective_keyword_action"] = strings.TrimSpace(decision.EffectiveKeywordAction)
	}
	if decision.RiskContextType != "" {
		detail["risk_context_type"] = strings.TrimSpace(decision.RiskContextType)
	}
	if decision.RiskContextReason != "" {
		detail["risk_context_reason"] = strings.TrimSpace(decision.RiskContextReason)
	}
	raw, _ := json.Marshal(detail)
	service.SetOpsDiagnostic(c, message, string(raw))
}

func contentModerationCheckErrorDecision() *service.ContentModerationDecision {
	return &service.ContentModerationDecision{
		Allowed:    false,
		Blocked:    true,
		Flagged:    true,
		StatusCode: http.StatusServiceUnavailable,
		Message:    "内容安全模块暂时不可用，请稍后重试",
		Action:     service.ContentModerationActionError,
	}
}

func (h *OpenAIGatewayHandler) checkContentModeration(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentModerationDecision {
	if h == nil || h.contentModerationService == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		return contentModerationCheckErrorDecision()
	}
	return runContentModeration(c, reqLog, h.contentModerationService, apiKey, subject, protocol, model, body)
}

func runContentModeration(c *gin.Context, reqLog *zap.Logger, svc *service.ContentModerationService, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentModerationDecision {
	if svc == nil || c == nil || c.Request == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		return contentModerationCheckErrorDecision()
	}
	input := buildContentModerationInput(c, apiKey, subject, protocol, model, body)
	if reqLog != nil {
		reqLog.Info("content_moderation.gateway_check_start",
			zap.String("request_id", input.RequestID),
			zap.Int64("user_id", input.UserID),
			zap.Int64("api_key_id", input.APIKeyID),
			zap.String("api_key_name", input.APIKeyName),
			zap.Int64p("group_id", input.GroupID),
			zap.String("group_name", input.GroupName),
			zap.String("endpoint", input.Endpoint),
			zap.String("provider", input.Provider),
			zap.String("protocol", input.Protocol),
			zap.String("model", input.Model),
			zap.Int("body_bytes", len(body)),
		)
	}
	decision, err := svc.Check(c.Request.Context(), input)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.check_failed", zap.Error(err))
		}
		return contentModerationCheckErrorDecision()
	}
	if reqLog != nil && decision != nil {
		reqLog.Info("content_moderation.gateway_check_done",
			zap.String("request_id", input.RequestID),
			zap.Bool("allowed", decision.Allowed),
			zap.Bool("blocked", decision.Blocked),
			zap.Bool("flagged", decision.Flagged),
			zap.String("action", decision.Action),
			zap.Int("status_code", decision.StatusCode),
			zap.String("highest_category", decision.HighestCategory),
			zap.Float64("highest_score", decision.HighestScore),
		)
	}
	return decision
}

func buildContentModerationInput(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) service.ContentModerationCheckInput {
	input := service.ContentModerationCheckInput{
		RequestID: contentModerationRequestID(c.Request.Context()),
		UserID:    subject.UserID,
		Endpoint:  GetInboundEndpoint(c),
		Provider:  contentModerationProvider(apiKey),
		Model:     strings.TrimSpace(model),
		Protocol:  protocol,
		Body:      body,
	}
	if forcedPlatform, ok := middleware2.GetForcePlatformFromContext(c); ok {
		input.Provider = strings.TrimSpace(forcedPlatform)
	}
	if apiKey != nil {
		input.APIKeyID = apiKey.ID
		input.APIKeyName = apiKey.Name
		if apiKey.User != nil {
			input.UserEmail = apiKey.User.Email
		}
		if apiKey.GroupID != nil {
			groupID := *apiKey.GroupID
			input.GroupID = &groupID
		}
		if apiKey.Group != nil {
			input.GroupName = apiKey.Group.Name
		}
	}
	if input.Endpoint == "" && c.Request != nil && c.Request.URL != nil {
		input.Endpoint = c.Request.URL.Path
	}
	return input
}

func contentModerationProvider(apiKey *service.APIKey) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return strings.TrimSpace(apiKey.Group.Platform)
}

func contentModerationRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if requestID, ok := ctx.Value(ctxkey.RequestID).(string); ok {
		return strings.TrimSpace(requestID)
	}
	return ""
}
