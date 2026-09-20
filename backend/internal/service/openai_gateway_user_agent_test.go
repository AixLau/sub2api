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
	values := map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: codexCLIVersion,
	}
	if value != "" {
		values[SettingKeyOpenAICodexUserAgent] = value
	}
	return NewSettingService(&codexVersionSettingRepoStub{values: values}, &config.Config{})
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
		require.Equal(t, "codex_vscode/"+codexCLIVersion, req.Header.Get("User-Agent"))
		require.Equal(t, "codex_vscode", req.Header.Get("Originator"))
		require.Equal(t, codexCLIVersion, req.Header.Get("Version"))
	})

	t.Run("passthrough", func(t *testing.T) {
		c := newContext()
		svc := &OpenAIGatewayService{settingService: settings}
		req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
		require.NoError(t, err)
		require.Equal(t, "codex_vscode/"+codexCLIVersion, req.Header.Get("User-Agent"))
		require.Equal(t, "codex_vscode", req.Header.Get("Originator"))
		require.Equal(t, codexCLIVersion, req.Header.Get("Version"))
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
		require.Equal(t, "codex_vscode/"+codexCLIVersion, headers.Get("User-Agent"))
		require.Equal(t, "codex_vscode", headers.Get("Originator"))
		require.Equal(t, codexCLIVersion, headers.Get("Version"))
	})
}

func TestOpenAICodexForceCodexCLIUsesCanonicalSystemIdentity(t *testing.T) {
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
	require.Equal(t, "codex_vscode/"+codexCLIVersion, req.Header.Get("User-Agent"))
	require.Equal(t, "codex_vscode", req.Header.Get("Originator"))
	require.Equal(t, codexCLIVersion, req.Header.Get("Version"))
}

// Every gateway transport must apply the global policy after client and account
// headers, including when the old identity enforcement switch is disabled.
func TestOpenAIOutboundUserAgentGlobalPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := codexIdentityEnforcement.Load()
	SetCodexIdentityEnforcementEnabled(false)
	t.Cleanup(func() { SetCodexIdentityEnforcementEnabled(previous) })
	for _, configured := range []string{"", "codex_vscode/0.1.0 (Linux; x86_64) terminal"} {
		settings := newOpenAICodexUASettingService(configured)
		want := resolveOpenAICodexCanonicalUserAgent(context.Background(), settings)
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
			for _, transport := range []string{"http", "passthrough", "websocket"} {
				t.Run(configured+"/"+accountType+"/"+transport, func(t *testing.T) {
					account := &Account{Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{
						"chatgpt_account_id": "chatgpt-acc", "user_agent": "codex-tui/0.2.0 (Mac OS X; arm64)",
						"header_override_enabled": true, "header_overrides": map[string]any{"user-agent": "override/1.0"},
					}}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					c.Request.Header.Set("User-Agent", "client/1.0")
					svc := &OpenAIGatewayService{settingService: settings}
					var headers http.Header
					switch transport {
					case "http":
						req, err := svc.buildUpstreamRequest(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token", false, "", true)
						require.NoError(t, err)
						headers = req.Header
					case "passthrough":
						req, err := svc.buildUpstreamRequestOpenAIPassthrough(c.Request.Context(), c, account, []byte(`{"model":"gpt-5"}`), "token")
						require.NoError(t, err)
						headers = req.Header
					case "websocket":
						var err error
						headers, _, err = svc.buildOpenAIWSHeaders(c.Request.Context(), c, account, "token", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, true, "", "", "", "", "")
						require.NoError(t, err)
					}
					require.Equal(t, want, headers.Get("User-Agent"))
					if account.UsesOpenAICodexProtocol() {
						identity := resolveCodexOutboundIdentityWithCanonicalUA("", want)
						require.Equal(t, identity.originator, headers.Get("Originator"))
						require.Equal(t, identity.version, headers.Get("Version"))
					}
				})
			}
		}
	}
}

func TestApplyOpenAIUpstreamIdentityClearsAllUACasings(t *testing.T) {
	headers := http.Header{"user-agent": {"inbound"}, "USER-AGENT": {"override"}, "User-Agent": {"account"}}
	settings := newOpenAICodexUASettingService("codex_vscode/0.1.0")
	applyOpenAIUpstreamIdentity(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, settings, headers)
	require.Equal(t, http.Header{"User-Agent": {"codex_vscode/" + codexCLIVersion}}, headers)
	for _, platform := range []string{PlatformAnthropic, PlatformGrok, PlatformGemini} {
		original := http.Header{"User-Agent": {"provider/1.0"}}
		applyOpenAIUpstreamIdentity(context.Background(), &Account{Platform: platform}, settings, original)
		require.Equal(t, "provider/1.0", original.Get("User-Agent"))
	}
}

func TestOpenAIAuxiliaryRequestsUseGlobalUserAgent(t *testing.T) {
	const ua = "codex_vscode/0.200.1 (Linux; x86_64) terminal"
	codexCanonicalUAMu.RLock()
	previous := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	SetCodexCanonicalUserAgentResolver(func() string { return ua })
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(previous) })

	quota := buildCodexCommonHeaders("token", "account", false)
	require.Equal(t, ua, quota["user-agent"])
	require.Equal(t, "codex_vscode", quota["originator"])

	auth := make(http.Header)
	ApplyCodexCanonicalAuthIdentity(auth)
	require.Equal(t, ua, auth.Get("User-Agent"))
	require.Equal(t, "codex_vscode", auth.Get("Originator"))
	require.Empty(t, auth.Get("Version"))

	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "key", "user_agent": "account/1.0", "header_override_enabled": true,
		"header_overrides": map[string]any{"user-agent": "override/1.0"},
	}}
	req, err := buildOpenAIAPIKeyModelsRequest(context.Background(), account, func(base string) (string, error) { return base, nil })
	require.NoError(t, err)
	require.Equal(t, ua, req.UserAgent())
	require.Empty(t, req.Header.Get("Originator"))
}

func TestPluginOutboundIdentityUsesGlobalUserAgent(t *testing.T) {
	account := Account{ID: 17, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "test-token", "chatgpt_account_id": "test-account", "user_agent": "account/1.0",
	}}
	settings := newOpenAICodexUASettingService("codex_vscode/0.200.1 (Linux; x86_64) terminal")
	gateway := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}}, settingService: settings}
	identity, err := gateway.ResolvePluginOutboundIdentity(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, resolveOpenAICodexCanonicalUserAgent(context.Background(), settings), identity.Headers.Get("User-Agent"))
	require.Equal(t, "codex_vscode", identity.Headers.Get("Originator"))
	require.Equal(t, "test-token", identity.Token)
	require.Equal(t, "test-account", identity.Headers.Get("Chatgpt-Account-Id"))
}
