package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveOpenAICompactSessionIDPrefersCanonicalHyphenHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	c.Request.Header.Set("session-id", "canonical-session")
	c.Request.Header.Set("session_id", "legacy-session")
	c.Request.Header.Set("conversation_id", "conversation-session")

	require.Equal(t, "canonical-session", resolveOpenAICompactSessionID(c))
}
