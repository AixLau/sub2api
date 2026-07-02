package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
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
	Protocol string
	Model    string
	Body     []byte
	Stream   bool
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
	if h == nil {
		return newOpenAIGatewayPipeline(nil).CheckModeration(c, reqLog, input)
	}
	pipeline := h.pipeline
	if pipeline == nil {
		guard := h.moderationGuard
		if guard == nil {
			guard = newContentModerationGuard(h.contentModerationService)
		}
		pipeline = newOpenAIGatewayPipeline(guard)
	}
	return pipeline.CheckModeration(c, reqLog, input)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPPreForwardPipeline(c *gin.Context, reqLog *zap.Logger, input openAIHTTPPreForwardPipelineInput) openAIHTTPPreForwardPipelineResult {
	pipeline := h.openAIHTTPPreForwardPipeline()
	return pipeline.RunHTTPPreForward(h, c, reqLog, input)
}

func (h *OpenAIGatewayHandler) EnterOpenAIHTTPGatewayPipeline(c *gin.Context, meta moderationcoverage.Entry) OpenAIHTTPGatewayPipelineEntryResult {
	meta = moderationcoverage.NormalizeEntry(meta)
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat, service.ContentModerationProtocolOpenAIEmbeddings:
	default:
		return OpenAIHTTPGatewayPipelineEntryResult{}
	}
	if h == nil {
		return OpenAIHTTPGatewayPipelineEntryResult{}
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
	if meta.Protocol == service.ContentModerationProtocolOpenAIChat {
		logComponent = "handler.openai_gateway.chat_completions"
	}
	reqLog := requestLogger(
		c,
		logComponent,
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)
	body, model, stream, ok := h.readOpenAIHTTPPreForwardRequest(c, meta.Protocol)
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
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat:
		pipelineInput.CyberFormat = cyberBlockFormatChat
	case service.ContentModerationProtocolOpenAIEmbeddings:
		pipelineInput.SkipCyberStage = true
	}

	if pipelineResult := h.runOpenAIHTTPPreForwardPipeline(c, reqLog, pipelineInput); pipelineResult.Blocked {
		return OpenAIHTTPGatewayPipelineEntryResult{Stop: true}
	}

	setOpenAIHTTPPreForwardRequest(c, openAIHTTPPreForwardRequest{
		Protocol: meta.Protocol,
		Model:    model,
		Body:     body,
		Stream:   stream,
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

func (h *OpenAIGatewayHandler) readOpenAIHTTPPreForwardRequest(c *gin.Context, protocol string) ([]byte, string, bool, bool) {
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return nil, "", false, false
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return nil, "", false, false
	}
	restoreRequestBody(c, body)
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return nil, "", false, false
	}
	if !gjson.ValidBytes(body) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, "", false, false
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || strings.TrimSpace(modelResult.String()) == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, "", false, false
	}
	stream := false
	if protocol == service.ContentModerationProtocolOpenAIChat {
		var ok bool
		stream, ok = parseOpenAICompatibleStream(body)
		if !ok {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
			return nil, "", false, false
		}
	}
	return body, modelResult.String(), stream, true
}

func setOpenAIHTTPPreForwardRequest(c *gin.Context, request openAIHTTPPreForwardRequest) {
	if c == nil {
		return
	}
	request.Protocol = strings.TrimSpace(request.Protocol)
	request.Model = strings.TrimSpace(request.Model)
	request.Body = append([]byte(nil), request.Body...)
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
	request.Body = append([]byte(nil), request.Body...)
	return request, true
}

func restoreRequestBody(c *gin.Context, body []byte) {
	if c == nil || c.Request == nil {
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
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
		return contentModerationCheckErrorDecision()
	}
	return runContentModeration(c, reqLog, g.service, input.APIKey, input.Subject, input.Protocol, input.Model, input.Body)
}
