package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFunctionPayloadLiteralStringsExecuteAndReplay(t *testing.T) {
	patterns := []string{`info\[['\"]path['\"]\]`, `DATA_PATH|root_path|Path\(|pickle\.dump`, `(write_text|pickle\.dump|json\.dump|\.tofile|np\.save)`}
	file := filepath.Join(t.TempDir(), "sample.py")
	require.NoError(t, os.WriteFile(file, []byte("info['path'] = 'first'\nPath('second')\nnp.save('third')\n"), 0600))
	for _, pattern := range patterns {
		for _, streaming := range []bool{false, true} {
			t.Run(pattern+map[bool]string{false: "/JSON", true: "/SSE"}[streaming], func(t *testing.T) {
				ctx := context.Background()
				toolSchema := []any{map[string]any{"type": "function", "name": "search_content", "parameters": map[string]any{"type": "object", "properties": map[string]any{"pattern": map[string]any{"type": "string"}, "path": map[string]any{"type": "string"}}}}}
				r, err := Prepare(ctx, encoded(map[string]any{"tools": toolSchema, "input": "search the file"}), "regex-session", memoryStore{}, nil, 256<<20)
				require.NoError(t, err)
				r.Feedback = func(context.Context, []byte) ([]byte, error) {
					t.Fatal("literal payload must not require correction inference")
					return nil, nil
				}
				code := "pattern: |-\n  " + pattern + "\npath: '" + file + "'\ncontextAround: 2\ncaseSensitive: true\n"
				call := rawCustomItem(object{"references": encoded([]string{clientToolReferencePrefix + "search_content"}), "code": encoded(code)})
				response := map[string]any{"id": "resp_literal", "status": "completed", "output": []json.RawMessage{call}}
				var out []byte
				if streaming {
					var wire strings.Builder
					require.NoError(t, r.Stream(ctx, strings.NewReader(event("response.completed", map[string]any{"response": response})), func(b []byte) error { wire.Write(b); return nil }))
					for _, line := range strings.Split(wire.String(), "\n") {
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						e, _ := parseObject([]byte(strings.TrimPrefix(line, "data: ")))
						if stringValue(e["type"]) == "response.completed" {
							out = e["response"]
						}
					}
				} else {
					var err error
					out, err = r.Response(ctx, encoded(response))
					require.NoError(t, err)
				}
				root, err := parseObject(out)
				require.NoError(t, err)
				var items []object
				require.NoError(t, json.Unmarshal(root["output"], &items))
				require.Len(t, items, 1)
				args, err := parseObject([]byte(stringValue(items[0]["arguments"])))
				require.NoError(t, err)
				require.Equal(t, pattern, stringValue(args["pattern"]), "regex bytes must not be repaired or unescaped")
				result, err := exec.Command("rg", "-e", stringValue(args["pattern"]), "--", stringValue(args["path"])).Output()
				require.NoError(t, err)
				require.NotEmpty(t, result)
				follow := encoded(map[string]any{"tools": toolSchema, "input": []any{map[string]any{"type": "function_call_output", "call_id": stringValue(items[0]["call_id"]), "output": string(result)}}})
				restored, err := Prepare(ctx, follow, r.scope, r.store, nil, 256<<20)
				require.NoError(t, err)
				require.JSONEq(t, string(call), string(preparedInput(t, restored)[1]), "BPS replay must retain the original carrier")
			})
		}
	}
}

func TestFunctionPayloadJSONTypesAndRejections(t *testing.T) {
	raw, err := parseFunctionPayload([]byte("path: 'C:\\work\\new\\test'\nregex: |-\n  \\bword\\s+\\w+\nlarge: 123456789012345678901234567890\nfraction: 1.234567890123456789\nlist: [true, null, 'false', {}]\n"))
	require.NoError(t, err)
	require.JSONEq(t, `{"path":"C:\\work\\new\\test","regex":"\\bword\\s+\\w+","large":123456789012345678901234567890,"fraction":1.234567890123456789,"list":[true,null,"false",{}]}`, string(raw))
	require.Contains(t, string(raw), "123456789012345678901234567890")
	require.Contains(t, string(raw), "1.234567890123456789")
	for _, input := range []string{
		`{"pattern":"private\s+pattern"}`,
		"null", "[]", "payload", "a: 1\na: 2", "a: 1\n---\nb: 2", "a: &x [1]\nb: *x", "a: !!str 1", "a: !private secret", "a: .nan", "a: .inf", "a: 0xff", "a: 0123", "a: 2026-09-27", "1: value", "a: [",
	} {
		_, err := parseFunctionPayload([]byte(input))
		require.Error(t, err, "must reject %q", input)
		require.NotContains(t, err.Error(), "private")
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestFunctionPayloadLargeArguments(t *testing.T) {
	// Function mappings share the existing request/response byte budgets;
	// there is no additional small node cap on bulk client operations.
	raw, err := parseFunctionPayload([]byte("cells: [" + strings.Repeat("1,", 20000) + "2]"))
	require.NoError(t, err)
	var args struct{ Cells []int }
	require.NoError(t, json.Unmarshal(raw, &args))
	require.Len(t, args.Cells, 20001)
	require.Equal(t, 2, args.Cells[20000])
}

func TestFunctionPayloadHistoryRoundTrip(t *testing.T) {
	raw := []byte(`{"pattern":"\\bword\\s+\\w+","path":"C:\\work\\new\\test","quote":"it's a string","lines":"first\nsecond\n","control":"\u0000\t","large":123456789012345678901234567890,"fraction":1.234567890123456789,"exponent":1e+32,"nested":[true,null,"2026-09-27",{}]}`)
	code, err := formatFunctionPayload(raw)
	require.NoError(t, err)
	require.Contains(t, code, `\bword\s+\w+`)
	require.Contains(t, code, "it's a string")
	require.Contains(t, code, "|-")
	restored, err := parseFunctionPayload([]byte(code))
	require.NoError(t, err)
	var want, got object
	require.NoError(t, json.Unmarshal(raw, &want))
	require.NoError(t, json.Unmarshal(restored, &got))
	require.Equal(t, want, got)
}

func TestFunctionPayloadNumbersBeyondMachineRange(t *testing.T) {
	raw := []byte(`{"large":` + strings.Repeat("9", 400) + `,"huge":1e999,"small":1e-999,"string":"1e999"}`)
	code, err := formatFunctionPayload(raw)
	require.NoError(t, err)
	for _, payload := range [][]byte{raw, []byte(code)} {
		restored, err := parseFunctionPayload(payload)
		require.NoError(t, err)
		var want, got object
		require.NoError(t, json.Unmarshal(raw, &want))
		require.NoError(t, json.Unmarshal(restored, &got))
		require.Equal(t, want, got)
	}
}
