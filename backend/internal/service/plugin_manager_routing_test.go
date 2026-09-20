package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pluginRoutingHTTPUpstream struct {
	doCalls        int
	doWithTLSCalls int
}

func (u *pluginRoutingHTTPUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.doCalls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("legacy")),
	}, nil
}

func (u *pluginRoutingHTTPUpstream) DoWithTLS(
	request *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	u.doWithTLSCalls++
	return u.Do(request, proxyURL, accountID, accountConcurrency)
}

func TestPluginManagerRoutingDoesNotTouchAPIKeyOrOtherProviders(t *testing.T) {
	manager := &PluginManager{}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)

	accounts := []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{ID: 3, Platform: PlatformGemini, Type: AccountTypeOAuth},
	}
	for _, account := range accounts {
		response, handled, routeErr := manager.RoundTripOpenAIOAuth(context.Background(), request, "", account)
		assert.Nil(t, response)
		assert.False(t, handled)
		assert.NoError(t, routeErr)
	}
}

func TestPluginManagerRoutingKeepsOAuthOnLegacyPathWithoutEnabledBinding(t *testing.T) {
	manager := &PluginManager{}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	response, handled, routeErr := manager.RoundTripOpenAIOAuth(context.Background(), request, "", account)

	assert.Nil(t, response)
	assert.False(t, handled)
	assert.NoError(t, routeErr)
}

func TestPluginManagerRoutingSelectsOnlyEligibleOpenAIOAuthAccounts(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{
		pluginID: 1, accountIDs: pluginAccountIDSet([]int64{10, 12}), unavailable: "测试不可用",
	})

	assert.True(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	assert.True(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	assert.False(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
	assert.False(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}))
	assert.False(t, manager.ShouldRouteOpenAIOAuth(&Account{ID: 10, Platform: PlatformGrok, Type: AccountTypeOAuth}))
	assert.False(t, manager.ShouldRouteOpenAIOAuth(nil))
}

func TestOpenAIGatewayPluginRoutingPreservesAPIKeyAndFailsClosedForOAuth(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{
		pluginID: 1, accountIDs: pluginAccountIDSet([]int64{2}), unavailable: "测试不可用",
	})
	upstream := &pluginRoutingHTTPUpstream{}
	service := &OpenAIGatewayService{pluginManager: manager, httpUpstream: upstream}

	apiKeyRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	apiKeyResponse, err := service.doOpenAIUpstream(apiKeyRequest, "", &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, apiKeyResponse)
	_ = apiKeyResponse.Body.Close()
	assert.Equal(t, 1, upstream.doCalls)

	oauthRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	oauthResponse, err := service.doOpenAIUpstream(oauthRequest, "", &Account{
		ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
	})
	require.Error(t, err)
	assert.Nil(t, oauthResponse)
	assert.Contains(t, err.Error(), "插件不可用")
	assert.Equal(t, 1, upstream.doCalls)

	nativeOAuthRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	nativeOAuthResponse, err := service.doOpenAIUpstream(nativeOAuthRequest, "", &Account{
		ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
	})
	require.NoError(t, err)
	require.NotNil(t, nativeOAuthResponse)
	_ = nativeOAuthResponse.Body.Close()
	assert.Equal(t, 2, upstream.doCalls, "未选中的 OAuth 账号必须使用原生传输")
}

func TestNormalizePluginAccountIDsRequiresSelectionAndCanonicalizes(t *testing.T) {
	_, err := normalizePluginAccountIDs(nil)
	require.ErrorContains(t, err, "至少选择一个")

	accountIDs, err := normalizePluginAccountIDs([]int64{9, 2, 9, 5})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 5, 9}, accountIDs)

	_, err = normalizePluginAccountIDs([]int64{1, 0})
	require.ErrorContains(t, err, "无效账号 ID")
}

func TestOpenAIGatewayForwardPathsRespectPluginBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"native", "passthrough", "ws-http-bridge", "messages", "chat"} {
		t.Run(mode, func(t *testing.T) {
			manager := &PluginManager{}
			manager.route.Store(&pluginRoute{
				pluginID: 1, accountIDs: pluginAccountIDSet([]int64{71}), unavailable: "plugin-review-unavailable",
			})
			upstream := &pluginRoutingHTTPUpstream{}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, pluginManager: manager, httpUpstream: upstream}
			account := &Account{
				ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "test-token"},
				Extra:       map[string]any{"openai_passthrough": mode == "passthrough"},
			}
			body := []byte(`{"model":"gpt-5.5","instructions":"Help with coding.","input":"hello","stream":true}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			var err error
			switch mode {
			case "ws-http-bridge":
				_, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", body, len(body), "gpt-5.5", "", "", "", "", 1, func([]byte) error { return nil })
			case "messages":
				body = []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
				_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "gpt-5.5")
			case "chat":
				body = []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}]}`)
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			default:
				_, err = svc.Forward(context.Background(), c, account, body)
			}
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, http.StatusBadGateway, failover.StatusCode)
			events, exists := c.Get(OpsUpstreamErrorsKey)
			require.True(t, exists)
			upstreamEvents, ok := events.([]*OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.Len(t, upstreamEvents, 1)
			require.Contains(t, upstreamEvents[0].Message, "plugin-review-unavailable")
			require.Zero(t, upstream.doCalls, "an enabled plugin binding must not be bypassed by a forwarding mode")
		})
	}
}
