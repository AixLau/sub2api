package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvelopeTransportSeparatesRoutingFromPayload(t *testing.T) {
	for _, tc := range []struct {
		name, payload, typ, field string
	}{
		{"functions.read_file", `{"path":"Tokyo's\\file","id":9007199254740993}`, "function_call", "arguments"},
		{"functions.exec", "text(await tools.exec_command({cmd: \"printf 'hello'\"}));\n", "custom_tool_call", "input"},
		{"functions.exec", `{"tool":"functions.read_file","args":{"path":"literal input"}}`, "custom_tool_call", "input"},
		{"functions.exec", "", "custom_tool_call", "input"},
	} {
		t.Run(tc.name+"/"+tc.payload, func(t *testing.T) {
			for _, finalOnly := range []bool{false, true} {
				r := customRequest(t)
				item := rawCustomItem(object{"summary": encoded("Inspect the repository"), "references": encoded([]string{"unrelated sheet address"}), "code": encoded(string(envelopeForTest(tc.name, tc.field, tc.payload)))})
				response := map[string]any{"id": "resp_reference", "status": "completed", "output": []any{item}}
				wire := event("response.completed", map[string]any{"response": response})
				if !finalOnly {
					wire = event("response.output_item.done", map[string]any{"output_index": 0, "item": item}) + wire
				}
				var out strings.Builder
				require.NoError(t, r.Stream(context.Background(), strings.NewReader(wire), func(b []byte) error { out.Write(b); return nil }))
				require.False(t, r.Failed, out.String())
				require.Len(t, r.converted, 1)
				for _, raw := range r.converted {
					call, err := parseObject(raw)
					require.NoError(t, err)
					require.Equal(t, tc.typ, stringValue(call["type"]))
					require.Equal(t, tc.payload, stringValue(call[tc.field]))
				}
				require.Equal(t, 1, strings.Count(out.String(), "event: response.output_item.done"))
				require.NotContains(t, out.String(), "run_officejs")
				converted, err := customRequest(t).Response(context.Background(), encoded(response))
				require.NoError(t, err)
				root, _ := parseObject(converted)
				var calls []object
				require.NoError(t, json.Unmarshal(root["output"], &calls))
				require.Equal(t, tc.payload, stringValue(calls[0][tc.field]))
			}
		})
	}
}

func TestEnvelopeTransportSummaryIsNotRouting(t *testing.T) {
	for _, summary := range []string{"", "sub2api.custom/functions.exec", "Run client tool functions.exec"} {
		r := customRequest(t)
		item := rawCustomItem(object{"summary": encoded(summary), "references": encoded([]string{"functions.exec"}), "code": encoded(string(envelopeForTest("functions.read_file", "arguments", `{"path":"example.txt"}`)))})
		call, err := r.convertCall(context.Background(), item)
		require.NoError(t, err)
		obj, _ := parseObject(call)
		require.Equal(t, "function_call", stringValue(obj["type"]))
		require.Equal(t, "read_file", stringValue(obj["name"]))
	}
}

func TestEnvelopeTransportExecutorNames(t *testing.T) {
	for _, tc := range []struct {
		name, namespace string
		allowed         bool
	}{
		{"run_officejs", "", true},
		{"functions.run_officejs", "", true},
		{"run_officejs", "functions", true},
		{"functions.run_officejs", "functions", true},
		{"run_officejs", "other", false},
		{"run_connector_action", "", false},
	} {
		r := customRequest(t)
		item, _ := parseObject(officeItem("functions.exec", "text('ok')", false))
		item["name"], item["namespace"] = encoded(tc.name), encoded(tc.namespace)
		call, err := r.convertCall(context.Background(), encoded(item))
		if tc.allowed {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, "传输执行器")
			require.Nil(t, call)
		}
	}
}

func TestTransportDiagnosticsNeverIncludePayload(t *testing.T) {
	for _, field := range []string{"arguments", "code"} {
		for _, payload := range []string{`{"secret":"private\'value"}`, `{"secret":"private"} trailing`} {
			_, err := parseToolObject([]byte(payload), field)
			require.ErrorContains(t, err, field)
			require.ErrorContains(t, err, "字节偏移")
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "secret")
		}
	}
}

func TestEnvelopeTransportHistoryPreservesJSONNumbers(t *testing.T) {
	const arguments = `{"id":9007199254740993,"amount":1.234567890123456789,"nested":{"code":"literal"}}`
	for _, raw := range []json.RawMessage{encoded(arguments), json.RawMessage(arguments)} {
		item := object{"call_id": encoded("call_history"), "arguments": raw}
		rebuilt, err := rebuildTransportCall(item, "function_call", "functions.read_file")
		require.NoError(t, err)
		native, _ := parseObject(rebuilt)
		outer, _ := parseObject([]byte(stringValue(native["arguments"])))
		envelope, err := parseObject([]byte(stringValue(outer["code"])))
		require.NoError(t, err)
		require.Equal(t, arguments, string(envelope["arguments"]))
		require.JSONEq(t, `[]`, string(outer["references"]))
	}
	for _, raw := range []json.RawMessage{nil, encoded("invalid private input"), encoded(nil), encoded([]any{}), encoded(42)} {
		for _, typ := range []string{"function_call", "custom_tool_call"} {
			if typ == "custom_tool_call" && isTextValue(raw) {
				continue // Any string, including non-JSON, is valid custom input.
			}
			rebuilt, err := rebuildTransportCall(object{"arguments": raw, "input": raw}, typ, "functions.exec")
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			require.Nil(t, rebuilt)
		}
	}
}

func envelopeForTest(name, field, payload string) json.RawMessage {
	value := encoded(payload)
	if field == "arguments" {
		value = json.RawMessage(payload)
	}
	return encoded(object{"name": encoded(name), field: value})
}

func TestCatalogAdvertisesEnvelopeTransport(t *testing.T) {
	r := customRequest(t)
	prompt := r.catalog.prompt()
	require.Contains(t, prompt, "protocol v4")
	require.Contains(t, prompt, "Set references=[]")
	require.Contains(t, prompt, "references and summary do not select a tool")
	require.Contains(t, prompt, `name="functions.exec"`)
	require.Contains(t, prompt, `name="functions.read_file"`)
	require.Contains(t, prompt, "through that parent tool")
	require.NotContains(t, prompt, "example.exec")
	require.Equal(t, prompt, r.catalog.prompt())
}

func TestEnvelopeNamespaceAndKinds(t *testing.T) {
	for _, payload := range []string{
		`{"name":"exec","namespace":"functions","input":"text('ok')"}`,
		`{"name":"functions.exec","input":"text('ok')"}`,
		`{"name":"exec","input":"text('ok')"}`,
	} {
		call, err := customRequest(t).convertCall(context.Background(), rawCustomItem(object{"code": encoded(payload)}))
		require.NoError(t, err)
		item, _ := parseObject(call)
		require.Equal(t, "functions", stringValue(item["namespace"]))
	}
	for _, payload := range []string{
		`{"name":"functions.exec","arguments":{}}`,
		`{"name":"functions.exec","input":null}`,
		`{"name":"functions.read_file","arguments":"{}"}`,
		`{"name":"functions.read_file","arguments":{},"input":"x"}`,
		`{"name":"functions.exec","input":"x","tool":"other"}`,
		`{"name":"run_officejs","arguments":{}}`,
		`{"name":"exec","namespace":"missing","input":"x"}`,
	} {
		call, err := customRequest(t).convertCall(context.Background(), rawCustomItem(object{"code": encoded(payload)}))
		require.Error(t, err, payload)
		require.Nil(t, call)
	}
}
