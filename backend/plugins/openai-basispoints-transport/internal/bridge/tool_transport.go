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
	name := qualifiedCallName(item)
	if stringValue(item["type"]) != "function_call" || !isTransportName(name) {
		return nil, toolIdentityError(item, len(c.tools))
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
		return nil, toolValidationError("upstream_tool_references", "invalid_references", "上游工具 references 必须是只含一个完整客户端工具名的字符串数组")
	}
	key := stringValue(refs[0])
	t, ok := c.tools[key]
	if !ok || isTransportName(key) {
		return nil, c.referenceError(item, key)
	}
	if !isTextValue(outer["code"]) {
		return nil, toolValidationError("upstream_tool_code", "code_not_string", "上游工具 code 必须是字符串")
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
	failure := &ToolCallError{stage: "upstream_tool_" + field, reason: "not_object", field: field}
	var obj object
	if err := json.Unmarshal(raw, &obj); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			failure.message = fmt.Sprintf("上游工具 %s 不是有效 JSON 对象（字节偏移 %d）", field, syntax.Offset)
			failure.reason, failure.jsonOffset = "invalid_json", syntax.Offset
			return nil, failure
		}
		failure.message = "上游工具 " + field + " 必须是 JSON 对象"
		return nil, failure
	}
	if obj == nil {
		failure.message = "上游工具 " + field + " 必须是 JSON 对象，不能为 null"
		return nil, failure
	}
	return obj, nil
}
