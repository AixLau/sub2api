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
	r, err := Prepare(context.Background(), []byte(`{"model":"gpt-6-astra","tools":`+weatherTool+`,"tool_choice":"auto","input":"Weather in Tokyo?"}`), "session-a", store)
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
			r2, err := Prepare(ctx, follow, "session-a", store)
			require.NoError(t, err)
			require.Equal(t, r.Turn.ID, r2.Turn.ID)
			require.Equal(t, r.Turn.Iteration+1, r2.Turn.Iteration)
			root2, _ := parseObject(r2.Body)
			var input []json.RawMessage
			require.NoError(t, json.Unmarshal(root2["input"], &input))
			require.Len(t, input, 3)
			require.JSONEq(t, string(native), string(input[1]), "must retain id and every field, including unknown provider metadata")
			result, _ := parseObject(input[2])
			require.Equal(t, "call_native_1", stringValue(result["call_id"]))
			require.Equal(t, "18°C", stringValue(result["output"]))
			retry, err := Prepare(ctx, follow, "session-a", store)
			require.NoError(t, err)
			require.Equal(t, r2.Turn, retry.Turn, "network retry must not advance iteration")
			_, err = Prepare(ctx, follow, "session-b", store)
			require.ErrorContains(t, err, "不存在")
			_, err = Prepare(ctx, follow, "session-a", memoryStore{})
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
	r2, err := Prepare(ctx, encoded(map[string]any{"tools": json.RawMessage(weatherTool), "input": input}), "session-a", store)
	require.NoError(t, err)
	require.Equal(t, r.Turn.ID, r2.Turn.ID)
	root, _ := parseObject(r2.Body)
	var restored []json.RawMessage
	require.NoError(t, json.Unmarshal(root["input"], &restored))
	require.Len(t, restored, 4)
	input = append(input, map[string]any{"role": "user", "content": "Now Osaka"})
	r3, err := Prepare(ctx, encoded(map[string]any{"tools": json.RawMessage(weatherTool), "input": input}), "session-a", store)
	require.NoError(t, err)
	require.NotEqual(t, r.Turn.ID, r3.Turn.ID)
	require.Zero(t, r3.Turn.Iteration)
}

func TestNamespaceCustomAndUndeclaredOrExecutablePayload(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	raw := []byte(`{"input":"hello","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"patch","description":"Apply patch"}]}]}`)
	r, err := Prepare(ctx, raw, "scope", store)
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
	bad, _ := parseObject(officeItem("functions.patch", "text", false))
	bad["arguments"] = encoded(`{"code":"Excel.run(() => doSomething())"}`)
	_, err = r.convertCall(ctx, encoded(bad))
	require.ErrorContains(t, err, "不执行代码")
	bad["name"] = encoded("write_range")
	_, err = r.convertCall(ctx, encoded(bad))
	require.ErrorContains(t, err, "非桥接工具")
}

func TestRequestBoundaries(t *testing.T) {
	for _, raw := range []string{
		`{"tools":[],"input":[],"agent_iteration":64}`,
		`{"tools":[],"input":[],"agent_iteration":-1}`,
		`{"tools":[],"input":[],"agent_iteration":null}`,
		`{"tools":[],"input":null}`,
		`{"tools":[],"input":[],"turn_id":null}`,
		`{"tools":[],"input":[],"tool_choice":"required"}`,
		`{"tools":[],"input":[{"type":"function_call_output","call_id":"foreign","output":"x"}]}`,
	} {
		_, err := Prepare(context.Background(), []byte(raw), "x", memoryStore{})
		require.Error(t, err, raw)
	}
	_, err := Prepare(context.Background(), []byte(`{"tools":`+weatherTool+`,"input":[]}`), "x", nil)
	require.ErrorContains(t, err, "KV")
	for _, choice := range []string{`"none"`, `{"type":"function","name":"get_weather"}`} {
		r, err := Prepare(context.Background(), []byte(`{"tools":`+weatherTool+`,"input":[],"tool_choice":`+choice+`}`), "x", memoryStore{})
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
