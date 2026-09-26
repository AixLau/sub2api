package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
)

// Preflight the entire batch before publishing any executable client output.
func (r *Request) resolveResponse(ctx context.Context, root object) (object, error) {
	var output []json.RawMessage
	if len(root["output"]) > 0 && json.Unmarshal(root["output"], &output) != nil {
		return nil, errors.New("上游 output 必须是数组")
	}
	converted := make([]json.RawMessage, len(output))
	failures := map[int]*ToolCallError{}
	var first *ToolCallError
	var capabilityFailure *ToolCallError
	calls := 0
	ids, callIDs := map[string]bool{}, map[string]bool{}
	for i, raw := range output {
		item, err := parseObject(raw)
		if err != nil {
			return nil, err
		}
		if isToolCall(item) {
			calls++
			id, callID := stringValue(item["id"]), stringValue(item["call_id"])
			if id == "" || callID == "" || ids[id] || callIDs[callID] {
				return nil, toolValidationError("upstream_tool_identity", "unreplayable_identity", "上游工具缺少唯一 id/call_id，不能安全回传工具结果")
			}
			ids[id], callIDs[callID] = true, true
		}
		if calls > 128 {
			return nil, errors.New("单次响应的工具数量超过 128")
		}
		converted[i], err = r.decodeCall(ctx, raw)
		if err != nil {
			var failure *ToolCallError
			if !errors.As(err, &failure) {
				return nil, err
			}
			failures[i] = failure
			if failure.stage == "upstream_tool_capability" {
				capabilityFailure = failure
			}
			if first == nil {
				first = failure
			}
		}
	}
	// Preflight the full batch: even a later unsupported native tool rules out
	// repairing the earlier malformed call. No sibling may escape for execution.
	if capabilityFailure != nil {
		return nil, capabilityFailure
	}
	if first != nil {
		if r.Feedback == nil || r.feedbackUsed || stringValue(root["status"]) != "completed" {
			return nil, first
		}
		return r.continueAfterToolFailure(ctx, root, output, failures)
	}
	if stringValue(root["status"]) == "completed" && r.catalog.choice == "required" && calls == 0 {
		return nil, toolValidationError("upstream_tool_choice", "required_tool_missing", "上游未返回 required 工具调用")
	}
	if status := stringValue(root["status"]); status == "failed" || status == "incomplete" {
		var visible []json.RawMessage
		for _, raw := range output {
			item, _ := parseObject(raw)
			if !isToolCall(item) {
				visible = append(visible, raw)
			}
		}
		root["output"] = encoded(visible)
		return root, nil
	}
	for i, raw := range output {
		if err := r.saveCall(ctx, raw, converted[i]); err != nil {
			return nil, err
		}
		if r.feedbackID != "" {
			item, _ := parseObject(converted[i])
			if id := stringValue(item["id"]); id != "" && !isToolCall(item) {
				if err := putState(ctx, r.store, stateKey(r.scope, "feedback_item", id), r.feedbackID); err != nil {
					return nil, err
				}
			}
		}
	}
	root["output"] = encoded(converted)
	return root, nil
}

type feedbackRecord struct {
	Items []json.RawMessage `json:"items"`
	Turn  Turn              `json:"turn"`
}

// A rejected batch is never sent to the client. Actual native calls and their
// failure outputs form one continuation, rather than resending the old request.
func (r *Request) continueAfterToolFailure(ctx context.Context, root object, output []json.RawMessage, failures map[int]*ToolCallError) (result object, resultErr error) {
	defer func() {
		if resultErr == nil || errors.Is(resultErr, context.Canceled) || errors.Is(resultErr, context.DeadlineExceeded) {
			return
		}
		var toolErr *ToolCallError
		var feedbackErr *FeedbackFailure
		if !errors.As(resultErr, &toolErr) && !errors.As(resultErr, &feedbackErr) {
			resultErr = &FeedbackFailure{Message: "工具错误反馈无法完成：" + resultErr.Error()}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Turn.Iteration+1 >= MaxIterations {
		return nil, errors.New("工具反馈达到 turn 轮次上限")
	}
	body, err := parseObject(r.Body)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if json.Unmarshal(body["input"], &input) != nil {
		return nil, errors.New("工具反馈缺少原请求 input")
	}
	var hidden, visible []json.RawMessage
	for _, raw := range output {
		item, _ := parseObject(raw)
		if isToolCall(item) {
			hidden = append(hidden, raw)
		} else {
			visible = append(visible, raw)
		}
		if stringValue(item["type"]) == "reasoning" && stringValue(item["encrypted_content"]) == "" {
			continue
		}
		input = append(input, raw)
	}
	for i, raw := range output {
		item, _ := parseObject(raw)
		if !isToolCall(item) {
			continue
		}
		detail := object{"code": encoded("TOOL_BATCH_NOT_EXECUTED"), "message": encoded("This call was not executed because another call in the same batch could not be converted. Submit any still-needed calls again using the client tool envelope.")}
		if failure := failures[i]; failure != nil {
			detail = object{"code": encoded("TOOL_BRIDGE_CONVERSION_FAILED"), "stage": encoded(failure.stage), "reason": encoded(failure.reason), "message": encoded(failure.Error())}
		}
		callID := stringValue(item["call_id"])
		typ := "function_call_output"
		if stringValue(item["type"]) == "custom_tool_call" {
			typ = "custom_tool_call_output"
		}
		result := encoded(object{"type": encoded(typ), "id": encoded(functionItemID(callID)), "call_id": item["call_id"],
			"output": encoded(string(encoded(object{"success": encoded(false), "executed": encoded(false), "error": encoded(detail)})))})
		input = append(input, result)
		hidden = append(hidden, result)
	}
	r.Turn.Iteration++
	r.feedbackID = digest(string(encoded(hidden)), r.Turn.ID)
	// Persist before continuing: replay also works if the repair returns text.
	if err := putState(ctx, r.store, stateKey(r.scope, "feedback", r.feedbackID), feedbackRecord{Items: hidden, Turn: r.Turn}); err != nil {
		return nil, err
	}
	body["input"] = encoded(input)
	meta, err := parseObject(body["metadata"])
	if err != nil {
		return nil, err
	}
	meta["agent_iteration"] = encoded(strconv.Itoa(r.Turn.Iteration))
	body["metadata"] = encoded(meta)
	nextBody := encoded(body)
	if len(nextBody) > MaxBodyBytes {
		return nil, errors.New("工具反馈请求超过大小限制")
	}
	r.feedbackUsed = true
	nextRaw, err := r.Feedback(ctx, nextBody)
	if err != nil {
		return nil, err
	}
	next, err := parseObject(nextRaw)
	if err != nil {
		return nil, err
	}
	root["usage"] = sumUsage(root["usage"], next["usage"])
	r.failureSnapshot = encoded(root)
	if stringValue(next["status"]) != "completed" {
		return nil, &FeedbackFailure{Message: "工具错误反馈未生成 completed 响应"}
	}
	// The continuation creates new calls. Reusing a native identity would
	// produce two different items/results for the same call during replay.
	ids, callIDs := map[string]bool{}, map[string]bool{}
	for _, raw := range output {
		item, _ := parseObject(raw)
		if id := stringValue(item["id"]); id != "" {
			ids[id] = true
		}
		if callID := stringValue(item["call_id"]); isToolCall(item) && callID != "" {
			callIDs[callID] = true
		}
	}
	var nextOutput []json.RawMessage
	if json.Unmarshal(next["output"], &nextOutput) != nil {
		return nil, errors.New("工具反馈 output 无效")
	}
	for _, raw := range nextOutput {
		item, _ := parseObject(raw)
		if ids[stringValue(item["id"])] || (isToolCall(item) && callIDs[stringValue(item["call_id"])]) {
			// Keep the precise conversion diagnostic when the repeated item
			// is still malformed, before checking valid-but-reused identities.
			if _, err := r.decodeCall(ctx, raw); err != nil {
				return nil, err
			}
			return nil, toolValidationError("upstream_tool_identity", "reused_feedback_identity", "工具反馈复用了上一轮调用标识，不能安全回放")
		}
	}
	next, err = r.resolveResponse(ctx, next)
	if err != nil {
		return nil, err
	}
	var final []json.RawMessage
	if json.Unmarshal(next["output"], &final) != nil {
		return nil, errors.New("工具反馈 output 无效")
	}
	next["output"] = encoded(append(visible, final...))
	next["usage"] = root["usage"]
	// Retain the response identity already sent to the downstream client.
	for _, field := range []string{"id", "created_at", "model"} {
		if value := root[field]; len(value) > 0 {
			next[field] = value
		}
	}
	return next, nil
}

// Sum nested token counters without converting JSON integers to floats.
func sumUsage(a, b json.RawMessage) json.RawMessage {
	left, ea := parseObject(a)
	right, eb := parseObject(b)
	if ea != nil {
		return b
	}
	if eb != nil {
		return a
	}
	for key, value := range right {
		if jsonKind(value) == "object" {
			left[key] = sumUsage(left[key], value)
			continue
		}
		x, ex := strconv.ParseInt(string(left[key]), 10, 64)
		y, ey := strconv.ParseInt(string(value), 10, 64)
		if ex == nil && ey == nil {
			left[key] = encoded(x + y)
		} else {
			left[key] = value
		}
	}
	return encoded(left)
}
