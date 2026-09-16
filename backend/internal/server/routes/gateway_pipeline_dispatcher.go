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
		switch platform := d.groupPlatformForRequest(c); {
		case platform == service.PlatformOpenAI:
			if !isOpenAICountTokensGenericAdmission(meta) {
				return GatewayPipelineEntryResult{}
			}
		case platform == service.PlatformGrok:
			return GatewayPipelineEntryResult{}
		case service.IsCNProvider(platform) && openAIHTTPBranchOwnsRequest(meta):
			// 国产 OpenAI 兼容供应商把 /v1/messages、/v1/responses、
			// /v1/chat/completions 三条自动路由交给 OpenAI 网关 handler，审核归
			// OpenAI HTTP 分支管线。通配前向管线若同时运行，同一请求会以另一个
			// protocol 被审核第二次：进程内决策缓存按 protocol 做 key，第二次必然
			// cache miss，于是重复调用审核模型并写入两条审核记录。
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

	switch {
	case platform == service.PlatformOpenAI:
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
	case platform == service.PlatformGrok || service.IsCNProvider(platform):
		// Grok 与国产 OpenAI 兼容供应商共用的自动路由分支：/v1/messages、
		// /v1/responses、/v1/chat/completions。供应商清单必须与 gateway.go 的
		// isOpenAIResponsesCompatibleGatewayPlatform 对齐；Images/Embeddings/
		// AlphaSearch 只对 OpenAI 开放，不在这些平台的自动路由内。
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

// openAIHTTPBranchOwnsRequest 报告这条通配前向元数据是否会被同一路由注册的
// OpenAI HTTP 分支接管。/v1/messages、/v1/responses、/v1/chat/completions 会按
// 平台把请求交给 OpenAI 网关 handler，并各自注册一条 coveredOpenAIHTTPRoute 分支；
// 平台命中该分支时，通配前向管线必须让位，否则同一请求会被两条管线各审一次。
// 未注册分支的通配路由（如 /v1/messages/count_tokens）仍由通配前向管线负责。
func openAIHTTPBranchOwnsRequest(meta ModeratedRouteMeta) bool {
	switch strings.TrimSpace(meta.Handler) {
	case "GatewayHandler.Messages":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolAnthropicMessages
	case "GatewayHandler.Responses":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolOpenAIResponses
	case "GatewayHandler.ChatCompletions":
		return strings.TrimSpace(meta.Protocol) == service.ContentModerationProtocolOpenAIChat
	default:
		return false
	}
}
