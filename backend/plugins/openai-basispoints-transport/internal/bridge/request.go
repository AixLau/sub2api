package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// Request is scoped to one HTTP exchange; it is not shared between goroutines.
type Request struct {
	Body         []byte
	Turn         Turn
	OmittedTools []string
	Failed       bool
	originals    map[string]string
	store        Store
	scope        string
	catalog      catalog
	converted    map[string]json.RawMessage
}

func Prepare(ctx context.Context, raw []byte, scope string, store Store) (*Request, error) {
	if len(raw) > MaxBodyBytes {
		return nil, errors.New("请求体超过桥接大小限制")
	}
	root, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	cat, err := readCatalog(root["tools"], root["tool_choice"])
	if err != nil {
		return nil, err
	}
	if len(cat.tools) > 0 && store == nil {
		return nil, errors.New("工具桥接需要宿主 KV 服务；当前插件未连接宿主 KV")
	}
	var input []json.RawMessage
	if len(root["input"]) > 0 && root["input"][0] == '"' {
		input = []json.RawMessage{encoded(map[string]any{"role": "user", "content": stringValue(root["input"])})}
	} else if string(root["input"]) == "null" || json.Unmarshal(root["input"], &input) != nil {
		return nil, errors.New("input 必须是字符串或数组")
	}
	lastUser := -1
	for i, raw := range input {
		item, err := parseObject(raw)
		if err != nil {
			return nil, err
		}
		if stringValue(item["role"]) == "user" {
			lastUser = i
		}
	}
	r := &Request{store: store, scope: scope, catalog: cat, converted: map[string]json.RawMessage{}, OmittedTools: cat.omitted, originals: map[string]string{}}
	var continuation *Turn
	records := map[string]*callRecord{}
	lookup := func(id string) (*callRecord, error) {
		if record := records[id]; record != nil {
			return record, nil
		}
		record, err := loadCall(ctx, store, scope, id)
		if err == nil {
			records[id] = record
		}
		return record, err
	}
	var restored []json.RawMessage
	seen := map[string]bool{}
	for i, raw := range input {
		item, _ := parseObject(raw)
		typ, id := stringValue(item["type"]), stringValue(item["call_id"])
		switch typ {
		case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output":
			if !strings.HasPrefix(id, callPrefix) {
				return nil, errors.New("工具历史不属于此桥接会话；请开始新会话")
			}
			record, err := lookup(id)
			if err != nil {
				return nil, err
			}
			original, _ := parseObject(record.Original)
			if i > lastUser {
				if continuation != nil && continuation.ID != record.Turn.ID {
					return nil, errors.New("同一次回放不能混用不同 turn_id")
				}
				if continuation == nil || record.Turn.Iteration >= continuation.Iteration {
					copy := record.Turn
					continuation = &copy
				}
			}
			if !seen[id] {
				restored = append(restored, record.Original)
				seen[id] = true
			}
			if typ == "function_call_output" || typ == "custom_tool_call_output" {
				if len(item["output"]) == 0 {
					return nil, errors.New("工具结果缺少 output")
				}
				// Preserve output exactly, including structured/multimodal outputs.
				out := object{"type": encoded("function_call_output"), "call_id": original["call_id"], "output": item["output"]}
				restored = append(restored, encoded(out))
			}
		default:
			restored = append(restored, raw)
		}
	}
	if prev := stringValue(root["previous_response_id"]); prev != "" && continuation == nil && lastUser < 0 {
		if store == nil {
			return nil, errors.New("previous_response_id 需要宿主 KV 状态")
		}
		raw, found, err := store.Get(ctx, stateKey(scope, "response", prev))
		if err != nil || !found {
			return nil, errors.New("上一响应的 turn 状态不存在；请开始新会话")
		}
		var t Turn
		if json.Unmarshal(raw, &t) != nil || t.ID == "" {
			return nil, errors.New("上一响应的 turn 状态无效")
		}
		continuation = &t
	}
	if continuation != nil {
		r.Turn = *continuation
		r.Turn.Iteration++
	} else {
		// Canonical user-turn prefix makes retries deterministic without sharing
		// a mutable counter across concurrent branches or unrelated sessions.
		prefix := input
		if lastUser >= 0 {
			prefix = input[:lastUser+1]
		}
		var canonical any
		decoder := json.NewDecoder(bytes.NewReader(encoded(prefix)))
		decoder.UseNumber()
		_ = decoder.Decode(&canonical)
		r.Turn.ID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(digest(scope, string(encoded(canonical))))).String()
	}
	if rawID, ok := root["turn_id"]; ok {
		id := stringValue(rawID)
		if id == "" {
			return nil, errors.New("turn_id 必须是非空字符串")
		}
		if continuation != nil && id != r.Turn.ID {
			return nil, errors.New("工具回放必须沿用原 turn_id")
		}
		r.Turn.ID = id
	}
	if rawIteration, ok := root["agent_iteration"]; ok {
		var n int
		if string(rawIteration) == "null" || json.Unmarshal(rawIteration, &n) != nil || n < r.Turn.Iteration {
			return nil, errors.New("agent_iteration 必须是递增的非负整数")
		}
		r.Turn.Iteration = n
	}
	if r.Turn.Iteration >= MaxIterations {
		return nil, errors.New("工具往返达到 64 轮上限；请开始新的 turn")
	}
	root["turn_id"] = encoded(r.Turn.ID)
	root["agent_iteration"] = encoded(r.Turn.Iteration)
	delete(root, "tools")
	delete(root, "tool_choice")
	delete(root, "parallel_tool_calls")
	// Catalog is ordinary developer text, never an upstream tool declaration.
	message := encoded(map[string]any{"role": "developer", "content": []any{map[string]string{"type": "input_text", "text": cat.prompt()}}})
	root["input"] = encoded(append([]json.RawMessage{message}, restored...))
	r.Body, err = json.Marshal(root)
	if len(r.Body) > MaxBodyBytes {
		return nil, errors.New("转换后请求体超过桥接大小限制")
	}
	return r, err
}
