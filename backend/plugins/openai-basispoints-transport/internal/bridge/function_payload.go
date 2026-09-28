package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Function arguments cross a string-valued carrier. YAML literal/single-quoted
// strings preserve regexes and paths without a second JSON escaping layer.
// The client still receives a native JSON object. This is parsing, never eval
// or repair: malformed input and non-JSON YAML features are rejected.
func parseFunctionPayload(raw []byte) (json.RawMessage, error) {
	fail := func(reason, message string) error {
		return &ToolCallError{stage: "upstream_tool_code", reason: reason, field: "code", message: message}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document, extra yaml.Node
	if err := decoder.Decode(&document); err != nil {
		// yaml.v3 errors can contain source tokens. Return only its numeric
		// location and bridge-owned guidance, never the raw parser message.
		location := ""
		var line int
		if _, scanErr := fmt.Sscanf(err.Error(), "yaml: line %d:", &line); scanErr == nil && line > 0 {
			location = fmt.Sprintf("（解析器报告第 %d 行）", line)
		}
		return nil, fail("invalid_yaml", "函数 code 必须是有效 YAML 参数映射"+location+"；字符串使用显式缩进 |2-，每行在字段缩进基础上添加两个空格，再保留原文全部缩进；末尾一个换行用 |2，多个换行用 |2+。不要从代码首行缩进推断 YAML 缩进")
	}
	if decoder.Decode(&extra) != io.EOF {
		return nil, fail("multiple_documents", "函数 code 只能包含一个 YAML 参数映射")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fail("not_object", "函数 code 必须是参数映射，不能为数组、字符串或 null")
	}
	var convert func(*yaml.Node) (json.RawMessage, error)
	convert = func(node *yaml.Node) (json.RawMessage, error) {
		if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Style&yaml.TaggedStyle != 0 {
			return nil, fail("unsupported_yaml_feature", "函数参数不支持 YAML 标签、锚点或别名")
		}
		switch node.Kind {
		case yaml.MappingNode:
			out := object{}
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Anchor != "" || key.Style&yaml.TaggedStyle != 0 {
					return nil, fail("invalid_object_key", "函数参数映射的键必须是普通字符串")
				}
				if _, exists := out[key.Value]; exists {
					return nil, fail("duplicate_key", "函数参数映射包含重复键")
				}
				value, err := convert(node.Content[i+1])
				if err != nil {
					return nil, err
				}
				out[key.Value] = value
			}
			return encoded(out), nil
		case yaml.SequenceNode:
			out := make([]json.RawMessage, len(node.Content))
			for i, child := range node.Content {
				var err error
				out[i], err = convert(child)
				if err != nil {
					return nil, err
				}
			}
			return encoded(out), nil
		case yaml.ScalarNode:
			// YAML's implicit resolver is limited to machine-sized numbers.
			// Recognize plain JSON literals first so large exponents/integers
			// never silently become strings; quoted numeric strings stay text.
			if node.Style == 0 && jsonKind([]byte(node.Value)) == "number" {
				return json.RawMessage(node.Value), nil
			}
			switch node.Tag {
			case "!!str":
				return encoded(node.Value), nil
			case "!!bool":
				return json.RawMessage(strings.ToLower(node.Value)), nil
			case "!!null":
				return json.RawMessage("null"), nil
			case "!!int", "!!float":
				// Keep arbitrary-precision JSON numeric literals byte-exact.
				if json.Valid([]byte(node.Value)) && jsonKind([]byte(node.Value)) == "number" {
					return json.RawMessage(node.Value), nil
				}
			}
		}
		return nil, fail("non_json_value", "函数参数只支持 JSON 数据类型；字符串请加单引号，数字请使用十进制 JSON 格式")
	}
	return convert(document.Content[0])
}

// Native client history is JSON. Render it in the same literal-string format
// taught for new calls, without routing wrappers or floating-point decoding.
func formatFunctionPayload(raw []byte) (string, error) {
	if !json.Valid(raw) || jsonKind(raw) != "object" {
		return "", errors.New("历史 function 工具 arguments 必须是 JSON 对象")
	}
	// Decode JSON strings before constructing YAML nodes: YAML rejects JSON's
	// UTF-16 surrogate-pair escapes. Tokens retain member order, while
	// UseNumber preserves numeric literals beyond floating-point precision.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var convert func(bool) (*yaml.Node, error)
	convert = func(key bool) (*yaml.Node, error) {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		node := &yaml.Node{Kind: yaml.ScalarNode}
		switch value := token.(type) {
		case json.Delim:
			node.Kind = yaml.SequenceNode
			if value == '{' {
				node.Kind = yaml.MappingNode
			}
			for decoder.More() {
				child, err := convert(node.Kind == yaml.MappingNode && len(node.Content)%2 == 0)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, child)
			}
			_, err = decoder.Token()
			return node, err
		case string:
			node.Tag, node.Value = "!!str", value
			node.Style = yaml.SingleQuotedStyle
			if !key && value != "" {
				node.Style = yaml.LiteralStyle
			}
			// YAML literal line breaks normalize these characters; let the
			// existing emitter escape them to preserve the JSON string.
			if strings.ContainsAny(value, "\r\u0085\u2028\u2029") {
				node.Style = yaml.DoubleQuotedStyle
			}
		case json.Number:
			node.Value = value.String()
		case bool:
			node.Tag, node.Value = "!!bool", strconv.FormatBool(value)
		case nil:
			node.Tag, node.Value = "!!null", "null"
		}
		return node, nil
	}
	document, err := convert(false)
	if err != nil {
		return "", errors.New("历史 function 参数无法转换为传输映射")
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return "", errors.New("历史 function 参数无法转换为传输映射")
	}
	if err := encoder.Close(); err != nil {
		return "", errors.New("历史 function 参数无法转换为传输映射")
	}
	return out.String(), nil
}
