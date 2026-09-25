package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidEnvelopeNeverEscapesToClient(t *testing.T) {
	for _, code := range []string{
		"{\"tool\":\"get_weather\",\"args\":{\"city\":\"Tokyo\\'s\"}}",
		"{\"tool\":\"missing\",\"args\":{}}",
		"{\"tool\":\"get_weather\",\"args\":\"wrong type\"}",
		"Excel.run(() => doSomething())",
	} {
		for _, finalOnly := range []bool{false, true} {
			r := prepareWeather(t, memoryStore{})
			item, _ := parseObject(officeItem("get_weather", map[string]any{}, false))
			item["arguments"] = encoded(string(encoded(object{"code": encoded(code)})))
			frame := event("response.output_item.done", map[string]any{"item": item})
			if finalOnly {
				frame = event("response.completed", map[string]any{"response": map[string]any{"output": []any{item}}})
			}
			var out strings.Builder
			err := r.Stream(context.Background(), strings.NewReader(frame), func(b []byte) error { out.Write(b); return nil })
			var toolErr *ToolCallError
			require.ErrorAs(t, err, &toolErr)
			require.Empty(t, out.String())
			require.Empty(t, r.converted)
		}
	}
}

func TestCodexCustomExecEnvelopeAndDirectCall(t *testing.T) {
	ctx := context.Background()
	raw := []byte("{\"input\":\"check status\",\"tools\":[{\"type\":\"namespace\",\"name\":\"functions\",\"tools\":[{\"type\":\"custom\",\"name\":\"exec\"}]}]}")
	source := "text(await tools.exec_command({cmd: \"printf 'hello'\"}));\n"
	for _, direct := range []bool{false, true} {
		store := memoryStore{}
		r, err := Prepare(ctx, raw, "s", store, nil)
		require.NoError(t, err)
		item := officeItem("functions.exec", source, false)
		if direct {
			item = encoded(map[string]any{"type": "custom_tool_call", "id": "ctc_exec", "call_id": "call_exec", "name": "exec", "namespace": "functions", "input": source})
		}
		stream := event("response.output_item.added", map[string]any{"item": json.RawMessage(item)}) +
			event("response.custom_tool_call_input.delta", map[string]any{"delta": "must not leak"}) +
			event("response.output_item.done", map[string]any{"item": json.RawMessage(item)}) +
			event("response.completed", map[string]any{"response": map[string]any{"output": []json.RawMessage{item}, "usage": map[string]int{"total_tokens": 7}}})
		var out strings.Builder
		require.NoError(t, r.Stream(ctx, strings.NewReader(stream), func(b []byte) error { out.Write(b); return nil }))
		require.NotContains(t, out.String(), "run_officejs")
		require.NotContains(t, out.String(), "must not leak")
		require.Equal(t, 1, strings.Count(out.String(), "event: response.custom_tool_call_input.delta"))
		require.Contains(t, out.String(), "\"namespace\":\"functions\"")
		require.Len(t, r.converted, 1)
		for _, converted := range r.converted {
			call, _ := parseObject(converted)
			require.Equal(t, source, stringValue(call["input"]))
			follow := encoded(map[string]any{"tools": json.RawMessage("[{\"type\":\"namespace\",\"name\":\"functions\",\"tools\":[{\"type\":\"custom\",\"name\":\"exec\"}]}]"), "input": []any{map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(call["call_id"]), "output": "ok"}}})
			restored, err := Prepare(ctx, follow, "s", store, nil)
			require.NoError(t, err)
			require.Equal(t, r.Turn.ID, restored.Turn.ID)
		}
	}
}
