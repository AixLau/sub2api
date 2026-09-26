package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

const openAISSEPingEvent = "event: ping\ndata: {\"type\":\"ping\"}\n\n"

// Codex measures idle time between parsed SSE events, not network reads. SSE
// comments are discarded by eventsource-stream and cannot reset that timer.
// A ping carries transport liveness without claiming model progress, exposing
// attempt-local response IDs, or committing a tool call. Other clients retain
// standard comment keepalives instead of receiving a Responses extension.
func openAISSEKeepalivePayload(c *gin.Context, comment string) string {
	if c != nil && c.Request != nil &&
		openai.IsCodexOfficialClientByHeaders(c.GetHeader("User-Agent"), c.GetHeader("originator")) {
		return openAISSEPingEvent
	}
	return comment
}
