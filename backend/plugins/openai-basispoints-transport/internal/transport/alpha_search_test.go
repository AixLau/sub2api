package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	pluginconfig "github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/config"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestAlphaSearchNativeForward(t *testing.T) { testAlphaSearchNativeForward(t, "") }

func testAlphaSearchNativeForward(t *testing.T, binary string) {
	for _, path := range []string{"/alpha/search", "/v1/alpha/search", "/backend-api/codex/alpha/search"} {
		for _, override := range []bool{false, true} {
			name := path
			if override {
				name += "/override"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				body := []byte(` {"model":"gpt-6-astra","queries":[{"q":"test query"}],"limit":3} `)
				response := `{"results":[{"url":"https://example.com","title":"search result"}]}`
				var bpsHits, nativeHits atomic.Int32
				bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					bpsHits.Add(1)
					w.WriteHeader(http.StatusTeapot)
				}))
				defer bps.Close()
				native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					nativeHits.Add(1)
					raw, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, body, raw)
					wantPath := path
					if override {
						wantPath = "/native/alpha/search"
					}
					require.Equal(t, wantPath, r.URL.Path)
					require.Equal(t, "feature=standalone&marker=%2F", r.URL.RawQuery)
					require.Equal(t, "Bearer synthetic-search", r.Header.Get("Authorization"))
					require.Equal(t, "synthetic-account", r.Header.Get("Chatgpt-Account-Id"))
					require.Equal(t, "codex-tui/0.150.0", r.Header.Get("User-Agent"))
					require.Equal(t, "codex-tui", r.Header.Get("Originator"))
					require.Equal(t, "synthetic-turn-metadata", r.Header.Get("X-Codex-Turn-Metadata"))
					for _, header := range []string{"OpenAI-Beta", "Session_id", "Conversation_id", "X-Basispoints-Auth-Mode", "X-Openai-Internal-Basispoints-Client-Agent-Profile", "X-Bps-Only", "X-Stainless-Lang"} {
						require.Empty(t, r.Header.Get(header), header)
					}
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Search-Request-ID", "synthetic-search-response")
					io.WriteString(w, response)
				}))
				defer native.Close()
				c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
				config := map[string]any{"upstream_base_url": bps.URL, "proxy_mode": "disabled", "native_fallback": false, "model_mapping": map[string]string{"gpt-6-astra": "must-not-map"}, "extra_headers": map[string]string{"X-Bps-Only": "must-not-inject"}}
				if override {
					config["native_upstream_base_url"] = native.URL + "/native"
				}
				raw, err := json.Marshal(config)
				require.NoError(t, err)
				applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
				require.NoError(t, err)
				require.True(t, applied.Applied)
				start := &pluginv1.ForwardRequestStart{RequestId: "req_search", AccountId: 7, Platform: "openai", AccountType: "oauth", Method: http.MethodPost, Url: native.URL + path + "?feature=standalone&marker=%2F", HasBody: true, ContentLength: int64(len(body)), Headers: map[string]*pluginv1.HeaderValues{
					"Authorization": {Values: []string{"Bearer synthetic-search"}}, "Chatgpt-Account-Id": {Values: []string{"synthetic-account"}}, "User-Agent": {Values: []string{"codex-tui/0.150.0"}}, "Originator": {Values: []string{"codex-tui"}}, "X-Codex-Turn-Metadata": {Values: []string{"synthetic-turn-metadata"}}, "Accept": {Values: []string{"application/json"}},
				}}
				responseStart, output, failure := forwardForTest(t, c, body, ctx, start)
				require.Nil(t, failure)
				require.Equal(t, int32(200), responseStart.StatusCode)
				require.Equal(t, response, string(output))
				require.Equal(t, "synthetic-search-response", headersFromProto(responseStart.Headers).Get("X-Search-Request-ID"))
				require.Zero(t, bpsHits.Load())
				require.Equal(t, int32(1), nativeHits.Load())
			})
		}
	}
}

func TestAlphaSearchPathAllowlist(t *testing.T) {
	for _, path := range []string{"/responses/compact", "/images/generations", "/unverified/alpha/search", "/alpha/search/more", "/alpha/search/"} {
		require.False(t, isAlphaSearchPath(path), path)
	}
}

func TestNativeRequestPreservesResponsesHeaders(t *testing.T) {
	p := New()
	cfg := pluginconfig.Defaults()
	cfg.ExtraHeaders = map[string]string{"X-Bps-Only": "must-not-inject"}
	u, err := url.Parse("https://chatgpt.com/backend-api/codex/responses")
	require.NoError(t, err)
	headers := http.Header{"Authorization": []string{"Bearer synthetic"}, "User-Agent": []string{"host-client/1.0"}, "Openai-Beta": []string{"responses=experimental"}, "X-Codex-Turn-State": []string{"synthetic-state"}}
	start := &pluginv1.ForwardRequestStart{Method: http.MethodPost, HasBody: true, ContentLength: 2, Headers: headersToProto(headers)}
	req, err := p.buildOutboundRequest(context.Background(), start, u, cfg, outboundIdentity{headers: headers}, io.NopCloser(strings.NewReader("{}")))
	require.NoError(t, err)
	require.Equal(t, headers, req.Header)
}

func TestProtectionFailurePreservesUpstreamEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body := []byte(`{"model":"gpt-5.6-sol","input":"synthetic request","stream":true}`)
	wire := wireEvent("response.failed", map[string]any{"response": map[string]any{"id": "resp_protection_test", "status": "failed", "output": []any{}, "error": map[string]string{"code": "upstream_error", "type": "internal_error", "message": "response protection is unavailable"}}})
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, wire)
	}))
	defer upstream.Close()
	var logs lockedLogBuffer
	logger := hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
	p := New()
	p.diagnosticLogger = logger
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "", testClientOptions{plugin: p, logger: logger})
	raw, err := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
	require.NoError(t, err)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	start, out, failure := forwardForTest(t, c, body, ctx)
	require.Nil(t, failure)
	require.Equal(t, int32(502), start.StatusCode)
	require.Equal(t, wire, string(out))
	require.Equal(t, int32(1), hits.Load(), "an unknown protection failure must not trigger speculative retries")
	require.Eventually(t, func() bool { return strings.Contains(logs.text(), "bps.request_finished") }, time.Second, time.Millisecond)
	assertLoggedBody(t, loggedBodyParts(t, logs.text()), body, true)
	entry := p.recentDiagnostics.snapshot()[0]
	require.Equal(t, "bps", entry.Route)
	require.Equal(t, 502, entry.Status)
}
