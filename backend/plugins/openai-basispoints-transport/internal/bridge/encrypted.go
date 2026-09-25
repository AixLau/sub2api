package bridge

import (
	"encoding/json"
	"errors"
)

// ErrUnsafeEncryptedReplay prevents discarding the only copy of tool results.
var ErrUnsafeEncryptedReplay = errors.New("无法安全重试：加密内容是工具结果或压缩历史的唯一副本；请重新提供结果或开始新会话")

// StripEncryptedContent removes optional protocol ciphertext after Prepare
// restores KV history. Tool arguments and JSON inside text are never traversed.
func StripEncryptedContent(body []byte) ([]byte, error) {
	root, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if json.Unmarshal(root["input"], &input) != nil {
		return body, nil
	}
	changed := false
	kept := make([]json.RawMessage, 0, len(input))
	for _, raw := range input {
		item, err := parseObject(raw)
		if err != nil {
			return nil, err
		}
		typ := stringValue(item["type"])
		_, encrypted := item["encrypted_content"]
		if typ == "reasoning" && encrypted {
			changed = true
			continue
		}
		if (typ == "compaction" || typ == "compaction_summary") && encrypted {
			return nil, ErrUnsafeEncryptedReplay
		}
		field := ""
		switch typ {
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		case "message", "":
			field = "content"
		}
		itemChanged := false
		if field != "" {
			value, didChange, err := stripContentParts(item[field])
			if err != nil {
				return nil, err
			}
			if didChange {
				item[field] = value
				itemChanged = true
			}
		}
		if encrypted {
			readable := false
			switch typ {
			case "function_call":
				args := item["arguments"]
				if s := stringValue(args); s != "" {
					args = []byte(s)
				}
				_, err := parseObject(args)
				readable = err == nil
			case "custom_tool_call":
				readable = stringValue(item["input"]) != ""
			default:
				readable = field != "" && readableContent(item[field])
			}
			if !readable {
				return nil, ErrUnsafeEncryptedReplay
			}
			delete(item, "encrypted_content")
			itemChanged = true
		}
		if itemChanged {
			raw = encoded(item)
			changed = true
		}
		kept = append(kept, raw)
	}
	if !changed {
		return body, nil
	}
	root["input"] = encoded(kept)
	return json.Marshal(root)
}

func stripContentParts(raw json.RawMessage) (json.RawMessage, bool, error) {
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return raw, false, nil
	}
	changed := false
	for i, rawPart := range parts {
		part, err := parseObject(rawPart)
		if err != nil {
			continue
		}
		if _, encrypted := part["encrypted_content"]; !encrypted {
			continue
		}
		if !readableTextPart(part) {
			return nil, false, ErrUnsafeEncryptedReplay
		}
		delete(part, "encrypted_content")
		parts[i] = encoded(part)
		changed = true
	}
	if !changed {
		return raw, false, nil
	}
	return encoded(parts), true, nil
}

func readableTextPart(part object) bool {
	switch stringValue(part["type"]) {
	case "input_text", "output_text", "text":
		return stringValue(part["text"]) != ""
	default:
		return false
	}
}

func readableContent(raw json.RawMessage) bool {
	if text := stringValue(raw); text != "" {
		return true
	}
	var parts []object
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !readableTextPart(part) {
			return false
		}
	}
	return true
}
