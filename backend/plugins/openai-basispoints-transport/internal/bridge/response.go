package bridge

import (
	"context"
	"encoding/json"
	"errors"
)

// convertCall receives a COMPLETE output item. In particular, summary,
// references, status, id and unknown future fields must survive in Original.
func (r *Request) convertCall(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	item, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	if stringValue(item["type"]) == "custom_tool_call" {
		return nil, errors.New("上游返回非桥接 custom 工具")
	}
	if stringValue(item["type"]) != "function_call" {
		return raw, nil
	}
	if stringValue(item["name"]) != "run_officejs" {
		return nil, errors.New("上游返回非桥接工具；未执行任何 Office 工具")
	}
	if r.catalog.choice == "none" {
		return nil, errors.New("上游违反 tool_choice=none")
	}
	id, callID := stringValue(item["id"]), stringValue(item["call_id"])
	if id == "" || callID == "" {
		return nil, errors.New("上游工具 item 缺少 id 或 call_id，无法完整回放")
	}
	canonicalOriginal := string(encoded(item))
	if r.originals[id] == canonicalOriginal {
		return r.converted[id], nil
	}
	if len(r.converted) >= 128 && r.converted[id] == nil {
		return nil, errors.New("单次响应的工具数量超过 128")
	}
	args := item["arguments"]
	if s := stringValue(args); s != "" {
		args = []byte(s)
	}
	outer, err := parseObject(args)
	if err != nil {
		return nil, errors.New("run_officejs arguments 不是有效 JSON")
	}
	code := stringValue(outer["code"])
	envelope, err := parseObject([]byte(code))
	if err != nil || len(envelope) != 2 {
		return nil, errors.New("run_officejs.code 必须是 tool/args JSON 信封；不执行代码")
	}
	name := stringValue(envelope["tool"])
	t, ok := r.catalog.tools[name]
	if !ok {
		return nil, errors.New("上游请求了客户端未声明的工具")
	}
	if r.catalog.forced != "" && r.catalog.forced != name {
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
			return nil, errors.New("custom 工具的 args 必须是字符串")
		}
		out["type"] = encoded("custom_tool_call")
		out["input"] = encoded(input)
	} else {
		if _, err := parseObject(envelope["args"]); err != nil {
			return nil, errors.New("function 工具的 args 必须是对象")
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
