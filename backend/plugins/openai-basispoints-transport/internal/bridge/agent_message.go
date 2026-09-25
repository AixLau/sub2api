package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Only explicitly plaintext agent messages can be adapted. Function calls
// emitted by this bridge declare their encryption metadata so Codex sends new
// tasks as input_text. Old encrypted_content cannot be classified by appearance.
func normalizeAgentMessage(item object, index int) (json.RawMessage, error) {
	path := fmt.Sprintf("input[%d]", index)
	if hasCiphertext(item["encrypted_content"]) {
		return nil, agentMessageError(path, "unexpected_encrypted_field")
	}
	raw := bytes.TrimSpace(item["content"])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		var parts []json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return nil, agentMessageError(path+".content", "invalid_content")
		}
		var joined strings.Builder
		for i, rawPart := range parts {
			partPath := fmt.Sprintf("%s.content[%d]", path, i)
			part, err := parseObject(rawPart)
			if err != nil {
				return nil, agentMessageError(partPath, "invalid_part")
			}
			var value string
			switch stringValue(part["type"]) {
			case "input_text", "text":
				if hasCiphertext(part["encrypted_content"]) || !isTextValue(part["text"]) {
					return nil, agentMessageError(partPath, "invalid_text_part")
				}
				value = stringValue(part["text"])
			case "encrypted_content":
				return nil, agentMessageError(partPath, "encrypted_payload")
			default:
				return nil, agentMessageError(partPath, "unsupported_part")
			}
			joined.WriteString(value)
		}
		text = joined.String()
	}
	if text == "" {
		return nil, nil
	}
	return encoded(messageItem("user", text)), nil
}

func agentMessageError(path, reason string) error {
	// Never include sender-controlled types, IDs, task text or ciphertext.
	return fmt.Errorf("无法安全转换 agent_message；请通过 input_text 提供明文任务 (path=%s, reason=%s)", path, reason)
}
