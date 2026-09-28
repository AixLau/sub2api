package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Codex ToolRegistry returns RespondToModel for an unknown tool; its runtime
// preserves call_id and returns a failed function/custom result to the model.
func TestUnsupportedToolFeedbackAndReplay(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, kind := range []string{"function_call", "custom_tool_call"} {
			for _, textOnly := range []bool{false, true} {
				t.Run(fmt.Sprintf("stream=%v/%s/text=%v", stream, kind, textOnly), func(t *testing.T) {
					ctx := context.Background()
					r := customRequest(t)
					unknown := object{"type": encoded(kind), "name": encoded("write_range"), "id": encoded("fc_unknown"), "call_id": encoded("call_unknown")}
					if kind == "function_call" {
						unknown["arguments"] = encoded(`{"sheetId":"private_sheet","writes":[{"cell":"A1","value":"private_value"}]}`)
					} else {
						unknown["namespace"] = encoded("functions")
						unknown["input"] = encoded("private_value")
					}
					held := feedbackCall("functions.exec", "text('held sibling')", "held")
					// Put a different conversion failure first: unsupported tools must
					// not veto feedback for the entire batch.
					malformed := feedbackCall("undeclared", nil, "malformed")
					initial := feedbackResponse("resp_initial", malformed, held, encoded(unknown))
					repaired := feedbackCall("functions.exec", "text('real client tool')", "fixed")
					if textOnly {
						repaired = feedbackMessage("msg_fixed", "This client has no spreadsheet write tool.")
					}
					hits := 0
					r.Feedback = func(_ context.Context, body []byte) ([]byte, error) {
						hits++
						require.Empty(t, r.converted, "no sibling may escape before feedback")
						root, err := parseObject(body)
						require.NoError(t, err)
						var input []object
						require.NoError(t, json.Unmarshal(root["input"], &input))
						results := map[string]object{}
						for _, item := range input {
							if isToolCall(item) && stringValue(item["call_id"]) == "call_unknown" {
								require.JSONEq(t, string(encoded(unknown)), string(encoded(item)))
							}
							if strings.HasSuffix(stringValue(item["type"]), "call_output") {
								prefix := "fc_"
								if stringValue(item["type"]) == "custom_tool_call_output" {
									prefix = "ctco_"
								}
								require.True(t, strings.HasPrefix(stringValue(item["id"]), prefix), "feedback result ID must match the native type")
								result, err := parseObject([]byte(stringValue(item["output"])))
								require.NoError(t, err)
								require.Equal(t, "false", string(result["success"]))
								require.Equal(t, "false", string(result["executed"]))
								detail, err := parseObject(result["error"])
								require.NoError(t, err)
								results[stringValue(item["call_id"])] = detail
								if stringValue(item["call_id"]) == "call_unknown" {
									require.Equal(t, kind+"_output", stringValue(item["type"]))
								}
							}
						}
						require.Len(t, results, 3)
						require.Equal(t, "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", stringValue(results["call_unknown"]["code"]))
						require.Equal(t, "upstream_tool_capability", stringValue(results["call_unknown"]["stage"]))
						require.Contains(t, stringValue(results["call_unknown"]["message"]), "client-tool:FULL_CATALOG_NAME")
						require.Equal(t, "TOOL_BATCH_NOT_EXECUTED", stringValue(results["call_held"]["code"]))
						require.Equal(t, "TOOL_BRIDGE_CONVERSION_FAILED", stringValue(results["call_malformed"]["code"]))
						require.NotContains(t, string(encoded(results)), "private_value")
						return encoded(feedbackResponse("resp_fixed", repaired)), nil
					}
					var final object
					if stream {
						var emitted strings.Builder
						wire := event("response.output_item.done", map[string]any{"output_index": 2, "item": unknown}) +
							event("response.completed", map[string]any{"response": initial})
						require.NoError(t, r.Stream(ctx, strings.NewReader(wire), func(b []byte) error { emitted.Write(b); return nil }))
						final = streamSnapshot(t, emitted.String())
						require.NotContains(t, emitted.String(), "private_value")
						require.NotContains(t, emitted.String(), "call_unknown")
						require.NotContains(t, emitted.String(), "held sibling")
					} else {
						raw, err := r.Response(ctx, encoded(initial))
						require.NoError(t, err)
						final, err = parseObject(raw)
						require.NoError(t, err)
					}
					require.Equal(t, 1, hits)
					require.Equal(t, "completed", stringValue(final["status"]))
					require.Equal(t, "resp_initial", stringValue(final["id"]))
					usage, _ := parseObject(final["usage"])
					require.Equal(t, "28", string(usage["total_tokens"]))
					var output []object
					require.NoError(t, json.Unmarshal(final["output"], &output))
					require.Len(t, output, 1)
					if !textOnly {
						require.Equal(t, "exec", stringValue(output[0]["name"]))
						// Exercise compact clients that send only the last tool result.
						output = []object{{"type": encoded("custom_tool_call_output"), "call_id": output[0]["call_id"], "output": encoded("client result")}}
					}
					next, err := Prepare(ctx, encoded(map[string]any{"input": output}), "custom-session", r.store, nil, 256<<20)
					require.NoError(t, err)
					require.Equal(t, 2, next.Turn.Iteration)
					calls, results := 0, 0
					for _, raw := range preparedInput(t, next) {
						item, _ := parseObject(raw)
						if stringValue(item["call_id"]) != "call_unknown" {
							continue
						}
						if isToolCall(item) {
							calls++
							require.JSONEq(t, string(encoded(unknown)), string(raw))
						} else {
							results++
							require.Equal(t, kind+"_output", stringValue(item["type"]))
							require.Contains(t, stringValue(item["output"]), "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE")
						}
					}
					require.Equal(t, 1, calls)
					require.Equal(t, 1, results)
				})
			}
		}
	}
}

func TestUnsupportedToolFeedbackBoundaries(t *testing.T) {
	for _, mode := range []string{"no_feedback", "repeated", "missing_id", "duplicate_call", "cancelled", "store_failure", "turn_limit", "upstream_failed", "required", "none", "forced"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			r := customRequest(t)
			unknown := object{"type": encoded("function_call"), "name": encoded("write_range"), "id": encoded("fc_bad"), "call_id": encoded("call_bad"), "arguments": encoded(`{}`)}
			initial := feedbackResponse("resp_bad", encoded(unknown))
			repaired := feedbackResponse("resp_fixed", feedbackCall("functions.exec", "text('client result')", "fixed"))
			wantHits := 0
			switch mode {
			case "repeated":
				wantHits = 1
				unknown["id"], unknown["call_id"] = encoded("fc_again"), encoded("call_again")
				repaired["output"] = encoded([]any{unknown})
			case "missing_id":
				delete(unknown, "call_id")
				initial["output"] = encoded([]any{unknown})
			case "duplicate_call":
				initial["output"] = encoded([]any{unknown, unknown})
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "store_failure":
				r.store = brokenStore{memoryStore{}}
			case "turn_limit":
				r.Turn.Iteration = MaxIterations - 1
			case "upstream_failed":
				initial["status"] = encoded("failed")
			case "required":
				wantHits = 1
				r.catalog.choice = "required"
				repaired["output"] = encoded([]any{})
			case "none":
				wantHits = 1
				r.catalog.choice = "none"
			case "forced":
				wantHits = 1
				r.catalog.forced = "functions.read_file"
			}
			hits := 0
			if mode != "no_feedback" {
				r.Feedback = func(context.Context, []byte) ([]byte, error) {
					hits++
					return encoded(repaired), nil
				}
			}
			_, err := r.Response(ctx, encoded(initial))
			require.Error(t, err)
			require.Equal(t, wantHits, hits)
			require.Empty(t, r.converted)
			if mode == "cancelled" {
				require.True(t, errors.Is(err, context.Canceled))
			}
			if mode == "no_feedback" || mode == "repeated" {
				require.Equal(t, "TOOL_BRIDGE_CAPABILITY_UNAVAILABLE", FailureCode(err))
			}
		})
	}
}
