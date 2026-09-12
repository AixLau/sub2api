package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexIdentityAudit_OAuthWSReuseKeepsHandshakeAndPerTurnMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1

	captureConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_oauth_a","model":"gpt-5.2"}}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_oauth_b","model":"gpt-5.2"}}`),
	}}
	captureDialer := &openAIWSCaptureDialer{conn: captureConn}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(captureDialer)
	t.Cleanup(pool.Close)

	svc := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}
	account := newTestOAuthAccount(4406, map[string]any{
		"responses_websockets_v2_enabled": true,
	})
	account.Name = "oauth-ws-identity-audit"
	account.Status = StatusActive
	account.Schedulable = true
	account.Concurrency = 1
	account.Credentials = map[string]any{
		"access_token":       "oauth-token",
		"chatgpt_account_id": "chatgpt-oauth-audit",
	}

	makeContext := func() *gin.Context {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
		c.Request.Header.Set("originator", "codex_cli_rs")
		c.Request.Header.Set("session-id", "client-session")
		c.Request.Header.Set("thread-id", "client-thread")
		c.Request.Header.Set("x-codex-turn-metadata", `{"installation_id":"client-install","session_id":"client-session","thread_id":"client-thread","turn_id":"compatibility-default","turn_started_at_unix_ms":1}`)
		return c
	}
	body := func(turnID string, startedAt int64) []byte {
		return []byte(`{"model":"gpt-5.2","stream":true,"prompt_cache_key":"client-session","client_metadata":{"session_id":"client-session","thread_id":"client-thread","x-codex-turn-metadata":"{\"installation_id\":\"client-install\",\"session_id\":\"client-session\",\"thread_id\":\"client-thread\",\"turn_id\":\"` + turnID + `\",\"turn_started_at_unix_ms\":` + formatIntForTest(startedAt) + `,\"tool_namespaces_info\":{\"mcp\":{\"functions\":[\"` + turnID + `\"]}},\"body_only\":\"` + turnID + `\"}"},"input":[{"type":"input_text","text":"hi"}]}`)
	}

	firstContext := makeContext()
	firstResult, err := svc.Forward(context.Background(), firstContext, account, body("turn-1", 1000))
	require.NoError(t, err)
	require.Equal(t, "resp_oauth_a", firstResult.RequestID)
	secondContext := makeContext()
	secondResult, err := svc.Forward(context.Background(), secondContext, account, body("turn-2", 2000))
	require.NoError(t, err)
	require.Equal(t, "resp_oauth_b", secondResult.RequestID)
	require.Nil(t, svc.httpUpstream.(*httpUpstreamRecorder).lastReq, "must reach forwardOpenAIWSV2")

	require.Equal(t, 1, captureDialer.DialCount(), "同一 OAuth namespace/session 应复用同一握手")
	wantSession := isolateOpenAIUpstreamSessionID(0, account, "client-session")
	wantThread := scopeCodexAccountIdentityValue(account, 0, "thread", "client-thread")
	require.Equal(t, wantSession, captureDialer.lastHeaders.Get("session-id"))
	require.Equal(t, wantThread, captureDialer.lastHeaders.Get("thread-id"))
	require.Len(t, captureConn.writes, 2)
	firstTurn := gjson.Parse(gjson.Get(requestToJSONString(captureConn.writes[0]), "client_metadata.x-codex-turn-metadata").String())
	secondTurn := gjson.Parse(gjson.Get(requestToJSONString(captureConn.writes[1]), "client_metadata.x-codex-turn-metadata").String())
	require.Equal(t, "turn-1", firstTurn.Get("tool_namespaces_info.mcp.functions.0").String())
	require.Equal(t, "turn-2", secondTurn.Get("tool_namespaces_info.mcp.functions.0").String())
	require.Equal(t, "turn-1", firstTurn.Get("body_only").String())
	require.Equal(t, "turn-2", secondTurn.Get("body_only").String())
	require.Equal(t, scopeCodexAccountIdentityValue(account, 0, "turn", "turn-1"), firstTurn.Get("turn_id").String())
	require.Equal(t, float64(1000), firstTurn.Get("turn_started_at_unix_ms").Float())
	require.Equal(t, scopeCodexAccountIdentityValue(account, 0, "turn", "turn-2"), secondTurn.Get("turn_id").String())
	require.Equal(t, float64(2000), secondTurn.Get("turn_started_at_unix_ms").Float())
	handshakeTurn := gjson.Parse(captureDialer.lastHeaders.Get(openAIWSTurnMetadataHeader))
	require.Equal(t, scopeCodexAccountIdentityValue(account, 0, "turn", "turn-1"), handshakeTurn.Get("turn_id").String())
	require.Equal(t, float64(1000), handshakeTurn.Get("turn_started_at_unix_ms").Float())
	require.False(t, handshakeTurn.Get("tool_namespaces_info").Exists())
}

func formatIntForTest(value int64) string {
	return strconv.FormatInt(value, 10)
}
