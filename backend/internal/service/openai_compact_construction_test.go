package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAICompactBuildersKeepBodyAndHeaderPoliciesAligned(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			for _, payload := range []struct{ name, raw string }{
				{name: "minimal", raw: `{"model":"gpt-5.5","input":[]}`},
				{name: "allowed_fields", raw: `{"model":"gpt-5.5","input":[{"role":"user","content":"retain compact input"}],"instructions":"retain instructions","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"parallel_tool_calls":true,"reasoning":{"effort":"high"},"service_tier":"default","text":{"verbosity":"low"},"previous_response_id":"resp_compact","client_metadata":{"trace":"remove before construction"},"stream":true,"store":true,"prompt_cache_key":"remove before construction"}`},
			} {
				t.Run(payload.name, func(t *testing.T) {
					// Both forwarding paths use this existing compact schema before
					// request construction. Headers must not add Responses-only fields back.
					body, _, err := normalizeOpenAICompactRequestBody([]byte(payload.raw))
					require.NoError(t, err)
					require.False(t, gjson.GetBytes(body, "client_metadata").Exists())
					for _, key := range []string{"model", "input", "instructions", "tools", "parallel_tool_calls", "reasoning", "service_tier", "text", "previous_response_id"} {
						if original := gjson.Get(payload.raw, key); original.Exists() {
							require.Equal(t, original.Raw, gjson.GetBytes(body, key).Raw, key)
						}
					}
					account := &Account{ID: 940101, Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{"chatgpt_account_id": "compact-construction-test"}}
					service := &OpenAIGatewayService{}
					requests := map[string]*http.Request{}
					for _, path := range []string{"ordinary", "passthrough"} {
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(body))
						c.Request.Header.Set("session_id", "compact-session")
						c.Request.Header.Set("session-id", "compact-session")
						c.Request.Header.Set("thread-id", "compact-thread")
						c.Request.Header.Set("x-codex-parent-thread-id", "compact-parent")
						c.Request.Header.Set("x-codex-installation-id", "compact-installation")
						c.Request.Header.Set("x-codex-window-id", "compact-window")
						c.Request.Header.Set("x-codex-turn-metadata", `{"session_id":"compact-session","turn_id":"compact-turn"}`)
						c.Request.Header.Set("accept", "text/event-stream")
						c.Request.Header.Set("originator", "unpaired-client")
						c.Request.Header.Set("version", "0.1.0")
						var req *http.Request
						if path == "ordinary" {
							req, err = service.buildUpstreamRequest(context.Background(), c, account, body, "test-token", false, "", true)
						} else {
							req, err = service.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
						}
						require.NoError(t, err)
						requests[path] = req
						t.Run(path+"/body", func(t *testing.T) {
							got, readErr := io.ReadAll(req.Body)
							require.NoError(t, readErr)
							require.NoError(t, req.Body.Close())
							require.False(t, gjson.GetBytes(got, "client_metadata").Exists(), string(got))
							require.Equal(t, body, got, "all allowed compact fields must remain byte-for-byte intact")
							require.Equal(t, int64(len(body)), req.ContentLength)
							require.NotNil(t, req.GetBody)
							replay, replayErr := req.GetBody()
							require.NoError(t, replayErr)
							defer replay.Close()
							replayed, replayErr := io.ReadAll(replay)
							require.NoError(t, replayErr)
							require.Equal(t, body, replayed)
						})
					}
					t.Run("headers", func(t *testing.T) {
						ordinary, passthrough := requests["ordinary"], requests["passthrough"]
						require.Equal(t, chatgptCodexURL+"/compact", ordinary.URL.String())
						require.Equal(t, ordinary.URL.String(), passthrough.URL.String())
						for _, key := range []string{"accept", "user-agent", "originator", "version", "session_id", "session-id", "thread-id", "x-codex-parent-thread-id", "x-codex-installation-id", "x-codex-window-id", "x-codex-turn-metadata"} {
							require.NotEmpty(t, ordinary.Header.Get(key), key)
							require.Equal(t, ordinary.Header.Get(key), passthrough.Header.Get(key), key)
						}
						require.Equal(t, "application/json", passthrough.Header.Get("accept"))
						require.NotEqual(t, "unpaired-client", passthrough.Header.Get("originator"))
					})
				})
			}
		})
	}
}
