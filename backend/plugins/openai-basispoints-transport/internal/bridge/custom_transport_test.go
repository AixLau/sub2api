package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func customRequest(t *testing.T) *Request {
	t.Helper()
	r, err := Prepare(context.Background(), encoded(map[string]any{"input": "run tool", "tools": []any{
		map[string]any{"type": "namespace", "name": "functions", "tools": []any{
			map[string]any{"type": "custom", "name": "exec"},
			map[string]any{"type": "function", "name": "read_file"},
		}},
	}}), "custom-session", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	return r
}

func rawCustomItem(outer object) json.RawMessage {
	return encoded(object{"type": encoded("function_call"), "id": encoded("fc_raw"), "call_id": encoded("call_raw"),
		"name": encoded("run_officejs"), "arguments": encoded(string(encoded(outer))), "status": encoded("completed")})
}

func TestRawCustomTransportPreservesInputAndReplay(t *testing.T) {
	ctx := context.Background()
	for _, source := range []string{
		"const text = \"中文\";\n// literal backslash quote: \\'\r\n\ttext;",
		"*** Begin Patch\n*** Add File: example.txt\n+quotes \" ' and \\t\n*** End Patch",
		string(encoded(map[string]any{"tool": "functions.read_file", "args": map[string]any{"path": "not-an-envelope"}})),
		"",
	} {
		r := customRequest(t)
		native := rawCustomItem(object{"summary": encoded("Run a client command"), "references": encoded([]string{}), "code": encoded(string(encoded(object{"name": encoded("functions.exec"), "input": encoded(source)})))})
		response := map[string]any{"id": "resp_raw", "status": "completed", "output": []any{native}}
		assertCall := func(raw json.RawMessage, complete bool) object {
			call, err := parseObject(raw)
			require.NoError(t, err)
			require.Equal(t, "custom_tool_call", stringValue(call["type"]))
			require.Equal(t, "exec", stringValue(call["name"]))
			require.Equal(t, "functions", stringValue(call["namespace"]))
			if complete {
				require.Equal(t, source, stringValue(call["input"]))
			}
			return call
		}
		out, err := r.Response(ctx, encoded(response))
		require.NoError(t, err)
		root, _ := parseObject(out)
		var calls []json.RawMessage
		require.NoError(t, json.Unmarshal(root["output"], &calls))
		require.Len(t, calls, 1)
		call := assertCall(calls[0], true)
		replayed, err := Prepare(ctx, encoded(map[string]any{"input": []any{map[string]any{
			"type": "custom_tool_call_output", "call_id": stringValue(call["call_id"]), "output": "tool result",
		}}}), "custom-session", r.store, nil, 256<<20)
		require.NoError(t, err)
		require.JSONEq(t, string(native), string(preparedInput(t, replayed)[1]))

		// A complete item must agree with its stream and terminal snapshot.
		r = customRequest(t)
		wire := event("response.output_item.done", map[string]any{"output_index": 0, "item": native}) +
			event("response.completed", map[string]any{"response": response})
		var delta strings.Builder
		var completedItems int
		require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func(frame []byte) error {
			for _, line := range strings.Split(string(frame), "\n") {
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				e, err := parseObject([]byte(strings.TrimPrefix(line, "data: ")))
				require.NoError(t, err)
				switch stringValue(e["type"]) {
				case "response.output_item.added":
					assertCall(e["item"], false)
				case "response.custom_tool_call_input.delta":
					delta.WriteString(stringValue(e["delta"]))
				case "response.custom_tool_call_input.done":
					require.Equal(t, source, stringValue(e["input"]))
				case "response.output_item.done":
					assertCall(e["item"], true)
					completedItems++
				case "response.completed":
					root, _ := parseObject(e["response"])
					require.NoError(t, json.Unmarshal(root["output"], &calls))
					assertCall(calls[0], true)
					completedItems++
				}
			}
			return nil
		}))
		require.Equal(t, source, delta.String())
		require.Equal(t, 2, completedItems)

		// A client call without a KV record uses the same raw protocol.
		call["call_id"] = encoded("foreign_call")
		rebuiltRaw, err := rebuildTransportCall(call, "custom_tool_call", "functions.exec")
		require.NoError(t, err)
		rebuilt, _ := parseObject(rebuiltRaw)
		outer, err := parseObject([]byte(stringValue(rebuilt["arguments"])))
		require.NoError(t, err)
		require.JSONEq(t, `[]`, string(outer["references"]))
		envelope, err := parseObject([]byte(stringValue(outer["code"])))
		require.NoError(t, err)
		require.Equal(t, source, stringValue(envelope["input"]))
	}
}

func TestRawCustomTransportRejectsInvalidReferencesAndKinds(t *testing.T) {
	for _, refs := range []any{nil, "functions.exec", []string{}, []string{"functions.exec", "functions.read_file"},
		[]any{nil}, []any{42}, []string{"functions.exec extra"}, []string{"undeclared"}, []string{"functions.read_file"},
		[]string{"run_officejs"}, []string{"functions.run_officejs"}, []string{""},
	} {
		r := customRequest(t)
		call, err := r.convertCall(context.Background(), rawCustomItem(object{"references": encoded(refs), "code": encoded("private raw source")}))
		require.Error(t, err, refs)
		var failure *ToolCallError
		require.ErrorAs(t, err, &failure)
		require.Nil(t, call)
		require.NotContains(t, err.Error(), "private raw source")
		require.Empty(t, r.converted)
	}
	for _, code := range []any{nil, 42, map[string]any{"input": "private"}} {
		r := customRequest(t)
		_, err := r.convertCall(context.Background(), rawCustomItem(object{"references": encoded([]string{"functions.exec"}), "code": encoded(code)}))
		require.Error(t, err)
	}
}

func TestObsoleteTransportFormatsCannotSelectTools(t *testing.T) {
	for _, summary := range []string{"Run client tool functions.exec", "sub2api.custom/functions.exec", "Run a command"} {
		for _, code := range []string{"text(await tools.exec_command({cmd: \"pwd\"}));", `{"tool":"functions.read_file","args":{"path":"x"}}`} {
			r := customRequest(t)
			call, err := r.convertCall(context.Background(), rawCustomItem(object{"summary": encoded(summary), "code": encoded(code)}))
			require.Error(t, err)
			require.Nil(t, call)
			require.Empty(t, r.converted)
		}
	}
}
