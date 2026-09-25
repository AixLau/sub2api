package transport

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
)

// upstreamResponseReadErrorCode keeps the plugin protocol useful to the host
// without exposing proxy URLs, authorization details, or Go transport text.
func upstreamResponseReadErrorCode(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	var toolErr *bridge.ToolCallError
	if errors.As(err, &toolErr) {
		return "TOOL_BRIDGE_CALL_INVALID"
	}
	if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
		return "UPSTREAM_RESPONSE_TIMEOUT"
	}
	return fallback
}

func upstreamResponseReadErrorMessage(err error) string {
	if err == nil {
		return "读取上游响应失败"
	}
	var toolErr *bridge.ToolCallError
	if errors.As(err, &toolErr) {
		return toolErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) || isNetTimeout(err) {
		return "读取上游响应超时"
	}
	// Bridge errors use stable, already-sanitized Chinese prefixes. Preserve
	// that context while avoiding raw net/http errors that can contain URLs.
	message := strings.TrimSpace(err.Error())
	if strings.HasPrefix(message, "上游 SSE ") {
		return message
	}
	return "读取上游响应失败"
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
