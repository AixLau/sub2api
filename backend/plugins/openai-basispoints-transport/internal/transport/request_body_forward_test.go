package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
)

func TestForwardFailedRequestBodies(t *testing.T) { testForwardFailedRequestBodies(t, "") }

func testForwardFailedRequestBodies(t *testing.T, binary string) {
	body := []byte(` {"model":"test-model","input":"original_body_marker"} `)
	large, err := json.Marshal(map[string]any{"model": "test-model", "input": strings.Repeat("中文/body\n", 8000)})
	require.NoError(t, err)
	failedJSON := `{"status":"failed","error":{"message":"rejected"}}`
	failedStream := wireEvent("response.failed", map[string]any{"response": map[string]any{"status": "failed"}})
	for _, tc := range []struct {
		name                  string
		native                bool
		status                int
		contentType, response string
		body                  []byte
		failed                bool
	}{
		{"http_error_large_bps", false, 400, "application/json", failedJSON, large, true},
		{"http_error_native", true, 503, "application/json", failedJSON, body, true},
		{"semantic_json_bps", false, 200, "application/json", failedJSON, body, true},
		{"semantic_json_native", true, 200, "application/json", failedJSON, body, true},
		{"semantic_stream_bps", false, 200, "text/event-stream", failedStream, body, true},
		{"semantic_stream_native", true, 200, "text/event-stream", failedStream, body, true},
		{"read_error_bps", false, 200, "application/json", failedJSON, body, true},
		{"read_error_native", true, 200, "text/event-stream", failedStream, body, true},
		{"connection_error", false, 0, "", "", body, true},
		{"invalid_request", false, 200, "application/json", `{"status":"completed","output":[]}`, []byte(` {"input": `), true},
		{"success_bps", false, 200, "application/json", `{"status":"completed","output":[]}`, body, false},
		{"success_native", true, 200, "text/event-stream", wireEvent("response.completed", map[string]any{"response": map[string]any{"status": "completed"}}), body, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			var logs lockedLogBuffer
			logger := hclog.New(&hclog.LoggerOptions{JSONFormat: true, Output: &logs, Level: hclog.Info})
			p := New()
			p.diagnosticLogger = logger
			c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary, testClientOptions{plugin: p, logger: logger})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if tc.status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return
				}
				if strings.HasPrefix(tc.name, "read_error") {
					w.Header().Set("Content-Length", "100000")
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			cfg := map[string]any{"upstream_base_url": upstream.URL, "native_upstream_base_url": upstream.URL, "proxy_mode": "disabled"}
			if tc.native {
				cfg["bps_model_mode"] = "selected"
				cfg["bps_models"] = []string{}
			}
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: raw})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			_, out, failure := forwardForTest(t, c, tc.body, ctx)
			if strings.HasPrefix(tc.name, "read_error") || tc.status == 0 {
				require.NotNil(t, failure)
			} else if tc.native || tc.status >= 400 {
				require.Nil(t, failure)
				require.Equal(t, tc.response, string(out))
			}
			require.Eventually(t, func() bool { return strings.Contains(logs.text(), "bps.request_finished") }, 3*time.Second, 10*time.Millisecond)
			parts := loggedBodyParts(t, logs.text())
			health, err := c.Health(ctx, &pluginv1.HealthRequest{})
			require.NoError(t, err)
			var state struct {
				Entries []diagnosticEntry `json:"recent_diagnostics"`
			}
			require.NoError(t, json.Unmarshal([]byte(health.StatusJson), &state))
			if tc.failed {
				require.NotContains(t, string(out), "original_body_marker")
				assertLoggedBody(t, parts, tc.body, true)
				require.Len(t, state.Entries, 1)
				require.Equal(t, parts[0].Body, state.Entries[0].RequestBody.Preview)
				require.Equal(t, parts[0].TraceID, state.Entries[0].TraceID)
				if tc.status >= 400 {
					require.Equal(t, "UPSTREAM_HTTP_ERROR", state.Entries[0].ErrorCode)
				}
			} else {
				require.Empty(t, parts)
				require.Empty(t, state.Entries)
				require.NotContains(t, logs.text(), "original_body_marker")
			}
			for _, header := range []string{"Bearer synthetic", "synthetic-account", "isolated-session"} {
				require.NotContains(t, logs.text(), header)
			}
		})
	}
}
