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

// clientEnvelope decodes the transport payload out of a native call. The model
// sometimes stuffs the client-tool envelope into another server executor (e.g.
// run_connector_action), and drifts on the envelope spelling: the canonical
// shape is {"tool":…,"args":…} but {"name":…,"arguments":…} and extra keys are
// accepted the same way, so a format drift degrades into a normal client call
// instead of an "unknown tool" failure.
func clientEnvelope(item object) (object, error) {
	args := item["arguments"]
	if s := stringValue(args); s != "" {
		args = []byte(s)
	}
	outer, err := parseObject(args)
	if err != nil {
		return nil, toolCallError("上游工具 arguments 不是有效 JSON 对象")
	}
	if envelope, marked, err := customTransportEnvelope(outer); marked {
		return envelope, err
	}
	code := stringValue(outer["code"])
	envelope, err := parseObject([]byte(code))
	if err != nil {
		return nil, toolCallError("上游工具 code 不是有效 JSON 对象信封；请使用 JSON 序列化生成完整信封")
	}
	tool := stringValue(envelope["tool"])
	if tool == "" {
		tool = stringValue(envelope["name"])
	}
	if tool == "" {
		return nil, toolCallError("上游工具信封缺少 tool 名称")
	}
	callArgs, ok := envelope["args"]
	if !ok {
		callArgs, ok = envelope["arguments"]
	}
	if !ok {
		return nil, toolCallError("上游工具信封缺少 args")
	}
	normalized := object{"tool": encoded(tool), "args": callArgs}
	return normalized, nil
}

// ToolCallError contains only fixed diagnostic text, never executable input.
type ToolCallError struct{ message string }

func (e *ToolCallError) Error() string   { return e.message }
func toolCallError(message string) error { return &ToolCallError{message: message} }

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
	item, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	typ := stringValue(item["type"])
	if typ != "function_call" && typ != "custom_tool_call" {
		return raw, nil
	}
	callID := stringValue(item["call_id"])
	if callID == "" {
		return nil, toolCallError("上游工具 item 缺少 call_id")
	}
	name := stringValue(item["name"])
	if ns := stringValue(item["namespace"]); ns != "" {
		name = ns + "." + name
	}
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
		envelope, err = clientEnvelope(item)
		if err != nil {
			return nil, err
		}
	}
	if r.catalog.choice == "none" {
		return nil, toolCallError("上游违反 tool_choice=none")
	}
	id := stringValue(item["id"])
	if id == "" {
		return nil, toolCallError("上游工具 item 缺少 id 或 call_id，无法完整回放")
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
		return nil, toolCallError("上游未遵循指定工具的 tool_choice")
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
			return nil, toolCallError("上游 custom 工具的 args 必须是 JSON 字符串")
		}
		out["type"] = encoded("custom_tool_call")
		out["input"] = encoded(input)
	} else {
		if _, err := parseObject(envelope["args"]); err != nil {
			return nil, toolCallError("上游 function 工具的 args 必须是 JSON 对象")
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
	if err := putState(ctx, r.store, stateKey(r.scope, "call", alias), callRecord{Original: raw, Client: converted, Turn: r.Turn}); err != nil {
		return nil, err
	}
	r.converted[id] = converted
	r.originals[id] = canonicalOriginal
	return converted, nil
}

func (r *Request) Response(ctx context.Context, raw []byte) ([]byte, error) {
	root, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	if status := stringValue(root["status"]); status == "failed" || status == "incomplete" {
		r.Failed = true
	}
	var output []json.RawMessage
	if len(root["output"]) > 0 {
		if json.Unmarshal(root["output"], &output) != nil {
			return nil, errors.New("上游 output 必须是数组")
		}
		for i, item := range output {
			output[i], err = r.convertCall(ctx, item)
			if err != nil {
				return nil, err
			}
		}
		root["output"] = encoded(output)
	}
	if stringValue(root["status"]) == "completed" && r.catalog.choice == "required" && len(r.converted) == 0 {
		return nil, errors.New("上游未返回 required 工具调用")
	}
	if id := stringValue(root["id"]); id != "" && r.store != nil {
		if err := putState(ctx, r.store, stateKey(r.scope, "response", id), r.Turn); err != nil {
			return nil, err
		}
	}
	return json.Marshal(root)
}
