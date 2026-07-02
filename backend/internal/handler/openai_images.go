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

	for {
		var account *service.Account
		var accountReleaseFunc func()
		routingRetry := false
		if routingStage := h.runOpenAIHTTPRoutingStage(c, RoutingStageAdapter{
			Routing: func(*gin.Context) ExecutableStageResult {
				reqLog.Debug("openai.images.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
				selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForImages(
					requestCtx,
					apiKey.GroupID,
					sessionHash,
					requestModel,
					failedAccountIDs,
					parsed.RequiredCapability,
					subject.UserID,
				)
				if err != nil {
					reqLog.Warn("openai.images.account_select_failed",
						zap.Error(err),
						zap.Int("excluded_account_count", len(failedAccountIDs)),
					)
					if len(failedAccountIDs) == 0 {
						cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformOpenAI)
						if !cls.ModelNotFound {
							markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
						}
						message := cls.Message
						if !cls.ModelNotFound {
							message = "No available compatible accounts"
						}
						h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
						return openAIHTTPExecutableStageResult{Stop: true, Err: err}
					}
					if lastFailoverErr != nil {
						h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
					} else {
						h.handleFailoverExhaustedSimple(c, 502, streamStarted)
					}
					return openAIHTTPExecutableStageResult{Stop: true, Err: err}
				}
				if selection == nil || selection.Account == nil {
					cls := classifyNoAccountErrorFromGin(c, h.gatewayService, apiKey, requestModel, requestModel, service.PlatformOpenAI)
					if !cls.ModelNotFound {
						markOpsRoutingCapacityLimited(c)
					}
					message := cls.Message
					if !cls.ModelNotFound {
						message = "No available compatible accounts"
					}
					h.handleStreamingAwareError(c, cls.Status, cls.ErrType, message, streamStarted)
					return openAIHTTPExecutableStageResult{Stop: true}
				}

				reqLog.Debug("openai.images.account_schedule_decision",
					zap.String("layer", scheduleDecision.Layer),
					zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
					zap.Int("candidate_count", scheduleDecision.CandidateCount),
					zap.Int("top_k", scheduleDecision.TopK),
					zap.Int64("latency_ms", scheduleDecision.LatencyMs),
					zap.Float64("load_skew", scheduleDecision.LoadSkew),
				)

				account = selection.Account
				sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
				reqLog.Debug("openai.images.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
				setOpsSelectedAccount(c, account.ID, account.Platform)

				releaseFunc, refreshedAccount, acquired, retryable := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, requestModel, false, "", parsed.RequiredCapability, parsed.Stream, &streamStarted, reqLog)
				if !acquired {
					if retryable && switchCount < maxAccountSwitches {
						failedAccountIDs[account.ID] = struct{}{}
						switchCount++
						routingRetry = true
						reqLog.Info("openai.images.concurrency_fallback",
							zap.Int64("failed_account_id", account.ID),
							zap.Int("switch_count", switchCount),
						)
						return openAIHTTPExecutableStageResult{}
					}
					if retryable {
						h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many concurrent requests, please retry later", streamStarted)
					}
					return openAIHTTPExecutableStageResult{Stop: true}
				}
				accountReleaseFunc = releaseFunc
				account = refreshedAccount
				return openAIHTTPExecutableStageResult{}
			},
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
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
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.images.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var imageUpstreamErr *service.OpenAIImagesUpstreamError
				if errors.As(err, &imageUpstreamErr) {
					retryableServerError := service.IsOpenAIImagesRetryableUpstreamError(imageUpstreamErr)
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, !retryableServerError, nil)
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
					return
				}
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
					if c.Writer.Size() != writerSizeBeforeForward {
						reqLog.Warn("openai.images.upstream_failover_skipped_after_flush",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						h.handleFailoverExhausted(c, failoverErr, true)
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
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount) {
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
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, false, nil)
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
		if result != nil {
			if account.Type == service.AccountTypeOAuth {
				h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
			}
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, result.FirstTokenMs)
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, true, nil)
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		if parsed.Multipart {
			requestPayloadHash = service.HashUsageRequestPayload([]byte(parsed.StickySessionSeed()))
		}
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := GetUpstreamEndpoint(c, account.Platform)

		upstreamModel := ""
		if result != nil {
			upstreamModel = result.UpstreamModel
		}
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
			ChannelUsageFields: channelMapping.ToUsageFields(requestModel, upstreamModel),
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

func isMultipartImagesContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "multipart/form-data")
}
