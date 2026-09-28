package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Regression for #485286: a bad FUNCTION payload consumed the sole hidden
// feedback round, then an unavailable write_range terminated the whole turn.
// A client with a result runtime should receive both errors through its normal
// tool loop, without another server-side inference or any requested side effect.
func TestClientToolFailuresContinueAndReplay(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, kind := range []string{"function_call", "custom_tool_call"} {
			t.Run(fmt.Sprintf("stream=%v/%s", stream, kind), func(t *testing.T) {
				ctx := context.Background()
				store := memoryStore{}
				tools := append(discoveryTools(), map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{
					map[string]any{"type": "function", "name": "list_agents"},
				}})
				input := []json.RawMessage{encoded(map[string]any{"role": "user", "content": "Inspect the task"})}
				for round := 0; round < 3; round++ {
					r, err := Prepare(ctx, encoded(map[string]any{"input": input, "tools": tools}), "client-errors", store, nil, 256<<20)
					require.NoError(t, err)
					require.Equal(t, round, r.Turn.Iteration)
					if round == 0 {
						r.Feedback = func(context.Context, []byte) ([]byte, error) {
							t.Fatal("client tool failures must not start hidden inference")
							return nil, nil
						}
					}
					var calls []json.RawMessage
					if round == 0 {
						calls = []json.RawMessage{feedbackCall("collaboration.list_agents", "await private_payload();", "malformed")}
					} else if round == 1 {
						unknown := object{"type": encoded(kind), "name": encoded("write_range"), "id": encoded("fc_unknown"), "call_id": encoded("call_unknown")}
						if kind == "function_call" {
							unknown["arguments"] = encoded(`{"value":"private_payload"}`)
						} else {
							unknown["input"] = encoded("private_payload")
						}
						calls = []json.RawMessage{encoded(unknown), feedbackCall("functions.exec", "await tools.update_plan({private_payload:true});", "held")}
					} else {
						calls = []json.RawMessage{feedbackCall("collaboration.list_agents", map[string]any{}, "corrected")}
					}
					initial := feedbackResponse(fmt.Sprintf("resp_%d", round), calls...)
					var final object
					if stream {
						var wire strings.Builder
						var upstream strings.Builder
						for i, call := range calls {
							upstream.WriteString(event("response.output_item.done", map[string]any{"output_index": i, "item": call}))
						}
						upstream.WriteString(event("response.completed", map[string]any{"response": initial}))
						err = r.Stream(ctx, strings.NewReader(upstream.String()), func(b []byte) error { wire.Write(b); return nil })
						require.NoError(t, err)
						final = streamSnapshot(t, wire.String())
					} else {
						raw, err := r.Response(ctx, encoded(initial))
						require.NoError(t, err)
						final, err = parseObject(raw)
						require.NoError(t, err)
					}
					require.Equal(t, "completed", stringValue(final["status"]))
					require.False(t, r.Failed)
					require.JSONEq(t, string(initial["usage"]), string(final["usage"]))
					var delivered []object
					require.NoError(t, json.Unmarshal(final["output"], &delivered))
					require.Len(t, delivered, len(calls))
					if round == 2 {
						require.Equal(t, "collaboration", stringValue(delivered[0]["namespace"]))
						require.Equal(t, "list_agents", stringValue(delivered[0]["name"]))
						require.JSONEq(t, "{}", stringValue(delivered[0]["arguments"]))
						continue
					}
					input = nil // Also exercise clients sending results without call items.
					for i, call := range delivered {
						require.Equal(t, "custom_tool_call", stringValue(call["type"]))
						require.Equal(t, "functions", stringValue(call["namespace"]))
						require.Equal(t, "exec", stringValue(call["name"]))
						require.True(t, strings.HasPrefix(stringValue(call["id"]), "ctc_"))
						program := stringValue(call["input"])
						require.NotContains(t, program, "private_payload")
						result, sideEffects := executeNativePlan(t, program, true, "")
						require.Empty(t, sideEffects, "error delivery must never execute the requested operation")
						var results []object
						require.NoError(t, json.Unmarshal(result, &results))
						require.Len(t, results, 1)
						require.Equal(t, "false", string(results[0]["success"]))
						require.Equal(t, "false", string(results[0]["executed"]))
						detail, err := parseObject(results[0]["error"])
						require.NoError(t, err)
						want := "TOOL_BRIDGE_CONVERSION_FAILED"
						if round == 1 {
							want = "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE"
							if i == 1 {
								want = "TOOL_BATCH_NOT_EXECUTED"
							}
						}
						require.Equal(t, want, stringValue(detail["code"]))
						input = append(input, encoded(object{"type": encoded("custom_tool_call_output"), "call_id": call["call_id"], "output": encoded(string(result))}))
					}
					replayed, err := Prepare(ctx, encoded(map[string]any{"input": input, "tools": tools}), "client-errors", store, nil, 256<<20)
					require.NoError(t, err)
					fullInput := make([]json.RawMessage, 0, len(delivered)+len(input))
					for _, call := range delivered {
						fullInput = append(fullInput, encoded(call))
					}
					fullInput = append(fullInput, input...)
					fullReplay, err := Prepare(ctx, encoded(map[string]any{"input": fullInput, "tools": tools}), "client-errors", store, nil, 256<<20)
					require.NoError(t, err)
					// Results-only replay inserts each original beside its result;
					// full history keeps the client's complete call batch first.
					require.ElementsMatch(t, preparedInput(t, replayed), preparedInput(t, fullReplay))
					for _, original := range calls {
						item, _ := parseObject(original)
						callCount, resultCount := 0, 0
						for _, raw := range preparedInput(t, replayed) {
							restored, _ := parseObject(raw)
							if stringValue(restored["call_id"]) != stringValue(item["call_id"]) {
								continue
							}
							if isToolCall(restored) {
								callCount++
								require.JSONEq(t, string(original), string(raw))
							} else {
								resultCount++
								require.Equal(t, stringValue(item["type"])+"_output", stringValue(restored["type"]))
							}
						}
						require.Equal(t, 1, callCount)
						require.Equal(t, 1, resultCount)
					}
				}
			})
		}
	}
}

func TestClientToolFailureBoundaries(t *testing.T) {
	for _, mode := range []string{"no_runtime", "undeclared_contract", "none", "forced_other", "forced_exec", "missing_id", "duplicate", "failed", "incomplete", "cancelled", "store_failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			r, err := Prepare(ctx, encoded(map[string]any{"input": "Inspect", "tools": discoveryTools()}), "boundaries", memoryStore{}, nil, 256<<20)
			require.NoError(t, err)
			call := nativeDiscoveryItem("write_range", map[string]any{}, "bad")
			root := feedbackResponse("resp_bad", call)
			switch mode {
			case "no_runtime":
				r.catalog.tools = map[string]tool{}
			case "undeclared_contract":
				runtime := r.catalog.tools["functions.exec"]
				runtime.Description = "An unrelated tool named exec"
				r.catalog.tools["functions.exec"] = runtime
			case "none":
				r.catalog.choice = "none"
			case "forced_other":
				r.catalog.choice, r.catalog.forced = "required", "other_tool"
			case "forced_exec":
				r.catalog.choice, r.catalog.forced = "required", "functions.exec"
			case "missing_id":
				item, _ := parseObject(call)
				delete(item, "id")
				root["output"] = encoded([]object{item})
			case "duplicate":
				root["output"] = encoded([]json.RawMessage{call, call})
			case "failed", "incomplete":
				root["status"] = encoded(mode)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "store_failure":
				r.store = brokenStore{memoryStore{}}
			}
			_, err = r.Response(ctx, encoded(root))
			if mode == "forced_exec" {
				require.NoError(t, err)
				require.Len(t, r.converted, 1)
			} else {
				require.Error(t, err)
				require.Empty(t, r.converted)
			}
		})
	}
}

func TestClientToolFailureDoesNotInvokeDeclaredOfficeExecutor(t *testing.T) {
	ctx := context.Background()
	tools := append(discoveryTools(), map[string]any{"type": "function", "name": "run_officejs"})
	r, err := Prepare(ctx, encoded(map[string]any{"input": "Inspect", "tools": tools}), "declared-office", memoryStore{}, nil, 256<<20)
	require.NoError(t, err)
	unknown := nativeDiscoveryItem("write_range", map[string]any{}, "unknown")
	raw, err := r.Response(ctx, encoded(feedbackResponse("resp_unknown", unknown)))
	require.NoError(t, err)
	root, err := parseObject(raw)
	require.NoError(t, err)
	var calls []object
	require.NoError(t, json.Unmarshal(root["output"], &calls))
	require.Len(t, calls, 1)
	require.Equal(t, "custom_tool_call", stringValue(calls[0]["type"]))
	require.Equal(t, "functions", stringValue(calls[0]["namespace"]))
	require.Equal(t, "exec", stringValue(calls[0]["name"]))
	result, sideEffects := executeNativePlan(t, stringValue(calls[0]["input"]), true, "")
	require.Empty(t, sideEffects)
	require.Contains(t, string(result), "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE")
}
