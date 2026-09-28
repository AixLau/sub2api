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

func TestFunctionPayloadExplicitIndentPreservesSource(t *testing.T) {
	// Production #489926: the first source line was more indented than a
	// later closing brace. Implicit YAML indentation treated it as a new key.
	broken := "old_string: |-\n      privateWork();\n    }\n"
	_, err := parseFunctionPayload([]byte(broken))
	require.ErrorContains(t, err, "解析器报告第")
	require.ErrorContains(t, err, "|2-")
	require.NotContains(t, err.Error(), "privateWork")
	var failure *ToolCallError
	require.ErrorAs(t, err, &failure)
	feedback := string(encoded(toolFailureResult(failure)))
	require.Contains(t, feedback, "|2-")
	require.NotContains(t, feedback, "privateWork")

	for _, tc := range []struct{ header, source string }{
		{"|2-", "    work();\n  }"},
		{"|2-", "    first();\n    second();"},
		{"|2", "    work();\n  }\n"},
		{"|2+", "    work();\n  }\n\n"},
	} {
		t.Run(tc.header+"/"+tc.source, func(t *testing.T) {
			literal := tc.header + "\n  " + strings.ReplaceAll(tc.source, "\n", "\n  ")
			code := "old_string: " + literal
			raw, err := parseFunctionPayload([]byte(code))
			require.NoError(t, err)
			args, err := parseObject(raw)
			require.NoError(t, err)
			require.Equal(t, tc.source, stringValue(args["old_string"]))
			// The same rule applies to nested mappings, relative to the key.
			nested := "edit:\n  old_string: " + strings.ReplaceAll(literal, "\n", "\n  ")
			raw, err = parseFunctionPayload([]byte(nested))
			require.NoError(t, err)
			args, err = parseObject(raw)
			require.NoError(t, err)
			edit, err := parseObject(args["edit"])
			require.NoError(t, err)
			require.Equal(t, tc.source, stringValue(edit["old_string"]))
			// Native history must preserve source indentation and chomping too.
			want := encoded(map[string]any{"old_string": tc.source, "nested": map[string]string{"content": tc.source}})
			history, err := formatFunctionPayload(want)
			require.NoError(t, err)
			restored, err := parseFunctionPayload([]byte(history))
			require.NoError(t, err)
			require.JSONEq(t, string(want), string(restored))
		})
	}
}

func TestFunctionPayloadEditFeedbackExecutesOnlyCorrectedBytes(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "JSON", true: "SSE"}[streaming], func(t *testing.T) {
			ctx := context.Background()
			tools := []any{map[string]any{"type": "function", "name": "Edit", "parameters": map[string]any{"type": "object", "properties": map[string]any{"old_string": map[string]string{"type": "string"}, "new_string": map[string]string{"type": "string"}}}}}
			r, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": "edit the source"}), "edit-session", memoryStore{}, nil, 256<<20)
			require.NoError(t, err)
			require.False(t, r.canDeliverToolFailures(), "ordinary function-only client like #489926")
			bad := feedbackCall("Edit", "old_string: |-\n      before();\n    }\n", "invalid_indent")
			corrected := feedbackCall("Edit", "old_string: |2-\n      before();\n    }\nnew_string: |2-\n      after();\n    }\n", "explicit_indent")
			hits := 0
			r.Feedback = func(_ context.Context, body []byte) ([]byte, error) {
				hits++
				root, err := parseObject(body)
				require.NoError(t, err)
				var input []object
				require.NoError(t, json.Unmarshal(root["input"], &input))
				result := input[len(input)-1]
				require.Equal(t, "function_call_output", stringValue(result["type"]))
				require.Contains(t, stringValue(result["output"]), "|2-")
				require.Contains(t, stringValue(result["output"]), "解析器报告第")
				return encoded(feedbackResponse("resp_fixed", corrected)), nil
			}
			var final object
			if streaming {
				var wire strings.Builder
				err = r.Stream(ctx, strings.NewReader(event("response.completed", map[string]any{"response": feedbackResponse("resp_edit", bad)})), func(b []byte) error { wire.Write(b); return nil })
				require.NoError(t, err)
				final = streamSnapshot(t, wire.String())
			} else {
				out, err := r.Response(ctx, encoded(feedbackResponse("resp_edit", bad)))
				require.NoError(t, err)
				final, err = parseObject(out)
				require.NoError(t, err)
			}
			require.Equal(t, 1, hits)
			var output []object
			require.NoError(t, json.Unmarshal(final["output"], &output))
			require.Len(t, output, 1, "the rejected Edit must never execute")
			args, err := parseObject([]byte(stringValue(output[0]["arguments"])))
			require.NoError(t, err)
			file := filepath.Join(t.TempDir(), "example.js")
			require.NoError(t, os.WriteFile(file, []byte("function example() {\n    before();\n  }\n"), 0600))
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			old, replacement := stringValue(args["old_string"]), stringValue(args["new_string"])
			require.Equal(t, "    before();\n  }", old)
			require.Equal(t, "    after();\n  }", replacement)
			require.Equal(t, 1, strings.Count(string(source), old))
			require.NoError(t, os.WriteFile(file, []byte(strings.Replace(string(source), old, replacement, 1)), 0600))
			updated, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, "function example() {\n    after();\n  }\n", string(updated))
		})
	}
}

func TestFunctionPayloadHistoryUnicodeEscapes(t *testing.T) {
	for name, raw := range map[string]string{
		"surrogate_pairs":      "{\"command\":\"printf '\\ud83d\\ude80'\",\"nested\":[{\"\\uD834\\uDD1E\":\"\\uDBFF\\uDFFF\"}]}",
		"literal_escapes":      "{\"command\":\"printf '\\\\ud83d\\\\ude80'\"}",
		"unicode_and_controls": "{\"text\":\"中文 🚀\\u0000\\t\\r\\n\\u2028\\u2029\"}",
	} {
		t.Run(name, func(t *testing.T) {
			code, err := formatFunctionPayload([]byte(raw))
			require.NoError(t, err)
			restored, err := parseFunctionPayload([]byte(code))
			require.NoError(t, err)
			require.JSONEq(t, raw, string(restored))
		})
	}
}

func TestPrepareFunctionHistoryWithSurrogatePairs(t *testing.T) {
	const arguments = "{\"command\":\"printf '\\ud83d\\ude80'\",\"timeout_ms\":1000}"
	for _, args := range []any{arguments, json.RawMessage(arguments)} {
		raw := encoded(map[string]any{
			"model": "gpt-5.6-luna",
			"tools": []any{map[string]any{"type": "function", "name": "terminal", "parameters": map[string]any{"type": "object"}}},
			"input": []any{
				messageItem("user", "Run the command"),
				map[string]any{"type": "function_call", "call_id": "call_unicode", "name": "terminal", "arguments": args},
				map[string]any{"type": "function_call_output", "call_id": "call_unicode", "output": "🚀"},
			},
		})
		request, err := Prepare(context.Background(), raw, "unicode-history", memoryStore{}, nil, 256<<20)
		require.NoError(t, err)
		items := preparedInput(t, request)
		require.Len(t, items, 4)
		call, err := parseObject(items[2])
		require.NoError(t, err)
		require.Equal(t, "run_officejs", stringValue(call["name"]))
		outer, err := parseObject([]byte(stringValue(call["arguments"])))
		require.NoError(t, err)
		restored, err := parseFunctionPayload([]byte(stringValue(outer["code"])))
		require.NoError(t, err)
		require.JSONEq(t, arguments, string(restored))
	}
}
