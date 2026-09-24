package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Stream leaves text/reasoning incremental. Function envelopes are held until
// output_item.done, because code is nested JSON and the complete native item is
// required for replay. It then emits a consistent standard tool event sequence.
func (r *Request) Stream(ctx context.Context, src io.Reader, emit func([]byte) error) error {
	s := &streamBridge{request: r, emit: emit, pending: map[int]string{}, delivered: map[string]bool{}}
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 32<<10), MaxBodyBytes)
	var data []string
	size := 0
	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		size = 0
		return s.event(ctx, []byte(raw))
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Text() // ScanLines handles both LF and CRLF.
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			part := strings.TrimPrefix(line, "data:")
			part = strings.TrimPrefix(part, " ")
			size += len(part)
			if size > MaxBodyBytes {
				return errors.New("上游 SSE 事件超过大小限制")
			}
			data = append(data, part)
		} else if strings.HasPrefix(line, ":") {
			if err := emit([]byte(line + "\n\n")); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("读取上游 SSE 失败")
	}
	if err := flush(); err != nil {
		return err
	}
	if !s.terminal {
		return errors.New("上游 SSE 在终结事件前断开")
	}
	return nil
}

type streamBridge struct {
	request   *Request
	emit      func([]byte) error
	sequence  int
	pending   map[int]string
	delivered map[string]bool
	terminal  bool
}

func (s *streamBridge) send(event object) error {
	event["sequence_number"] = encoded(s.sequence)
	s.sequence++
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.emit([]byte(fmt.Sprintf("event: %s\ndata: %s\n\n", stringValue(event["type"]), raw)))
}

func (s *streamBridge) event(ctx context.Context, raw []byte) error {
	if string(raw) == "[DONE]" {
		if !s.terminal {
			return errors.New("SSE 缺少 Responses 终结事件")
		}
		return s.emit([]byte("data: [DONE]\n\n"))
	}
	event, err := parseObject(raw)
	if err != nil {
		return errors.New("上游 SSE data 不是 JSON 对象")
	}
	typ := stringValue(event["type"])
	if typ == "" {
		return errors.New("上游 SSE 缺少事件类型")
	}
	var index int
	_ = json.Unmarshal(event["output_index"], &index)
	switch typ {
	case "response.output_item.added":
		item, err := parseObject(event["item"])
		if err != nil {
			return err
		}
		if stringValue(item["type"]) == "function_call" {
			s.pending[index] = stringValue(item["id"])
			return nil
		}
	case "response.function_call_arguments.delta", "response.function_call_arguments.done":
		// Never leak the outer OfficeJS arguments to the local tool executor.
		return nil
	case "response.output_item.done":
		item, err := parseObject(event["item"])
		if err != nil {
			return err
		}
		if stringValue(item["type"]) == "function_call" {
			return s.completeCall(ctx, index, event["item"], event["response_id"])
		}
	case "response.created", "response.in_progress":
		// Normally these snapshots have empty output. If a server includes an
		// in-progress native function, suppress it until the complete item exists.
		if response, err := parseObject(event["response"]); err == nil {
			var items []json.RawMessage
			_ = json.Unmarshal(response["output"], &items)
			for _, raw := range items {
				item, _ := parseObject(raw)
				if stringValue(item["type"]) == "function_call" {
					response["output"] = encoded([]any{})
					break
				}
			}
			event["response"] = encoded(response)
		}
	case "response.completed", "response.incomplete", "response.failed":
		response, err := parseObject(event["response"])
		if err != nil {
			return err
		}
		var output []json.RawMessage
		if len(response["output"]) > 0 && json.Unmarshal(response["output"], &output) != nil {
			return errors.New("上游 output 无效")
		}
		for i, raw := range output {
			item, _ := parseObject(raw)
			if stringValue(item["type"]) == "function_call" {
				if err := s.completeCall(ctx, i, raw, response["id"]); err != nil {
					return err
				}
			}
		}
		if typ == "response.completed" && len(s.pending) > 0 {
			return errors.New("上游工具流缺少完整 output item")
		}
		if typ == "response.completed" {
			response["status"] = encoded("completed")
		} else {
			s.request.Failed = true
		}
		converted, err := s.request.Response(ctx, encoded(response))
		if err != nil {
			return err
		}
		event["response"] = converted
		s.terminal = true
	case "error":
		s.request.Failed = true
		s.terminal = true
	}
	return s.send(event)
}

func (s *streamBridge) completeCall(ctx context.Context, index int, raw, responseID json.RawMessage) error {
	converted, err := s.request.convertCall(ctx, raw)
	if err != nil {
		return err
	}
	item, _ := parseObject(converted)
	id := stringValue(item["id"])
	delete(s.pending, index)
	if s.delivered[id] {
		return nil
	}
	s.delivered[id] = true
	field, deltaType, doneType := "arguments", "response.function_call_arguments.delta", "response.function_call_arguments.done"
	if stringValue(item["type"]) == "custom_tool_call" {
		field = "input"
		deltaType = "response.custom_tool_call_input.delta"
		doneType = "response.custom_tool_call_input.done"
	}
	value := item[field]
	added, _ := parseObject(converted)
	added[field] = encoded("")
	added["status"] = encoded("in_progress")
	events := []object{
		{"type": encoded("response.output_item.added"), "output_index": encoded(index), "item": encoded(added)},
		{"type": encoded(deltaType), "output_index": encoded(index), "item_id": item["id"], "delta": value},
		{"type": encoded(doneType), "output_index": encoded(index), "item_id": item["id"], field: value},
		{"type": encoded("response.output_item.done"), "output_index": encoded(index), "item": converted},
	}
	for _, event := range events {
		if len(responseID) > 0 {
			event["response_id"] = responseID
		}
		if err := s.send(event); err != nil {
			return err
		}
	}
	return nil
}
