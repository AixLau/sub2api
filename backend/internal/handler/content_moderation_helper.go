package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const selectedAccountModerationStateContextKey = "selected_account_moderation_state"
const contentModerationForwardConflictRecorderContextKey = "content_moderation_forward_conflict_recorder"

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
	if contentModerationIsNonViolationDeferred(decision) {
		return http.StatusServiceUnavailable
	}
	if decision == nil || decision.StatusCode < 400 || decision.StatusCode > 599 {
		return http.StatusForbidden
	}
	return decision.StatusCode
}

func contentModerationErrorCode(decision *service.ContentModerationDecision) string {
	if decision != nil {
		switch strings.TrimSpace(decision.Action) {
		case service.ContentModerationActionSemanticReviewDeferred:
			return "content_review_required"
		case service.ContentModerationActionSemanticReviewUnavailable:
			return "content_review_unavailable"
		case service.ContentModerationActionSemanticReviewIncomplete:
			return "content_review_incomplete"
		}
	}
	return "content_policy_violation"
}

func contentModerationIsNonViolationDeferred(decision *service.ContentModerationDecision) bool {
	if decision == nil {
		return false
	}
	switch strings.TrimSpace(decision.Action) {
	case service.ContentModerationActionSemanticReviewDeferred,
		service.ContentModerationActionSemanticReviewUnavailable,
		service.ContentModerationActionSemanticReviewIncomplete:
		return true
	default:
		return false
	}
}

func markOpsContentModerationDiagnostic(c *gin.Context, decision *service.ContentModerationDecision) {
	if c == nil || decision == nil {
		return
	}
	message := "内容审计拦截：命中风险规则"
	if contentModerationIsNonViolationDeferred(decision) {
		message = "内容审计暂缓：审核未形成可放行结论"
	} else if keyword := strings.TrimSpace(decision.MatchedKeyword); keyword != "" {
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
		Allowed: true,
		Action:  service.ContentModerationActionError,
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

func (h *OpenAIGatewayHandler) ModerateBatchImageSubmit(c *gin.Context) bool {
	reqLog := requestLogger(c, "handler.batch_image.submit_moderation")
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return false
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return false
		}
		markOpsRequestBodyReadError(c, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return false
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return false
	}
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return false
	}

	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	decision := h.checkContentModeration(c, reqLog, apiKey, subject, service.ContentModerationProtocolBatchImages, model, body)
	if decision != nil && decision.Blocked {
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
		return false
	}
	restoreRequestBody(c, body)
	return true
}

func runContentModeration(c *gin.Context, reqLog *zap.Logger, svc *service.ContentModerationService, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol string, model string, body []byte) *service.ContentModerationDecision {
	if svc == nil || c == nil || c.Request == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		decision := contentModerationCheckErrorDecision()
		markContentModerationReceipt(c, protocol, "", decision, false)
		return decision
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
		decision = contentModerationCheckErrorDecision()
	}
	markContentModerationReceipt(c, protocol, "", decision, false)
	recordContentModerationReceiptMetric(svc, c, "")
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

func markContentModerationReceipt(c *gin.Context, protocol, policyRevision string, decision *service.ContentModerationDecision, selectedAccountPending bool) {
	if c == nil {
		return
	}
	receipt := moderationcoverage.ModerationExecutionReceipt{
		Protocol:       protocol,
		PolicyRevision: policyRevision,
		LocalScanDone:  !selectedAccountPending,
		Outcome:        "error",
		ForwardAllowed: false,
	}
	if c.Request != nil {
		receipt.RequestID = contentModerationRequestID(c.Request.Context())
	}
	if selectedAccountPending {
		receipt.Outcome = "deferred_selected_account"
		moderationcoverage.MarkModerationReceipt(c, receipt)
		return
	}
	if decision != nil {
		action := strings.TrimSpace(decision.Action)
		if receipt.PolicyRevision == "" {
			receipt.PolicyRevision = strings.TrimSpace(decision.PolicyRevision)
		}
		receipt.SemanticCalled = strings.HasPrefix(action, "semantic_review_")
		switch {
		case contentModerationIsNonViolationDeferred(decision):
			receipt.Outcome = "deferred"
			receipt.ForwardAllowed = false
		case decision.Blocked:
			receipt.Outcome = "reject"
			receipt.ForwardAllowed = false
		case action == service.ContentModerationActionError:
			receipt.Outcome = "error"
			receipt.ForwardAllowed = decision.Allowed
		case action == service.ContentModerationActionSemanticReviewAllow:
			receipt.Outcome = "allow"
			receipt.ForwardAllowed = decision.Allowed
		default:
			receipt.Outcome = "no_hit"
			receipt.ForwardAllowed = decision.Allowed && !decision.Blocked
		}
	}
	moderationcoverage.MarkModerationReceipt(c, receipt)
}

func ensureContentModerationReceipt(c *gin.Context, protocol string, receiptCountBefore int) {
	if c == nil || len(moderationcoverage.ModerationReceiptsFromContext(c)) != receiptCountBefore {
		return
	}
	markContentModerationReceipt(c, protocol, "", &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}, false)
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

func runSelectedAccountContentModeration(c *gin.Context, reqLog *zap.Logger, svc *service.ContentModerationService, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte, account *service.Account) *service.ContentModerationGateResult {
	if svc == nil || account == nil || c == nil || c.Request == nil || !svc.RequiresSelectedAccount(c.Request.Context()) {
		return nil
	}
	input := buildContentModerationInput(c, apiKey, subject, protocol, model, body)
	input.AccountID = account.ID
	input.AccountName = account.Name
	input.AccountType = account.Type
	var prior *service.ContentModerationAttemptState
	if value, ok := c.Get(selectedAccountModerationStateContextKey); ok {
		prior, _ = value.(*service.ContentModerationAttemptState)
	}
	result, err := svc.CheckAccountAttempt(c.Request.Context(), input, prior)
	if err != nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.account_attempt_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		}
		result := &service.ContentModerationGateResult{Decision: contentModerationCheckErrorDecision(), Disposition: service.ContentModerationDispositionProviderErrorOpen}
		markContentModerationReceipt(c, protocol, "", result.Decision, false)
		recordContentModerationReceiptMetric(svc, c, "selected_account")
		markSelectedAccountPipelineAdmission(c)
		return result
	}
	c.Set(selectedAccountModerationStateContextKey, result.NextState)
	if result != nil {
		markContentModerationReceipt(c, protocol, result.PolicyRevision, result.Decision, false)
		recordContentModerationReceiptMetric(svc, c, "selected_account")
		markSelectedAccountPipelineAdmission(c)
	}
	return result
}

func recordContentModerationReceiptMetric(svc *service.ContentModerationService, c *gin.Context, pipeline string) {
	if svc == nil || c == nil {
		return
	}
	receipt, ok := moderationcoverage.ModerationReceiptFromContext(c)
	if !ok {
		return
	}
	if pipeline == "" {
		if meta, metaOK := moderationcoverage.RouteMetaFromContext(c); metaOK {
			pipeline = meta.Pipeline
		}
	}
	svc.RecordModerationReceipt(pipeline, receipt.Outcome)
	c.Set(contentModerationForwardConflictRecorderContextKey, func(decision string) {
		svc.RecordModerationForwardConflict(decision)
	})
}

func recordContentModerationForwardConflict(c *gin.Context) {
	if c == nil {
		return
	}
	decision := "missing"
	if receipt, ok := moderationcoverage.ModerationReceiptFromContext(c); ok {
		switch receipt.Outcome {
		case "deferred", "deferred_selected_account":
			decision = "deferred"
		default:
			decision = "blocked"
		}
	}
	value, ok := c.Get(contentModerationForwardConflictRecorderContextKey)
	if !ok {
		return
	}
	record, ok := value.(func(string))
	if ok && record != nil {
		record(decision)
	}
}

func markSelectedAccountPipelineAdmission(c *gin.Context) {
	if c == nil || !moderationcoverage.ModerationReceiptAllowsForward(c) {
		return
	}
	meta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok || meta.Pipeline == "" {
		return
	}
	moderationcoverage.MarkPipelineAdmittedAfterModeration(c, meta.Pipeline, moderationcoverage.StagePreForward, "ContentModeration.CheckAccountAttempt")
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
