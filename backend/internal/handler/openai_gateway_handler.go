package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientmsg"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// OpenAIGatewayHandler handles OpenAI API gateway requests
type OpenAIGatewayHandler struct {
	gatewayService           *service.OpenAIGatewayService
	billingCacheService      *service.BillingCacheService
	apiKeyService            *service.APIKeyService
	usageRecordWorkerPool    *service.UsageRecordWorkerPool
	errorPassthroughService  *service.ErrorPassthroughService
	contentModerationService *service.ContentModerationService
	moderationGuard          moderationGuard
	pipeline                 *OpenAIGatewayPipeline
	forwardStageRegistry     *ForwardStageRegistry
	stageAdapterRegistry     *StageAdapterRegistry
	opsService               *service.OpsService
	concurrencyHelper        *ConcurrencyHelper
	imageLimiter             *imageConcurrencyLimiter
	imageUserLimiterMu       sync.Mutex
	imageUserLimiters        map[int64]*imageConcurrencyLimiter
	maxAccountSwitches       int
	cfg                      *config.Config
}

func resolveOpenAIMessagesDispatchMappedModel(apiKey *service.APIKey, requestedModel string) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return strings.TrimSpace(apiKey.Group.ResolveMessagesDispatchModel(requestedModel))
}

type openAIModelBodyReplaceFunc func([]byte, string) []byte

func openAIModelMappedBody(body []byte, mapped bool, mappedModel string, replace openAIModelBodyReplaceFunc) []byte {
	if !mapped || replace == nil {
		return body
	}
	return replace(body, mappedModel)
}

func newOpenAIModelMappedBodyCache(body []byte, replace openAIModelBodyReplaceFunc) func(bool, string) []byte {
	replacedBodies := make(map[string][]byte)
	return func(mapped bool, mappedModel string) []byte {
		if !mapped {
			return body
		}
		if cachedBody, ok := replacedBodies[mappedModel]; ok {
			return cachedBody
		}
		replacedBody := openAIModelMappedBody(body, true, mappedModel, replace)
		replacedBodies[mappedModel] = replacedBody
		return replacedBody
	}
}

func usageRecordContext(parent context.Context, base context.Context) context.Context {
	if base == nil {
		base = context.Background()
	}
	if parent == nil {
		return base
	}
	if clientRequestID, _ := parent.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
		base = context.WithValue(base, ctxkey.ClientRequestID, strings.TrimSpace(clientRequestID))
	}
	if requestID, _ := parent.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
		base = context.WithValue(base, ctxkey.RequestID, strings.TrimSpace(requestID))
	}
	return base
}

func wrapUsageRecordTaskContext(parent context.Context, task service.UsageRecordTask) service.UsageRecordTask {
	if task == nil {
		return nil
	}
	return func(ctx context.Context) {
		task(usageRecordContext(parent, ctx))
	}
}

func openAICompatibleRequestPlatform(apiKey *service.APIKey) string {
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == service.PlatformGrok {
		return service.PlatformGrok
	}
	return service.PlatformOpenAI
}

func openAIResponsesRequiredImageCapability(reqModel string, body []byte) service.OpenAIImagesCapability {
	if service.IsImageGenerationIntent("/v1/responses", reqModel, body) {
		return service.OpenAIImagesCapabilityResponsesImageTool
	}
	return ""
}

// NewOpenAIGatewayHandler creates a new OpenAIGatewayHandler
func NewOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	opsService *service.OpsService,
	cfg *config.Config,
) *OpenAIGatewayHandler {
	pingInterval := time.Duration(0)
	maxAccountSwitches := 3
	if cfg != nil {
		pingInterval = time.Duration(cfg.Concurrency.PingInterval) * time.Second
		if cfg.Gateway.MaxAccountSwitches > 0 {
			maxAccountSwitches = cfg.Gateway.MaxAccountSwitches
		}
	}
	guard := newContentModerationGuard(contentModerationService)
	stageRegistry := NewStageAdapterRegistry()
	return &OpenAIGatewayHandler{
		gatewayService:           gatewayService,
		billingCacheService:      billingCacheService,
		apiKeyService:            apiKeyService,
		usageRecordWorkerPool:    usageRecordWorkerPool,
		errorPassthroughService:  errorPassthroughService,
		contentModerationService: contentModerationService,
		moderationGuard:          guard,
		pipeline:                 newOpenAIGatewayPipeline(guard, gatewayService),
		forwardStageRegistry:     stageRegistry,
		stageAdapterRegistry:     stageRegistry,
		opsService:               opsService,
		concurrencyHelper:        NewConcurrencyHelper(concurrencyService, SSEPingFormatComment, pingInterval),
		imageLimiter:             &imageConcurrencyLimiter{},
		imageUserLimiters:        map[int64]*imageConcurrencyLimiter{},
		maxAccountSwitches:       maxAccountSwitches,
		cfg:                      cfg,
	}
}

// Responses handles OpenAI Responses API endpoint
// POST /openai/v1/responses
func (h *OpenAIGatewayHandler) Responses(c *gin.Context) {
	// 局部兜底：确保该 handler 内部任何 panic 都不会击穿到进程级。
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)
	compactStartedAt := time.Now()
	defer h.logOpenAIRemoteCompactOutcome(c, compactStartedAt)
	setOpenAIClientTransportHTTP(c)

	requestStart := time.Now()

	// Get apiKey and user from context (set by ApiKeyAuth middleware)
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
		"handler.openai_gateway.responses",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var sessionHashBody []byte
	var reqModel string
	var reqStream bool
	var imageReleaseFunc func()
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIResponses); ok {
		body = preForwardRequest.Body
		sessionHashBody = preForwardRequest.CyberBody
		if len(sessionHashBody) == 0 {
			sessionHashBody = body
		}
		reqModel = preForwardRequest.Model
		reqStream = preForwardRequest.Stream
		imageReleaseFunc = preForwardRequest.ImageReleaseFunc
	} else {
		body, reqModel, reqStream, sessionHashBody, _, ok = h.readOpenAIHTTPPreForwardRequest(c, reqLog, service.ContentModerationProtocolOpenAIResponses)
		if !ok {
			return
		}
	}
	// body-signal compact：上游 unary 等待期间向下游发 SSE 注释行心跳，防止
	// 反向代理空闲超时掐断长压缩连接（#3887）。首拍延迟一个心跳间隔，快速
	// 失败仍走 JSON+状态码链路；未标记客户端流式或间隔为 0 时是 no-op。
	stopCompactKeepalive := service.StartOpenAICompactSSEKeepalive(c, h.openAICompactKeepaliveInterval())
	defer stopCompactKeepalive()
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
	}

	requestCtx := c.Request.Context()
	requiredImageCapability := openAIResponsesRequiredImageCapability(reqModel, body)
	if requiredImageCapability != "" {
		requestCtx = service.WithOpenAIImageGenerationIntent(requestCtx)
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(requestCtx, apiKey.GroupID, reqModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)

	// 提前校验 function_call_output 是否具备可关联上下文，避免上游 400。
	if !h.validateFunctionCallOutputRequest(c, body, reqLog) {
		return
	}

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	// Get subscription info (may be nil)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(apiKey)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	// 确保请求取消时也会释放槽位，避免长连接被动中断造成泄漏
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	// 2. Re-check billing eligibility after wait
	if billingStage := h.runOpenAIHTTPBillingStage(c, OpenAIHTTPBillingStage{
		Handler:        h,
		ReqLog:         reqLog,
		APIKey:         apiKey,
		Subscription:   subscription,
		StreamStarted:  streamStarted,
		ErrorComponent: "openai.billing_eligibility_check_failed",
	}); billingStage.Stop {
		return
	}

	// Generate session hash (header first; fallback to prompt_cache_key)
	sessionHash := h.gatewayService.GenerateSessionHash(c, sessionHashBody)
	requireCompact := isOpenAIRemoteCompactPath(c)
	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError

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
			RequestedModel:             reqModel,
			SessionHash:                &sessionHash,
			PreviousResponseID:         previousResponseID,
			FailedAccountIDs:           failedAccountIDs,
			RequiredTransport:          service.OpenAIUpstreamTransportAny,
			RequiredCapability:         service.OpenAIEndpointCapabilityChatCompletions,
			RequiredImageCapability:    requiredImageCapability,
			RequireCompact:             requireCompact,
			RequestPlatform:            requestPlatform,
			Stream:                     reqStream,
			StreamStarted:              &streamStarted,
			MaxAccountSwitches:         maxAccountSwitches,
			SwitchCount:                &switchCount,
			LastFailoverErr:            lastFailoverErr,
			UseSimpleFailoverExhausted: true,
			LogPrefix:                  "openai",
			Account:                    &account,
			AccountReleaseFunc:         &accountReleaseFunc,
			Retry:                      &routingRetry,
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
		}

		// Forward request
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		var writerSizeBeforeForward int
		var result *service.OpenAIForwardResult
		var err error
		stageResult := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:          h.gatewayService,
			Kind:                    OpenAIHTTPForwardResponses,
			Account:                 account,
			Body:                    forwardBody,
			ReleaseFunc:             accountReleaseFunc,
			WriterSizeBeforeForward: &writerSizeBeforeForward,
			Result:                  &result,
		})
		err = stageResult.Err
		cyberBlockKeyHTTP := ""
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockKeyHTTP = service.CyberSessionBlockKey(apiKey.ID, c, sessionHashBody)
		}
		h.runOpenAIHTTPCyberUsageStage(c, OpenAIHTTPCyberUsageStageInput{
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			Model:              reqModel,
			ForwardErrored:     err != nil,
			CyberBlockKey:      cyberBlockKeyHTTP,
			ChannelUsageFields: channelMapping.ToUsageFields(reqModel, ""),
			RequestPayloadHash: service.HashUsageRequestPayload(body),
			RequestBody:        body,
		})
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
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
				ChannelUsageFields: channelMapping.ToUsageFields(reqModel, resultUpstreamModel(result)),
				LogComponent:       "handler.openai_gateway.responses",
				LogMessage:         "openai.failed_upstream_usage_record_failed",
				LogUserID:          subject.UserID,
				LogModel:           reqModel,
			})
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
				return
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					h.runOpenAIHTTPScheduleResultStage(c, account, false, nil)
					// 池模式：同账号重试
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-c.Request.Context().Done():
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
					reqLog.Warn("openai.upstream_failover_switching",
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
					reqLog.Warn("openai.forward_failed", fields...)
					return
				}
				reqLog.Error("openai.forward_failed", fields...)
				return
			}
		}
		if result != nil && account.Type == service.AccountTypeOAuth && !account.IsShadow() {
			h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(c.Request.Context(), account.ID, result.ResponseHeaders)
		}

		// 捕获请求信息（用于异步记录，避免在 goroutine 中访问 gin.Context）
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, result)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
		cyberBlocked := service.GetOpsCyberPolicy(c) != nil
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
			ChannelUsageFields: channelMapping.ToUsageFields(reqModel, result.UpstreamModel),
			CyberBlocked:       cyberBlocked,
			ScheduleSuccess:    &scheduleSucceeded,
			LogComponent:       "handler.openai_gateway.responses",
			LogMessage:         "openai.record_usage_failed",
			LogUserID:          subject.UserID,
			LogModel:           reqModel,
		})
		reqLog.Debug("openai.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func isOpenAIRemoteCompactPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	return strings.HasSuffix(normalizedPath, "/responses/compact")
}

// isBareOpenAIResponsesPath 仅匹配裸 /responses 端点（无 /compact 等子路径），
// body-signal 提升只允许发生在这里，避免误伤 /responses/{id}/... 形态的请求。
func isBareOpenAIResponsesPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	return strings.HasSuffix(normalizedPath, "/responses")
}

func isOpenAIRemoteCompactionV2Request(c *gin.Context, body []byte) bool {
	stream, valid := parseOpenAICompatibleStream(body)
	if !valid || !stream || c == nil || c.Request == nil {
		return false
	}
	for _, header := range c.Request.Header.Values("x-codex-beta-features") {
		for _, feature := range strings.Split(header, ",") {
			if strings.TrimSpace(feature) == "remote_compaction_v2" {
				return true
			}
		}
	}
	return false
}

// normalizeOpenAIResponsesCompactRequest keeps Codex remote compaction v2 on
// its native streaming /responses wire and preserves the legacy body-signal
// promotion for clients that do not explicitly advertise that protocol.
// 返回归一化后的 body；ok=false 表示错误响应已写出，调用方应直接 return。
func (h *OpenAIGatewayHandler) normalizeOpenAIResponsesCompactRequest(c *gin.Context, reqLog *zap.Logger, body []byte) ([]byte, bool) {
	isCompactRequest := service.IsOpenAIResponsesCompactPathForTest(c)
	if !isCompactRequest && isBareOpenAIResponsesPath(c) && service.HasCompactionTriggerInInput(body) {
		if isOpenAIRemoteCompactionV2Request(c, body) {
			return body, true
		}
		c.Request.URL.Path = strings.TrimRight(c.Request.URL.Path, "/") + "/compact"
		isCompactRequest = true
		clientStream := gjson.GetBytes(body, "stream").Bool()
		if clientStream {
			service.MarkOpenAICompactClientStream(c)
		}
		reqLog.Info("codex.remote_compact.detected_body_signal", zap.Bool("client_stream", clientStream))
	}
	if !isCompactRequest {
		return body, true
	}
	if compactSeed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); compactSeed != "" {
		c.Set(service.OpenAICompactSessionSeedKeyForTest(), compactSeed)
	}
	normalizedCompactBody, normalizedCompact, compactErr := service.NormalizeOpenAICompactRequestBodyForTest(body)
	if compactErr != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to normalize compact request body")
		return nil, false
	}
	if normalizedCompact {
		body = normalizedCompactBody
	}
	return body, true
}

func (h *OpenAIGatewayHandler) logOpenAIRemoteCompactOutcome(c *gin.Context, startedAt time.Time) {
	if !isOpenAIRemoteCompactPath(c) {
		return
	}

	var (
		ctx    = context.Background()
		path   string
		status int
	)
	if c != nil {
		if c.Request != nil {
			ctx = c.Request.Context()
			if c.Request.URL != nil {
				path = strings.TrimSpace(c.Request.URL.Path)
			}
		}
		if c.Writer != nil {
			status = c.Writer.Status()
		}
	}

	outcome := "failed"
	if status >= 200 && status < 300 {
		outcome = "succeeded"
	}
	// compact 心跳提交后失败的 wire 状态码固化为 200，真实结局以流内错误
	// 标记为准（response.failed 降级路径会 MarkOpsStreamError）。
	if outcome == "succeeded" && c != nil {
		if _, hasStreamErr := service.GetOpsStreamError(c); hasStreamErr {
			outcome = "failed"
		}
	}
	latencyMs := time.Since(startedAt).Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}

	fields := []zap.Field{
		zap.String("component", "handler.openai_gateway.responses"),
		zap.Bool("remote_compact", true),
		zap.String("compact_outcome", outcome),
		zap.Int("status_code", status),
		zap.Int64("latency_ms", latencyMs),
		zap.String("path", path),
		zap.Bool("force_codex_cli", h != nil && h.cfg != nil && h.cfg.Gateway.ForceCodexCLI),
	}

	if c != nil {
		if userAgent := strings.TrimSpace(c.GetHeader("User-Agent")); userAgent != "" {
			fields = append(fields, zap.String("request_user_agent", userAgent))
		}
		if v, ok := c.Get(opsModelKey); ok {
			if model, ok := v.(string); ok && strings.TrimSpace(model) != "" {
				fields = append(fields, zap.String("request_model", strings.TrimSpace(model)))
			}
		}
		if v, ok := c.Get(opsAccountIDKey); ok {
			if accountID, ok := v.(int64); ok && accountID > 0 {
				fields = append(fields, zap.Int64("account_id", accountID))
			}
		}
		if c.Writer != nil {
			if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("x-request-id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			} else if upstreamRequestID := strings.TrimSpace(c.Writer.Header().Get("X-Request-Id")); upstreamRequestID != "" {
				fields = append(fields, zap.String("upstream_request_id", upstreamRequestID))
			}
		}
	}

	log := logger.FromContext(ctx).With(fields...)
	if outcome == "succeeded" {
		log.Info("codex.remote_compact.succeeded")
		return
	}
	log.Warn("codex.remote_compact.failed")
}

// Messages handles Anthropic Messages API requests routed to OpenAI platform.
// POST /v1/messages (when group platform is OpenAI)
func (h *OpenAIGatewayHandler) Messages(c *gin.Context) {
	streamStarted := false
	defer h.recoverAnthropicMessagesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	// 检查分组是否允许 /v1/messages 调度
	if apiKey.Group != nil && apiKey.Group.Platform != service.PlatformGrok && !apiKey.Group.AllowMessagesDispatch {
		h.anthropicErrorResponse(c, http.StatusForbidden, "permission_error",
			"This group does not allow /v1/messages dispatch")
		return
	}

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	var body []byte
	var reqModel string
	var reqStream bool
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIMessages); ok {
		body = preForwardRequest.Body
		reqModel = preForwardRequest.Model
		reqStream = preForwardRequest.Stream
	} else {
		var err error
		body, err = readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
		if err != nil {
			if maxErr, ok := extractMaxBytesError(err); ok {
				h.anthropicErrorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
				return
			}
			markOpsRequestBodyReadError(c, err)
			h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
			return
		}
		if len(body) == 0 {
			h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
			return
		}

		if !gjson.ValidBytes(body) {
			logRequestBodyParseFailure(reqLog, body, nil)
			h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
			return
		}

		modelResult := gjson.GetBytes(body, "model")
		if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
			h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
			return
		}
		reqModel = modelResult.String()
		var ok bool
		reqStream, ok = parseOpenAICompatibleStream(body)
		if !ok {
			h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
			return
		}
	}
	routingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
	preferredMappedModel := resolveOpenAIMessagesDispatchMappedModel(apiKey, reqModel)

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	// 解析渠道级模型映射
	channelMappingMsg, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	mappedBodyForMessages := newOpenAIModelMappedBodyCache(body, h.gatewayService.ReplaceModelInBody)

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(apiKey)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
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
		ErrorComponent: "openai_messages.billing_eligibility_check_failed",
		ErrorResponder: func(c *gin.Context, status int, code, message string) {
			h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
		},
	}); billingStage.Stop {
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)
	sessionHash, promptCacheKey = resolveOpenAIMessagesMetadataSession(sessionHash, promptCacheKey, reqModel, body)
	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	effectiveMappedModel := preferredMappedModel
	var err error

	for {
		currentRoutingModel := routingModel
		if effectiveMappedModel != "" {
			currentRoutingModel = effectiveMappedModel
		}
		var account *service.Account
		var accountReleaseFunc func()
		routingRetry := false
		if routingStage := h.runOpenAIHTTPRoutingStage(c, OpenAIHTTPRoutingStage{
			Handler:            h,
			ReqLog:             reqLog,
			APIKey:             apiKey,
			SubjectUserID:      subject.UserID,
			RequestedModel:     currentRoutingModel,
			DisplayModel:       reqModel,
			SessionHash:        &sessionHash,
			FailedAccountIDs:   failedAccountIDs,
			RequiredTransport:  service.OpenAIUpstreamTransportAny,
			RequiredCapability: service.OpenAIEndpointCapabilityChatCompletions,
			RequestPlatform:    requestPlatform,
			Stream:             reqStream,
			StreamStarted:      &streamStarted,
			MaxAccountSwitches: maxAccountSwitches,
			SwitchCount:        &switchCount,
			LastFailoverErr:    lastFailoverErr,
			ErrorFormat:        openAIHTTPRoutingErrorAnthropicMessages,
			LogPrefix:          "openai_messages",
			Account:            &account,
			AccountReleaseFunc: &accountReleaseFunc,
			Retry:              &routingRetry,
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		defaultMappedModel := strings.TrimSpace(effectiveMappedModel)
		// 应用渠道模型映射到请求体
		forwardBody := mappedBodyForMessages(channelMappingMsg.Mapped, channelMappingMsg.MappedModel)
		writerSizeBeforeForward := c.Writer.Size()
		var result *service.OpenAIForwardResult
		stageResult := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:     h.gatewayService,
			Kind:               OpenAIHTTPForwardMessages,
			Account:            account,
			Body:               forwardBody,
			PromptCacheKey:     promptCacheKey,
			DefaultMappedModel: defaultMappedModel,
			ReleaseFunc:        accountReleaseFunc,
			Result:             &result,
		})
		err = stageResult.Err
		cyberBlockKeyMsg := ""
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockKeyMsg = service.CyberSessionBlockKey(apiKey.ID, c, body)
		}
		h.runOpenAIHTTPCyberUsageStage(c, OpenAIHTTPCyberUsageStageInput{
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			Model:              reqModel,
			ForwardErrored:     err != nil,
			CyberBlockKey:      cyberBlockKeyMsg,
			ChannelUsageFields: channelMappingMsg.ToUsageFields(reqModel, ""),
			RequestPayloadHash: service.HashUsageRequestPayload(body),
			RequestBody:        body,
		})
		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
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
				ChannelUsageFields: channelMappingMsg.ToUsageFields(reqModel, resultUpstreamModel(result)),
				LogComponent:       "handler.openai_gateway.messages",
				LogMessage:         "openai_messages.failed_upstream_usage_record_failed",
				LogUserID:          subject.UserID,
				LogModel:           reqModel,
			})
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai_messages.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
				return
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleAnthropicFailoverExhausted(c, failoverErr, true)
						return
					}
					h.runOpenAIHTTPScheduleResultStage(c, account, false, nil)
					// 池模式：同账号重试
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							sameAccountRetryCount[account.ID]++
							reqLog.Warn("openai_messages.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
							)
							select {
							case <-c.Request.Context().Done():
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
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount++
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount) {
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					reqLog.Warn("openai_messages.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				if result != nil && result.ClientDisconnect {
					reqLog.Info("openai_messages.client_disconnected",
						zap.Int64("account_id", account.ID),
						zap.Error(err),
					)
					return
				}
				h.runOpenAIHTTPScheduleResultStage(c, account, false, nil)
				wroteFallback := h.ensureAnthropicErrorResponse(c, streamStarted)
				reqLog.Warn("openai_messages.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Error(err),
				)
				return
			}
		}
		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		requestPayloadHash := service.HashUsageRequestPayload(body)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, result)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)

		cyberBlocked := service.GetOpsCyberPolicy(c) != nil
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
			ChannelUsageFields: channelMappingMsg.ToUsageFields(reqModel, result.UpstreamModel),
			CyberBlocked:       cyberBlocked,
			ScheduleSuccess:    &scheduleSucceeded,
			LogComponent:       "handler.openai_gateway.messages",
			LogMessage:         "openai_messages.record_usage_failed",
			LogUserID:          subject.UserID,
			LogModel:           reqModel,
		})
		reqLog.Debug("openai_messages.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func resolveOpenAIMessagesMetadataSession(sessionHash, promptCacheKey, reqModel string, body []byte) (string, string) {
	// Anthropic metadata.user_id 只作为账号粘性信号。上游 GPT/Codex 缓存键
	// 交给 ForwardAsAnthropic 从 cache_control 或完整消息 digest 派生，避免
	// 固定 metadata key 压住后续 turn 的缓存滚动。
	if sessionHash != "" {
		return sessionHash, promptCacheKey
	}
	if userID := strings.TrimSpace(gjson.GetBytes(body, "metadata.user_id").String()); userID != "" {
		seed := reqModel + "-" + userID
		sessionHash = service.DeriveSessionHashFromSeed(seed)
	}
	return sessionHash, promptCacheKey
}

// anthropicErrorResponse writes an error in Anthropic Messages API format.
func (h *OpenAIGatewayHandler) anthropicErrorResponse(c *gin.Context, status int, errType, message string) {
	markOpsClientMessageDiagnostic(c, errType, message)
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": clientmsg.Localize(message),
		},
	})
}

// anthropicStreamingAwareError handles errors that may occur during streaming,
// using Anthropic SSE error format.
func (h *OpenAIGatewayHandler) anthropicStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	markOpsClientMessageDiagnostic(c, errType, message)
	if streamStarted {
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			errPayload, _ := json.Marshal(gin.H{
				"type": "error",
				"error": gin.H{
					"type":    errType,
					"message": clientmsg.Localize(message),
				},
			})
			fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", errPayload) //nolint:errcheck
			flusher.Flush()
		}
		return
	}
	h.anthropicErrorResponse(c, status, errType, message)
}

// handleAnthropicFailoverExhausted maps upstream failover errors to Anthropic format.
func (h *OpenAIGatewayHandler) handleAnthropicFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(failoverErr.StatusCode)
	h.anthropicStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

// ensureAnthropicErrorResponse writes a fallback Anthropic error if no response was written.
func (h *OpenAIGatewayHandler) ensureAnthropicErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return false
	}
	h.anthropicStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
	return true
}

func (h *OpenAIGatewayHandler) validateFunctionCallOutputRequest(c *gin.Context, body []byte, reqLog *zap.Logger) bool {
	if !gjson.GetBytes(body, `input.#(type=="function_call_output")`).Exists() {
		return true
	}

	validation := service.ValidateFunctionCallOutputContextBytes(body)
	if !validation.HasFunctionCallOutput {
		return true
	}

	previousResponseID := gjson.GetBytes(body, "previous_response_id").String()
	if strings.TrimSpace(previousResponseID) != "" || validation.HasToolCallContext {
		return true
	}

	if validation.HasFunctionCallOutputMissingCallID {
		reqLog.Warn("openai.request_validation_failed",
			zap.String("reason", "function_call_output_missing_call_id"),
		)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
		return false
	}
	if validation.HasItemReferenceForAllCallIDs {
		return true
	}

	reqLog.Warn("openai.request_validation_failed",
		zap.String("reason", "function_call_output_missing_item_reference"),
	)
	h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "function_call_output requires item_reference ids matching each call_id on HTTP requests; continuation via previous_response_id is only supported on Responses WebSocket v2")
	return false
}

func (h *OpenAIGatewayHandler) acquireResponsesUserSlot(
	c *gin.Context,
	userID int64,
	userConcurrency int,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), bool) {
	ctx := c.Request.Context()
	userReleaseFunc, err := h.concurrencyHelper.AcquireUserSlotWithWait(c, userID, userConcurrency, reqStream, streamStarted)
	if err != nil {
		reqLog.Warn("openai.user_slot_acquire_failed", zap.Error(err))
		h.handleConcurrencyError(c, err, "user", *streamStarted)
		return nil, false
	}
	return wrapReleaseOnDone(ctx, userReleaseFunc), true
}

func (h *OpenAIGatewayHandler) acquireResponsesAccountSlot(
	c *gin.Context,
	groupID *int64,
	sessionHash string,
	selection *service.AccountSelectionResult,
	requestedModel string,
	requireCompact bool,
	requiredCapability service.OpenAIEndpointCapability,
	requiredImageCapability service.OpenAIImagesCapability,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), *service.Account, bool, bool) {
	if selection == nil || selection.Account == nil {
		markOpsRoutingCapacityLimited(c)
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, false
	}

	ctx := c.Request.Context()
	account := selection.Account
	if selection.Acquired {
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
		if refreshErr != nil {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			markOpsRoutingCapacityLimited(c)
			reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
			return nil, nil, false, false
		}
		selection.Account = refreshed
		account = refreshed
		return wrapReleaseOnDone(ctx, selection.ReleaseFunc), account, true, false
	}
	if selection.WaitPlan == nil {
		markOpsRoutingCapacityLimited(c)
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, false
	}

	fastReleaseFunc, fastAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(
		ctx,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
	)
	if err != nil {
		reqLog.Warn("openai.account_slot_quick_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		h.handleConcurrencyError(c, err, "account", *streamStarted)
		return nil, nil, false, false
	}
	if fastAcquired {
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
		if refreshErr != nil {
			if fastReleaseFunc != nil {
				fastReleaseFunc()
			}
			markOpsRoutingCapacityLimited(c)
			reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
			return nil, nil, false, false
		}
		selection.Account = refreshed
		account = refreshed
		if err := h.gatewayService.BindStickySession(ctx, groupID, sessionHash, account.ID); err != nil {
			reqLog.Warn("openai.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		}
		return wrapReleaseOnDone(ctx, fastReleaseFunc), account, true, false
	}

	canWait, waitErr := h.concurrencyHelper.IncrementAccountWaitCount(ctx, account.ID, selection.WaitPlan.MaxWaiting)
	if waitErr != nil {
		reqLog.Warn("openai.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(waitErr))
	} else if !canWait {
		reqLog.Info("openai.account_wait_queue_full",
			zap.Int64("account_id", account.ID),
			zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
		)
		return nil, nil, false, true
	}

	accountWaitCounted := waitErr == nil && canWait
	releaseWait := func() {
		if accountWaitCounted {
			h.concurrencyHelper.DecrementAccountWaitCount(ctx, account.ID)
			accountWaitCounted = false
		}
	}
	defer releaseWait()

	// Dynamic timeout: shorter wait when alternatives exist
	effectiveTimeout := selection.WaitPlan.Timeout
	if selection.CandidateCount > 1 {
		effectiveTimeout = 5 * time.Second
		reqLog.Debug("openai.using_short_wait_timeout",
			zap.Int("candidate_count", selection.CandidateCount),
			zap.Duration("timeout", effectiveTimeout),
		)
	}

	accountReleaseFunc, err := h.concurrencyHelper.AcquireAccountSlotWithWaitTimeout(
		c,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
		effectiveTimeout,
		reqStream,
		streamStarted,
	)
	if err != nil {
		reqLog.Warn("openai.account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		if IsConcurrencyRetryableError(err) {
			return nil, nil, false, true
		}
		h.handleConcurrencyError(c, err, "account", *streamStarted)
		return nil, nil, false, false
	}

	// Slot acquired: no longer waiting in queue.
	releaseWait()
	refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
	if refreshErr != nil {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		markOpsRoutingCapacityLimited(c)
		reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, false
	}
	selection.Account = refreshed
	account = refreshed
	if err := h.gatewayService.BindStickySession(ctx, groupID, sessionHash, account.ID); err != nil {
		reqLog.Warn("openai.bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
	return wrapReleaseOnDone(ctx, accountReleaseFunc), account, true, false
}

// ResponsesWebSocket handles OpenAI Responses API WebSocket ingress endpoint
// GET /openai/v1/responses (Upgrade: websocket)
func (h *OpenAIGatewayHandler) ResponsesWebSocket(c *gin.Context) {
	if !isOpenAIWSUpgradeRequest(c.Request) {
		h.errorResponse(c, http.StatusUpgradeRequired, "invalid_request_error", "WebSocket upgrade required (Upgrade: websocket)")
		return
	}
	if !requireOpenAIWebSocketGatewayPipelineEntrypoint(c) {
		return
	}
	setOpenAIClientTransportWS(c)

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
		"handler.openai_gateway.responses_ws",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.Bool("openai_ws_mode", true),
	)
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	reqLog.Info("openai.websocket_ingress_started")
	clientIP := ip.GetClientIP(c)
	userAgent := strings.TrimSpace(c.GetHeader("User-Agent"))

	wsConn, err := coderws.Accept(c.Writer, c.Request, &coderws.AcceptOptions{
		CompressionMode: coderws.CompressionContextTakeover,
	})
	if err != nil {
		reqLog.Warn("openai.websocket_accept_failed",
			zap.Error(err),
			zap.String("client_ip", clientIP),
			zap.String("request_user_agent", userAgent),
			zap.String("upgrade_header", strings.TrimSpace(c.GetHeader("Upgrade"))),
			zap.String("connection_header", strings.TrimSpace(c.GetHeader("Connection"))),
			zap.String("sec_websocket_version", strings.TrimSpace(c.GetHeader("Sec-WebSocket-Version"))),
			zap.Bool("has_sec_websocket_key", strings.TrimSpace(c.GetHeader("Sec-WebSocket-Key")) != ""),
		)
		return
	}
	defer func() {
		_ = wsConn.CloseNow()
	}()
	wsConn.SetReadLimit(service.ResolveOpenAIWSClientReadLimitBytes(h.cfg))

	ctx := c.Request.Context()
	maxIngressConnections := 0
	if h.cfg != nil {
		maxIngressConnections = h.cfg.Gateway.OpenAIWS.MaxIngressConnectionsPerAPIKey
	}
	ingressLease, ingressLeaseAcquired, ingressLeaseErr := h.concurrencyHelper.AcquireOpenAIWSIngressLease(ctx, apiKey.ID, maxIngressConnections)
	if ingressLeaseErr != nil {
		reqLog.Error("openai.websocket_ingress_lease_acquire_failed", zap.Error(ingressLeaseErr))
		closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to reserve websocket ingress capacity")
		return
	}
	if !ingressLeaseAcquired {
		reqLog.Info("openai.websocket_ingress_capacity_rejected", zap.Int("max_ingress_connections_per_api_key", maxIngressConnections))
		closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "too many open websocket connections, please retry later")
		return
	}
	if ingressLease != nil {
		defer ingressLease.Release()
		ctx = ingressLease.Context()
		c.Request = c.Request.WithContext(ctx)
	}

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	msgType, firstMessage, err := wsConn.Read(readCtx)
	cancel()
	if err != nil {
		if errors.Is(context.Cause(ctx), service.ErrOpenAIWSIngressLeaseLost) {
			reqLog.Warn("openai.websocket_ingress_lease_lost_before_first_message", zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "websocket ingress capacity lease lost; please reconnect")
			return
		}
		closeStatus, closeReason := summarizeWSCloseErrorForLog(err)
		reqLog.Warn("openai.websocket_read_first_message_failed",
			zap.Error(err),
			zap.String("client_ip", clientIP),
			zap.String("close_status", closeStatus),
			zap.String("close_reason", closeReason),
			zap.Duration("read_timeout", 30*time.Second),
		)
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "missing first response.create message")
		return
	}
	if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "unsupported websocket message type")
		return
	}
	if !gjson.ValidBytes(firstMessage) {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "invalid JSON payload")
		return
	}

	reqModel := strings.TrimSpace(gjson.GetBytes(firstMessage, "model").String())
	if reqModel == "" {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "model is required in first response.create payload")
		return
	}
	previousResponseID := strings.TrimSpace(gjson.GetBytes(firstMessage, "previous_response_id").String())
	previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
	if previousResponseID != "" && previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "previous_response_id must be a response.id (resp_*), not a message id")
		return
	}
	firstMessageToolCoverage := service.AnalyzeToolCallOutputContextCoverageBytes(firstMessage)
	previousResponseCanMove := !firstMessageToolCoverage.HasFunctionCallOutput || firstMessageToolCoverage.ContextCoversAllCallIDs
	reqLog = reqLog.With(
		zap.Bool("ws_ingress", true),
		zap.String("model", reqModel),
		zap.Bool("has_previous_response_id", previousResponseID != ""),
		zap.String("previous_response_id_kind", previousResponseIDKind),
	)
	setOpsRequestContext(c, reqModel, true)
	setOpsEndpointContext(c, "", int16(service.RequestTypeWSV2))

	// F5a: 握手层会话屏蔽检查。WS 握手无 body，显式标识仅来自握手 header
	// （session_id / conversation_id）；无标识则放行，连接内仍有本地 flag 兜底。
	initialPipelineResult := h.runOpenAIWebSocketFramePipeline(c, OpenAIWebSocketInitialFramePipelineAdapter{}, reqLog, openAIWebSocketPipelineInput{
		APIKey:        apiKey,
		Subject:       subject,
		Protocol:      service.ContentModerationProtocolOpenAIResponses,
		Model:         reqModel,
		Body:          firstMessage,
		CyberBody:     nil,
		ImageEndpoint: "/v1/responses",
	})
	cyberBlockKey := initialPipelineResult.CyberBlockKey
	if initialPipelineResult.Blocked {
		closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, reqModel, initialPipelineResult)
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, closeReason)
		return
	}
	cyberBlockedThisConn := false

	// 解析渠道级模型映射
	channelMappingWS, _ := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, reqModel)

	var currentUserRelease func()
	var currentAccountRelease func()
	releaseAccountSlot := func() {
		if currentAccountRelease != nil {
			currentAccountRelease()
			currentAccountRelease = nil
		}
	}
	releaseTurnSlots := func() {
		releaseAccountSlot()
		if currentUserRelease != nil {
			currentUserRelease()
			currentUserRelease = nil
		}
	}
	// 必须尽早注册，确保任何 early return 都能释放已获取的并发槽位。
	defer releaseTurnSlots()

	userReleaseFunc, userAcquired, err := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(ctx, subject.UserID, subject.Concurrency, apiKey.ID)
	if err != nil {
		reqLog.Warn("openai.websocket_user_slot_acquire_failed", zap.Error(err))
		closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to acquire user concurrency slot")
		return
	}
	if !userAcquired {
		closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "too many concurrent requests, please retry later")
		return
	}
	currentUserRelease = wrapReleaseOnDone(ctx, userReleaseFunc)
	ensureUserSlotHeld := func() bool {
		if currentUserRelease != nil {
			return true
		}
		userReleaseFunc, userAcquired, err := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(ctx, subject.UserID, subject.Concurrency, apiKey.ID)
		if err != nil {
			reqLog.Warn("openai.websocket_user_slot_reacquire_failed", zap.Error(err))
			closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "failed to acquire user concurrency slot")
			return false
		}
		if !userAcquired {
			closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "too many concurrent requests, please retry later")
			return false
		}
		currentUserRelease = wrapReleaseOnDone(ctx, userReleaseFunc)
		return true
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(apiKey)
	requiredTransport := service.OpenAIUpstreamTransportResponsesWebsocketV2Ingress
	if requestPlatform == service.PlatformGrok {
		requiredTransport = service.OpenAIUpstreamTransportHTTPSSE
	}
	if billingStage := h.runOpenAIWebSocketStage(c, OpenAIWebSocketBillingStage{
		Handler:          h,
		RequestContext:   ctx,
		QuotaPlatformCtx: c.Request.Context(),
		ReqLog:           reqLog,
		APIKey:           apiKey,
		Subscription:     subscription,
		ClientConn:       wsConn,
	}); billingStage.Stop {
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHashWithFallback(
		c,
		firstMessage,
		openAIWSIngressFallbackSessionSeed(subject.UserID, apiKey.ID, apiKey.GroupID),
	)
	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	failedAccountIDs := make(map[int64]struct{})
	var lastFailoverErr *service.UpstreamFailoverError

	for {
		var account *service.Account
		var accountMaxConcurrency int
		var token string
		stickyPreviousHit := false
		scheduleLayer := ""
		if routingStage := h.runOpenAIWebSocketStage(c, OpenAIWebSocketRoutingStage{
			Handler:                 h,
			RequestContext:          ctx,
			ReqLog:                  reqLog,
			APIKey:                  apiKey,
			SubjectUserID:           subject.UserID,
			RequestedModel:          reqModel,
			SessionHash:             sessionHash,
			PreviousResponseID:      previousResponseID,
			FailedAccountIDs:        failedAccountIDs,
			RequiredTransport:       requiredTransport,
			PreviousResponseCanMove: previousResponseCanMove,
			RequestPlatform:         requestPlatform,
			ClientConn:              wsConn,
			LastFailoverErr:         lastFailoverErr,
			Account:                 &account,
			AccountMaxConcurrency:   &accountMaxConcurrency,
			CurrentAccountRelease:   &currentAccountRelease,
			Token:                   &token,
			StickyPreviousHit:       &stickyPreviousHit,
			ScheduleLayer:           &scheduleLayer,
		}); routingStage.Stop {
			return
		}
		if gate := runSelectedAccountContentModeration(c, reqLog, h.contentModerationService, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, firstMessage, account); gate != nil && gate.Decision != nil && gate.Decision.Blocked {
			releaseAccountSlot()
			pipelineResult := openAIWebSocketPipelineResult{
				Blocked:            true,
				BlockReason:        openAIWebSocketPipelineBlockReasonModeration,
				ModerationDecision: gate.Decision,
				Message:            gate.Decision.Message,
			}
			closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, reqModel, pipelineResult)
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, closeReason)
			return
		}

		var requestPayloadHash string
		hooks := &service.OpenAIWSIngressHooks{
			InitialRequestModel: reqModel,
			BeforeRequest: func(turn int, payload []byte, originalModel string) error {
				if turn == 1 {
					return nil
				}
				if !gjson.ValidBytes(payload) {
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request payload", errors.New("invalid json"))
				}
				model := strings.TrimSpace(originalModel)
				if model == "" {
					model = strings.TrimSpace(gjson.GetBytes(payload, "model").String())
				}
				if model == "" {
					model = reqModel
				}
				pipelineResult := h.runOpenAIWebSocketFramePipeline(c, OpenAIWebSocketFollowupFramePipelineAdapter{}, reqLog, openAIWebSocketPipelineInput{
					APIKey:   apiKey,
					Subject:  subject,
					Protocol: service.ContentModerationProtocolOpenAIResponses,
					Model:    model,
					Body:     payload,
				})
				if pipelineResult.Blocked {
					closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, model, pipelineResult)
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, closeReason, nil)
				}
				if gate := runSelectedAccountContentModeration(c, reqLog, h.contentModerationService, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, payload, account); gate != nil && gate.Decision != nil && gate.Decision.Blocked {
					pipelineResult := openAIWebSocketPipelineResult{
						Blocked:            true,
						BlockReason:        openAIWebSocketPipelineBlockReasonModeration,
						ModerationDecision: gate.Decision,
						Message:            gate.Decision.Message,
					}
					closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, model, pipelineResult)
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, closeReason, nil)
				}
				return nil
			},
			BeforeTurn: func(turn int) error {
				// turn==1 的会话屏蔽已由握手层检查覆盖；连接内 flag 只拦截后续 turn。
				if cyberBlockedThisConn {
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, cyberSessionBlockedClientMessageForAPIKey(apiKey), nil)
				}
				if turn == 1 {
					return nil
				}
				// 防御式清理：避免异常路径下旧槽位覆盖导致泄漏。
				releaseTurnSlots()
				// 非首轮 turn 需要重新抢占并发槽位，避免长连接空闲占槽。
				userReleaseFunc, userAcquired, err := h.concurrencyHelper.TryAcquireUserSlotForAPIKey(ctx, subject.UserID, subject.Concurrency, apiKey.ID)
				if err != nil {
					return service.NewOpenAIWSClientCloseError(coderws.StatusInternalError, "failed to acquire user concurrency slot", err)
				}
				if !userAcquired {
					return service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "too many concurrent requests, please retry later", nil)
				}
				accountReleaseFunc, accountAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(ctx, account.ID, accountMaxConcurrency)
				if err != nil {
					if userReleaseFunc != nil {
						userReleaseFunc()
					}
					return service.NewOpenAIWSClientCloseError(coderws.StatusInternalError, "failed to acquire account concurrency slot", err)
				}
				if !accountAcquired {
					if userReleaseFunc != nil {
						userReleaseFunc()
					}
					return service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "account is busy, please retry later", nil)
				}
				currentUserRelease = wrapReleaseOnDone(ctx, userReleaseFunc)
				currentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
				return nil
			},
			AfterTurn: func(turn int, result *service.OpenAIForwardResult, turnErr error) {
				// Reuse is bounded to one pending frame. Once a turn is forwarded,
				// blocked, or abandoned, an identical later frame must be moderated anew.
				c.Set(selectedAccountModerationStateContextKey, (*service.ContentModerationAttemptState)(nil))
				_ = h.runOpenAIWebSocketStage(c, OpenAIWebSocketUsageStage{
					Handler:              h,
					RequestContext:       ctx,
					ReqLog:               reqLog,
					APIKey:               apiKey,
					Account:              account,
					Subscription:         subscription,
					Model:                reqModel,
					TurnErr:              turnErr,
					Result:               result,
					CyberBlockKey:        cyberBlockKey,
					ChannelMapping:       channelMappingWS,
					RequestPayloadHash:   requestPayloadHash,
					ReleaseTurnSlots:     releaseTurnSlots,
					CyberBlockedThisConn: &cyberBlockedThisConn,
					UserAgent:            userAgent,
					ClientIP:             clientIP,
				})
			},
		}

		// 应用渠道模型映射到 WebSocket 首条消息
		wsFirstMessage := firstMessage
		if channelMappingWS.Mapped {
			wsFirstMessage = h.gatewayService.ReplaceModelInBody(firstMessage, channelMappingWS.MappedModel)
		}
		// 切组/会话失配防护：previous_response_id 未在当前分组命中粘连账号（StickyPreviousHit=false），
		// 说明该会话链不属于本次调度到的账号，原样转发会触发上游会话链鉴权失败（“鉴权失败，请检查 API Key”）。
		// 故剥离首包里的 previous_response_id，改用首包内 input 重建上下文；带 function_call_output 的
		// 工具续链无法重建，保持原样。仅作用于首轮首包，后续 turn 的续链由 WS 转发层既有逻辑处理。
		if previousResponseID != "" && !stickyPreviousHit && previousResponseCanMove {
			wsFirstMessage = service.RemovePreviousResponseIDFromBody(wsFirstMessage)
			reqLog.Debug("openai.websocket_previous_response_id_stripped_cross_group",
				zap.Int64("account_id", account.ID),
				zap.String("schedule_layer", scheduleLayer),
			)
		}

		// WebSocket 首包可能很大，hash 必须在 hooks 外算成字符串，避免 AfterTurn 闭包保活请求体。
		requestPayloadHash = service.HashUsageRequestPayload(wsFirstMessage)

		var proxyErr error
		_ = h.runOpenAIWebSocketStage(c, OpenAIWebSocketForwardStage{
			GatewayService: h.gatewayService,
			RequestContext: ctx,
			ClientConn:     wsConn,
			Account:        account,
			Token:          token,
			FirstMessage:   wsFirstMessage,
			Hooks:          hooks,
			Err:            &proxyErr,
		})
		if proxyErr != nil {
			var failoverErr *service.UpstreamFailoverError
			if errors.As(proxyErr, &failoverErr) {
				scheduleFailed := false
				_ = h.runOpenAIWebSocketStage(c, OpenAIWebSocketUsageStage{
					Handler:         h,
					RequestContext:  ctx,
					ReqLog:          reqLog,
					APIKey:          apiKey,
					Account:         account,
					Subscription:    subscription,
					Model:           reqModel,
					TurnErr:         proxyErr,
					ChannelMapping:  channelMappingWS,
					ScheduleSuccess: &scheduleFailed,
				})
				releaseAccountSlot()
				failedAccountIDs[account.ID] = struct{}{}
				h.gatewayService.CooldownUserAccount(ctx, subject.UserID, account.ID, h.gatewayService.UserAccountCooldownTTL(ctx))
				lastFailoverErr = failoverErr
				if switchCount >= maxAccountSwitches {
					closeOpenAIWSFailoverExhausted(wsConn, failoverErr)
					return
				}
				switchCount++
				if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount) {
					closeOpenAIWSFailoverExhausted(wsConn, failoverErr)
					return
				}
				h.gatewayService.RecordOpenAIAccountSwitch()
				reqLog.Warn("openai.websocket_upstream_failover_switching",
					zap.Int64("account_id", account.ID),
					zap.Int("upstream_status", failoverErr.StatusCode),
					zap.Int("switch_count", switchCount),
					zap.Int("max_switches", maxAccountSwitches),
				)
				if !ensureUserSlotHeld() {
					return
				}
				continue
			}

			if errors.Is(context.Cause(ctx), service.ErrOpenAIWSIngressLeaseLost) {
				reqLog.Warn("openai.websocket_ingress_lease_lost",
					zap.Int64("account_id", account.ID),
					zap.Error(proxyErr),
				)
				closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "websocket ingress capacity lease lost; please reconnect")
				return
			}

			var closeErr *service.OpenAIWSClientCloseError
			if errors.As(proxyErr, &closeErr) && closeErr.StatusCode() == coderws.StatusNormalClosure {
				reqLog.Info("openai.websocket_ingress_closed_normally",
					zap.Int64("account_id", account.ID),
					zap.String("reason", closeErr.Reason()),
				)
				closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
				return
			}

			scheduleFailed := false
			_ = h.runOpenAIWebSocketStage(c, OpenAIWebSocketUsageStage{
				Handler:         h,
				RequestContext:  ctx,
				ReqLog:          reqLog,
				APIKey:          apiKey,
				Account:         account,
				Subscription:    subscription,
				Model:           reqModel,
				TurnErr:         proxyErr,
				ChannelMapping:  channelMappingWS,
				ScheduleSuccess: &scheduleFailed,
			})
			closeStatus, closeReason := summarizeWSCloseErrorForLog(proxyErr)
			reqLog.Warn("openai.websocket_proxy_failed",
				zap.Int64("account_id", account.ID),
				zap.Error(proxyErr),
				zap.String("close_status", closeStatus),
				zap.String("close_reason", closeReason),
			)
			if errors.As(proxyErr, &closeErr) {
				closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
				return
			}
			closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "upstream websocket proxy failed")
			return
		}
		reqLog.Info("openai.websocket_ingress_closed", zap.Int64("account_id", account.ID))
		return
	}

}

func (h *OpenAIGatewayHandler) recoverResponsesPanic(c *gin.Context, streamStarted *bool) {
	recovered := recover()
	if recovered == nil {
		return
	}

	started := false
	if streamStarted != nil {
		started = *streamStarted
	}
	wroteFallback := h.ensureForwardErrorResponse(c, started)
	requestLogger(c, "handler.openai_gateway.responses").Error(
		"openai.responses_panic_recovered",
		zap.Bool("fallback_error_response_written", wroteFallback),
		zap.Any("panic", recovered),
		zap.ByteString("stack", debug.Stack()),
	)
}

// recoverAnthropicMessagesPanic recovers from panics in the Anthropic Messages
// handler and returns an Anthropic-formatted error response.
func (h *OpenAIGatewayHandler) recoverAnthropicMessagesPanic(c *gin.Context, streamStarted *bool) {
	recovered := recover()
	if recovered == nil {
		return
	}

	started := streamStarted != nil && *streamStarted
	requestLogger(c, "handler.openai_gateway.messages").Error(
		"openai.messages_panic_recovered",
		zap.Bool("stream_started", started),
		zap.Any("panic", recovered),
		zap.ByteString("stack", debug.Stack()),
	)
	if !started {
		h.anthropicErrorResponse(c, http.StatusInternalServerError, "api_error", "Internal server error")
	}
}

func (h *OpenAIGatewayHandler) ensureResponsesDependencies(c *gin.Context, reqLog *zap.Logger) bool {
	missing := h.missingResponsesDependencies()
	if len(missing) == 0 {
		return true
	}

	if reqLog == nil {
		reqLog = requestLogger(c, "handler.openai_gateway.responses")
	}
	reqLog.Error("openai.handler_dependencies_missing", zap.Strings("missing_dependencies", missing))

	if c != nil && c.Writer != nil && !c.Writer.Written() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "api_error",
				"message": "Service temporarily unavailable",
			},
		})
	}
	return false
}

func (h *OpenAIGatewayHandler) missingResponsesDependencies() []string {
	missing := make([]string, 0, 5)
	if h == nil {
		return append(missing, "handler")
	}
	if h.gatewayService == nil {
		missing = append(missing, "gatewayService")
	}
	if h.billingCacheService == nil {
		missing = append(missing, "billingCacheService")
	}
	if h.apiKeyService == nil {
		missing = append(missing, "apiKeyService")
	}
	if h.concurrencyHelper == nil || h.concurrencyHelper.concurrencyService == nil {
		missing = append(missing, "concurrencyHelper")
	}
	return missing
}

func getContextInt64(c *gin.Context, key string) (int64, bool) {
	if c == nil || key == "" {
		return 0, false
	}
	v, ok := c.Get(key)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	default:
		return 0, false
	}
}

func (h *OpenAIGatewayHandler) submitUsageRecordTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if h.usageRecordWorkerPool != nil {
		h.usageRecordWorkerPool.Submit(task)
		return
	}
	// 回退路径：worker 池未注入时同步执行，避免退回到无界 goroutine 模式。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.responses"),
				zap.Any("panic", recovered),
			).Error("openai.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

func (h *OpenAIGatewayHandler) submitOpenAIUsageRecordTask(parent context.Context, result *service.OpenAIForwardResult, task service.UsageRecordTask) {
	if result != nil && result.ImageCount > 0 {
		h.submitMandatoryUsageRecordTask(parent, task)
		return
	}
	h.submitUsageRecordTask(parent, task)
}

func (h *OpenAIGatewayHandler) submitMandatoryUsageRecordTask(parent context.Context, task service.UsageRecordTask) {
	if task == nil {
		return
	}
	task = wrapUsageRecordTaskContext(parent, task)
	if h.usageRecordWorkerPool != nil {
		if mode := h.usageRecordWorkerPool.Submit(task); mode != service.UsageRecordSubmitModeDropped {
			return
		}
		logger.L().With(
			zap.String("component", "handler.openai_gateway.usage"),
		).Warn("openai.usage_record_task_mandatory_sync_fallback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "handler.openai_gateway.usage"),
				zap.Any("panic", recovered),
			).Error("openai.usage_record_task_panic_recovered")
		}
	}()
	task(ctx)
}

func (h *OpenAIGatewayHandler) acquireImageGenerationSlot(c *gin.Context, streamStarted bool) (func(), bool) {
	if h == nil || h.cfg == nil {
		return nil, true
	}
	imageConcurrency := h.cfg.Gateway.ImageConcurrency
	if !imageConcurrency.Enabled {
		return nil, true
	}
	wait := strings.TrimSpace(imageConcurrency.OverflowMode) == config.ImageConcurrencyOverflowModeWait
	var releases []func()
	releaseAll := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			if releases[i] != nil {
				releases[i]()
			}
		}
	}

	userLimit := h.currentImageGenerationUserLimit(imageConcurrency)
	if userLimit > 0 {
		if userID, ok := imageGenerationConcurrencyUserID(c); ok {
			userImageConcurrency := imageConcurrency
			userImageConcurrency.MaxConcurrentRequestsPerUser = userLimit
			release, acquired := h.acquireUserImageGenerationSlot(c, userID, userImageConcurrency, wait)
			if !acquired {
				releaseAll()
				h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Image generation concurrency limit exceeded, please retry later", streamStarted)
				return nil, false
			}
			if release != nil {
				releases = append(releases, release)
			}
		}
	}

	if h.imageLimiter != nil {
		release, acquired := h.imageLimiter.Acquire(
			c.Request.Context(),
			true,
			imageConcurrency.MaxConcurrentRequests,
			wait,
			time.Duration(imageConcurrency.WaitTimeoutSeconds)*time.Second,
			imageConcurrency.MaxWaitingRequests,
		)
		if !acquired {
			releaseAll()
			h.handleStreamingAwareError(c, http.StatusTooManyRequests, "rate_limit_error", "Image generation concurrency limit exceeded, please retry later", streamStarted)
			return nil, false
		}
		if release != nil {
			releases = append(releases, release)
		}
	}

	if len(releases) == 0 {
		return nil, true
	}
	return releaseAll, true
}

func (h *OpenAIGatewayHandler) currentImageGenerationUserLimit(imageConcurrency config.ImageConcurrencyConfig) int {
	hardLimit := imageConcurrency.MaxConcurrentRequestsPerUser
	if hardLimit <= 0 {
		return 0
	}
	totalLimit := imageConcurrency.MaxConcurrentRequests
	if totalLimit <= 0 || h == nil || h.imageLimiter == nil {
		return hardLimit
	}
	idleSlots := totalLimit - h.imageLimiter.activeCount()
	if idleSlots <= 0 {
		return hardLimit
	}
	if idleSlots < hardLimit {
		return idleSlots
	}
	return hardLimit
}

func imageGenerationConcurrencyUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if ok && subject.UserID > 0 {
		return subject.UserID, true
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if ok && apiKey != nil && apiKey.User != nil && apiKey.User.ID > 0 {
		return apiKey.User.ID, true
	}
	return 0, false
}

func (h *OpenAIGatewayHandler) acquireUserImageGenerationSlot(c *gin.Context, userID int64, imageConcurrency config.ImageConcurrencyConfig, wait bool) (func(), bool) {
	limiter := h.imageLimiterForUser(userID)
	release, acquired := limiter.Acquire(
		c.Request.Context(),
		true,
		imageConcurrency.MaxConcurrentRequestsPerUser,
		wait,
		time.Duration(imageConcurrency.WaitTimeoutSeconds)*time.Second,
		imageConcurrency.MaxWaitingRequests,
	)
	if !acquired {
		h.pruneImageLimiterForUser(userID, limiter)
		return nil, false
	}
	if release == nil {
		h.pruneImageLimiterForUser(userID, limiter)
		return nil, true
	}
	return func() {
		release()
		h.pruneImageLimiterForUser(userID, limiter)
	}, true
}

func (h *OpenAIGatewayHandler) imageLimiterForUser(userID int64) *imageConcurrencyLimiter {
	h.imageUserLimiterMu.Lock()
	defer h.imageUserLimiterMu.Unlock()
	if h.imageUserLimiters == nil {
		h.imageUserLimiters = map[int64]*imageConcurrencyLimiter{}
	}
	limiter := h.imageUserLimiters[userID]
	if limiter == nil {
		limiter = &imageConcurrencyLimiter{}
		h.imageUserLimiters[userID] = limiter
	}
	return limiter
}

func (h *OpenAIGatewayHandler) pruneImageLimiterForUser(userID int64, limiter *imageConcurrencyLimiter) {
	if h == nil || limiter == nil || !limiter.idle() {
		return
	}
	h.imageUserLimiterMu.Lock()
	defer h.imageUserLimiterMu.Unlock()
	if h.imageUserLimiters[userID] == limiter && limiter.idle() {
		delete(h.imageUserLimiters, userID)
	}
}

// handleConcurrencyError handles concurrency-related acquire errors.
func (h *OpenAIGatewayHandler) handleConcurrencyError(c *gin.Context, err error, slotType string, streamStarted bool) {
	markOpsConcurrencyErrorDiagnostic(c, err)
	status, errType, message := concurrencyErrorResponse(err, slotType)
	h.handleStreamingAwareError(c, status, errType, message, streamStarted)
}

func (h *OpenAIGatewayHandler) handleFailoverExhausted(c *gin.Context, failoverErr *service.UpstreamFailoverError, streamStarted bool) {
	statusCode := failoverErr.StatusCode
	responseBody := failoverErr.ResponseBody
	if service.IsOpenAISilentRefusalErrorBody(responseBody) {
		service.SetOpsUpstreamError(c, statusCode, service.OpenAISilentRefusalClientMessage(), "")
		h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", service.OpenAISilentRefusalClientMessage(), streamStarted)
		return
	}

	// 先检查透传规则
	if h.errorPassthroughService != nil && len(responseBody) > 0 {
		if rule := h.errorPassthroughService.MatchRule("openai", statusCode, responseBody); rule != nil {
			// 确定响应状态码
			respCode := statusCode
			if !rule.PassthroughCode && rule.ResponseCode != nil {
				respCode = *rule.ResponseCode
			}

			// 确定响应消息
			msg := service.ExtractUpstreamErrorMessage(responseBody)
			if !rule.PassthroughBody && rule.CustomMessage != nil {
				msg = *rule.CustomMessage
			}

			if rule.SkipMonitoring {
				c.Set(service.OpsSkipPassthroughKey, true)
			}

			h.handleStreamingAwareError(c, respCode, "upstream_error", msg, streamStarted)
			return
		}
	}

	// 记录原始上游状态码，以便 ops 错误日志捕获真实的上游错误
	upstreamMsg := service.ExtractUpstreamErrorMessage(responseBody)
	service.SetOpsUpstreamError(c, statusCode, upstreamMsg, "")

	// 使用默认的错误映射
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

// handleFailoverExhaustedSimple 简化版本，用于没有响应体的情况
func (h *OpenAIGatewayHandler) handleFailoverExhaustedSimple(c *gin.Context, statusCode int, streamStarted bool) {
	status, errType, errMsg := h.mapUpstreamError(statusCode)
	service.SetOpsUpstreamError(c, statusCode, errMsg, "")
	h.handleStreamingAwareError(c, status, errType, errMsg, streamStarted)
}

func (h *OpenAIGatewayHandler) mapUpstreamError(statusCode int) (int, string, string) {
	switch statusCode {
	case 401:
		return http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"
	case 403:
		return http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"
	case 429:
		return http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"
	case 529:
		return http.StatusServiceUnavailable, "upstream_error", "Upstream service overloaded, please retry later"
	case 500, 502, 503, 504:
		return http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"
	default:
		return http.StatusBadGateway, "upstream_error", "Upstream request failed"
	}
}

// handleStreamingAwareError handles errors that may occur after streaming has started
func (h *OpenAIGatewayHandler) handleStreamingAwareError(c *gin.Context, status int, errType, message string, streamStarted bool) {
	markOpsClientMessageDiagnostic(c, errType, message)
	// body-signal compact 心跳可能已把响应头提交为 200：先停心跳（建立
	// happens-before，接管 ResponseWriter），并升级为流内错误处理。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		streamStarted = true
	}
	if streamStarted {
		// /v1/responses 的严格 SDK（Codex CLI）要求终止事件必须属于
		// response.completed/failed/incomplete/cancelled 集合。
		// 通用 `event: error` 帧不被识别为终止事件，会导致
		// "stream closed before response.completed"。
		if inboundIsResponses(c) {
			if writeResponsesFailedSSE(c, errType, message) {
				return
			}
		}
		// Stream already started, send error as SSE event then close
		flusher, ok := c.Writer.(http.Flusher)
		if ok {
			// SSE 错误事件固定 schema，使用 Quote 直拼可避免额外 Marshal 分配。
			errorEvent := "event: error\ndata: " + `{"error":{"type":` + strconv.Quote(errType) + `,"message":` + strconv.Quote(clientmsg.Localize(message)) + `}}` + "\n\n"
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	h.errorResponse(c, status, errType, message)
}

// ensureForwardErrorResponse 在 Forward 返回错误但尚未写响应时补写统一错误响应。
func (h *OpenAIGatewayHandler) ensureForwardErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	// 先停 compact 心跳再读 Writer 状态，避免与心跳 goroutine 竞争。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		streamStarted = true
	}
	if service.IsResponseCommitted(c) {
		return false
	}
	if c.Writer.Written() {
		streamStarted = true
	}
	h.handleStreamingAwareError(c, http.StatusBadGateway, "upstream_error", "Upstream request failed", streamStarted)
	return true
}

func shouldLogOpenAIForwardFailureAsWarn(c *gin.Context, wroteFallback bool) bool {
	if wroteFallback {
		return false
	}
	if c == nil || c.Writer == nil {
		return false
	}
	return c.Writer.Written()
}

// openAIForwardErrorAlreadyCommunicated reports whether Forward returned an
// error after it had already written the upstream terminal error response to
// the client.
//
// This matters for Responses streams: upstream may return HTTP 200 with a
// non-retryable `response.failed` event (for example a policy/safety rejection).
// The service layer forwards that terminal event verbatim, then returns an
// error so the caller can log/account for the failed upstream response. The
// handler must not append its generic fallback `response.failed`, otherwise
// strict clients may see the useful upstream message replaced by "Upstream
// request failed" or receive duplicate terminal events.
func openAIForwardErrorAlreadyCommunicated(c *gin.Context, writerSizeBeforeForward int, err error) bool {
	if err == nil || c == nil || c.Writer == nil {
		return false
	}
	// 与快照同口径：排除 compact 心跳字节，避免"仅心跳写出"被误判为
	// 响应已写出（#3887）。
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward ||
		service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return false
	}

	// cyber_policy 命中时上游原始错误体已透传给客户端（非流式 c.Data 写出 400 body，
	// 流式写出 response.failed 事件），不能再让 ensureForwardErrorResponse 追加
	// fallback —— 否则在已写出的完整响应尾部追加 SSE（responses 端点尾随
	// response.failed、chat 端点尾随 event:error），污染响应体。Size 已变化证明响应确已写出。
	if service.GetOpsCyberPolicy(c) != nil {
		return true
	}

	msg := strings.TrimSpace(err.Error())
	for _, prefix := range []string{
		"upstream response failed:",
		"non-streaming openai protocol error:",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// errorResponse returns OpenAI API format error response
func (h *OpenAIGatewayHandler) errorResponse(c *gin.Context, status int, errType, message string) {
	markOpsClientMessageDiagnostic(c, errType, message)
	// body-signal compact 心跳可能已把响应头提交为 200：JSON 错误体会与已
	// 提交的 SSE 流交错，必须降级为 response.failed 终止事件（#3887）。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		service.MarkOpsStreamError(c, errType, message, status)
		if writeResponsesFailedSSE(c, errType, message) {
			return
		}
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errType,
			"message": clientmsg.Localize(message),
		},
	})
}

// openAICompactKeepaliveInterval 复用流式 keepalive 配置作为 compact 下游
// 心跳间隔；0 表示禁用（与流式路径语义一致）。
func (h *OpenAIGatewayHandler) openAICompactKeepaliveInterval() time.Duration {
	if h.cfg == nil || h.cfg.Gateway.StreamKeepaliveInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.Gateway.StreamKeepaliveInterval) * time.Second
}

func setOpenAIClientTransportHTTP(c *gin.Context) {
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportHTTP)
}

func setOpenAIClientTransportWS(c *gin.Context) {
	service.SetOpenAIClientTransport(c, service.OpenAIClientTransportWS)
}

func ensureOpenAIPoolModeSessionHash(sessionHash string, account *service.Account) string {
	if sessionHash != "" || account == nil || !account.IsPoolMode() {
		return sessionHash
	}
	// 为当前请求生成一次性粘性会话键，确保同账号重试不会重新负载均衡到其他账号。
	return "openai-pool-retry-" + uuid.NewString()
}

func openAIWSIngressFallbackSessionSeed(userID, apiKeyID int64, groupID *int64) string {
	gid := int64(0)
	if groupID != nil {
		gid = *groupID
	}
	return fmt.Sprintf("openai_ws_ingress:%d:%d:%d", gid, userID, apiKeyID)
}

func isOpenAIWSUpgradeRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Connection"))), "upgrade")
}

func closeOpenAIClientWS(conn *coderws.Conn, status coderws.StatusCode, reason string) {
	if conn == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 120 {
		reason = reason[:120]
	}
	_ = conn.Close(status, reason)
	_ = conn.CloseNow()
}

func closeOpenAIWSFailoverExhausted(conn *coderws.Conn, failoverErr *service.UpstreamFailoverError) {
	if failoverErr == nil {
		closeOpenAIClientWS(conn, coderws.StatusInternalError, "upstream websocket proxy failed")
		return
	}
	switch failoverErr.StatusCode {
	case http.StatusTooManyRequests:
		closeOpenAIClientWS(conn, coderws.StatusTryAgainLater, "upstream rate limit exceeded, please retry later")
	case 529, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		closeOpenAIClientWS(conn, coderws.StatusTryAgainLater, "upstream service temporarily unavailable")
	case http.StatusUnauthorized, http.StatusForbidden:
		closeOpenAIClientWS(conn, coderws.StatusPolicyViolation, "upstream websocket authentication failed")
	default:
		closeOpenAIClientWS(conn, coderws.StatusInternalError, "upstream websocket proxy failed")
	}
}

func writeContentModerationWSError(ctx context.Context, conn *coderws.Conn, decision *service.ContentModerationDecision) {
	if conn == nil || decision == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	message := strings.TrimSpace(decision.Message)
	if message == "" {
		message = "content moderation blocked this request"
	}
	payload, err := json.Marshal(gin.H{
		"event_id": "evt_content_moderation_blocked",
		"type":     "error",
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    contentModerationErrorCode(decision),
			"message": message,
		},
	})
	if err != nil {
		payload = []byte(`{"event_id":"evt_content_moderation_blocked","type":"error","error":{"type":"invalid_request_error","code":"content_policy_violation","message":"content moderation blocked this request"}}`)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = conn.Write(writeCtx, coderws.MessageText, payload)
}

func (h *OpenAIGatewayHandler) writeOpenAIWebSocketPipelineBlock(ctx context.Context, c *gin.Context, conn *coderws.Conn, apiKey *service.APIKey, model string, result openAIWebSocketPipelineResult) string {
	message := strings.TrimSpace(result.Message)
	switch result.BlockReason {
	case openAIWebSocketPipelineBlockReasonModeration:
		if result.ModerationDecision != nil {
			markOpsContentModerationDiagnostic(c, result.ModerationDecision)
			writeContentModerationWSError(ctx, conn, result.ModerationDecision)
			if message == "" {
				message = result.ModerationDecision.Message
			}
		}
	case openAIWebSocketPipelineBlockReasonImagePermission:
		if message == "" {
			message = service.ImageGenerationPermissionMessage()
		}
	case openAIWebSocketPipelineBlockReasonCyberSession:
		writeCyberSessionBlockedWSError(ctx, conn, cyberSessionBlockPlatform(apiKey, service.ContentModerationProtocolOpenAIResponses, cyberBlockFormatResponses))
		if h != nil && result.CyberBlockKey != "" {
			h.enqueueCyberSessionBlockedOpsEntry(c, apiKey, model, result.CyberBlockKey)
		}
		if message == "" {
			message = cyberSessionBlockedClientMessageForAPIKey(apiKey)
		}
	default:
		if message == "" {
			message = "request blocked by policy"
		}
	}
	return message
}

// writeCyberSessionBlockedWSError sends an error frame telling the client this
// session is blocked by the cyber session block (F5a) before closing.
func writeCyberSessionBlockedWSError(ctx context.Context, conn *coderws.Conn, platform string) {
	if conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	message := cyberSessionBlockedClientMessage(platform)
	payload, err := json.Marshal(gin.H{
		"event_id": "evt_cyber_session_blocked",
		"type":     "error",
		"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": message,
		},
	})
	if err != nil {
		payload = []byte(`{"event_id":"evt_cyber_session_blocked","type":"error","error":{"type":"permission_error","code":"session_blocked_by_cyber_policy","message":"会话已被OpenAI网络安全策略屏蔽,请开启新会话"}}`)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = conn.Write(writeCtx, coderws.MessageText, payload)
}

// cyberPolicyRecordedKey guards against double-firing recordCyberPolicyIfMarked
// within one request (e.g. in a retry/failover loop).
const cyberPolicyRecordedKey = "ops_cyber_recorded"

// cyberPolicyOpsErrorMeta carries request-scoped fields captured outside the
// async goroutine for building the cyber ops_error_logs entry.
type cyberPolicyOpsErrorMeta struct {
	RequestID       string
	ClientRequestID string
	Platform        string
	Model           string
	RequestPath     string
	Stream          bool
	InboundEndpoint string
	UserAgent       string
	APIKeyPrefix    string
	UserID          int64
	APIKeyID        int64
	AccountID       int64
	GroupID         *int64
	ClientIP        string
	CreatedAt       time.Time
	SessionBlockKey string
}

// buildCyberPolicyOpsErrorEntry builds the ops_error_logs entry for an upstream
// cyber_policy hit. StatusCode mirrors what the codex client actually received
// (400 non-stream / 200 stream), per F6.
func buildCyberPolicyOpsErrorEntry(meta cyberPolicyOpsErrorMeta, mark *service.CyberPolicyMark) *service.OpsInsertErrorLogInput {
	rt := int16(service.RequestTypeCyberBlocked)
	entry := &service.OpsInsertErrorLogInput{
		RequestID:         meta.RequestID,
		ClientRequestID:   meta.ClientRequestID,
		Platform:          meta.Platform,
		Model:             meta.Model,
		RequestPath:       meta.RequestPath,
		Stream:            meta.Stream,
		InboundEndpoint:   meta.InboundEndpoint,
		RequestType:       &rt,
		UserAgent:         meta.UserAgent,
		APIKeyPrefix:      meta.APIKeyPrefix,
		ErrorPhase:        "request",
		ErrorType:         "cyber_policy",
		Severity:          "P3",
		StatusCode:        mark.UpstreamStatus,
		IsBusinessLimited: true,
		ErrorMessage:      "cyber_policy: " + mark.Message,
		// 原始 body 直接入队；ops service 落库前统一走 sanitizeErrorBodyForStorage 脱敏与截断。
		ErrorBody:   mark.Body,
		ErrorSource: "upstream_http",
		ErrorOwner:  "provider",
		CreatedAt:   meta.CreatedAt,
	}
	upstreamMsg := strings.TrimSpace(mark.Message)
	if upstreamMsg == "" {
		upstreamMsg = "cyber_policy"
	}
	entry.UpstreamErrorMessage = &upstreamMsg
	entry.UpstreamErrorDetail = opsJSONDetail(map[string]string{
		"source":          "cyber_policy",
		"reason":          "upstream_cyber_policy",
		"code":            strings.TrimSpace(mark.Code),
		"message":         upstreamMsg,
		"upstream_status": strconv.Itoa(mark.UpstreamStatus),
	})
	if meta.UserID > 0 {
		entry.UserID = &meta.UserID
	}
	if meta.APIKeyID > 0 {
		entry.APIKeyID = &meta.APIKeyID
	}
	if meta.AccountID > 0 {
		entry.AccountID = &meta.AccountID
	}
	entry.GroupID = meta.GroupID
	if meta.ClientIP != "" {
		entry.ClientIP = &meta.ClientIP
	}
	return entry
}

func cyberSessionBlockedClientMessageForAPIKey(apiKey *service.APIKey) string {
	return cyberSessionBlockedClientMessage(cyberSessionBlockPlatform(apiKey, "", cyberBlockFormatResponses))
}

func cyberSessionBlockedClientMessage(platform string) string {
	return fmt.Sprintf("会话已被%s网络安全策略屏蔽,请开启新会话", cyberSessionBlockPlatformLabel(platform))
}

func cyberSessionBlockPlatform(apiKey *service.APIKey, protocol string, format cyberSessionBlockFormat) string {
	if apiKey != nil && apiKey.Group != nil && strings.TrimSpace(apiKey.Group.Platform) != "" {
		return strings.TrimSpace(apiKey.Group.Platform)
	}
	switch protocol {
	case service.ContentModerationProtocolAnthropicMessages:
		return service.PlatformAnthropic
	case service.ContentModerationProtocolOpenAIChat,
		service.ContentModerationProtocolOpenAIMessages,
		service.ContentModerationProtocolOpenAIResponses,
		service.ContentModerationProtocolOpenAIImages,
		service.ContentModerationProtocolOpenAIEmbeddings:
		return service.PlatformOpenAI
	}
	if format == cyberBlockFormatAnthropic {
		return service.PlatformAnthropic
	}
	return service.PlatformOpenAI
}

func cyberSessionBlockPlatformLabel(platform string) string {
	value := strings.TrimSpace(platform)
	switch strings.ToLower(value) {
	case service.PlatformOpenAI:
		return "OpenAI"
	case service.PlatformAnthropic:
		return "Anthropic"
	case service.PlatformGemini:
		return "Gemini"
	case service.PlatformGrok:
		return "Grok"
	case service.PlatformAntigravity:
		return "Antigravity"
	}
	if value == "" {
		return "OpenAI"
	}
	return value
}

// buildCyberSessionBlockedOpsEntry builds the ops_error_logs entry for a request
// rejected locally by the cyber session block (F5a). Distinct error_type from
// upstream `cyber_policy`; never feeds moderation logs / violation counting
// (the request never reached upstream — see spec).
func buildCyberSessionBlockedOpsEntry(meta cyberPolicyOpsErrorMeta) *service.OpsInsertErrorLogInput {
	rt := int16(service.RequestTypeCyberBlocked)
	entry := &service.OpsInsertErrorLogInput{
		RequestID:         meta.RequestID,
		ClientRequestID:   meta.ClientRequestID,
		Platform:          meta.Platform,
		Model:             meta.Model,
		RequestPath:       meta.RequestPath,
		Stream:            meta.Stream,
		InboundEndpoint:   meta.InboundEndpoint,
		RequestType:       &rt,
		UserAgent:         meta.UserAgent,
		APIKeyPrefix:      meta.APIKeyPrefix,
		ErrorPhase:        "request",
		ErrorType:         "cyber_policy_session_blocked",
		Severity:          "P3",
		StatusCode:        http.StatusForbidden,
		IsBusinessLimited: true,
		ErrorMessage:      "cyber_policy_session_blocked: request rejected locally by session block",
		ErrorSource:       "gateway_local",
		ErrorOwner:        "platform",
		CreatedAt:         meta.CreatedAt,
		// AccountID 有意不设：请求在账号选择前即被拒绝。
	}
	msg := "网络安全策略已封锁会话：本次请求在账号选择前被本地拒绝"
	entry.UpstreamErrorMessage = &msg
	detail := map[string]string{
		"source":  "cyber_session_block",
		"reason":  "local_session_blocked",
		"message": msg,
	}
	if meta.SessionBlockKey != "" {
		entry.ErrorBody = "session_block_key=" + meta.SessionBlockKey
		detail["session_block_key"] = meta.SessionBlockKey
	}
	entry.UpstreamErrorDetail = opsJSONDetail(detail)
	if meta.UserID > 0 {
		entry.UserID = &meta.UserID
	}
	if meta.APIKeyID > 0 {
		entry.APIKeyID = &meta.APIKeyID
	}
	entry.GroupID = meta.GroupID
	if meta.ClientIP != "" {
		entry.ClientIP = &meta.ClientIP
	}
	return entry
}

func opsJSONDetail(detail map[string]string) *string {
	if len(detail) == 0 {
		return nil
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return nil
	}
	value := string(raw)
	return &value
}

// cyberSessionBlockFormat selects the per-endpoint error envelope for a locally
// blocked session (用户决策：兼容路径各自格式).
type cyberSessionBlockFormat int

const (
	cyberBlockFormatResponses cyberSessionBlockFormat = iota
	cyberBlockFormatChat
	cyberBlockFormatAnthropic
)

// rejectIfCyberSessionBlocked checks the session-block table BEFORE account
// selection. Returns true when the request was rejected (response already
// written + ops entry enqueued). Fail-open: disabled switch / empty key /
// store error → false.
func (h *OpenAIGatewayHandler) rejectIfCyberSessionBlocked(c *gin.Context, apiKey *service.APIKey, body []byte, model string, format cyberSessionBlockFormat) bool {
	if h == nil || h.gatewayService == nil || apiKey == nil {
		return false
	}
	// 开关默认关：先走 ~ns 级缓存开关检查，再付出 key 派生(gjson+sha256)成本。
	if enabled, _ := h.gatewayService.CyberSessionBlockRuntime(c.Request.Context()); !enabled {
		return false
	}
	key := service.CyberSessionBlockKey(apiKey.ID, c, body)
	if key == "" {
		return false
	}
	if !h.gatewayService.IsCyberSessionBlocked(c.Request.Context(), key) {
		return false
	}
	// body-signal compact 心跳可能已把响应头提交为 200（cyber 检查在用户槽位
	// 长等待之后执行）：以 response.failed 终止事件回传；未提交时停拍后照常
	// 写 JSON（#3887）。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		message := cyberSessionBlockedClientMessage(cyberSessionBlockPlatform(apiKey, "", format))
		service.MarkOpsStreamError(c, "permission_error", message, http.StatusForbidden)
		if writeResponsesFailedSSE(c, "permission_error", message) {
			h.enqueueCyberSessionBlockedOpsEntry(c, apiKey, model, key)
			h.recordCyberSessionBlockedRiskEvent(c, apiKey, model, key, body)
			return true
		}
	}
	switch format {
	case cyberBlockFormatAnthropic:
		c.JSON(http.StatusForbidden, gin.H{"type": "error", "error": gin.H{
			"type":    "permission_error",
			"message": cyberSessionBlockedClientMessage(cyberSessionBlockPlatform(apiKey, "", format)),
		}})
	default: // cyberBlockFormatResponses 与 cyberBlockFormatChat：同构的 OpenAI error envelope
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"type":    "permission_error",
			"code":    "session_blocked_by_cyber_policy",
			"message": cyberSessionBlockedClientMessage(cyberSessionBlockPlatform(apiKey, "", format)),
		}})
	}
	h.enqueueCyberSessionBlockedOpsEntry(c, apiKey, model, key)
	h.recordCyberSessionBlockedRiskEvent(c, apiKey, model, key, body)
	return true
}

// enqueueCyberSessionBlockedOpsEntry captures request meta and enqueues the
// ops_error_logs entry for a locally blocked request.
func (h *OpenAIGatewayHandler) enqueueCyberSessionBlockedOpsEntry(c *gin.Context, apiKey *service.APIKey, model string, sessionBlockKey string) {
	if h.opsService == nil {
		return
	}
	meta := cyberPolicyOpsErrorMeta{Model: model, InboundEndpoint: GetInboundEndpoint(c), CreatedAt: time.Now(), SessionBlockKey: sessionBlockKey}
	meta.RequestID = c.Writer.Header().Get("X-Request-Id")
	if c.Request != nil && c.Request.URL != nil {
		meta.RequestPath = c.Request.URL.Path
	}
	if v, ok := c.Get(opsStreamKey); ok {
		if b, ok := v.(bool); ok {
			meta.Stream = b
		}
	}
	meta.Platform = resolveOpsPlatform(apiKey, guessPlatformFromPath(meta.RequestPath))
	if c.Request != nil {
		meta.ClientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		meta.UserAgent = c.GetHeader("User-Agent")
		meta.ClientIP = strings.TrimSpace(ip.GetClientIP(c))
	}
	meta.APIKeyID = apiKey.ID
	meta.GroupID = apiKey.GroupID
	meta.APIKeyPrefix = keyPrefix(apiKey.Key, 8)
	if apiKey.User != nil {
		meta.UserID = apiKey.User.ID
	}
	enqueueOpsErrorLog(h.opsService, buildCyberSessionBlockedOpsEntry(meta))
}

func (h *OpenAIGatewayHandler) recordCyberSessionBlockedRiskEvent(c *gin.Context, apiKey *service.APIKey, model string, sessionBlockKey string, requestBody []byte) {
	if h == nil || h.contentModerationService == nil || c == nil {
		return
	}
	requestID := c.Writer.Header().Get("X-Request-Id")
	inboundEndpoint := GetInboundEndpoint(c)
	var userID, apiKeyID int64
	var userEmail, apiKeyName, groupName string
	var groupID *int64
	if apiKey != nil {
		apiKeyID = apiKey.ID
		apiKeyName = apiKey.Name
		groupID = apiKey.GroupID
		if apiKey.User != nil {
			userID = apiKey.User.ID
			userEmail = apiKey.User.Email
		}
		if apiKey.Group != nil {
			groupName = apiKey.Group.Name
		}
	}
	cmSvc := h.contentModerationService
	body := append([]byte(nil), requestBody...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmSvc.RecordCyberSessionBlockedEvent(ctx, service.CyberSessionBlockedRecordInput{
			RequestID:       requestID,
			UserID:          userID,
			UserEmail:       userEmail,
			APIKeyID:        apiKeyID,
			APIKeyName:      apiKeyName,
			GroupID:         groupID,
			GroupName:       groupName,
			Endpoint:        inboundEndpoint,
			Model:           model,
			SessionBlockKey: sessionBlockKey,
			RequestBody:     body,
		})
	}()
}

// recordCyberPolicyIfMarked 在 gateway forward 返回后检查 cyber 标记，异步写风控日志/邮件，
// 并在 forward 返回错误时写一条 tokens=0 用量行。标记由 gateway 服务层在透传 cyber 后设置；
// 当前请求已发给用户，本方法只做事后记录，不影响响应。forwardErrored 为 true 时才写用量行，
// 避免与正常 RecordUsage(forward 成功路径)重复。每请求至多记录一次。
func (h *OpenAIGatewayHandler) recordCyberPolicyIfMarked(c *gin.Context, apiKey *service.APIKey, account *service.Account, subscription *service.UserSubscription, model string, forwardErrored bool, cyberBlockKey string, channelFields service.ChannelUsageFields, requestPayloadHash string, requestBody []byte) {
	mark := service.GetOpsCyberPolicy(c)
	if mark == nil {
		return
	}
	if c.GetBool(cyberPolicyRecordedKey) {
		return
	}
	c.Set(cyberPolicyRecordedKey, true)

	requestID := c.Writer.Header().Get("X-Request-Id")
	var userID, apiKeyID int64
	var userEmail, apiKeyName, groupName string
	var groupID *int64
	if apiKey != nil {
		apiKeyID = apiKey.ID
		apiKeyName = apiKey.Name
		groupID = apiKey.GroupID
		if apiKey.User != nil {
			userID = apiKey.User.ID
			userEmail = apiKey.User.Email
		}
		if apiKey.Group != nil {
			groupName = apiKey.Group.Name
		}
	}
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := ""
	var accountID int64
	if account != nil {
		accountID = account.ID
		upstreamEndpoint = resolveOpenAIUpstreamEndpoint(c, account, nil)
	}
	stream := false
	if v, ok := c.Get(opsStreamKey); ok {
		if b, ok := v.(bool); ok {
			stream = b
		}
	}
	cmSvc := h.contentModerationService
	gwSvc := h.gatewayService
	opsSvc := h.opsService
	apiKeySvc := h.apiKeyService
	requestPath := ""
	if c.Request != nil && c.Request.URL != nil {
		requestPath = c.Request.URL.Path
	}
	platform := resolveOpsPlatform(apiKey, guessPlatformFromPath(requestPath))
	var clientRequestID, userAgent, clientIPStr string
	if c.Request != nil {
		clientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		userAgent = c.GetHeader("User-Agent")
		clientIPStr = strings.TrimSpace(ip.GetClientIP(c))
	}
	apiKeyPrefix := ""
	if apiKey != nil {
		apiKeyPrefix = keyPrefix(apiKey.Key, 8)
	}
	opsMeta := cyberPolicyOpsErrorMeta{
		RequestID:       requestID,
		ClientRequestID: clientRequestID,
		Platform:        platform,
		Model:           model,
		RequestPath:     requestPath,
		Stream:          stream,
		InboundEndpoint: inboundEndpoint,
		UserAgent:       userAgent,
		APIKeyPrefix:    apiKeyPrefix,
		UserID:          userID,
		APIKeyID:        apiKeyID,
		AccountID:       accountID,
		GroupID:         groupID,
		ClientIP:        clientIPStr,
		CreatedAt:       time.Now(),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cmSvc != nil {
			cmSvc.RecordCyberPolicyEvent(ctx, service.CyberPolicyRecordInput{
				RequestID:       requestID,
				UserID:          userID,
				UserEmail:       userEmail,
				APIKeyID:        apiKeyID,
				APIKeyName:      apiKeyName,
				GroupID:         groupID,
				GroupName:       groupName,
				Endpoint:        inboundEndpoint,
				Model:           model,
				UpstreamMessage: mark.Message,
				UpstreamBody:    mark.Body,
				UpstreamStatus:  mark.UpstreamStatus,
				UpstreamInTok:   mark.UpstreamInTok,
				UpstreamOutTok:  mark.UpstreamOutTok,
				RequestBody:     requestBody,
			})
		}
		if forwardErrored && gwSvc != nil {
			gwSvc.RecordCyberPolicyUsageLog(ctx, service.CyberPolicyUsageInput{
				APIKey:             apiKey,
				Account:            account,
				Subscription:       subscription,
				RequestID:          requestID,
				Model:              model,
				Stream:             stream,
				InputTokens:        mark.UpstreamInTok,
				OutputTokens:       mark.UpstreamOutTok,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIPStr,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      apiKeySvc,
				ChannelUsageFields: channelFields,
			})
		}
		if gwSvc != nil && cyberBlockKey != "" {
			gwSvc.MarkCyberSessionBlocked(ctx, cyberBlockKey)
		}
		if opsSvc != nil {
			enqueueOpsErrorLog(opsSvc, buildCyberPolicyOpsErrorEntry(opsMeta, mark))
		}
	}()
}

// clearCyberPolicyTurnState resets the cyber mark and the per-request recorded
// guard. WS-only: called at the END of AfterTurn, after recordCyberPolicyIfMarked
// and RecordUsage (which reads CyberBlocked) have both consumed the mark.
func clearCyberPolicyTurnState(c *gin.Context) {
	if c == nil {
		return
	}
	service.ClearOpsCyberPolicy(c)
	c.Set(cyberPolicyRecordedKey, false)
}

func summarizeWSCloseErrorForLog(err error) (string, string) {
	if err == nil {
		return "-", "-"
	}
	statusCode := coderws.CloseStatus(err)
	if statusCode == -1 {
		return "-", "-"
	}
	closeStatus := fmt.Sprintf("%d(%s)", int(statusCode), statusCode.String())
	closeReason := "-"
	var closeErr coderws.CloseError
	if errors.As(err, &closeErr) {
		reason := strings.TrimSpace(closeErr.Reason)
		if reason != "" {
			closeReason = reason
		}
	}
	return closeStatus, closeReason
}
