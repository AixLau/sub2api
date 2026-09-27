package transport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
)

// Regression for #483933: synchronous Chat Completions becomes a streaming
// Responses request before reaching this plugin. Explicit none must reach BPS
// as medium and produce a completed response, without a route change.
func TestForwardNoneReasoningEffort(t *testing.T) {
	testForwardNoneReasoningEffort(t, "")
}

func testForwardNoneReasoningEffort(t *testing.T, binary string) {
	t.Helper()
	for _, tc := range []struct {
		name, body string
		chat       bool
	}{
		{"responses buffered", `{"model":"gpt-6-sol","input":"Reply OK","reasoning":{"effort":"none"}}`, false},
		{"responses streaming", `{"model":"gpt-6-sol","input":"Reply OK","stream":true,"reasoning":{"effort":"none"}}`, false},
		{"top level", `{"model":"gpt-6-sol","input":"Reply OK","reasoning_effort":"none"}`, false},
		{"chat completions synchronous", `{"model":"gpt-6-sol","messages":[{"role":"user","content":"Reply OK"}],"stream":false,"reasoning_effort":"none"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			hits := make(chan map[string]json.RawMessage, 1)
			bps := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				hits <- wire
				const response = `{"id":"resp_none","object":"response","model":"gpt-6-sol","status":"completed","output":[{"id":"msg_none","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK"}]}]}`
				if string(wire["stream"]) == "true" {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":"+response+"}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, response)
				}
			}))
			defer bps.Close()
			client := clientForTest(t, &testHostKV{values: map[string][]byte{}}, binary)
			config, err := json.Marshal(map[string]any{"upstream_base_url": bps.URL, "proxy_mode": "disabled", "native_fallback": false})
			require.NoError(t, err)
			applied, err := client.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
			require.NoError(t, err)
			require.True(t, applied.Applied)
			body := []byte(tc.body)
			if tc.chat {
				var chat apicompat.ChatCompletionsRequest
				require.NoError(t, json.Unmarshal(body, &chat))
				responses, err := apicompat.ChatCompletionsToResponses(&chat)
				require.NoError(t, err)
				require.True(t, responses.Stream)
				require.Equal(t, "none", responses.Reasoning.Effort)
				body, err = json.Marshal(responses)
				require.NoError(t, err)
			}
			start, output, failure := forwardForTest(t, client, body, ctx)
			require.Nil(t, failure)
			require.NotNil(t, start)
			require.Equal(t, int32(http.StatusOK), start.StatusCode)
			require.Contains(t, string(output), `"status":"completed"`)
			require.Contains(t, string(output), `"text":"OK"`)
			require.NotContains(t, string(output), "response.failed")
			select {
			case wire := <-hits:
				require.JSONEq(t, `"medium"`, string(wire["reasoning_effort"]))
				require.NotContains(t, wire, "reasoning")
			default:
				t.Fatal("request did not reach BPS")
			}
		})
	}
}
