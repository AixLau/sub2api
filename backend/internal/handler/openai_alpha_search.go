package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// AlphaSearch proxies the standalone search endpoint used by Codex Responses Lite.
func (h *OpenAIGatewayHandler) AlphaSearch(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	setOpenAIClientTransportHTTP(c)
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group.Platform != service.PlatformOpenAI && apiKey.Group.Platform != service.PlatformComposite {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search is only available for OpenAI and Composite groups")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.alpha_search",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var moderationBody []byte
	var requestedModel string
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIResponses); ok {
		body = preForwardRequest.Body
		moderationBody = preForwardRequest.contentModerationBody()
		requestedModel = strings.TrimSpace(preForwardRequest.Model)
	} else {
		var err error
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
				return
			}
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
			return
		}
		if len(body) == 0 {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
			return
		}
		if !gjson.ValidBytes(body) {
			logRequestBodyParseFailure(reqLog, body, nil)
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
			return
		}
		modelResult := gjson.GetBytes(body, "model")
		if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
			return
		}
		requestedModel = strings.TrimSpace(modelResult.String())
		moderationBody = service.OpenAIAlphaSearchModerationBody(body)
	}
	if !compositeTargetPlatformAllowed(c, apiKey, requestedModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Codex alpha search only supports OpenAI models for Composite groups")
		return
	}
	reqLog = reqLog.With(zap.String("model", requestedModel))
	setOpsRequestContext(c, requestedModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, requestedModel, moderationBody); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestedModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}

	if billingStage := h.runOpenAIHTTPBillingStage(c, OpenAIHTTPBillingStage{
		Handler:        h,
		ReqLog:         reqLog,
		APIKey:         apiKey,
		Subscription:   subscription,
		StreamStarted:  false,
		ErrorComponent: "openai_alpha_search.billing_eligibility_check_failed",
	}); billingStage.Stop {
		return
	}

	searchID := strings.TrimSpace(gjson.GetBytes(body, "id").String())
	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(c, nil, searchID)
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError
	switchCount := 0
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	routingStart := time.Now()

	// 分组利润控制：alpha search 文本入口请求级装门并固定 pricingAt
	//（记录路径经 service.OpenAIPricingAtFromContext 从请求 ctx 回读）。
	asPricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(asPricingCtx)

	for {
		var account *service.Account
		var accountRelease func()
		if routingStage := h.runOpenAIHTTPRoutingStage(c, OpenAIHTTPRoutingStage{
			Handler:              h,
			ReqLog:               reqLog,
			APIKey:               apiKey,
			SubjectUserID:        subject.UserID,
			RequestedModel:       requestedModel,
			SessionHash:          &sessionHash,
			FailedAccountIDs:     failedAccountIDs,
			RequiredTransport:    service.OpenAIUpstreamTransportHTTPSSE,
			RequiredCapability:   service.OpenAIEndpointCapabilityAlphaSearch,
			UseUpstreamTokenCost: false,
			RequestPlatform:      service.PlatformOpenAI,
			StreamStarted:        &streamStarted,
			LastFailoverErr:      lastFailoverErr,
			ProfitVetoCount:      &profitVetoCount,
			LogPrefix:            "openai_alpha_search",
			Account:              &account,
			AccountReleaseFunc:   &accountRelease,
		}); routingStage.Stop {
			return
		}
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		writerSizeBeforeForward := c.Writer.Size()
		forwardStart := time.Now()
		var result *service.OpenAIForwardResult
		forwardStage := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:          h.gatewayService,
			Kind:                    OpenAIHTTPForwardAlphaSearch,
			Account:                 account,
			Body:                    forwardBody,
			ReleaseFunc:             accountRelease,
			WriterSizeBeforeForward: &writerSizeBeforeForward,
			Result:                  &result,
		})
		err := forwardStage.Err
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, time.Since(forwardStart).Milliseconds())

		if err == nil {
			scheduleSuccess := true
			upstreamModel := ""
			if result != nil {
				upstreamModel = result.UpstreamModel
			}
			h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
				Handler:            h,
				Result:             result,
				APIKey:             apiKey,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    GetInboundEndpoint(c),
				UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
				UserAgent:          c.GetHeader("User-Agent"),
				ClientIP:           ip.GetClientIP(c),
				RequestPayloadHash: service.HashUsageRequestPayload(body),
				ChannelUsageFields: channelMapping.ToUsageFields(requestedModel, upstreamModel),
				ScheduleSuccess:    &scheduleSuccess,
				Mandatory:          true,
				LogComponent:       "handler.openai_gateway.alpha_search",
				LogMessage:         "openai_alpha_search.record_usage_failed",
				LogUserID:          subject.UserID,
				LogModel:           requestedModel,
			})
			return
		}

		h.runOpenAIHTTPFailedUsageStage(c, OpenAIHTTPUsageStage{
			Handler:            h,
			RequestContext:     c.Request.Context(),
			Result:             result,
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    GetInboundEndpoint(c),
			UpstreamEndpoint:   resolveOpenAIUpstreamEndpoint(c, account, result),
			UserAgent:          c.GetHeader("User-Agent"),
			ClientIP:           ip.GetClientIP(c),
			RequestPayloadHash: service.HashUsageRequestPayload(body),
			QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
			ChannelUsageFields: channelMapping.ToUsageFields(requestedModel, resultUpstreamModel(result)),
			LogComponent:       "handler.openai_gateway.alpha_search",
			LogMessage:         "openai_alpha_search.failed_upstream_usage_record_failed",
			LogUserID:          subject.UserID,
			LogModel:           requestedModel,
		})

		var failoverErr *service.UpstreamFailoverError
		if !errors.As(err, &failoverErr) {
			h.runOpenAIHTTPScheduleResultStage(c, account, requestedModel, false, nil)
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_alpha_search.forward_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			return
		}

		h.runOpenAIHTTPScheduleResultStage(c, account, requestedModel, false, nil)
		if c.Writer.Size() != writerSizeBeforeForward {
			h.handleFailoverExhausted(c, failoverErr, true)
			return
		}
		if failoverClientGone(c) {
			reqLog.Info("openai_alpha_search.failover_aborted_client_disconnected",
				zap.Int64("account_id", account.ID),
				zap.Int("upstream_status", failoverErr.StatusCode),
			)
			return
		}
		h.gatewayService.RecordOpenAIAccountSwitch()
		failedAccountIDs[account.ID] = struct{}{}
		lastFailoverErr = failoverErr
		if switchCount >= h.maxAccountSwitches {
			h.handleFailoverExhausted(c, failoverErr, false)
			return
		}
		switchCount++
		if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
			h.handleFailoverExhausted(c, failoverErr, false)
			return
		}
		reqLog.Warn("openai_alpha_search.upstream_failover_switching",
			zap.Int64("account_id", account.ID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("switch_count", switchCount),
		)
	}
}

// recordAlphaSearchUsage 为一次成功的 alpha/search 网页搜索落按次计费用量行
// （上游不返回 usage 字段，按 WebSearchCalls 走分组单价 × 倍率的按次口径）。
// 与 images 一致使用 mandatory 池提交，池满时同步兜底执行，保证扣费不丢。
func (h *OpenAIGatewayHandler) recordAlphaSearchUsage(
	c *gin.Context,
	apiKey *service.APIKey,
	account *service.Account,
	subscription *service.UserSubscription,
	channelMapping service.ChannelMappingResult,
	requestedModel string,
	body []byte,
	result *service.OpenAIForwardResult,
	userID int64,
) {
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	sessionID := service.ExtractClientSessionID(c)
	requestPayloadHash := service.HashUsageRequestPayload(body)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
			Result:             result,
			APIKey:             apiKey,
			User:               apiKey.User,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			SessionID:          sessionID,
			PhaseLatency:       service.UsagePhaseLatencySnapshot(c),
			ChannelUsageFields: channelMapping.ToUsageFields(requestedModel, result.UpstreamModel),
			PricingAt:          service.OpenAIPricingAtFromContext(c.Request.Context()),
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.alpha_search"),
				zap.Int64("user_id", userID),
				zap.Int64("api_key_id", apiKey.ID),
				zap.Any("group_id", apiKey.GroupID),
				zap.String("model", requestedModel),
				zap.Int64("account_id", account.ID),
			).Error("openai_alpha_search.record_usage_failed", zap.Error(err))
		}
	})
}
