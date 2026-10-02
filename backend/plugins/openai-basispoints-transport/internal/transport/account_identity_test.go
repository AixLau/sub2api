package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestForwardAccountClientIdentityIsRequestScoped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	seen := make(chan http.Header, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"id\":\"resp_ok\",\"status\":\"completed\",\"output\":[]}"))
	}))
	defer upstream.Close()
	client := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, err := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	require.NoError(t, err)
	applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	body := []byte("{\"model\":\"gpt-6-astra\",\"input\":\"hello\",\"stream\":false}")
	// A -> B -> refreshed A rotates account credentials while keeping BPS UA fixed.
	for i, accountID := range []int64{7, 9, 7} {
		ua := fmt.Sprintf("captured-browser/%d", i)
		md, err := pluginv1.AccountClientHeadersMetadata(http.Header{"User-Agent": {ua}, "X-OpenAI-Account-User-Id": {fmt.Sprintf("user-%d", accountID)}})
		require.NoError(t, err)
		account := fmt.Sprintf("chatgpt-%d", accountID)
		token := fmt.Sprintf("Bearer token-%d", i)
		start := &pluginv1.ForwardRequestStart{AccountId: accountID, Platform: "openai", AccountType: "oauth", Method: "POST", Url: "https://chatgpt.com/backend-api/codex/responses", HasBody: true, ContentLength: int64(len(body)), Headers: map[string]*pluginv1.HeaderValues{
			"Authorization": {Values: []string{token}}, "Chatgpt-Account-Id": {Values: []string{account}},
			"User-Agent": {Values: []string{"native-codex/1.0"}}, "Cookie": {Values: []string{"native-cookie"}},
			pluginv1.AccountClientHeadersMetadataKey: {Values: []string{"forged-http-snapshot"}},
		}}
		response, _, failure := forwardForTest(t, client, body, metadata.NewOutgoingContext(ctx, md), start)
		require.Nil(t, failure)
		require.EqualValues(t, 200, response.StatusCode)
		h := <-seen
		require.Equal(t, token, h.Get("Authorization"))
		require.Equal(t, account, h.Get("Chatgpt-Account-Id"))
		require.Equal(t, account, h.Get("X-OpenAI-Account-Id"))
		require.Equal(t, bpsWebViewUserAgent, h.Get("User-Agent"))
		require.Equal(t, fmt.Sprintf("user-%d", accountID), h.Get("X-OpenAI-Account-User-Id"))
		require.Empty(t, h.Get("Cookie"))
		require.Empty(t, h.Get(pluginv1.AccountClientHeadersMetadataKey))
	}
}

type identityProbeHost struct {
	pluginv1.UnimplementedHostServiceServer
	requests chan int64
}

func (s *identityProbeHost) ResolveOutboundIdentity(ctx context.Context, req *pluginv1.ResolveOutboundIdentityRequest) (*pluginv1.ResolveOutboundIdentityResponse, error) {
	s.requests <- req.AccountId
	md, err := pluginv1.AccountClientHeadersMetadata(http.Header{"User-Agent": {"probe-captured-browser"}, "X-OpenAI-Account-User-Id": {"account-user"}})
	if err != nil {
		return nil, err
	}
	if err := grpc.SetHeader(ctx, md); err != nil {
		return nil, err
	}
	return &pluginv1.ResolveOutboundIdentityResponse{
		Found: true, AccountId: req.AccountId, Token: "fresh-probe-token",
		Headers: map[string]*pluginv1.HeaderValues{
			"Chatgpt-Account-Id": {Values: []string{"actual-chatgpt-account"}},
			"User-Agent":         {Values: []string{"codex-native"}},
		},
	}, nil
}

func TestBPS403ProbeUsesResolvedAccountIdentity(t *testing.T) {
	seen := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Clone()
		w.WriteHeader(http.StatusForbidden)
	}))
	defer upstream.Close()
	p := New()
	host := &identityProbeHost{requests: make(chan int64, 1)}
	client := clientForTest(t, host, "", testClientOptions{plugin: p})
	cfg, err := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	require.NoError(t, err)
	applied, err := client.ApplyConfig(context.Background(), &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	require.False(t, p.bps403Probe(7))
	require.EqualValues(t, 7, <-host.requests)
	h := <-seen
	require.Equal(t, "Bearer fresh-probe-token", h.Get("Authorization"))
	require.Equal(t, "actual-chatgpt-account", h.Get("Chatgpt-Account-Id"))
	require.Equal(t, "actual-chatgpt-account", h.Get("X-OpenAI-Account-Id"))
	require.Equal(t, "account-user", h.Get("X-OpenAI-Account-User-Id"))
	require.Equal(t, bpsWebViewUserAgent, h.Get("User-Agent"))
	require.Equal(t, "chatgpt", h.Get("X-Basispoints-Auth-Mode"))
}
