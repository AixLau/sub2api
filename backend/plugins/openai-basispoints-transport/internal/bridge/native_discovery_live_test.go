package bridge

import (
	"context"
	_ "embed"
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

//go:embed testdata/live_bps_discovery.py
var liveDiscoveryTransport string

// Explicit opt-in: a bounded inference test, not a production deployment.
// The SSH host reads only the named account's authorization into memory.
func TestNativeDiscoveryLiveBPS(t *testing.T) {
	socket, email := os.Getenv("BPS_DISCOVERY_LIVE_SSH_SOCKET"), os.Getenv("BPS_DISCOVERY_LIVE_EMAIL")
	target := os.Getenv("BPS_DISCOVERY_LIVE_SSH_TARGET")
	if socket == "" || email == "" || target == "" {
		t.Skip("live account test is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	file := filepath.Join(t.TempDir(), "SKILL.md")
	marker := "client-file-" + uuid.NewString()
	require.NoError(t, os.WriteFile(file, []byte("# Probe Skill\nThe secret verification marker for this fixture is: "+marker+"\n"), 0600))
	input := []any{messageItem("developer", skillInstructions(file)), messageItem("user", "Run this discovery integration check. First call the native list_skills with limit 8 AND native list_connectors with page_size 10. After the results, call native read_skills for skill sample in full mode. Then report the exact marker from that file. Use those native discovery tools directly so the adapter is exercised; do not call run_officejs or author custom code. Do not invent the file contents.")}
	store := memoryStore{}
	scope := "live-discovery-" + uuid.NewString()
	seen := map[string]int{}
	for round := 0; round < 5; round++ {
		r, err := Prepare(ctx, encoded(map[string]any{"model": "gpt-6-astra", "stream": true, "reasoning": map[string]string{"effort": "low"}, "tools": discoveryTools(), "input": input}), scope, store, nil, 256<<20)
		require.NoError(t, err)
		final := liveBPSResponse(t, ctx, r, socket, target, email)
		var output []json.RawMessage
		require.NoError(t, json.Unmarshal(final["output"], &output))
		count := 0
		finalText := ""
		for _, raw := range output {
			item, _ := parseObject(raw)
			if isToolCall(item) {
				program := stringValue(item["input"])
				require.True(t, strings.HasPrefix(program, nativeDiscoveryScript), "only generated discovery programs may execute in this probe")
				record, err := loadCall(ctx, store, scope, stringValue(item["call_id"]))
				require.NoError(t, err)
				native, _ := parseObject(record.Original)
				name := strings.TrimPrefix(qualifiedCallName(native), "functions.")
				require.Contains(t, []string{"list_skills", "read_skills", "list_connectors"}, name)
				seen[name]++
				count++
				results := executeDiscovery(t, program)
				input = append(input, json.RawMessage(raw), map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(item["call_id"]), "output": string(encoded(results))})
			} else {
				input = append(input, json.RawMessage(raw))
				if stringValue(item["type"]) == "message" {
					finalText += string(item["content"])
				}
			}
		}
		t.Logf("round=%d http=200 client_calls=%d usage=%s", round+1, count, string(final["usage"]))
		if count == 0 {
			require.Contains(t, finalText, marker, "model must read and use the real client file")
			for _, name := range []string{"list_skills", "read_skills", "list_connectors"} {
				require.Positive(t, seen[name], fmt.Sprintf("live probe did not exercise %s", name))
			}
			t.Logf("PASS: real upstream discovery, client file read, result replay, final marker; operations=%v", seen)
			return
		}
	}
	t.Fatal("probe exceeded its five-inference limit")
}

func liveBPSResponse(t *testing.T, ctx context.Context, r *Request, socket, target, email string) object {
	t.Helper()
	body := encoded(map[string]any{"email": email, "body": json.RawMessage(r.Body)})
	script := "'" + strings.ReplaceAll(liveDiscoveryTransport, "'", "'\"'\"'") + "'"
	cmd := exec.CommandContext(ctx, "ssh", "-S", socket, "-o", "BatchMode=yes", target, "python3 -c "+script)
	cmd.Stdin = strings.NewReader(string(body))
	result, err := cmd.Output()
	require.NoError(t, err, "remote probe failed without exposing authorization")
	response, err := parseObject(result)
	require.NoError(t, err)
	if string(response["status"]) != "200" {
		// The remote probe redacts authorization before returning the error.
		detail := stringValue(response["body"])
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
		t.Logf("upstream rejection: %s", detail)
	}
	require.Equal(t, "200", string(response["status"]), "upstream refused the authorized probe")
	var wire strings.Builder
	// No Feedback handler: every successful call must reach the client directly.
	require.NoError(t, r.Stream(ctx, strings.NewReader(stringValue(response["body"])), func(b []byte) error { wire.Write(b); return nil }))
	final := streamSnapshot(t, wire.String())
	require.False(t, r.Failed, "tool conversion failed: %s", string(final["error"]))
	return final
}
