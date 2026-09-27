package transport

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAuthorizedLocalFunctionArguments(t *testing.T) {
	ctx, c, model, forward := newAuthorizedLiveProbe(t)
	patterns := []string{`info\[['"]path['"]\]`, `DATA_PATH|root_path|Path\(|pickle\.dump`, `(write_text|pickle\.dump|json\.dump|\.tofile|np\.save)`}
	receipts := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	file := filepath.Join(t.TempDir(), "sample.py")
	require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf("info['path'] = '%s'\nPath('%s')\nnp.save('%s')\n", receipts[0], receipts[1], receipts[2])), 0600))
	toolCatalog := []any{map[string]any{"type": "function", "name": "search_content", "description": "Search a local file using the exact regular expression supplied in pattern. Return the actual matching lines.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"pattern": map[string]any{"type": "string", "enum": patterns}, "path": map[string]any{"type": "string"}}, "required": []string{"pattern", "path"}, "additionalProperties": false}}}
	input := []any{map[string]any{"role": "user", "content": fmt.Sprintf("Call search_content exactly three times on %s, once per exact regex below. Do not combine or simplify the patterns. They are literal regexes, including their backslashes. After receiving all three results, reply with the three UUID receipts from the actual matching lines and do not call any more tools.\n1. %s\n2. %s\n3. %s", file, patterns[0], patterns[1], patterns[2])}}
	seen := map[string]bool{}
	finished := false
	requests := 0
	for round := 0; round < 4; round++ {
		requests++
		final := forward(t, map[string]any{"model": model, "input": input, "tools": toolCatalog, "stream": true, "reasoning": map[string]string{"effort": "low"}})
		var output []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(final["output"], &output))
		results := []any{}
		var answer strings.Builder
		for _, item := range output {
			input = append(input, item)
			if string(item["type"]) == `"message"` {
				answer.Write(item["content"])
				continue
			}
			if string(item["type"]) != `"function_call"` {
				continue
			}
			require.JSONEq(t, `"search_content"`, string(item["name"]))
			var arguments, callID string
			require.NoError(t, json.Unmarshal(item["arguments"], &arguments))
			require.NoError(t, json.Unmarshal(item["call_id"], &callID))
			var args struct{ Pattern, Path string }
			require.NoError(t, json.Unmarshal([]byte(arguments), &args))
			require.Contains(t, patterns, args.Pattern, "the client must receive the exact requested regex bytes")
			require.Equal(t, file, args.Path, "only the local probe fixture may be searched")
			require.False(t, seen[args.Pattern], "do not repeat completed searches")
			result, err := exec.CommandContext(ctx, "rg", "-e", args.Pattern, "--", args.Path).Output()
			require.NoError(t, err)
			require.NotEmpty(t, result)
			seen[args.Pattern] = true
			results = append(results, map[string]any{"type": "function_call_output", "call_id": callID, "output": string(result)})
		}
		input = append(input, results...)
		if len(results) == 0 {
			require.Len(t, seen, 3, "all three searches must actually execute")
			for _, receipt := range receipts {
				require.Contains(t, answer.String(), receipt, "the second turn must consume actual client results")
			}
			finished = true
			break
		}
	}
	require.True(t, finished, "probe exceeded four inference rounds")
	entries := completedRequestsForTest(t, c, ctx, requests)
	for _, entry := range entries {
		require.Equal(t, "bps", entry.Route)
		require.Equal(t, 1, entry.Attempt, "literal FUNCTION arguments must not require correction inference")
	}
	health, err := c.Health(ctx, &pluginv1.HealthRequest{})
	require.NoError(t, err)
	require.True(t, health.Healthy)
	t.Log("PASS real BPS -> three exact regex arguments -> actual rg matches -> receipt replay; no correction inference")
}

func TestAuthorizedLocalNativeStructuredOutput(t *testing.T) {
	ctx, c, model, forward := newAuthorizedLiveProbe(t)
	cfg := []byte(`{"proxy_mode":"account","native_fallback":true,"tools_via_native":false,"auto_disable_bps_on_403":false,"response_header_timeout_seconds":1,"request_timeout_seconds":120}`)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: cfg})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	final := forward(t, map[string]any{"model": model, "input": []any{map[string]string{"role": "user", "content": "Return the word ready in the status field."}}, "instructions": "Return the requested JSON object.", "stream": true, "store": false, "reasoning": map[string]string{"effort": "low"}, "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "probe", "strict": true, "schema": map[string]any{"type": "object", "properties": map[string]any{"status": map[string]any{"type": "string"}}, "required": []string{"status"}, "additionalProperties": false}}}})
	require.Contains(t, string(final["output"]), "ready")
	entries := completedRequestsForTest(t, c, ctx, 1)
	require.Equal(t, "native", entries[0].Route)
	require.Equal(t, "structured_output", entries[0].Reason)
	t.Log("PASS native structured response completed with separate BPS header budget")
}
