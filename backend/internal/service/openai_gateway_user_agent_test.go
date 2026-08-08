package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenAICodexUASettingService(value string) *SettingService {
	values := map[string]string{}
	if value != "" {
		values[SettingKeyOpenAICodexUserAgent] = value
	}
	return NewSettingService(&betaPolicySettingRepoStub{values: values}, &config.Config{})
}

func TestResolveOpenAICodexUpstreamUserAgentPrecedence(t *testing.T) {
	ctx := context.Background()
	settings := newOpenAICodexUASettingService("system-codex/1.0")

	t.Run("account custom user agent wins", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"user_agent": "account-codex/2.0"}}
		require.Equal(t, "account-codex/2.0", resolveOpenAICodexUpstreamUserAgent(ctx, account, settings))
	})

	t.Run("system setting is used without account custom value", func(t *testing.T) {
		require.Equal(t, "system-codex/1.0", resolveOpenAICodexUpstreamUserAgent(ctx, &Account{}, settings))
	})

	t.Run("hardcoded default is the final fallback", func(t *testing.T) {
		require.Equal(t, DefaultOpenAICodexUserAgent, resolveOpenAICodexUpstreamUserAgent(ctx, &Account{}, nil))
	})
}

func TestOpenAICodexOAuthUpstreamRequestsIgnoreClientUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := newOpenAICodexUASettingService("codex_vscode/1.0")
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "chatgpt-acc"},
	}

	newContext := func() *gin.Context {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")
		return c
	}

	t.Run("regular http", func(t *testing.T) {
		c := newContext()
		svc := &OpenAIGatewayService{settingService: settings}
		req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token", false, "", true)
		require.NoError(t, err)
		require.Equal(t, "codex_vscode/1.0", req.Header.Get("User-Agent"))
		require.Equal(t, "codex_vscode", req.Header.Get("Originator"))
	})

	t.Run("passthrough", func(t *testing.T) {
		c := newContext()
		svc := &OpenAIGatewayService{settingService: settings}
		req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
		require.NoError(t, err)
		require.Equal(t, "codex_vscode/1.0", req.Header.Get("User-Agent"))
		require.Equal(t, "codex_vscode", req.Header.Get("Originator"))
	})

	t.Run("websocket", func(t *testing.T) {
		c := newContext()
		svc := &OpenAIGatewayService{settingService: settings}
		headers, _, err := svc.buildOpenAIWSHeaders(
			c.Request.Context(),
			c,
			account,
			"token",
			OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
			true,
			"",
			"",
			"",
			"",
			"",
		)
		require.NoError(t, err)
		require.Equal(t, "codex_vscode/1.0", headers.Get("User-Agent"))
		require.Equal(t, "codex_vscode", headers.Get("Originator"))
	})
}

func TestOpenAICodexAccountUserAgentWinsEvenWhenForceCodexCLIEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.1.0")

	settings := newOpenAICodexUASettingService("codex_vscode/1.0")
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "chatgpt-acc",
			"user_agent":         "codex-tui/2.0",
		},
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: true}}
	svc := &OpenAIGatewayService{cfg: cfg, settingService: settings}

	req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
	require.NoError(t, err)
	require.Equal(t, "codex-tui/2.0", req.Header.Get("User-Agent"))
	require.Equal(t, "codex-tui", req.Header.Get("Originator"))
}
