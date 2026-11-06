//go:build unit

package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// attachGatewayRouteForTest mirrors what the production router does before a
// GatewayHandler runs: it attaches the moderated route metadata (the Gateway
// pre-forward pipeline refuses to bill, route or forward without it) and the
// allow receipt issued by the route-level moderation stage, which handler-level
// tests do not mount. The route is derived from the request path.
func attachGatewayRouteForTest(c *gin.Context) {
	path := c.Request.URL.Path
	var routePath, handlerName, protocol string
	switch {
	case strings.Contains(path, "/v1beta/models/"):
		routePath, handlerName, protocol = "/v1beta/models/*modelAction", "GatewayHandler.GeminiV1BetaModels", service.ContentModerationProtocolGemini
		if strings.HasPrefix(path, "/antigravity/") {
			routePath = "/antigravity/v1beta/models/*modelAction"
		}
	case strings.HasSuffix(path, "/chat/completions"):
		routePath, handlerName, protocol = "/v1/chat/completions", "GatewayHandler.ChatCompletions", service.ContentModerationProtocolOpenAIChat
	case strings.HasSuffix(path, "/responses"):
		routePath, handlerName, protocol = "/v1/responses", "GatewayHandler.Responses", service.ContentModerationProtocolOpenAIResponses
	case strings.HasSuffix(path, "/messages"):
		routePath, handlerName, protocol = "/v1/messages", "GatewayHandler.Messages", service.ContentModerationProtocolAnthropicMessages
		if strings.HasPrefix(path, "/antigravity/") {
			routePath = "/antigravity/v1/messages"
		}
	default:
		panic("attachGatewayRouteForTest: unsupported path " + path)
	}
	moderationcoverage.SetRouteMeta(c, moderationcoverage.AnnotatePipelineCoverage(moderationcoverage.Entry{
		Method: http.MethodPost, Path: routePath, Handler: handlerName,
		Upstream: true, ModerationRequired: true, Protocol: protocol,
		Status: moderationcoverage.StatusCovered,
	}))
	markForwardableModerationReceipt(c, protocol)
}
