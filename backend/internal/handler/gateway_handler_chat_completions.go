package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/clientmsg"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API endpoint for Anthropic platform groups.
// POST /v1/chat/completions
// This converts Chat Completions requests to Anthropic format (via Responses format chain),
// forwards to Anthropic upstream, and converts responses back to Chat Completions format.
func (h *GatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.chatCompletionsErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	var body []byte
	var reqModel string
	var reqStream bool
	if preForwardRequest, ok := gatewayPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIChat); ok {
		body = preForwardRequest.Body
		reqModel = preForwardRequest.Model
		reqStream = preForwardRequest.Stream
	} else {
		var ok bool
		body, reqModel, reqStream, ok = h.readOpenAICompatibleGatewayPreForwardRequest(c, h.chatCompletionsErrorResponse, reqLog)
		if !ok {
			return
		}
	}
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformResolved(c, apiKey, reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by composite groups")
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.chatCompletionsErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	// Claude Code only restriction
	if apiKey.Group != nil && apiKey.Group.ClaudeCodeOnly {
		h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group is restricted to Claude Code clients (/v1/messages only)")
		return
	}

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	// Error passthrough binding
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())

	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted)
	if err != nil {
		reqLog.Warn("gateway.cc.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", streamStarted)
		return
	}
	userReleaseFunc = wrapReleaseOnDone(c.Request.Context(), userReleaseFunc)
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing
	billingStage := h.runGatewayBillingStage(c, GatewayBillingStage{
		Handler:        h,
		RequestContext: c.Request.Context(),
		APIKey:         apiKey,
		Group:          apiKey.Group,
		Subscription:   subscription,
	})
	if err := billingStage.Err; err != nil {
		reqLog.Info("gateway.cc.billing_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetailsForContext(c, err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.chatCompletionsErrorResponse(c, status, code, message)
		return
	}

	// Parse request for session hash
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, _ := service.ParseGatewayRequest(bodyRef, "chat_completions")
	if parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: reqModel, Stream: reqStream, Body: bodyRef}
	}
	parsedReq.SessionContext = &service.SessionContext{
		ClientIP:  ip.GetClientIP(c),
		UserAgent: c.GetHeader("User-Agent"),
		APIKeyID:  apiKey.ID,
	}
	sessionHash := h.gatewayService.GenerateSessionHash(parsedReq)
	groupPlatform := effectiveAPIKeyPlatform(c, apiKey)
	selectionSessionHash := sessionHash
	if groupPlatform == service.PlatformGemini && selectionSessionHash != "" {
		selectionSessionHash = "gemini:" + selectionSessionHash
	}

	// 3. Account selection + failover loop
	fs := NewFailoverState(h.maxAccountSwitches, false)
	if groupPlatform == service.PlatformGemini {
		fs = NewFailoverState(h.maxAccountSwitchesGemini, false)
	}

	for {
		if c.Request.Context().Err() != nil {
			return
		}
		var selection *service.AccountSelectionResult
		routingStage := h.runGatewayRoutingStage(c, GatewayRoutingStage{
			Handler:          h,
			RequestContext:   c.Request.Context(),
			GroupID:          apiKey.GroupID,
			SessionHash:      selectionSessionHash,
			Model:            reqModel,
			FailedAccountIDs: fs.FailedAccountIDs,
			Sub2APIUserID:    subject.UserID,
			Selection:        &selection,
		})
		if routingStage.Stop {
			return
		}
		err := routingStage.Err
		if err != nil {
			if len(fs.FailedAccountIDs) == 0 {
				cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel, groupPlatform)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				message := cls.Message
				if !cls.ModelNotFound {
					message = "No available accounts: " + err.Error()
				}
				h.chatCompletionsErrorResponse(c, cls.Status, cls.ErrType, message)
				return
			}
			action := fs.HandleSelectionExhausted(c.Request.Context())
			switch action {
			case FailoverContinue:
				continue
			case FailoverCanceled:
				failoverClientGone(c)
				return
			default:
				if fs.LastFailoverErr != nil {
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
				} else {
					h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "server_error", "All available accounts exhausted")
				}
				return
			}
		}
		account := selection.Account
		setOpsSelectedAccount(c, account.ID, account.Platform)

		// 4. Acquire account concurrency slot
		accountReleaseFunc := selection.ReleaseFunc
		if !selection.Acquired {
			if selection.WaitPlan == nil {
				markOpsRoutingCapacityLimited(c)
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			// Dynamic timeout: shorter wait when alternatives exist
			effectiveTimeout := selection.WaitPlan.Timeout
			if selection.CandidateCount > 1 {
				effectiveTimeout = 5 * time.Second
				reqLog.Debug("gateway.cc.using_short_wait_timeout",
					zap.Int("candidate_count", selection.CandidateCount),
					zap.Duration("timeout", effectiveTimeout),
				)
			}

			accountReleaseFunc, err = h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
				c,
				account.ID,
				selection.WaitPlan.MaxConcurrency,
				effectiveTimeout,
				reqStream,
				&streamStarted,
			)
			if err != nil {
				reqLog.Warn("gateway.cc.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
				h.handleConcurrencyError(c, err, "account", streamStarted)
				return
			}
			refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(c.Request.Context(), account, reqModel)
			if refreshErr != nil {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				markOpsRoutingCapacityLimited(c)
				reqLog.Info("gateway.cc.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
				h.chatCompletionsErrorResponse(c, http.StatusServiceUnavailable, "api_error", "No available accounts")
				return
			}
			account = refreshed
			selection.Account = refreshed
		}
		accountReleaseFunc = wrapReleaseOnDone(c.Request.Context(), accountReleaseFunc)

		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}

		// 5. Forward request
		writerSizeBeforeForward := c.Writer.Size()
		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		var result *service.ForwardResult
		if account.Platform == service.PlatformGemini {
			if h.geminiCompatService == nil {
				h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", "Gemini compatibility service is not configured")
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
				return
			}
		}
		stageResult := h.runGatewayForwardStage(c, GatewayChatCompletionsForwardStage{
			GatewayService:      h.gatewayService,
			GeminiCompatService: h.geminiCompatService,
			RequestContext:      c.Request.Context(),
			Account:             account,
			ParsedRequest:       parsedReq,
			Body:                forwardBody,
			Result:              &result,
		})
		err = stageResult.Err

		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}

		if err != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(err, &failoverErr) {
				if c.Writer.Size() != writerSizeBeforeForward {
					h.handleCCFailoverExhausted(c, failoverErr, true)
					return
				}
				action := fs.HandleFailoverErrorForUser(c.Request.Context(), h.gatewayService, subject.UserID, account.ID, account.Platform, account.GetPoolModeRetryCount(), failoverErr)
				switch action {
				case FailoverContinue:
					continue
				case FailoverExhausted:
					h.handleCCFailoverExhausted(c, fs.LastFailoverErr, streamStarted)
					return
				case FailoverCanceled:
					failoverClientGone(c)
					return
				}
			}
			upstreamErrorAlreadyCommunicated := gatewayForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
			wroteFallback := false
			if !upstreamErrorAlreadyCommunicated {
				wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
			}
			reqLog.Error("gateway.cc.forward_failed",
				zap.Int64("account_id", account.ID),
				zap.Bool("fallback_error_response_written", wroteFallback),
				zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
				zap.Error(err),
			)
			return
		}

		// 6. Record usage
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		_ = h.runGatewayUsageStage(c, GatewayUsageStage{
			Handler:            h,
			RequestContext:     c.Request.Context(),
			Result:             result,
			QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			ClientIP:           clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			LogComponent:       "gateway.chat_completions.record_usage",
			LogMessage:         "gateway.cc.record_usage_failed",
			LogUserID:          subject.UserID,
			LogModel:           reqModel,
		})
		return
	}
}

// chatCompletionsErrorResponse writes an error in OpenAI Chat Completions format.
func (h *GatewayHandler) chatCompletionsErrorResponse(c *gin.Context, status int, errType, message string) {
	markOpsClientMessageDiagnostic(c, errType, message)
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": clientmsg.Localize(message),
		},
	})
}

// handleCCFailoverExhausted writes a failover-exhausted error in CC format.
func (h *GatewayHandler) handleCCFailoverExhausted(c *gin.Context, lastErr *service.UpstreamFailoverError, streamStarted bool) {
	if streamStarted {
		return
	}
	if lastErr != nil {
		copyFailoverRetryAfter(c, lastErr.ResponseHeaders)
	}
	if lastErr != nil && lastErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(lastErr)
		h.chatCompletionsErrorResponse(c, status, "server_error", message)
		return
	}
	statusCode := http.StatusBadGateway
	if lastErr != nil && lastErr.StatusCode > 0 {
		statusCode = lastErr.StatusCode
	}
	if lastErr != nil && service.IsOpenAISilentRefusalErrorBody(lastErr.ResponseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.chatCompletionsErrorResponse(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage())
		return
	}
	h.chatCompletionsErrorResponse(c, statusCode, "server_error", "All available accounts exhausted")
}
