package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientmsg"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
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
	gatewayService             *service.OpenAIGatewayService
	billingCacheService        *service.BillingCacheService
	apiKeyService              *service.APIKeyService
	usageRecordWorkerPool      *service.UsageRecordWorkerPool
	errorPassthroughService    *service.ErrorPassthroughService
	contentModerationService   *service.ContentModerationService
	moderationGuard            moderationGuard
	pipeline                   *OpenAIGatewayPipeline
	forwardStageRegistry       *ForwardStageRegistry
	stageAdapterRegistry       *StageAdapterRegistry
	securityAuditCoordinator   *securityaudit.Coordinator
	grokMediaEligibilityProber grokMediaEligibilityProber
	opsService                 *service.OpsService
	concurrencyHelper          *ConcurrencyHelper
	imageLimiter               *imageConcurrencyLimiter
	imageUserLimiterMu         sync.Mutex
	imageUserLimiters          map[int64]*imageConcurrencyLimiter
	maxAccountSwitches         int
	cfg                        *config.Config
}

type openAIWSTurnChannelMappingSnapshot struct {
	turn    int
	mapping service.ChannelMappingResult
}

var errOpenAIWSUnsupportedModelSwitch = errors.New("selected account does not support websocket model switch")

func newOpenAIWSUnsupportedModelSwitchError(model string) error {
	cause := fmt.Errorf("%w: model %q", errOpenAIWSUnsupportedModelSwitch, strings.TrimSpace(model))
	return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "model switch requires reconnect", cause)
}

func shouldReportOpenAIWSProxyAccountFailure(err error) bool {
	return err != nil && !errors.Is(err, errOpenAIWSUnsupportedModelSwitch) && !service.IsOpenAIWSSessionPreemptedError(err)
}

// openAIWSIngressEndedByClient identifies normal client-side WebSocket
// termination so it is not reported as an upstream account failure.
func openAIWSIngressEndedByClient(err error) bool {
	if err == nil {
		return true
	}
	var closeErr *service.OpenAIWSClientCloseError
	if errors.As(err, &closeErr) && closeErr.StatusCode() == coderws.StatusNormalClosure {
		return true
	}
	if coderws.CloseStatus(err) == coderws.StatusNormalClosure {
		return true
	}
	return errors.Is(err, context.Canceled)
}

func openAIWSTurnBillingModel(result *service.OpenAIForwardResult, mapping service.ChannelMappingResult, requestedModel, upstreamModel string) string {
	billingModel := ""
	if result != nil {
		billingModel = strings.TrimSpace(result.BillingModel)
	}
	if billingModel == "" {
		billingModel = strings.TrimSpace(upstreamModel)
	}
	if billingModel == "" {
		billingModel = strings.TrimSpace(requestedModel)
	}

	requestedModel = strings.TrimSpace(requestedModel)
	switch mapping.BillingModelSource {
	case service.BillingModelSourceRequested:
		if requestedModel != "" {
			billingModel = requestedModel
		}
	case service.BillingModelSourceChannelMapped:
		mappedModel := strings.TrimSpace(mapping.MappedModel)
		if mappedModel != "" && mappedModel != requestedModel {
			billingModel = mappedModel
		}
	}
	return billingModel
}

type grokMediaEligibilityProber interface {
	ProbeMediaEligibility(ctx context.Context, accountID int64) (bool, string, error)
}

const maxOpenAIFirstOutputTimeoutSwitches = 1

func openAIForwardSucceededForScheduling(result *service.OpenAIForwardResult) bool {
	return result.SucceededForScheduling()
}

func openAIAccountScheduleModel(c *gin.Context, account *service.Account, forwardModel string, requireCompact bool, result *service.OpenAIForwardResult) string {
	if result != nil {
		if actual := strings.TrimSpace(result.UpstreamModel); actual != "" {
			return actual
		}
	}
	if c != nil {
		if value, ok := c.Get(service.OpsUpstreamModelKey); ok {
			if actual, ok := value.(string); ok && strings.TrimSpace(actual) != "" {
				return strings.TrimSpace(actual)
			}
		}
	}
	return service.ResolveOpenAIAccountUpstreamModelForRequest(account, forwardModel, requireCompact)
}

func resolveOpenAIMessagesDispatchMappedModel(c *gin.Context, apiKey *service.APIKey, requestedModel string) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	if apiKey.Group.Platform == service.PlatformComposite && c != nil && c.Request != nil {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok &&
			(platform == service.PlatformGrok || service.IsCNProvider(platform)) {
			return ""
		}
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

func seedOpenAIForwardImageIntentHint(c *gin.Context, channelMapped bool, imageIntent bool) {
	if channelMapped {
		// 渠道映射改变了规范请求，保持 unknown，由 Forward 按映射后的 model/body 初始化。
		return
	}
	service.SetOpenAIImageIntentHint(c, imageIntent)
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

func openAICompatibleRequestPlatform(ctx context.Context, apiKey *service.APIKey) string {
	if platform, ok := service.ResolvedTargetPlatformFromContext(ctx); ok {
		if platform == service.PlatformGrok {
			return service.PlatformGrok
		}
		return service.PlatformOpenAI
	}
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == service.PlatformGrok {
		return service.PlatformGrok
	}
	return service.PlatformOpenAI
}

func openAIResponsesRequiredImageCapability(reqModel string, body []byte) service.OpenAIImagesCapability {
	if service.IsExplicitImageGenerationIntent("/v1/responses", reqModel, body) {
		return service.OpenAIImagesCapabilityResponsesImageTool
	}
	return ""
}

func openAIResponsesRequiredCapability(imageIntent bool, platform string) service.OpenAIEndpointCapability {
	if imageIntent && platform == service.PlatformOpenAI {
		return service.OpenAIEndpointCapabilityResponses
	}
	return service.OpenAIEndpointCapabilityChatCompletions
}

// openAIResponsesRequiredCapabilityForRequest returns the endpoint capability
// required by an image or Responses request. needsResponses includes both the
// legacy /responses/compact endpoint and native remote compaction v2.
func openAIResponsesRequiredCapabilityForRequest(imageIntent bool, needsResponses bool, platform string) service.OpenAIEndpointCapability {
	if needsResponses && platform == service.PlatformOpenAI {
		return service.OpenAIEndpointCapabilityResponses
	}
	return openAIResponsesRequiredCapability(imageIntent, platform)
}

func allowOpenAICompatibleMessagesDispatch(c *gin.Context, apiKey *service.APIKey) bool {
	if apiKey == nil || apiKey.Group == nil {
		return true
	}
	if apiKey.Group.Platform == service.PlatformGrok {
		return true
	}
	if service.IsCNProvider(apiKey.Group.Platform) {
		return true
	}
	if apiKey.Group.Platform == service.PlatformComposite && c != nil && c.Request != nil {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok &&
			(platform == service.PlatformGrok || service.IsCNProvider(platform)) {
			return true
		}
	}
	return apiKey.Group.AllowMessagesDispatch
}

func openAICompatibleTextTargetAllowed(c *gin.Context, apiKey *service.APIKey, model string) bool {
	return compositeTargetPlatformAllowed(c, apiKey, model,
		service.PlatformOpenAI, service.PlatformGrok,
		service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek)
}

func isResponsesWebSocketCompositePlatform(platform string) bool {
	switch platform {
	case service.PlatformOpenAI, service.PlatformGrok:
		return true
	default:
		return false
	}
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
	var moderationBody []byte
	if preForwardRequest, ok := openAIHTTPPreForwardRequestFromContext(c, service.ContentModerationProtocolOpenAIResponses); ok {
		body = preForwardRequest.Body
		moderationBody = preForwardRequest.contentModerationBody()
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
		moderationBody = body
	}
	if normalizedBody, changed := normalizeCodexDelegationBootstrap(body); changed {
		body = normalizedBody
		reqLog.Info("openai.codex_delegation_bootstrap_normalized",
			zap.String("normalization", "call_output_to_user_message"),
		)
	}
	// Reasoning policy may rewrite the forwarded JSON. Moderation state is
	// keyed to the original client payload produced by the pre-forward guard.
	legacyCompact := service.IsOpenAIResponsesCompactPath(c)
	nativeV2 := isBareOpenAIResponsesPath(c) && isOpenAIRemoteCompactionV2Request(body)
	if nativeV2 {
		// 原生 v2 压缩出站前补注 x-codex-beta-features: remote_compaction_v2，
		// 与真实 Codex 线型一致（网关链剥头后本级负责恢复，#5586）。
		service.MarkOpenAINativeCompactionV2(c)
	}
	// body-signal compact：上游 unary 等待期间向下游发 SSE 注释行心跳，防止
	// 反向代理空闲超时掐断长压缩连接（#3887）。首拍延迟一个心跳间隔，快速
	// 失败仍走 JSON+状态码链路；未标记客户端流式或间隔为 0 时是 no-op。
	stopCompactKeepalive := service.StartOpenAICompactSSEKeepalive(c, h.openAICompactKeepaliveInterval())
	defer stopCompactKeepalive()

	if apiKey.Group != nil && apiKey.Group.Platform == service.PlatformOpenAI {
		if cappedBody, changed := service.ApplyOpenAIReasoningEffortPolicy(body, apiKey.Group.MaxReasoningEffort, apiKey.Group.ReasoningEffortMappings); changed {
			body = cappedBody
		}
	}
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !compositeTargetPlatformAllowed(c, apiKey, reqModel, service.PlatformOpenAI, service.PlatformGrok) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))
	previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	if previousResponseID != "" {
		previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
		reqLog = reqLog.With(
			zap.Bool("has_previous_response_id", true),
			zap.String("previous_response_id_kind", previousResponseIDKind),
			zap.Int("previous_response_id_len", len(previousResponseID)),
		)
		if previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
			reqLog.Warn("openai.request_validation_failed", zap.String("reason", "previous_response_id_looks_like_message_id"))
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id must be a response.id (resp_*), not a message id")
			return
		}
		groupID := int64(0)
		if apiKey.GroupID != nil {
			groupID = *apiKey.GroupID
		}
		owned, ownershipErr := h.gatewayService.ValidateOpenAIHTTPResponseOwner(
			c.Request.Context(), groupID, previousResponseID, subject.UserID, apiKey.ID,
		)
		if ownershipErr != nil {
			reqLog.Warn("openai.previous_response_owner_lookup_failed", zap.Error(ownershipErr))
		}
		if !owned {
			reqLog.Warn("openai.request_validation_failed", zap.String("reason", "previous_response_owner_mismatch"))
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id is not available for this user")
			return
		}
	}
	service.SetOpenAIHTTPResponseOwner(c, subject.UserID, apiKey.ID)

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, moderationBody); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	if imageReleaseFunc != nil {
		defer imageReleaseFunc()
	}

	requestCtx := c.Request.Context()
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)
	imageIntent := openAIResponsesImageIntentForPlatform(apiKey, reqModel, body)
	if h.rejectDirectOpenAIResponsesImagePermission(c, apiKey, imageIntent) {
		return
	}
	var requiredImageCapability service.OpenAIImagesCapability
	if imageIntent && requestPlatform == service.PlatformOpenAI {
		requiredImageCapability = openAIResponsesRequiredImageCapability(reqModel, body)
	}
	if requiredImageCapability != "" {
		requestCtx = service.WithOpenAIImageGenerationIntent(requestCtx)
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(requestCtx, apiKey.GroupID, reqModel)
	forwardBody := openAIModelMappedBody(body, channelMapping.Mapped, channelMapping.MappedModel, h.gatewayService.ReplaceModelInBody)
	seedOpenAIForwardImageIntentHint(c, channelMapping.Mapped, imageIntent)
	forwardModel := reqModel
	if channelMapping.Mapped {
		forwardModel = channelMapping.MappedModel
	}
	c.Request = c.Request.WithContext(service.WithOpenAIForwardModel(
		c.Request.Context(),
		forwardModel,
		legacyCompact,
	))

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
	if failoverClientGone(c) {
		return
	}

	// Generate session hash (header first; fallback to prompt_cache_key).
	// The pre-forward pipeline already rejects blocked cyber sessions.
	sessionHash := h.gatewayService.GenerateSessionHash(c, sessionHashBody)
	requestCtx = service.WithOpenAIGuardianParentAffinity(requestCtx, c, sessionHashBody, reqModel)
	c.Request = c.Request.WithContext(requestCtx)
	requireCompact := legacyCompact

	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	firstOutputTimeoutSwitchCount := 0
	var passthroughFailoverState openAIPassthroughFailoverState

	// 生图意图的 /v1/responses 请求必须调度到确实支持 Responses API 的账号，否则
	// 会在 forward 阶段被静默降级为无法生图的 Chat Completions 直转（#4417）。
	// 仅对 OpenAI 平台生效：Grok 生图走独立的 forwardGrokResponses 路径，不应被过滤。
	// 复用前置权限与并发阶段在未修改 body 上确认的显式生图意图，避免大 tools 请求重复扫描。
	// 该判断已排除 Codex 被动 image_gen namespace，避免 CC-only 账号被误过滤（#4476）。
	needsResponses := nativeV2 || legacyCompact
	requiredCapability := openAIResponsesRequiredCapabilityForRequest(imageIntent, needsResponses, requestPlatform)

	// 分组利润控制：请求级装配定价上下文——pricingAt 固定本请求的
	// D 与计费高峰因子，选号、槽位终检与全部 failover 重入共用同一门与阈值。
	// 生图意图只影响能力路由与图片计费，不关门：混合 /v1/responses 请求的
	// token 计费部分仍受利润门保护，独立图片/视频端点才在门外。
	pricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(requestCtx, apiKey.GroupID)
	requestCtx = pricingCtx
	requestCtx = service.WithCodexRestrictionRequest(requestCtx, c, body)
	c.Request = c.Request.WithContext(requestCtx)

	for {
		// Streaming forwards may outlive the client so usage can be drained, but a
		// canceled request must never start a new account attempt.
		if !openAIRequestAllowsFailoverReplay(c) {
			return
		}
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
			RequiredCapability:         requiredCapability,
			RequiredImageCapability:    requiredImageCapability,
			RequireCompact:             requireCompact,
			UseUpstreamTokenCost:       !imageIntent,
			RequestPlatform:            requestPlatform,
			Stream:                     reqStream,
			StreamStarted:              &streamStarted,
			MaxAccountSwitches:         maxAccountSwitches,
			SwitchCount:                &switchCount,
			LastFailoverErr:            lastFailoverErr,
			ProfitVetoCount:            &profitVetoCount,
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
		if previousResponseID != "" && requestPlatform == service.PlatformOpenAI && !account.IsOpenAIApiKey() {
			failedAccountIDs[account.ID] = struct{}{}
			if accountReleaseFunc != nil {
				accountReleaseFunc()
				accountReleaseFunc = nil
			}
			lastFailoverErr = &service.UpstreamFailoverError{
				StatusCode:       http.StatusBadRequest,
				Stage:            service.GatewayFailureStageInference,
				Scope:            service.GatewayFailureScopeRequest,
				Reason:           service.OpenAIHTTPContinuationUnsupportedReason,
				ClientStatusCode: http.StatusBadRequest,
				ClientMessage:    "previous_response_id requires an OpenAI API-key account for HTTP requests",
			}
			reqLog.Debug("openai.account_skipped_http_continuation_unsupported",
				zap.Int64("account_id", account.ID),
				zap.String("account_type", account.Type),
			)
			continue
		}

		// Forward request
		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()
		attemptBody := h.deriveOpenAIForwardAttemptBody(reqLog, forwardBody, account, &passthroughFailoverState)
		var writerSizeBeforeForward int
		var result *service.OpenAIForwardResult
		var err error
		stageResult := h.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
			GatewayService:          h.gatewayService,
			Kind:                    OpenAIHTTPForwardResponses,
			Account:                 account,
			Body:                    attemptBody,
			ReleaseFunc:             accountReleaseFunc,
			WriterSizeBeforeForward: &writerSizeBeforeForward,
			Result:                  &result,
		})
		err = stageResult.Err
		cyberBlockKeyHTTP := ""
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockKeyHTTP = service.CyberSessionExplicitBlockKey(apiKey.ID, c, sessionHashBody)
		}
		h.runOpenAIHTTPCyberUsageStage(c, OpenAIHTTPCyberUsageStageInput{
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			Model:              reqModel,
			ForwardErrored:     err != nil,
			CyberBlockKey:      cyberBlockKeyHTTP,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, ""),
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
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, resultUpstreamModel(result)),
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
					if failoverClientGone(c) {
						reqLog.Info("openai.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if !openAIForwardMayFailover(c, writerSizeBeforeForward, failoverErr) {
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.SafeToFailoverAfterWrite && c.Writer.Written() {
						streamStarted = true
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.runOpenAIHTTPScheduleResultStage(c, account, forwardModel, requireCompact, result, false, nil, err)
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					if openAIFirstOutputFailoverExhausted(failoverErr, &firstOutputTimeoutSwitchCount) {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					// 池模式：同账号重试
					if failoverErr.RetryableOnSameAccount {
						retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
						if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
							sameAccountRetryCount[account.ID]++
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(retryDelay):
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
					failoverSwitchFields := []zap.Field{
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					}
					if account.Proxy != nil {
						failoverSwitchFields = append(failoverSwitchFields,
							zap.Int64("proxy_id", account.Proxy.ID),
							zap.String("proxy_name", account.Proxy.Name),
							zap.String("proxy_host", account.Proxy.Host),
							zap.Int("proxy_port", account.Proxy.Port),
						)
					} else if account.ProxyID != nil {
						failoverSwitchFields = append(failoverSwitchFields, zap.Int64p("proxy_id", account.ProxyID))
					}
					reqLog.Warn("openai.upstream_failover_switching", failoverSwitchFields...)
					continue
				}
				h.runOpenAIHTTPScheduleResultStage(c, account, forwardModel, requireCompact, result, false, nil, err)
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
		sessionID := service.ExtractClientSessionID(c)

		// 使用量记录通过有界 worker 池提交，避免请求热路径创建无界 goroutine。
		cyberBlocked := service.GetOpsCyberPolicy(c) != nil
		scheduleSucceeded := true
		_ = h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
			Handler:                h,
			RequestContext:         c.Request.Context(),
			Result:                 result,
			APIKey:                 apiKey,
			Account:                account,
			Subscription:           subscription,
			InboundEndpoint:        inboundEndpoint,
			UpstreamEndpoint:       upstreamEndpoint,
			UserAgent:              userAgent,
			ClientIP:               clientIP,
			SessionID:              sessionID,
			RequestPayloadHash:     requestPayloadHash,
			QuotaPlatform:          quotaPlatform,
			ChannelUsageFields:     clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
			CyberBlocked:           cyberBlocked,
			ScheduleSuccess:        &scheduleSucceeded,
			ScheduleModel:          forwardModel,
			ScheduleRequireCompact: requireCompact,
			LogComponent:           "handler.openai_gateway.responses",
			LogMessage:             "openai.record_usage_failed",
			LogUserID:              subject.UserID,
			LogModel:               reqModel,
		})
		reqLog.Debug("openai.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

func isOpenAILegacyCompactPath(c *gin.Context) bool {
	return service.IsOpenAIResponsesCompactPath(c)
}

// isBareOpenAIResponsesPath 仅匹配裸 /responses 端点（无 /compact 等子路径），
// body-signal 提升只允许发生在这里，避免误伤 /responses/{id}/... 形态的请求。
func isBareOpenAIResponsesPath(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return false
	}
	normalizedPath := strings.TrimRight(strings.TrimSpace(c.Request.URL.Path), "/")
	switch normalizedPath {
	case EndpointResponses, "/openai/v1/responses", "/responses", "/backend-api/codex/responses":
		return true
	default:
		return false
	}
}

func isOpenAIRemoteCompactionV2Request(body []byte) bool {
	stream, valid := parseOpenAICompatibleStream(body)
	return valid && stream && service.HasCompactionTriggerInInput(body)
}

// normalizeOpenAIResponsesCompactRequest keeps Codex remote compaction v2 on
// its native streaming /responses wire and preserves the legacy body-signal
// promotion for non-streaming requests.
// 返回归一化后的 body；ok=false 表示错误响应已写出，调用方应直接 return。
func (h *OpenAIGatewayHandler) normalizeOpenAIResponsesCompactRequest(c *gin.Context, reqLog *zap.Logger, body []byte) ([]byte, bool) {
	isCompactRequest := isOpenAILegacyCompactPath(c)
	if !isCompactRequest && isBareOpenAIResponsesPath(c) && service.HasCompactionTriggerInInput(body) {
		if normalized, changed, err := service.NormalizeCompactionTriggerInputOrder(body); err != nil {
			reqLog.Warn("codex.remote_compact.trigger_order_normalization_failed", zap.Error(err))
		} else if changed {
			body = normalized
		}
		if isOpenAIRemoteCompactionV2Request(body) {
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
	if !isOpenAILegacyCompactPath(c) {
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
	if !allowOpenAICompatibleMessagesDispatch(c, apiKey) {
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

	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.anthropicErrorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	bindOpenAIReasoningEffortPolicyForMessagesRequest(c, apiKey, body)
	routingModel := service.NormalizeOpenAICompatRequestedModel(reqModel)
	preferredMappedModel := resolveOpenAIMessagesDispatchMappedModel(c, apiKey, reqModel)

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIMessages, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.anthropicSecurityAuditError(c, decision)
		return
	}

	// 解析渠道级模型映射
	channelMappingMsg, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)
	mappedBodyForMessages := newOpenAIModelMappedBodyCache(body, h.gatewayService.ReplaceModelInBody)

	// 绑定错误透传服务，允许 service 层在非 failover 错误场景复用规则。
	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)

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
	sessionHash, promptCacheKey = resolveOpenAIMessagesMetadataSession(c, sessionHash, promptCacheKey, reqModel, body)
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatAnthropic) {
		return
	}
	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	effectiveMappedModel := preferredMappedModel
	var err error

	// 分组利润控制：Messages 文本入口同样请求级装门并固定 pricingAt。
	msgPricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(msgPricingCtx)

	for {
		if failoverClientGone(c) {
			return
		}
		currentRoutingModel := routingModel
		if effectiveMappedModel != "" {
			currentRoutingModel = effectiveMappedModel
		}
		var account *service.Account
		var accountReleaseFunc func()
		routingRetry := false
		if routingStage := h.runOpenAIHTTPRoutingStage(c, OpenAIHTTPRoutingStage{
			Handler:              h,
			ReqLog:               reqLog,
			APIKey:               apiKey,
			SubjectUserID:        subject.UserID,
			RequestedModel:       currentRoutingModel,
			DisplayModel:         reqModel,
			SessionHash:          &sessionHash,
			FailedAccountIDs:     failedAccountIDs,
			RequiredTransport:    service.OpenAIUpstreamTransportAny,
			RequiredCapability:   service.OpenAIEndpointCapabilityChatCompletions,
			UseUpstreamTokenCost: true,
			RequestPlatform:      requestPlatform,
			Stream:               reqStream,
			StreamStarted:        &streamStarted,
			MaxAccountSwitches:   maxAccountSwitches,
			SwitchCount:          &switchCount,
			LastFailoverErr:      lastFailoverErr,
			ProfitVetoCount:      &profitVetoCount,
			ErrorFormat:          openAIHTTPRoutingErrorAnthropicMessages,
			LogPrefix:            "openai_messages",
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
			cyberBlockKeyMsg = service.CyberSessionExplicitBlockKey(apiKey.ID, c, body)
		}
		h.runOpenAIHTTPCyberUsageStage(c, OpenAIHTTPCyberUsageStageInput{
			APIKey:             apiKey,
			Account:            account,
			Subscription:       subscription,
			Model:              reqModel,
			ForwardErrored:     err != nil,
			CyberBlockKey:      cyberBlockKeyMsg,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMappingMsg, reqModel, ""),
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
				ChannelUsageFields: clientRequestedUsageFields(c, channelMappingMsg, reqModel, resultUpstreamModel(result)),
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
					if failoverClientGone(c) {
						reqLog.Info("openai_messages.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					if c.Writer.Size() != writerSizeBeforeForward {
						h.gatewayService.ObserveOpenAIAccountHealthFailure(c.Request.Context(), account, err)
						h.handleAnthropicFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.runOpenAIHTTPScheduleResultStage(c, account, currentRoutingModel, false, result, false, nil, err)
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleAnthropicFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					// 池模式：同账号重试
					if failoverErr.RetryableOnSameAccount {
						retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
						if sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
							sameAccountRetryCount[account.ID]++
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai_messages.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(retryDelay):
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
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
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
				h.runOpenAIHTTPScheduleResultStage(c, account, currentRoutingModel, false, result, false, nil, err)
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
		sessionID := service.ExtractClientSessionID(c)

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
			SessionID:          sessionID,
			RequestPayloadHash: requestPayloadHash,
			QuotaPlatform:      quotaPlatform,
			ChannelUsageFields: clientRequestedUsageFields(c, channelMappingMsg, reqModel, resultUpstreamModel(result)),
			ScheduleModel:      currentRoutingModel,
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

func resolveOpenAIMessagesMetadataSession(c *gin.Context, sessionHash, promptCacheKey, reqModel string, body []byte) (string, string) {
	// Anthropic metadata.user_id 只作为账号粘性信号。上游 GPT/Codex 缓存键
	// 交给 ForwardAsAnthropic 从 cache_control 或完整消息 digest 派生，避免
	// 固定 metadata key 压住后续 turn 的缓存滚动。
	//
	// Claude Code 的会话头只用于本地账号粘性，不提升为上游 prompt_cache_key。
	if promptCacheKey == "" {
		if claudeSessionID := service.ClaudeCodeSessionIDFromHeader(c); claudeSessionID != "" {
			return service.DeriveSessionHashFromSeed(claudeSessionID), promptCacheKey
		}
	}
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
	if failoverErr != nil {
		copyFailoverRetryAfter(c, failoverErr.ResponseHeaders)
	}
	if failoverErr != nil && failoverErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(failoverErr)
		h.anthropicStreamingAwareError(c, status, "api_error", message, streamStarted)
		return
	}
	if failoverErr != nil && failoverErr.IsOpenAICapacityShed() && strings.TrimSpace(failoverErr.ClientMessage) != "" {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		h.anthropicStreamingAwareError(c, status, "api_error", failoverErr.ClientMessage, streamStarted)
		return
	}
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

func normalizeCodexDelegationBootstrap(body []byte) ([]byte, bool) {
	if !hasUniqueJSONMembers(body) {
		return body, false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var request map[string]any
	if err := decoder.Decode(&request); err != nil {
		return body, false
	}
	if previousResponseID, exists := request["previous_response_id"]; exists {
		value, ok := previousResponseID.(string)
		if !ok || strings.TrimSpace(value) != "" {
			return body, false
		}
	}
	input, ok := request["input"].([]any)
	if !ok {
		return body, false
	}
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := stringField(item, "type")
		if typ == "item_reference" || strings.HasSuffix(typ, "_call") {
			return body, false
		}
		if isResponsesCallOutputType(typ) {
			callIDValue, exists := item["call_id"]
			callID, isString := callIDValue.(string)
			if exists && (!isString || strings.TrimSpace(callID) != "") {
				return body, false
			}
			if !isCodexDelegationCandidate(item) {
				return body, false
			}
		}
	}
	changed := false
	for i, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok || !isCodexDelegationCandidate(item) {
			continue
		}
		output, ok := item["output"].(string)
		if !ok || !validCodexDelegationEnvelope(output) {
			continue
		}
		input[i] = map[string]any{
			"type": "message", "role": "user",
			"content": []any{map[string]any{"type": "input_text", "text": output}},
		}
		changed = true
	}
	if !changed {
		return body, false
	}
	normalized, err := json.Marshal(request)
	if err != nil {
		return body, false
	}
	return normalized, true
}

func hasUniqueJSONMembers(body []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if !consumeUniqueJSONValue(decoder) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}

func consumeUniqueJSONValue(decoder *json.Decoder) bool {
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return true
	}
	switch delim {
	case '{':
		members := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return false
			}
			key, ok := keyToken.(string)
			if !ok {
				return false
			}
			if _, duplicate := members[key]; duplicate {
				return false
			}
			members[key] = struct{}{}
			if !consumeUniqueJSONValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !consumeUniqueJSONValue(decoder) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func isResponsesCallOutputType(typ string) bool {
	return strings.HasSuffix(typ, "_call_output") || typ == "tool_search_output"
}

func isCodexDelegationCandidate(item map[string]any) bool {
	if stringField(item, "type") != "function_call_output" ||
		!isCodexDelegationTool(stringField(item, "namespace"), stringField(item, "name")) {
		return false
	}
	output, ok := item["output"].(string)
	return ok && validCodexDelegationEnvelope(output)
}

func stringField(item map[string]any, key string) string {
	value, _ := item[key].(string)
	return value
}

func isCodexDelegationTool(namespace, name string) bool {
	return (namespace == "codex_app" || namespace == "codex_tui") &&
		(name == "create_thread" || name == "send_message_to_thread")
}

func validCodexDelegationEnvelope(value string) bool {
	decoder := xml.NewDecoder(strings.NewReader(value))
	var rootSeen, sourceSeen, inputSeen bool
	var childName string
	var childText bytes.Buffer
	depth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return rootSeen && depth == 0 && sourceSeen && inputSeen
		}
		if err != nil {
			return false
		}
		switch current := token.(type) {
		case xml.StartElement:
			depth++
			if current.Name.Space != "" || len(current.Attr) != 0 || (depth == 1 && current.Name.Local != "codex_delegation") || depth > 2 {
				return false
			}
			if depth == 1 {
				if rootSeen {
					return false
				}
				rootSeen = true
				continue
			}
			if current.Name.Local != "source_thread_id" && current.Name.Local != "input" {
				return false
			}
			childName = current.Name.Local
			childText.Reset()
		case xml.EndElement:
			if current.Name.Space != "" {
				return false
			}
			if depth == 2 {
				if current.Name.Local != childName || strings.TrimSpace(childText.String()) == "" {
					return false
				}
				if childName == "source_thread_id" {
					if sourceSeen {
						return false
					}
					sourceSeen = true
				} else {
					if inputSeen {
						return false
					}
					inputSeen = true
				}
				childName = ""
			}
			depth--
			if depth < 0 {
				return false
			}
		case xml.CharData:
			if depth == 2 {
				_, _ = childText.Write(current)
			} else if len(bytes.TrimSpace(current)) != 0 {
				return false
			}
		case xml.Comment, xml.ProcInst, xml.Directive:
			return false
		}
	}
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

// openAISlotAcquireResult 是账号槽位获取的三态结果。
type openAISlotAcquireResult int

const (
	openAISlotAcquireOK openAISlotAcquireResult = iota
	// openAISlotAcquireFailed：错误响应已写出，调用方直接 return。
	openAISlotAcquireFailed
	// openAISlotAcquireProfitVetoed：槽位获取成功后利润终检否决。槽位已释放、
	// 未写任何响应；调用方应经 recordOpenAIProfitVeto 把该账号加入本请求排除集
	// 后重新选号，全池耗尽由下一轮选号返回标准 no available accounts。
	openAISlotAcquireProfitVetoed
	// openAISlotAcquireRetryableRejected：选号后账号已失效，未写响应；调用方
	// 应排除该账号后重新选号，但不应把它计入利润否决预算。
	openAISlotAcquireRetryableRejected
)

type openAISlotRetryReason int

const (
	openAISlotRetryNone openAISlotRetryReason = iota
	openAISlotRetryCapacity
	openAISlotRetryAccountUnavailable
	openAISlotRetryProfitVeto
)

// openAIWSTurnPricing 持有 WebSocket 连接内「当前 turn」的计费定价时刻。
// 由 BeforeTurn 在每个 turn 开始时冻结，AfterTurn 的用量提交读取它；turn 在
// 连接内串行推进，互斥锁只为跨用量提交 goroutine 的读取安全。
//
// 零值语义（重要）：ws_v2 passthrough ingress 只实现了 AfterTurn，没有任何
// turn 起始回调，BeforeTurn 永远不会被调用。此时本值保持零，RecordUsage 经
// openAIUsagePricingAt 回退到记录时刻——与引入分组利润控制前的基线一致。
// 绝不能用建连时刻初始化：那会把透传连接的所有 turn 钉死在建连时的高峰因子，
// 客户端只要峰前一分钟建连并保活，整条连接就能全程按谷价结算，正是利润控制
// 想堵的漏洞。透传 ingress 目前不做 turn 级利润复核，只有建连时的准入门。
type openAIWSTurnPricing struct {
	mu sync.Mutex
	at time.Time
}

func (p *openAIWSTurnPricing) freeze(at time.Time) {
	p.mu.Lock()
	p.at = at
	p.mu.Unlock()
}

func (p *openAIWSTurnPricing) current() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.at
}

func (p *openAIWSTurnPricing) currentOr(fallback time.Time) time.Time {
	if current := p.current(); !current.IsZero() {
		return current
	}
	return fallback
}

// recordOpenAIProfitVeto 记录 OpenAI 侧选号循环的一次利润门终检否决：把账号
// 加入本请求排除集并递增否决计数。返回 false 表示否决次数已达
// maxProfitVetoAttempts，调用方必须停止重选并按「无可用账号」终止。
//
// OpenAI 路径用的是各自的 failedAccountIDs map + for 循环（不是 FailoverState），
// 这里用一个独立计数器复用同一上限语义。上限是必需的：WaitPlan 分支先阻塞
// 排队（sticky 45s / fallback 30s）拿到槽位才终检，无上限重选会把单次请求的
// 延迟放大到 N × WaitPlan.Timeout。
func recordOpenAIProfitVeto(failedAccountIDs map[int64]struct{}, accountID int64, vetoCount *int) bool {
	failedAccountIDs[accountID] = struct{}{}
	*vetoCount++
	return *vetoCount < maxProfitVetoAttempts
}

// handleOpenAIProfitVetoExhausted 在利润否决预算耗尽时写出错误响应。
// 与 acquireResponsesAccountSlot 内部的 no-available-accounts 失败分支同形，
// 保证同一调用方在两条路径上拿到一致的响应格式。
func (h *OpenAIGatewayHandler) handleOpenAIProfitVetoExhausted(
	c *gin.Context,
	streamStarted bool,
	reqLog *zap.Logger,
	vetoCount int,
) {
	reqLog.Warn("openai.profit_veto_attempts_exhausted", zap.Int("profit_veto_count", vetoCount))
	markOpsRoutingCapacityLimited(c)
	h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", profitVetoExhaustedMessage, streamStarted)
}

func (h *OpenAIGatewayHandler) acquireResponsesAccountSlotForRequest(
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
) (func(), *service.Account, bool, openAISlotRetryReason) {
	if selection == nil || selection.Account == nil {
		markOpsRoutingCapacityLimited(c)
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, openAISlotRetryNone
	}

	// 终检与准入后绑定使用选号结果携带的门：composite 等跨分组调度解析出的
	// 门只存在于调度栈的局部 ctx，必须经选号结果重放到本函数的 ctx 上。
	ctx := service.ContextWithSelectionProfitGate(c.Request.Context(), selection)
	account := selection.Account
	if selection.Acquired {
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
		if refreshErr != nil {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
			if errors.Is(refreshErr, service.ErrNoAvailableAccounts) {
				return nil, account, false, openAISlotRetryAccountUnavailable
			}
			markOpsRoutingCapacityLimited(c)
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
			return nil, nil, false, openAISlotRetryNone
		}
		selection.Account = refreshed
		account = refreshed
		latest, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(ctx, refreshed)
		if vetoed {
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			reqLog.Debug("openai.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			return nil, account, false, openAISlotRetryProfitVeto
		}
		account = latest
		selection.Account = latest
		// 调度器已抢槽路径无门时由选号内部完成 eager 绑定；门下选号内部
		// 推迟绑定，这里在终检通过后补准入后绑定。
		if selection.ProfitGateActive() {
			if err := h.gatewayService.BindStickySessionAfterProfitAdmission(ctx, groupID, sessionHash, account.ID); err != nil {
				reqLog.Warn("openai.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			}
		}
		return wrapReleaseOnDone(ctx, selection.ReleaseFunc), account, true, openAISlotRetryNone
	}
	if selection.WaitPlan == nil {
		markOpsRoutingCapacityLimited(c)
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, openAISlotRetryNone
	}

	fastReleaseFunc, fastAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(
		ctx,
		account.ID,
		selection.WaitPlan.MaxConcurrency,
	)
	if err != nil {
		reqLog.Warn("openai.account_slot_quick_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		h.handleConcurrencyError(c, err, "account", *streamStarted)
		return nil, nil, false, openAISlotRetryNone
	}
	if fastAcquired {
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
		if refreshErr != nil {
			if fastReleaseFunc != nil {
				fastReleaseFunc()
			}
			reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
			if errors.Is(refreshErr, service.ErrNoAvailableAccounts) {
				return nil, account, false, openAISlotRetryAccountUnavailable
			}
			markOpsRoutingCapacityLimited(c)
			h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
			return nil, nil, false, openAISlotRetryNone
		}
		selection.Account = refreshed
		account = refreshed
		// 分组利润控制：快速抢槽成功后终检。选号与抢槽之间账号
		// 倍率可能刷新，越线则释放槽位交由调用方排除重选，不绑定粘连。
		latest, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(ctx, refreshed)
		if vetoed {
			if fastReleaseFunc != nil {
				fastReleaseFunc()
			}
			reqLog.Debug("openai.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			return nil, account, false, openAISlotRetryProfitVeto
		}
		account = latest
		selection.Account = latest
		if err := h.gatewayService.BindStickySessionAfterProfitAdmission(ctx, groupID, sessionHash, account.ID); err != nil {
			reqLog.Warn("openai.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
		}
		return wrapReleaseOnDone(ctx, fastReleaseFunc), account, true, openAISlotRetryNone
	}

	canWait, waitErr := h.concurrencyHelper.IncrementAccountWaitCount(ctx, account.ID, selection.WaitPlan.MaxWaiting)
	if waitErr != nil {
		reqLog.Warn("openai.account_wait_counter_increment_failed", zap.Int64("account_id", account.ID), zap.Error(waitErr))
	} else if !canWait {
		reqLog.Info("openai.account_wait_queue_full",
			zap.Int64("account_id", account.ID),
			zap.Int("max_waiting", selection.WaitPlan.MaxWaiting),
		)
		return nil, nil, false, openAISlotRetryCapacity
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
			return nil, nil, false, openAISlotRetryCapacity
		}
		h.handleConcurrencyError(c, err, "account", *streamStarted)
		return nil, nil, false, openAISlotRetryNone
	}

	// Slot acquired: no longer waiting in queue.
	releaseWait()
	refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, requestedModel, requireCompact, requiredCapability, requiredImageCapability)
	if refreshErr != nil {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		reqLog.Info("openai.selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
		if errors.Is(refreshErr, service.ErrNoAvailableAccounts) {
			return nil, account, false, openAISlotRetryAccountUnavailable
		}
		markOpsRoutingCapacityLimited(c)
		h.handleStreamingAwareError(c, http.StatusServiceUnavailable, "api_error", "No available accounts", *streamStarted)
		return nil, nil, false, openAISlotRetryNone
	}
	selection.Account = refreshed
	account = refreshed
	// 分组利润控制：WaitPlan 排队成功后终检。排队期间账号倍率
	// 可能上调，越线则释放槽位交由调用方排除重选，不绑定粘连。
	latest, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(ctx, refreshed)
	if vetoed {
		if accountReleaseFunc != nil {
			accountReleaseFunc()
		}
		reqLog.Debug("openai.account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
		return nil, account, false, openAISlotRetryProfitVeto
	}
	account = latest
	selection.Account = latest
	if err := h.gatewayService.BindStickySessionAfterProfitAdmission(ctx, groupID, sessionHash, account.ID); err != nil {
		reqLog.Warn("openai.bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
	return wrapReleaseOnDone(ctx, accountReleaseFunc), account, true, openAISlotRetryNone
}

func (h *OpenAIGatewayHandler) acquireResponsesAccountSlot(
	c *gin.Context,
	groupID *int64,
	sessionHash string,
	selection *service.AccountSelectionResult,
	reqStream bool,
	streamStarted *bool,
	reqLog *zap.Logger,
) (func(), openAISlotAcquireResult) {
	release, rejectedAccount, acquired, retryReason := h.acquireResponsesAccountSlotForRequest(
		c, groupID, sessionHash, selection, "", false, "", "", reqStream, streamStarted, reqLog,
	)
	if acquired {
		return release, openAISlotAcquireOK
	}
	if retryReason == openAISlotRetryProfitVeto && rejectedAccount != nil {
		return nil, openAISlotAcquireProfitVetoed
	}
	if retryReason == openAISlotRetryAccountUnavailable && rejectedAccount != nil {
		return nil, openAISlotAcquireRetryableRejected
	}
	return nil, openAISlotAcquireFailed
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
	clientLifecycleCtx := c.Request.Context()
	ctx := clientLifecycleCtx
	maxIngressConnections := 0
	if h.cfg != nil {
		maxIngressConnections = h.cfg.Gateway.OpenAIWS.MaxIngressConnectionsPerAPIKey
	}
	ingressLease, ingressLeaseAcquired, ingressLeaseErr := h.concurrencyHelper.AcquireOpenAIWSIngressLease(ctx, apiKey.ID, maxIngressConnections)
	if ingressLeaseErr != nil {
		reqLog.Error("openai.websocket_ingress_lease_acquire_failed", zap.Error(ingressLeaseErr))
		h.errorResponse(c, http.StatusServiceUnavailable, "service_unavailable", "WebSocket ingress capacity is temporarily unavailable")
		return
	}
	if !ingressLeaseAcquired {
		reqLog.Info("openai.websocket_ingress_capacity_rejected", zap.Int("max_ingress_connections_per_api_key", maxIngressConnections))
		c.Header("Retry-After", "5")
		h.errorResponse(c, http.StatusTooManyRequests, "rate_limit_error", "Too many open WebSocket connections, please retry later")
		return
	}
	if ingressLease != nil {
		defer ingressLease.Release()
		ctx = ingressLease.Context()
		c.Request = c.Request.WithContext(ctx)
	}

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

	firstMessageTimeout := service.ResolveOpenAIWSClientFirstMessageTimeout(h.cfg)
	msgType, firstMessage, err := service.ReadOpenAIWSClientMessage(
		ctx,
		wsConn,
		firstMessageTimeout,
		coderws.StatusPolicyViolation,
		"missing first response.create message",
	)
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
			zap.Duration("read_timeout", firstMessageTimeout),
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
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	ctx = c.Request.Context()
	if apiKey.Group != nil && apiKey.Group.Platform == service.PlatformComposite {
		platform, ok := service.ResolvedTargetPlatformFromContext(ctx)
		if !ok || (platform != service.PlatformOpenAI && platform != service.PlatformGrok) {
			closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, "Responses WebSocket API only supports OpenAI-compatible models for composite groups")
			return
		}
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
		zap.String("session_initial_model", reqModel),
		zap.Bool("has_previous_response_id", previousResponseID != ""),
		zap.String("previous_response_id_kind", previousResponseIDKind),
	)
	setOpsRequestContext(c, reqModel, true)
	setOpsEndpointContext(c, "", int16(service.RequestTypeWSV2))

	beginContentModerationFrame(c, ctx)
	if moderationDecision := h.checkWithModerationGuard(c, reqLog, moderationGuardInput{
		APIKey: apiKey, Subject: subject, Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model: reqModel, Body: firstMessage,
	}); moderationDecision != nil && moderationDecision.Blocked {
		pipelineResult := openAIWebSocketPipelineResult{
			Blocked: true, BlockReason: openAIWebSocketPipelineBlockReasonModeration,
			ModerationDecision: moderationDecision, Message: moderationDecision.Message,
		}
		closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, reqModel, pipelineResult)
		closeOpenAIClientWS(wsConn, openAIWebSocketPipelineCloseStatus(pipelineResult), closeReason)
		return
	}
	if decision := h.checkSecurityAuditStage(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, reqModel, firstMessage, "first_turn"); decision != nil && !decision.AllowNextStage {
		writeSecurityAuditWSError(ctx, wsConn, decision)
		closeOpenAIClientWS(wsConn, securityAuditWSCloseStatus(decision), securityAuditWSCloseReason(decision))
		return
	}

	imageIntent := service.IsImageGenerationIntentForPlatform("/v1/responses", reqModel, firstMessage, openAICompatibleRequestPlatform(c.Request.Context(), apiKey))
	if imageIntent && !service.GroupAllowsImageGeneration(apiKey.Group) {
		closeOpenAIClientWS(wsConn, coderws.StatusPolicyViolation, service.ImageGenerationPermissionMessage())
		return
	}
	// F5a: the first response.create body is available here, so the cyber stage
	// can apply both explicit-session and scope-gated transcript matching.
	initialPipelineResult := h.runOpenAIWebSocketFramePipeline(c, OpenAIWebSocketInitialFramePipelineAdapter{}, reqLog, openAIWebSocketPipelineInput{
		APIKey:        apiKey,
		Subject:       subject,
		Protocol:      service.ContentModerationProtocolOpenAIResponses,
		Model:         reqModel,
		Body:          firstMessage,
		CyberBody:     firstMessage,
		ImageEndpoint: "/v1/responses",
	})
	cyberBlockKey := initialPipelineResult.CyberBlockKey
	if initialPipelineResult.Blocked {
		closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, reqModel, initialPipelineResult)
		closeOpenAIClientWS(wsConn, openAIWebSocketPipelineCloseStatus(initialPipelineResult), closeReason)
		return
	}
	cyberBlockedThisConn := false
	var cyberTurnBodiesMu sync.Mutex
	cyberTurnBodies := map[int][]byte{1: append([]byte(nil), firstMessage...)}
	setCyberTurnBody := func(turn int, payload []byte) {
		cyberTurnBodiesMu.Lock()
		cyberTurnBodies[turn] = append([]byte(nil), payload...)
		cyberTurnBodiesMu.Unlock()
	}
	takeCyberTurnBody := func(turn int) []byte {
		cyberTurnBodiesMu.Lock()
		body := cyberTurnBodies[turn]
		delete(cyberTurnBodies, turn)
		cyberTurnBodiesMu.Unlock()
		return body
	}

	// 解析渠道级模型映射
	channelMappingWS, _ := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, reqModel)
	wsForwardModel := reqModel
	if channelMappingWS.Mapped && strings.TrimSpace(channelMappingWS.MappedModel) != "" {
		wsForwardModel = strings.TrimSpace(channelMappingWS.MappedModel)
	}

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
	requestPlatform := openAICompatibleRequestPlatform(ctx, apiKey)
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
	ctx = service.WithOpenAIGuardianParentAffinity(ctx, c, firstMessage, reqModel)
	ctx = service.WithCodexRestrictionRequest(ctx, c, firstMessage)
	c.Request = c.Request.WithContext(ctx)
	maxAccountSwitches := h.maxAccountSwitches
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var lastFailoverErr *service.UpstreamFailoverError
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	wsAttemptMessage := append([]byte(nil), firstMessage...)
	waitForWSSameAccountRetry := func(account *service.Account, failoverErr *service.UpstreamFailoverError) bool {
		if account == nil || failoverErr == nil || failoverErr.StatusCode != http.StatusTooManyRequests || failoverErr.SameAccountRetryDeadline.IsZero() {
			return false
		}
		retryLimit := effectiveSameAccountRetryLimit(failoverErr, account)
		if !sameAccountRetryAllowed(failoverErr, sameAccountRetryCount[account.ID], retryLimit) {
			return false
		}
		sameAccountRetryCount[account.ID]++
		retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
		reqLog.Warn("openai.websocket.same_account_retry",
			zap.Int64("account_id", account.ID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("retry_count", sameAccountRetryCount[account.ID]),
			zap.Duration("retry_delay", retryDelay),
		)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(retryDelay):
			return true
		}
	}
	handleWSFailover := func(account *service.Account, failoverErr *service.UpstreamFailoverError) bool {
		if ctx.Err() != nil {
			return false
		}
		if failoverErr.ShouldReportAccountScheduleFailure() {
			h.gatewayService.ReportOpenAIAccountScheduleResult(
				account,
				openAIAccountScheduleModel(c, account, wsForwardModel, false, nil),
				false,
				nil,
				failoverErr,
			)
		}
		releaseAccountSlot()
		if !failoverErr.ShouldRetryNextAccount() {
			closeOpenAIWSFailoverExhausted(c, wsConn, failoverErr)
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		h.gatewayService.RecordOpenAIAccountSwitch()
		failedAccountIDs[account.ID] = struct{}{}
		h.gatewayService.CooldownUserAccount(ctx, subject.UserID, account.ID, h.gatewayService.UserAccountCooldownTTL(ctx))
		lastFailoverErr = failoverErr
		if switchCount >= maxAccountSwitches {
			closeOpenAIWSFailoverExhausted(c, wsConn, failoverErr)
			return false
		}
		switchCount++
		if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
			closeOpenAIWSFailoverExhausted(c, wsConn, failoverErr)
			return false
		}
		reqLog.Warn("openai.websocket_upstream_failover_switching",
			zap.Int64("account_id", account.ID),
			zap.Int("upstream_status", failoverErr.StatusCode),
			zap.Int("switch_count", switchCount),
			zap.Int("max_switches", maxAccountSwitches),
		)
		if ctx.Err() != nil {
			return false
		}
		return ensureUserSlotHeld()
	}
	// 与 HTTP Responses 路径保持一致：生图意图请求要求账号支持 Responses API（#4417）。
	// WSv2 传输本身已隐含 Responses 支持，此处为防御性对齐。
	// Platform-aware intent keeps passive tool declarations from being treated as image requests.
	requiredCapability := service.OpenAIEndpointCapabilityChatCompletions
	if service.IsImageGenerationIntentForPlatform("/v1/responses", reqModel, firstMessage, requestPlatform) && requestPlatform == service.PlatformOpenAI {
		requiredCapability = service.OpenAIEndpointCapabilityResponses
	}

	// 分组利润控制：WS 桥按连接装配定价上下文并装门（选号与抢槽共用该
	// ctx）。连接内不重选号，但每个 turn 开始经 BeforeTurn 重新冻结 pricingAt
	// 并按最新门复核当前账号（准入与计费同源），峰前建连保活不能让后续 turn
	// 继续按建连时刻的谷价计费。生图意图只影响能力路由与图片计费，不关门。
	// 建连时刻只用于选号/准入，不作为任何 turn 的计费定价时刻。
	wsPricingCtx, _ := h.gatewayService.WithOpenAIRequestPricingContext(ctx, apiKey.GroupID)
	ctx = wsPricingCtx

	for {
		var account *service.Account
		var accountMaxConcurrency int
		var token string
		stickyPreviousHit := false
		scheduleLayer := ""
		routingRetry := false
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
			RequiredCapability:      requiredCapability,
			UseUpstreamTokenCost:    !imageIntent,
			PreviousResponseCanMove: previousResponseCanMove,
			RequestPlatform:         requestPlatform,
			ClientConn:              wsConn,
			LastFailoverErr:         lastFailoverErr,
			HandleFailover:          handleWSFailover,
			ProfitVetoCount:         &profitVetoCount,
			Retry:                   &routingRetry,
			AdmittedContext:         &ctx,
			Account:                 &account,
			AccountMaxConcurrency:   &accountMaxConcurrency,
			CurrentAccountRelease:   &currentAccountRelease,
			Token:                   &token,
			StickyPreviousHit:       &stickyPreviousHit,
			ScheduleLayer:           &scheduleLayer,
		}); routingStage.Stop {
			return
		}
		if routingRetry {
			continue
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
			closeOpenAIClientWS(wsConn, openAIWebSocketPipelineCloseStatus(pipelineResult), closeReason)
			return
		}
		maxReasoningEffort, reasoningEffortMappings, _ := openAIReasoningEffortPolicyForRequest(c, apiKey)
		var requestPayloadHash string
		// Passthrough rejects overlapping response.create frames, so one immutable
		// turn-tagged slot preserves the exact mapping used for the in-flight request.
		var turnChannelMapping atomic.Pointer[openAIWSTurnChannelMappingSnapshot]
		turnChannelMapping.Store(&openAIWSTurnChannelMappingSnapshot{turn: 1, mapping: channelMappingWS})
		// turn 级定价：BeforeTurn 重新冻结 pricingAt 并按最新门复核当前账号，
		// AfterTurn 的计费读取所属 turn 的时刻。零值起步的语义见
		// openAIWSTurnPricing 的注释——绝不能用建连时刻初始化。
		var turnPricing openAIWSTurnPricing
		hooks := &service.OpenAIWSIngressHooks{
			ClientLifecycleContext:  clientLifecycleCtx,
			InitialRequestModel:     reqModel,
			MaxReasoningEffort:      maxReasoningEffort,
			ReasoningEffortMappings: reasoningEffortMappings,
			BeforeRequest: func(turn int, payload []byte, originalModel string) error {
				c.Set(securityAuditWSTurnContextKey, turn)
				service.BeginOpsStreamTurn(c, turn)
				setCyberTurnBody(turn, payload)
				// Passthrough ingress intentionally skips BeforeTurn, so enforce only
				// the connection-level cyber session gate here as well. Native ingress
				// visits this hook first and gets the same side-effect-free close error;
				// its original BeforeTurn guard remains as defense in depth.
				if cyberBlockedThisConn {
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, cyberSessionBlockedClientMessageForAPIKey(apiKey), nil)
				}
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
				frameCtx := context.WithValue(ctx, ctxkey.RequestedPublicModel, model)
				beginContentModerationFrame(c, frameCtx)
				if moderationDecision := h.checkWithModerationGuard(c, reqLog, moderationGuardInput{
					APIKey: apiKey, Subject: subject, Protocol: service.ContentModerationProtocolOpenAIResponses,
					Model: model, Body: payload,
				}); moderationDecision != nil && moderationDecision.Blocked {
					pipelineResult := openAIWebSocketPipelineResult{
						Blocked: true, BlockReason: openAIWebSocketPipelineBlockReasonModeration,
						ModerationDecision: moderationDecision, Message: moderationDecision.Message,
					}
					closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, model, pipelineResult)
					return service.NewOpenAIWSClientCloseError(openAIWebSocketPipelineCloseStatus(pipelineResult), closeReason, nil)
				}
				if decision := h.checkSecurityAuditStage(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, payload, "subsequent_turn"); decision != nil && !decision.AllowNextStage {
					writeSecurityAuditWSError(ctx, wsConn, decision)
					return service.NewOpenAIWSClientCloseError(securityAuditWSCloseStatus(decision), securityAuditWSCloseReason(decision), nil)
				}
				pipelineResult := h.runOpenAIWebSocketFramePipeline(c, OpenAIWebSocketFollowupFramePipelineAdapter{}, reqLog, openAIWebSocketPipelineInput{
					APIKey:    apiKey,
					Subject:   subject,
					Protocol:  service.ContentModerationProtocolOpenAIResponses,
					Model:     model,
					Body:      payload,
					CyberBody: payload,
				})
				if pipelineResult.Blocked {
					closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, model, pipelineResult)
					return service.NewOpenAIWSClientCloseError(openAIWebSocketPipelineCloseStatus(pipelineResult), closeReason, nil)
				}
				if gate := runSelectedAccountContentModeration(c, reqLog, h.contentModerationService, apiKey, subject, service.ContentModerationProtocolOpenAIResponses, model, payload, account); gate != nil && gate.Decision != nil && gate.Decision.Blocked {
					pipelineResult := openAIWebSocketPipelineResult{
						Blocked:            true,
						BlockReason:        openAIWebSocketPipelineBlockReasonModeration,
						ModerationDecision: gate.Decision,
						Message:            gate.Decision.Message,
					}
					closeReason := h.writeOpenAIWebSocketPipelineBlock(ctx, c, wsConn, apiKey, model, pipelineResult)
					return service.NewOpenAIWSClientCloseError(openAIWebSocketPipelineCloseStatus(pipelineResult), closeReason, nil)
				}
				return nil
			},
			MapRequestModel: func(turn int, originalModel string) (string, error) {
				model := strings.TrimSpace(originalModel)
				if model == "" {
					model = reqModel
				}
				setOpsRequestContext(c, model, true)
				mapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, model)
				mappedModelUnchanged := false
				if previous := turnChannelMapping.Load(); previous != nil && previous.turn < turn {
					mappedModelUnchanged = strings.TrimSpace(previous.mapping.MappedModel) == strings.TrimSpace(mapping.MappedModel)
				}
				if turn > 1 && !mappedModelUnchanged && !account.IsModelSupported(model) && !account.IsModelSupported(mapping.MappedModel) {
					return "", newOpenAIWSUnsupportedModelSwitchError(mapping.MappedModel)
				}
				turnChannelMapping.Store(&openAIWSTurnChannelMappingSnapshot{turn: turn, mapping: mapping})
				return mapping.MappedModel, nil
			},
			BeforeTurn: func(turn int) error {
				// turn==1 的会话屏蔽已由握手层检查覆盖；连接内 flag 只拦截后续 turn。
				if cyberBlockedThisConn {
					return service.NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, cyberSessionBlockedClientMessageForAPIKey(apiKey), nil)
				}
				// 长连接跨峰谷/倍率刷新防护：每个 turn 按当前时刻重装门并复核
				// 当前账号，越线即要求客户端重连重选（连接绑定单一上游账号，
				// 无法中途换号）。本 turn 的准入与计费共用同一 pricingAt。
				turnCtx, turnAt := h.gatewayService.WithOpenAITurnPricingContext(ctx, apiKey.GroupID)
				if _, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(turnCtx, account); vetoed {
					reqLog.Info("openai.websocket_turn_profit_vetoed",
						zap.Int("turn", turn),
						zap.Int64("account_id", account.ID),
						zap.String("reason", reason))
					return service.NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "account is no longer eligible for this connection, please reconnect", nil)
				}
				turnPricing.freeze(turnAt)
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
				completeContentModerationFrame(c, turn, turnErr)
				cyberBlockBody := takeCyberTurnBody(turn)
				turnRequestedModel := reqModel
				turnUpstreamModel := ""
				if result != nil && turn > 1 {
					if model := strings.TrimSpace(result.Model); model != "" {
						turnRequestedModel = model
					}
				}
				if result != nil {
					turnUpstreamModel = strings.TrimSpace(result.UpstreamModel)
				}
				var turnMapping service.ChannelMappingResult
				if snapshot := turnChannelMapping.Load(); snapshot != nil && snapshot.turn == turn {
					turnMapping = snapshot.mapping
				} else {
					turnMapping, _ = h.gatewayService.ResolveChannelMappingAndRestrict(ctx, apiKey.GroupID, turnRequestedModel)
				}
				if turnUpstreamModel == "" {
					turnUpstreamModel = turnRequestedModel
				}
				if result != nil {
					result.BillingModel = openAIWSTurnBillingModel(result, turnMapping, turnRequestedModel, turnUpstreamModel)
				}
				_ = h.runOpenAIWebSocketStage(c, OpenAIWebSocketUsageStage{
					Handler:              h,
					RequestContext:       ctx,
					ReqLog:               reqLog,
					APIKey:               apiKey,
					Account:              account,
					Subscription:         subscription,
					Model:                turnRequestedModel,
					UpstreamModel:        turnUpstreamModel,
					TurnErr:              turnErr,
					Result:               result,
					CyberBlockKey:        cyberBlockKey,
					CyberBlockBody:       cyberBlockBody,
					ChannelMapping:       turnMapping,
					RequestPayloadHash:   requestPayloadHash,
					ReleaseTurnSlots:     releaseTurnSlots,
					CyberBlockedThisConn: &cyberBlockedThisConn,
					UserAgent:            userAgent,
					ClientIP:             clientIP,
					SessionID:            service.ExtractClientSessionID(c),
					PricingAt:            turnPricing.current(),
				})
			},
		}

		wsFirstMessage := wsAttemptMessage
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
		if preemptCtx, cleanupPreempt, armed := h.gatewayService.BeginOpenAIWSIngressSessionPreemption(ctx, c, account, wsFirstMessage); armed {
			ctx = preemptCtx
			defer cleanupPreempt()
		}

		for {
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
			if proxyErr == nil {
				reqLog.Info("openai.websocket_ingress_closed", zap.Int64("account_id", account.ID))
				return
			}
			if service.IsOpenAIWSSessionPreemptedError(proxyErr) {
				return
			}
			var failoverErr *service.UpstreamFailoverError
			if errors.As(proxyErr, &failoverErr) {
				retryPayload, retryCurrentTurn := service.OpenAIWSCurrentTurnRetryPayload(proxyErr)
				nextAttemptMessage, retrySafe := openAIWSNextAttemptMessage(wsAttemptMessage, retryPayload, retryCurrentTurn)
				if !retrySafe {
					closeOpenAIWSFailoverExhausted(c, wsConn, failoverErr)
					return
				}
				wsAttemptMessage = nextAttemptMessage
				if retryCurrentTurn {
					previousResponseID = ""
					reqLog.Warn("openai.websocket_current_turn_failover_retry",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("retry_payload_bytes", len(retryPayload)),
					)
				}
				if waitForWSSameAccountRetry(account, failoverErr) {
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, wsForwardModel, false, nil), false, nil, proxyErr)
					}
					if !ensureUserSlotHeld() {
						return
					}
					if currentAccountRelease == nil {
						accountRelease, acquired, acquireErr := h.concurrencyHelper.TryAcquireAccountSlot(ctx, account.ID, accountMaxConcurrency)
						if acquireErr != nil || !acquired {
							reqLog.Warn("openai.websocket_same_account_retry_slot_unavailable",
								zap.Int64("account_id", account.ID),
								zap.Error(acquireErr),
							)
							closeOpenAIClientWS(wsConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
							return
						}
						currentAccountRelease = wrapReleaseOnDone(ctx, accountRelease)
					}
					wsFirstMessage = wsAttemptMessage
					continue
				}
				if handleWSFailover(account, failoverErr) {
					break
				}
				return
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
			hasClientCloseErr := errors.As(proxyErr, &closeErr)
			if openAIWSIngressEndedByClient(proxyErr) {
				closedFields := []zap.Field{zap.Int64("account_id", account.ID)}
				if hasClientCloseErr {
					closedFields = append(closedFields, zap.String("reason", closeErr.Reason()))
				} else {
					closedFields = append(closedFields, zap.Error(proxyErr))
				}
				reqLog.Info("openai.websocket_ingress_closed_normally", closedFields...)
				if hasClientCloseErr {
					closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
				} else {
					closeOpenAIClientWS(wsConn, coderws.StatusNormalClosure, "")
				}
				return
			}

			if shouldReportOpenAIWSProxyAccountFailure(proxyErr) {
				h.gatewayService.ReportOpenAIAccountScheduleResult(account, openAIAccountScheduleModel(c, account, wsForwardModel, false, nil), false, nil, proxyErr)
			}
			closeStatus, closeReason := summarizeWSCloseErrorForLog(proxyErr)
			proxyFailedFields := []zap.Field{
				zap.Int64("account_id", account.ID),
				zap.Error(proxyErr),
				zap.String("close_status", closeStatus),
				zap.String("close_reason", closeReason),
			}
			if account.Proxy != nil {
				proxyFailedFields = append(proxyFailedFields,
					zap.Int64("proxy_id", account.Proxy.ID),
					zap.String("proxy_name", account.Proxy.Name),
					zap.String("proxy_host", account.Proxy.Host),
					zap.Int("proxy_port", account.Proxy.Port),
				)
			} else if account.ProxyID != nil {
				proxyFailedFields = append(proxyFailedFields, zap.Int64p("proxy_id", account.ProxyID))
			}
			reqLog.Warn("openai.websocket_proxy_failed", proxyFailedFields...)
			if hasClientCloseErr {
				closeOpenAIClientWS(wsConn, closeErr.StatusCode(), closeErr.Reason())
				return
			}
			closeOpenAIClientWS(wsConn, coderws.StatusInternalError, "upstream websocket proxy failed")
			return
		}
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
		if mode := h.usageRecordWorkerPool.Submit(task); mode != service.UsageRecordSubmitModeDroppedStopped {
			return
		}
		// 池已停止（进程关停窗口）：计费任务不能静默丢失，降级为内联同步执行。
		// 显式配置的 drop/sample 溢出丢弃仍按配置语义保留。
		logger.L().With(
			zap.String("component", "handler.openai_gateway.responses"),
		).Warn("openai.usage_record_task_stopped_sync_fallback")
	}
	// 回退路径：worker 池未注入或已停止时同步执行，避免退回到无界 goroutine 模式。
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
	// Money-critical bills never drop on pool overflow: media, search surcharge, voice.
	if result != nil && (result.ImageCount > 0 || result.VideoCount > 0 ||
		result.SearchCount > 0 || result.WebSearchCalls > 0 || result.AudioUsage != nil) {
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
		if mode := h.usageRecordWorkerPool.Submit(task); !mode.Dropped() {
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
	if failoverErr == nil {
		h.handleFailoverExhaustedSimple(c, http.StatusBadGateway, streamStarted)
		return
	}
	if failoverErr.IsOpenAIRequestBodyTooLarge() {
		service.SetOpsUpstreamError(c, http.StatusRequestEntityTooLarge, service.OpenAIRequestBodyTooLargeClientMessage, "")
		h.handleStreamingAwareError(
			c,
			http.StatusRequestEntityTooLarge,
			"invalid_request_error",
			service.OpenAIRequestBodyTooLargeClientMessage,
			streamStarted,
		)
		return
	}
	if failoverErr.Reason == service.OpenAIHTTPContinuationUnsupportedReason {
		message := strings.TrimSpace(failoverErr.ClientMessage)
		if message == "" {
			message = "previous_response_id requires an OpenAI API-key account for HTTP requests"
		}
		h.handleStreamingAwareError(c, http.StatusBadRequest, "invalid_request_error", message, streamStarted)
		return
	}
	copyFailoverRetryAfter(c, failoverErr.ResponseHeaders)
	if failoverErr.IsCredentialFailure() {
		status, message := credentialFailoverClientResponse(failoverErr)
		h.handleStreamingAwareError(c, status, "upstream_error", message, streamStarted)
		return
	}
	if failoverErr.IsOpenAICapacityShed() && strings.TrimSpace(failoverErr.ClientMessage) != "" {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		h.handleStreamingAwareError(c, status, "server_error", failoverErr.ClientMessage, streamStarted)
		return
	}
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

func credentialFailoverClientResponse(failoverErr *service.UpstreamFailoverError) (int, string) {
	if failoverErr != nil && failoverErr.Reason == service.OpenAIUpstreamAccessStateReason && strings.TrimSpace(failoverErr.ClientMessage) != "" {
		status := failoverErr.ClientStatusCode
		if status <= 0 {
			status = http.StatusServiceUnavailable
		}
		return status, failoverErr.ClientMessage
	}
	if failoverErr != nil && failoverErr.Reason == service.AntigravityCredentialRejectedReason {
		return http.StatusBadGateway, service.AntigravityCredentialRejectedClientMessage
	}
	return http.StatusServiceUnavailable, service.GrokCredentialUnavailableClientMessage
}

func copyFailoverRetryAfter(c *gin.Context, headers http.Header) {
	if c == nil || headers == nil {
		return
	}
	retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
	if retryAfter == "" || len(retryAfter) > 128 || strings.ContainsAny(retryAfter, "\r\n") || !isSafeRetryAfter(retryAfter) {
		return
	}
	c.Header("Retry-After", retryAfter)
}

func isSafeRetryAfter(value string) bool {
	digitsOnly := true
	for _, char := range value {
		if char < '0' || char > '9' {
			digitsOnly = false
			break
		}
	}
	if digitsOnly {
		seconds, err := strconv.ParseUint(value, 10, 32)
		return err == nil && seconds <= uint64((7*24*time.Hour)/time.Second)
	}
	retryAt, err := http.ParseTime(value)
	if err != nil {
		return false
	}
	return !retryAt.After(time.Now().Add(7 * 24 * time.Hour))
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
	h.handleStreamingAwareErrorWithCode(c, status, errType, "", message, streamStarted, false)
}

func (h *OpenAIGatewayHandler) handleStreamingAwareErrorWithCode(
	c *gin.Context,
	status int,
	errType string,
	code string,
	message string,
	streamStarted bool,
	countTowardsSLA bool,
) {
	markOpsClientMessageDiagnostic(c, errType, message)
	// body-signal compact 心跳可能已把响应头提交为 200：先停心跳（建立
	// happens-before，接管 ResponseWriter），并升级为流内错误处理。
	if service.StopOpenAICompactSSEKeepaliveCommitted(c) {
		streamStarted = true
	}
	if streamStarted {
		if countTowardsSLA {
			service.MarkOpsStreamFailure(c, errType, code, message, status)
		} else {
			service.MarkOpsStreamError(c, errType, message, status)
		}
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
			errorObject := gin.H{"type": errType, "message": clientmsg.Localize(message)}
			if code != "" {
				errorObject["code"] = code
			}
			payload, err := json.Marshal(gin.H{"error": errorObject})
			if err != nil {
				payload = []byte(`{"error":{"type":"upstream_error","message":"Upstream request failed"}}`)
			}
			errorEvent := "event: error\ndata: " + string(payload) + "\n\n"
			if _, err := fmt.Fprint(c.Writer, errorEvent); err != nil {
				_ = c.Error(err)
			}
			flusher.Flush()
		}
		return
	}

	// Normal case: return JSON response with proper status code
	if code == "" {
		h.errorResponse(c, status, errType, message)
		return
	}
	c.JSON(status, gin.H{"error": gin.H{
		"type": errType, "code": code, "message": clientmsg.Localize(message),
	}})
}

func (h *OpenAIGatewayHandler) ensureOpenAIStreamReadErrorResponse(c *gin.Context, err error, streamStarted bool) bool {
	code, message, ok := service.OpenAIUpstreamStreamReadErrorDetails(err)
	if !ok || c == nil || c.Writer == nil || service.IsResponseCommitted(c) {
		return false
	}
	if c.Writer.Written() {
		streamStarted = true
	}
	h.handleStreamingAwareErrorWithCode(
		c, http.StatusBadGateway, "upstream_error", code, message, streamStarted, true,
	)
	return true
}

// ensureForwardErrorResponse 在 Forward 返回错误但尚未写响应时补写统一错误响应。
func (h *OpenAIGatewayHandler) ensureForwardErrorResponse(c *gin.Context, streamStarted bool) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	// 先停 compact 心跳再读 Writer 状态，避免与心跳 goroutine 竞争。
	compactKeepaliveCommitted := service.StopOpenAICompactSSEKeepaliveCommitted(c)
	if compactKeepaliveCommitted {
		streamStarted = true
	}
	imageKeepalivePresent := service.OpenAIImagesJSONKeepalivePresent(c)
	service.StopOpenAIImagesJSONKeepaliveCommitted(c)
	imageKeepalivePaddingOnly := false
	imageKeepaliveResponseWritten := false
	if imageKeepalivePresent {
		adjustedSize := service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
		imageKeepalivePaddingOnly = adjustedSize < 0
		imageKeepaliveResponseWritten = adjustedSize >= 0
	}
	compactKeepaliveHasMeaningfulOutput := compactKeepaliveCommitted && service.OpenAICompactKeepaliveAdjustedWrittenSize(c) > 0
	// Compact keepalive may have committed 200 headers without writing a
	// semantic SSE event. In that case the Responses stream still needs its
	// protocol-correct terminal response.failed event.
	if (service.IsResponseCommitted(c) && (!compactKeepaliveCommitted || compactKeepaliveHasMeaningfulOutput)) || (!compactKeepaliveCommitted && imageKeepaliveResponseWritten) {
		return false
	}
	if c.Writer.Written() && !imageKeepalivePaddingOnly {
		streamStarted = true
	}
	if imageKeepalivePaddingOnly {
		markOpsClientMessageDiagnostic(c, "upstream_error", "Upstream request failed")
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"type":    "upstream_error",
				"message": "Upstream request failed",
			},
		})
		return true
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

func openAIForwardMayFailover(c *gin.Context, writerSizeBeforeForward int, failoverErr *service.UpstreamFailoverError) bool {
	if c == nil || c.Writer == nil {
		return false
	}
	if service.OpenAICompactKeepaliveAdjustedWrittenSize(c) == writerSizeBeforeForward {
		return true
	}
	return failoverErr != nil && failoverErr.SafeToFailoverAfterWrite
}

func openAIRequestAllowsFailoverReplay(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	return !failoverClientGone(c)
}

func openAIFirstOutputFailoverExhausted(failoverErr *service.UpstreamFailoverError, switchCount *int) bool {
	if failoverErr == nil ||
		!failoverErr.SafeToFailoverAfterWrite ||
		failoverErr.StatusCode != http.StatusGatewayTimeout ||
		switchCount == nil {
		return false
	}
	if *switchCount >= maxOpenAIFirstOutputTimeoutSwitches {
		return true
	}
	*switchCount = *switchCount + 1
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

func openAIWSNextAttemptMessage(current, retryPayload []byte, retryCurrentTurn bool) ([]byte, bool) {
	if !retryCurrentTurn {
		return append([]byte(nil), current...), true
	}
	if len(retryPayload) == 0 {
		return nil, false
	}
	return append([]byte(nil), retryPayload...), true
}

func closeOpenAIWSFailoverExhausted(c *gin.Context, conn *coderws.Conn, failoverErr *service.UpstreamFailoverError) {
	intendedStatus := http.StatusBadGateway
	errorType := "upstream_error"
	errorCode := "upstream_ws_failover_exhausted"
	message := "upstream websocket proxy failed"
	closeStatus := coderws.StatusInternalError

	if failoverErr != nil {
		if reason := strings.TrimSpace(string(failoverErr.Reason)); reason != "" {
			errorCode = reason
		}
		if failoverErr.Stage == service.GatewayFailureStageAccountAuth {
			intendedStatus = http.StatusServiceUnavailable
			errorType = "api_error"
			message = service.GrokCredentialUnavailableClientMessage
			closeStatus = coderws.StatusTryAgainLater
		} else {
			switch failoverErr.StatusCode {
			case http.StatusTooManyRequests:
				intendedStatus = http.StatusTooManyRequests
				errorType = "rate_limit_error"
				message = "upstream rate limit exceeded, please retry later"
				closeStatus = coderws.StatusTryAgainLater
			case 529, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
				intendedStatus = failoverErr.StatusCode
				message = "upstream service temporarily unavailable"
				closeStatus = coderws.StatusTryAgainLater
			case http.StatusUnauthorized, http.StatusForbidden:
				intendedStatus = failoverErr.StatusCode
				errorType = "authentication_error"
				message = "upstream websocket authentication failed"
				closeStatus = coderws.StatusPolicyViolation
			}
		}
	}

	service.MarkOpsStreamFailure(c, errorType, errorCode, message, intendedStatus)
	closeOpenAIClientWS(conn, closeStatus, message)
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
	eventID := "evt_content_moderation_blocked"
	errorType := "invalid_request_error"
	if contentModerationIsNonViolationDeferred(decision) {
		eventID = "evt_content_review_deferred"
		errorType = "server_error"
	}
	payload, err := json.Marshal(gin.H{
		"event_id": eventID,
		"type":     "error",
		"error": gin.H{
			"type":    errorType,
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

func openAIWebSocketPipelineCloseStatus(result openAIWebSocketPipelineResult) coderws.StatusCode {
	if result.BlockReason == openAIWebSocketPipelineBlockReasonModeration &&
		contentModerationIsNonViolationDeferred(result.ModerationDecision) {
		return coderws.StatusTryAgainLater
	}
	return coderws.StatusPolicyViolation
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
	key := findBlockedCyberSessionKey(c.Request.Context(), h.gatewayService, apiKey.ID, c, body)
	if key == "" {
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

type cyberSessionBlockWritePlan struct {
	scopeKey string
	keys     []string
}

func buildCyberSessionBlockWritePlan(apiKeyID int64, c *gin.Context, body []byte) cyberSessionBlockWritePlan {
	plan := cyberSessionBlockWritePlan{}
	if key := service.CyberSessionExplicitBlockKey(apiKeyID, c, body); key != "" {
		plan.keys = append(plan.keys, key)
	}
	transcriptKeys := service.CyberSessionTranscriptBlockKeys(apiKeyID, body)
	for _, key := range transcriptKeys {
		if len(plan.keys) == 0 || key != plan.keys[0] {
			plan.keys = append(plan.keys, key)
		}
	}
	if len(transcriptKeys) > 0 {
		plan.scopeKey = cyberSessionScopeKey(apiKeyID, c)
	}
	return plan
}

func findBlockedCyberSessionKey(ctx context.Context, gatewayService *service.OpenAIGatewayService, apiKeyID int64, c *gin.Context, body []byte) string {
	if gatewayService == nil {
		return ""
	}
	clientIP, userAgent := "", ""
	if c != nil {
		clientIP = strings.TrimSpace(ip.GetClientIP(c))
		userAgent = c.GetHeader("User-Agent")
	}
	return gatewayService.FindCyberSessionBlockedForRequest(ctx, apiKeyID, c, body, clientIP, userAgent)
}

func cyberSessionScopeKey(apiKeyID int64, c *gin.Context) string {
	if c == nil {
		return ""
	}
	return service.CyberSessionScopeKey(apiKeyID, strings.TrimSpace(ip.GetClientIP(c)), c.GetHeader("User-Agent"))
}

// enqueueCyberSessionBlockedOpsEntry captures request meta and enqueues the
// ops_error_logs entry for a locally blocked request.
func (h *OpenAIGatewayHandler) enqueueCyberSessionBlockedOpsEntry(c *gin.Context, apiKey *service.APIKey, model string, sessionBlockKey string) {
	if h.opsService == nil {
		return
	}
	// The dedicated cyber_session_blocked entry owns Ops semantics for this
	// request; suppress the generic middleware record of the same 403 response.
	c.Set(opsDedicatedErrorRecordedKey, true)
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
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	meta.Platform = resolveOpsPlatform(requestCtx, apiKey, guessPlatformFromPath(meta.RequestPath))
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
	model = clientRequestedModel(c, model)

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
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	platform := resolveOpsPlatform(requestCtx, apiKey, guessPlatformFromPath(requestPath))
	var clientRequestID, userAgent, clientIPStr string
	if c.Request != nil {
		clientRequestID, _ = c.Request.Context().Value(ctxkey.ClientRequestID).(string)
		userAgent = c.GetHeader("User-Agent")
		clientIPStr = strings.TrimSpace(ip.GetClientIP(c))
	}
	// 提前拍成标量，避免在下方 goroutine 内访问 gin.Context。
	sessionID := service.ExtractClientSessionID(c)
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
	if gwSvc != nil && apiKey != nil {
		plan := buildCyberSessionBlockWritePlan(apiKey.ID, c, requestBody)
		if cyberBlockKey != "" {
			found := false
			for _, key := range plan.keys {
				if key == cyberBlockKey {
					found = true
					break
				}
			}
			if !found {
				plan.keys = append(plan.keys, cyberBlockKey)
			}
		}
		if len(plan.keys) > 0 {
			blockCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			gwSvc.MarkCyberSessionBlocked(blockCtx, plan.scopeKey, plan.keys)
			cancel()
		}
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
				SessionID:          sessionID,
				RequestPayloadHash: requestPayloadHash,
				APIKeyService:      apiKeySvc,
				ChannelUsageFields: channelFields,
			})
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
