package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// isTransportName reports whether a native call rides the client-tool transport.
// Some hosts display the tool as functions.run_officejs.
func isTransportName(name string) bool {
	return name == "run_officejs" || name == "functions.run_officejs"
}

// ToolCallError contains only safe diagnostic text, never executable input.
type ToolCallError struct {
	message              string
	diagnostics          *toolCallDiagnostics
	stage, reason, field string
	jsonOffset           int64
}

func (e *ToolCallError) Error() string   { return e.message }
func toolCallError(message string) error { return &ToolCallError{message: message} }

func toolValidationError(stage, reason, message string) error {
	return &ToolCallError{message: message, stage: stage, reason: reason}
}

// FailureResponse carries a semantic rejection without breaking the transport.
// Only response identity and usage are copied; executable output never escapes.
func FailureResponse(code string, cause error, snapshot json.RawMessage) json.RawMessage {
	response := object{"id": encoded("resp_" + uuid.NewString()), "object": encoded("response"), "status": encoded("failed"), "output": encoded([]any{})}
	if previous, err := parseObject(snapshot); err == nil {
		for _, field := range []string{"id", "model", "created_at", "usage"} {
			if value, ok := previous[field]; ok {
				response[field] = value
			}
		}
	}
	detail := map[string]any{"type": "invalid_request_error", "code": code, "message": cause.Error()}
	var callErr *ToolCallError
	if errors.As(cause, &callErr) && callErr.diagnostics != nil {
		detail["diagnostics"] = callErr.diagnostics
	}
	var feedbackErr *FeedbackFailure
	if errors.As(cause, &feedbackErr) {
		// The rejected tool batch remains the cause after repair fails.
		// Keep it a non-retryable tool error, with the failed stage explicit.
		detail["diagnostics"] = map[string]string{"stage": "upstream_tool_feedback", "reason": "continuation_failed"}
	}
	var replayErr *EncryptedReplayError
	if errors.As(cause, &replayErr) {
		detail["diagnostics"] = replayErr
	}
	response["error"] = encoded(detail)
	return encoded(response)
}

// convertCall receives a COMPLETE output item. In particular, summary,
// references, status, id and unknown future fields must survive in Original.
// Every delivered call must resolve to a declared client tool. Invalid server
// calls must not escape as unsupported Office tools and poison the next turn.
func (r *Request) convertCall(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	converted, err := r.decodeCall(ctx, raw)
	if err != nil {
		return nil, err
	}
	if err := r.saveCall(ctx, raw, converted); err != nil {
		return nil, err
	}
	return converted, nil
}

// Decode performs no persistence or execution. A whole batch must pass before
// any client call is published, including streams with parallel tool calls.
func (r *Request) decodeCall(ctx context.Context, raw json.RawMessage) (convertedItem json.RawMessage, resultErr error) {
	item, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	typ := stringValue(item["type"])
	if typ != "function_call" && typ != "custom_tool_call" {
		return raw, nil
	}
	defer func() {
		if resultErr != nil {
			r.observeCallFailure(ctx, item, resultErr)
		}
	}()
	callID := stringValue(item["call_id"])
	if callID == "" {
		return nil, toolValidationError("upstream_tool_identity", "missing_call_id", "上游工具 item 缺少 call_id")
	}
	name := qualifiedCallName(item)
	var envelope object
	direct := false
	if t, key, declared := r.catalog.lookup(name); declared {
		direct = true
		// A declared tool's code field is business data, not a transport.
		args := item["arguments"]
		if t.Custom {
			args = item["input"]
		} else if text := stringValue(args); text != "" {
			args = []byte(text)
		}
		envelope = object{"tool": encoded(key), "args": args}
	} else {
		envelope, err = r.catalog.transportPayload(item)
		if err != nil {
			return nil, err
		}
	}
	if r.catalog.choice == "none" {
		return nil, toolValidationError("upstream_tool_choice", "tool_choice_none", "上游违反 tool_choice=none")
	}
	id := stringValue(item["id"])
	if id == "" {
		return nil, toolValidationError("upstream_tool_identity", "missing_item_id", "上游工具 item 缺少 id 或 call_id，无法完整回放")
	}
	canonicalOriginal := string(encoded(item))
	if r.originals[id] == canonicalOriginal {
		return r.converted[id], nil
	}
	if len(r.converted) >= 128 && r.converted[id] == nil {
		return nil, errors.New("单次响应的工具数量超过 128")
	}
	tool := stringValue(envelope["tool"])
	t, toolKey, ok := r.catalog.lookup(tool)
	if !ok {
		return nil, toolCallError("上游工具信封指定了客户端未声明的工具")
	}
	if r.catalog.forced != "" && r.catalog.forced != toolKey && r.catalog.forced != tool {
		return nil, toolValidationError("upstream_tool_choice", "forced_tool_mismatch", "上游未遵循指定工具的 tool_choice")
	}
	alias := callPrefix + digest(r.scope, r.Turn.ID, id, callID)[:32]
	out := object{"type": encoded("function_call"), "id": item["id"], "call_id": encoded(alias), "name": encoded(t.Name)}
	if value := item["status"]; len(value) > 0 {
		out["status"] = value
	}
	if t.Namespace != "" {
		out["namespace"] = encoded(t.Namespace)
	}
	if t.Custom {
		var input string
		if !isTextValue(envelope["args"]) || json.Unmarshal(envelope["args"], &input) != nil {
			return nil, toolValidationError("upstream_tool_arguments", "custom_input_not_string", "上游 custom 工具的 args 必须是 JSON 字符串")
		}
		out["type"] = encoded("custom_tool_call")
		out["input"] = encoded(input)
	} else {
		if _, err := parseObject(envelope["args"]); err != nil {
			return nil, toolValidationError("upstream_tool_arguments", "function_arguments_not_object", "上游 function 工具的 args 必须是 JSON 对象")
		}
		out["arguments"] = encoded(string(envelope["args"]))
		// An absent/null list lets Codex infer encryption from tool schemas.
		// Relay arguments are plaintext, even if the outer Office executor
		// carries encryption metadata for its own fields. Only direct calls
		// can declare encrypted arguments in the client's field namespace.
		out["encrypted_function_args"] = encoded([]string{})
		if fields := bytes.TrimSpace(item["encrypted_function_args"]); direct && len(fields) > 0 && string(fields) != "null" {
			var names []json.RawMessage
			if json.Unmarshal(fields, &names) != nil {
				return nil, toolCallError("上游 encrypted_function_args 必须是字符串数组")
			}
			canonical := make([]string, len(names))
			for i, name := range names {
				if !isTextValue(name) {
					return nil, toolCallError("上游 encrypted_function_args 必须是字符串数组")
				}
				canonical[i] = stringValue(name)
			}
			out["encrypted_function_args"] = encoded(canonical)
		}
	}
	converted := encoded(out)
	if previous := r.converted[id]; previous != nil {
		// The final response may add metadata but cannot change an already
		// delivered function's identity or arguments.
		a, _ := parseObject(previous)
		for _, key := range []string{"type", "call_id", "name", "namespace", "arguments", "input", "encrypted_function_args"} {
			if string(a[key]) != string(out[key]) {
				return nil, errors.New("上游在完成事件中修改了已输出的工具调用")
			}
		}
	}
	return converted, nil
}

func (r *Request) saveCall(ctx context.Context, raw, converted json.RawMessage) error {
	item, _ := parseObject(raw)
	if !isToolCall(item) {
		return nil
	}
	out, _ := parseObject(converted)
	id, alias := stringValue(item["id"]), stringValue(out["call_id"])
	if err := putState(ctx, r.store, stateKey(r.scope, "call", alias), callRecord{Original: raw, Client: converted, Turn: r.Turn, FeedbackID: r.feedbackID}); err != nil {
		return err
	}
	r.converted[id] = converted
	r.originals[id] = string(encoded(item))
	return nil
}

func (r *Request) Response(ctx context.Context, raw []byte) ([]byte, error) {
	root, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	r.responseID = stringValue(root["id"])
	root, err = r.resolveResponse(ctx, root)
	if err != nil {
		return nil, err
	}
	if status := stringValue(root["status"]); status == "failed" || status == "incomplete" {
		r.Failed = true
	}
	if id := stringValue(root["id"]); id != "" && r.store != nil {
		if err := putState(ctx, r.store, stateKey(r.scope, "response", id), r.Turn); err != nil {
			return nil, err
		}
	}
	return json.Marshal(root)
}

// FeedbackFailure is a terminal continuation failure, not a retryable account
// or model-tool error. The native first response may already have emitted text.
type FeedbackFailure struct{ Message string }

func (e *FeedbackFailure) Error() string { return e.Message }

func (r *Request) FailureSnapshot(fallback json.RawMessage) json.RawMessage {
	if len(r.failureSnapshot) > 0 {
		return r.failureSnapshot
	}
	return fallback
}
