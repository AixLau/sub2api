//go:build unit

package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountTestService_QueryOpenAIAPIKeyUpstreamUsageCallsSub2APIUsage(t *testing.T) {
	upstream := &queuedHTTPUpstream{responses: []*http.Response{
		newJSONResponse(http.StatusOK, `{"mode":"unrestricted","balance":12.34,"remaining":12.34}`),
	}}
	svc := &AccountTestService{
		httpUpstream: upstream,
		cfg: &config.Config{
			Security: config.SecurityConfig{
				URLAllowlist: config.URLAllowlistConfig{
					Enabled:           false,
					AllowInsecureHTTP: true,
				},
			},
		},
	}
	account := &Account{
		ID:          42,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 3,
		Credentials: map[string]any{
			"api_key":  "sk-upstream",
			"base_url": "http://sub2api.example/v1",
		},
	}

	result, err := svc.QueryOpenAIAPIKeyUpstreamUsage(context.Background(), account)

	require.NoError(t, err)
	require.Equal(t, "unrestricted", result["mode"])
	require.Equal(t, 12.34, result["balance"])
	require.Equal(t, 12.34, result["remaining"])
	require.Len(t, upstream.requests, 1)
	req := upstream.requests[0]
	require.Equal(t, http.MethodGet, req.Method)
	require.Equal(t, "http://sub2api.example/v1/usage", req.URL.String())
	require.Equal(t, "Bearer sk-upstream", req.Header.Get("Authorization"))
	require.Equal(t, "application/json", req.Header.Get("Accept"))
}

func TestAccountTestService_QueryOpenAIAPIKeyUpstreamUsageRejectsNonAPIKeyAccount(t *testing.T) {
	svc := &AccountTestService{}
	_, err := svc.QueryOpenAIAPIKeyUpstreamUsage(context.Background(), &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "Unsupported account type")
}

func TestBuildOpenAIUsageURL(t *testing.T) {
	require.Equal(t, "https://example.com/v1/usage", buildOpenAIUsageURL("https://example.com"))
	require.Equal(t, "https://example.com/v1/usage", buildOpenAIUsageURL("https://example.com/v1"))
	require.Equal(t, "https://example.com/v1/usage", buildOpenAIUsageURL("https://example.com/v1/usage"))
	require.True(t, strings.HasSuffix(buildOpenAIUsageURL("https://example.com/"), "/v1/usage"))
}
