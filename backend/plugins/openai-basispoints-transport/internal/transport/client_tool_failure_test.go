package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

func TestForwardClientToolFailuresUseNormalClientLoop(t *testing.T) {
	testForwardClientToolFailures(t, "")
}

func testForwardClientToolFailures(t *testing.T, binary string) {
	t.Helper()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			tools := []any{
				map[string]any{"type": "namespace", "name": "functions", "tools": []any{
					map[string]any{"type": "custom", "name": "exec", "description": "Run JavaScript with tools, ALL_TOOLS and text(value)."},
				}},
				map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
					map[string]any{"type": "function", "name": "list_agents"},
				}},
			}
			var hits atomic.Int32
			observed := make(chan map[string]json.RawMessage, 4)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				observed <- body
				round := hits.Add(1)
				output := []any{}
				if round <= 3 {
					code := "{}"
					if round == 1 {
						code = "await private_payload();"
					}
					args, _ := json.Marshal(map[string]any{"references": []string{"client-tool:collaboration.list_agents"}, "code": code})
					call := map[string]any{"type": "function_call", "name": "run_officejs", "id": fmt.Sprintf("fc_%d", round), "call_id": fmt.Sprintf("call_%d", round), "arguments": string(args)}
					if round == 2 {
						call["name"], call["arguments"] = "write_range", "{\"value\":\"private_payload\"}"
					}
					output = append(output, call)
				}
				response := map[string]any{"id": fmt.Sprintf("resp_%d", round), "status": "completed", "output": output, "usage": map[string]int{"total_tokens": 11}}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					if len(output) > 0 {
						io.WriteString(w, wireEvent("response.output_item.done", map[string]any{"output_index": 0, "item": output[0]}))
					}
					io.WriteString(w, wireEvent("response.completed", map[string]any{"response": response}))
				} else {
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				}
			}))
			defer upstream.Close()
			client := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
			cfg, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled"})
			applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			var input any = "Inspect the task"
			for round := 1; round <= 4; round++ {
				body, _ := json.Marshal(map[string]any{"input": input, "tools": tools, "stream": stream})
				start, out, failure := forwardForTest(t, client, body, ctx)
				require.Nil(t, failure)
				require.EqualValues(t, http.StatusOK, start.StatusCode)
				require.EqualValues(t, round, hits.Load(), "one upstream inference per client request")
				require.NotContains(t, string(out), "private_payload")
				response := terminalResponse(t, out, stream)
				require.Equal(t, "\"completed\"", string(response["status"]))
				require.JSONEq(t, "{\"total_tokens\":11}", string(response["usage"]))
				request := <-observed
				if round > 1 {
					require.Contains(t, string(request["input"]), fmt.Sprintf("\"call_id\":\"call_%d\"", round-1))
					require.Contains(t, string(request["input"]), "function_call_output")
				}
				if round == 4 {
					break
				}
				var calls []struct {
					Type, Name, Namespace, Input string
					CallID                       string `json:"call_id"`
				}
				require.NoError(t, json.Unmarshal(response["output"], &calls))
				require.Len(t, calls, 1)
				result, resultType := "[]", "function_call_output"
				if round < 3 {
					require.Equal(t, "exec", calls[0].Name)
					require.Equal(t, "functions", calls[0].Namespace)
					require.Equal(t, "custom_tool_call", calls[0].Type)
					harness := "const src=require('node:fs').readFileSync(0,'utf8'),out=[];new Function('text',src)(v=>out.push(v));process.stdout.write(JSON.stringify(out));"
					cmd := exec.CommandContext(ctx, "node", "-e", harness)
					cmd.Stdin = strings.NewReader(calls[0].Input)
					printed, err := cmd.Output()
					require.NoError(t, err)
					result, resultType = string(printed), "custom_tool_call_output"
					require.Contains(t, result, "\"executed\":false")
					if round == 2 {
						require.Contains(t, result, "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE")
					}
				} else {
					require.Equal(t, "list_agents", calls[0].Name)
					require.Equal(t, "collaboration", calls[0].Namespace)
				}
				input = []any{map[string]any{"type": resultType, "call_id": calls[0].CallID, "output": result}}
			}
			entries := completedRequestsForTest(t, client, ctx, 4)
			for _, entry := range entries {
				require.Equal(t, 1, entry.Attempt)
				require.Empty(t, entry.ErrorCode)
			}
		})
	}
}
