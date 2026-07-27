package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/clientmsg"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type gatewayPreForwardErrorFormat int

const (
	gatewayPreForwardErrorAnthropic gatewayPreForwardErrorFormat = iota
	gatewayPreForwardErrorGemini
	gatewayPreForwardErrorOpenAIChat
	gatewayPreForwardErrorOpenAIResponses
)

type gatewayPreForwardPipelineInput struct {
	APIKey      *service.APIKey
	Subject     middleware2.AuthSubject
	Protocol    string
	Model       string
	Body        []byte
	ErrorFormat gatewayPreForwardErrorFormat
}

type gatewayPreForwardPipelineResult struct {
	Blocked bool
}

type gatewayPreForwardRequest struct {
	Protocol    string
	Model       string
	Body        []byte
	Stream      bool
	Parsed      *service.ParsedRequest
	ErrorFormat gatewayPreForwardErrorFormat
}

const gatewayPreForwardRequestContextKey = "gateway_pre_forward_request"

type GatewayPreForwardPipeline struct {
	moderationGuard moderationGuard
	stages          []gatewayPreForwardStage
}

func newGatewayPreForwardPipeline(guard moderationGuard) *GatewayPreForwardPipeline {
	if guard == nil {
		guard = newContentModerationGuard(nil)
	}
	return &GatewayPreForwardPipeline{moderationGuard: guard}
}

func (p *GatewayPreForwardPipeline) CheckModeration(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	guard := newContentModerationGuard(nil)
	if p != nil && p.moderationGuard != nil {
		guard = p.moderationGuard
	}
	return guard.Check(c, reqLog, input)
}

func (h *GatewayHandler) runGatewayPreForwardPipeline(c *gin.Context, reqLog *zap.Logger, input gatewayPreForwardPipelineInput) gatewayPreForwardPipelineResult {
	pipeline := h.gatewayPreForwardPipeline()
	return pipeline.Run(h, c, reqLog, input)
}

func (h *GatewayHandler) EnterGatewayPreForwardPipeline(c *gin.Context, meta moderationcoverage.Entry) gatewayPreForwardPipelineResult {
	meta = moderationcoverage.NormalizeEntry(meta)
	if meta.Pipeline != moderationcoverage.PipelineGatewayPreForward {
		return gatewayPreForwardPipelineResult{}
	}
	if !gatewayPreForwardEntrypointSupported(meta.Handler, meta.Protocol) {
		return gatewayPreForwardPipelineResult{}
	}
	if h.contentModerationService != nil {
		protectedCtx, err := h.contentModerationService.AcquireRequestResources(c.Request.Context(), c.Request.ContentLength, c.GetHeader("Content-Encoding"))
		if err != nil {
			if errors.Is(err, service.ErrRequestMemoryBudgetExhausted) {
				c.Header("Retry-After", "1")
				writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusTooManyRequests, "request_memory_budget_exhausted", "Request memory budget exhausted")
			} else {
				var sizeErr *service.RequestBodyTooLargeError
				if errors.As(err, &sizeErr) {
					markOpsRequestBodyTooLarge(c, sizeErr.Limit, nil, false)
					writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusRequestEntityTooLarge, "request_body_too_large", buildBodyTooLargeMessage(sizeErr.Limit))
				} else {
					writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusBadRequest, "invalid_request_error", err.Error())
				}
			}
			return gatewayPreForwardPipelineResult{Blocked: true}
		}
		c.Request = c.Request.WithContext(protectedCtx)
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return gatewayPreForwardPipelineResult{Blocked: true}
	}
	if meta.Protocol == service.ContentModerationProtocolGemini && !middleware2.HasForcePlatform(c) {
		if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformGemini {
			googleError(c, http.StatusBadRequest, "API key group platform is not gemini")
			return gatewayPreForwardPipelineResult{Blocked: true}
		}
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusInternalServerError, "api_error", "User context not found")
		return gatewayPreForwardPipelineResult{Blocked: true}
	}
	reqLog := requestLogger(
		c,
		"handler.gateway.messages",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	body, parsedReq, model, stream, ok := h.readGatewayPreForwardEntrypointRequest(c, meta)
	if !ok {
		return gatewayPreForwardPipelineResult{Blocked: true}
	}
	reqLog = reqLog.With(zap.String("model", model), zap.Bool("stream", stream))
	setOpsRequestContext(c, model, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))
	if model == "" {
		writeGatewayPreForwardEntrypointError(c, meta.Protocol, http.StatusBadRequest, "invalid_request_error", "model is required")
		return gatewayPreForwardPipelineResult{Blocked: true}
	}

	errorFormat := gatewayPreForwardErrorFormatForHandler(meta.Handler)
	result := h.runGatewayPreForwardPipeline(c, reqLog, gatewayPreForwardPipelineInput{
		APIKey:      apiKey,
		Subject:     subject,
		Protocol:    meta.Protocol,
		Model:       model,
		Body:        body,
		ErrorFormat: errorFormat,
	})
	if result.Blocked {
		return result
	}
	setGatewayPreForwardRequest(c, gatewayPreForwardRequest{
		Protocol:    meta.Protocol,
		Model:       model,
		Body:        body,
		Stream:      stream,
		Parsed:      parsedReq,
		ErrorFormat: errorFormat,
	})
	restoreRequestBody(c, body)
	return gatewayPreForwardPipelineResult{}
}

func gatewayPreForwardEntrypointSupported(handlerName, protocol string) bool {
	switch strings.TrimSpace(protocol) {
	case service.ContentModerationProtocolAnthropicMessages:
		switch strings.TrimSpace(handlerName) {
		case "GatewayHandler.Messages", "GatewayHandler.CountTokens":
			return true
		default:
			return false
		}
	case service.ContentModerationProtocolGemini:
		return strings.TrimSpace(handlerName) == "GatewayHandler.GeminiV1BetaModels"
	case service.ContentModerationProtocolOpenAIChat:
		return strings.TrimSpace(handlerName) == "GatewayHandler.ChatCompletions"
	case service.ContentModerationProtocolOpenAIResponses:
		return strings.TrimSpace(handlerName) == "GatewayHandler.Responses"
	default:
		return false
	}
}

func writeGatewayPreForwardEntrypointError(c *gin.Context, protocol string, status int, code, message string) {
	switch strings.TrimSpace(protocol) {
	case service.ContentModerationProtocolGemini:
		googleError(c, status, message)
	default:
		if code == "" {
			code = "api_error"
		}
		c.JSON(status, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    code,
				"message": clientmsg.Localize(message),
			},
		})
	}
}

func (h *GatewayHandler) readGatewayPreForwardEntrypointRequest(c *gin.Context, meta moderationcoverage.Entry) ([]byte, *service.ParsedRequest, string, bool, bool) {
	switch strings.TrimSpace(meta.Protocol) {
	case service.ContentModerationProtocolGemini:
		body, model, stream, ok := h.readGatewayGeminiPreForwardRequest(c)
		if !ok {
			return nil, nil, "", false, false
		}
		return body, &service.ParsedRequest{Model: model, Stream: stream}, model, stream, true
	case service.ContentModerationProtocolOpenAIChat:
		body, model, stream, ok := h.readOpenAICompatibleGatewayPreForwardRequest(c, h.chatCompletionsErrorResponse, nil)
		if !ok {
			return nil, nil, "", false, false
		}
		return body, &service.ParsedRequest{Model: model, Stream: stream, Body: service.NewRequestBodyRef(body)}, model, stream, true
	case service.ContentModerationProtocolOpenAIResponses:
		body, model, stream, ok := h.readOpenAICompatibleGatewayPreForwardRequest(c, h.responsesErrorResponse, nil)
		if !ok {
			return nil, nil, "", false, false
		}
		return body, &service.ParsedRequest{Model: model, Stream: stream, Body: service.NewRequestBodyRef(body)}, model, stream, true
	default:
		body, parsedReq, ok := h.readGatewayMessagesPreForwardRequest(c, nil)
		if !ok {
			return nil, nil, "", false, false
		}
		return body, parsedReq, parsedReq.Model, parsedReq.Stream, true
	}
}

func (h *GatewayHandler) readGatewayGeminiPreForwardRequest(c *gin.Context) ([]byte, string, bool, bool) {
	modelName, action, err := parseGeminiModelAction(strings.TrimPrefix(c.Param("modelAction"), "/"))
	if err != nil {
		googleError(c, http.StatusNotFound, err.Error())
		return nil, "", false, false
	}
	stream := action == "streamGenerateContent"

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			googleError(c, http.StatusRequestEntityTooLarge, buildBodyTooLargeMessage(maxErr.Limit))
			return nil, "", false, false
		}
		markOpsRequestBodyReadError(c, err)
		googleError(c, http.StatusBadRequest, "Failed to read request body")
		return nil, "", false, false
	}
	if len(body) == 0 {
		googleError(c, http.StatusBadRequest, "Request body is empty")
		return nil, "", false, false
	}
	return body, modelName, stream, true
}

func (h *GatewayHandler) readOpenAICompatibleGatewayPreForwardRequest(
	c *gin.Context,
	writeError func(*gin.Context, int, string, string),
	reqLog *zap.Logger,
) ([]byte, string, bool, bool) {
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			writeError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, "", false, false
		}
		markOpsRequestBodyReadError(c, err)
		writeError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, "", false, false
	}
	if len(body) == 0 {
		writeError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, "", false, false
	}
	setOpsRequestContext(c, "", false)
	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		writeError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, "", false, false
	}
	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		writeError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, "", false, false
	}
	stream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		writeError(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return nil, "", false, false
	}
	return body, modelResult.String(), stream, true
}

func gatewayPreForwardErrorFormatForHandler(handlerName string) gatewayPreForwardErrorFormat {
	switch strings.TrimSpace(handlerName) {
	case "GatewayHandler.GeminiV1BetaModels":
		return gatewayPreForwardErrorGemini
	case "GatewayHandler.ChatCompletions":
		return gatewayPreForwardErrorOpenAIChat
	case "GatewayHandler.Responses":
		return gatewayPreForwardErrorOpenAIResponses
	default:
		return gatewayPreForwardErrorAnthropic
	}
}

func (h *GatewayHandler) readGatewayMessagesPreForwardRequest(c *gin.Context, reqLog *zap.Logger) ([]byte, *service.ParsedRequest, bool) {
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, nil, false
		}
		markOpsRequestBodyReadError(c, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, nil, false
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, nil, false
	}
	bodyRef := service.NewRequestBodyRef(body)
	parsedReq, err := service.ParseGatewayRequest(bodyRef, domain.PlatformAnthropic)
	if err != nil {
		logRequestBodyParseFailure(reqLog, body, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, nil, false
	}
	return body, parsedReq, true
}

func setGatewayPreForwardRequest(c *gin.Context, request gatewayPreForwardRequest) {
	if c == nil {
		return
	}
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.Model = strings.TrimSpace(request.Model)
	request.Body = append([]byte(nil), request.Body...)
	c.Set(gatewayPreForwardRequestContextKey, request)
}

func gatewayPreForwardRequestFromContext(c *gin.Context, protocol string) (gatewayPreForwardRequest, bool) {
	if c == nil {
		return gatewayPreForwardRequest{}, false
	}
	value, ok := c.Get(gatewayPreForwardRequestContextKey)
	if !ok {
		return gatewayPreForwardRequest{}, false
	}
	request, ok := value.(gatewayPreForwardRequest)
	if !ok {
		return gatewayPreForwardRequest{}, false
	}
	if strings.TrimSpace(request.Protocol) != strings.TrimSpace(protocol) || len(request.Body) == 0 || strings.TrimSpace(request.Model) == "" || request.Parsed == nil {
		return gatewayPreForwardRequest{}, false
	}
	request.Body = append([]byte(nil), request.Body...)
	return request, true
}

func (h *GatewayHandler) runGatewayForwardStage(c *gin.Context, adapter ForwardStage) ExecutableStageResult {
	adapter = h.gatewayForwardStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.SourceGatewayForwardStage,
		executableForwardStageWithContext(c, adapter),
	)
}

func (h *GatewayHandler) gatewayForwardStageFromRouteDescriptor(c *gin.Context, fallback ForwardStage) ForwardStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedForwardStage(moderationcoverage.PipelineGatewayPreForward, "pipeline route metadata is required before forward")
	}
	descriptors := stageAdapterDescriptorsForRuntimeRoute(routeMeta)
	found := false
	for _, descriptor := range descriptors {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineGatewayPreForward ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageForward {
			continue
		}
		found = true
		if adapter, ok := bindForwardStageAdapterForDescriptor(h.gatewayStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedForwardStage(moderationcoverage.PipelineGatewayPreForward, "pipeline forward stage descriptor is required before forward")
	}
	return blockedForwardStage(moderationcoverage.PipelineGatewayPreForward, "pipeline forward stage adapter is not bound by route descriptor")
}

func (h *GatewayHandler) gatewayStageAdapterRegistry() *StageAdapterRegistry {
	if h == nil {
		return nil
	}
	registry := h.stageAdapterRegistry
	if registry == nil {
		registry = h.forwardStageRegistry
	}
	if registry == nil {
		registry = NewStageAdapterRegistry()
		h.stageAdapterRegistry = registry
	}
	return registry
}

type GatewayMessagesGeminiForwardStage struct {
	GeminiCompatService       *service.GeminiMessagesCompatService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	Model                     string
	Action                    string
	Stream                    bool
	Body                      []byte
	HasBoundSession           bool
	SessionGroupID            int64
	SessionKey                string
	Result                    **service.ForwardResult
}

func (GatewayMessagesGeminiForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayMessagesGeminiForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity {
		result, err = s.AntigravityGatewayService.ForwardGemini(
			ctx,
			c,
			s.Account,
			s.Model,
			s.Action,
			s.Stream,
			s.Body,
			s.HasBoundSession,
			service.WithForwardGeminiSession(s.SessionGroupID, s.SessionKey),
		)
	} else {
		result, err = s.GeminiCompatService.Forward(ctx, c, s.Account, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayMessagesForwardStage struct {
	GatewayService            *service.GatewayService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	ParsedRequest             *service.ParsedRequest
	Body                      []byte
	HasBoundSession           bool
	Result                    **service.ForwardResult
}

func (GatewayMessagesForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayMessagesForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity && s.Account.Type != service.AccountTypeAPIKey {
		result, err = s.AntigravityGatewayService.Forward(ctx, c, s.Account, s.Body, s.HasBoundSession)
	} else {
		result, err = s.GatewayService.Forward(ctx, c, s.Account, s.ParsedRequest)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayChatCompletionsForwardStage struct {
	GatewayService            *service.GatewayService
	GeminiCompatService       *service.GeminiMessagesCompatService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	ParsedRequest             *service.ParsedRequest
	Body                      []byte
	Result                    **service.ForwardResult
}

func (GatewayChatCompletionsForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayChatCompletionsForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account != nil && s.Account.Platform == service.PlatformGemini {
		if s.GeminiCompatService == nil {
			return ExecutableStageResult{Err: errors.New("gemini compatibility service is not configured")}
		}
		result, err = s.GeminiCompatService.ForwardAsChatCompletions(ctx, c, s.Account, s.Body)
	} else if shouldUseAntigravityCompat(s.Account) {
		if s.AntigravityGatewayService == nil {
			return ExecutableStageResult{Err: errors.New("antigravity compatibility service is not configured")}
		}
		setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
		result, err = s.AntigravityGatewayService.ForwardAsChatCompletions(ctx, c, s.Account, s.Body, s.ParsedRequest)
	} else {
		result, err = s.GatewayService.ForwardAsChatCompletions(ctx, c, s.Account, s.Body, s.ParsedRequest)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayResponsesForwardStage struct {
	GatewayService            *service.GatewayService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	ParsedRequest             *service.ParsedRequest
	Body                      []byte
	Result                    **service.ForwardResult
}

func (GatewayResponsesForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayResponsesForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.GatewayService == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if shouldUseAntigravityCompat(s.Account) {
		if s.AntigravityGatewayService == nil {
			return ExecutableStageResult{Err: errors.New("antigravity compatibility service is not configured")}
		}
		setActualUpstreamEndpoint(c, EndpointAntigravityGenerateContent)
		result, err = s.AntigravityGatewayService.ForwardAsResponses(ctx, c, s.Account, s.Body, s.ParsedRequest)
	} else {
		result, err = s.GatewayService.ForwardAsResponses(ctx, c, s.Account, s.Body, s.ParsedRequest)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayGeminiV1BetaForwardStage struct {
	GeminiCompatService       *service.GeminiMessagesCompatService
	AntigravityGatewayService *service.AntigravityGatewayService
	RequestContext            context.Context
	Account                   *service.Account
	Model                     string
	Action                    string
	Stream                    bool
	Body                      []byte
	HasBoundSession           bool
	SessionGroupID            int64
	SessionKey                string
	Result                    **service.ForwardResult
}

func (GatewayGeminiV1BetaForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayGeminiV1BetaForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	var result *service.ForwardResult
	var err error
	if s.Account.Platform == service.PlatformAntigravity && s.Account.Type != service.AccountTypeAPIKey {
		result, err = s.AntigravityGatewayService.ForwardGemini(
			ctx,
			c,
			s.Account,
			s.Model,
			s.Action,
			s.Stream,
			s.Body,
			s.HasBoundSession,
			service.WithForwardGeminiSession(s.SessionGroupID, s.SessionKey),
		)
	} else {
		result, err = s.GeminiCompatService.ForwardNative(ctx, c, s.Account, s.Model, s.Action, s.Stream, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

type GatewayCountTokensForwardStage struct {
	GatewayService *service.GatewayService
	Account        *service.Account
	ParsedRequest  *service.ParsedRequest
}

func (GatewayCountTokensForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s GatewayCountTokensForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.GatewayService == nil {
		return ExecutableStageResult{}
	}
	return ExecutableStageResult{
		Err: s.GatewayService.ForwardCountTokens(c.Request.Context(), c, s.Account, s.ParsedRequest),
	}
}

type GatewayBillingStage struct {
	Handler          *GatewayHandler
	RequestContext   context.Context
	QuotaPlatformCtx context.Context
	QuotaPlatform    string
	APIKey           *service.APIKey
	Group            *service.Group
	Subscription     *service.UserSubscription
}

func (GatewayBillingStage) StageName() string {
	return moderationcoverage.StageBilling
}

func (s GatewayBillingStage) RunBilling(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.billingCacheService == nil || s.APIKey == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	quotaPlatform := s.QuotaPlatform
	if quotaPlatform == "" {
		quotaCtx := s.QuotaPlatformCtx
		if quotaCtx == nil {
			quotaCtx = c.Request.Context()
		}
		quotaPlatform = service.QuotaPlatform(quotaCtx, s.APIKey)
	}
	group := s.Group
	if group == nil {
		group = s.APIKey.Group
	}
	return ExecutableStageResult{
		Err: h.billingCacheService.CheckBillingEligibility(ctx, s.APIKey.User, s.APIKey, group, s.Subscription, quotaPlatform),
	}
}

type GatewayRoutingStage struct {
	Handler           *GatewayHandler
	RequestContext    context.Context
	GroupID           *int64
	SessionHash       string
	Model             string
	FailedAccountIDs  map[int64]struct{}
	MetadataUserID    string
	Sub2APIUserID     int64
	UseModelSelection bool
	Selection         **service.AccountSelectionResult
	Account           **service.Account
}

func (GatewayRoutingStage) StageName() string {
	return moderationcoverage.StageRouting
}

func (s GatewayRoutingStage) RunRouting(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.gatewayService == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	if s.UseModelSelection {
		account, err := h.gatewayService.SelectAccountForModel(ctx, s.GroupID, s.SessionHash, s.Model)
		if s.Account != nil {
			*s.Account = account
		}
		if err == nil && account != nil {
			if value, ok := c.Get(gatewayPreForwardRequestContextKey); ok {
				if request, requestOK := value.(gatewayPreForwardRequest); requestOK {
					apiKey, _ := middleware2.GetAPIKeyFromContext(c)
					subject, _ := middleware2.GetAuthSubjectFromContext(c)
					gate := runSelectedAccountContentModeration(c, requestLogger(c, "handler.gateway.account_moderation"), h.contentModerationService, apiKey, subject, request.Protocol, request.Model, request.Body, account)
					if gate != nil && gate.Decision != nil && gate.Decision.Blocked {
						h.writeGatewayPreForwardModerationError(c, request.ErrorFormat, gate.Decision)
						return ExecutableStageResult{Stop: true}
					}
				}
			}
		}
		return ExecutableStageResult{Err: err}
	}
	selection, err := h.gatewayService.SelectAccountWithLoadAwareness(ctx, s.GroupID, s.SessionHash, s.Model, s.FailedAccountIDs, s.MetadataUserID, s.Sub2APIUserID)
	if s.Selection != nil {
		*s.Selection = selection
	}
	if err == nil && selection != nil && selection.Account != nil {
		if value, ok := c.Get(gatewayPreForwardRequestContextKey); ok {
			if request, requestOK := value.(gatewayPreForwardRequest); requestOK {
				apiKey, _ := middleware2.GetAPIKeyFromContext(c)
				subject, _ := middleware2.GetAuthSubjectFromContext(c)
				gate := runSelectedAccountContentModeration(c, requestLogger(c, "handler.gateway.account_moderation"), h.contentModerationService, apiKey, subject, request.Protocol, request.Model, request.Body, selection.Account)
				if gate != nil && gate.Decision != nil && gate.Decision.Blocked {
					if selection.Acquired && selection.ReleaseFunc != nil {
						selection.ReleaseFunc()
						selection.ReleaseFunc = nil
					}
					h.writeGatewayPreForwardModerationError(c, request.ErrorFormat, gate.Decision)
					return ExecutableStageResult{Stop: true}
				}
			}
		}
	}
	return ExecutableStageResult{Err: err}
}

func (h *GatewayHandler) runGatewayBillingStage(c *gin.Context, adapter BillingStage) ExecutableStageResult {
	adapter = h.gatewayBillingStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.SourceGatewayBillingStage,
		executableBillingStageWithContext(c, adapter),
	)
}

func (h *GatewayHandler) runGatewayRoutingStage(c *gin.Context, adapter RoutingStage) ExecutableStageResult {
	adapter = h.gatewayRoutingStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.SourceGatewayRoutingStage,
		executableRoutingStageWithContext(c, adapter),
	)
}

func (h *GatewayHandler) runGatewayUsageStage(c *gin.Context, adapter UsageStage) ExecutableStageResult {
	adapter = h.gatewayUsageStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.SourceGatewayUsageStage,
		executableUsageStageWithContext(c, adapter),
	)
}

func (h *GatewayHandler) gatewayBillingStageFromRouteDescriptor(c *gin.Context, fallback BillingStage) BillingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedBillingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline route metadata is required before billing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineGatewayPreForward ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageBilling {
			continue
		}
		found = true
		if adapter, ok := bindBillingStageAdapterForDescriptor(h.gatewayStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedBillingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline billing stage descriptor is required before billing")
	}
	return blockedBillingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline billing stage adapter is not bound by route descriptor")
}

func (h *GatewayHandler) gatewayRoutingStageFromRouteDescriptor(c *gin.Context, fallback RoutingStage) RoutingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedRoutingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline route metadata is required before routing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineGatewayPreForward ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageRouting {
			continue
		}
		found = true
		if adapter, ok := bindRoutingStageAdapterForDescriptor(h.gatewayStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedRoutingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline routing stage descriptor is required before routing")
	}
	return blockedRoutingStage(moderationcoverage.PipelineGatewayPreForward, "pipeline routing stage adapter is not bound by route descriptor")
}

func (h *GatewayHandler) gatewayUsageStageFromRouteDescriptor(c *gin.Context, fallback UsageStage) UsageStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedUsageStage(moderationcoverage.PipelineGatewayPreForward, "pipeline route metadata is required before usage")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineGatewayPreForward ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageUsage {
			continue
		}
		found = true
		if adapter, ok := bindUsageStageAdapterForDescriptor(h.gatewayStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedUsageStage(moderationcoverage.PipelineGatewayPreForward, "pipeline usage stage descriptor is required before usage")
	}
	return blockedUsageStage(moderationcoverage.PipelineGatewayPreForward, "pipeline usage stage adapter is not bound by route descriptor")
}

type GatewayUsageStage struct {
	Handler               *GatewayHandler
	RequestContext        context.Context
	Result                *service.ForwardResult
	QuotaPlatform         string
	APIKey                *service.APIKey
	Account               *service.Account
	Subscription          *service.UserSubscription
	InboundEndpoint       string
	UpstreamEndpoint      string
	UserAgent             string
	ClientIP              string
	SessionID             string
	RequestPayloadHash    string
	ForceCacheBilling     bool
	APIKeyService         *service.APIKeyService
	ChannelUsageFields    service.ChannelUsageFields
	LongContext           bool
	LongContextThreshold  int
	LongContextMultiplier float64
	LogComponent          string
	LogMessage            string
	LogUserID             int64
	LogModel              string
}

func (GatewayUsageStage) StageName() string {
	return moderationcoverage.StageUsage
}

func (s GatewayUsageStage) RunUsage(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	record := func(taskCtx context.Context) {
		var err error
		if s.LongContext {
			err = h.gatewayService.RecordUsageWithLongContext(taskCtx, &service.RecordUsageLongContextInput{
				Result:                s.Result,
				QuotaPlatform:         s.QuotaPlatform,
				APIKey:                s.APIKey,
				User:                  s.APIKey.User,
				Account:               s.Account,
				Subscription:          s.Subscription,
				InboundEndpoint:       s.InboundEndpoint,
				UpstreamEndpoint:      s.UpstreamEndpoint,
				UserAgent:             s.UserAgent,
				IPAddress:             s.ClientIP,
				SessionID:             s.SessionID,
				RequestPayloadHash:    s.RequestPayloadHash,
				LongContextThreshold:  s.LongContextThreshold,
				LongContextMultiplier: s.LongContextMultiplier,
				ForceCacheBilling:     s.ForceCacheBilling,
				APIKeyService:         s.APIKeyService,
				ChannelUsageFields:    s.ChannelUsageFields,
			})
		} else {
			err = h.gatewayService.RecordUsage(taskCtx, &service.RecordUsageInput{
				Result:             s.Result,
				QuotaPlatform:      s.QuotaPlatform,
				APIKey:             s.APIKey,
				User:               s.APIKey.User,
				Account:            s.Account,
				Subscription:       s.Subscription,
				InboundEndpoint:    s.InboundEndpoint,
				UpstreamEndpoint:   s.UpstreamEndpoint,
				UserAgent:          s.UserAgent,
				IPAddress:          s.ClientIP,
				SessionID:          s.SessionID,
				RequestPayloadHash: s.RequestPayloadHash,
				ForceCacheBilling:  s.ForceCacheBilling,
				APIKeyService:      s.APIKeyService,
				ChannelUsageFields: s.ChannelUsageFields,
			})
		}
		if err != nil {
			logger.L().With(
				zap.String("component", s.LogComponent),
				zap.Int64("user_id", s.LogUserID),
				zap.Int64("api_key_id", s.APIKey.ID),
				zap.Any("group_id", s.APIKey.GroupID),
				zap.String("model", s.LogModel),
				zap.Int64("account_id", s.Account.ID),
			).Error(s.LogMessage, zap.Error(err))
		}
	}
	h.submitUsageRecordTask(ctx, record)
	return ExecutableStageResult{}
}

func (h *GatewayHandler) gatewayPreForwardPipeline() *GatewayPreForwardPipeline {
	if h == nil {
		return newGatewayPreForwardPipeline(nil)
	}
	if h.preForwardPipeline != nil {
		return h.preForwardPipeline
	}
	guard := h.moderationGuard
	if guard == nil {
		guard = newContentModerationGuard(h.contentModerationService)
	}
	return newGatewayPreForwardPipeline(guard)
}

type gatewayPreForwardStage interface {
	Name() string
	Run(*gatewayPreForwardStageContext) gatewayPreForwardStageResult
}

type gatewayPreForwardStageContext struct {
	handler  *GatewayHandler
	pipeline *GatewayPreForwardPipeline
	c        *gin.Context
	reqLog   *zap.Logger
	input    gatewayPreForwardPipelineInput
}

type gatewayPreForwardStageResult struct {
	Blocked bool
}

type GatewayPreForwardModerationStage struct{}

func (GatewayPreForwardModerationStage) Name() string {
	return moderationcoverage.StageModeration
}

func (GatewayPreForwardModerationStage) Run(ctx *gatewayPreForwardStageContext) gatewayPreForwardStageResult {
	if ctx == nil {
		return gatewayPreForwardStageResult{}
	}
	pipeline := ctx.pipeline
	if pipeline == nil {
		pipeline = newGatewayPreForwardPipeline(nil)
	}
	input := ctx.input
	decision := pipeline.CheckModeration(ctx.c, ctx.reqLog, moderationGuardInput{
		APIKey:   input.APIKey,
		Subject:  input.Subject,
		Protocol: input.Protocol,
		Model:    input.Model,
		Body:     input.Body,
	})
	if decision == nil || !decision.Blocked {
		return gatewayPreForwardStageResult{}
	}
	if ctx.handler != nil {
		ctx.handler.writeGatewayPreForwardModerationError(ctx.c, input.ErrorFormat, decision)
	}
	return gatewayPreForwardStageResult{Blocked: true}
}

func (p *GatewayPreForwardPipeline) Run(h *GatewayHandler, c *gin.Context, reqLog *zap.Logger, input gatewayPreForwardPipelineInput) gatewayPreForwardPipelineResult {
	ctx := &gatewayPreForwardStageContext{
		handler:  h,
		pipeline: p,
		c:        c,
		reqLog:   reqLog,
		input:    input,
	}
	result := newGatewayPipeline(
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.SourceGatewayPreForward,
		gatewayPreForwardExecutableStages(ctx, p.preForwardStages()),
	).Run(c)
	if result.Stop || result.Err != nil {
		return gatewayPreForwardPipelineResult{Blocked: true}
	}
	return gatewayPreForwardPipelineResult{}
}

func gatewayPreForwardExecutableStages(ctx *gatewayPreForwardStageContext, stages []gatewayPreForwardStage) []ExecutableStage {
	executableStages := make([]ExecutableStage, 0, len(stages)+1)
	for _, stage := range stages {
		if stage == nil {
			continue
		}
		stage := stage
		executableStages = append(executableStages, ExecutableStage{
			Name: stage.Name(),
			Run: func() ExecutableStageResult {
				receiptCountBefore := 0
				if ctx != nil {
					receiptCountBefore = len(moderationcoverage.ModerationReceiptsFromContext(ctx.c))
				}
				result := stage.Run(ctx)
				if !result.Blocked && stage.Name() == moderationcoverage.StageModeration && ctx != nil {
					ensureContentModerationReceipt(ctx.c, ctx.input.Protocol, receiptCountBefore)
				}
				return ExecutableStageResult{Stop: result.Blocked}
			},
		})
	}
	executableStages = append(executableStages, ExecutableStage{
		Name: moderationcoverage.StagePreForward,
		Run: func() ExecutableStageResult {
			if ctx != nil {
				moderationcoverage.MarkPipelineAdmittedAfterModeration(ctx.c, moderationcoverage.PipelineGatewayPreForward, moderationcoverage.StagePreForward, moderationcoverage.SourceGatewayPreForward)
			}
			return ExecutableStageResult{}
		},
	})
	return executableStages
}

func (p *GatewayPreForwardPipeline) preForwardStages() []gatewayPreForwardStage {
	if p != nil && len(p.stages) > 0 {
		return p.stages
	}
	return []gatewayPreForwardStage{GatewayPreForwardModerationStage{}}
}

func (h *GatewayHandler) writeGatewayPreForwardModerationError(c *gin.Context, format gatewayPreForwardErrorFormat, decision *service.ContentModerationDecision) {
	markOpsContentModerationDiagnostic(c, decision)
	switch format {
	case gatewayPreForwardErrorGemini:
		googleError(c, contentModerationStatus(decision), decision.Message)
	case gatewayPreForwardErrorOpenAIChat:
		h.chatCompletionsErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	case gatewayPreForwardErrorOpenAIResponses:
		h.responsesErrorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	default:
		h.errorResponse(c, contentModerationStatus(decision), contentModerationErrorCode(decision), decision.Message)
	}
}
