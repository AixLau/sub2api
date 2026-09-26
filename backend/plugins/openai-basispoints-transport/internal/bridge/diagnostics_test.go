package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolDiagnosticObserver(t *testing.T) {
	for _, tc := range []struct {
		name, stage, reason string
		outer               object
		executor            string
	}{
		{"identity", "upstream_tool_identity", "undeclared_tool", object{"code": encoded("private_code")}, "functions.run_connector_action"},
		{"arguments", "upstream_tool_arguments", "invalid_json", object{}, "run_officejs"},
		{"references", "upstream_tool_references", "invalid_references", object{"references": encoded("private_reference")}, "run_officejs"},
		{"unknown_target", "upstream_tool_references", "undeclared_target", object{"references": encoded([]string{"private_reference"})}, "run_officejs"},
		{"code_type", "upstream_tool_code", "code_not_string", object{"references": encoded([]string{"functions.read_file"}), "code": encoded(map[string]string{"secret": "private_code"})}, "run_officejs"},
		{"json", "upstream_tool_code", "invalid_json", object{"references": encoded([]string{"functions.read_file"}), "code": encoded(`{"secret":"private\'code"}`)}, "run_officejs"},
		{"array", "upstream_tool_code", "not_object", object{"references": encoded([]string{"functions.read_file"}), "code": encoded(`["private_code"]`)}, "run_officejs"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{true: "/sse", false: "/json"}[stream], func(t *testing.T) {
				r := customRequest(t)
				var observed []Diagnostic
				ctx := WithDiagnosticObserver(context.Background(), func(d Diagnostic) { observed = append(observed, d) })
				item, _ := parseObject(rawCustomItem(tc.outer))
				item["name"] = encoded(tc.executor)
				if tc.name == "arguments" {
					item["arguments"] = encoded(`{"secret":"private\'arguments"}`)
				}
				item["id"], item["call_id"] = encoded("private_id"), encoded("private_call_id")
				response := map[string]any{"id": "resp_diagnostic", "status": "completed", "output": []any{item}}
				if stream {
					wire := event("response.created", map[string]any{"response": map[string]any{"id": "resp_diagnostic"}}) + event("response.output_item.done", map[string]any{"output_index": 0, "item": item}) + event("response.completed", map[string]any{"response": response})
					require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func([]byte) error { return nil }))
					require.True(t, r.Failed)
				} else {
					_, err := r.Response(ctx, encoded(response))
					require.Error(t, err)
				}
				require.Len(t, observed, 1, "one record despite the SSE semantic error returning nil")
				d := observed[0]
				require.Equal(t, tc.stage, d.Stage)
				require.Equal(t, "resp_diagnostic", d.ResponseID)
				require.Equal(t, map[bool]string{true: "response.output_item.done", false: "json_response"}[stream], d.SourceEvent)
				require.Equal(t, tc.reason, d.Reason)
				require.Equal(t, 2, d.CatalogTools)
				if tc.name == "json" {
					require.Positive(t, d.JSONOffset)
					require.Equal(t, "code", d.Field)
					require.Equal(t, "invalid", d.CodeJSONType)
					require.Equal(t, "functions.read_file", d.TargetTool)
				}
				raw, err := json.Marshal(d)
				require.NoError(t, err)
				require.NotContains(t, string(raw), "private")
				require.Empty(t, r.converted)
			})
		}
	}
}

func TestDiagnosticObserverDoesNotInterpretDirectBusinessArguments(t *testing.T) {
	r := customRequest(t)
	var observed []Diagnostic
	ctx := WithDiagnosticObserver(context.Background(), func(d Diagnostic) { observed = append(observed, d) })
	item := object{"type": encoded("function_call"), "name": encoded("functions.read_file"), "call_id": encoded("call_private"),
		"arguments": encoded(`{"references":["functions.read_file"],"code":"private_code"}`)}
	_, err := r.convertCall(ctx, encoded(item)) // no id: cannot save replay
	require.Error(t, err)
	require.Len(t, observed, 1)
	require.Empty(t, observed[0].CodeType)
	require.Empty(t, observed[0].TargetTool)
}

func TestSuccessfulToolCallsDoNotProduceFailureDiagnostics(t *testing.T) {
	r := customRequest(t)
	ctx := WithDiagnosticObserver(context.Background(), func(Diagnostic) { t.Fatal("successful call logged as failure") })
	_, err := r.convertCall(ctx, officeItem("functions.exec", "private raw source", false))
	require.NoError(t, err)
}
