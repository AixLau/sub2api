package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexSessionIdentityTestStore struct {
	GatewayCache
	mu     sync.Mutex
	values map[string]string
	sets   int
}

func (s *codexSessionIdentityTestStore) GetCodexSessionIdentity(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", ErrCodexSessionIdentityNotFound
	}
	return value, nil
}

func (s *codexSessionIdentityTestStore) SetCodexSessionIdentityIfAbsent(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[key]; ok {
		return false, nil
	}
	s.values[key] = value
	s.sets++
	return true, nil
}

func (s *codexSessionIdentityTestStore) CompareAndSwapCodexSessionIdentity(_ context.Context, key, expected, value string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values[key] != expected {
		return false, nil
	}
	s.values[key] = value
	return true, nil
}

func newCodexSessionIdentityTestContext(t *testing.T, userID, apiKeyID int64) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: apiKeyID, UserID: userID})
	return c
}

func newCodexUUIDv7ForTest(t *testing.T) string {
	t.Helper()
	value, err := uuid.NewV7()
	require.NoError(t, err)
	return value.String()
}

func TestResolveCodexMappedSessionIdentityUsesDurableUUIDv7Mapping(t *testing.T) {
	store := &codexSessionIdentityTestStore{values: make(map[string]string)}
	account := &Account{ID: 7101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "upstream-7101"}}
	c := newCodexSessionIdentityTestContext(t, 41, 51)
	service := &OpenAIGatewayService{cache: store}
	raw := newCodexUUIDv7ForTest(t)
	started := time.Now()

	first, err := service.resolveCodexMappedSessionIdentity(context.Background(), c, account, raw)
	require.NoError(t, err)
	second, err := service.resolveCodexMappedSessionIdentity(context.Background(), c, account, raw)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.True(t, isCodexUUIDv7(first))
	require.Equal(t, 1, store.sets)
	parsed, err := uuid.Parse(first)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), parsed.Version())
	sec, _ := parsed.Time().UnixTime()
	require.InDelta(t, started.Unix(), sec, 2)
}

func TestResolveCodexMappedSessionIdentityScopesUserAndAccount(t *testing.T) {
	store := &codexSessionIdentityTestStore{values: make(map[string]string)}
	service := &OpenAIGatewayService{cache: store}
	raw := newCodexUUIDv7ForTest(t)
	accountA := &Account{ID: 7201, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "upstream-a"}}
	accountB := &Account{ID: 7202, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "upstream-b"}}
	a, err := service.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityTestContext(t, 1, 11), accountA, raw)
	require.NoError(t, err)
	b, err := service.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityTestContext(t, 2, 22), accountA, raw)
	require.NoError(t, err)
	c, err := service.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityTestContext(t, 1, 11), accountB, raw)
	require.NoError(t, err)
	require.NotEqual(t, a, b)
	require.NotEqual(t, a, c)
	require.Equal(t, 3, store.sets)
}

func TestResolveCodexMappedSessionIdentityFailsClosedWithoutStore(t *testing.T) {
	account := &Account{ID: 7301, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "upstream-7301"}}
	service := &OpenAIGatewayService{}
	_, err := service.resolveCodexMappedSessionIdentity(context.Background(), newCodexSessionIdentityTestContext(t, 3, 33), account, newCodexUUIDv7ForTest(t))
	require.ErrorIs(t, err, ErrCodexSessionIdentityStoreUnavailable)
}

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
	wantSession := isolateOpenAIUpstreamSessionID(0, account, "session-final")
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

func TestBuildUpstreamRequestAutoConversationFollowsMappedSession(t *testing.T) {
	for _, mode := range []string{CodexSessionIdentityMappingLegacy, CodexSessionIdentityMappingV2} {
		t.Run(mode, func(t *testing.T) {
			store := &codexSessionIdentityTestStore{values: make(map[string]string)}
			service := &OpenAIGatewayService{cache: store}
			service.cfg = &config.Config{Gateway: config.GatewayConfig{CodexSessionIdentityMapping: mode}}
			account := &Account{ID: 7501, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "chatgpt-conversation"}}
			raw := newCodexUUIDv7ForTest(t)
			body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"` + raw + `","client_metadata":{"session_id":"` + raw + `"}}`)
			build := func(userID, apiKeyID int64, acc *Account, conversationID string) *http.Request {
				c := newCodexSessionIdentityTestContext(t, userID, apiKeyID)
				c.Request.Header.Set("session-id", raw)
				if conversationID != "" {
					c.Request.Header.Set("conversation_id", conversationID)
				}
				req, err := service.buildUpstreamRequest(context.Background(), c, acc, body, "token", true, raw, true)
				require.NoError(t, err)
				return req
			}
			for _, incoming := range []struct{ name, value string }{
				{name: "absent"},
				{name: "same_as_session", value: raw},
				{name: "different_from_session", value: "independent-conversation"},
			} {
				t.Run(incoming.name, func(t *testing.T) {
					first := build(75, 751, account, incoming.value)
					retry := build(75, 751, account, incoming.value)
					mapped := first.Header.Get("session-id")
					require.NotEmpty(t, mapped)
					require.Equal(t, mapped, first.Header.Get("session_id"))
					require.Equal(t, mapped, first.Header.Get("conversation_id"), "ordinary HTTP rebuilds conversation from session even when the client supplied it")
					require.Equal(t, mapped, retry.Header.Get("session-id"))
					require.Equal(t, mapped, retry.Header.Get("conversation_id"))
					require.NotEqual(t, raw, first.Header.Get("conversation_id"))
					if mode == CodexSessionIdentityMappingV2 {
						require.True(t, isCodexUUIDv7(mapped))
					} else {
						require.Equal(t, isolateOpenAIUpstreamSessionID(751, account, raw), mapped)
					}
					upstreamBody, err := io.ReadAll(first.Body)
					require.NoError(t, err)
					require.Equal(t, mapped, gjson.GetBytes(upstreamBody, "client_metadata.session_id").String())
					require.Equal(t, mapped, gjson.GetBytes(upstreamBody, "prompt_cache_key").String())
					otherUser := build(76, 752, account, incoming.value)
					otherAccount := build(75, 751, codexSessionIdentityV2Account("chatgpt-conversation-other"), incoming.value)
					for _, other := range []*http.Request{otherUser, otherAccount} {
						require.Equal(t, other.Header.Get("session-id"), other.Header.Get("conversation_id"))
						require.NotEqual(t, mapped, other.Header.Get("conversation_id"))
						require.NotEqual(t, raw, other.Header.Get("conversation_id"))
					}
				})
			}
		})
	}
}

func TestBuildOpenAIPassthroughPreservesExplicitConversationIsolation(t *testing.T) {
	store := &codexSessionIdentityTestStore{values: make(map[string]string)}
	service := &OpenAIGatewayService{cache: store}
	account := codexSessionIdentityV2Account("chatgpt-explicit-conversation")
	rawSession := newCodexUUIDv7ForTest(t)
	rawConversation := newCodexUUIDv7ForTest(t)
	c := newCodexSessionIdentityTestContext(t, 75, 751)
	c.Request.Header.Set("session-id", rawSession)
	c.Request.Header.Set("conversation_id", rawConversation)
	body := []byte(`{"model":"gpt-5.6-codex","stream":true,"prompt_cache_key":"` + rawSession + `","client_metadata":{"session_id":"` + rawSession + `"}}`)
	req, err := service.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "token")
	require.NoError(t, err)
	require.Equal(t, isolateOpenAIUpstreamSessionID(751, account, rawConversation), req.Header.Get("conversation_id"))
	require.NotEqual(t, req.Header.Get("session-id"), req.Header.Get("conversation_id"))
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
	wantSession := isolateOpenAIUpstreamSessionID(0, account, "session-passthrough")
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

func TestBuildOpenAIPassthroughUsesCanonicalSessionHeaderWithoutPromptCacheKey(t *testing.T) {
	store := &codexSessionIdentityTestStore{values: make(map[string]string)}
	service := &OpenAIGatewayService{cache: store}
	account := &Account{
		ID:          7401,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "chatgpt-passthrough-session"},
	}
	raw := newCodexUUIDv7ForTest(t)
	c := newCodexSessionIdentityTestContext(t, 74, 741)
	c.Request.Header.Set("session-id", raw)

	req, err := service.buildUpstreamRequestOpenAIPassthrough(
		context.Background(), c, account,
		[]byte(`{"model":"gpt-5.6-codex","stream":true}`),
		"token",
	)
	require.NoError(t, err)
	mapped := req.Header.Get("session-id")
	require.True(t, isCodexUUIDv7(mapped))
	require.NotEqual(t, raw, mapped)
	require.Equal(t, mapped, req.Header.Get("session_id"))
}
