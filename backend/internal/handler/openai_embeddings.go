package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// Embeddings handles the OpenAI-compatible Embeddings API.
// POST /v1/embeddings
func (h *OpenAIGatewayHandler) Embeddings(c *gin.Context) {
	streamStarted := false
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.embeddings",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var reqModel string
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIEmbeddings); ok {
		body = preForwardRequest.Body
		reqModel = preForwardRequest.Model
	} else {
		var err error
		body, err = readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
				return
			}
			markOpsRequestBodyReadError(c, err)
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
		reqModel = modelResult.String()
	}

	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformAllowed(c, apiKey, reqModel, service.PlatformOpenAI) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel))
	setOpsRequestContext(c, reqModel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "openai_embeddings", reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if billingStage := h.runOpenAIHTTPBillingStage(c, OpenAIHTTPBillingStage{
		Handler:        h,
		ReqLog:         reqLog,
		APIKey:         apiKey,
		Subscription:   subscription,
		ErrorComponent: "openai_embeddings.billing_check_failed",
		ErrorResponder: h.errorResponse,
	}); billingStage.Stop {
		return
	}

	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError
	switchCount := 0
	maxAccountSwitches := h.maxAccountSwitches
	if maxAccountSwitches <= 0 {
		maxAccountSwitches = 3
	}
	routingStart := time.Now()

	// 分组利润控制：embeddings 文本入口请求级装门并固定 pricingAt。
	embPricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(embPricingCtx)

	for {
		var account *service.Account
		var accountReleaseFunc func()
		routingRetry := false
		if routingStage := h.runOpenAIHTTPRoutingStage(c, OpenAIHTTPRoutingStage{
			Handler:              h,
			ReqLog:               reqLog,
			APIKey:               apiKey,
			SubjectUserID:        subject.UserID,
			RequestedModel:       reqModel,
			FailedAccountIDs:     failedAccountIDs,
			RequiredTransport:    service.OpenAIUpstreamTransportHTTPSSE,
			RequiredCapability:   service.OpenAIEndpointCapabilityEmbeddings,
			UseUpstreamTokenCost: true,
			RequestPlatform:      requestPlatform,
			MaxAccountSwitches:   maxAccountSwitches,
			SwitchCount:          &switchCount,
			LastFailoverErr:      lastFailoverErr,
			ProfitVetoCount:      &profitVetoCount,
			ErrorFormat:          openAIHTTPRoutingErrorEmbeddings,
			LogPrefix:            "openai_embeddings",
			Account:              &account,
			AccountReleaseFunc:   &accountReleaseFunc,
			Retry:                &routingRetry,
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var writerSizeBeforeForward int
		var result *service.OpenAIForwardResult
		var err error
		stageResult := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:          h.gatewayService,
			Kind:                    OpenAIHTTPForwardEmbeddings,
			Account:                 account,
			Body:                    forwardBody,
			ReleaseFunc:             accountReleaseFunc,
			WriterSizeBeforeForward: &writerSizeBeforeForward,
			Result:                  &result,
		})
		err = stageResult.Err

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)

		if err != nil {
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
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, resultUpstreamModel(result)),
				LogComponent:       "handler.openai_gateway.embeddings",
				LogMessage:         "openai_embeddings.failed_upstream_usage_record_failed",
				LogUserID:          subject.UserID,
				LogModel:           reqModel,
			})
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleFailoverExhausted(c, failoverErr, true)
					return
				}
				h.runOpenAIHTTPScheduleResultStage(c, account, reqModel, false, result, false, nil, err)
				if failoverClientGone(c) {
					reqLog.Info("openai_embeddings.failover_aborted_client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
					)
					return
				}
				h.gatewayService.RecordOpenAIAccountSwitch()
				failedAccountIDs[account.ID] = struct{}{}
				h.gatewayService.CooldownUserAccount(c.Request.Context(), subject.UserID, account.ID, h.gatewayService.UserAccountCooldownTTL(c.Request.Context()))
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					h.handleFailoverExhausted(c, failoverErr, false)
					return
				}
				switchCount++
				reqLog.Warn("openai_embeddings.upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				continue
			}
			h.runOpenAIHTTPScheduleResultStage(c, account, reqModel, false, result, false, nil, err)
			if c.Writer.Size() == writerSizeBeforeForward {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Upstream request failed")
			}
			reqLog.Warn("openai_embeddings.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(err),
			)
			return
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)

		scheduleSucceeded := true
		_ = h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
			Handler:            h,
			RequestContext:     c.Request.Context(),
			Result:             result,
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			ClientIP:           clientIP,
			SessionID:          sessionID,
			QuotaPlatform:      quotaPlatform,
			ScheduleSuccess:    &scheduleSucceeded,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			LogComponent:       "handler.openai_gateway.embeddings",
			LogMessage:         "openai_embeddings.record_usage_failed",
			LogUserID:          subject.UserID,
			LogModel:           reqModel,
		})
		reqLog.Debug("openai_embeddings.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}
