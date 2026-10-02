package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// observeOpenAIPluginMetadata bridges ticket metadata embedded in a response
// event into the existing response-header observer. The Codex protocol has
// emitted these fields in HTTP/SSE bodies as well as after a WebSocket
// handshake; keeping extraction in the host preserves the plugin's
// no-body/no-transport contract while still harvesting the ticket and route
// cookies.
func (s *OpenAIGatewayService) observeOpenAIPluginMetadata(ctx context.Context, account *Account, model, transport string, message []byte, sentTicketState string) {
	if s == nil || s.pluginManager == nil || account == nil || len(message) == 0 {
		return
	}
	headers := extractCodexWSMetadataHeaders(message)
	if len(headers) == 0 {
		return
	}
	s.pluginManager.ObserveOpenAIOutboundResponse(ctx, account, strings.TrimSpace(model), transport, &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     headers,
	}, sentTicketState)
}

func (s *OpenAIGatewayService) observeOpenAIPluginWSMetadata(ctx context.Context, account *Account, model string, message []byte, sentTicketState string) {
	s.observeOpenAIPluginMetadata(ctx, account, model, "websocket", message, sentTicketState)
}

func (s *OpenAIGatewayService) observeOpenAIPluginWSHandshake(ctx context.Context, account *Account, model string, headers http.Header, sentTicketState string) {
	if s == nil || s.pluginManager == nil || account == nil || len(headers) == 0 {
		return
	}
	s.pluginManager.ObserveOpenAIOutboundResponse(ctx, account, strings.TrimSpace(model), "websocket", &http.Response{
		StatusCode: http.StatusSwitchingProtocols,
		Status:     "101 Switching Protocols",
		Header:     headers.Clone(),
	}, sentTicketState)
}

func (s *OpenAIGatewayService) observeOpenAIPluginSSEMetadata(ctx context.Context, account *Account, model string, message []byte, sentTicketState string) {
	s.observeOpenAIPluginMetadata(ctx, account, model, "sse", message, sentTicketState)
}

func preferredOpenAIPluginModel(original, mapped string) string {
	if model := strings.TrimSpace(original); model != "" {
		return model
	}
	return strings.TrimSpace(mapped)
}

func sentOpenAICodexTurnState(resp *http.Response, c *gin.Context) string {
	if resp != nil && resp.Request != nil {
		if state := strings.TrimSpace(resp.Request.Header.Get(openAIWSTurnStateHeader)); state != "" {
			return state
		}
	}
	if c != nil && c.Request != nil {
		return strings.TrimSpace(c.Request.Header.Get(openAIWSTurnStateHeader))
	}
	return ""
}

func extractCodexWSMetadataHeaders(message []byte) http.Header {
	var root any
	if json.Unmarshal(message, &root) != nil {
		return nil
	}
	out := make(http.Header)
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 6 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			for key, raw := range node {
				if strings.EqualFold(key, "x-codex-turn-state") || strings.EqualFold(key, "set-cookie") {
					for _, item := range metadataHeaderValues(raw) {
						if strings.EqualFold(key, "x-codex-turn-state") {
							out.Set("x-codex-turn-state", item)
						} else {
							out.Add("Set-Cookie", item)
						}
					}
					continue
				}
				if strings.EqualFold(key, "headers") || strings.EqualFold(key, "response") || strings.EqualFold(key, "metadata") || strings.EqualFold(key, "codex.response.metadata") || strings.EqualFold(key, "data") || strings.EqualFold(key, "event") || strings.EqualFold(key, "payload") || strings.EqualFold(key, "response_metadata") {
					walk(raw, depth+1)
				}
			}
		case []any:
			for _, item := range node {
				walk(item, depth+1)
			}
		}
	}
	walk(root, 0)
	if out.Get("x-codex-turn-state") == "" && len(out.Values("Set-Cookie")) == 0 {
		return nil
	}
	return out
}

func metadataHeaderValues(value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return []string{typed}
		}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	}
	return nil
}

// observeOpenAIPluginSemantic forwards one bounded model declaration to the
// Codex ticket hook. It returns a semantic transport error only when the hook
// explicitly enables degraded-model blocking; observer outages remain
// fail-open and never alter the upstream response.
func (s *OpenAIGatewayService) observeOpenAIPluginSemantic(ctx context.Context, account *Account, requestedModel, transport, servedModel, eventType string, complete bool, sentTicketState ...string) error {
	if s == nil || s.pluginManager == nil || account == nil || account.Type != AccountTypeOAuth || account.Platform != PlatformOpenAI {
		return nil
	}
	servedModel = strings.TrimSpace(servedModel)
	if servedModel == "" {
		return nil
	}
	blocked, message := s.pluginManager.ObserveOpenAIOutboundSemantic(ctx, account, strings.TrimSpace(requestedModel), transport, servedModel, eventType, complete, sentTicketState...)
	if !blocked {
		return nil
	}
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("upstream served %s instead of requested %s", servedModel, strings.TrimSpace(requestedModel))
	}
	return &PluginTransportError{Code: "DEGRADED_MODEL", Message: message, RequestSent: true}
}

// writeOpenAIPluginDegradedResponse is used by non-streaming handlers, where
// no SSE frame exists to carry the semantic rejection. It marks the response
// committed so outer failover/error plumbing cannot append a second payload.
func writeOpenAIPluginDegradedResponse(c *gin.Context, err error) {
	if c == nil || IsResponseCommitted(c) {
		return
	}
	var pluginErr *PluginTransportError
	if !errors.As(err, &pluginErr) || pluginErr == nil || pluginErr.Code != "DEGRADED_MODEL" {
		return
	}
	MarkResponseCommitted(c)
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusBadRequest, gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    pluginErr.Code,
			"message": pluginErr.Message,
		},
	})
}
