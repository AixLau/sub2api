package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Routing is independent of the payload and human-readable summary. The
// catalog alone determines whether code is JSON arguments or custom raw text.
// Only the declared native executor can carry this protocol.
func (c catalog) transportPayload(item object) (object, error) {
	name := stringValue(item["name"])
	if ns := stringValue(item["namespace"]); ns != "" {
		name = ns + "." + name
	}
	if stringValue(item["type"]) != "function_call" || !isTransportName(name) {
		return nil, toolCallError("上游调用不是客户端已声明的工具或 run_officejs 传输执行器")
	}
	args := item["arguments"]
	if isTextValue(args) {
		args = []byte(stringValue(args))
	}
	outer, err := parseToolObject(args, "arguments")
	if err != nil {
		return nil, err
	}
	var refs []json.RawMessage
	if json.Unmarshal(outer["references"], &refs) != nil || len(refs) != 1 || !isTextValue(refs[0]) {
		return nil, toolCallError("上游工具 references 必须是只含一个完整客户端工具名的字符串数组")
	}
	key := stringValue(refs[0])
	t, ok := c.tools[key]
	if !ok || isTransportName(key) {
		return nil, toolCallError("上游工具 references 指定了客户端未声明的工具或传输执行器")
	}
	if !isTextValue(outer["code"]) {
		return nil, toolCallError("上游工具 code 必须是字符串")
	}
	payload := outer["code"]
	if !t.Custom {
		payload = []byte(stringValue(payload))
		if _, err := parseToolObject(payload, "code"); err != nil {
			return nil, err
		}
	}
	return object{"tool": encoded(key), "args": payload}, nil
}

// Report only a fixed field name and byte offset, never parser error text,
// which may include a private argument, source fragment or credential.
func parseToolObject(raw []byte, field string) (object, error) {
	var obj object
	if err := json.Unmarshal(raw, &obj); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, toolCallError(fmt.Sprintf("上游工具 %s 不是有效 JSON 对象（字节偏移 %d）", field, syntax.Offset))
		}
		return nil, toolCallError("上游工具 " + field + " 必须是 JSON 对象")
	}
	if obj == nil {
		return nil, toolCallError("上游工具 " + field + " 必须是 JSON 对象，不能为 null")
	}
	return obj, nil
}
