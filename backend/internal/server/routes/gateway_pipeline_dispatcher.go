package routes

import (
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
	switch meta.Protocol {
	case service.ContentModerationProtocolOpenAIChat,
		service.ContentModerationProtocolOpenAIMessages,
		service.ContentModerationProtocolOpenAIResponses,
		service.ContentModerationProtocolOpenAIImages,
		service.ContentModerationProtocolOpenAIEmbeddings:
	default:
		return GatewayPipelineEntryResult{}
	}
	if d.isOpenAIPlatform != nil && !d.isOpenAIPlatform(c) {
		return GatewayPipelineEntryResult{}
	}
	if d.openAIHTTP == nil {
		return GatewayPipelineEntryResult{}
	}
	return d.openAIHTTP.EnterGatewayPipeline(c, meta)
}

func (d *GatewayPipelineEntrypointDispatcher) enterOpenAIWebSocket(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if meta.Protocol != service.ContentModerationProtocolOpenAIResponses {
		return GatewayPipelineEntryResult{}
	}
	moderationcoverage.MarkPipelineEntrypointEntered(c, moderationcoverage.PipelineOpenAIWebSocket, "GatewayPipelineRegistrar.OpenAIWebSocket")
	return GatewayPipelineEntryResult{}
}

func (d *GatewayPipelineEntrypointDispatcher) enterGatewayPreForward(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	switch meta.Protocol {
	case service.ContentModerationProtocolAnthropicMessages,
		service.ContentModerationProtocolGemini,
		service.ContentModerationProtocolOpenAIChat,
		service.ContentModerationProtocolOpenAIResponses:
	default:
		return GatewayPipelineEntryResult{}
	}
	switch meta.Handler {
	case "GatewayHandler.Messages",
		"GatewayHandler.CountTokens",
		"GatewayHandler.GeminiV1BetaModels",
		"GatewayHandler.ChatCompletions",
		"GatewayHandler.Responses":
	default:
		return GatewayPipelineEntryResult{}
	}
	if !middleware.HasForcePlatform(c) {
		groupPlatform := ""
		if d.groupPlatform != nil {
			groupPlatform = d.groupPlatform(c)
		}
		switch groupPlatform {
		case service.PlatformOpenAI, service.PlatformGrok:
			return GatewayPipelineEntryResult{}
		}
	}
	if d.gatewayPreForward == nil {
		return GatewayPipelineEntryResult{}
	}
	return d.gatewayPreForward.EnterGatewayPipeline(c, meta)
}
