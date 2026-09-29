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
