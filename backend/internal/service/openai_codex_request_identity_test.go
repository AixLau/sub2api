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

func TestNormalizeCodexOutboundIdentityMapSynchronizesAllCarriers(t *testing.T) {
	headers := http.Header{}
	headers.Set("session-id", "session-header")
	headers.Set("thread-id", "thread-header")
	headers.Set("x-client-request-id", "request-header")
	headers.Set("x-codex-installation-id", "install-header")
	headers.Set("x-codex-window-id", "window-header")
	headers.Set("x-codex-parent-thread-id", "parent-header")
	headers.Set("x-codex-turn-metadata", `{"session_id":"nested-old","sandbox":"seatbelt"}`)
	body := map[string]any{
		"model": "gpt-5.6-codex",
		"client_metadata": map[string]any{
			"session_id": "body-old",
			"trace":      "preserve",
		},
	}

	identity, changed, err := normalizeCodexOutboundIdentityMap(headers, body, "fallback-session")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "session-header", identity.sessionID)
	require.Equal(t, "thread-header", identity.threadID)
	require.Equal(t, "parent-header", identity.parentThreadID)

	clientMetadata, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "preserve", clientMetadata["trace"])
	require.Equal(t, "session-header", clientMetadata["session_id"])
	require.Equal(t, "thread-header", clientMetadata["thread_id"])
	require.Equal(t, "parent-header", clientMetadata["x-codex-parent-thread-id"])
	turnMetadata := clientMetadata[openAIWSTurnMetadataHeader].(string)
	require.Equal(t, "session-header", gjson.Get(turnMetadata, "session_id").String())
	require.Equal(t, "seatbelt", gjson.Get(turnMetadata, "sandbox").String())
	require.Equal(t, "thread-header", headers.Get("thread-id"))
	require.Equal(t, "thread-header", headers.Get("x-client-request-id"))
	require.Equal(t, "session-header", headers.Get("session_id"))
}

func TestNormalizeCodexOutboundIdentityRawPreservesUnrelatedPayload(t *testing.T) {
	headers := http.Header{}
	headers.Set("session-id", "session-raw")
	headers.Set("thread-id", "thread-raw")
	headers.Set("x-codex-parent-thread-id", "parent-raw")
	body := []byte(`{"model":"gpt-5.6-codex","input":[{"type":"input_text","text":"keep"}],"client_metadata":{"trace":"preserve"}}`)

	normalized, identity, changed, err := normalizeCodexOutboundIdentityRaw(headers, body, "fallback")
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "session-raw", identity.sessionID)
	require.Equal(t, "keep", gjson.GetBytes(normalized, "input.0.text").String())
	require.Equal(t, "preserve", gjson.GetBytes(normalized, "client_metadata.trace").String())
	require.Equal(t, "session-raw", gjson.GetBytes(normalized, "client_metadata.session_id").String())
	require.Equal(t, "thread-raw", gjson.GetBytes(normalized, "client_metadata.thread_id").String())
	require.Equal(t, "parent-raw", gjson.GetBytes(normalized, "client_metadata.x-codex-parent-thread-id").String())
	require.Equal(t, "thread-raw", headers.Get("x-client-request-id"))
	require.True(t, bytes.Contains(normalized, []byte(`"trace":"preserve"`)))
}

func TestNormalizeCodexOutboundIdentityMapTrimsCompatibilityMetadata(t *testing.T) {
	headers := http.Header{}
	headers.Set("session-id", "session-map")
	body := map[string]any{"client_metadata": map[string]any{
		"session_id":               "session-map",
		openAIWSTurnMetadataHeader: `{"session_id":"session-map","tool_namespaces_info":{"mcp":{"functions":["keep-in-body"]}},"body_only":"keep"}`,
	}}
	_, changed, err := normalizeCodexOutboundIdentityMap(headers, body, "")
	require.NoError(t, err)
	require.True(t, changed)
	bodyTurn := auditCodexJSON(t, body["client_metadata"].(map[string]any)[openAIWSTurnMetadataHeader].(string))
	headerTurn := auditCodexJSON(t, headers.Get(openAIWSTurnMetadataHeader))
	require.Contains(t, bodyTurn, "tool_namespaces_info")
	require.Equal(t, "keep", bodyTurn["body_only"])
	require.NotContains(t, headerTurn, "tool_namespaces_info")
	require.Equal(t, "keep", headerTurn["body_only"])
}

func TestNormalizeCodexOutboundIdentityRawTrimsCompatibilityMetadata(t *testing.T) {
	headers := http.Header{}
	headers.Set("session-id", "session-raw")
	body := []byte(`{"client_metadata":{"session_id":"session-raw","x-codex-turn-metadata":"{\"session_id\":\"session-raw\",\"tool_namespaces_info\":{\"mcp\":{\"functions\":[\"keep-in-body\"]}},\"body_only\":\"keep\"}"}}`)
	normalized, _, changed, err := normalizeCodexOutboundIdentityRaw(headers, body, "")
	require.NoError(t, err)
	require.True(t, changed)
	bodyTurn := auditCodexJSON(t, gjson.GetBytes(normalized, "client_metadata."+openAIWSTurnMetadataHeader).String())
	headerTurn := auditCodexJSON(t, headers.Get(openAIWSTurnMetadataHeader))
	require.Contains(t, bodyTurn, "tool_namespaces_info")
	require.NotContains(t, headerTurn, "tool_namespaces_info")
}

func TestBuildUpstreamRequestEmitsStandardCodexHeadersAndBodyParity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("session-id", "session-final")
	c.Request.Header.Set("thread-id", "thread-final")
	c.Request.Header.Set("x-client-request-id", "request-stale")
	c.Request.Header.Set("x-codex-parent-thread-id", "parent-final")
	c.Request.Header.Set("x-codex-installation-id", "install-final")
	c.Request.Header.Set("x-codex-window-id", "window-final")

	account := &Account{
		ID:          7001,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "chatgpt-final"},
	}
	body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"session-final","client_metadata":{"session_id":"body-stale","trace":"preserve","x-codex-turn-metadata":"{\"session_id\":\"body-stale\",\"tool_namespaces_info\":{\"mcp\":{\"functions\":[\"keep-in-body\"]}}}"}}`)
	service := &OpenAIGatewayService{}
	req, err := service.buildUpstreamRequest(context.Background(), c, account, body, "token", true, "session-final", true)
	require.NoError(t, err)
	wantSession := scopeCodexAccountIdentityValue(account, 0, "session", "session-final")
	wantThread := scopeCodexAccountIdentityValue(account, 0, "thread", "thread-final")
	wantParent := scopeCodexAccountIdentityValue(account, 0, "thread", "parent-final")
	wantInstall := scopeCodexAccountIdentityValue(account, 0, "installation", "install-final")
	wantWindow := scopeCodexAccountIdentityValue(account, 0, "window", "window-final")
	require.Equal(t, wantSession, req.Header.Get("session-id"))
	require.Equal(t, wantSession, req.Header.Get("session_id"))
	require.Equal(t, wantThread, req.Header.Get("thread-id"))
	require.Equal(t, wantThread, req.Header.Get("x-client-request-id"))
	require.Equal(t, wantParent, req.Header.Get("x-codex-parent-thread-id"))
	require.Equal(t, wantInstall, req.Header.Get("x-codex-installation-id"))
	require.Equal(t, wantWindow, req.Header.Get("x-codex-window-id"))

	upstreamBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, wantSession, gjson.GetBytes(upstreamBody, "client_metadata.session_id").String())
	require.Equal(t, wantThread, gjson.GetBytes(upstreamBody, "client_metadata.thread_id").String())
	require.Equal(t, wantParent, gjson.GetBytes(upstreamBody, "client_metadata.x-codex-parent-thread-id").String())
	require.Equal(t, wantInstall, gjson.GetBytes(upstreamBody, "client_metadata.x-codex-installation-id").String())
	require.Equal(t, wantWindow, gjson.GetBytes(upstreamBody, "client_metadata.x-codex-window-id").String())
	require.Equal(t, "preserve", gjson.GetBytes(upstreamBody, "client_metadata.trace").String())
	require.Contains(t, gjson.GetBytes(upstreamBody, "client_metadata.x-codex-turn-metadata").String(), "tool_namespaces_info")
	require.NotContains(t, req.Header.Get(openAIWSTurnMetadataHeader), "tool_namespaces_info")
}

func TestBuildOpenAIPassthroughEmitsStandardCodexHeadersAndBodyParity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("session-id", "session-passthrough")
	c.Request.Header.Set("thread-id", "thread-passthrough")
	c.Request.Header.Set("x-client-request-id", "request-stale")
	c.Request.Header.Set("x-codex-parent-thread-id", "parent-passthrough")
	body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"session-passthrough","client_metadata":{"trace":"preserve"}}`)
	account := &Account{
		ID:          7002,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "chatgpt-passthrough"},
	}
	service := &OpenAIGatewayService{}
	req, err := service.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
	require.NoError(t, err)
	wantSession := scopeCodexAccountIdentityValue(account, 0, "session", "session-passthrough")
	wantThread := scopeCodexAccountIdentityValue(account, 0, "thread", "thread-passthrough")
	wantParent := scopeCodexAccountIdentityValue(account, 0, "thread", "parent-passthrough")
	require.Equal(t, wantSession, req.Header.Get("session-id"))
	require.Equal(t, wantSession, req.Header.Get("session_id"))
	require.Equal(t, wantThread, req.Header.Get("thread-id"))
	require.Equal(t, wantThread, req.Header.Get("x-client-request-id"))
	require.Equal(t, wantParent, req.Header.Get("x-codex-parent-thread-id"))
	upstreamBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, wantSession, gjson.GetBytes(upstreamBody, "client_metadata.session_id").String())
	require.Equal(t, wantThread, gjson.GetBytes(upstreamBody, "client_metadata.thread_id").String())
	require.Equal(t, wantParent, gjson.GetBytes(upstreamBody, "client_metadata.x-codex-parent-thread-id").String())
	require.Equal(t, "preserve", gjson.GetBytes(upstreamBody, "client_metadata.trace").String())
}
