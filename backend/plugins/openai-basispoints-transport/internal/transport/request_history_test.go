package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func completedRequestsForTest(t *testing.T, c *pluginv1.TransportClient, ctx context.Context, count int) []diagnosticEntry {
	t.Helper()
	var entries []diagnosticEntry
	// Receiving End does not wait for the server's finish defer.
	require.Eventually(t, func() bool {
		health, err := c.Health(ctx, &pluginv1.HealthRequest{})
		if err != nil {
			return false
		}
		var state struct {
			Entries []diagnosticEntry `json:"recent_requests"`
		}
		if json.Unmarshal([]byte(health.StatusJson), &state) != nil {
			return false
		}
		entries = state.Entries
		return len(entries) == count
	}, 3*time.Second, 10*time.Millisecond)
	return entries
}

func TestCompletedRequestHistoryKeepsReasonsWithoutPayloads(t *testing.T) {
	p := New()
	p.diagnosticLogger = hclog.NewNullLogger()
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(pluginv1.ClientSessionIDMetadataKey, "client-session"))
	_, d := p.startDiagnostics(ctx, &pluginv1.ForwardRequestStart{RequestId: "req-complete", AccountId: 7})
	d.entry.ConfigRevision = "revision"
	d.body([]byte(`{"model":"test-model","input":"private prompt"}`), true)
	d.route("bps", "selected")
	d.attempt()
	d.retry("invalid_encrypted_content")
	d.route("native", "unsafe_encrypted_replay")
	d.attempt()
	d.upstream(&http.Response{StatusCode: 502, Header: http.Header{"X-Request-Id": []string{"upstream-id"}}})
	d.entry.Tool = &bridge.Diagnostic{}
	require.Empty(t, p.recentRequests.snapshot(), "events and attempts are not completed requests")
	d.finish(nil)
	entries := p.recentRequests.snapshot()
	require.Len(t, entries, 1)
	got := entries[0]
	require.Equal(t, "bps.request_finished", got.Event)
	require.Equal(t, "req-complete", got.RequestID)
	require.Equal(t, "client-session", got.SessionID)
	require.Equal(t, "revision", got.ConfigRevision)
	require.Equal(t, "native", got.Route)
	require.Equal(t, "unsafe_encrypted_replay", got.Reason)
	require.Equal(t, "invalid_encrypted_content", got.RetryReason)
	require.Equal(t, "UPSTREAM_HTTP_ERROR", got.ErrorCode)
	require.Equal(t, 2, got.Attempt)
	require.Equal(t, 1, got.RouteHistory[0].Attempt)
	require.Equal(t, 2, got.RouteHistory[1].Attempt)
	require.NotEmpty(t, got.StartedAt)
	require.NotEmpty(t, got.TraceID)
	require.Nil(t, got.RequestBody)
	require.Nil(t, got.Tool)
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private prompt")
	require.Len(t, p.recentDiagnostics.snapshot(), 1)
	require.Equal(t, "unsafe_encrypted_replay", p.recentDiagnostics.snapshot()[0].Reason)
}

func TestCompletedRequestHistoryIsBoundedAndConcurrent(t *testing.T) {
	p := New()
	p.diagnosticLogger = hclog.NewNullLogger()
	var wg sync.WaitGroup
	for n := 0; n < 150; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: fmt.Sprintf("req-%d", n)})
			d.route("bps", "selected")
			d.finish(nil)
			p.recentRequests.snapshot()
		}(n)
	}
	wg.Wait()
	require.Len(t, p.recentRequests.snapshot(), 100)
	_, last := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: "last"})
	last.route("native", "image_input")
	last.finish(nil)
	last.entry.RouteHistory[0].Reason = "mutated-original"
	snapshot := p.recentRequests.snapshot()
	require.Equal(t, "last", snapshot[99].RequestID)
	require.Equal(t, "image_input", snapshot[99].RouteHistory[0].Reason)
	snapshot[99].RouteHistory[0].Reason = "mutated-snapshot"
	require.Equal(t, "image_input", p.recentRequests.snapshot()[99].RouteHistory[0].Reason)
}

func TestForwardRequestHistoryCorrelatesNativeAndBPS(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("native=%v/stream=%v", native, stream), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ctx = metadata.AppendToOutgoingContext(ctx, pluginv1.ClientSessionIDMetadataKey, "client-session")
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if req.Header.Get(pluginv1.ClientSessionIDMetadataKey) != "" || req.Header.Get("session_id") == "client-session" {
						t.Error("audit identity leaked upstream")
					}
					w.Header().Set("X-Request-ID", "upstream-correlation")
					response := map[string]any{"id": "resp_correlation", "status": "completed", "output": []any{}}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, wireEvent("response.created", map[string]any{"response": response}))
						io.WriteString(w, wireEvent("response.completed", map[string]any{"response": response}))
					} else {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
					}
				}))
				defer upstream.Close()
				c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
				cfg := map[string]any{"upstream_base_url": upstream.URL, "native_upstream_base_url": upstream.URL, "proxy_mode": "disabled"}
				if native {
					cfg["bps_model_mode"] = "selected"
					cfg["bps_models"] = []string{}
				}
				raw, err := json.Marshal(cfg)
				require.NoError(t, err)
				result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
				require.NoError(t, err)
				require.True(t, result.Applied)
				body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "private prompt", "stream": stream})
				_, _, failure := forwardForTest(t, c, body, ctx)
				require.Nil(t, failure)
				d := completedRequestsForTest(t, c, ctx, 1)[0]
				require.Equal(t, "client-session", d.SessionID)
				require.Equal(t, "req_diagnostic_test", d.RequestID)
				require.Equal(t, "resp_correlation", d.ResponseID)
				require.Equal(t, "upstream-correlation", d.UpstreamRequestID)
				require.NotEmpty(t, d.ConfigRevision)
				require.Len(t, d.RouteHistory, 1)
				require.Empty(t, d.ErrorCode)
				require.Nil(t, d.RequestBody)
				if native {
					require.Equal(t, "native", d.Route)
					require.Equal(t, "model_not_selected", d.Reason)
				} else {
					require.Equal(t, "bps", d.Route)
					require.Equal(t, "selected", d.Reason)
				}
			})
		}
	}
}

func TestRequestHistoryPinsConfigurationAcrossHotReload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := New()
	p.diagnosticLogger = hclog.NewNullLogger()
	var nextConfig []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Reload while the old request is still running.
		result, err := p.ApplyConfig(req.Context(), &pluginv1.ApplyConfigRequest{ConfigJson: nextConfig})
		if err != nil || !result.Applied {
			t.Error("reload failed")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"resp_reload","status":"completed","output":[]}`)
	}))
	defer upstream.Close()
	cfg := map[string]any{"upstream_base_url": upstream.URL, "native_upstream_base_url": upstream.URL, "proxy_mode": "disabled"}
	raw, _ := json.Marshal(cfg)
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "", testClientOptions{plugin: p})
	result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
	require.NoError(t, err)
	require.True(t, result.Applied)
	previousRevision := p.state.Load().revision
	cfg["bps_model_mode"] = "selected"
	cfg["bps_models"] = []string{}
	nextConfig, _ = json.Marshal(cfg)
	_, _, failure := forwardForTest(t, c, []byte(`{"model":"test-model","input":"hello"}`), ctx)
	require.Nil(t, failure)
	d := completedRequestsForTest(t, c, ctx, 1)[0]
	require.Equal(t, previousRevision, d.ConfigRevision)
	require.Equal(t, "bps", d.Route)
	policy := routingPolicy(p.state.Load())
	require.NotEqual(t, previousRevision, policy.ConfigRevision)
	require.Equal(t, "selected", policy.BPSModelMode)
	_, _, failure = forwardForTest(t, c, []byte(`{"model":"test-model","input":"hello again"}`), ctx)
	require.Nil(t, failure)
	entries := completedRequestsForTest(t, c, ctx, 2)
	require.Equal(t, policy.ConfigRevision, entries[1].ConfigRevision)
	require.Equal(t, "model_not_selected", entries[1].Reason)
}

func TestConfigurationAuditExposesOnlyRoutingPolicy(t *testing.T) {
	p := New()
	var logs bytes.Buffer
	p.diagnosticLogger = hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
	cfg := []byte(`{"bps_model_mode":"selected","bps_models":["test-model"],"tools_via_native":true,"extra_headers":{"X-Private-Header":"private-config-value"},"upstream_base_url":"https://private-upstream.example/api"}`)
	result, err := p.ApplyConfig(context.Background(), &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, result.Applied)
	var event map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(logs.Bytes(), &event))
	var policy routingPolicySnapshot
	require.NoError(t, json.Unmarshal(event["routing_policy"], &policy))
	require.Equal(t, p.state.Load().revision, policy.ConfigRevision)
	require.Equal(t, []string{"test-model"}, policy.BPSModels)
	require.True(t, policy.ToolsViaNative)
	require.NotContains(t, logs.String(), "private-config-value")
	require.NotContains(t, logs.String(), "private-upstream")
	copy := routingPolicy(p.state.Load())
	copy.BPSModels[0] = "mutated"
	require.Equal(t, "test-model", p.state.Load().cfg.BPSModels[0])
	logs.Reset()
	rejected, err := p.ApplyConfig(context.Background(), &pluginv1.ApplyConfigRequest{ConfigJson: []byte(`{"bps_model_mode":"invalid"}`)})
	require.NoError(t, err)
	require.False(t, rejected.Applied)
	require.Equal(t, policy.ConfigRevision, p.state.Load().revision)
	require.Empty(t, logs.String(), "failed validation must not emit config_applied")
}
