package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

type lockedLogBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *lockedLogBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

func TestForwardDiagnosticLogs(t *testing.T) { testForwardDiagnosticLogs(t, "") }

func testForwardDiagnosticLogs(t *testing.T, binary string) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("diagnostics/stream=%v", stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ctx = metadata.AppendToOutgoingContext(ctx, pluginv1.ClientSessionIDMetadataKey, "client-diagnostic-session")
			var logs lockedLogBuffer
			logger := hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
			p := New()
			p.diagnosticLogger = logger
			c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary, testClientOptions{plugin: p, logger: logger})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Set("X-Request-ID", "upstream_diag_request")
				item := map[string]any{"type": "function_call", "id": "private_item_id", "call_id": "private_call_id", "name": "functions.run_officejs", "namespace": "functions",
					"arguments": map[string]any{"references": []string{"client-tool:get_weather"}, "summary": "private_summary", "code": `{"city":"private\'city"}`}}
				response := map[string]any{"id": "resp_logs", "status": "completed", "output": []any{item}}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, wireEvent("response.output_item.done", map[string]any{"output_index": 0, "item": item}))
					io.WriteString(w, wireEvent("response.completed", map[string]any{"response": response}))
				} else {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}))
			defer upstream.Close()
			cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
			result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, result.Applied)
			body, _ := json.Marshal(map[string]any{"model": "test-model", "input": "private_prompt", "stream": stream, "tools": []any{map[string]any{"type": "function", "name": "get_weather", "description": "private_schema"}}})
			_, out, failure := forwardForTest(t, c, body, ctx)
			require.Nil(t, failure)
			require.Contains(t, string(out), "TOOL_BRIDGE_CALL_INVALID")
			health, err := c.Health(ctx, &pluginv1.HealthRequest{})
			require.NoError(t, err)
			var state struct {
				Entries []diagnosticEntry `json:"recent_diagnostics"`
			}
			require.NoError(t, json.Unmarshal([]byte(health.StatusJson), &state))
			require.Len(t, state.Entries, 3) // rejection, feedback rejection, final failure
			d := state.Entries[0]
			require.Equal(t, "req_diagnostic_test", d.RequestID)
			require.Equal(t, "client-diagnostic-session", d.SessionID)
			require.Equal(t, "selected", d.Reason)
			require.NotEmpty(t, d.ConfigRevision)
			require.Equal(t, "upstream_diag_request", d.UpstreamRequestID)
			require.Equal(t, "bps.tool_rejected", d.Event)
			require.Equal(t, "upstream_tool_code", d.Tool.Stage)
			require.Positive(t, d.Tool.JSONOffset)
			require.Equal(t, "get_weather", d.Tool.TargetTool) // routing is independent of payload syntax
			require.Equal(t, "test-model", d.Model)
			require.Equal(t, stream, d.Stream)
			require.Equal(t, 1, d.Attempt)
			// Packaged processes deliver stderr asynchronously through go-plugin.
			require.Eventually(t, func() bool { return strings.Contains(logs.text(), "bps.request_finished") }, 3*time.Second, 10*time.Millisecond)
			text := logs.text()
			require.Equal(t, 2, strings.Count(text, `"@message":"bps.tool_rejected"`))
			require.Contains(t, text, `"request_id":"req_diagnostic_test"`)
			require.Contains(t, text, `"session_id":"client-diagnostic-session"`)
			require.Contains(t, text, `"stage":"upstream_tool_code"`)
			require.Contains(t, text, "private_prompt")
			require.Contains(t, text, "private_schema")
			require.Equal(t, string(body), d.RequestBody.Preview)
			require.True(t, d.RequestBody.Complete)
			for _, secret := range []string{"private_summary", "private_call_id", "private_item_id", "private\\'city", "Bearer synthetic", "synthetic-account", "isolated-session"} {
				require.NotContains(t, text, secret)
				require.NotContains(t, health.StatusJson, secret)
			}
		})
	}
}

func TestDiagnosticRingBoundedAndConcurrent(t *testing.T) {
	p := New()
	p.diagnosticLogger = hclog.NewNullLogger()
	var wg sync.WaitGroup
	for n := 0; n < 80; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: fmt.Sprintf("req-%d", n)})
			d.fail("TOOL_BRIDGE_REQUEST_INVALID")
			p.recentDiagnostics.snapshot()
		}(n)
	}
	wg.Wait()
	require.Len(t, p.recentDiagnostics.snapshot(), recentDiagnosticLimit)
	p.recentDiagnostics.add(diagnosticEntry{RequestID: "last"})
	snapshot := p.recentDiagnostics.snapshot()
	require.Equal(t, "last", snapshot[len(snapshot)-1].RequestID)
	snapshot[0].RequestID = "mutated"
	require.NotEqual(t, "mutated", p.recentDiagnostics.snapshot()[0].RequestID)
}

func TestDiagnosticCorrelationFieldsAreSanitized(t *testing.T) {
	p := New()
	p.diagnosticLogger = hclog.NewNullLogger()
	ctx, d := p.startDiagnostics(context.Background(), &pluginv1.ForwardRequestStart{RequestId: "private\nrequest"})
	d.body([]byte(`{"model":"private/url?token=secret"}`), true)
	d.upstream(&http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{strings.Repeat("s", 129)}}})
	diagnosticsFrom(ctx).fail("TOOL_BRIDGE_CALL_INVALID")
	entries := p.recentDiagnostics.snapshot()
	require.Equal(t, `<redacted>`, entries[0].Model)
	// The user explicitly requested the body. Identity fields remain redacted;
	// body previews preserve the submitted bytes independently.
	entries[0].RequestBody = nil
	raw, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private")
	require.NotContains(t, string(raw), "secret")
	require.Contains(t, string(raw), "redacted")
}
