package bridge

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrUnsafeEncryptedReplay means a complete independent copy was not verified.
var ErrUnsafeEncryptedReplay = errors.New("无法安全重试：未能确认加密历史具有完整可用副本；已保留原始内容，请重新提供结果或开始新会话")

// EncryptedReplayError contains structural metadata only, never content or IDs.
type EncryptedReplayError struct {
	Path     string `json:"path"`
	ItemType string `json:"item_type"`
	Reason   string `json:"reason"`
}

func (e *EncryptedReplayError) Error() string {
	return fmt.Sprintf("%s (path=%s, type=%s, reason=%s)", ErrUnsafeEncryptedReplay, e.Path, e.ItemType, e.Reason)
}
func (e *EncryptedReplayError) Unwrap() error { return ErrUnsafeEncryptedReplay }

func unsafeReplay(path, typ, reason string) error {
	switch typ {
	case "reasoning", "compaction", "compaction_summary", "message", "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output", "input_text", "output_text", "text", "input_image", "input_file", "refusal":
	default:
		typ = "unknown"
	}
	return &EncryptedReplayError{Path: path, ItemType: typ, Reason: reason}
}

func hasCiphertext(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null" && string(raw) != "\"\""
}

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
	for i, raw := range input {
		path := fmt.Sprintf("input[%d]", i)
		item, err := parseObject(raw)
		if err != nil {
			return nil, err
		}
		typ := stringValue(item["type"])
		_, encrypted := item["encrypted_content"]
		// Null/empty metadata contains no encrypted state to discard.
		itemChanged := encrypted && !hasCiphertext(item["encrypted_content"])
		if itemChanged {
			delete(item, "encrypted_content")
			encrypted = false
		}
		if typ == "reasoning" && encrypted {
			changed = true
			continue
		}
		if (typ == "compaction" || typ == "compaction_summary") && encrypted {
			return nil, unsafeReplay(path, typ, "opaque_compaction")
		}
		field := ""
		switch typ {
		case "function_call_output", "custom_tool_call_output":
			field = "output"
		case "message", "":
			field = "content"
		}
		if field != "" {
			value, didChange, err := stripContentParts(item[field], path+"."+field)
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
				readable = isTextValue(item["input"])
			default:
				readable = field != "" && readableContent(item[field])
			}
			if !readable {
				return nil, unsafeReplay(path, typ, "complete_copy_unverified")
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

func stripContentParts(raw json.RawMessage, path string) (json.RawMessage, bool, error) {
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
		if hasCiphertext(part["encrypted_content"]) && !readableContentPart(part) {
			return nil, false, unsafeReplay(fmt.Sprintf("%s[%d]", path, i), stringValue(part["type"]), "complete_copy_unverified")
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

func isTextValue(raw json.RawMessage) bool {
	var text string
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &text) == nil
}

func readableContentPart(part object) bool {
	switch stringValue(part["type"]) {
	case "input_text", "output_text", "text":
		return isTextValue(part["text"])
	case "refusal":
		return isTextValue(part["refusal"])
	case "input_image":
		// Inline data is independent; file IDs/URLs alone cannot prove access.
		value := stringValue(part["image_url"])
		return strings.HasPrefix(value, "data:image/") && validInlineData(value)
	case "input_file":
		return validInlineData(stringValue(part["file_data"]))
	default:
		return false
	}
}

func validInlineData(value string) bool {
	if strings.HasPrefix(value, "data:") {
		_, payload, found := strings.Cut(value, ";base64,")
		if !found {
			return false
		}
		value = payload
	}
	n, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(value)))
	return err == nil && n > 0
}

func readableContent(raw json.RawMessage) bool {
	if isTextValue(raw) {
		return true
	}
	var parts []object
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !readableContentPart(part) {
			return false
		}
	}
	return true
}
