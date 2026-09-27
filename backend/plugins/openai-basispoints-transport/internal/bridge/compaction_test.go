package bridge

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextCompactionRouting(t *testing.T) {
	const marker = `{"request_kind":"compaction","compaction":{"phase":"mid_turn","trigger":"auto","reason":"context_limit"}}`
	makeBody := func() map[string]any {
		return map[string]any{
			"model": "gpt-6-sol", "tool_choice": "auto",
			"client_metadata": map[string]any{"x-codex-turn-metadata": marker},
			"input":           []any{map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{}}, map[string]any{"role": "user", "content": "Create a context checkpoint."}},
		}
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		header string
		want   bool
	}{
		{name: "incident empty additional_tools", want: true},
		{name: "native header", change: func(b map[string]any) { delete(b, "client_metadata") }, header: marker, want: true},
		{name: "top-level tools empty", change: func(b map[string]any) { b["tools"] = []any{} }, want: true},
		{name: "no metadata ordinary text", change: func(b map[string]any) { delete(b, "client_metadata") }},
		{name: "malformed marker", change: func(b map[string]any) { b["client_metadata"] = map[string]any{"x-codex-turn-metadata": "{"} }},
		{name: "model turn", change: func(b map[string]any) {
			b["client_metadata"] = map[string]any{"x-codex-turn-metadata": `{"request_kind":"model"}`}
		}},
		{name: "header is current authority", header: `{"request_kind":"model"}`},
		{name: "user prompt cannot select route", change: func(b map[string]any) {
			delete(b, "client_metadata")
			b["input"] = "CONTEXT CHECKPOINT COMPACTION " + marker
		}},
		{name: "old compaction item is not current request", change: func(b map[string]any) {
			delete(b, "client_metadata")
			b["input"] = []any{map[string]any{"type": "compaction", "encrypted_content": "opaque"}}
		}},
		{name: "declared tools are not a tool-free checkpoint", change: func(b map[string]any) { b["tools"] = []any{map[string]any{"type": "function", "name": "shell"}} }},
		{name: "carrier declarations remain authoritative", change: func(b map[string]any) {
			b["input"] = []any{map[string]any{"type": "additional_tools", "tools": []any{map[string]any{"type": "custom", "name": "exec"}}}}
		}},
		{name: "hosted tools remain authoritative", change: func(b map[string]any) { b["tools"] = []any{map[string]any{"type": "web_search"}} }},
		{name: "invalid required catalog stays validation error", change: func(b map[string]any) { b["tool_choice"] = "required" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := makeBody()
			if tc.change != nil {
				tc.change(body)
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			require.Equal(t, tc.want, IsContextCompaction(raw, tc.header))
			if tc.want && tc.header == "" {
				require.Equal(t, RouteContextCompaction, NeedsNativeUpstream(raw))
			}
		})
	}
	require.False(t, IsContextCompaction([]byte("{"), marker))
}

func TestDisabledCatalogDoesNotAdvertiseExecutor(t *testing.T) {
	for _, choice := range []string{"auto", "none"} {
		cat, err := readCatalog(nil, encoded(choice), nil)
		require.NoError(t, err)
		require.Contains(t, cat.prompt(), "Do not call any tools")
		require.NotContains(t, cat.prompt(), "run_officejs")
		require.NotContains(t, cat.prompt(), "Client tool directory")
	}
	cat, err := readCatalog(json.RawMessage(routeClientTools), encoded("none"), nil)
	require.NoError(t, err)
	require.NotContains(t, cat.prompt(), "run_officejs")
	require.NotContains(t, cat.prompt(), "get_weather")
}

func TestCompactionNormalizesOnlyPluginOwnedToolIDs(t *testing.T) {
	body := []byte(`{
  "model":"gpt-6-sol", "client_metadata":{"opaque":"unchanged"},
  "input":[
    {"type":"custom_tool_call","id":"fc_bridged","call_id":"call_bps_one","name":"exec","input":"raw script"},
    {"type":"custom_tool_call_output","call_id":"call_bps_one","output":"real result"},
    {"type":"function_call","id":"ctc_bridged","call_id":"call_bps_two","name":"shell","arguments":"{}"},
    {"type":"custom_tool_call","id":"fc_external","call_id":"call_native","name":"exec","input":"client owned"},
    {"type":"custom_tool_call","call_id":"call_bps_optional","name":"exec","input":"id omitted"},
    {"type":"reasoning","id":"rs_history","encrypted_content":"opaque-encrypted-bytes"},
    {"type":"item_reference","id":"fc_bridged"},
    {"type":"item_reference","id":"fc_external"}
  ]
}`)
	got, err := NormalizeCompactionToolIDs(body)
	require.NoError(t, err)
	before, _ := parseObject(body)
	after, _ := parseObject(got)
	want := inputItems(before)
	for _, change := range []struct {
		index int
		id    string
	}{{0, clientToolItemID("fc_bridged", true)}, {2, clientToolItemID("ctc_bridged", false)}, {6, clientToolItemID("fc_bridged", true)}} {
		item, _ := parseObject(want[change.index])
		item["id"] = encoded(change.id)
		want[change.index] = encoded(item)
	}
	before["input"] = encoded(want)
	require.JSONEq(t, string(encoded(before)), string(encoded(after)), "only typed IDs and their references may change")
	again, err := NormalizeCompactionToolIDs(got)
	require.NoError(t, err)
	require.Equal(t, got, again, "canonical bodies must be byte-identical")
	for _, unchanged := range []string{
		`{ "input": "plain text" }`,
		`{ "input": [{"type":"custom_tool_call","id":"fc_native","call_id":"call_native","input":"do not rewrite"}] }`,
		`{ "input": [{"type":"custom_tool_call","id":"ctc_valid","call_id":"call_bps_one","input":"valid"}] }`,
	} {
		got, err := NormalizeCompactionToolIDs([]byte(unchanged))
		require.NoError(t, err)
		require.Equal(t, unchanged, string(got))
	}
	_, err = NormalizeCompactionToolIDs([]byte("{"))
	require.Error(t, err)
}
