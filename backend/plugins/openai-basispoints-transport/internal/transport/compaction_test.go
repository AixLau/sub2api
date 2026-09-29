package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestCompactionBypassesInjectedToolSuite(t *testing.T) {
	testCompactionBypassesInjectedToolSuite(t, "")
}

func testCompactionBypassesInjectedToolSuite(t *testing.T, binary string) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[streaming], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var bpsHits atomic.Int32
			bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				bpsHits.Add(1)
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer bps.Close()
			response := []byte(`{"id":"resp_checkpoint","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Checkpoint: continue the pending work."}]}]}`)
			contentType := "application/json"
			if streaming {
				contentType = "text/event-stream"
				response = []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + string(response) + "}\n\n")
			}
			captured := make(chan []byte, 1)
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "/responses", req.URL.Path)
				captured <- raw
				w.Header().Set("Content-Type", contentType)
				_, _ = w.Write(response)
			}))
			defer native.Close()
			c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
			cfg, err := json.Marshal(map[string]any{"upstream_base_url": bps.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "native_fallback": false})
			require.NoError(t, err)
			applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			body, err := json.Marshal(map[string]any{
				"model": "gpt-6-sol", "stream": streaming, "tool_choice": "auto",
				"client_metadata": map[string]any{"x-codex-turn-metadata": `{"request_kind":"compaction","compaction":{"phase":"mid_turn","trigger":"auto","reason":"context_limit"}}`},
				"input": []any{
					map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{}},
					map[string]any{"type": "reasoning", "encrypted_content": "opaque-history-preserved"},
					map[string]any{"type": "custom_tool_call", "call_id": "call_bps_prior", "name": "exec", "input": "text(await tools.exec_command({cmd:'pwd'}));"},
					map[string]any{"type": "custom_tool_call_output", "call_id": "call_bps_prior", "output": "workspace"},
					map[string]any{"role": "user", "content": "Create a context checkpoint."},
				},
			})
			require.NoError(t, err)
			start, output, failure := forwardForTest(t, c, body, ctx)
			require.Nil(t, failure)
			require.EqualValues(t, http.StatusOK, start.StatusCode)
			require.Equal(t, response, output, "native checkpoint must reach the client unchanged")
			require.Equal(t, body, <-captured, "do not rewrite encrypted history or restore tool declarations")
			require.Zero(t, bpsHits.Load(), "tool-free compaction must never reach the injected BPS tool suite")
			health, err := c.Health(ctx, &pluginv1.HealthRequest{})
			require.NoError(t, err)
			require.Contains(t, health.StatusJson, `"reason":"context_compaction"`)
		})
	}
}

func TestRemoteCompactionV2KeepsToolsAndValidatesCompactionItem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var responseBody atomic.Value
	responseBody.Store(`{"id":"resp_v2","object":"response","status":"completed","output":[{"type":"compaction","encrypted_content":"opaque"}]}`)
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, responseBody.Load().(string))
	}))
	defer native.Close()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, err := json.Marshal(map[string]any{"upstream_base_url": native.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "native_fallback": false})
	require.NoError(t, err)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	body := map[string]any{
		"model": "gpt-6-sol", "stream": false,
		"tools":           []any{map[string]any{"type": "function", "name": "exec_command"}},
		"client_metadata": map[string]any{"x-codex-turn-metadata": `{"request_kind":"compaction"}`},
		"input":           []any{map[string]any{"type": "compaction_trigger"}, map[string]any{"role": "user", "content": "checkpoint"}},
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	_, output, failure := forwardForTest(t, c, raw, ctx)
	require.Nil(t, failure)
	require.Contains(t, string(output), `"type":"compaction"`)

	responseBody.Store(`{"id":"resp_v2","object":"response","status":"completed","output":[{"type":"message","content":[]}]}`)
	_, _, failure = forwardForTest(t, c, raw, ctx)
	require.NotNil(t, failure)
	require.Equal(t, "UPSTREAM_RESPONSE_INVALID", failure.Code)
}

func TestRemoteCompactionV2SSETerminalContract(t *testing.T) {
	item := map[string]any{"type": "compaction", "encrypted_content": "opaque"}
	done := wireEvent("response.output_item.done", map[string]any{"item": item})
	completed := wireEvent("response.completed", map[string]any{"response": map[string]any{"id": "resp_v2", "status": "completed", "output": []any{}}})
	failed := wireEvent("response.failed", map[string]any{"response": map[string]any{"id": "resp_v2", "status": "failed", "error": map[string]any{"code": "context_length_exceeded", "message": "too long"}}})
	for _, tc := range []struct {
		name, response, wantFailure string
		holdOpen, upstreamFailed    bool
	}{
		{name: "completed without HTTP EOF", response: done + completed, holdOpen: true},
		{name: "snapshot without done", response: wireEvent("response.completed", map[string]any{"response": map[string]any{"id": "resp_v2", "output": []any{item}}}), holdOpen: true, wantFailure: "UPSTREAM_RESPONSE_INVALID"},
		{name: "done without completed", response: done, wantFailure: "UPSTREAM_RESPONSE_FAILED"},
		{name: "duplicate done", response: done + done + completed, holdOpen: true, wantFailure: "UPSTREAM_RESPONSE_INVALID"},
		{name: "context limit failure", response: failed, holdOpen: true, upstreamFailed: true},
		{name: "failure before output validation", response: done + done + failed, holdOpen: true, upstreamFailed: true},
		{name: "quota failure", response: wireEvent("error", map[string]any{"error": map[string]any{"code": "insufficient_quota", "message": "quota exhausted"}}), holdOpen: true, upstreamFailed: true},
		{name: "incomplete", response: wireEvent("response.incomplete", map[string]any{"response": map[string]any{"id": "resp_v2", "status": "incomplete", "incomplete_details": map[string]any{"reason": "max_output_tokens"}}}), holdOpen: true, upstreamFailed: true},
		{name: "interrupted", response: wireEvent("response.incomplete", map[string]any{"response": map[string]any{"id": "resp_v2", "status": "incomplete", "incomplete_details": map[string]any{"reason": "interrupted"}}}), holdOpen: true, upstreamFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			release := make(chan struct{})
			requestBody := make(chan []byte, 1)
			upstreamClosed := make(chan struct{})
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				requestBody <- raw
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, tc.response)
				w.(http.Flusher).Flush()
				if tc.holdOpen {
					select {
					case <-r.Context().Done():
					case <-release:
					}
				}
				close(upstreamClosed)
			}))
			defer native.Close()
			defer close(release)
			p := New()
			c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "", testClientOptions{plugin: p})
			cfg, err := json.Marshal(map[string]any{"upstream_base_url": native.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "native_fallback": false, "request_timeout_seconds": 4, "response_idle_timeout_seconds": 2})
			require.NoError(t, err)
			applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			raw := remoteCompactionV2Request(t)
			start, output, failure := forwardForTest(t, c, raw, ctx)
			require.Equal(t, raw, <-requestBody, "the native request must preserve the complete tool catalog")
			if tc.wantFailure != "" {
				require.NotNil(t, failure)
				require.Equal(t, tc.wantFailure, failure.Code)
				require.Nil(t, start)
				require.Empty(t, output, "unvalidated output must stay buffered")
			} else {
				require.Nil(t, failure, "explicit upstream errors must not become plugin validation errors")
				require.NotNil(t, start)
				require.EqualValues(t, http.StatusOK, start.StatusCode)
				require.Equal(t, tc.response, string(output), "preserve original event types, codes and details")
			}
			select {
			case <-upstreamClosed:
			case <-time.After(time.Second):
				t.Fatal("terminal handling must close the upstream response body")
			}
			require.Eventually(t, func() bool { return p.stats.succeeded.Load()+p.stats.failed.Load() == 1 }, time.Second, time.Millisecond)
			if tc.upstreamFailed || tc.wantFailure != "" {
				require.EqualValues(t, 1, p.stats.failed.Load())
				require.Zero(t, p.stats.succeeded.Load())
			} else {
				require.EqualValues(t, 1, p.stats.succeeded.Load())
			}
		})
	}
}

func TestRemoteCompactionV2SSEIdleTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{})
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, _ = io.WriteString(w, ": bps-upstream-activity\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-release:
				return
			case <-ticker.C:
			}
		}
	}))
	defer native.Close()
	defer close(release)
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, err := json.Marshal(map[string]any{"upstream_base_url": native.URL, "native_upstream_base_url": native.URL, "proxy_mode": "disabled", "request_timeout_seconds": 10, "response_idle_timeout_seconds": 1})
	require.NoError(t, err)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	start, output, failure := forwardForTest(t, c, remoteCompactionV2Request(t), ctx)
	require.NotNil(t, failure)
	require.Equal(t, "UPSTREAM_RESPONSE_TIMEOUT", failure.Code)
	require.Nil(t, start)
	require.Empty(t, output, "comments must not bypass compaction validation or extend the event idle budget")
}

func remoteCompactionV2Request(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"model": "gpt-6-sol", "stream": true,
		"tools":           []any{map[string]any{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object"}}},
		"client_metadata": map[string]any{"x-codex-turn-metadata": `{"request_kind":"compaction"}`},
		"input":           []any{map[string]any{"role": "user", "content": "checkpoint"}, map[string]any{"type": "compaction_trigger"}},
	})
	require.NoError(t, err)
	return raw
}
