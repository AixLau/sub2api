package transport

import (
	"context"
	"encoding/json"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func recoveryClient(t *testing.T, ctx context.Context, upstream string) *pluginv1.TransportClient {
	t.Helper()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream, "proxy_mode": "disabled"})
	result, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, result.Applied)
	return c
}

func wireEvent(typ string, fields map[string]any) string {
	fields["type"] = typ
	raw, _ := json.Marshal(fields)
	return "event: " + typ + "\ndata: " + string(raw) + "\n\n"
}

func TestEncryptedSSERecoveryBoundaries(t *testing.T) {
	for _, mode := range []string{"recover", "repeated", "after_output", "opaque"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var hits atomic.Int32
			var mu sync.Mutex
			var bodies []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				raw, _ := io.ReadAll(req.Body)
				mu.Lock()
				bodies = append(bodies, string(raw))
				mu.Unlock()
				attempt := hits.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == "recover" && attempt == 2 {
					io.WriteString(w, wireEvent("response.completed", map[string]any{"response": map[string]any{"id": "resp_recovered", "output": []any{}, "usage": map[string]int{"total_tokens": 9}}}))
					w.(http.Flusher).Flush()
					<-req.Context().Done()
					return
				}
				io.WriteString(w, wireEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_failed_attempt", "output": []any{}}}))
				if mode == "after_output" {
					io.WriteString(w, wireEvent("response.output_text.delta", map[string]any{"delta": "already delivered"}))
				}
				io.WriteString(w, wireEvent("response.failed", map[string]any{"response": map[string]any{"id": "resp_failed_attempt", "status": "failed", "output": []any{}, "error": map[string]string{"code": "invalid_encrypted_content", "message": "Encrypted function output content could not be decrypted or decoded."}}}))
			}))
			defer upstream.Close()
			c := recoveryClient(t, ctx, upstream.URL)
			input := []any{map[string]any{"role": "user", "content": "hi"}, map[string]any{"type": "reasoning", "encrypted_content": "foreign"}}
			if mode == "opaque" {
				input = append(input, map[string]any{"type": "compaction", "encrypted_content": "only-copy"})
			}
			body, _ := json.Marshal(map[string]any{"input": input, "stream": true})
			_, out, failure := forwardForTest(t, c, body, ctx)
			if mode == "opaque" {
				require.NotNil(t, failure)
				require.Equal(t, "TOOL_BRIDGE_ENCRYPTED_REPLAY_UNSAFE", failure.Code)
				require.Equal(t, int32(1), hits.Load())
				return
			}
			require.Nil(t, failure)
			if mode == "after_output" {
				require.Equal(t, int32(1), hits.Load())
				require.Contains(t, string(out), "already delivered")
			} else {
				require.Equal(t, int32(2), hits.Load())
			}
			if mode == "recover" {
				require.Contains(t, string(out), "resp_recovered")
				require.Contains(t, string(out), "total_tokens")
				require.NotContains(t, string(out), "resp_failed_attempt")
				mu.Lock()
				defer mu.Unlock()
				require.Contains(t, bodies[0], "foreign")
				require.NotContains(t, bodies[1], "foreign")
				var first, second map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(bodies[0]), &first))
				require.NoError(t, json.Unmarshal([]byte(bodies[1]), &second))
				require.JSONEq(t, string(first["metadata"]), string(second["metadata"]))
			} else {
				require.Contains(t, string(out), "invalid_encrypted_content")
			}
		})
	}
}

func TestInvalidToolCallReturnsDiagnosticWithoutExecutableOutput(t *testing.T) {
	for _, stream := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		item := map[string]any{"type": "function_call", "name": "run_officejs", "id": "fc_bad", "call_id": "call_bad", "arguments": map[string]any{"code": "malformed JSON"}}
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			response := map[string]any{"status": "completed", "output": []any{item}}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, wireEvent("response.completed", map[string]any{"response": response}))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		c := recoveryClient(t, ctx, upstream.URL)
		_, out, failure := forwardForTest(t, c, []byte("{\"input\":\"hi\"}"), ctx)
		require.NotNil(t, failure)
		require.Equal(t, "TOOL_BRIDGE_CALL_INVALID", failure.Code)
		require.Contains(t, failure.Message, "JSON")
		require.True(t, failure.RequestSent)
		require.NotContains(t, string(out), "run_officejs")
		upstream.Close()
	}
}
