package bridge

import (
	"context"
	"encoding/json"
	"errors"
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
func clientEnvelope(item object) (object, bool) {
	args := item["arguments"]
	if s := stringValue(args); s != "" {
		args = []byte(s)
	}
	outer, err := parseObject(args)
	if err != nil {
		return nil, false
	}
	code := stringValue(outer["code"])
	envelope, err := parseObject([]byte(code))
	if err != nil {
		return nil, false
	}
	tool := stringValue(envelope["tool"])
	if tool == "" {
		tool = stringValue(envelope["name"])
	}
	if tool == "" {
		return nil, false
	}
	callArgs, ok := envelope["args"]
	if !ok {
		callArgs, ok = envelope["arguments"]
	}
	if !ok {
		return nil, false
	}
	normalized := object{"tool": encoded(tool), "args": callArgs}
	return normalized, true
}

// nativeCallWithTextArguments copies a native call item and rewrites object-form
// arguments into the JSON string clients expect.
func nativeCallWithTextArguments(item object) object {
	copy := object{}
	for key, value := range item {
		copy[key] = value
	}
	rawArgs := item["arguments"]
	if len(rawArgs) > 0 && rawArgs[0] != '"' {
		copy["arguments"] = encoded(string(rawArgs))
	}
	return copy
}

// convertCall receives a COMPLETE output item. In particular, summary,
// references, status, id and unknown future fields must survive in Original.
// Calls that do not carry a client-tool envelope are never executed here and
// never fail the stream: they are handed to the client unchanged so the client
// can answer them and the turn can continue.
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
		return nil, errors.New("上游工具 item 缺少 call_id")
	}
	envelope, isClientCall := clientEnvelope(item)
	if !isClientCall {
		return encoded(nativeCallWithTextArguments(item)), nil
	}
	if r.catalog.choice == "none" {
		return nil, errors.New("上游违反 tool_choice=none")
	}
	id := stringValue(item["id"])
	if id == "" {
		return nil, errors.New("上游工具 item 缺少 id 或 call_id，无法完整回放")
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
		// The envelope names a tool the client never declared. Do not fail the
		// stream over it; the client sees the raw call and can answer it.
		return encoded(nativeCallWithTextArguments(item)), nil
	}
	if r.catalog.forced != "" && r.catalog.forced != toolKey && r.catalog.forced != tool {
		return nil, errors.New("上游未遵循指定工具的 tool_choice")
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
		if len(envelope["args"]) == 0 || envelope["args"][0] != '"' || json.Unmarshal(envelope["args"], &input) != nil {
			// Not our envelope after all; let the client judge it.
			return encoded(nativeCallWithTextArguments(item)), nil
		}
		out["type"] = encoded("custom_tool_call")
		out["input"] = encoded(input)
	} else {
		if _, err := parseObject(envelope["args"]); err != nil {
			return encoded(nativeCallWithTextArguments(item)), nil
		}
		out["arguments"] = encoded(string(envelope["args"]))
	}
	converted := encoded(out)
	if previous := r.converted[id]; previous != nil {
		// The final response may add metadata but cannot change an already
		// delivered function's identity or arguments.
		a, _ := parseObject(previous)
		for _, key := range []string{"type", "call_id", "name", "namespace", "arguments", "input"} {
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
