package routes

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterGatewayRoutes 注册 API 网关路由（Claude/OpenAI/Gemini 兼容）
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)
	requireGroupGoogle := middleware.RequireGroupAssignment(settingService, middleware.GoogleErrorWriter)

	isOpenAIResponsesCompatibleGatewayPlatform := func(c *gin.Context) bool {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI, service.PlatformGrok:
			return true
		default:
			return false
		}
	}
	isOpenAIGatewayPlatform := func(c *gin.Context) bool {
		return getGroupPlatform(c) == service.PlatformOpenAI
	}
	rejectGrokUnsupportedEndpoint := func(c *gin.Context, endpoint string) {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": endpoint + " is not supported for Grok groups",
			},
		})
	}

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	openAIHTTPPipelineEntrypoints := GatewayPipelineEntrypoints{
		moderationcoverage.PipelineOpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			switch meta.Protocol {
			case service.ContentModerationProtocolOpenAIChat, service.ContentModerationProtocolOpenAIMessages, service.ContentModerationProtocolOpenAIResponses, service.ContentModerationProtocolOpenAIImages, service.ContentModerationProtocolOpenAIEmbeddings:
			default:
				return GatewayPipelineEntryResult{}
			}
			if !isOpenAIGatewayPlatform(c) {
				return GatewayPipelineEntryResult{}
			}
			result := h.OpenAIGateway.EnterOpenAIHTTPGatewayPipeline(c, meta)
			return GatewayPipelineEntryResult{Stop: result.Stop}
		}),
		moderationcoverage.PipelineOpenAIWebSocket: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			if meta.Protocol != service.ContentModerationProtocolOpenAIResponses {
				return GatewayPipelineEntryResult{}
			}
			moderationcoverage.MarkPipelineEntrypointEntered(c, moderationcoverage.PipelineOpenAIWebSocket, "GatewayPipelineRegistrar.OpenAIWebSocket")
			return GatewayPipelineEntryResult{}
		}),
		moderationcoverage.PipelineGatewayPreForward: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			switch meta.Protocol {
			case service.ContentModerationProtocolAnthropicMessages, service.ContentModerationProtocolGemini:
			default:
				return GatewayPipelineEntryResult{}
			}
			switch meta.Handler {
			case "GatewayHandler.Messages", "GatewayHandler.CountTokens", "GatewayHandler.GeminiV1BetaModels":
			default:
				return GatewayPipelineEntryResult{}
			}
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI, service.PlatformGrok:
				return GatewayPipelineEntryResult{}
			}
			result := h.Gateway.EnterGatewayPreForwardPipeline(c, meta)
			return GatewayPipelineEntryResult{Stop: result.Blocked}
		}),
	}
	moderatedGateway := NewGatewayPipelineRegistrar(gateway, openAIHTTPPipelineEntrypoints)
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.Use(requireGroupAnthropic)
	{
		openAIMessagesRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/messages",
			"OpenAIGatewayHandler.Messages",
			service.ContentModerationProtocolOpenAIMessages,
			"OpenAI groups using the Anthropic-compatible Messages endpoint are moderated by the OpenAI HTTP pipeline before scheduling and upstream forwarding.",
		))
		// /v1/messages: auto-route based on group platform
		moderatedGateway.POST("/messages", coveredModeratedRoute(
			"/v1/messages",
			"GatewayHandler.Messages",
			service.ContentModerationProtocolAnthropicMessages,
			"Anthropic Messages requests are moderated after request parsing and before billing, scheduling, and upstream forwarding.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Messages API")
				return
			}
			if isOpenAIGatewayPlatform(c) {
				setModeratedRouteBranchMeta(c, openAIMessagesRouteMeta)
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI groups get 404
		moderatedGateway.POST("/messages/count_tokens", coveredModeratedRoute(
			"/v1/messages/count_tokens",
			"GatewayHandler.CountTokens",
			service.ContentModerationProtocolAnthropicMessages,
			"Anthropic count_tokens can forward client context to upstream, so it is moderated after model validation and before billing, scheduling, and forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"type": "error",
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Token counting is not supported for this platform",
					},
				})
				return
			}
			h.Gateway.CountTokens(c)
		})
		moderatedGateway.GETNoAudit("/models", intentionalNoAuditRoute(
			"/v1/models",
			"GatewayHandler.Models",
			"Model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.Models)
		moderatedGateway.GETNoAudit("/usage", intentionalNoAuditRoute(
			"/v1/usage",
			"GatewayHandler.Usage",
			"Usage lookup does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.Usage)
		// OpenAI Responses API: auto-route based on group platform
		moderatedGateway.POST("/responses", coveredOpenAIHTTPRoute(
			"/v1/responses",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"OpenAI Responses requests are moderated before permission checks, scheduling, and upstream forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		moderatedGateway.POST("/responses/*subpath", coveredOpenAIHTTPRoute(
			"/v1/responses/*subpath",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Versioned Responses subpaths reach the same Responses handler and moderation hook before upstream forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		moderatedGateway.GET("/responses", coveredOpenAIWebSocketRoute(
			"/v1/responses",
			"OpenAIGatewayHandler.ResponsesWebSocket",
			service.ContentModerationProtocolOpenAIResponses,
			"Responses WebSocket audits the first frame and subsequent client turns before upstream writes.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		moderatedGateway.POST("/chat/completions", coveredOpenAIHTTPRoute(
			"/v1/chat/completions",
			"OpenAIGatewayHandler.ChatCompletions",
			service.ContentModerationProtocolOpenAIChat,
			"OpenAI-compatible chat requests are moderated before image permission checks, scheduling, and upstream forwarding.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Chat Completions API")
				return
			}
			if isOpenAIGatewayPlatform(c) {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
		moderatedGateway.POST("/embeddings", coveredOpenAIHTTPRoute(
			"/v1/embeddings",
			"OpenAIGatewayHandler.Embeddings",
			service.ContentModerationProtocolOpenAIEmbeddings,
			"Embeddings input can be submitted to upstream policy systems, so input is moderated before channel mapping, scheduling, and forwarding.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformOpenAI {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Embeddings API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Embeddings(c)
		})
		moderatedGateway.POST("/images/generations", coveredOpenAIHTTPRoute(
			"/v1/images/generations",
			"OpenAIGatewayHandler.Images",
			service.ContentModerationProtocolOpenAIImages,
			"Image generation permission is checked before moderation; prompt and image metadata are then moderated before scheduling and upstream forwarding.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformOpenAI {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Images API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Images(c)
		})
		moderatedGateway.POST("/images/edits", coveredOpenAIHTTPRoute(
			"/v1/images/edits",
			"OpenAIGatewayHandler.Images",
			service.ContentModerationProtocolOpenAIImages,
			"Image edit permission is checked before moderation; prompt and image metadata are then moderated before scheduling and upstream forwarding.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformOpenAI {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Images API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Images(c)
		})
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	moderatedGemini := NewGatewayPipelineRegistrar(gemini, openAIHTTPPipelineEntrypoints)
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(requireGroupGoogle)
	{
		moderatedGemini.GETNoAudit("/models", intentionalNoAuditRoute(
			"/v1beta/models",
			"GatewayHandler.GeminiV1BetaListModels",
			"Gemini model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.GeminiV1BetaListModels)
		moderatedGemini.GETNoAudit("/models/:model", intentionalNoAuditRoute(
			"/v1beta/models/:model",
			"GatewayHandler.GeminiV1BetaGetModel",
			"Gemini model metadata lookup does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.GeminiV1BetaGetModel)
		// Gin treats ":" as a param marker, but Gemini uses "{model}:{action}" in the same segment.
		moderatedGemini.POST("/models/*modelAction", coveredModeratedRoute(
			"/v1beta/models/*modelAction",
			"GatewayHandler.GeminiV1BetaModels",
			service.ContentModerationProtocolGemini,
			"Gemini model action requests are moderated before account selection and upstream forwarding.",
		), h.Gateway.GeminiV1BetaModels)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	moderatedRoot := NewGatewayPipelineRegistrar(r, openAIHTTPPipelineEntrypoints)
	moderatedRoot.POST("/responses", coveredOpenAIHTTPRoute(
		"/responses",
		"OpenAIGatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses alias reaches the same OpenAI-compatible Responses handler and moderation hook.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	moderatedRoot.POST("/responses/*subpath", coveredOpenAIHTTPRoute(
		"/responses/*subpath",
		"OpenAIGatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses subpath alias reaches the same OpenAI-compatible Responses handler and moderation hook.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, responsesHandler)
	moderatedRoot.GET("/responses", coveredOpenAIWebSocketRoute(
		"/responses",
		"OpenAIGatewayHandler.ResponsesWebSocket",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses WebSocket alias audits the first frame and subsequent client turns before upstream writes.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
			return
		}
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	codexDirect := r.Group("/backend-api/codex")
	moderatedCodexDirect := NewGatewayPipelineRegistrar(codexDirect, openAIHTTPPipelineEntrypoints)
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic)
	{
		moderatedCodexDirect.POST("/responses", coveredOpenAIHTTPRoute(
			"/backend-api/codex/responses",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses route reaches the same OpenAI-compatible Responses handler and moderation hook.",
		), responsesHandler)
		moderatedCodexDirect.POST("/responses/*subpath", coveredOpenAIHTTPRoute(
			"/backend-api/codex/responses/*subpath",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses subpaths reach the same OpenAI-compatible Responses handler and moderation hook.",
		), responsesHandler)
		moderatedCodexDirect.GET("/responses", coveredOpenAIWebSocketRoute(
			"/backend-api/codex/responses",
			"OpenAIGatewayHandler.ResponsesWebSocket",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses WebSocket route audits the first frame and subsequent client turns before upstream writes.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) == service.PlatformGrok {
				rejectGrokUnsupportedEndpoint(c, "Responses WebSocket API")
				return
			}
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	moderatedRoot.POST("/chat/completions", coveredOpenAIHTTPRoute(
		"/chat/completions",
		"OpenAIGatewayHandler.ChatCompletions",
		service.ContentModerationProtocolOpenAIChat,
		"Root chat alias reaches the same OpenAI-compatible chat handler and moderation hook.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			rejectGrokUnsupportedEndpoint(c, "Chat Completions API")
			return
		}
		if isOpenAIGatewayPlatform(c) {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	moderatedRoot.POST("/embeddings", coveredOpenAIHTTPRoute(
		"/embeddings",
		"OpenAIGatewayHandler.Embeddings",
		service.ContentModerationProtocolOpenAIEmbeddings,
		"Root embeddings alias reaches the same Embeddings handler and moderation hook.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformOpenAI {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Embeddings API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Embeddings(c)
	})
	moderatedRoot.POST("/images/generations", coveredOpenAIHTTPRoute(
		"/images/generations",
		"OpenAIGatewayHandler.Images",
		service.ContentModerationProtocolOpenAIImages,
		"Root image generation alias reaches the same Images handler and moderation hook before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformOpenAI {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Images(c)
	})
	moderatedRoot.POST("/images/edits", coveredOpenAIHTTPRoute(
		"/images/edits",
		"OpenAIGatewayHandler.Images",
		service.ContentModerationProtocolOpenAIImages,
		"Root image edit alias reaches the same Images handler and moderation hook before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformOpenAI {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Images(c)
	})

	// Antigravity 模型列表
	moderatedRoot.GETNoAudit("/antigravity/models", intentionalNoAuditRoute(
		"/antigravity/models",
		"GatewayHandler.AntigravityModels",
		"Antigravity model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
	), gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	moderatedAntigravityV1 := NewModeratedRouteRegistrar(antigravityV1)
	antigravityV1.Use(bodyLimit)
	antigravityV1.Use(clientRequestID)
	antigravityV1.Use(opsErrorLogger)
	antigravityV1.Use(endpointNorm)
	antigravityV1.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1.Use(gin.HandlerFunc(apiKeyAuth))
	antigravityV1.Use(requireGroupAnthropic)
	{
		moderatedAntigravityV1.POST("/messages", coveredModeratedRoute(
			"/antigravity/v1/messages",
			"GatewayHandler.Messages",
			service.ContentModerationProtocolAnthropicMessages,
			"Antigravity Messages uses the same GatewayHandler.Messages moderation hook before upstream forwarding.",
		), h.Gateway.Messages)
		moderatedAntigravityV1.POST("/messages/count_tokens", coveredModeratedRoute(
			"/antigravity/v1/messages/count_tokens",
			"GatewayHandler.CountTokens",
			service.ContentModerationProtocolAnthropicMessages,
			"Antigravity count_tokens uses the shared CountTokens handler and is moderated after model validation and before account selection or upstream forwarding.",
		), h.Gateway.CountTokens)
		moderatedAntigravityV1.GETNoAudit("/models", intentionalNoAuditRoute(
			"/antigravity/v1/models",
			"GatewayHandler.AntigravityModels",
			"Antigravity v1 model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.AntigravityModels)
		moderatedAntigravityV1.GETNoAudit("/usage", intentionalNoAuditRoute(
			"/antigravity/v1/usage",
			"GatewayHandler.Usage",
			"Antigravity v1 usage lookup does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.Usage)
	}

	antigravityV1Beta := r.Group("/antigravity/v1beta")
	moderatedAntigravityV1Beta := NewGatewayPipelineRegistrar(antigravityV1Beta, openAIHTTPPipelineEntrypoints)
	antigravityV1Beta.Use(bodyLimit)
	antigravityV1Beta.Use(clientRequestID)
	antigravityV1Beta.Use(opsErrorLogger)
	antigravityV1Beta.Use(endpointNorm)
	antigravityV1Beta.Use(middleware.ForcePlatform(service.PlatformAntigravity))
	antigravityV1Beta.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	antigravityV1Beta.Use(requireGroupGoogle)
	{
		moderatedAntigravityV1Beta.GETNoAudit("/models", intentionalNoAuditRoute(
			"/antigravity/v1beta/models",
			"GatewayHandler.GeminiV1BetaListModels",
			"Antigravity Gemini model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.GeminiV1BetaListModels)
		moderatedAntigravityV1Beta.GETNoAudit("/models/:model", intentionalNoAuditRoute(
			"/antigravity/v1beta/models/:model",
			"GatewayHandler.GeminiV1BetaGetModel",
			"Antigravity Gemini model metadata lookup does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.GeminiV1BetaGetModel)
		moderatedAntigravityV1Beta.POST("/models/*modelAction", coveredModeratedRoute(
			"/antigravity/v1beta/models/*modelAction",
			"GatewayHandler.GeminiV1BetaModels",
			service.ContentModerationProtocolGemini,
			"Antigravity Gemini-compatible model action requests use the same Gemini handler and moderation hook before upstream forwarding.",
		), h.Gateway.GeminiV1BetaModels)
	}

}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}
