package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQualifiedCallName(t *testing.T) {
	for _, tc := range []struct{ name, namespace, want string }{
		{"run_officejs", "", "run_officejs"},
		{"run_officejs", "functions", "functions.run_officejs"},
		{"functions.run_officejs", "functions", "functions.run_officejs"},
		{"functions.functions.run_officejs", "functions", "functions.functions.run_officejs"},
		{"exec", "functions", "functions.exec"},
		{"functions.exec", "functions", "functions.exec"},
		{"functions.exec", "other", "other.functions.exec"},
		{"functions", "functions", "functions.functions"},
		{"", "functions", ""},
	} {
		require.Equal(t, tc.want, qualifiedCallName(object{"name": encoded(tc.name), "namespace": encoded(tc.namespace)}))
	}
}

func TestQualifiedCallsKeepStreamAndReplayIdentity(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"transport", "direct_function", "direct_custom"} {
		t.Run(kind, func(t *testing.T) {
			r := customRequest(t)
			var original json.RawMessage
			item, _ := parseObject(officeItem("functions.exec", "text('hello')", false))
			item["name"], item["namespace"] = encoded("functions.run_officejs"), encoded("functions")
			wantName, wantType, payloadField, payload := "exec", "custom_tool_call", "input", "text('hello')"
			if kind == "direct_function" {
				wantName, wantType, payloadField, payload = "read_file", "function_call", "arguments", `{"path":"example.txt"}`
				item["name"] = encoded("functions.read_file")
				item["arguments"] = encoded(payload)
			} else if kind == "direct_custom" {
				item["type"], item["name"], item["input"] = encoded(wantType), encoded("functions.exec"), encoded(payload)
				delete(item, "arguments")
			}
			original = encoded(item)
			response := map[string]any{"id": "resp_qualified", "status": "completed", "output": []any{original}}
			wire := event("response.output_item.done", map[string]any{"item": original}) + event("response.completed", map[string]any{"response": response})
			var out strings.Builder
			require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func(b []byte) error { out.Write(b); return nil }))
			require.False(t, r.Failed, out.String())
			require.Len(t, r.converted, 1)
			for _, converted := range r.converted {
				call, _ := parseObject(converted)
				require.Equal(t, wantName, stringValue(call["name"]))
				require.Equal(t, "functions", stringValue(call["namespace"]))
				require.Equal(t, wantType, stringValue(call["type"]))
				require.Equal(t, payload, stringValue(call[payloadField]))
				follow := encoded(map[string]any{"input": []any{map[string]any{"type": wantType + "_output", "call_id": stringValue(call["call_id"]), "output": "result"}}})
				replayed, err := Prepare(ctx, follow, "custom-session", r.store, nil)
				require.NoError(t, err)
				require.JSONEq(t, string(original), string(preparedInput(t, replayed)[1]))
			}
			result, err := customRequest(t).Response(ctx, encoded(response))
			require.NoError(t, err)
			root, _ := parseObject(result)
			var calls []object
			require.NoError(t, json.Unmarshal(root["output"], &calls))
			require.Equal(t, wantName, stringValue(calls[0]["name"]))
			if wantType == "function_call" {
				require.JSONEq(t, `[]`, string(calls[0]["encrypted_function_args"]))
			}
		})
	}
}

func TestQualifiedForeignHistoryAndToolChoice(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"exec", "functions.exec"} {
		tools := []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec"}}}}
		input := []any{map[string]any{"type": "custom_tool_call", "name": name, "namespace": "functions", "call_id": "call_original", "input": "text('hello')"}}
		body := encoded(map[string]any{"tools": tools, "input": input, "tool_choice": map[string]any{"type": "custom", "name": name, "namespace": "functions"}})
		r, err := Prepare(ctx, body, "session", memoryStore{}, nil)
		require.NoError(t, err)
		require.Equal(t, "functions.exec", r.catalog.forced)
		native, _ := parseObject(preparedInput(t, r)[1])
		require.Equal(t, "run_officejs", stringValue(native["name"]))
		args, _ := parseObject([]byte(stringValue(native["arguments"])))
		require.JSONEq(t, `[]`, string(args["references"]))
		envelope, err := parseObject([]byte(stringValue(args["code"])))
		require.NoError(t, err)
		require.Equal(t, "text('hello')", stringValue(envelope["input"]))
		call, err := r.convertCall(ctx, officeItem("functions.exec", "text('hello')", false))
		require.NoError(t, err)
		require.NotEmpty(t, call)
	}
}

func TestUnknownToolIdentityDiagnosticsAreSafe(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"functions.run_connector_action", "functions.functions.run_officejs", "private input\nsecret", strings.Repeat("x", 129)} {
		r := customRequest(t)
		item := object{"type": encoded("function_call"), "name": encoded(name), "namespace": encoded("functions"), "id": encoded("private_id"), "call_id": encoded("private_call_id"),
			"arguments": encoded(`{"summary":"private_summary","code":"private_code","references":["private_reference"]}`)}
		converted, err := r.convertCall(ctx, encoded(item))
		require.Error(t, err)
		require.Nil(t, converted)
		require.Empty(t, r.converted)
		failed, _ := parseObject(FailureResponse("TOOL_BRIDGE_CALL_INVALID", fmt.Errorf("wrapped: %w", err), nil))
		failure, _ := parseObject(failed["error"])
		diagnostics, _ := parseObject(failure["diagnostics"])
		require.Equal(t, "upstream_tool_identity", stringValue(diagnostics["stage"]))
		require.Equal(t, diagnosticIdentifier(name), stringValue(diagnostics["name"]))
		require.Equal(t, "functions", stringValue(diagnostics["namespace"]))
		require.Equal(t, "2", string(diagnostics["catalog_tools"]))
		wire := event("response.created", map[string]any{"response": map[string]any{"id": "resp_failed", "output": []any{}}}) + event("response.output_item.done", map[string]any{"item": item})
		var out strings.Builder
		require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func(b []byte) error { out.Write(b); return nil }))
		require.True(t, r.Failed)
		require.Equal(t, 1, strings.Count(out.String(), "event: response.failed"))
		require.Contains(t, out.String(), "upstream_tool_identity")
		require.NotContains(t, out.String(), "event: response.output_item.done")
		for _, sensitive := range []string{"private input", "secret", "private_summary", "private_code", "private_reference", "private_id", "private_call_id"} {
			require.NotContains(t, string(encoded(failure)), sensitive)
			require.NotContains(t, out.String(), sensitive)
		}
	}
}
