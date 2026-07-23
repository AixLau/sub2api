package routes

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	textBodyLimit := middleware.RequestBodyLimit(cfg.Gateway.TextMaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()
	compositeTarget := compositeTargetPlatformMiddleware(compositeResolver)
	compositeGeminiTarget := compositeGeminiTargetPlatformMiddleware(compositeResolver)

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
	countTokensHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.CountTokens(c)
		case service.PlatformGrok:
			h.OpenAIGateway.GrokCountTokens(c)
		default:
			h.Gateway.CountTokens(c)
		}
	}
	modelsHandler := func(c *gin.Context) {
		if isOpenAIGatewayPlatform(c) && c.Query("client_version") != "" {
			h.OpenAIGateway.CodexModels(c)
			return
		}
		h.Gateway.Models(c)
	}
	isOpenAIOnlyEndpointGatewayPlatform := func(c *gin.Context) bool {
		return getGroupPlatform(c) == service.PlatformOpenAI
	}
	imagesHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.Images(c)
		case service.PlatformGrok:
			h.OpenAIGateway.GrokImages(c)
		default:
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
		}
	}
	videoGenerationHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.GrokVideoGeneration(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Videos API is not supported for this platform",
			},
		})
	}
	videoStatusHandler := func(c *gin.Context) {
		// Video status requests do not carry a model, so composite groups cannot
		// be resolved by compositeTargetPlatformMiddleware. Route them through
		// the Grok handler and let scheduler/account selection enforce capacity.
		if getGroupPlatform(c) == service.PlatformGrok || getGroupPlatform(c) == service.PlatformComposite {
			h.OpenAIGateway.GrokVideoStatus(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Videos API is not supported for this platform",
			},
		})
	}
	videoContentHandler := func(c *gin.Context) {
		// Video content requests do not carry a model, so composite groups cannot
		// be resolved by compositeTargetPlatformMiddleware. Route them through
		// the Grok handler just like video status lookups.
		if getGroupPlatform(c) == service.PlatformGrok || getGroupPlatform(c) == service.PlatformComposite {
			h.OpenAIGateway.GrokVideoContent(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Videos API is not supported for this platform",
			},
		})
	}
	videoEditHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.GrokVideoEdit(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Videos API is not supported for this platform"}})
	}
	videoExtensionHandler := func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.GrokVideoExtension(c)
			return
		}
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"type": "not_found_error", "message": "Videos API is not supported for this platform"}})
	}
	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	globalGatewayPipelineEntrypoint := NewGatewayPipelineEntrypointDispatcherForHandlers(h, getGroupPlatform, isOpenAIGatewayPlatform)
	openAIHTTPPipelineEntrypoints := GatewayPipelineEntrypoints{
		moderationcoverage.PipelineGatewayGlobal: globalGatewayPipelineEntrypoint,
	}
	moderatedGateway := NewGatewayPipelineRegistrar(gateway, openAIHTTPPipelineEntrypoints)
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	moderatedGateway.GETNoAudit("/sub2api/billing", intentionalNoAuditRoute(
		"/v1/sub2api/billing",
		"GatewayHandler.KeyBillingInfo",
		"API-key billing lookup reads local billing state and does not submit model-visible content upstream.",
	), h.Gateway.KeyBillingInfo)
	gateway.Use(compositeTarget)
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
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIMessagesRouteMeta).Stop {
					return
				}
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens: OpenAI bridges upstream, Grok estimates
		// locally, and Anthropic-compatible platforms retain their existing path.
		moderatedGateway.POST("/messages/count_tokens", coveredModeratedRoute(
			"/v1/messages/count_tokens",
			"GatewayHandler.CountTokens",
			service.ContentModerationProtocolAnthropicMessages,
			"Anthropic count_tokens can forward client context to upstream, so it is moderated after model validation and before billing, scheduling, and forwarding.",
		), countTokensHandler)
		moderatedGateway.GETNoAudit("/models", intentionalNoAuditRoute(
			"/v1/models",
			"GatewayHandler.Models",
			"Model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
		), modelsHandler)
		moderatedGateway.GETNoAudit("/usage", intentionalNoAuditRoute(
			"/v1/usage",
			"GatewayHandler.Usage",
			"Usage lookup does not submit model-visible user content to upstream moderation-sensitive paths.",
		), h.Gateway.Usage)
		// OpenAI Responses API: auto-route based on group platform
		openAIResponsesRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/responses",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"OpenAI Responses requests are moderated before permission checks, scheduling, and upstream forwarding.",
		))
		moderatedGateway.POST("/responses", coveredModeratedRoute(
			"/v1/responses",
			"GatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Responses requests for non-OpenAI groups are moderated by the shared Gateway pre-forward pipeline before scheduling and upstream forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIResponsesRouteMeta).Stop {
					return
				}
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		openAIResponsesSubpathRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/responses/*subpath",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Versioned Responses subpaths reach the same Responses handler and moderation hook before upstream forwarding.",
		))
		moderatedGateway.POST("/responses/*subpath", coveredModeratedRoute(
			"/v1/responses/*subpath",
			"GatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Responses subpaths for non-OpenAI groups use the shared Gateway pre-forward pipeline before upstream forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIResponsesSubpathRouteMeta).Stop {
					return
				}
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		moderatedGateway.POST("/alpha/search", coveredOpenAIHTTPRoute(
			"/v1/alpha/search",
			"OpenAIGatewayHandler.AlphaSearch",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex standalone search input is moderated before account selection and upstream forwarding.",
		), textBodyLimit, h.OpenAIGateway.AlphaSearch)
		moderatedGateway.GET("/responses", coveredOpenAIWebSocketRoute(
			"/v1/responses",
			"OpenAIGatewayHandler.ResponsesWebSocket",
			service.ContentModerationProtocolOpenAIResponses,
			"Responses WebSocket audits the first frame and subsequent client turns before upstream writes.",
		), func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		openAIChatCompletionsRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/chat/completions",
			"OpenAIGatewayHandler.ChatCompletions",
			service.ContentModerationProtocolOpenAIChat,
			"OpenAI-compatible chat requests are moderated before image permission checks, scheduling, and upstream forwarding.",
		))
		moderatedGateway.POST("/chat/completions", coveredModeratedRoute(
			"/v1/chat/completions",
			"GatewayHandler.ChatCompletions",
			service.ContentModerationProtocolOpenAIChat,
			"Chat Completions requests for non-OpenAI groups are moderated by the shared Gateway pre-forward pipeline before scheduling and upstream forwarding.",
		), func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIChatCompletionsRouteMeta).Stop {
					return
				}
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
		), textBodyLimit, func(c *gin.Context) {
			if !isOpenAIOnlyEndpointGatewayPlatform(c) {
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
			imagesHandler(c)
		})
		moderatedGateway.POST("/images/edits", coveredOpenAIHTTPRoute(
			"/v1/images/edits",
			"OpenAIGatewayHandler.Images",
			service.ContentModerationProtocolOpenAIImages,
			"Image edit permission is checked before moderation; prompt and image metadata are then moderated before scheduling and upstream forwarding.",
		), func(c *gin.Context) {
			imagesHandler(c)
		})
		moderatedGateway.POST("/images/generations/async", coveredOpenAIHTTPRoute(
			"/v1/images/generations/async",
			"OpenAIGatewayHandler.Images",
			service.ContentModerationProtocolOpenAIImages,
			"Async image generation uses the same permission and moderation pipeline before task creation.",
		), h.AsyncImage.Submit)
		moderatedGateway.POST("/images/edits/async", coveredOpenAIHTTPRoute(
			"/v1/images/edits/async",
			"OpenAIGatewayHandler.Images",
			service.ContentModerationProtocolOpenAIImages,
			"Async image edits use the same permission and moderation pipeline before task creation.",
		), h.AsyncImage.Submit)
		moderatedGateway.GETNoAudit("/images/tasks/:task_id", intentionalNoAuditRoute(
			"/v1/images/tasks/:task_id",
			"AsyncImageHandler.Get",
			"Async image task lookup reads existing task state and does not submit new model-visible content.",
		), h.AsyncImage.Get)
		openAIVideoGenerationRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/videos/generations",
			"OpenAIGatewayHandler.GrokVideoGeneration",
			service.ContentModerationProtocolOpenAIImages,
			"Grok video generation prompt metadata is moderated through the shared Grok media handler before permission checks, scheduling, and upstream forwarding.",
		))
		moderatedGateway.POST("/videos/generations", intentionalNoAuditRoute(
			"/v1/videos/generations",
			"OpenAIGatewayHandler.GrokVideoGeneration",
			"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformGrok {
				videoGenerationHandler(c)
				return
			}
			if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIVideoGenerationRouteMeta).Stop {
				return
			}
			videoGenerationHandler(c)
		})
		openAIVideoEditRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/videos/edits",
			"OpenAIGatewayHandler.GrokVideoEdit",
			service.ContentModerationProtocolOpenAIImages,
			"Grok video edit prompt and source media metadata are moderated before permission checks, scheduling, and upstream forwarding.",
		))
		moderatedGateway.POST("/videos/edits", intentionalNoAuditRoute(
			"/v1/videos/edits",
			"OpenAIGatewayHandler.GrokVideoEdit",
			"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformGrok {
				videoEditHandler(c)
				return
			}
			if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIVideoEditRouteMeta).Stop {
				return
			}
			videoEditHandler(c)
		})
		openAIVideoExtensionRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/v1/videos/extensions",
			"OpenAIGatewayHandler.GrokVideoExtension",
			service.ContentModerationProtocolOpenAIImages,
			"Grok video extension prompt and source video metadata are moderated before permission checks, scheduling, and upstream forwarding.",
		))
		moderatedGateway.POST("/videos/extensions", intentionalNoAuditRoute(
			"/v1/videos/extensions",
			"OpenAIGatewayHandler.GrokVideoExtension",
			"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
		), func(c *gin.Context) {
			if getGroupPlatform(c) != service.PlatformGrok {
				videoExtensionHandler(c)
				return
			}
			if enterModeratedRouteBranchPipeline(c, moderatedGateway, openAIVideoExtensionRouteMeta).Stop {
				return
			}
			videoExtensionHandler(c)
		})
		moderatedGateway.GETNoAudit("/videos/:request_id", intentionalNoAuditRoute(
			"/v1/videos/:request_id",
			"OpenAIGatewayHandler.GrokVideoStatus",
			"Grok video status lookup uses an upstream request id and does not submit new model-visible user content.",
		), videoStatusHandler)
		moderatedGateway.GETNoAudit("/videos/:request_id/content", intentionalNoAuditRoute(
			"/v1/videos/:request_id/content",
			"OpenAIGatewayHandler.GrokVideoContent",
			"Grok video content lookup proxies already-generated output and does not submit new model-visible user content.",
		), videoContentHandler)
		moderatedGateway.POST("/images/batches", coveredModeratedRoute(
			"/v1/images/batches",
			"BatchImageHandler.Submit",
			service.ContentModerationProtocolBatchImages,
			"Batch image submit is moderated after account selection and before pricing, job creation, balance hold, or provider submission.",
		), h.BatchImage.Submit)
		moderatedGateway.GETNoAudit("/images/batches", intentionalNoAuditRoute(
			"/v1/images/batches",
			"BatchImageHandler.List",
			"Batch image listing reads local job metadata and does not submit new model-visible user content.",
		), h.BatchImage.List)
		moderatedGateway.GETNoAudit("/images/batches/models", intentionalNoAuditRoute(
			"/v1/images/batches/models",
			"BatchImageHandler.Models",
			"Batch image model listing reads allowed model metadata and does not submit model-visible user content.",
		), h.BatchImage.Models)
		moderatedGateway.GETNoAudit("/images/batches/:id", intentionalNoAuditRoute(
			"/v1/images/batches/:id",
			"BatchImageHandler.Get",
			"Batch image detail reads existing local job state and does not submit new model-visible user content.",
		), h.BatchImage.Get)
		moderatedGateway.GETNoAudit("/images/batches/:id/items", intentionalNoAuditRoute(
			"/v1/images/batches/:id/items",
			"BatchImageHandler.Items",
			"Batch image item listing reads existing local item state and does not submit new model-visible user content.",
		), h.BatchImage.Items)
		moderatedGateway.GETNoAudit("/images/batches/:id/items/:custom_id/content", intentionalNoAuditRoute(
			"/v1/images/batches/:id/items/:custom_id/content",
			"BatchImageHandler.ItemContent",
			"Batch image item content download streams already generated output and does not submit new model-visible user content.",
		), h.BatchImage.ItemContent)
		moderatedGateway.GETNoAudit("/images/batches/:id/download", intentionalNoAuditRoute(
			"/v1/images/batches/:id/download",
			"BatchImageHandler.Download",
			"Batch image archive download streams already generated output and does not submit new model-visible user content.",
		), h.BatchImage.Download)
		moderatedGateway.POST("/images/batches/:id/cancel", intentionalNoAuditRoute(
			"/v1/images/batches/:id/cancel",
			"BatchImageHandler.Cancel",
			"Batch image cancel updates local job state and does not submit new model-visible user content.",
		), h.BatchImage.Cancel)
		moderatedGateway.DELETENoAudit("/images/batches/:id", intentionalNoAuditRoute(
			"/v1/images/batches/:id",
			"BatchImageHandler.DeleteRecord",
			"Batch image record deletion removes local job metadata and does not submit model-visible user content.",
		), h.BatchImage.DeleteRecord)
		moderatedGateway.DELETENoAudit("/images/batches/:id/outputs", intentionalNoAuditRoute(
			"/v1/images/batches/:id/outputs",
			"BatchImageHandler.DeleteOutputs",
			"Batch image output deletion removes local generated files and does not submit model-visible user content.",
		), h.BatchImage.DeleteOutputs)
	}

	// Gemini 原生 API 兼容层（Gemini SDK/CLI 直连）
	gemini := r.Group("/v1beta")
	moderatedGemini := NewGatewayPipelineRegistrar(gemini, openAIHTTPPipelineEntrypoints)
	gemini.Use(bodyLimit)
	gemini.Use(clientRequestID)
	gemini.Use(opsErrorLogger)
	gemini.Use(endpointNorm)
	gemini.Use(middleware.APIKeyAuthWithSubscriptionGoogle(apiKeyService, subscriptionService, cfg))
	gemini.Use(compositeGeminiTarget)
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
	rootOpenAIResponsesRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/responses",
		"OpenAIGatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses alias reaches the same OpenAI-compatible Responses handler and moderation hook.",
	))
	moderatedRoot := NewGatewayPipelineRegistrar(r, openAIHTTPPipelineEntrypoints)
	responsesHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIResponsesRouteMeta).Stop {
				return
			}
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	moderatedRoot.POST("/responses", coveredModeratedRoute(
		"/responses",
		"GatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses alias for non-OpenAI groups uses the shared Gateway pre-forward pipeline before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, responsesHandler)
	rootOpenAIResponsesSubpathRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/responses/*subpath",
		"OpenAIGatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses subpath alias reaches the same OpenAI-compatible Responses handler and moderation hook.",
	))
	responsesSubpathHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIResponsesSubpathRouteMeta).Stop {
				return
			}
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	moderatedRoot.POST("/responses/*subpath", coveredModeratedRoute(
		"/responses/*subpath",
		"GatewayHandler.Responses",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses subpath alias for non-OpenAI groups uses the shared Gateway pre-forward pipeline before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, responsesSubpathHandler)
	moderatedRoot.POST("/alpha/search", coveredOpenAIHTTPRoute(
		"/alpha/search",
		"OpenAIGatewayHandler.AlphaSearch",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Codex standalone search input is moderated before account selection and upstream forwarding.",
	), textBodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.OpenAIGateway.AlphaSearch)
	moderatedRoot.GET("/responses", coveredOpenAIWebSocketRoute(
		"/responses",
		"OpenAIGatewayHandler.ResponsesWebSocket",
		service.ContentModerationProtocolOpenAIResponses,
		"Root Responses WebSocket alias audits the first frame and subsequent client turns before upstream writes.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) == service.PlatformGrok {
			h.OpenAIGateway.ResponsesWebSocket(c)
			return
		}
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	moderatedRoot.GETNoAudit("/models", intentionalNoAuditRoute(
		"/models",
		"GatewayHandler.Models",
		"Root model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, modelsHandler)
	moderatedRoot.POST("/messages/count_tokens", coveredModeratedRoute(
		"/messages/count_tokens",
		"GatewayHandler.CountTokens",
		service.ContentModerationProtocolAnthropicMessages,
		"Root count_tokens can forward client context to upstream, so it is moderated after model validation and before billing, scheduling, and forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, countTokensHandler)
	codexDirect := r.Group("/backend-api/codex")
	moderatedCodexDirect := NewGatewayPipelineRegistrar(codexDirect, openAIHTTPPipelineEntrypoints)
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic)
	{
		codexOpenAIResponsesRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/backend-api/codex/responses",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses route reaches the same OpenAI-compatible Responses handler and moderation hook.",
		))
		codexResponsesHandler := func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedCodexDirect, codexOpenAIResponsesRouteMeta).Stop {
					return
				}
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		}
		moderatedCodexDirect.POST("/responses", coveredModeratedRoute(
			"/backend-api/codex/responses",
			"GatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses route for non-OpenAI groups uses the shared Gateway pre-forward pipeline before upstream forwarding.",
		), codexResponsesHandler)
		codexOpenAIResponsesSubpathRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
			"/backend-api/codex/responses/*subpath",
			"OpenAIGatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses subpaths reach the same OpenAI-compatible Responses handler and moderation hook.",
		))
		codexResponsesSubpathHandler := func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				if enterModeratedRouteBranchPipeline(c, moderatedCodexDirect, codexOpenAIResponsesSubpathRouteMeta).Stop {
					return
				}
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		}
		moderatedCodexDirect.POST("/responses/*subpath", coveredModeratedRoute(
			"/backend-api/codex/responses/*subpath",
			"GatewayHandler.Responses",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses subpaths for non-OpenAI groups use the shared Gateway pre-forward pipeline before upstream forwarding.",
		), codexResponsesSubpathHandler)
		moderatedCodexDirect.POST("/alpha/search", coveredOpenAIHTTPRoute(
			"/backend-api/codex/alpha/search",
			"OpenAIGatewayHandler.AlphaSearch",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct standalone search input is moderated before account selection and upstream forwarding.",
		), textBodyLimit, h.OpenAIGateway.AlphaSearch)
		moderatedCodexDirect.GET("/responses", coveredOpenAIWebSocketRoute(
			"/backend-api/codex/responses",
			"OpenAIGatewayHandler.ResponsesWebSocket",
			service.ContentModerationProtocolOpenAIResponses,
			"Codex direct Responses WebSocket route audits the first frame and subsequent client turns before upstream writes.",
		), func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		moderatedCodexDirect.GETNoAudit("/models", intentionalNoAuditRoute(
			"/backend-api/codex/models",
			"OpenAIGatewayHandler.CodexModels",
			"Codex model manifest lookup reads model metadata and does not submit model-visible user content.",
		), h.OpenAIGateway.CodexModels)
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	rootOpenAIChatCompletionsRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/chat/completions",
		"OpenAIGatewayHandler.ChatCompletions",
		service.ContentModerationProtocolOpenAIChat,
		"Root chat alias reaches the same OpenAI-compatible chat handler and moderation hook.",
	))
	moderatedRoot.POST("/chat/completions", coveredModeratedRoute(
		"/chat/completions",
		"GatewayHandler.ChatCompletions",
		service.ContentModerationProtocolOpenAIChat,
		"Root chat alias for non-OpenAI groups uses the shared Gateway pre-forward pipeline before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIChatCompletionsRouteMeta).Stop {
				return
			}
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
	), textBodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if !isOpenAIOnlyEndpointGatewayPlatform(c) {
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
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		imagesHandler(c)
	})
	moderatedRoot.POST("/images/generations/async", coveredOpenAIHTTPRoute(
		"/images/generations/async",
		"OpenAIGatewayHandler.Images",
		service.ContentModerationProtocolOpenAIImages,
		"Root async image generation alias uses the same permission and moderation pipeline before task creation.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Submit)
	moderatedRoot.POST("/images/edits/async", coveredOpenAIHTTPRoute(
		"/images/edits/async",
		"OpenAIGatewayHandler.Images",
		service.ContentModerationProtocolOpenAIImages,
		"Root async image edit alias uses the same permission and moderation pipeline before task creation.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Submit)
	moderatedRoot.GETNoAudit("/images/tasks/:task_id", intentionalNoAuditRoute(
		"/images/tasks/:task_id",
		"AsyncImageHandler.Get",
		"Root async image task lookup reads existing task state and does not submit new model-visible content.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Get)
	moderatedRoot.POST("/images/edits", coveredOpenAIHTTPRoute(
		"/images/edits",
		"OpenAIGatewayHandler.Images",
		service.ContentModerationProtocolOpenAIImages,
		"Root image edit alias reaches the same Images handler and moderation hook before upstream forwarding.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		imagesHandler(c)
	})
	rootOpenAIVideoGenerationRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/videos/generations",
		"OpenAIGatewayHandler.GrokVideoGeneration",
		service.ContentModerationProtocolOpenAIImages,
		"Root Grok video generation alias reaches the shared Grok media moderation hook before upstream forwarding.",
	))
	moderatedRoot.POST("/videos/generations", intentionalNoAuditRoute(
		"/videos/generations",
		"OpenAIGatewayHandler.GrokVideoGeneration",
		"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			videoGenerationHandler(c)
			return
		}
		if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIVideoGenerationRouteMeta).Stop {
			return
		}
		videoGenerationHandler(c)
	})
	rootOpenAIVideoEditRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/videos/edits",
		"OpenAIGatewayHandler.GrokVideoEdit",
		service.ContentModerationProtocolOpenAIImages,
		"Root Grok video edit alias reaches the shared Grok media moderation hook before upstream forwarding.",
	))
	moderatedRoot.POST("/videos/edits", intentionalNoAuditRoute(
		"/videos/edits",
		"OpenAIGatewayHandler.GrokVideoEdit",
		"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			videoEditHandler(c)
			return
		}
		if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIVideoEditRouteMeta).Stop {
			return
		}
		videoEditHandler(c)
	})
	rootOpenAIVideoExtensionRouteMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/videos/extensions",
		"OpenAIGatewayHandler.GrokVideoExtension",
		service.ContentModerationProtocolOpenAIImages,
		"Root Grok video extension alias reaches the shared Grok media moderation hook before upstream forwarding.",
	))
	moderatedRoot.POST("/videos/extensions", intentionalNoAuditRoute(
		"/videos/extensions",
		"OpenAIGatewayHandler.GrokVideoExtension",
		"Non-Grok groups are rejected before upstream content handling; Grok groups enter the OpenAI HTTP moderation branch.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if getGroupPlatform(c) != service.PlatformGrok {
			videoExtensionHandler(c)
			return
		}
		if enterModeratedRouteBranchPipeline(c, moderatedRoot, rootOpenAIVideoExtensionRouteMeta).Stop {
			return
		}
		videoExtensionHandler(c)
	})
	moderatedRoot.GETNoAudit("/videos/:request_id", intentionalNoAuditRoute(
		"/videos/:request_id",
		"OpenAIGatewayHandler.GrokVideoStatus",
		"Root Grok video status lookup uses an upstream request id and does not submit new model-visible user content.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, videoStatusHandler)
	moderatedRoot.GETNoAudit("/videos/:request_id/content", intentionalNoAuditRoute(
		"/videos/:request_id/content",
		"OpenAIGatewayHandler.GrokVideoContent",
		"Root Grok video content lookup proxies already-generated output and does not submit new model-visible user content.",
	), bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, videoContentHandler)

	// Antigravity 模型列表
	moderatedRoot.GETNoAudit("/antigravity/models", intentionalNoAuditRoute(
		"/antigravity/models",
		"GatewayHandler.AntigravityModels",
		"Antigravity model listing does not submit model-visible user content to upstream moderation-sensitive paths.",
	), gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, h.Gateway.AntigravityModels)

	// Antigravity 专用路由（仅使用 antigravity 账户，不混合调度）
	antigravityV1 := r.Group("/antigravity/v1")
	moderatedAntigravityV1 := NewGatewayPipelineRegistrar(antigravityV1, openAIHTTPPipelineEntrypoints)
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
	if apiKey.Group.Platform == service.PlatformComposite {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	return apiKey.Group.Platform
}

func compositeTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = service.NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
			c.Next()
			return
		}
		if c.Request == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			message := "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
				message = "Request body is too large"
			}
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
			c.Abort()
			return
		}

		model := compositeRequestModelFromBody(c.GetHeader("Content-Type"), body)
		if model != "" {
			decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, compositeRouteEndpointForPath(c.Request.URL.Path))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
				c.Abort()
				return
			}
			if decision.Matched {
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				if upstreamModel := strings.TrimSpace(decision.UpstreamModel); upstreamModel != "" && upstreamModel != model && gjson.ValidBytes(body) {
					if rewritten, rewriteErr := sjson.SetBytes(body, "model", upstreamModel); rewriteErr == nil {
						body = rewritten
					}
				}
			}
		}
		resetRequestBody(c, body)
		c.Next()
	}
}

func compositeRequestModelFromBody(contentType string, body []byte) string {
	if model := strings.TrimSpace(gjson.GetBytes(body, "model").String()); model != "" {
		return model
	}
	return compositeMultipartModelFromBody(contentType, body)
}

func compositeMultipartModelFromBody(contentType string, body []byte) string {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return ""
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return ""
		}
		if err != nil {
			return ""
		}
		if part.FormName() != "model" || part.FileName() != "" {
			continue
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
}

func compositeGeminiTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = service.NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if ok && apiKey != nil && apiKey.Group != nil && apiKey.Group.Platform == service.PlatformComposite {
			model := compositeGeminiModelFromParams(c)
			if model != "" {
				decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, service.CompositeRouteEndpointGemini)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
					c.Abort()
					return
				}
				if decision.Matched {
					c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				}
			}
			if _, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context()); !resolved {
				c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGemini))
			}
		}
		c.Next()
	}
}

func compositeGeminiModelFromParams(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if model := strings.TrimSpace(c.Param("model")); model != "" {
		return model
	}
	modelAction := strings.TrimPrefix(strings.TrimSpace(c.Param("modelAction")), "/")
	if modelAction == "" {
		return ""
	}
	if idx := strings.LastIndex(modelAction, ":"); idx >= 0 {
		return strings.TrimSpace(modelAction[:idx])
	}
	return modelAction
}

func resetRequestBody(c *gin.Context, body []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

func compositeRouteEndpointForPath(path string) string {
	switch {
	case strings.Contains(path, "/messages/count_tokens"):
		return service.CompositeRouteEndpointCountTokens
	case strings.Contains(path, "/messages"):
		return service.CompositeRouteEndpointMessages
	case strings.Contains(path, "/responses"):
		return service.CompositeRouteEndpointResponses
	case strings.Contains(path, "/chat/completions"):
		return service.CompositeRouteEndpointChatCompletions
	case strings.Contains(path, "/embeddings"):
		return service.CompositeRouteEndpointEmbeddings
	case strings.Contains(path, "/images/"):
		return service.CompositeRouteEndpointImages
	case strings.Contains(path, "/v1beta/"):
		return service.CompositeRouteEndpointGemini
	default:
		return service.CompositeRouteEndpointAny
	}
}
