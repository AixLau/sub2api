package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type memoryStore map[string][]byte

func (m memoryStore) Get(_ context.Context, key string) ([]byte, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}
func (m memoryStore) Put(_ context.Context, key string, v []byte) error {
	m[key] = append([]byte(nil), v...)
	return nil
}

const weatherTool = `[{"type":"function","name":"get_weather","description":"Weather in a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]`

func prepareWeather(t *testing.T, store Store) *Request {
	t.Helper()
	r, err := Prepare(context.Background(), []byte(`{"model":"gpt-6-astra","tools":`+weatherTool+`,"tool_choice":"auto","input":"Weather in Tokyo?"}`), "session-a", store, nil)
	require.NoError(t, err)
	return r
}

func officeItem(name string, args any, objectArguments bool) json.RawMessage {
	outer := map[string]any{"summary": "Read weather", "references": []string{"Tokyo weather"}, "destructive": false, "code": string(encoded(map[string]any{"tool": name, "args": args}))}
	var arguments any = string(encoded(outer))
	if objectArguments {
		arguments = outer
	}
	return encoded(map[string]any{"type": "function_call", "id": "fc_full_1", "call_id": "call_native_1", "status": "completed", "name": "run_officejs", "arguments": arguments, "provider_extension": map[string]any{"retain": true}})
}

func TestRoundTripFullNativeItemAndStableTurn(t *testing.T) {
	ctx := context.Background()
	for _, objectArgs := range []bool{true, false} {
		t.Run(fmt.Sprint(objectArgs), func(t *testing.T) {
			store := memoryStore{}
			r := prepareWeather(t, store)
			root, _ := parseObject(r.Body)
			require.NotContains(t, root, "tools")
			require.NotContains(t, root, "tool_choice")
			require.Contains(t, string(root["input"]), "Client tool directory")
			native := officeItem("get_weather", map[string]any{"city": "Tokyo"}, objectArgs)
			response, err := r.Response(ctx, encoded(map[string]any{"id": "resp_first", "status": "completed", "output": []json.RawMessage{native}, "usage": map[string]int{"input_tokens": 15, "output_tokens": 7}}))
			require.NoError(t, err)
			responseObj, _ := parseObject(response)
			require.JSONEq(t, `{"input_tokens":15,"output_tokens":7}`, string(responseObj["usage"]))
			var calls []object
			require.NoError(t, json.Unmarshal(responseObj["output"], &calls))
			call := calls[0]
			require.Equal(t, "get_weather", stringValue(call["name"]))
			require.JSONEq(t, `{"city":"Tokyo"}`, stringValue(call["arguments"]))
			require.NotEqual(t, "call_native_1", stringValue(call["call_id"]))
			require.NotContains(t, call, "provider_extension")
			// Simulate a new plugin process. The downstream sends ONLY the result.
			follow := encoded(map[string]any{"tools": json.RawMessage(weatherTool), "previous_response_id": "resp_first", "input": []any{map[string]any{"type": "function_call_output", "call_id": stringValue(call["call_id"]), "output": "18°C"}}})
			r2, err := Prepare(ctx, follow, "session-a", store, nil)
			require.NoError(t, err)
			require.Equal(t, r.Turn.ID, r2.Turn.ID)
			require.Equal(t, r.Turn.Iteration+1, r2.Turn.Iteration)
			root2, _ := parseObject(r2.Body)
			var meta map[string]string
			require.NoError(t, json.Unmarshal(root2["metadata"], &meta))
			require.Equal(t, r.Turn.ID, meta["turn_id"])
			require.Equal(t, "1", meta["agent_iteration"])
			var input []json.RawMessage
			require.NoError(t, json.Unmarshal(root2["input"], &input))
			require.Len(t, input, 3)
			require.JSONEq(t, string(native), string(input[1]), "must retain id and every field, including unknown provider metadata")
			result, _ := parseObject(input[2])
			require.Equal(t, "call_native_1", stringValue(result["call_id"]))
			require.Equal(t, "18°C", stringValue(result["output"]))
			require.Equal(t, "fc_call_native_1", stringValue(result["id"]))
			retry, err := Prepare(ctx, follow, "session-a", store, nil)
			require.NoError(t, err)
			require.Equal(t, r2.Turn, retry.Turn, "network retry must not advance iteration")
			_, err = Prepare(ctx, follow, "session-b", store, nil)
			require.ErrorContains(t, err, "不存在")
			_, err = Prepare(ctx, follow, "session-a", memoryStore{}, nil)
			require.ErrorContains(t, err, "不存在")
		})
	}
}

func TestHistoryRestorationDeduplicatesCallAndResetsForNewUser(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	r := prepareWeather(t, store)
	native := officeItem("get_weather", map[string]any{"city": "Tokyo"}, false)
	call, err := r.convertCall(ctx, native)
	require.NoError(t, err)
	obj, _ := parseObject(call)
	input := []any{map[string]any{"role": "user", "content": "Weather in Tokyo?"}, json.RawMessage(call), map[string]any{"type": "function_call_output", "call_id": stringValue(obj["call_id"]), "output": "18°C"}}
	r2, err := Prepare(ctx, encoded(map[string]any{"tools": json.RawMessage(weatherTool), "input": input}), "session-a", store, nil)
	require.NoError(t, err)
	require.Equal(t, r.Turn.ID, r2.Turn.ID)
	root, _ := parseObject(r2.Body)
	var restored []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &restored))
	require.Len(t, restored, 4)
	input = append(input, map[string]any{"role": "user", "content": "Now Osaka"})
	r3, err := Prepare(ctx, encoded(map[string]any{"tools": json.RawMessage(weatherTool), "input": input}), "session-a", store, nil)
	require.NoError(t, err)
	require.NotEqual(t, r.Turn.ID, r3.Turn.ID)
	require.Zero(t, r3.Turn.Iteration)
}

func TestNamespaceCustomAndUndeclaredOrExecutablePayload(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	raw := []byte(`{"input":"hello","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"patch","description":"Apply patch"}]}]}`)
	r, err := Prepare(ctx, raw, "scope", store, nil)
	require.NoError(t, err)
	call, err := r.convertCall(ctx, officeItem("functions.patch", "line 1\nline 2", false))
	require.NoError(t, err)
	obj, _ := parseObject(call)
	require.Equal(t, "custom_tool_call", stringValue(obj["type"]))
	require.Equal(t, "functions", stringValue(obj["namespace"]))
	require.Equal(t, "patch", stringValue(obj["name"]))
	require.Equal(t, "line 1\nline 2", stringValue(obj["input"]))
	_, err = r.convertCall(ctx, officeItem("not_declared", map[string]any{}, true))
	require.ErrorContains(t, err, "未声明")

	// A valid envelope stuffed into another server executor is still a client call.
	foreign, _ := parseObject(officeItem("functions.patch", "line 1", false))
	foreign["name"] = encoded("run_connector_action")
	foreign["id"] = encoded("fc_foreign_exec")
	converted, err := r.convertCall(ctx, encoded(foreign))
	require.NoError(t, err)
	convertedObj, _ := parseObject(converted)
	require.Equal(t, "custom_tool_call", stringValue(convertedObj["type"]))
	require.Equal(t, "patch", stringValue(convertedObj["name"]))

	// Undeclared server tools must never reach the client executor.
	jsCode, _ := parseObject(officeItem("functions.patch", "text", false))
	jsCode["arguments"] = encoded(`{"code":"Excel.run(() => doSomething())"}`)
	connector, _ := parseObject(officeItem("functions.patch", "text", false))
	connector["name"] = encoded("run_connector_action")
	connector["arguments"] = encoded(`{"action":"search","query":"x"}`)
	for _, raw := range []json.RawMessage{
		officeItem("not_declared", map[string]any{}, true),
		encoded(jsCode),
		encoded(connector),
	} {
		passed, err := r.convertCall(ctx, raw)
		require.Error(t, err)
		require.Nil(t, passed)
	}
}

func TestNativeHistoryPassesThroughUnchanged(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": "hi"},
		map[string]any{"type": "function_call", "id": "fc_conn", "call_id": "call_conn_1", "name": "run_connector_action", "arguments": `{"action":"search"}`, "status": "completed"},
		map[string]any{"type": "function_call_output", "id": "fc_call_conn_1", "call_id": "call_conn_1", "output": "done"},
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5.6-sol", "input": input})
	require.NoError(t, err)
	r, err := Prepare(context.Background(), raw, "s", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	require.Len(t, items, 4, "catalog + user + native call + output")
	call, _ := parseObject(items[2])
	require.Equal(t, "run_connector_action", stringValue(call["name"]))
	require.Equal(t, "call_conn_1", stringValue(call["call_id"]))
	require.JSONEq(t, `{"action":"search"}`, stringValue(call["arguments"]))
	out, _ := parseObject(items[3])
	require.Equal(t, "call_conn_1", stringValue(out["call_id"]))
	require.Equal(t, "done", stringValue(out["output"]))
}

func TestTransportNameAlias(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	r := prepareWeather(t, store)
	for _, name := range []string{"run_officejs", "functions.run_officejs"} {
		native := officeItem("get_weather", map[string]any{"city": "Tokyo"}, false)
		item, _ := parseObject(native)
		item["name"] = encoded(name)
		item["id"] = encoded("fc_alias_" + name)
		call, err := r.convertCall(ctx, encoded(item))
		require.NoError(t, err, name)
		obj, _ := parseObject(call)
		require.Equal(t, "get_weather", stringValue(obj["name"]))
		require.NotEqual(t, "run_officejs", stringValue(obj["name"]))
	}
}

func TestRequestBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{"tools":[],"input":[],"agent_iteration":512}`,
		`{"tools":[],"input":[],"agent_iteration":-1}`,
		`{"tools":[],"input":[],"agent_iteration":null}`,
		`{"tools":[],"input":null}`,
		`{"tools":[],"input":[],"turn_id":null}`,
		`{"tools":[],"input":[],"tool_choice":"required"}`,
		`{"tools":[],"input":[{"type":"function_call_output","call_id":"foreign","output":"x"}]}`,
		`{"tools":[],"input":[],"reasoning":{"effort":"banana"}}`,
	} {
		_, err := Prepare(context.Background(), []byte(raw), "x", memoryStore{}, nil)
		require.Error(t, err, raw)
	}
	_, err := Prepare(context.Background(), []byte(`{"tools":`+weatherTool+`,"input":[]}`), "x", nil, nil)
	require.ErrorContains(t, err, "KV")
	for _, choice := range []string{`"none"`, `{"type":"function","name":"get_weather"}`} {
		r, err := Prepare(context.Background(), []byte(`{"tools":`+weatherTool+`,"input":[],"tool_choice":`+choice+`}`), "x", memoryStore{}, nil)
		require.NoError(t, err)
		root, _ := parseObject(r.Body)
		require.NotContains(t, root, "tools")
		require.NotContains(t, root, "tool_choice")
	}
}

func event(typ string, fields map[string]any) string {
	fields["type"] = typ
	return "event: " + typ + "\r\ndata: " + string(encoded(fields)) + "\r\n\r\n"
}

func TestWireShapeMatchesExcelAddIn(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-5.6-luna-excel",
		"instructions": "Be brief.",
		"input": [{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}],"internal_chat_message_metadata_passthrough":{"turn_id":"vary"}}],
		"tools": ` + weatherTool + `,
		"tool_choice": "auto",
		"parallel_tool_calls": true,
		"stream": true,
		"store": true,
		"max_output_tokens": 1234,
		"temperature": 0.5,
		"top_p": 0.9,
		"text": {"verbosity":"low"},
		"include": ["reasoning.encrypted_content"],
		"previous_response_id": "resp_x",
		"prompt_cache_key": "pk-1",
		"reasoning": {"effort":"x-high","summary":"auto"},
		"metadata": {"client":"codex","n":3},
		"user": "u1"
	}`)
	r, err := Prepare(context.Background(), raw, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	for _, field := range []string{"tools", "tool_choice", "parallel_tool_calls", "max_output_tokens", "temperature", "top_p", "text", "include", "previous_response_id", "reasoning", "instructions", "user", "turn_id", "agent_iteration"} {
		require.NotContains(t, root, field, "wire shape must not forward %s", field)
	}
	require.Equal(t, "gpt-5.6-luna", stringValue(root["model"]))
	require.Equal(t, "explicit", stringValue(root["model_selection"]))
	require.Equal(t, "true", string(root["stream"]))
	require.Equal(t, "false", string(root["store"]))
	require.Equal(t, "xhigh", stringValue(root["reasoning_effort"]))
	require.Equal(t, "pk-1", stringValue(root["prompt_cache_key"]))
	require.JSONEq(t, `[{"type":"compaction","compact_threshold":200000}]`, string(root["context_management"]))
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &input))
	require.Len(t, input, 3)
	require.Contains(t, string(input[0]), "Be brief.")
	require.JSONEq(t, `{"type":"message","role":"developer","content":[{"type":"input_text","text":"Be brief."}]}`, string(input[0]))
	require.Contains(t, string(input[1]), "Client tool directory")
	require.NotContains(t, string(input[2]), "internal_chat_message_metadata_passthrough")
	require.Contains(t, string(input[2]), `"type":"message"`)
	var meta map[string]string
	require.NoError(t, json.Unmarshal(root["metadata"], &meta))
	require.Equal(t, "scope-1", meta["task_id"])
	require.NotEmpty(t, meta["turn_id"])
	require.Equal(t, "0", meta["agent_iteration"])
	require.Equal(t, "codex", meta["client"])
	require.Equal(t, "3", meta["n"])

	withCompaction := []byte(`{"input":"hi","context_management":[{"type":"compaction","compact_threshold":1000}]}`)
	r2, err := Prepare(context.Background(), withCompaction, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	root2, _ := parseObject(r2.Body)
	require.JSONEq(t, `[{"type":"compaction","compact_threshold":1000}]`, string(root2["context_management"]))
}

func TestForeignToolHistoryRebuildsTransportEnvelope(t *testing.T) {
	ctx := context.Background()
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "weather?"}}},
		map[string]any{"type": "function_call", "id": "fc_old", "call_id": "call_old_1", "name": "get_weather", "arguments": `{"city":"Tokyo"}`},
		map[string]any{"type": "function_call_output", "id": "fc_call_old_1", "call_id": "call_old_1", "output": "18°C"},
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5.6-luna", "tools": json.RawMessage(weatherTool), "input": input})
	require.NoError(t, err)
	r, err := Prepare(ctx, raw, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, r.Turn.Iteration)
	root, _ := parseObject(r.Body)
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	require.Len(t, items, 4)
	call, _ := parseObject(items[2])
	require.Equal(t, "run_officejs", stringValue(call["name"]))
	require.Equal(t, "call_old_1", stringValue(call["call_id"]))
	require.Equal(t, "fc_call_old_1", stringValue(call["id"]))
	var outer map[string]any
	require.NoError(t, json.Unmarshal([]byte(stringValue(call["arguments"])), &outer))
	var envelope map[string]any
	require.NoError(t, json.Unmarshal([]byte(outer["code"].(string)), &envelope))
	require.Equal(t, "get_weather", envelope["tool"])
	require.Equal(t, map[string]any{"city": "Tokyo"}, envelope["args"])
	out, _ := parseObject(items[3])
	require.Equal(t, "function_call_output", stringValue(out["type"]))
	require.Equal(t, "call_old_1", stringValue(out["call_id"]))
	require.Equal(t, "18°C", stringValue(out["output"]))
	require.Equal(t, "fc_call_old_1", stringValue(out["id"]))

	retry, err := Prepare(ctx, raw, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	require.Equal(t, r.Turn, retry.Turn, "network retry must not advance iteration")

	// A second completed round in the same turn keeps the turn and advances.
	more := append(append([]any{}, input...),
		map[string]any{"type": "function_call", "id": "fc_old2", "call_id": "call_old_2", "name": "get_weather", "arguments": `{"city":"Osaka"}`},
		map[string]any{"type": "function_call_output", "id": "fc_call_old_2", "call_id": "call_old_2", "output": "22°C"},
	)
	raw2, err := json.Marshal(map[string]any{"model": "gpt-5.6-luna", "tools": json.RawMessage(weatherTool), "input": more})
	require.NoError(t, err)
	r2, err := Prepare(ctx, raw2, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	require.Equal(t, r.Turn.ID, r2.Turn.ID)
	require.Equal(t, 2, r2.Turn.Iteration)
}

func TestReasoningAndItemReferenceHygiene(t *testing.T) {
	input := []any{
		map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}},
		map[string]any{"type": "reasoning", "id": "rs_2", "summary": []any{}, "encrypted_content": "gAAA=="},
		map[string]any{"type": "item_reference", "id": "ir_1"},
		map[string]any{"type": "image_generation", "id": "ig_1", "status": "completed", "result": nil},
		map[string]any{"type": "image_generation", "id": "ig_2", "status": "completed", "result": "aGVsbG8="},
		map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}},
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5.6-luna", "input": input})
	require.NoError(t, err)
	r, err := Prepare(context.Background(), raw, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	require.Len(t, items, 4, "bare reasoning, item_reference and thin image_generation must be dropped")
	reasoning, _ := parseObject(items[1])
	require.Equal(t, "reasoning", stringValue(reasoning["type"]))
	require.Equal(t, "gAAA==", stringValue(reasoning["encrypted_content"]))
	require.JSONEq(t, "[]", string(reasoning["summary"]))
	require.NotContains(t, reasoning, "id")
	image, _ := parseObject(items[2])
	require.Equal(t, "image_generation", stringValue(image["type"]))
	require.Equal(t, "ig_2", stringValue(image["id"]))
}

func TestReasoningEffortPolicy(t *testing.T) {
	ctx := context.Background()
	for requested, expected := range map[string]string{
		"low": "low", "medium": "medium", "high": "high", "xhigh": "xhigh",
		"ultra": "ultra", "ULTRA": "ultra",
		"max": "xhigh", "x-high": "xhigh", "extra-high": "xhigh",
	} {
		raw := []byte(`{"input":"hi","reasoning":{"effort":"` + requested + `"}}`)
		r, err := Prepare(ctx, raw, "s", memoryStore{}, nil)
		require.NoError(t, err)
		root, _ := parseObject(r.Body)
		require.Equal(t, expected, stringValue(root["reasoning_effort"]), requested)
	}
	r, err := Prepare(ctx, []byte(`{"input":"hi"}`), "s", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	require.Equal(t, "medium", stringValue(root["reasoning_effort"]))
	_, err = Prepare(ctx, []byte(`{"input":"hi","reasoning":{"effort":"banana"}}`), "s", memoryStore{}, nil)
	require.ErrorContains(t, err, "reasoning effort")
}

func TestModelMapping(t *testing.T) {
	mapping := map[string]string{
		"gpt-6-astra-basispoints": "gpt-6-astra",
		"gpt-5.6-luna-excel":      "gpt-5.6-luna",
		"alias-excel":             "target-excel",
	}
	for requested, want := range map[string]string{
		"gpt-6-astra-basispoints": "gpt-6-astra",
		"gpt-5.6-luna-excel":      "gpt-5.6-luna",
		"alias-excel":             "target-excel",
		"gpt-6-sol":               "gpt-6-sol",
		"gpt-5.6-terra-excel":     "gpt-5.6-terra",
	} {
		raw := []byte(`{"model":"` + requested + `","input":"hi"}`)
		r, err := Prepare(context.Background(), raw, "s", memoryStore{}, mapping)
		require.NoError(t, err)
		root, _ := parseObject(r.Body)
		require.Equal(t, want, stringValue(root["model"]), requested)
	}
	// Without a mapping the default pass-through keeps working.
	r, err := Prepare(context.Background(), []byte(`{"model":"gpt-5.6-luna-excel","input":"hi"}`), "s", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	require.Equal(t, "gpt-5.6-luna", stringValue(root["model"]))
}

func TestResponsesLiteAdditionalToolsBuildCatalog(t *testing.T) {
	// Codex "Responses Lite" declares tools in input[] carrier items instead of
	// the top-level tools array. The catalog must pick them up or the model is
	// told to answer without tools.
	raw := []byte(`{
		"model": "gpt-5.5",
		"input": [
			{"type":"additional_tools","tools":[{"type":"function","name":"exec_command","description":"Run a terminal command","parameters":{"type":"object"}}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"check git status"}]}
		]
	}`)
	r, err := Prepare(context.Background(), raw, "scope-1", memoryStore{}, nil)
	require.NoError(t, err)
	root, _ := parseObject(r.Body)
	var items []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &items))
	require.Len(t, items, 2, "catalog + user message only; the carrier item is folded in")
	require.NotContains(t, string(root["input"]), "additional_tools")
	require.Contains(t, string(root["input"]), "exec_command")
	require.NotContains(t, string(root["input"]), "do not call any tools")

	// A transport envelope for the carrier-declared tool converts normally.
	call, err := r.convertCall(context.Background(), officeItem("exec_command", map[string]any{"cmd": "git status"}, false))
	require.NoError(t, err)
	obj, _ := parseObject(call)
	require.Equal(t, "exec_command", stringValue(obj["name"]))
}

func TestFunctionItemIDPassesUpstreamValidation(t *testing.T) {
	for _, callID := range []string{
		"call_native_1",
		"call_bps_667a8d0f69a2486a91e153ad",
		"fc_667a8d0f69a2486a91e153ad#16a5533fe8b14b5cbb1df4f9c9b5daec",
		"fc_cleanhexalready",
		"",
	} {
		id := functionItemID(callID)
		require.True(t, strings.HasPrefix(id, "fc_"), callID)
		require.LessOrEqual(t, len(id), 64, callID)
		require.NotContains(t, id, "#", callID)
		require.False(t, strings.HasPrefix(id, "fc_fc_"), callID)
		require.Regexp(t, `^fc_[A-Za-z0-9_-]+$`, id, callID)
	}
	require.Equal(t, "fc_call_native_1", functionItemID("call_native_1"))
	require.NotEqual(t, functionItemID("call_1"), functionItemID("call_2"))
}

func TestStreamKeepsTextIncrementalAndTranslatesAllToolEvents(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	r := prepareWeather(t, store)
	native := officeItem("get_weather", map[string]any{"city": "東京"}, false)
	added, _ := parseObject(native)
	added["arguments"] = encoded("")
	added["status"] = encoded("in_progress")
	stream := event("response.created", map[string]any{"response": map[string]any{"id": "resp1", "output": []any{}}}) +
		event("response.output_text.delta", map[string]any{"item_id": "msg1", "output_index": 0, "delta": "Checking"}) +
		event("response.output_item.added", map[string]any{"output_index": 1, "item": added}) +
		event("response.function_call_arguments.delta", map[string]any{"output_index": 1, "item_id": "fc_full_1", "delta": "{\"code\":\""}) +
		event("response.function_call_arguments.done", map[string]any{"output_index": 1, "item_id": "fc_full_1", "arguments": "native wrapper"}) +
		event("response.output_item.done", map[string]any{"output_index": 1, "item": native}) +
		event("response.completed", map[string]any{"response": map[string]any{"id": "resp1", "status": "completed", "output": []any{map[string]any{"type": "message", "id": "msg1"}, native}, "usage": map[string]int{"total_tokens": 25}}}) + "data: [DONE]\n\n"
	var frames []string
	err := r.Stream(ctx, strings.NewReader(stream), func(raw []byte) error { frames = append(frames, string(raw)); return nil })
	require.NoError(t, err)
	joined := strings.Join(frames, "")
	require.Contains(t, joined, "Checking")
	require.NotContains(t, joined, "run_officejs")
	require.NotContains(t, joined, "native wrapper")
	require.Equal(t, 1, strings.Count(joined, "event: response.output_item.added"))
	require.Equal(t, 1, strings.Count(joined, "event: response.function_call_arguments.delta"))
	require.Equal(t, 1, strings.Count(joined, "event: response.function_call_arguments.done"))
	require.Equal(t, 1, strings.Count(joined, "event: response.output_item.done"))
	var lastSequence = -1
	for _, frame := range frames {
		for _, line := range strings.Split(frame, "\n") {
			if !strings.HasPrefix(line, "data: {") {
				continue
			}
			var e map[string]any
			require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e))
			require.Greater(t, int(e["sequence_number"].(float64)), lastSequence)
			lastSequence = int(e["sequence_number"].(float64))
			if e["type"] == "response.function_call_arguments.delta" {
				require.JSONEq(t, `{"city":"東京"}`, e["delta"].(string))
			}
		}
	}
}

func TestStreamFinalOnlyAndTruncated(t *testing.T) {
	r := prepareWeather(t, memoryStore{})
	native := officeItem("get_weather", map[string]any{"city": "Tokyo"}, true)
	stream := event("response.completed", map[string]any{"response": map[string]any{"id": "resp-final-only", "status": "completed", "output": []json.RawMessage{native}}})
	var frames []byte
	require.NoError(t, r.Stream(context.Background(), strings.NewReader(stream), func(b []byte) error { frames = append(frames, b...); return nil }))
	require.Contains(t, string(frames), "response.function_call_arguments.delta")
	err := r.Stream(context.Background(), strings.NewReader(event("response.created", map[string]any{"response": map[string]any{"output": []any{}}})), func([]byte) error { return nil })
	require.ErrorContains(t, err, "终结")
}

type brokenStore struct{ memoryStore }

func (brokenStore) Put(context.Context, string, []byte) error { return errors.New("storage down") }
func TestStoreFailureNeverReleasesExecutableCall(t *testing.T) {
	r := prepareWeather(t, brokenStore{memoryStore{}})
	stream := event("response.output_item.done", map[string]any{"output_index": 0, "item": officeItem("get_weather", map[string]any{}, false)})
	var emitted int
	err := r.Stream(context.Background(), strings.NewReader(stream), func([]byte) error { emitted++; return nil })
	require.ErrorContains(t, err, "保存")
	require.Zero(t, emitted)
}

func TestEnvelopeSpellingVariantsAndBareNameLookup(t *testing.T) {
	ctx := context.Background()
	raw := []byte(`{"input":"hi","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec","description":"Run command"}]}]}`)
	r, err := Prepare(ctx, raw, "s", memoryStore{}, nil)
	require.NoError(t, err)

	for _, code := range []string{
		`{"tool":"exec","args":"ls"}`,
		`{"name":"exec","arguments":"ls"}`,
		`{"tool":"functions.exec","args":"ls"}`,
		`{"tool":"exec","args":"ls","note":"extra key tolerated"}`,
	} {
		native := officeItem("run_officejs", map[string]any{}, false)
		item, _ := parseObject(native)
		item["arguments"] = encoded(string(encoded(map[string]any{"code": code})))
		item["id"] = encoded("fc_variant_" + code[:16])
		call, err := r.convertCall(ctx, encoded(item))
		require.NoError(t, err, code)
		obj, _ := parseObject(call)
		require.Equal(t, "custom_tool_call", stringValue(obj["type"]), code)
		require.Equal(t, "exec", stringValue(obj["name"]), code)
		require.Equal(t, "ls", stringValue(obj["input"]), code)
	}
}
