package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForwardToolFeedback(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, replySSE := range []bool{false, true} {
			for _, mode := range []string{"repair", "repeated", "http_failure", "truncated", "upstream_failed", "upstream_incomplete"} {
				t.Run(fmt.Sprintf("stream=%v/replySSE=%v/%s", stream, replySSE, mode), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					var hits atomic.Int32
					var observed [][]byte
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						raw, _ := io.ReadAll(req.Body)
						observed = append(observed, raw)
						attempt := hits.Add(1)
						if attempt == 2 && mode == "http_failure" {
							w.WriteHeader(500)
							io.WriteString(w, `{"error":{"code":"server_error","message":"An error occurred while processing"}}`)
							return
						}
						if attempt == 2 && mode == "truncated" {
							w.Header().Set("Content-Type", "text/event-stream")
							io.WriteString(w, wireEvent("response.created", map[string]any{"response": map[string]any{"id": "resp_partial"}}))
							return
						}
						name := "unknown_client_target"
						suffix := "bad"
						if attempt == 2 && mode == "repair" {
							name = "get_weather"
							suffix = "fixed"
						}
						envelope, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{"city": "Tokyo"}})
						outer, _ := json.Marshal(map[string]any{"references": []string{"A1"}, "code": string(envelope)})
						item := map[string]any{"type": "function_call", "id": "fc_" + suffix, "call_id": "call_" + suffix, "name": "run_officejs", "arguments": string(outer)}
						response := map[string]any{"id": "resp_" + suffix, "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 9, "output_tokens": 2, "total_tokens": 11}}
						terminal := "response.completed"
						if attempt == 2 && (mode == "upstream_failed" || mode == "upstream_incomplete") {
							status := strings.TrimPrefix(mode, "upstream_")
							response["status"], response["output"] = status, []any{}
							response["error"] = map[string]any{"code": "server_error", "message": "upstream failure"}
							terminal = "response." + status
						}
						if (attempt == 1 && stream) || (attempt == 2 && replySSE) {
							w.Header().Set("Content-Type", "text/event-stream")
							io.WriteString(w, wireEvent("response.output_item.done", map[string]any{"output_index": 0, "item": item}))
							io.WriteString(w, wireEvent(terminal, map[string]any{"response": response}))
							w.(http.Flusher).Flush()
							if attempt == 2 {
								<-req.Context().Done()
							}
						} else {
							w.Header().Set("Content-Type", "application/json")
							json.NewEncoder(w).Encode(response)
						}
					}))
					defer upstream.Close()
					c := recoveryClient(t, ctx, upstream.URL)
					body, _ := json.Marshal(map[string]any{"input": "weather", "stream": stream, "tools": []any{map[string]any{"type": "function", "name": "get_weather"}}})
					_, out, rpcErr := forwardForTest(t, c, body, ctx)
					require.Nil(t, rpcErr, "tool feedback failure must be semantic, never trigger account retry")
					require.Equal(t, int32(2), hits.Load(), "one feedback round, no blind retry")
					response := terminalResponse(t, out, stream)
					if stream || mode == "repair" {
						require.Equal(t, `"resp_bad"`, string(response["id"]))
					}
					if mode == "repair" {
						require.Equal(t, `"completed"`, string(response["status"]))
						require.NotContains(t, string(out), "unknown_client_target")
						require.Contains(t, string(out), "get_weather")
					} else {
						if stream {
							require.Equal(t, `"failed"`, string(response["status"]))
						}
						require.Contains(t, string(out), "TOOL_BRIDGE_CALL_INVALID")
						if mode != "repeated" {
							require.Contains(t, string(out), "upstream_tool_feedback")
						}
						require.NotContains(t, string(out), "response.function_call_arguments.delta")
					}
					if mode == "repair" || (stream && mode != "http_failure" && mode != "truncated") {
						var usage map[string]int
						require.NoError(t, json.Unmarshal(response["usage"], &usage))
						require.Equal(t, 22, usage["total_tokens"])
					}
					require.Len(t, observed, 2)
					var continuation struct {
						Input    []map[string]json.RawMessage
						Metadata map[string]string
					}
					require.NoError(t, json.Unmarshal(observed[1], &continuation))
					require.Equal(t, "1", continuation.Metadata["agent_iteration"])
					last := continuation.Input[len(continuation.Input)-1]
					require.Equal(t, `"function_call_output"`, string(last["type"]))
					require.Equal(t, `"call_bad"`, string(last["call_id"]))
					require.Contains(t, string(last["output"]), "TOOL_BRIDGE_CONVERSION_FAILED")
					entries := completedRequestsForTest(t, c, ctx, 1)
					require.Equal(t, "bps", entries[0].Route)
					require.Equal(t, 2, entries[0].Attempt)
					if mode == "repair" {
						require.Empty(t, entries[0].ErrorCode)
					}
				})
			}
		}
	}
}

func TestFeedbackReaderRequiresTerminalEvent(t *testing.T) {
	for _, wire := range []string{"data: [DONE]\n\n", "data: invalid\n\n", wireEvent("error", map[string]any{})} {
		resp := &http.Response{Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
		_, err := readFeedbackResponse(context.Background(), resp)
		require.Error(t, err)
	}
}

func TestFeedbackReaderRejectsNonterminalJSON(t *testing.T) {
	for _, raw := range []string{`null`, `{`, `{"status":"in_progress","output":[]}`, `{"status":"completed","output":{}}`, `{"status":"completed"}`} {
		resp := &http.Response{Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}
		_, err := readFeedbackResponse(context.Background(), resp)
		require.Error(t, err)
	}
}
