package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

type moderationGuard interface {
	Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision
}

type moderationGuardInput struct {
	APIKey   *service.APIKey
	Subject  middleware2.AuthSubject
	Protocol string
	Model    string
	Body     []byte
}

type openAIHTTPPreForwardPipelineInput struct {
	APIKey                          *service.APIKey
	Subject                         middleware2.AuthSubject
	Protocol                        string
	Model                           string
	Body                            []byte
	CyberBody                       []byte
	CyberFormat                     cyberSessionBlockFormat
	ModerationErrorFormat           openAIHTTPModerationErrorFormat
	SkipCyberStage                  bool
	EnableImageStage                bool
	ImagePermissionBeforeModeration bool
	ImageEndpoint                   string
	StreamStarted                   bool
}

type openAIHTTPPreForwardPipelineResult struct {
	Blocked          bool
	ImageReleaseFunc func()
}

type OpenAIHTTPGatewayPipelineEntryResult struct {
	Stop bool
}

type openAIHTTPPreForwardRequest struct {
	Protocol           string
	Model              string
	Body               []byte
	CyberBody          []byte
	Stream             bool
	ImageReleaseFunc   func()
	ImagesRequest      *service.OpenAIImagesRequest
	ModerationBody     []byte
	UsesModerationBody bool
}

const openAIHTTPPreForwardRequestContextKey = "openai_http_pre_forward_request"

type openAIWebSocketPipelineBlockReason int

const (
	openAIWebSocketPipelineBlockReasonNone openAIWebSocketPipelineBlockReason = iota
	openAIWebSocketPipelineBlockReasonModeration
	openAIWebSocketPipelineBlockReasonImagePermission
	openAIWebSocketPipelineBlockReasonCyberSession
)

type openAIWebSocketPipelineInput struct {
	APIKey        *service.APIKey
	Subject       middleware2.AuthSubject
	Protocol      string
	Model         string
	Body          []byte
	CyberBody     []byte
	ImageEndpoint string
}

type openAIWebSocketPipelineResult struct {
	Blocked            bool
	BlockReason        openAIWebSocketPipelineBlockReason
	ModerationDecision *service.ContentModerationDecision
	Message            string
	CyberBlockKey      string
}

type contentModerationGuard struct {
	service *service.ContentModerationService
}

func newContentModerationGuard(svc *service.ContentModerationService) moderationGuard {
	return &contentModerationGuard{service: svc}
}

func (h *OpenAIGatewayHandler) checkWithModerationGuard(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	if cached, ok := contentModerationDecisionFromCache(c, input.Protocol, input.Model, input.Body); ok {
		return cached
	}
	if h == nil {
		decision := newOpenAIGatewayPipeline(nil).CheckModeration(c, reqLog, input)
		cacheContentModerationDecision(c, input.Protocol, input.Model, input.Body, decision)
		return decision
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		pipeline = newOpenAIGatewayPipeline(guard)
	}
	decision := pipeline.CheckModeration(c, reqLog, input)
	if !selectedAccountModerationRequired(c, input.Protocol, input.Model, input.Body) {
		cacheContentModerationDecision(c, input.Protocol, input.Model, input.Body, decision)
	}
	return decision
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPPreForwardPipeline(c *gin.Context, reqLog *zap.Logger, input openAIHTTPPreForwardPipelineInput) openAIHTTPPreForwardPipelineResult {
	pipeline := h.openAIHTTPPreForwardPipeline()
	return pipeline.RunHTTPPreForward(h, c, reqLog, input)
}

func (h *OpenAIGatewayHandler) EnterOpenAIHTTPGatewayPipeline(c *gin.Context, meta moderationcoverage.Entry) OpenAIHTTPGatewayPipelineEntryResult {
	meta = moderationcoverage.NormalizeEntry(meta)
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat, service.ContentModerationProtocolOpenAIMessages, service.ContentModerationProtocolOpenAIResponses, service.ContentModerationProtocolOpenAIImages, service.ContentModerationProtocolOpenAIEmbeddings:
	default:
		return OpenAIHTTPGatewayPipelineEntryResult{}
	}
	if h == nil {
		return OpenAIHTTPGatewayPipelineEntryResult{}
	}
	if h.contentModerationService != nil {
		protectedCtx, protectErr := h.contentModerationService.AcquireRequestResources(c.Request.Context(), c.Request.ContentLength, c.GetHeader("Content-Encoding"))
		if protectErr != nil {
			if errors.Is(protectErr, service.ErrRequestMemoryBudgetExhausted) {
				var budgetErr *service.RequestMemoryBudgetExhaustedError
				if errors.As(protectErr, &budgetErr) {
					logger.FromContext(c.Request.Context()).Warn("request_memory_admission_rejected",
						zap.Int64("request_content_length", budgetErr.RequestContentLength),
						zap.Int64("estimated_charge_bytes", budgetErr.EstimatedChargeBytes),
						zap.Int64("active_bytes", budgetErr.ActiveBytes),
						zap.Int64("admission_limit_bytes", budgetErr.AdmissionLimitBytes),
						zap.Int64("available_bytes", budgetErr.AvailableBytes),
						zap.Int("active_reservations", budgetErr.ActiveReservations),
						zap.Int("waiting_requests", budgetErr.WaitingRequests),
						zap.Int("admission_wait_ms", budgetErr.AdmissionWaitMS),
						zap.Bool("ambiguous_length", budgetErr.AmbiguousLength),
						zap.Bool("small_request", budgetErr.SmallRequest),
					)
					if raw, err := json.Marshal(budgetErr); err == nil {
						service.SetOpsDiagnostic(c, "请求内存准入预算耗尽", string(raw))
					}
				}
				c.Header("Retry-After", "1")
				h.errorResponse(c, http.StatusTooManyRequests, "request_memory_budget_exhausted", "Request memory budget exhausted")
			} else if varMax := new(service.RequestBodyTooLargeError); errors.As(protectErr, &varMax) {
				markOpsRequestBodyTooLarge(c, varMax.Limit, nil, false)
				h.errorResponse(c, http.StatusRequestEntityTooLarge, "request_body_too_large", buildBodyTooLargeMessage(varMax.Limit))
			} else {
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", protectErr.Error())
			}
			return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
		}
		c.Request = c.Request.WithContext(protectedCtx)
	}

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
	}

	logComponent := "handler.openai_gateway.embeddings"
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat:
		logComponent = "handler.openai_gateway.chat_completions"
	case service.ContentModerationProtocolOpenAIMessages:
		logComponent = "handler.openai_gateway.messages"
	case service.ContentModerationProtocolOpenAIResponses:
		logComponent = "handler.openai_gateway.responses"
	case service.ContentModerationProtocolOpenAIImages:
		logComponent = "handler.openai_gateway.images"
	}
	reqLog := requestLogger(
		c,
		logComponent,
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	body, model, stream, cyberBody, imagesRequest, ok := h.readOpenAIHTTPPreForwardRequest(c, reqLog, meta.Protocol)
	if !ok {
		return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
	}
	reqLog = reqLog.With(zap.String("model", model), zap.Bool("stream", stream))
	setOpsRequestContext(c, model, stream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(stream, false)))

	pipelineInput := openAIHTTPPreForwardPipelineInput{
		APIKey:   apiKey,
		Subject:  subject,
		Protocol: meta.Protocol,
		Model:    model,
		Body:     body,
	}
	usesModerationBody := false
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat:
		pipelineInput.CyberFormat = cyberBlockFormatChat
	case service.ContentModerationProtocolOpenAIMessages:
		pipelineInput.CyberBody = body
		pipelineInput.CyberFormat = cyberBlockFormatAnthropic
		pipelineInput.ModerationErrorFormat = openAIHTTPModerationErrorAnthropic
	case service.ContentModerationProtocolOpenAIResponses:
		pipelineInput.CyberBody = cyberBody
		pipelineInput.CyberFormat = cyberBlockFormatResponses
		pipelineInput.EnableImageStage = true
		pipelineInput.ImageEndpoint = "/v1/responses"
	case service.ContentModerationProtocolOpenAIImages:
		pipelineInput.Body = nil
		if imagesRequest != nil {
			pipelineInput.Body = imagesRequest.ModerationBody()
			pipelineInput.ImageEndpoint = imagesRequest.Endpoint
		}
		pipelineInput.SkipCyberStage = true
		pipelineInput.EnableImageStage = true
		pipelineInput.ImagePermissionBeforeModeration = true
		usesModerationBody = true
	case service.ContentModerationProtocolOpenAIEmbeddings:
		pipelineInput.SkipCyberStage = true
	}
	if meta.Handler == "OpenAIGatewayHandler.AlphaSearch" {
		pipelineInput.Body = service.OpenAIAlphaSearchModerationBody(body)
		usesModerationBody = true
	}

	pipelineResult := h.runOpenAIHTTPPreForwardPipeline(c, reqLog, pipelineInput)
	if pipelineResult.Blocked {
		return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
	}

	var moderationBody []byte
	if usesModerationBody {
		moderationBody = pipelineInput.Body
	}
	setOpenAIHTTPPreForwardRequest(c, openAIHTTPPreForwardRequest{
		Protocol:           meta.Protocol,
		Model:              model,
		Body:               body,
		CyberBody:          cyberBody,
		Stream:             stream,
		ImageReleaseFunc:   pipelineResult.ImageReleaseFunc,
		ImagesRequest:      imagesRequest,
		ModerationBody:     moderationBody,
		UsesModerationBody: usesModerationBody,
	})
	restoreRequestBody(c, body)
	return OpenAIHTTPGatewayPipelineEntryResult{}
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketInitialFramePipeline(c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	pipeline := h.openAIWebSocketPipeline()
	return pipeline.RunWebSocketInitialFrame(h, c, reqLog, input)
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketFollowupFramePipeline(c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	pipeline := h.openAIWebSocketPipeline()
	return pipeline.RunWebSocketFollowupFrame(h, c, reqLog, input)
}

type OpenAIWebSocketFramePipelineAdapter interface {
	RunFramePipeline(*OpenAIGatewayHandler, *gin.Context, *zap.Logger, openAIWebSocketPipelineInput) openAIWebSocketPipelineResult
}

type OpenAIWebSocketInitialFramePipelineAdapter struct{}

func (OpenAIWebSocketInitialFramePipelineAdapter) RunFramePipeline(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	if h == nil {
		return newOpenAIGatewayPipeline(nil).RunWebSocketInitialFrame(nil, c, reqLog, input)
	}
	return h.runOpenAIWebSocketInitialFramePipeline(c, reqLog, input)
}

type OpenAIWebSocketFollowupFramePipelineAdapter struct{}

func (OpenAIWebSocketFollowupFramePipelineAdapter) RunFramePipeline(h *OpenAIGatewayHandler, c *gin.Context, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	if h == nil {
		return newOpenAIGatewayPipeline(nil).RunWebSocketFollowupFrame(nil, c, reqLog, input)
	}
	return h.runOpenAIWebSocketFollowupFramePipeline(c, reqLog, input)
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketFramePipeline(c *gin.Context, adapter OpenAIWebSocketFramePipelineAdapter, reqLog *zap.Logger, input openAIWebSocketPipelineInput) openAIWebSocketPipelineResult {
	if adapter == nil {
		return openAIWebSocketPipelineResult{}
	}
	return adapter.RunFramePipeline(h, c, reqLog, input)
}

func (h *OpenAIGatewayHandler) openAIHTTPPreForwardPipeline() *OpenAIGatewayPipeline {
	if h == nil {
		return newOpenAIGatewayPipeline(nil)
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		return newOpenAIGatewayPipeline(guard, h.gatewayService)
	}
	if pipeline.cyberSessionChecker == nil && h.gatewayService != nil {
		return pipeline.withCyberSessionChecker(h.gatewayService)
	}
	return pipeline
}

func (h *OpenAIGatewayHandler) readOpenAIHTTPPreForwardRequest(c *gin.Context, reqLog *zap.Logger, protocol string) ([]byte, string, bool, []byte, *service.OpenAIImagesRequest, bool) {
	bodyReadStart := time.Now()
	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	bodyReadMs := time.Since(bodyReadStart).Milliseconds()
	if err != nil {
		if requestErr := c.Request.Context().Err(); requestErr != nil {
			reqLog.Debug("openai.request_body_read_canceled",
				zap.Int64("request_body_read_ms", bodyReadMs),
				zap.Error(requestErr),
			)
			markOpsRequestBodyReadError(c, requestErr)
			c.Status(statusClientClosedRequest)
			return nil, "", false, nil, nil, false
		}
		reqLog.Warn("openai.request_body_read_failed",
			zap.Int64("request_body_read_ms", bodyReadMs),
			zap.Error(err),
		)
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, "", false, nil, nil, false
		}
		markOpsRequestBodyReadError(c, err)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, "", false, nil, nil, false
	}
	reqLog.Debug("openai.request_body_read_done",
		zap.Int64("request_body_read_ms", bodyReadMs),
		zap.Int64("body_bytes", int64(len(body))),
	)
	restoreRequestBody(c, body)
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, "", false, nil, nil, false
	}

	cyberBody := body
	if protocol == service.ContentModerationProtocolOpenAIResponses {
		setOpsRequestContext(c, "", false)
		if service.IsOpenAIResponsesCompactPath(c) {
			if compactSeed := strings.TrimSpace(gjson.GetBytes(body, "prompt_cache_key").String()); compactSeed != "" {
				c.Set(service.OpenAICompactSessionSeedKeyForTest(), compactSeed)
			}
			normalizedCompactBody, normalizedCompact, compactErr := service.NormalizeOpenAICompactRequestBodyForTest(body)
			if compactErr != nil {
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to normalize compact request body")
				return nil, "", false, nil, nil, false
			}
			if normalizedCompact {
				body = normalizedCompactBody
			}
		}
		restoreRequestBody(c, body)
	}
	if protocol == service.ContentModerationProtocolOpenAIImages {
		setOpsRequestContext(c, "", false)
		parsed, err := h.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return nil, "", false, nil, nil, false
		}
		return body, parsed.Model, parsed.Stream, nil, parsed, true
	}

	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, "", false, nil, nil, false
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, "", false, nil, nil, false
	}
	stream := false
	if protocol == service.ContentModerationProtocolOpenAIChat || protocol == service.ContentModerationProtocolOpenAIMessages || protocol == service.ContentModerationProtocolOpenAIResponses {
		var ok bool
		stream, ok = parseOpenAICompatibleStream(body)
		if !ok {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
			return nil, "", false, nil, nil, false
		}
	}
	if protocol == service.ContentModerationProtocolOpenAIResponses {
		previousResponseID := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
		if previousResponseID != "" {
			previousResponseIDKind := service.ClassifyOpenAIPreviousResponseIDKind(previousResponseID)
			if reqLog != nil {
				reqLog = reqLog.With(
					zap.Bool("has_previous_response_id", true),
					zap.String("previous_response_id_kind", previousResponseIDKind),
					zap.Int("previous_response_id_len", len(previousResponseID)),
				)
			}
			if previousResponseIDKind == service.OpenAIPreviousResponseIDKindMessageID {
				if reqLog != nil {
					reqLog.Warn("openai.request_validation_failed",
						zap.String("reason", "previous_response_id_looks_like_message_id"),
					)
				}
				h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id must be a response.id (resp_*), not a message id")
				return nil, "", false, nil, nil, false
			}
			if reqLog != nil {
				reqLog.Warn("openai.request_validation_failed",
					zap.String("reason", "previous_response_id_requires_wsv2"),
				)
			}
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "previous_response_id is only supported on Responses WebSocket v2")
			return nil, "", false, nil, nil, false
		}
	}
	return body, modelResult.String(), stream, cyberBody, nil, true
}

func setOpenAIHTTPPreForwardRequest(c *gin.Context, request openAIHTTPPreForwardRequest) {
	if c == nil {
		return
	}
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.Model = strings.TrimSpace(request.Model)
	request.Body = append([]byte(nil), request.Body...)
	request.CyberBody = append([]byte(nil), request.CyberBody...)
	request.ModerationBody = append([]byte(nil), request.ModerationBody...)
	c.Set(openAIHTTPPreForwardRequestContextKey, request)
}

func openAIHTTPPreForwardRequestFromContext(c *gin.Context, protocol string) (openAIHTTPPreForwardRequest, bool) {
	if c == nil {
		return openAIHTTPPreForwardRequest{}, false
	}
	value, ok := c.Get(openAIHTTPPreForwardRequestContextKey)
	if !ok {
		return openAIHTTPPreForwardRequest{}, false
	}
	request, ok := value.(openAIHTTPPreForwardRequest)
	if !ok {
		return openAIHTTPPreForwardRequest{}, false
	}
	if strings.TrimSpace(request.Protocol) != strings.TrimSpace(protocol) || len(request.Body) == 0 || strings.TrimSpace(request.Model) == "" {
		return openAIHTTPPreForwardRequest{}, false
	}
	// The setter owns one isolated copy. Consumers treat these request-scoped
	// byte slices as immutable so large multipart moderation bodies stay zero-copy.
	return request, true
}

func (r openAIHTTPPreForwardRequest) contentModerationBody() []byte {
	if r.UsesModerationBody {
		return r.ModerationBody
	}
	return r.Body
}

func restoreRequestBody(c *gin.Context, body []byte) {
	if c == nil || c.Request == nil {
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
}

func requireOpenAIWebSocketGatewayPipelineEntrypoint(c *gin.Context) bool {
	if _, ok := moderationcoverage.PipelineEntrypointEnteredFromContext(c, moderationcoverage.PipelineOpenAIWebSocket); ok {
		return true
	}
	if c != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "OpenAI WebSocket pipeline entrypoint missing",
		})
	}
	return false
}

func (h *OpenAIGatewayHandler) openAIWebSocketPipeline() *OpenAIGatewayPipeline {
	return h.openAIHTTPPreForwardPipeline()
}

func (h *OpenAIGatewayHandler) checkCyberSessionWithPipeline(c *gin.Context, reqLog *zap.Logger, input openAIGatewayCyberSessionInput) bool {
	if h == nil {
		result := newOpenAIGatewayPipeline(nil).CheckCyberSession(c, reqLog, input)
		return result != nil && result.Blocked
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		pipeline = newOpenAIGatewayPipeline(guard, h.gatewayService)
	} else if pipeline.cyberSessionChecker == nil && h.gatewayService != nil {
		pipeline = pipeline.withCyberSessionChecker(h.gatewayService)
	}
	result := pipeline.CheckCyberSession(c, reqLog, input)
	if result == nil || !result.Blocked {
		return false
	}
	h.enqueueCyberSessionBlockedOpsEntry(c, input.APIKey, input.Model, result.BlockKey)
	return true
}

func (g *contentModerationGuard) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	if g == nil || g.service == nil {
		if reqLog != nil {
			reqLog.Warn("content_moderation.service_unavailable")
		}
		decision := contentModerationCheckErrorDecision()
		markContentModerationReceipt(c, input.Protocol, "", decision, false)
		return decision
	}
	if selectedAccountModerationRequired(c, input.Protocol, input.Model, input.Body) {
		return &service.ContentModerationDecision{Allowed: true, Action: service.ContentModerationActionAllow}
	}
	if c != nil && c.Request != nil && g.service.RequiresSelectedAccount(c.Request.Context()) {
		baselineInput := buildContentModerationInput(c, input.APIKey, input.Subject, input.Protocol, input.Model, input.Body)
		baseline := g.service.CheckSelectedAccountBaseline(c.Request.Context(), baselineInput)
		markPromptInjectionBaselineCompleted(c, input.Protocol, input.Model, input.Body, baseline)
		baselineDecision := baseline.Decision
		if baselineDecision != nil && baselineDecision.Blocked {
			cacheContentModerationDecision(c, input.Protocol, input.Model, input.Body, baselineDecision)
			markContentModerationReceipt(c, input.Protocol, baselineDecision.PolicyRevision, baselineDecision, false)
			recordContentModerationReceiptMetric(g.service, c, "selected_account_baseline")
			return baselineDecision
		}
		markSelectedAccountModerationRequired(c, input.Protocol, input.Model, input.Body)
		markContentModerationReceipt(c, input.Protocol, "", nil, true)
		recordContentModerationReceiptMetric(g.service, c, "selected_account")
		return &service.ContentModerationDecision{Allowed: true, Action: service.ContentModerationActionAllow}
	}
	return runContentModeration(c, reqLog, g.service, input.APIKey, input.Subject, input.Protocol, input.Model, input.Body)
}
