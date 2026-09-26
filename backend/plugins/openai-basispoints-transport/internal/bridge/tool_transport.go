package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const clientToolReferencePrefix = "client-tool:"

// Route and payload occupy separate native fields. The outer function arguments
// are decoded once; custom code is never parsed or re-escaped as nested JSON.
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
		return nil, toolValidationError("upstream_tool_code", "code_not_string", "传输 code 必须是客户端工具的原始字符串载荷")
	}
	t, key, err := c.transportTarget(item, outer)
	if err != nil {
		return nil, err
	}
	payload := outer["code"]
	if t.Custom {
		return object{"tool": encoded(key), "args": payload}, nil
	}
	payload = []byte(stringValue(payload))
	if _, err := parseToolObject(payload, "code"); err != nil {
		return nil, err
	}
	return object{"tool": encoded(key), "args": payload}, nil
}

func (c catalog) transportTarget(item, outer object) (tool, string, error) {
	var refs []string
	if json.Unmarshal(outer["references"], &refs) != nil || len(refs) != 1 {
		return tool{}, "", toolValidationError("upstream_tool_references", "invalid_route", "references 必须仅包含一个 client-tool: 工具路由")
	}
	name, ok := strings.CutPrefix(refs[0], clientToolReferencePrefix)
	if !ok || name == "" {
		return tool{}, "", toolValidationError("upstream_tool_references", "invalid_route", "references 必须使用 client-tool: 加目录中的完整工具名")
	}
	t, ok := c.tools[name]
	if !ok || isTransportName(name) {
		return tool{}, "", c.targetError(item, name)
	}
	return t, name, nil
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
