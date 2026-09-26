package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Native Office metadata and the client call are separate protocols. Only
// code carries our JSON envelope; references and summary never select tools.
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
	if !isTextValue(outer["code"]) {
		return nil, toolValidationError("upstream_tool_code", "code_not_string", "传输 code 必须是工具信封的 JSON 字符串")
	}
	envelope, err := parseToolObject([]byte(stringValue(outer["code"])), "code")
	if err != nil {
		return nil, err
	}
	if !isTextValue(envelope["name"]) || (envelope["namespace"] != nil && !isTextValue(envelope["namespace"])) {
		return nil, toolValidationError("upstream_tool_envelope", "invalid_name", "工具信封 name 和 namespace 必须是字符串")
	}
	name = qualifiedCallName(envelope)
	t, key, ok := c.lookup(name)
	if !ok || isTransportName(name) {
		return nil, c.targetError(item, name)
	}
	for field := range envelope {
		if field != "name" && field != "namespace" && field != "arguments" && field != "input" {
			return nil, toolValidationError("upstream_tool_envelope", "unknown_field", "工具信封只允许 name、namespace 以及 arguments 或 input 字段")
		}
	}
	payload := envelope["arguments"]
	if t.Custom {
		payload = envelope["input"]
		if envelope["arguments"] != nil || !isTextValue(payload) {
			return nil, toolValidationError("upstream_tool_envelope", "custom_input_not_string", "custom 工具信封必须使用原始字符串 input，不能使用 arguments")
		}
	} else {
		if envelope["input"] != nil {
			return nil, toolValidationError("upstream_tool_envelope", "unexpected_input", "function 工具信封必须使用 arguments 对象，不能使用 input")
		}
		if _, err := parseToolObject(payload, "envelope_arguments"); err != nil {
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
