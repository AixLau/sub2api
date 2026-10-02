package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCloudMintAndProxyPool(t *testing.T) {
	c, normalized, err := Parse([]byte(`{
        "enabled": true,
        "harvest_proxy_mode": "pool",
        "harvest_proxy_pool": ["socks5h://a.example:1080", "socks5h://a.example:1080", "http://b.example:8080"],
        "cloud_mint": {
            "enabled": true,
            "url": "http://127.0.0.1:8317/mint",
            "proxy_env": "CPA_MINT_PROXY",
            "key_env": "CPA_RELAY_KEY",
            "transport": "websocket",
            "gateway": "unified-88",
            "ticket_length": 780,
            "ttl_seconds": 300,
            "wait_ms": 1000,
            "timeout_ms": 5000,
            "fail_closed": true,
            "mint_model": "gpt-6-astra"
        }
    }`))
	require.NoError(t, err)
	require.Len(t, c.HarvestProxyPool, 2)
	require.Equal(t, "websocket", c.CloudMint.Transport)
	require.Equal(t, "CPA_MINT_PROXY", c.CloudMint.ProxyEnv)
	require.NotEmpty(t, normalized)
}

func TestPoolModeRequiresProxies(t *testing.T) {
	_, _, err := Parse([]byte(`{"harvest_proxy_mode":"pool"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "harvest_proxy_pool")
}

func TestRouteCookieLifetimeAndRoleDefaults(t *testing.T) {
	c := Defaults()
	require.Equal(t, "business", c.Role)
	require.Equal(t, 3900, c.RouteCookieTTLSeconds)
	require.True(t, c.LogDecisions)

	parsed, _, err := Parse([]byte(`{"role":"probe","dry_run":true,"route_cookie_ttl_seconds":7200}`))
	require.NoError(t, err)
	require.Equal(t, "probe", parsed.Role)
	require.True(t, parsed.DryRun)
	require.Equal(t, 7200, parsed.RouteCookieTTLSeconds)
}

func TestParseKeepsBooleanDefaultsWhenFieldsAreOmitted(t *testing.T) {
	c, _, err := Parse([]byte(`{"cloud_mint":{"enabled":true,"url":"http://127.0.0.1:8317/mint"}}`))
	require.NoError(t, err)
	require.True(t, c.LogDecisions)
	require.True(t, c.CookieValidation)
	require.True(t, c.GatewayValidation)
	require.True(t, c.CloudMint.FailClosed)
	require.True(t, c.CloudMint.Enabled)
}

func TestCloudMintRejectsPoolProxy(t *testing.T) {
	_, _, err := Parse([]byte(`{"cloud_mint":{"enabled":true,"url":"https://relay.example/","proxy_url":"ip-pool"}}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "ip-pool")
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	_, _, err := Parse([]byte(`{} {}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple JSON values")
}
