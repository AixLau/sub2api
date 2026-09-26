package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// This regression uses the failing request's model and a long script with
// quotes, Unicode, backslashes and newlines. Only an exact fixture may execute.
func TestRawCustomTransportLiveBPS(t *testing.T) {
	account := liveBPSAccountFromEnv(t)
	model := os.Getenv("BPS_DISCOVERY_LIVE_MODEL")
	if model == "" {
		model = "gpt-5.6-terra"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	source, expected := longScriptFixture()
	marker := "written-" + uuid.NewString()
	file := filepath.Join(t.TempDir(), "probe.txt")
	tools := []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec", "description": "Run raw JavaScript. tools.write_probe({content: string}) writes the supplied text to the test file and returns a verification marker. text(value) returns the result."}}}}
	input := []any{messageItem("user", "Run this exact JavaScript through the declared functions.exec tool, preserving every character. Do not abbreviate the supplied content. After the tool succeeds, reply only with the returned verification marker.\n\n"+source)}
	store := memoryStore{}
	scope := "live-raw-" + uuid.NewString()
	for round := 0; round < 3; round++ {
		r, err := Prepare(ctx, encoded(map[string]any{"model": model, "stream": true, "reasoning": map[string]string{"effort": "low"}, "tools": tools, "input": input}), scope, store, nil, 256<<20)
		require.NoError(t, err)
		final := liveBPSResponse(t, ctx, r, account)
		var output []json.RawMessage
		require.NoError(t, json.Unmarshal(final["output"], &output))
		count := 0
		finalText := ""
		for _, raw := range output {
			item, _ := parseObject(raw)
			input = append(input, raw)
			if !isToolCall(item) {
				if stringValue(item["type"]) == "message" {
					finalText += string(item["content"])
				}
				continue
			}
			require.Equal(t, "custom_tool_call", stringValue(item["type"]))
			require.Equal(t, source, strings.TrimSpace(stringValue(item["input"])), "the real model must preserve the exact long script")
			executeLongScript(t, ctx, stringValue(item["input"]), source, expected, file, marker)
			input = append(input, map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(item["call_id"]), "output": marker})
			count++
		}
		t.Logf("model=%s round=%d http=200 client_calls=%d script_bytes=%d usage=%s", model, round+1, count, len(source), string(final["usage"]))
		if count == 0 {
			require.Contains(t, finalText, marker)
			actual, err := os.ReadFile(file)
			require.NoError(t, err)
			require.Equal(t, expected, string(actual))
			return
		}
	}
	t.Fatal("probe exceeded three inference rounds")
}

func longScriptFixture() (string, string) {
	var document strings.Builder
	for i := 1; i <= 180; i++ {
		fmt.Fprintf(&document, "第 %03d 项：核对实验结果，保留 \"双引号\"、'单引号'、路径 C:\\research\\trial_%03d，以及制表符\t和换行。\n", i, i)
	}
	expected := document.String()
	source := "const document = " + string(encoded(expected)) + ";\ntext(await tools.write_probe({content: document}));"
	return source, expected
}

func executeLongScript(t *testing.T, ctx context.Context, program, source, expected, file, marker string) {
	t.Helper()
	require.Equal(t, source, strings.TrimSpace(program), "only the known fixture may execute")
	harness := "const fs=require('node:fs'); const source=fs.readFileSync(0,'utf8');const tools={write_probe:async p=>{fs.writeFileSync(process.argv[1],p.content);return process.argv[2];}};const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor; new AsyncFunction('tools','text',source)(tools,v=>process.stdout.write(v));"
	cmd := exec.CommandContext(ctx, "node", "-e", harness, file, marker)
	cmd.Stdin = strings.NewReader(program)
	result, err := cmd.Output()
	require.NoError(t, err)
	actual, err := os.ReadFile(file)
	require.NoError(t, err)
	require.Equal(t, expected, string(actual))
	require.Equal(t, marker, string(result))
}

func TestLongCustomScriptExecutesAndReplaysWithoutJSONRepair(t *testing.T) {
	ctx := context.Background()
	source, expected := longScriptFixture()
	require.Greater(t, len(source), 19813, "cover the first rejected call's size")
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			r := customRequest(t)
			r.Feedback = func(context.Context, []byte) ([]byte, error) {
				t.Fatal("raw scripts do not need JSON repair inference")
				return nil, nil
			}
			native := officeItem("functions.exec", source, false)
			response := feedbackResponse("long_script", native)
			var final object
			if stream {
				var wire strings.Builder
				require.NoError(t, r.Stream(ctx, strings.NewReader(event("response.completed", map[string]any{"response": response})), func(b []byte) error { wire.Write(b); return nil }))
				require.False(t, r.Failed)
				final = streamSnapshot(t, wire.String())
			} else {
				out, err := r.Response(ctx, encoded(response))
				require.NoError(t, err)
				final, _ = parseObject(out)
			}
			var calls []object
			require.NoError(t, json.Unmarshal(final["output"], &calls))
			require.Len(t, calls, 1)
			marker := "written-" + uuid.NewString()
			executeLongScript(t, ctx, stringValue(calls[0]["input"]), source, expected, filepath.Join(t.TempDir(), "probe.txt"), marker)
			next, err := Prepare(ctx, encoded(map[string]any{"input": []any{map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(calls[0]["call_id"]), "output": marker}}}), r.scope, r.store, nil, 256<<20)
			require.NoError(t, err)
			restored := preparedInput(t, next)
			require.JSONEq(t, string(native), string(restored[1]))
			result, _ := parseObject(restored[2])
			require.Equal(t, marker, stringValue(result["output"]))
			require.Equal(t, "call_native_1", stringValue(result["call_id"]))
		})
	}
}
