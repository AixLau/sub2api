package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrCompactionResponseInvalid distinguishes an invalid successful response
// from an interrupted stream or a transport timeout. Upstream failure payloads
// are forwarded verbatim instead of being replaced with this plugin error.
var ErrCompactionResponseInvalid = errors.New("远程压缩响应无效")

// ReadRemoteCompactionResponse buffers a bounded response for native forwarding.
// SSE ends at its first terminal event, not HTTP EOF. Success requires exactly
// one valid compaction from output_item.done followed by response.completed; the
// completed snapshot cannot supply missing events. Failed/incomplete responses
// are returned unchanged so the client retains their original error semantics.
// The caller owns cancellation, idle deadlines and closing src.
func ReadRemoteCompactionResponse(src io.Reader, contentType string) ([]byte, error) {
	src = io.LimitReader(src, MaxResponseBytes+1)
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return readCompactionSSE(src)
	}
	body, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxResponseBytes {
		return nil, fmt.Errorf("%w: 超过大小限制", ErrCompactionResponseInvalid)
	}
	root, err := parseObject(body)
	if err != nil {
		return nil, fmt.Errorf("%w: 不是 JSON 对象", ErrCompactionResponseInvalid)
	}
	if status := stringValue(root["status"]); status == "failed" || status == "incomplete" ||
		stringValue(root["type"]) == "error" || stringValue(root["type"]) == "response.failed" ||
		stringValue(root["type"]) == "response.incomplete" || (len(root["error"]) > 0 && !bytes.Equal(root["error"], []byte("null"))) {
		return body, nil
	}
	if stringValue(root["status"]) != "completed" {
		return nil, fmt.Errorf("%w: 缺少 completed 状态", ErrCompactionResponseInvalid)
	}
	var output []json.RawMessage
	if json.Unmarshal(root["output"], &output) != nil {
		return nil, fmt.Errorf("%w: 缺少有效 output", ErrCompactionResponseInvalid)
	}
	count := 0
	for _, item := range output {
		if isValidCompactionItem(item) {
			count++
		}
	}
	if err := validateCompactionCount(count); err != nil {
		return nil, err
	}
	return body, nil
}

func readCompactionSSE(src io.Reader) ([]byte, error) {
	// Match the native probe's bounded ReadSlice pattern while retaining the
	// exact bytes (including comments, CRLF and multiline data) for forwarding.
	reader := bufio.NewReader(src)
	var body, data []byte
	lineStart, compactions := 0, 0
	for {
		part, err := reader.ReadSlice('\n')
		if len(body)+len(part) > MaxResponseBytes {
			return nil, fmt.Errorf("%w: 超过大小限制", ErrCompactionResponseInvalid)
		}
		body = append(body, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) {
			// An unterminated SSE frame is not dispatched by eventsource-stream.
			return nil, fmt.Errorf("上游 SSE 在远程压缩终结事件前断开: %w", io.ErrUnexpectedEOF)
		}
		if err != nil {
			return nil, err
		}
		line := bytes.TrimSuffix(bytes.TrimSuffix(body[lineStart:], []byte{'\n'}), []byte{'\r'})
		lineStart = len(body)
		if bytes.HasPrefix(line, []byte("data:")) {
			data = append(data, bytes.TrimPrefix(line[5:], []byte{' '})...)
			data = append(data, '\n')
		}
		if len(line) != 0 || len(data) == 0 {
			continue
		}
		root, parseErr := parseObject(data[:len(data)-1])
		data = data[:0]
		if parseErr != nil {
			// Codex ignores unparseable/unknown events. In particular [DONE]
			// cannot stand in for a completed Responses event.
			continue
		}
		switch stringValue(root["type"]) {
		case "response.output_item.done":
			if isValidCompactionItem(root["item"]) {
				compactions++
			}
		case "response.failed", "response.incomplete", "error":
			// Classify terminal state before validating success. Do not mask
			// context/quota failures or interrupted responses as invalid output.
			return body, nil
		case "response.completed":
			var response struct {
				ID *string `json:"id"`
			}
			if json.Unmarshal(root["response"], &response) != nil || response.ID == nil {
				return nil, fmt.Errorf("%w: response.completed 缺少有效 response.id", ErrCompactionResponseInvalid)
			}
			if err := validateCompactionCount(compactions); err != nil {
				return nil, err
			}
			return body, nil
		}
	}
}

func isValidCompactionItem(raw json.RawMessage) bool {
	var item struct {
		Type             string  `json:"type"`
		ID               *string `json:"id"`
		EncryptedContent *string `json:"encrypted_content"`
	}
	// encrypted_content is required by Codex's ResponseItem::Compaction; a
	// type label alone is not a deserializable compaction item.
	return json.Unmarshal(raw, &item) == nil &&
		(item.Type == "compaction" || item.Type == "compaction_summary") &&
		item.EncryptedContent != nil
}

func validateCompactionCount(count int) error {
	if count != 1 {
		return fmt.Errorf("%w: 必须包含恰好一个有效 compaction 输出项，实际为 %d 个", ErrCompactionResponseInvalid, count)
	}
	return nil
}
