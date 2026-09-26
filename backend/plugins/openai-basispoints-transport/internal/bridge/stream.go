package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Stream leaves text/reasoning incremental. Tool payloads are held until
// response.completed so the whole tool batch can be validated together and the
// complete native item saved for replay before any executable output is sent.
func (r *Request) Stream(ctx context.Context, src io.Reader, emit func([]byte) error) error {
	s := &streamBridge{request: r, emit: emit, pending: map[int]string{}, delivered: map[string]bool{}, indices: map[int]int{}, items: map[string]int{}, completed: map[int]json.RawMessage{}}
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
		err := s.event(ctx, []byte(raw))
		var toolErr *ToolCallError
		var feedbackErr *FeedbackFailure
		if errors.As(err, &toolErr) || errors.As(err, &feedbackErr) {
			s.request.Failed = true
			s.request.FailureCode = FailureCode(err)
			s.terminal = true
			return s.send(object{"type": encoded("response.failed"), "response": FailureResponse(s.request.FailureCode, err, s.request.FailureSnapshot(s.snapshot))})
		}
		return err
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
			// A terminal Responses event is sufficient to complete the exchange.
			// Do not wait for the server to close the HTTP connection: some
			// upstream proxies keep it alive, and a later read timeout must not
			// turn an already completed response into UPSTREAM_RESPONSE_FAILED.
			if s.terminal {
				return nil
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
		if s.terminal {
			return nil
		}
		return fmt.Errorf("读取上游 SSE 失败: %w", err)
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
	snapshot  json.RawMessage
	indices   map[int]int
	items     map[string]int
	completed map[int]json.RawMessage
	nextIndex int
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
	s.request.sourceEvent = diagnosticIdentifier(typ)
	if response, err := parseObject(event["response"]); err == nil {
		s.snapshot = encoded(response)
		s.request.responseID = stringValue(response["id"])
	}
	if id := stringValue(event["response_id"]); id != "" {
		s.request.responseID = id
	}
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
		if isToolCall(item) {
			s.pending[index] = stringValue(item["id"])
			return nil
		}
	case "response.function_call_arguments.delta", "response.function_call_arguments.done",
		"response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		// Never leak the outer OfficeJS arguments to the local tool executor.
		return nil
	case "response.output_item.done":
		item, err := parseObject(event["item"])
		if err != nil {
			return err
		}
		if isToolCall(item) {
			s.completed[index] = event["item"]
			delete(s.pending, index)
			if s.request.Feedback == nil {
				_, err := s.request.decodeCall(ctx, event["item"])
				return err
			}
			return nil
		}
	case "response.created", "response.in_progress":
		// Normally these snapshots have empty output. If a server includes an
		// in-progress native function, suppress it until the complete item exists.
		if response, err := parseObject(event["response"]); err == nil {
			var items []json.RawMessage
			_ = json.Unmarshal(response["output"], &items)
			for _, raw := range items {
				item, _ := parseObject(raw)
				if isToolCall(item) {
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
			if isToolCall(item) {
				if previous := s.completed[i]; previous != nil {
					old, _ := parseObject(previous)
					for _, key := range []string{"id", "call_id", "type", "name", "namespace", "arguments", "input"} {
						if string(old[key]) != string(item[key]) {
							return errors.New("上游在完成快照中修改了工具调用")
						}
					}
				}
				delete(s.pending, i)
			}
		}
		for i := range s.completed {
			if i >= len(output) {
				return errors.New("上游完成快照遗漏工具调用")
			}
			item, _ := parseObject(output[i])
			if !isToolCall(item) {
				return errors.New("上游完成快照将工具调用替换为非工具项")
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
		final, _ := parseObject(converted)
		var items []json.RawMessage
		_ = json.Unmarshal(final["output"], &items)
		ordered := map[int]json.RawMessage{}
		for _, raw := range items {
			item, _ := parseObject(raw)
			id := stringValue(item["id"])
			position, exists := s.items[id]
			if !exists {
				position = s.nextIndex
				s.nextIndex++
				s.items[id] = position
			}
			ordered[position] = raw
			if isToolCall(item) {
				if err := s.completeCall(ctx, position, raw, final["id"]); err != nil {
					return err
				}
			} else if !exists {
				if err := s.snapshotItem(position, item, final["id"]); err != nil {
					return err
				}
			}
		}
		positions := make([]int, 0, len(ordered))
		for i := range ordered {
			positions = append(positions, i)
		}
		sort.Ints(positions)
		items = items[:0]
		for _, i := range positions {
			items = append(items, ordered[i])
		}
		final["output"] = encoded(items)
		event["response"] = encoded(final)
		switch stringValue(final["status"]) {
		case "failed":
			event["type"] = encoded("response.failed")
		case "incomplete":
			event["type"] = encoded("response.incomplete")
		}
		s.terminal = true
	case "error":
		s.request.Failed = true
		s.terminal = true
	}
	if _, ok := event["output_index"]; ok {
		position, exists := s.indices[index]
		if !exists {
			position = s.nextIndex
			s.nextIndex++
			s.indices[index] = position
		}
		event["output_index"] = encoded(position)
		if id := stringValue(event["item_id"]); id != "" {
			s.items[id] = position
		}
		if item, err := parseObject(event["item"]); err == nil {
			s.items[stringValue(item["id"])] = position
		}
	}
	return s.send(event)
}

func isToolCall(item object) bool {
	typ := stringValue(item["type"])
	return typ == "function_call" || typ == "custom_tool_call"
}

func (s *streamBridge) completeCall(ctx context.Context, index int, raw, responseID json.RawMessage) error {
	converted := raw
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

// Continuation snapshots may contain text as well as repaired tool calls.
func (s *streamBridge) snapshotItem(index int, item object, responseID json.RawMessage) error {
	base := object{"output_index": encoded(index), "response_id": responseID, "item_id": item["id"]}
	send := func(typ string, extra object) error {
		e := object{"type": encoded(typ)}
		for k, v := range base {
			e[k] = v
		}
		for k, v := range extra {
			e[k] = v
		}
		return s.send(e)
	}
	added, _ := parseObject(encoded(item))
	added["status"] = encoded("in_progress")
	if stringValue(item["type"]) == "message" {
		added["content"] = encoded([]any{})
	}
	if err := send("response.output_item.added", object{"item": encoded(added)}); err != nil {
		return err
	}
	var parts []object
	_ = json.Unmarshal(item["content"], &parts)
	for i, part := range parts {
		typ := stringValue(part["type"])
		if typ != "output_text" && typ != "refusal" {
			continue
		}
		field := "text"
		if typ == "refusal" {
			field = "refusal"
		}
		empty, _ := parseObject(encoded(part))
		empty[field] = encoded("")
		base["content_index"] = encoded(i)
		if err := send("response.content_part.added", object{"part": encoded(empty)}); err != nil {
			return err
		}
		if err := send("response."+typ+".delta", object{"delta": part[field]}); err != nil {
			return err
		}
		if err := send("response."+typ+".done", object{field: part[field]}); err != nil {
			return err
		}
		if err := send("response.content_part.done", object{"part": encoded(part)}); err != nil {
			return err
		}
	}
	delete(base, "content_index")
	return send("response.output_item.done", object{"item": encoded(item)})
}
