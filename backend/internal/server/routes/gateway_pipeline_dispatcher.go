package routes

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type GatewayPipelineEntrypointDispatcherConfig struct {
	GroupPlatform     func(*gin.Context) string
	IsOpenAIPlatform  func(*gin.Context) bool
	OpenAIHTTP        GatewayPipelineEntrypoint
	GatewayPreForward GatewayPipelineEntrypoint
}

type GatewayPipelineEntrypointDispatcher struct {
	groupPlatform     func(*gin.Context) string
	isOpenAIPlatform  func(*gin.Context) bool
	openAIHTTP        GatewayPipelineEntrypoint
	gatewayPreForward GatewayPipelineEntrypoint
}

func NewGatewayPipelineEntrypointDispatcher(config GatewayPipelineEntrypointDispatcherConfig) *GatewayPipelineEntrypointDispatcher {
	return &GatewayPipelineEntrypointDispatcher{
		groupPlatform:     config.GroupPlatform,
		isOpenAIPlatform:  config.IsOpenAIPlatform,
		openAIHTTP:        config.OpenAIHTTP,
		gatewayPreForward: config.GatewayPreForward,
	}
}

func NewGatewayPipelineEntrypointDispatcherForHandlers(
	h *handler.Handlers,
	groupPlatform func(*gin.Context) string,
	isOpenAIPlatform func(*gin.Context) bool,
) *GatewayPipelineEntrypointDispatcher {
	return NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		GroupPlatform:    groupPlatform,
		IsOpenAIPlatform: isOpenAIPlatform,
		OpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			if h == nil || h.OpenAIGateway == nil {
				return GatewayPipelineEntryResult{}
			}
			result := h.OpenAIGateway.EnterOpenAIHTTPGatewayPipeline(c, meta)
			return GatewayPipelineEntryResult{Stop: result.Stop}
		}),
		GatewayPreForward: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			if h == nil || h.Gateway == nil {
				return GatewayPipelineEntryResult{}
			}
			result := h.Gateway.EnterGatewayPreForwardPipeline(c, meta)
			return GatewayPipelineEntryResult{Stop: result.Blocked}
		}),
	})
}

func (d *GatewayPipelineEntrypointDispatcher) EnterGatewayPipeline(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if d == nil {
		return GatewayPipelineEntryResult{}
	}
	switch meta.Pipeline {
	case moderationcoverage.PipelineOpenAIHTTP:
		return d.enterOpenAIHTTP(c, meta)
	case moderationcoverage.PipelineOpenAIWebSocket:
		return d.enterOpenAIWebSocket(c, meta)
	case moderationcoverage.PipelineGatewayPreForward:
		return d.enterGatewayPreForward(c, meta)
	default:
		return GatewayPipelineEntryResult{}
	}
}

func (d *GatewayPipelineEntrypointDispatcher) enterOpenAIHTTP(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if !openAIHTTPAdmissionSupported(d.groupPlatformForRequest(c), meta) {
		return GatewayPipelineEntryResult{}
	}
	if d.openAIHTTP == nil {
		return GatewayPipelineEntryResult{}
	}
	return d.openAIHTTP.EnterGatewayPipeline(c, meta)
}

func (d *GatewayPipelineEntrypointDispatcher) enterOpenAIWebSocket(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if meta.Protocol != service.ContentModerationProtocolOpenAIResponses ||
		meta.Handler != "OpenAIGatewayHandler.ResponsesWebSocket" {
		return GatewayPipelineEntryResult{}
	}
	const source = "GatewayPipelineRegistrar.OpenAIWebSocket"
	moderationcoverage.MarkPipelineEntrypointEntered(c, moderationcoverage.PipelineOpenAIWebSocket, source)
	return GatewayPipelineEntryResult{}
}

func (d *GatewayPipelineEntrypointDispatcher) enterGatewayPreForward(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if !gatewayPreForwardAdmissionSupported(meta) {
		return GatewayPipelineEntryResult{}
	}
	if !middleware.HasForcePlatform(c) {
		switch d.groupPlatformForRequest(c) {
		case service.PlatformOpenAI:
			if !isOpenAICountTokensGenericAdmission(meta) {
				return GatewayPipelineEntryResult{}
			}
		case service.PlatformGrok:
			return GatewayPipelineEntryResult{}
		}
	}
	if d.gatewayPreForward == nil {
		return GatewayPipelineEntryResult{}
	}
	return d.gatewayPreForward.EnterGatewayPipeline(c, meta)
}

func (d *GatewayPipelineEntrypointDispatcher) groupPlatformForRequest(c *gin.Context) string {
	if d != nil && d.groupPlatform != nil {
		if platform := strings.TrimSpace(d.groupPlatform(c)); platform != "" {
			return platform
		}
	}
	if d != nil && d.isOpenAIPlatform != nil && d.isOpenAIPlatform(c) {
		return service.PlatformOpenAI
	}
	return ""
}

func openAIHTTPAdmissionSupported(platform string, meta ModeratedRouteMeta) bool {
	platform = strings.TrimSpace(platform)
	handlerName := strings.TrimSpace(meta.Handler)
	protocol := strings.TrimSpace(meta.Protocol)

	switch platform {
	case service.PlatformOpenAI:
		switch handlerName {
		case "OpenAIGatewayHandler.ChatCompletions":
			return protocol == service.ContentModerationProtocolOpenAIChat
		case "OpenAIGatewayHandler.Messages":
			return protocol == service.ContentModerationProtocolOpenAIMessages
		case "OpenAIGatewayHandler.Responses":
			return protocol == service.ContentModerationProtocolOpenAIResponses
		case "OpenAIGatewayHandler.AlphaSearch":
			return protocol == service.ContentModerationProtocolOpenAIResponses
		case "OpenAIGatewayHandler.Images":
			return protocol == service.ContentModerationProtocolOpenAIImages
		case "OpenAIGatewayHandler.Embeddings":
			return protocol == service.ContentModerationProtocolOpenAIEmbeddings
		default:
			return false
		}
	case service.PlatformGrok:
		switch handlerName {
		case "OpenAIGatewayHandler.ChatCompletions":
			return protocol == service.ContentModerationProtocolOpenAIChat
		case "OpenAIGatewayHandler.Messages":
			return protocol == service.ContentModerationProtocolOpenAIMessages
		case "OpenAIGatewayHandler.Responses":
			return protocol == service.ContentModerationProtocolOpenAIResponses
		default:
			return false
		}
	default:
		return false
	}
}

func gatewayPreForwardAdmissionSupported(meta ModeratedRouteMeta) bool {
	switch strings.TrimSpace(meta.Handler) {
	case "GatewayHandler.Messages":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolAnthropicMessages
	case "GatewayHandler.CountTokens":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolAnthropicMessages
	case "GatewayHandler.GeminiV1BetaModels":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolGemini
	case "GatewayHandler.ChatCompletions":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolOpenAIChat
	case "GatewayHandler.Responses":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolOpenAIResponses
	default:
		return false
	}
}

func isOpenAICountTokensGenericAdmission(meta ModeratedRouteMeta) bool {
	return strings.TrimSpace(meta.Handler) == "GatewayHandler.CountTokens" &&
		strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolAnthropicMessages
}
