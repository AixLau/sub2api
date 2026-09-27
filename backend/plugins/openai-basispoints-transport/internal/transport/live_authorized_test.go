package transport

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in. The selected export stays local; credentials only enter
// the plugin's authorized outbound request. No account import or token refresh.
func TestAuthorizedLocalPlanAndCompaction(t *testing.T) {
	authFile := os.Getenv("BPS_DISCOVERY_LIVE_AUTH_FILE")
	if authFile == "" {
		t.Skip("select a local authorization export to run live inference")
	}
	var exported struct {
		Accounts []struct {
			Platform    string `json:"platform"`
			Type        string `json:"type"`
			ProxyKey    string `json:"proxy_key"`
			Credentials struct {
				AccessToken string `json:"access_token"`
				AccountID   string `json:"chatgpt_account_id"`
			} `json:"credentials"`
		} `json:"accounts"`
		Proxies []struct {
			Key                                string `json:"proxy_key"`
			Protocol, Host, Username, Password string
			Port                               int
		} `json:"proxies"`
	}
	raw, err := os.ReadFile(authFile)
	require.NoError(t, err, "read selected export")
	require.NoError(t, json.Unmarshal(raw, &exported), "parse selected export")
	require.Len(t, exported.Accounts, 1, "select an export with exactly one account")
	a := exported.Accounts[0]
	require.Equal(t, "openai", a.Platform)
	require.Equal(t, "oauth", a.Type)
	require.True(t, a.Credentials.AccessToken != "" && a.Credentials.AccountID != "", "export needs OAuth access authorization")
	proxy := ""
	if a.ProxyKey != "" {
		for _, p := range exported.Proxies {
			if p.Key != a.ProxyKey {
				continue
			}
			require.Contains(t, []string{"http", "https", "socks5"}, p.Protocol)
			u := &url.URL{Scheme: p.Protocol, Host: net.JoinHostPort(p.Host, strconv.Itoa(p.Port))}
			if p.Username != "" {
				u.User = url.UserPassword(p.Username, p.Password)
			}
			proxy = u.String()
		}
		require.True(t, proxy != "", "the selected account proxy must exist in its export")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	c := clientForTest(t, &testHostKV{values: map[string][]byte{}}, "")
	config := []byte(`{"proxy_mode":"account","native_fallback":false,"tools_via_native":false,"auto_disable_bps_on_403":false}`)
	applied, err := c.ApplyConfig(ctx, &pluginv1.ApplyConfigRequest{ConfigJson: config})
	require.NoError(t, err)
	require.True(t, applied.Applied)
	model := os.Getenv("BPS_DISCOVERY_LIVE_MODEL")
	if model == "" {
		model = "gpt-6-sol"
	}
	session := "local-bps-test-" + uuid.NewString()
	forward := func(t *testing.T, body map[string]any) map[string]json.RawMessage {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		start := &pluginv1.ForwardRequestStart{AccountId: 1, Platform: "openai", AccountType: "oauth", Method: "POST", Url: "https://chatgpt.com/backend-api/codex/responses", Host: "chatgpt.com", HasBody: true, ContentLength: int64(len(encoded)), ProxyUrl: proxy, RequestId: uuid.NewString(), Headers: map[string]*pluginv1.HeaderValues{
			"authorization":      {Values: []string{"Bearer " + a.Credentials.AccessToken}},
			"chatgpt-account-id": {Values: []string{a.Credentials.AccountID}},
			"session_id":         {Values: []string{session}},
			"Content-Type":       {Values: []string{"application/json"}},
			"Accept":             {Values: []string{"text/event-stream"}},
			"User-Agent":         {Values: []string{"codex-tui/0.158.0 (Mac OS; arm64)"}},
			"OpenAI-Beta":        {Values: []string{"responses=experimental"}},
		}}
		header, wire, failure := forwardForTest(t, c, encoded, ctx, start)
		if failure != nil {
			t.Fatalf("plugin transport failed: code=%s", failure.Code)
		}
		require.NotNil(t, header)
		t.Logf("model=%s HTTP=%d response_bytes=%d", model, header.StatusCode, len(wire))
		if header.StatusCode != 200 {
			var rejection struct {
				Error struct{ Code, Type, Param, Message string }
			}
			_ = json.Unmarshal(wire, &rejection)
			message := strings.ReplaceAll(rejection.Error.Message, a.Credentials.AccessToken, "[redacted]")
			message = strings.ReplaceAll(message, a.Credentials.AccountID, "[redacted]")
			if len(message) > 512 {
				message = message[:512]
			}
			t.Fatalf("upstream refused probe: status=%d code=%s type=%s param=%s message=%s", header.StatusCode, rejection.Error.Code, rejection.Error.Type, rejection.Error.Param, message)
		}
		var final map[string]json.RawMessage
		completedItems := map[int]json.RawMessage{}
		for _, line := range strings.Split(string(wire), "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event struct {
				Type        string
				Response    map[string]json.RawMessage
				Item        json.RawMessage
				OutputIndex int `json:"output_index"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
				continue
			}
			if event.Type == "response.completed" || event.Type == "response.failed" || event.Type == "response.incomplete" {
				final = event.Response
			}
			if event.Type == "response.output_item.done" {
				completedItems[event.OutputIndex] = event.Item
			}
		}
		require.NotNil(t, final, "must receive a terminal response event")
		if string(final["status"]) != `"completed"` {
			var failure struct{ Code, Message string }
			_ = json.Unmarshal(final["error"], &failure)
			t.Fatalf("upstream semantic failure: code=%s", failure.Code)
		}
		// Native Codex Lite streams deliver output through item events and may
		// omit it from the completed snapshot. Reconstruct only the test view;
		// forwardNative must preserve the actual upstream wire unchanged.
		var output []json.RawMessage
		require.NoError(t, json.Unmarshal(final["output"], &output))
		if len(output) == 0 && len(completedItems) > 0 {
			indexes := make([]int, 0, len(completedItems))
			for index := range completedItems {
				indexes = append(indexes, index)
			}
			slices.Sort(indexes)
			for _, index := range indexes {
				output = append(output, completedItems[index])
			}
			final["output"], err = json.Marshal(output)
			require.NoError(t, err)
		}
		var usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
			Total  int `json:"total_tokens"`
		}
		_ = json.Unmarshal(final["usage"], &usage)
		t.Logf("terminal=completed input_tokens=%d output_tokens=%d total_tokens=%d", usage.Input, usage.Output, usage.Total)
		return final
	}
	tools := []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec", "description": "Run JavaScript in the client. ALL_TOOLS lists enabled tool metadata; tools exposes those tools; text(value) returns actual output. The client has update_plan enabled."}}}}
	input := []any{map[string]any{"role": "user", "content": "Call the native update_plan now with one in_progress step named Verify the local test. This probe specifically requires native update_plan to exercise the adapter; do not call run_officejs or write code. After the tool returns a receipt, reply only with that exact receipt and no further tool calls."}}
	receipt := "plan-receipt-" + uuid.NewString()
	planSucceeded := t.Run("native_plan_real_upstream", func(t *testing.T) {
		stateFile := filepath.Join(t.TempDir(), "plan.json")
		calls := 0
		for round := 0; round < 3; round++ {
			final := forward(t, map[string]any{"model": model, "input": input, "tools": tools, "stream": true, "reasoning": map[string]string{"effort": "low"}})
			var output []map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(final["output"], &output))
			current := 0
			text := ""
			for _, item := range output {
				input = append(input, item)
				if string(item["type"]) == `"message"` {
					text += string(item["content"])
					continue
				}
				if string(item["type"]) != `"custom_tool_call"` {
					continue
				}
				var program, callID string
				require.NoError(t, json.Unmarshal(item["input"], &program))
				require.NoError(t, json.Unmarshal(item["call_id"], &callID))
				fixture, err := os.ReadFile("../bridge/native_plan.js")
				require.NoError(t, err)
				prefix := string(fixture) + "\nawait bpsClientUpdatePlan("
				require.True(t, strings.HasPrefix(program, prefix) && strings.HasSuffix(program, ");\n"), "only the fixed plan adapter may execute in this probe")
				require.True(t, json.Valid([]byte(strings.TrimSuffix(strings.TrimPrefix(program, prefix), ");\n"))), "adapter arguments must be data")
				harness := `const fs=require('node:fs');const code=fs.readFileSync(0,'utf8');const ALL_TOOLS=[{name:'update_plan'}];const tools={update_plan:async p=>{fs.writeFileSync(process.argv[1],JSON.stringify(p));return {receipt:process.argv[2]};}};const out=[];const F=Object.getPrototypeOf(async function(){}).constructor;new F('ALL_TOOLS','tools','text',code)(ALL_TOOLS,tools,x=>out.push(x)).then(()=>process.stdout.write(JSON.stringify(out))).catch(()=>process.exit(1));`
				cmd := exec.CommandContext(ctx, "node", "-e", harness, stateFile, receipt)
				cmd.Stdin = strings.NewReader(program)
				result, err := cmd.Output()
				require.NoError(t, err)
				require.Contains(t, string(result), receipt)
				state, err := os.ReadFile(stateFile)
				require.NoError(t, err)
				require.Contains(t, string(state), "Verify the local test")
				require.Contains(t, string(state), "in_progress")
				input = append(input, map[string]any{"type": "custom_tool_call_output", "call_id": callID, "output": string(result)})
				current++
				calls++
			}
			if current == 0 {
				require.Positive(t, calls)
				require.Contains(t, text, receipt)
				t.Logf("PASS real BPS native plan -> client file side effect -> receipt replay; calls=%d", calls)
				return
			}
		}
		t.Fatal("probe exceeded three inference rounds")
	})
	t.Run("native_compaction_after_BPS", func(t *testing.T) {
		if !planSucceeded {
			t.Skip("BPS history requires successful plan test")
		}
		for _, priorFunctionID := range []bool{false, true} {
			t.Run(map[bool]string{false: "canonical_item_ID", true: "persisted_function_ID"}[priorFunctionID], func(t *testing.T) {
				checkpoint := append([]any{map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{}}}, input...)
				if priorFunctionID {
					changed := false
					for index, value := range checkpoint {
						raw, err := json.Marshal(value)
						require.NoError(t, err)
						var item map[string]json.RawMessage
						require.NoError(t, json.Unmarshal(raw, &item))
						if string(item["type"]) == `"custom_tool_call"` {
							item["id"] = json.RawMessage(`"fc_persisted_BPS_call"`)
							checkpoint[index] = item
							changed = true
						}
					}
					require.True(t, changed, "the checkpoint must contain actual prior BPS tool history")
				}
				checkpoint = append(checkpoint, map[string]any{"role": "user", "content": "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a short handoff summary, state which tool was exercised, and include the exact receipt from its real result. Do not run tools."})
				final := forward(t, map[string]any{"model": model, "input": checkpoint, "instructions": "Summarize the supplied conversation for continuation.", "stream": true, "store": false, "tool_choice": "auto", "parallel_tool_calls": false, "reasoning": map[string]string{"effort": "low", "context": "all_turns"}, "client_metadata": map[string]string{"x-codex-turn-metadata": `{"request_kind":"compaction","compaction":{"phase":"mid_turn","reason":"context_limit","trigger":"auto"}}`}})
				require.Contains(t, string(final["output"]), receipt)
				require.NotContains(t, string(final["output"]), `"type":"function_call"`)
				require.NotContains(t, string(final["output"]), `"type":"custom_tool_call"`)
				health, err := c.Health(ctx, &pluginv1.HealthRequest{})
				require.NoError(t, err)
				var status struct {
					Recent []struct{ Route, Reason string } `json:"recent_requests"`
				}
				require.NoError(t, json.Unmarshal([]byte(health.StatusJson), &status))
				found := false
				for _, r := range status.Recent {
					if r.Route == "native" && r.Reason == "context_compaction" {
						found = true
					}
				}
				require.True(t, found, "compaction must use native route before any BPS attempt")
				t.Logf("PASS native context compaction with prior BPS tool history; no tool calls")
			})
		}
	})
}
