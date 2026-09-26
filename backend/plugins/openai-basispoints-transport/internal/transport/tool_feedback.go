package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugins/openai-basispoints-transport/internal/bridge"
)

// The repair round is buffered until it has a complete, validated snapshot.
// Servers may return SSE even for stream=false. Stop at the terminal event,
// not EOF, and apply a total byte limit as well as the request deadline.
func readFeedbackResponse(ctx context.Context, response *http.Response) ([]byte, error) {
	if !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		raw, err := io.ReadAll(io.LimitReader(response.Body, bridge.MaxResponseBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > bridge.MaxResponseBytes {
			return nil, errors.New("工具反馈响应超过大小限制")
		}
		return validateFeedbackSnapshot(raw, "")
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
			return validateFeedbackSnapshot(event.Response, strings.TrimPrefix(event.Type, "response."))
		case "error":
			return nil, errors.New("上游工具反馈返回 error 事件")
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

// A repair must be a terminal Responses snapshot before any calls can escape.
func validateFeedbackSnapshot(raw []byte, eventStatus string) ([]byte, error) {
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
	if eventStatus == "" {
		return raw, nil
	}
	return json.Marshal(response)
}
