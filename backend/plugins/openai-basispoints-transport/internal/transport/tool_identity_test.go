package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestForwardToolIdentityAndDiagnostics(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/known=%v", stream, known), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				name := "functions.run_officejs"
				if !known {
					name = "functions.run_connector_action"
				}
				item := map[string]any{"type": "function_call", "name": name, "namespace": "functions", "id": "fc_test", "call_id": "call_test",
					"arguments": map[string]any{"summary": "private_summary", "references": []string{"get_weather"}, "code": `{"city":"private_city"}`}}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					response := map[string]any{"id": "resp_identity", "status": "completed", "output": []any{item}}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						io.WriteString(w, wireEvent("response.completed", map[string]any{"response": response}))
					} else {
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(response)
					}
				}))
				defer upstream.Close()
				c := recoveryClient(t, ctx, upstream.URL)
				body, err := json.Marshal(map[string]any{"input": "weather", "stream": stream, "tools": []any{map[string]any{"type": "function", "name": "get_weather"}}})
				require.NoError(t, err)
				start, out, failure := forwardForTest(t, c, body, ctx)
				require.Nil(t, failure)
				response := terminalResponse(t, out, stream)
				if known {
					require.Equal(t, int32(200), start.StatusCode)
					require.NotContains(t, response, "error")
					var calls []map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(response["output"], &calls))
					require.Len(t, calls, 1)
					require.JSONEq(t, `"get_weather"`, string(calls[0]["name"]))
					require.NotContains(t, string(out), "private_summary")
					return
				}
				if stream {
					require.Equal(t, int32(200), start.StatusCode)
				} else {
					require.Equal(t, int32(400), start.StatusCode)
				}
				var detail struct {
					Code        string
					Diagnostics struct {
						Stage, Name, Namespace string
					}
				}
				require.NoError(t, json.Unmarshal(response["error"], &detail))
				require.Equal(t, "TOOL_BRIDGE_CALL_INVALID", detail.Code)
				require.Equal(t, "upstream_tool_identity", detail.Diagnostics.Stage)
				require.Equal(t, name, detail.Diagnostics.Name)
				require.Equal(t, "functions", detail.Diagnostics.Namespace)
				require.NotContains(t, string(out), "private_city")
				require.NotContains(t, string(out), "private_summary")
			})
		}
	}
}
