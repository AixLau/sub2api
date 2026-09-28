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

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestForwardToolFeedback(t *testing.T) {
	testForwardToolFeedback(t, "")
}

func testForwardToolFeedback(t *testing.T, binary string) {
	for _, stream := range []bool{false, true} {
		for _, replySSE := range []bool{false, true} {
			for _, mode := range []string{"repair", "repeated", "http_failure", "bps_forbidden", "truncated", "upstream_failed", "upstream_incomplete", "http_rate_limit", "sse_error", "flat_sse_error", "proxy_html",
				"capability_repair", "capability_repeated", "capability_http_failure", "capability_upstream_failed"} {
				t.Run(fmt.Sprintf("stream=%v/replySSE=%v/%s", stream, replySSE, mode), func(t *testing.T) {
					capability := strings.HasPrefix(mode, "capability_")
					mode := strings.TrimPrefix(mode, "capability_")
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					var hits atomic.Int32
					var observed [][]byte
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						raw, _ := io.ReadAll(req.Body)
						observed = append(observed, raw)
						attempt := hits.Add(1)
						if attempt == 2 && (mode == "http_failure" || mode == "bps_forbidden" || mode == "http_rate_limit" || mode == "proxy_html") {
							code := http.StatusInternalServerError
							if mode == "http_rate_limit" {
								w.Header().Set("Retry-After", "1")
								w.WriteHeader(http.StatusTooManyRequests)
								io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","type":"tokens","message":"TPM limit"}}`)
								return
							}
							if mode == "proxy_html" {
								w.WriteHeader(http.StatusBadGateway)
								io.WriteString(w, "<html>private proxy debug body</html>")
								return
							}
							if mode == "bps_forbidden" {
								code = http.StatusForbidden
							}
							w.WriteHeader(code)
							io.WriteString(w, `{"error":{"code":"server_error","message":"An error occurred while processing"}}`)
							return
						}
						if attempt == 2 && (mode == "sse_error" || mode == "flat_sse_error") {
							w.Header().Set("Content-Type", "text/event-stream")
							w.Header().Set("Retry-After-Ms", "231")
							detail := map[string]any{"code": "rate_limit_exceeded", "message": "TPM limit"}
							if mode == "sse_error" {
								detail = map[string]any{"error": detail}
							}
							io.WriteString(w, wireEvent("error", detail))
							w.(http.Flusher).Flush()
							<-req.Context().Done()
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
						payload, _ := json.Marshal(map[string]any{"city": "Tokyo"})
						outer, _ := json.Marshal(map[string]any{"references": []string{"client-tool:" + name}, "code": string(payload)})
						item := map[string]any{"type": "function_call", "id": "fc_" + suffix, "call_id": "call_" + suffix, "name": "run_officejs", "arguments": string(outer)}
						if capability && suffix == "bad" {
							item["name"] = "write_range"
							item["arguments"] = `{"sheetId":"private_sheet","writes":[{"cell":"A1","value":"private_value"}]}`
						}
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
					c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
					cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
					applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
					require.NoError(t, err)
					require.True(t, applied.Applied)
					body, _ := json.Marshal(map[string]any{"input": "weather", "stream": stream, "tools": []any{map[string]any{"type": "function", "name": "get_weather"}}})
					_, out, rpcErr := forwardForTest(t, c, body, ctx)
					require.Nil(t, rpcErr, "tool feedback failure must be semantic, not an RPC disconnect")
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
						status := `"failed"`
						if mode == "upstream_incomplete" {
							status = `"incomplete"`
						}
						if stream || (mode != "repeated" && mode != "truncated") {
							require.Equal(t, status, string(response["status"]))
							require.JSONEq(t, `[]`, string(response["output"]))
						}
						code := "TOOL_BRIDGE_CALL_INVALID"
						switch mode {
						case "http_failure", "bps_forbidden", "upstream_failed", "upstream_incomplete":
							code = "server_error"
						case "http_rate_limit", "sse_error", "flat_sse_error":
							code = "rate_limit_exceeded"
							require.Contains(t, string(out), "retry-after")
						case "proxy_html":
							code = "upstream_error"
							require.NotContains(t, string(out), "private proxy")
						}
						if capability && mode == "repeated" {
							code = "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE"
						}
						require.Contains(t, string(out), code)
						if mode == "truncated" {
							require.Contains(t, string(out), "upstream_tool_feedback")
							require.Contains(t, string(out), "缺少终结事件")
						}
						require.NotContains(t, string(out), "response.function_call_arguments.delta")
					}
					if mode == "repair" || mode == "upstream_failed" || mode == "upstream_incomplete" || (stream && mode == "repeated") {
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
					feedbackCode := "TOOL_BRIDGE_CONVERSION_FAILED"
					if capability {
						feedbackCode = "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE"
					}
					require.Contains(t, string(last["output"]), feedbackCode)
					require.NotContains(t, string(out), "private_value")
					if mode == "bps_forbidden" {
						health, err := c.Health(ctx, &pluginv1.HealthRequest{})
						require.NoError(t, err)
						var status struct {
							Blocked map[string]string `json:"bps_403_blocked_accounts"`
						}
						require.NoError(t, json.Unmarshal([]byte(health.StatusJson), &status))
						require.NotEmpty(t, status.Blocked["7"])
					}
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

func TestFeedbackReaderPreservesUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"http", "application/json", `{"error":{"code":"rate_limit_exceeded","type":"tokens","message":"TPM limit","param":null}}`, 429},
		{"failed_json", "application/json", `{"status":"failed","output":[],"error":{"code":"rate_limit_exceeded","type":"tokens","message":"TPM limit","param":null}}`, 200},
		{"failed_sse", "text/event-stream", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"output\":[],\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"TPM limit\",\"param\":null}}}\n\n", 200},
		{"http_sse", "text/event-stream", "data: {\"type\":\"error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"TPM limit\",\"param\":null}}\n\n", 429},
		{"nested_sse", "text/event-stream", "data: {\"type\":\"error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"tokens\",\"message\":\"TPM limit\",\"param\":null}}\n\n", 200},
		{"flat_sse", "text/event-stream", "data: {\"type\":\"error\",\"code\":\"rate_limit_exceeded\",\"message\":\"TPM limit\",\"param\":null,\"sequence_number\":7}\n\n", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}, "Retry-After": {"1"}, "Retry-After-Ms": {"231"}, "Set-Cookie": {"private"}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			raw, err := readFeedbackResponse(context.Background(), resp)
			require.NoError(t, err)
			var got struct {
				Status string
				Output []json.RawMessage
				Error  map[string]json.RawMessage
			}
			require.NoError(t, json.Unmarshal(raw, &got))
			require.Equal(t, "failed", got.Status)
			require.Empty(t, got.Output)
			require.Equal(t, `"rate_limit_exceeded"`, string(got.Error["code"]))
			require.Equal(t, `"TPM limit"`, string(got.Error["message"]))
			require.Equal(t, "null", string(got.Error["param"]))
			if tc.status == 429 {
				require.Equal(t, "429", string(got.Error["status_code"]))
			}
			require.Contains(t, string(got.Error["headers"]), `"retry-after-ms":"231"`)
			require.NotContains(t, string(raw), "private")
			require.NotContains(t, string(raw), "sequence_number")
		})
	}
}
