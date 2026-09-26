package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func agentMessage(author, recipient, text string) object {
	return object{
		"type": encoded("agent_message"), "author": encoded(author), "recipient": encoded(recipient),
		"content": encoded([]any{map[string]any{"type": "input_text", "text": text}}),
	}
}

func preparedInput(t *testing.T, request *Request) []json.RawMessage {
	t.Helper()
	root, err := parseObject(request.Body)
	require.NoError(t, err)
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &input))
	return input
}

func TestPrepareAgentMessagesPreservesTextAndOrder(t *testing.T) {
	envelope := "Message Type: NEW_TASK\nTask name: /root/worker\nSender: /root\nPayload:\n"
	body := "核对 SVG. Preserve \"quotes\", tabs\tand \r\nline endings."
	task := agentMessage("/root", "/root/worker", body)
	task["id"] = encoded("amsg_private")
	task["content"] = encoded([]any{
		map[string]any{"type": "input_text", "text": envelope},
		map[string]any{"type": "input_text", "text": body},
	})
	task["internal_chat_message_metadata_passthrough"] = encoded(map[string]any{"turn_id": "private"})
	input := []any{messageItem("user", "parent context"), task, messageItem("assistant", "working"),
		agentMessage("/root", "/root/worker", "继续执行"),
		agentMessage("/root/worker", "/root", "Message Type: FINAL_ANSWER\nPayload:\ndone")}
	raw := encoded(map[string]any{"input": input})
	original := append([]byte(nil), raw...)
	r, err := Prepare(context.Background(), raw, "s", nil, nil, 256<<20)
	require.NoError(t, err)
	require.Equal(t, original, []byte(raw))
	got := preparedInput(t, r)
	require.Len(t, got, 6)
	for i, text := range []string{"parent context", envelope + body, "working", "继续执行", "Message Type: FINAL_ANSWER\nPayload:\ndone"} {
		role := "user"
		if i == 2 {
			role = "assistant"
		}
		require.JSONEq(t, string(encoded(messageItem(role, text))), string(got[i+1]))
	}
	require.NotContains(t, string(r.Body), "amsg_private")
	require.NotContains(t, string(r.Body), "encrypted_content")
}

func TestPrepareAgentMessagesNeverGuessCiphertext(t *testing.T) {
	for _, payload := range []string{"ordinary-looking task from an old session", "gAAAAABopaqueCiphertext", "继续执行", ""} {
		item := agentMessage("/root", "/root/worker", "private envelope")
		item["content"] = encoded([]any{
			map[string]any{"type": "input_text", "text": "private envelope"},
			map[string]any{"type": "encrypted_content", "encrypted_content": payload},
		})
		raw := encoded(map[string]any{"input": []any{item}})
		original := append([]byte(nil), raw...)
		r, err := Prepare(context.Background(), raw, "s", nil, nil, 256<<20)
		require.Nil(t, r)
		require.ErrorContains(t, err, "input[0].content[1]")
		require.ErrorContains(t, err, "encrypted_payload")
		require.NotContains(t, err.Error(), "private envelope")
		if payload != "" {
			require.NotContains(t, err.Error(), payload)
		}
		require.Equal(t, original, []byte(raw))
	}
	// The protocol type, not the appearance of text, is authoritative.
	r, err := Prepare(context.Background(), encoded(map[string]any{"input": []any{agentMessage("/root", "/root/worker", "gAAAAABliteral test data")}}), "s", nil, nil, 256<<20)
	require.NoError(t, err)
	require.Contains(t, string(r.Body), "gAAAAABliteral test data")
}

func TestPrepareAgentMessagesRejectIncompleteContent(t *testing.T) {
	for _, content := range []any{42, map[string]any{"text": "private"}, []any{nil},
		[]any{map[string]any{"type": "input_text", "text": 42}},
		[]any{map[string]any{"type": "private_unknown_type", "text": "private"}},
		[]any{map[string]any{"type": "input_text", "text": "readable", "encrypted_content": "private"}},
	} {
		item := agentMessage("/root", "/root/worker", "")
		item["content"] = encoded(content)
		_, err := Prepare(context.Background(), encoded(map[string]any{"input": []any{item}}), "s", nil, nil, 256<<20)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private")
	}
}

func TestPrepareAgentMessagesTrackNewTurnsAndToolContinuations(t *testing.T) {
	ctx, store := context.Background(), memoryStore{}
	parent := prepareWeather(t, store)
	parentCall, err := parent.convertCall(ctx, officeItem("get_weather", map[string]any{"city": "Tokyo"}, false))
	require.NoError(t, err)
	parentItem, _ := parseObject(parentCall)
	input := []any{messageItem("user", "parent task"), parentCall,
		map[string]any{"type": "function_call_output", "call_id": stringValue(parentItem["call_id"]), "output": "18 C"},
		agentMessage("/root", "/root/worker", "new task")}
	prepare := func() *Request {
		r, err := Prepare(ctx, encoded(map[string]any{"tools": json.RawMessage(weatherTool), "input": input, "previous_response_id": "not-in-store"}), "session-a", store, nil, 256<<20)
		require.NoError(t, err)
		return r
	}
	child := prepare()
	require.NotEqual(t, parent.Turn.ID, child.Turn.ID)
	require.Zero(t, child.Turn.Iteration)
	require.Equal(t, child.Turn, prepare().Turn)
	call, err := child.convertCall(ctx, officeItem("get_weather", map[string]any{"city": "Kyoto"}, false))
	require.NoError(t, err)
	item, _ := parseObject(call)
	input = append(input, call, map[string]any{"type": "function_call_output", "call_id": stringValue(item["call_id"]), "output": "20 C"})
	continued := prepare()
	require.Equal(t, child.Turn.ID, continued.Turn.ID)
	require.Equal(t, 1, continued.Turn.Iteration)
	require.Equal(t, continued.Turn, prepare().Turn)
	input = append(input, agentMessage("/root", "/root/worker", ""))
	require.Equal(t, continued.Turn, prepare().Turn, "empty messages do not reset the turn")
	input = append(input, agentMessage("/root", "/root/worker", "继续执行"))
	followup := prepare()
	require.NotEqual(t, child.Turn.ID, followup.Turn.ID)
	require.Zero(t, followup.Turn.Iteration)
	require.Equal(t, followup.Turn, prepare().Turn)
	input[len(input)-1] = agentMessage("/root", "/root/another-worker", "继续执行")
	require.NotEqual(t, followup.Turn.ID, prepare().Turn.ID, "original agent identity remains in the turn fingerprint")
}

func collaborationRequest(t *testing.T, name string) *Request {
	t.Helper()
	r, err := Prepare(context.Background(), encoded(map[string]any{"input": "delegate task", "tools": []any{map[string]any{
		"type": "namespace", "name": "collaboration", "tools": []any{map[string]any{
			"type": "function", "name": name, "parameters": map[string]any{
				"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string", "encrypted": true}},
			},
		}},
	}}}), "parent", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	return r
}

func TestCollaborationCallsDeclarePlaintextInJSONAndSSE(t *testing.T) {
	ctx := context.Background()
	args := map[string]any{"message": "保留任务\nquotes \" and tabs\t", "target": "worker"}
	for _, name := range []string{"spawn_agent", "send_message", "followup_task"} {
		for _, direct := range []bool{false, true} {
			native, _ := parseObject(officeItem("collaboration."+name, args, false))
			native["encrypted_function_args"] = encoded([]string{"code"})
			if direct {
				native["name"], native["namespace"] = encoded(name), encoded("collaboration")
				native["arguments"] = encoded(string(encoded(args)))
				delete(native, "encrypted_function_args")
			}
			assertCall := func(raw json.RawMessage) {
				item, err := parseObject(raw)
				require.NoError(t, err)
				require.Equal(t, "[]", string(item["encrypted_function_args"]), name)
				require.Equal(t, name, stringValue(item["name"]))
				require.Equal(t, "collaboration", stringValue(item["namespace"]))
				if stringValue(item["arguments"]) != "" {
					require.JSONEq(t, string(encoded(args)), stringValue(item["arguments"]))
				}
			}
			terminal := map[string]any{"id": "resp_agent", "status": "completed", "output": []any{native}}
			r := collaborationRequest(t, name)
			out, err := r.Response(ctx, encoded(terminal))
			require.NoError(t, err)
			root, _ := parseObject(out)
			var calls []json.RawMessage
			require.NoError(t, json.Unmarshal(root["output"], &calls))
			require.Len(t, calls, 1)
			assertCall(calls[0])
			clientCall, _ := parseObject(calls[0])
			replayed, err := Prepare(ctx, encoded(map[string]any{"input": []any{map[string]any{
				"type": "function_call_output", "call_id": stringValue(clientCall["call_id"]), "output": "delivered",
			}}}), "parent", r.store, nil, 256<<20)
			require.NoError(t, err)
			require.JSONEq(t, string(encoded(native)), string(preparedInput(t, replayed)[1]))
			r = collaborationRequest(t, name)
			wire := event("response.output_item.done", map[string]any{"output_index": 0, "item": native}) +
				event("response.completed", map[string]any{"response": terminal})
			marked := 0
			require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func(frame []byte) error {
				for _, line := range strings.Split(string(frame), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					e, err := parseObject([]byte(strings.TrimPrefix(line, "data: ")))
					require.NoError(t, err)
					switch stringValue(e["type"]) {
					case "response.output_item.added", "response.output_item.done":
						assertCall(e["item"])
						marked++
					case "response.completed":
						response, _ := parseObject(e["response"])
						require.NoError(t, json.Unmarshal(response["output"], &calls))
						assertCall(calls[0])
						marked++
					}
				}
				return nil
			}))
			require.Equal(t, 3, marked)
		}
	}
}

func TestDirectCallsPreserveExplicitEncryptionMetadata(t *testing.T) {
	for _, metadata := range []any{nil, []string{}, []string{"message"}} {
		r := collaborationRequest(t, "spawn_agent")
		native := object{"type": encoded("function_call"), "id": encoded("fc_direct"), "call_id": encoded("call_direct"),
			"name": encoded("collaboration.spawn_agent"), "arguments": encoded(string(encoded(map[string]any{"message": "opaque"}))),
			"encrypted_function_args": encoded(metadata)}
		call, err := r.convertCall(context.Background(), encoded(native))
		require.NoError(t, err)
		item, _ := parseObject(call)
		if metadata == nil {
			metadata = []string{}
		}
		require.JSONEq(t, string(encoded(metadata)), string(item["encrypted_function_args"]))
		require.Equal(t, native["arguments"], item["arguments"])
		native["encrypted_function_args"] = encoded([]string{"changed"})
		_, err = r.convertCall(context.Background(), encoded(native))
		require.ErrorContains(t, err, "修改了已输出")
	}
}

func TestDirectCallsRejectMalformedEncryptionMetadata(t *testing.T) {
	for _, metadata := range []any{"message", map[string]any{"message": true}, []any{nil}, []any{42}} {
		r := collaborationRequest(t, "spawn_agent")
		native := object{"type": encoded("function_call"), "id": encoded("fc_direct"), "call_id": encoded("call_direct"),
			"name": encoded("collaboration.spawn_agent"), "arguments": encoded(string(encoded(map[string]any{"message": "private task"}))),
			"encrypted_function_args": encoded(metadata)}
		call, err := r.convertCall(context.Background(), encoded(native))
		require.ErrorContains(t, err, "encrypted_function_args")
		require.NotContains(t, err.Error(), "private task")
		require.Nil(t, call)
		require.Empty(t, r.converted)
	}
}
