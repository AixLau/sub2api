package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
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
	// Decode the client contract as JSON, not as YAML. In particular JSON
	// UTF-16 surrogate pairs are valid strings but invalid YAML escapes.
	// UseNumber retains arbitrary-precision numeric literals in history.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return "", errors.New("历史 function 参数无法转换为传输映射")
	}
	document := functionHistoryNode(value, false)
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

// Construct YAML's syntax tree from decoded JSON types. The library remains
// responsible for escaping and emission; numbers never pass through float64.
func functionHistoryNode(value any, key bool) *yaml.Node {
	switch v := value.(type) {
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			node.Content = append(node.Content, functionHistoryNode(k, true), functionHistoryNode(v[k], false))
		}
		return node
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, child := range v {
			node.Content = append(node.Content, functionHistoryNode(child, false))
		}
		return node
	case string:
		style := yaml.SingleQuotedStyle
		if !key && v != "" {
			style = yaml.LiteralStyle
		}
		// YAML literal line breaks normalize these characters. Quoting lets
		// the emitter preserve their exact JSON string values instead.
		if strings.ContainsAny(v, "\r\u0085\u2028\u2029") {
			style = yaml.DoubleQuotedStyle
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Style: style, Value: v}
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: string(v)}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(v)}
	default: // json.Decoder's remaining value is null.
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	}
}
