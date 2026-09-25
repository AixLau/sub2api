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

func terminalResponse(t *testing.T, out []byte, stream bool) map[string]json.RawMessage {
	t.Helper()
	var response map[string]json.RawMessage
	if !stream {
		require.NoError(t, json.Unmarshal(out, &response))
		return response
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var frame struct {
			Type     string
			Response map[string]json.RawMessage
		}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame))
		if frame.Type == "response.completed" || frame.Type == "response.failed" {
			response = frame.Response
		}
	}
	require.NotNil(t, response, "stream must carry a terminal response")
	return response
}

func TestForwardAgentTaskAndFollowup(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			task := "核对任务\n保留 \"quotes\", \\ 和 tabs\t"
			args, err := json.Marshal(map[string]any{"tool": "collaboration.spawn_agent", "args": map[string]any{"message": task, "task_name": "worker"}})
			require.NoError(t, err)
			native := map[string]any{"type": "function_call", "id": "fc_parent", "call_id": "call_parent", "name": "run_officejs",
				"arguments": map[string]any{"code": string(args)}, "encrypted_function_args": []string{"code"}}
			requests := make(chan map[string]json.RawMessage, 3)
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- body
				output := []any{}
				if hits.Add(1) == 1 {
					output = append(output, native)
				}
				response := map[string]any{"id": "resp_agent", "status": "completed", "output": output}
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
			tools := []any{map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{map[string]any{
				"type": "function", "name": "spawn_agent", "parameters": map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string", "encrypted": true}}},
			}}}}
			parent, err := json.Marshal(map[string]any{"input": "delegate", "tools": tools, "stream": stream})
			require.NoError(t, err)
			start, out, failure := forwardForTest(t, c, parent, ctx)
			require.Nil(t, failure)
			require.Equal(t, int32(200), start.StatusCode)
			response := terminalResponse(t, out, stream)
			var calls []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(response["output"], &calls))
			require.Len(t, calls, 1)
			require.Equal(t, "[]", string(calls[0]["encrypted_function_args"]))
			var arguments string
			require.NoError(t, json.Unmarshal(calls[0]["arguments"], &arguments))
			var delivered map[string]string
			require.NoError(t, json.Unmarshal([]byte(arguments), &delivered))
			require.Equal(t, task, delivered["message"])
			<-requests

			// Codex uses the explicit empty encryption list to mark the task as
			// input_text. Exercise that child request through the public RPC.
			envelope := "Message Type: NEW_TASK\nPayload:\n"
			input := []any{map[string]any{"type": "reasoning", "encrypted_content": "inherited-reasoning", "summary": []any{}},
				map[string]any{"type": "compaction", "encrypted_content": "inherited-compaction"},
				map[string]any{"type": "agent_message", "author": "/root", "recipient": "/root/worker", "content": []any{
					map[string]any{"type": "input_text", "text": envelope}, map[string]any{"type": "input_text", "text": delivered["message"]},
				}},
			}
			var firstTurn string
			for round, expected := range []string{envelope + task, "继续执行\n保持原有顺序"} {
				if round == 1 {
					input = append(input, map[string]any{"type": "message", "role": "assistant", "content": "working"},
						map[string]any{"type": "agent_message", "content": []any{map[string]any{"type": "input_text", "text": expected}}})
				}
				body, err := json.Marshal(map[string]any{"input": input, "stream": stream})
				require.NoError(t, err)
				start, out, failure = forwardForTest(t, c, body, ctx)
				require.Nil(t, failure)
				require.Equal(t, int32(200), start.StatusCode)
				require.Equal(t, "\"completed\"", string(terminalResponse(t, out, stream)["status"]))
				got := <-requests
				var items []map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(got["input"], &items))
				require.Len(t, items, len(input)+1)
				require.Equal(t, "\"inherited-reasoning\"", string(items[1]["encrypted_content"]))
				require.Equal(t, "\"inherited-compaction\"", string(items[2]["encrypted_content"]))
				last := items[len(items)-1]
				require.Equal(t, "\"message\"", string(last["type"]))
				require.Equal(t, "\"user\"", string(last["role"]))
				var content []map[string]string
				require.NoError(t, json.Unmarshal(last["content"], &content))
				require.Equal(t, []map[string]string{{"type": "input_text", "text": expected}}, content)
				var metadata map[string]string
				require.NoError(t, json.Unmarshal(got["metadata"], &metadata))
				require.Equal(t, "0", metadata["agent_iteration"])
				if round == 0 {
					firstTurn = metadata["turn_id"]
				} else {
					require.NotEqual(t, firstTurn, metadata["turn_id"])
				}
			}
			require.Equal(t, int32(3), hits.Load(), "valid task requests must not require retries")
		})
	}
}

func TestForwardEncryptedAgentMessageFailsBeforeNetwork(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var hits atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				hits.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer upstream.Close()
			c := recoveryClient(t, ctx, upstream.URL)
			body, err := json.Marshal(map[string]any{"stream": stream, "input": []any{map[string]any{"type": "agent_message", "content": []any{
				map[string]any{"type": "input_text", "text": "private task envelope"},
				map[string]any{"type": "encrypted_content", "encrypted_content": "private readable task"},
			}}}})
			require.NoError(t, err)
			start, out, failure := forwardForTest(t, c, body, ctx)
			require.Nil(t, failure, "semantic failures must not trigger transport failover")
			if stream {
				require.Equal(t, int32(200), start.StatusCode)
				require.Equal(t, 1, strings.Count(string(out), "event: response.failed"))
			} else {
				require.Equal(t, int32(400), start.StatusCode)
			}
			require.Contains(t, string(out), "TOOL_BRIDGE_REQUEST_INVALID")
			require.Contains(t, string(out), "input[0].content[1]")
			require.NotContains(t, string(out), "private")
			require.Zero(t, hits.Load())
		})
	}
}
