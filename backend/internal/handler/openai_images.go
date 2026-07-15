package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Images handles OpenAI Images API requests.
// POST /v1/images/generations
// POST /v1/images/edits
func (h *OpenAIGatewayHandler) Images(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

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
		"handler.openai_gateway.images",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var parsed *service.OpenAIImagesRequest
	var imageReleaseFunc func()
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIImages); ok {
		body = preForwardRequest.Body
		parsed = preForwardRequest.ImagesRequest
		imageReleaseFunc = preForwardRequest.ImageReleaseFunc
	} else {
		var err error
		body, err = pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
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

		setOpsRequestContext(c, "", false)

		parsed, err = h.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
	}
	if parsed == nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}
	requestModel := parsed.Model

	reqLog = reqLog.With(
		zap.String("model", requestModel),
		zap.Bool("stream", parsed.Stream),
		zap.Bool("multipart", parsed.Multipart),
		zap.String("capability", string(parsed.RequiredCapability)),
	)

	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
	}

	if parsed.Multipart {
		setOpsRequestContext(c, requestModel, parsed.Stream)
	} else {
		setOpsRequestContext(c, requestModel, parsed.Stream)
	}
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(parsed.Stream, false)))

	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, requestModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, parsed.Stream, &streamStarted, reqLog)
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
		StreamStarted:  streamStarted,
		ErrorComponent: "openai.images.billing_eligibility_check_failed",
	}); billingStage.Stop {
		return
	}

	sessionHash := h.gatewayService.GenerateExplicitSessionHash(c, body)
	requestCtx := service.WithOpenAIImageGenerationIntent(c.Request.Context())

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	stopJSONKeepalive := func() {}
	jsonKeepaliveStarted := false
	defer func() { stopJSONKeepalive() }()
	var oauth429FailoverState service.OpenAIOAuth429FailoverState

	for {
		var account *service.Account
		var accountReleaseFunc func()
		routingRetry := false
		if routingStage := h.runOpenAIHTTPRoutingStage(c, OpenAIHTTPRoutingStage{
			Handler:                    h,
			RequestContext:             requestCtx,
			ReqLog:                     reqLog,
			APIKey:                     apiKey,
			SubjectUserID:              subject.UserID,
			RequestedModel:             requestModel,
			SessionHash:                &sessionHash,
			FailedAccountIDs:           failedAccountIDs,
			RequiredImageCapability:    parsed.RequiredCapability,
			Stream:                     parsed.Stream,
			StreamStarted:              &streamStarted,
			MaxAccountSwitches:         maxAccountSwitches,
			SwitchCount:                &switchCount,
			LastFailoverErr:            lastFailoverErr,
			UseSimpleFailoverExhausted: true,
			NoAccountMessage:           "No available compatible accounts",
			LogPrefix:                  "openai.images",
			Account:                    &account,
			AccountReleaseFunc:         &accountReleaseFunc,
			Retry:                      &routingRetry,
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		if !parsed.Stream && !jsonKeepaliveStarted {
			stopJSONKeepalive = service.StartOpenAIImagesJSONKeepalive(c, h.openAIImagesJSONKeepaliveInterval())
			jsonKeepaliveStarted = true
		}
		forwardStart := time.Now()
		var writerSizeBeforeForward int
		var result *service.OpenAIForwardResult
		var err error
		stageResult := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:          h.gatewayService,
			Kind:                    OpenAIHTTPForwardImages,
			RequestContext:          requestCtx,
			Account:                 account,
			Body:                    body,
			ParsedImagesRequest:     parsed,
			ChannelMappedModel:      channelMapping.MappedModel,
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
		if result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			requestPayloadHash := service.HashUsageRequestPayload(body)
			if parsed.Multipart {
				requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
			}
			h.runOpenAIHTTPFailedUsageStage(c, OpenAIHTTPUsageStage{
				Handler:            h,
				RequestContext:     requestCtx,
				Result:             result,
				APIKey:             apiKey,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    GetInboundEndpoint(c),
				UpstreamEndpoint:   resolveOpenAIUpstreamEndpoint(c, account, result),
				UserAgent:          c.GetHeader("User-Agent"),
				ClientIP:           ip.GetClientIP(c),
				RequestPayloadHash: requestPayloadHash,
				QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
				ChannelUsageFields: channelMapping.ToUsageFields(requestModel, resultUpstreamModel(result)),
				LogComponent:       "handler.openai_gateway.images",
				LogMessage:         "openai.images.failed_upstream_usage_record_failed",
				LogUserID:          subject.UserID,
				LogModel:           requestModel,
			})
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.images.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
				return
			} else {
				var imageUpstreamErr *service.OpenAIImagesUpstreamError
				if errors.As(err, &imageUpstreamErr) {
					retryableServerError := service.IsOpenAIImagesRetryableUpstreamError(imageUpstreamErr)
					h.runOpenAIHTTPScheduleResultStage(c, account, !retryableServerError, nil)
					logEvent := "openai.images.upstream_user_error"
					if retryableServerError {
						logEvent = "openai.images.upstream_server_error_after_flush"
					}
					reqLog.Warn(logEvent,
						zap.Int64("account_id", account.ID),
						zap.Int("status_code", imageUpstreamErr.StatusCode),
						zap.String("error_type", imageUpstreamErr.ErrorType),
						zap.String("error_code", imageUpstreamErr.Code),
						zap.Error(err),
					)
					if !retryableServerError {
						return
					}
					err = &service.UpstreamFailoverError{StatusCode: imageUpstreamErr.StatusCode}
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					h.runOpenAIHTTPScheduleResultStage(c, account, false, nil)
					if service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) != writerSizeBeforeForward {
						reqLog.Warn("openai.images.upstream_failover_skipped_after_flush",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverClientGone(c) {
						reqLog.Info("openai.images.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai.images.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-requestCtx.Done():
								return
							case <-time.After(sameAccountRetryDelay):
							}
							continue
						}
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					failedAccountIDs[account.ID] = struct{}{}
					h.gatewayService.CooldownUserAccount(c.Request.Context(), subject.UserID, account.ID, h.gatewayService.UserAccountCooldownTTL(c.Request.Context()))
					lastFailoverErr = failoverErr
					if switchCount >= maxAccountSwitches {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					reqLog.Warn("openai.images.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				h.runOpenAIHTTPScheduleResultStage(c, account, false, nil)
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
				}
				fields := []zap.Field{
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				}
				if shouldLogOpenAIForwardFailureAsWarn(c, wroteFallback) {
					reqLog.Warn("openai.images.forward_failed", fields...)
					return
				}
				reqLog.Error("openai.images.forward_failed", fields...)
				return
			}
		}
		if result != nil && account.Type == service.AccountTypeOAuth && !account.IsShadow() {
			h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		if parsed.Multipart {
			requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
		}
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		upstreamModel := ""
		if result != nil {
			upstreamModel = result.UpstreamModel
		}
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
			RequestPayloadHash: requestPayloadHash,
			QuotaPlatform:      quotaPlatform,
			ChannelUsageFields: channelMapping.ToUsageFields(requestModel, upstreamModel),
			ScheduleSuccess:    &scheduleSucceeded,
			Mandatory:          true,
			LogComponent:       "handler.openai_gateway.images",
			LogMessage:         "openai.images.record_usage_failed",
			LogUserID:          subject.UserID,
			LogModel:           requestModel,
		})

		reqLog.Debug("openai.images.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func (h *OpenAIGatewayHandler) openAIImagesJSONKeepaliveInterval() time.Duration {
	if h.cfg == nil || h.cfg.Gateway.ImageNonstreamKeepaliveInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.ImageNonstreamKeepaliveInterval) * time.Second
}

func isMultipartImagesContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data")
}
