package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
)

// The repair round is buffered until it has a complete, validated snapshot.
// Servers may return SSE even for stream=false. Stop at the terminal event,
// not EOF, and apply a total byte limit as well as the request deadline.
func readFeedbackResponse(ctx context.Context, response *http.Response) ([]byte, error) {
	if response.StatusCode >= 400 && !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(io.LimitReader(response.Body, maxErrorProbeBytes+1))
		if err == nil && len(raw) <= maxErrorProbeBytes {
			if snapshot, decodeErr := feedbackErrorSnapshot(raw, response); decodeErr == nil {
				return snapshot, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Preserve the known HTTP failure even when a proxy returns HTML or
		// an unreadable body; do not expose arbitrary proxy response contents.
		fallback, _ := json.Marshal(map[string]any{"error": map[string]any{
			"code": "upstream_error", "message": fmt.Sprintf("工具错误反馈被上游拒绝（HTTP %d）", response.StatusCode),
		}})
		return feedbackErrorSnapshot(fallback, response)
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(io.LimitReader(response.Body, bridge.MaxResponseBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > bridge.MaxResponseBytes {
			return nil, errors.New("工具反馈响应超过大小限制")
		}
		return validateFeedbackSnapshot(raw, "", response)
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, bridge.MaxResponseBytes+1))
	scanner.Buffer(make([]byte, 32<<10), bridge.MaxResponseBytes)
	var data []string
	size := 0
	flush := func() ([]byte, error) {
		if len(data) == 0 {
			return nil, nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		var event struct {
			Type     string
			Response json.RawMessage
		}
		if json.Unmarshal([]byte(raw), &event) != nil {
			return nil, errors.New("工具反馈 SSE data 无效")
		}
		switch event.Type {
		case "response.completed", "response.failed", "response.incomplete":
			if len(event.Response) == 0 {
				return nil, errors.New("工具反馈缺少响应快照")
			}
			return validateFeedbackSnapshot(event.Response, strings.TrimPrefix(event.Type, "response."), response)
		case "error":
			return feedbackErrorSnapshot([]byte(raw), response)
		}
		return nil, nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		size += len(line) + 1
		if size > bridge.MaxResponseBytes {
			return nil, errors.New("工具反馈响应超过大小限制")
		}
		if line == "" {
			if raw, err := flush(); raw != nil || err != nil {
				return raw, err
			}
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if raw, err := flush(); raw != nil || err != nil {
		return raw, err
	}
	return nil, errors.New("工具反馈 SSE 缺少终结事件")
}

// Turn a provider error into a terminal Responses snapshot. The downstream
// headers may already be committed, so retain retry/rate-limit metadata inside
// the error instead of losing it when the continuation's HTTP body is closed.
func feedbackErrorSnapshot(raw []byte, response *http.Response) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || envelope == nil {
		return nil, errors.New("工具反馈错误不是 JSON 对象")
	}
	var detail map[string]json.RawMessage
	if nested := envelope["error"]; len(nested) > 0 {
		_ = json.Unmarshal(nested, &detail)
	} else {
		// Flat SSE errors use type=error for the event, not the error category.
		detail = make(map[string]json.RawMessage)
		for _, field := range []string{"code", "message", "param", "status_code", "headers"} {
			if value, ok := envelope[field]; ok {
				detail[field] = value
			}
		}
	}
	var code, message string
	_ = json.Unmarshal(detail["code"], &code)
	_ = json.Unmarshal(detail["message"], &message)
	if code == "" && message == "" {
		return nil, errors.New("工具反馈 error 事件缺少结构化错误")
	}
	addFeedbackErrorMetadata(detail, response)
	return json.Marshal(map[string]any{"object": "response", "status": "failed", "output": []any{}, "error": detail})
}

func addFeedbackErrorMetadata(detail map[string]json.RawMessage, response *http.Response) {
	if response.StatusCode >= 400 && len(detail["status_code"]) == 0 {
		detail["status_code"], _ = json.Marshal(response.StatusCode)
	}
	headers := map[string]json.RawMessage{}
	_ = json.Unmarshal(detail["headers"], &headers)
	if headers == nil {
		headers = map[string]json.RawMessage{}
	}
	for key := range response.Header {
		name := strings.ToLower(key)
		if name == "retry-after" || name == "retry-after-ms" || name == "x-request-id" || strings.HasPrefix(name, "x-ratelimit-") {
			if _, exists := headers[name]; !exists {
				headers[name], _ = json.Marshal(response.Header.Get(key))
			}
		}
	}
	if len(headers) > 0 {
		detail["headers"], _ = json.Marshal(headers)
	}
}

// A repair must be a terminal Responses snapshot before any calls can escape.
func validateFeedbackSnapshot(raw []byte, eventStatus string, upstream *http.Response) ([]byte, error) {
	var response map[string]json.RawMessage
	if json.Unmarshal(raw, &response) != nil || response == nil {
		return nil, errors.New("工具反馈响应不是 JSON 对象")
	}
	var status string
	_ = json.Unmarshal(response["status"], &status)
	if eventStatus != "" {
		if status != "" && status != eventStatus {
			return nil, errors.New("工具反馈终结事件与状态不一致")
		}
		status = eventStatus
		response["status"], _ = json.Marshal(status)
	}
	if status != "completed" && status != "failed" && status != "incomplete" {
		return nil, errors.New("工具反馈缺少终结状态")
	}
	var output []json.RawMessage
	if status == "completed" && len(response["output"]) == 0 {
		return nil, errors.New("工具反馈缺少 output")
	}
	if outputRaw := response["output"]; len(outputRaw) > 0 && (string(outputRaw) == "null" || json.Unmarshal(outputRaw, &output) != nil) {
		return nil, errors.New("工具反馈 output 必须是数组")
	}
	if eventStatus == "" && status == "completed" {
		return raw, nil
	}
	if status != "completed" {
		var detail map[string]json.RawMessage
		if json.Unmarshal(response["error"], &detail) == nil && detail != nil {
			addFeedbackErrorMetadata(detail, upstream)
			response["error"], _ = json.Marshal(detail)
		}
	}
	return json.Marshal(response)
}
