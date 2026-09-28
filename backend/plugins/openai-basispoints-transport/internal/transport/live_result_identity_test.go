package transport

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Explicitly authorized live contract check for the 0.6.8 custom-result ID
// regression. Run only fixed local receipt code; no model-generated code or
// production account import is involved. Each case makes one BPS inference.
func TestAuthorizedCustomToolResultReplay(t *testing.T) {
	ctx, client, model, forward := newAuthorizedLiveProbe(t)
	for _, declared := range []bool{false, true} {
		name := "native_custom_history"
		if declared {
			name = "custom_history_rebuilt_as_function"
		}
		t.Run(name, func(t *testing.T) {
			program := "text(await tools.probe_receipt());"
			harness := `const code=require('node:fs').readFileSync(0,'utf8');const tools={probe_receipt:async()=>"replay-receipt-"+require('node:crypto').randomUUID()};const F=Object.getPrototypeOf(async function(){}).constructor;new F('tools','text',code)(tools,x=>process.stdout.write(x)).catch(()=>process.exit(1));`
			cmd := exec.CommandContext(ctx, "node", "-e", harness)
			cmd.Stdin = strings.NewReader(program)
			result, err := cmd.Output()
			require.NoError(t, err)
			receipt := string(result)
			require.True(t, strings.HasPrefix(receipt, "replay-receipt-"))
			tools := []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec", "description": "Run JavaScript in the client. tools.probe_receipt returns a new random receipt; text(value) returns actual output."}}}}
			callID := "ctc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			// Production history mixed namespace-qualified functions.exec calls
			// with a native exec call lacking a namespace. Exercise both paths.
			call := map[string]any{"type": "custom_tool_call", "id": callID, "call_id": callID, "name": "exec", "input": program, "status": "completed"}
			if declared {
				call["namespace"] = "functions"
			}
			input := []any{
				map[string]any{"role": "user", "content": "The local receipt probe has already run. Read its actual tool output and reply with exactly that receipt. Do not call any more tools."},
				call,
				map[string]any{"type": "custom_tool_call_output", "id": "ctco_client", "call_id": callID, "output": string(result)},
			}
			final := forward(t, map[string]any{"model": model, "tools": tools, "input": input, "stream": true, "tool_choice": "none", "reasoning": map[string]string{"effort": "low"}})
			var output []struct {
				Type    string
				Content []struct{ Type, Text string }
			}
			require.NoError(t, json.Unmarshal(final["output"], &output))
			var text strings.Builder
			for _, item := range output {
				require.NotContains(t, []string{"function_call", "custom_tool_call"}, item.Type)
				for _, content := range item.Content {
					if content.Type == "output_text" {
						text.WriteString(content.Text)
					}
				}
			}
			require.Equal(t, receipt, strings.TrimSpace(text.String()))
			t.Log("PASS real BPS accepted result identity and returned the actual local receipt")
		})
	}
	entries := completedRequestsForTest(t, client, ctx, 2)
	for _, entry := range entries {
		require.Equal(t, "bps", entry.Route)
		require.Equal(t, 1, entry.Attempt, "correct result IDs must not need correction inference")
	}
	health, err := client.Health(ctx, &pluginv1.HealthRequest{})
	require.NoError(t, err)
	require.True(t, health.Healthy)
}

func TestAuthorizedFunctionHistoryUnicode(t *testing.T) {
	ctx, _, model, forward := newAuthorizedLiveProbe(t)
	result, err := exec.CommandContext(ctx, "node", "-e", `process.stdout.write("receipt-"+require("node:crypto").randomUUID())`).Output()
	require.NoError(t, err)
	final := forward(t, map[string]any{
		"model": model, "stream": true, "tool_choice": "none", "reasoning": map[string]string{"effort": "low"},
		"tools": []any{map[string]any{"type": "function", "name": "probe", "parameters": map[string]any{"type": "object", "properties": map[string]any{"content": map[string]string{"type": "string"}}}}},
		"input": []any{
			map[string]any{"type": "function_call", "id": "fc_unicode", "call_id": "call_unicode", "name": "probe", "arguments": `{"content":"Unicode \ud83d\ude80 history"}`},
			map[string]any{"type": "function_call_output", "call_id": "call_unicode", "output": string(result)},
			map[string]any{"role": "user", "content": "The probe already ran. Reply with exactly its receipt and nothing else. Do not call any tools."},
		},
	})
	var output []struct {
		Type    string
		Content []struct{ Type, Text string }
	}
	require.NoError(t, json.Unmarshal(final["output"], &output))
	var answer strings.Builder
	for _, item := range output {
		require.Contains(t, []string{"message", "reasoning"}, item.Type)
		for _, content := range item.Content {
			if content.Type == "output_text" {
				answer.WriteString(content.Text)
			}
		}
	}
	require.Equal(t, string(result), strings.TrimSpace(answer.String()))
	t.Log("PASS real BPS accepted JSON surrogate-pair function history and returned the actual local receipt")
}
