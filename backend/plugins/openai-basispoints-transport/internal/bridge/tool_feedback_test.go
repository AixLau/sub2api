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

func feedbackCall(name string, payload any, suffix string) json.RawMessage {
	item, _ := parseObject(officeItem(name, payload, false))
	item["id"], item["call_id"] = encoded("fc_"+suffix), encoded("call_"+suffix)
	return encoded(item)
}
func feedbackMessage(id, text string) json.RawMessage {
	return encoded(object{"type": encoded("message"), "id": encoded(id), "role": encoded("assistant"), "status": encoded("completed"), "content": encoded([]any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}})})
}
func feedbackResponse(id string, output ...json.RawMessage) object {
	return object{"id": encoded(id), "status": encoded("completed"), "output": encoded(output), "usage": encoded(map[string]any{"input_tokens": 11, "output_tokens": 3, "total_tokens": 14, "input_tokens_details": map[string]int{"cached_tokens": 5}})}
}
func streamSnapshot(t *testing.T, wire string) object {
	t.Helper()
	sequence := 0
	var final object
	for _, line := range strings.Split(wire, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		e, err := parseObject([]byte(strings.TrimPrefix(line, "data: ")))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprint(sequence), string(e["sequence_number"]))
		sequence++
		switch stringValue(e["type"]) {
		case "response.completed", "response.failed", "response.incomplete":
			require.Nil(t, final, "only one terminal event")
			final, err = parseObject(e["response"])
			require.NoError(t, err)
		}
	}
	require.NotNil(t, final)
	return final
}

func TestToolFeedbackRepairsBatchWithoutDispatchingRejectedCalls(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, textOnly := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/text=%v", stream, textOnly), func(t *testing.T) {
				r := customRequest(t)
				good := feedbackCall("functions.read_file", map[string]any{"path": "not executed"}, "held")
				bad := feedbackCall("tools.exec_command", map[string]any{"cmd": "private command"}, "bad")
				before := feedbackMessage("msg_before", "Checking.")
				initial := feedbackResponse("resp_initial", good, before, bad)
				corrected := feedbackCall("functions.exec", "text('corrected')\n", "fixed")
				if textOnly {
					corrected = feedbackMessage("msg_fixed", "Done.")
				}
				repair := feedbackResponse("resp_repair", corrected)
				hits := 0
				var emitted strings.Builder
				r.Feedback = func(ctx context.Context, body []byte) ([]byte, error) {
					hits++
					require.Empty(t, r.converted, "preflight cannot save or dispatch a valid sibling")
					require.NotContains(t, emitted.String(), "function_call_arguments.delta")
					require.NotContains(t, emitted.String(), "custom_tool_call_input.delta")
					root, err := parseObject(body)
					require.NoError(t, err)
					var input []object
					require.NoError(t, json.Unmarshal(root["input"], &input))
					results := map[string]object{}
					var nativeCalls []string
					for _, item := range input {
						if isToolCall(item) {
							nativeCalls = append(nativeCalls, stringValue(item["call_id"]))
						}
						if stringValue(item["type"]) == "function_call_output" {
							var result object
							require.NoError(t, json.Unmarshal([]byte(stringValue(item["output"])), &result))
							require.Equal(t, "false", string(result["executed"]))
							require.Equal(t, "false", string(result["success"]))
							detail, _ := parseObject(result["error"])
							results[stringValue(item["call_id"])] = detail
						}
					}
					require.Equal(t, []string{"call_held", "call_bad"}, nativeCalls)
					require.Equal(t, "TOOL_BATCH_NOT_EXECUTED", stringValue(results["call_held"]["code"]))
					require.Equal(t, "TOOL_BRIDGE_CONVERSION_FAILED", stringValue(results["call_bad"]["code"]))
					require.NotContains(t, string(encoded(results)), "private command")
					meta, _ := parseObject(root["metadata"])
					require.Equal(t, r.Turn.ID, stringValue(meta["turn_id"]))
					require.Equal(t, "1", stringValue(meta["agent_iteration"]))
					return encoded(repair), nil
				}
				var final object
				if stream {
					wire := event("response.created", map[string]any{"response": map[string]any{"id": "resp_initial", "output": []any{}}}) +
						event("response.output_item.done", map[string]any{"output_index": 0, "item": good}) +
						event("response.output_item.added", map[string]any{"output_index": 1, "item": before}) +
						event("response.output_text.delta", map[string]any{"output_index": 1, "item_id": "msg_before", "delta": "Checking."}) +
						event("response.output_item.done", map[string]any{"output_index": 1, "item": before}) +
						event("response.output_item.done", map[string]any{"output_index": 2, "item": bad}) +
						event("response.completed", map[string]any{"response": initial})
					require.NoError(t, r.Stream(context.Background(), strings.NewReader(wire), func(b []byte) error { emitted.Write(b); return nil }))
					final = streamSnapshot(t, emitted.String())
					require.Equal(t, 1, strings.Count(emitted.String(), "event: response.output_text.delta")-map[bool]int{true: 1, false: 0}[textOnly])
					require.NotContains(t, emitted.String(), "run_officejs")
					require.NotContains(t, emitted.String(), "call_held")
					require.NotContains(t, emitted.String(), "private command")
				} else {
					out, err := r.Response(context.Background(), encoded(initial))
					require.NoError(t, err)
					final, _ = parseObject(out)
				}
				require.Equal(t, 1, hits)
				require.False(t, r.Failed)
				require.Equal(t, "resp_initial", stringValue(final["id"]))
				usage, _ := parseObject(final["usage"])
				require.Equal(t, "28", string(usage["total_tokens"]))
				details, _ := parseObject(usage["input_tokens_details"])
				require.Equal(t, "10", string(details["cached_tokens"]))
				var output []json.RawMessage
				require.NoError(t, json.Unmarshal(final["output"], &output))
				require.Len(t, output, 2)
				replayInput := append([]json.RawMessage{}, output...)
				if !textOnly {
					call, _ := parseObject(output[1])
					replayInput = append(replayInput, encoded(object{"type": encoded("custom_tool_call_output"), "call_id": call["call_id"], "output": encoded("client result")}))
				}
				next, err := Prepare(context.Background(), encoded(map[string]any{"input": replayInput}), "custom-session", r.store, nil, 256<<20)
				require.NoError(t, err)
				require.Equal(t, 2, next.Turn.Iteration)
				replay := preparedInput(t, next)
				require.Equal(t, 1, strings.Count(string(encoded(replay)), `"call_id":"call_bad","id":"fc_bad"`), "hidden call restored exactly once")
				calls, results := map[string]int{}, map[string]int{}
				for _, raw := range replay {
					item, _ := parseObject(raw)
					if isToolCall(item) {
						calls[stringValue(item["call_id"])]++
					}
					if strings.HasSuffix(stringValue(item["type"]), "call_output") {
						results[stringValue(item["call_id"])]++
					}
				}
				require.Equal(t, 1, calls["call_bad"])
				require.Equal(t, 1, results["call_bad"])
				require.Equal(t, 1, calls["call_held"])
				require.Equal(t, 1, results["call_held"])
				if !textOnly {
					require.Equal(t, 1, calls["call_fixed"])
					require.Equal(t, 1, results["call_fixed"])
					// Compact clients may send only the last tool result.
					onlyResult, err := Prepare(context.Background(), encoded(map[string]any{"input": replayInput[len(replayInput)-1:]}), "custom-session", r.store, nil, 256<<20)
					require.NoError(t, err)
					require.Equal(t, 2, onlyResult.Turn.Iteration)
					compact := string(encoded(preparedInput(t, onlyResult)))
					require.Contains(t, compact, "TOOL_BRIDGE_CONVERSION_FAILED")
					require.Contains(t, compact, "TOOL_BATCH_NOT_EXECUTED")
					require.Contains(t, compact, "client result")
				}
			})
		}
	}
}

func TestToolFeedbackBoundaries(t *testing.T) {
	for _, mode := range []string{"repeated", "missing_id", "duplicate_call", "cancelled", "store_failure", "required", "upstream_failed"} {
		t.Run(mode, func(t *testing.T) {
			r := customRequest(t)
			bad := feedbackCall("not_declared", map[string]any{}, "bad")
			response := feedbackResponse("resp_bad", bad)
			ctx := context.Background()
			switch mode {
			case "missing_id":
				item, _ := parseObject(bad)
				delete(item, "call_id")
				response["output"] = encoded([]any{item})
			case "duplicate_call":
				response["output"] = encoded([]any{bad, bad})
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "store_failure":
				r.store = brokenStore{memoryStore{}}
			case "required":
				r.catalog.choice = "required"
			case "upstream_failed":
				response["status"] = encoded("failed")
			}
			hits := 0
			r.Feedback = func(context.Context, []byte) ([]byte, error) {
				hits++
				if mode == "required" {
					return encoded(feedbackResponse("resp_empty")), nil
				}
				return encoded(response), nil
			}
			_, err := r.Response(ctx, encoded(response))
			if mode == "upstream_failed" {
				require.NoError(t, err)
				require.True(t, r.Failed)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, r.converted)
			if mode == "repeated" || mode == "required" {
				require.Equal(t, 1, hits)
			} else {
				require.Zero(t, hits)
			}
		})
	}
}

func TestToolFeedbackCancellationDoesNotDispatch(t *testing.T) {
	r := customRequest(t)
	r.Feedback = func(context.Context, []byte) ([]byte, error) { return nil, context.Canceled }
	_, err := r.Response(context.Background(), encoded(feedbackResponse("resp_bad", feedbackCall("unknown", nil, "bad"))))
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, r.converted)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestToolFeedbackPreservesFailedAndIncompleteResponses(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", status, stream), func(t *testing.T) {
				r := customRequest(t)
				r.catalog.choice = "required"
				initial := feedbackResponse("resp_initial", feedbackCall("unknown", nil, "bad"), feedbackMessage("msg_before", "Before."))
				initial["model"], initial["created_at"] = encoded("test-model"), encoded(123)
				next := feedbackResponse("resp_continuation", encoded(object{"type": encoded("function_call"), "name": encoded("write_range"), "arguments": encoded("private executable input")}), feedbackMessage("msg_partial", "Partial."))
				next["status"] = encoded(status)
				if status == "failed" {
					next["error"] = encoded(map[string]any{"code": "rate_limit_exceeded", "message": "TPM limit", "headers": map[string]string{"retry-after-ms": "231"}})
				} else {
					next["incomplete_details"] = encoded(map[string]string{"reason": "max_output_tokens"})
				}
				hits := 0
				r.Feedback = func(context.Context, []byte) ([]byte, error) { hits++; return encoded(next), nil }
				var final object
				if stream {
					var emitted strings.Builder
					wire := event("response.completed", map[string]any{"response": initial})
					require.NoError(t, r.Stream(context.Background(), strings.NewReader(wire), func(b []byte) error { emitted.Write(b); return nil }))
					final = streamSnapshot(t, emitted.String())
					require.Contains(t, emitted.String(), "event: response."+status)
					require.NotContains(t, emitted.String(), "event: response.completed")
				} else {
					raw, err := r.Response(context.Background(), encoded(initial))
					require.NoError(t, err)
					final, err = parseObject(raw)
					require.NoError(t, err)
				}
				require.Equal(t, 1, hits)
				require.True(t, r.Failed)
				require.Empty(t, r.converted)
				for _, field := range []string{"id", "model", "created_at"} {
					require.JSONEq(t, string(initial[field]), string(final[field]))
				}
				require.Equal(t, status, stringValue(final["status"]))
				for _, field := range []string{"error", "incomplete_details"} {
					if len(next[field]) > 0 {
						require.JSONEq(t, string(next[field]), string(final[field]))
					}
				}
				var output []object
				var counters struct {
					Total int `json:"total_tokens"`
				}
				require.NoError(t, json.Unmarshal(final["usage"], &counters))
				require.Equal(t, 28, counters.Total)
				require.NoError(t, json.Unmarshal(final["output"], &output))
				require.Len(t, output, 2)
				require.NotContains(t, string(encoded(final)), "private executable input")
				require.NotContains(t, string(encoded(final)), "function_call")
			})
		}
	}
}

func TestToolFeedbackRejectsReusedNativeCallID(t *testing.T) {
	r := customRequest(t)
	r.Feedback = func(context.Context, []byte) ([]byte, error) {
		fixed, _ := parseObject(feedbackCall("functions.exec", "correct input", "new"))
		fixed["call_id"] = encoded("call_bad")
		return encoded(feedbackResponse("resp_new", encoded(fixed))), nil
	}
	_, err := r.Response(context.Background(), encoded(feedbackResponse("resp_bad", feedbackCall("unknown", nil, "bad"))))
	var failure *ToolCallError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, "reused_feedback_identity", failure.reason)
	require.Empty(t, r.converted)
}

func TestStreamRejectsReplacedToolInTerminalSnapshot(t *testing.T) {
	r := customRequest(t)
	call := feedbackCall("functions.exec", "test input", "original")
	wire := event("response.output_item.done", map[string]any{"output_index": 0, "item": call}) +
		event("response.completed", map[string]any{"response": feedbackResponse("resp_end", feedbackMessage("msg_end", "Changed"))})
	var emitted strings.Builder
	err := r.Stream(context.Background(), strings.NewReader(wire), func(b []byte) error { emitted.Write(b); return nil })
	require.ErrorContains(t, err, "替换为非工具项")
	require.NotContains(t, emitted.String(), "custom_tool_call")
	require.Empty(t, r.converted)
}

func TestStreamPreservesFailureAfterUnfinishedToolBatch(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		t.Run(status, func(t *testing.T) {
			r := customRequest(t)
			r.Feedback = func(context.Context, []byte) ([]byte, error) {
				t.Fatal("failed batch must not be repaired")
				return nil, nil
			}
			call := feedbackCall("unknown", nil, "held")
			terminal := object{"id": encoded("resp_failed"), "output": encoded([]any{}), "error": encoded(map[string]string{"code": "server_error", "message": "upstream failed"})}
			wire := event("response.output_item.done", map[string]any{"output_index": 0, "item": call}) + event("response."+status, map[string]any{"response": terminal})
			var emitted strings.Builder
			require.NoError(t, r.Stream(context.Background(), strings.NewReader(wire), func(b []byte) error { emitted.Write(b); return nil }))
			final := streamSnapshot(t, emitted.String())
			require.Equal(t, status, stringValue(final["status"]))
			require.JSONEq(t, string(terminal["error"]), string(final["error"]))
			require.JSONEq(t, `[]`, string(final["output"]))
			require.True(t, r.Failed)
			require.Empty(t, r.converted)
			require.NotContains(t, emitted.String(), "unknown")
		})
	}
}
