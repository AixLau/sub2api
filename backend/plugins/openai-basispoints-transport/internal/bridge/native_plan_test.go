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

	"github.com/stretchr/testify/require"
)

// The client handler fixture records an actual side effect. Merely emitting
// a synthetic success without invoking the client's tool cannot pass.
func executeNativePlan(t *testing.T, program string, enabled bool, behavior string) (json.RawMessage, []object) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("node is required for native plan execution tests")
	}
	statePath := filepath.Join(t.TempDir(), "plan-events.jsonl")
	harness := `const fs = require('node:fs');
const source = fs.readFileSync(0, 'utf8'), out = [];
const statePath = process.argv[1], enabled = process.argv[2] === 'true', behavior = process.argv[3];
const tools = { update_plan: async params => {
 if (behavior === 'throw') throw new Error('client-private-detail');
 if (behavior === 'deny') return { isError: true, content: [{type:'text',text:'Plan mode does not allow update_plan'}] };
 fs.appendFileSync(statePath, JSON.stringify(params) + '\n');
 return {};
}};
const ALL_TOOLS = enabled ? [{name:'update_plan',description:'Updates the task plan.'}] : [];
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
new AsyncFunction('tools','ALL_TOOLS','text',source)(tools,ALL_TOOLS,value=>out.push(value))
 .then(()=>process.stdout.write(JSON.stringify(out))).catch(()=>process.exit(1));`
	cmd := exec.Command("node", "-e", harness, statePath, fmt.Sprint(enabled), behavior)
	cmd.Stdin = strings.NewReader(program)
	output, err := cmd.Output()
	require.NoError(t, err)
	require.True(t, json.Valid(output), string(output))
	var events []object
	data, err := os.ReadFile(statePath)
	if !os.IsNotExist(err) {
		require.NoError(t, err)
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			event, err := parseObject([]byte(line))
			require.NoError(t, err)
			events = append(events, event)
		}
	}
	return output, events
}

func nativePlanArgs() map[string]any {
	return map[string]any{"summary": "Verified input; implementing the change", "plan": []any{
		map[string]string{"id": "inspect", "description": "Inspect the input", "status": "completed", "result": "Confirmed values"},
		map[string]string{"id": "edit", "description": "Apply the change", "status": "in_progress", "result": ""},
	}}
}

func TestNativePlanExecutesAndReplaysWithoutFeedback(t *testing.T) {
	ctx := context.Background()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			store := memoryStore{}
			tools := discoveryTools()
			r, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": "Update the plan"}), "native-plan", store, nil, 256<<20)
			require.NoError(t, err)
			r.Feedback = func(context.Context, []byte) ([]byte, error) {
				t.Fatal("valid plan must execute on client without feedback inference")
				return nil, nil
			}
			native := nativeDiscoveryItem("update_plan", nativePlanArgs(), "plan")
			response := feedbackResponse("resp_plan", native)
			var final object
			if stream {
				var wire strings.Builder
				err = r.Stream(ctx, strings.NewReader(event("response.completed", map[string]any{"response": response})), func(b []byte) error { wire.Write(b); return nil })
				require.NoError(t, err)
				require.False(t, r.Failed, wire.String())
				final = streamSnapshot(t, wire.String())
			} else {
				raw, err := r.Response(ctx, encoded(response))
				require.NoError(t, err)
				final, err = parseObject(raw)
				require.NoError(t, err)
			}
			var calls []object
			require.NoError(t, json.Unmarshal(final["output"], &calls))
			require.Len(t, calls, 1)
			call := calls[0]
			require.Equal(t, "custom_tool_call", stringValue(call["type"]))
			require.Equal(t, "functions", stringValue(call["namespace"]))
			require.Equal(t, "exec", stringValue(call["name"]))
			result, events := executeNativePlan(t, stringValue(call["input"]), true, "")
			require.JSONEq(t, "[{}]", string(result), "return only the real handler result")
			require.Len(t, events, 1)
			want := map[string]any{"explanation": "Verified input; implementing the change", "plan": []any{
				map[string]string{"step": "Inspect the input\nResult: Confirmed values", "status": "completed"},
				map[string]string{"step": "Apply the change", "status": "in_progress"},
			}}
			require.JSONEq(t, string(encoded(want)), string(encoded(events[0])))
			next, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": []any{map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(call["call_id"]), "output": string(result)}}}), "native-plan", store, nil, 256<<20)
			require.NoError(t, err)
			restored := preparedInput(t, next)
			require.JSONEq(t, string(native), string(restored[1]))
			output, err := parseObject(restored[2])
			require.NoError(t, err)
			require.Equal(t, "function_call_output", stringValue(output["type"]))
			require.Equal(t, "call_plan", stringValue(output["call_id"]))
			require.Equal(t, string(result), stringValue(output["output"]))
			require.Equal(t, r.Turn.ID, next.Turn.ID)
			require.Equal(t, r.Turn.Iteration+1, next.Turn.Iteration)
		})
	}
}

func TestNativePlanClientValidationAndActualErrors(t *testing.T) {
	valid := map[string]any{"explanation": "Use native contract", "plan": []any{map[string]string{"step": "Quotes '\" and newline\n remain data", "status": "pending"}}}
	program := func(args any) string {
		return nativePlanScript + "\nawait bpsClientUpdatePlan(" + string(encoded(args)) + ");"
	}
	result, events := executeNativePlan(t, program(valid), true, "")
	require.JSONEq(t, "[{}]", string(result))
	require.Len(t, events, 1)
	require.JSONEq(t, string(encoded(valid)), string(encoded(events[0])))
	for _, tc := range []struct {
		name, behavior, want string
		enabled              bool
	}{
		{"missing", "", "CLIENT_PLAN_TOOL_UNAVAILABLE", false},
		{"thrown", "throw", "CLIENT_PLAN_EXECUTION_FAILED", true},
		{"denied", "deny", "Plan mode does not allow update_plan", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, events := executeNativePlan(t, program(valid), tc.enabled, tc.behavior)
			require.Empty(t, events)
			require.Contains(t, string(result), tc.want)
			require.NotContains(t, string(result), "client-private-detail")
			require.NotContains(t, string(result), "\"success\":true")
		})
	}
	for _, args := range []any{
		map[string]any{"plan": nil},
		map[string]any{"plan": []any{map[string]string{"step": "bad", "status": "failed"}}},
		map[string]any{"plan": []any{map[string]string{"step": "bad", "status": "skipped"}}},
		map[string]any{"plan": []any{map[string]string{"step": "one", "description": "different", "status": "pending"}}},
		map[string]any{"plan": []any{map[string]string{"description": "one", "status": "in_progress"}, map[string]string{"description": "two", "status": "in_progress"}}},
		map[string]any{"plan": []any{}, "unrecognized": true},
	} {
		result, events := executeNativePlan(t, program(args), true, "")
		require.Empty(t, events)
		require.Contains(t, string(result), "CLIENT_PLAN_ARGUMENTS_INVALID")
	}
}

func TestNativePlanRespectsDeclaredToolsNamespaceAndChoice(t *testing.T) {
	ctx := context.Background()
	native := nativeDiscoveryItem("update_plan", map[string]any{"plan": []any{}}, "scope")
	for _, name := range []string{"update_plan", "functions.update_plan"} {
		r, err := Prepare(ctx, encoded(map[string]any{"tools": discoveryTools(), "input": "test"}), name, memoryStore{}, nil, 256<<20)
		require.NoError(t, err)
		item, _ := parseObject(native)
		item["name"] = encoded(name)
		call, err := r.convertCall(ctx, encoded(item))
		require.NoError(t, err)
		require.Contains(t, string(call), "bpsClientUpdatePlan")
	}
	for _, choice := range []any{"none", map[string]string{"type": "function", "name": "other"}} {
		tools := append(discoveryTools(), map[string]any{"type": "function", "name": "other"})
		r, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": "test", "tool_choice": choice}), "choice", memoryStore{}, nil, 256<<20)
		require.NoError(t, err)
		_, err = r.convertCall(ctx, native)
		require.Error(t, err)
		require.Empty(t, r.converted)
	}
	r := customRequest(t)
	_, err := r.convertCall(ctx, native)
	require.Error(t, err)
	require.Equal(t, "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", FailureCode(err))
	r, err = Prepare(ctx, encoded(map[string]any{"tools": discoveryTools(), "input": "test"}), "foreign", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	item, _ := parseObject(native)
	item["namespace"] = encoded("unrelated")
	_, err = r.convertCall(ctx, encoded(item))
	require.Error(t, err)
	require.Equal(t, "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", FailureCode(err))
}

func TestNativePlanUnavailableReturnsToolResultAndContinues(t *testing.T) {
	ctx := context.Background()
	store := memoryStore{}
	tools := discoveryTools()
	r, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": "Use the available tools"}), "missing-plan", store, nil, 256<<20)
	require.NoError(t, err)
	r.Feedback = func(context.Context, []byte) ([]byte, error) {
		t.Fatal("client capabilities must be checked by the client")
		return nil, nil
	}
	native := nativeDiscoveryItem("update_plan", nativePlanArgs(), "missing")
	var wire strings.Builder
	err = r.Stream(ctx, strings.NewReader(event("response.completed", map[string]any{"response": feedbackResponse("resp_missing", native, feedbackCall("functions.exec", "text('sibling tool may still execute')", "sibling"))})), func(b []byte) error { wire.Write(b); return nil })
	require.NoError(t, err)
	require.False(t, r.Failed, wire.String())
	final := streamSnapshot(t, wire.String())
	var calls []object
	require.NoError(t, json.Unmarshal(final["output"], &calls))
	require.Len(t, calls, 2, "the unavailable nested plan must not discard another tool")
	result, events := executeNativePlan(t, stringValue(calls[0]["input"]), false, "")
	require.Empty(t, events)
	require.Contains(t, string(result), "CLIENT_PLAN_TOOL_UNAVAILABLE")
	require.Equal(t, "text('sibling tool may still execute')", stringValue(calls[1]["input"]))
	next, err := Prepare(ctx, encoded(map[string]any{"tools": tools, "input": []any{
		map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(calls[0]["call_id"]), "output": string(result)},
		map[string]any{"type": "custom_tool_call_output", "call_id": stringValue(calls[1]["call_id"]), "output": "sibling tool completed"},
	}}), "missing-plan", store, nil, 256<<20)
	require.NoError(t, err)
	restored := preparedInput(t, next)
	require.Contains(t, string(encoded(restored)), "CLIENT_PLAN_TOOL_UNAVAILABLE")
	require.Contains(t, string(encoded(restored)), "call_missing")
	answer, err := next.Response(ctx, encoded(feedbackResponse("resp_continued", feedbackMessage("msg_continued", "The available tool completed the task."))))
	require.NoError(t, err)
	require.False(t, next.Failed)
	require.Contains(t, string(answer), "The available tool completed the task.")
}

func TestNativePlanDirectClientToolPreservesNativeCall(t *testing.T) {
	args := map[string]any{"explanation": "Native client tool", "plan": []any{map[string]string{"step": "Run the native tool", "status": "in_progress"}}}
	r, err := Prepare(context.Background(), encoded(map[string]any{"tools": []any{map[string]any{"type": "function", "name": "update_plan"}}, "input": "Use native plan"}), "direct-plan", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	raw, err := r.convertCall(context.Background(), nativeDiscoveryItem("update_plan", args, "direct"))
	require.NoError(t, err)
	call, err := parseObject(raw)
	require.NoError(t, err)
	require.Equal(t, "function_call", stringValue(call["type"]))
	require.Equal(t, "update_plan", stringValue(call["name"]))
	require.JSONEq(t, string(encoded(args)), stringValue(call["arguments"]))
	require.NotContains(t, string(raw), "bpsClientUpdatePlan")
}
