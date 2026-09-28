package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestForwardFunctionHistoryUnicode(t *testing.T) { testForwardFunctionHistoryUnicode(t, "") }

func testForwardFunctionHistoryUnicode(t *testing.T, binary string) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "unicode_JSON", true: "unicode_SSE"}[stream], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			checked := make(chan bool, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body struct {
					Input []struct{ Type, Name, Arguments string }
				}
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				found := false
				for _, item := range body.Input {
					if item.Type != "function_call" {
						continue
					}
					var envelope struct{ Code string }
					if err := json.Unmarshal([]byte(item.Arguments), &envelope); err != nil {
						t.Error(err)
						continue
					}
					var args struct{ Content string }
					if err := yaml.Unmarshal([]byte(envelope.Code), &args); err != nil {
						t.Error(err)
						continue
					}
					found = item.Name == "run_officejs" && args.Content == "release 🚀\r\nnext\u0085line"
				}
				checked <- found
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_unicode\",\"status\":\"completed\",\"output\":[]}}\n\n"))
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"id":"resp_unicode","status":"completed","output":[]}`))
				}
			}))
			defer upstream.Close()
			client := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
			config, _ := json.Marshal(map[string]any{"upstream_base_url": upstream.URL, "proxy_mode": "disabled", "tools_via_native": false})
			applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			body, err := json.Marshal(map[string]any{"model": "gpt-5.6-luna", "stream": stream, "tool_choice": "none", "tools": []any{map[string]any{"type": "function", "name": "Edit", "parameters": map[string]any{"type": "object"}}}, "input": []any{
				map[string]any{"type": "function_call", "call_id": "call_unicode", "name": "Edit", "arguments": `{"content":"release \ud83d\ude80\r\nnext\u0085line"}`},
				map[string]any{"type": "function_call_output", "call_id": "call_unicode", "output": "done"},
				map[string]any{"role": "user", "content": "Continue from the completed tool."},
			}})
			require.NoError(t, err)
			header, wire, failure := forwardForTest(t, client, body, ctx)
			require.Nil(t, failure)
			require.EqualValues(t, 200, header.StatusCode)
			require.JSONEq(t, `"completed"`, string(terminalResponse(t, wire, stream)["status"]))
			select {
			case valid := <-checked:
				require.True(t, valid, "decoded history must reach upstream unchanged")
			default:
				t.Fatal("history rejected before upstream")
			}
		})
	}
}
