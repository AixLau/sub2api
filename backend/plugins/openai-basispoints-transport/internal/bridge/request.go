package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
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

// Prepare rebuilds the client body into the Basis Points / Excel add-in wire
// shape. The upstream rejects client-supplied tools and any field outside that
// vocabulary with a bare 422, so the outgoing body is assembled from known
// fields only instead of deleting a denylist from the caller's body.
// modelMapping translates requested models to upstream slugs; unmapped models
// keep the default pass-through with the -excel alias suffix removed.
func Prepare(ctx context.Context, raw []byte, scope string, store Store, modelMapping map[string]string) (*Request, error) {
	if len(raw) > MaxBodyBytes {
		return nil, errors.New("请求体超过桥接大小限制")
	}
	root, err := parseObject(raw)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if len(root["input"]) > 0 && root["input"][0] == '"' {
		input = []json.RawMessage{encoded(messageItem("user", stringValue(root["input"])))}
	} else if string(root["input"]) == "null" || json.Unmarshal(root["input"], &input) != nil {
		return nil, errors.New("input 必须是字符串或数组")
	}
	cat, err := readCatalog(root["tools"], root["tool_choice"], input)
	if err != nil {
		return nil, err
	}
	if len(cat.tools) > 0 && store == nil {
		return nil, errors.New("工具桥接需要宿主 KV 服务；当前插件未连接宿主 KV")
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
	restoredCalls := map[string]bool{}
	nativeCallIDs := map[string]string{}
	trackTurn := func(record *callRecord, i int) error {
		if i <= lastUser {
			return nil
		}
		if continuation != nil && continuation.ID != record.Turn.ID {
			return errors.New("同一次回放不能混用不同 turn_id")
		}
		if continuation == nil || record.Turn.Iteration >= continuation.Iteration {
			copy := record.Turn
			continuation = &copy
		}
		return nil
	}
	for i, raw := range input {
		item, err := parseObject(raw)
		if err != nil {
			return nil, err
		}
		typ, callID := stringValue(item["type"]), stringValue(item["call_id"])
		switch typ {
		case "function_call", "custom_tool_call":
			if callID == "" {
				return nil, errors.New("工具调用缺少 call_id")
			}
			name, namespace := stringValue(item["name"]), stringValue(item["namespace"])
			catalogName := name
			if namespace != "" {
				catalogName = namespace + "." + name
			}
			var native json.RawMessage
			var nativeCallID string
			switch {
			case strings.HasPrefix(callID, callPrefix):
				record, err := lookup(callID)
				if err != nil {
					return nil, err
				}
				if err := trackTurn(record, i); err != nil {
					return nil, err
				}
				original, _ := parseObject(record.Original)
				native, nativeCallID = record.Original, stringValue(original["call_id"])
			case isTransportName(name):
				// Already a native upstream item (e.g. captured from an Excel
				// session); replay it verbatim instead of re-wrapping it.
				native, nativeCallID = raw, callID
			case name == "":
				return nil, errors.New("工具调用缺少名称")
			default:
				if _, declared := r.catalog.tools[catalogName]; declared {
					// History the plugin never minted: pre-plugin client tool
					// calls. Rebuild a transport envelope from the client's own
					// item so the turn can continue.
					native = rebuildTransportCall(item, typ, catalogName)
				} else {
					// Server-injected native tool (run_connector_action,
					// update_plan, ...) in history: replay unchanged.
					native = raw
				}
				nativeCallID = callID
			}
			if !restoredCalls[callID] {
				restored = append(restored, native)
				restoredCalls[callID] = true
				nativeCallIDs[callID] = nativeCallID
			}
		case "function_call_output", "custom_tool_call_output":
			if len(item["output"]) == 0 {
				return nil, errors.New("工具结果缺少 output")
			}
			nativeCallID, known := nativeCallIDs[callID]
			if !known {
				if !strings.HasPrefix(callID, callPrefix) {
					return nil, errors.New("工具结果缺少对应的工具调用；请开始新会话")
				}
				record, err := lookup(callID)
				if err != nil {
					return nil, err
				}
				if err := trackTurn(record, i); err != nil {
					return nil, err
				}
				if !restoredCalls[callID] {
					original, _ := parseObject(record.Original)
					restored = append(restored, record.Original)
					restoredCalls[callID] = true
					nativeCallIDs[callID] = stringValue(original["call_id"])
				}
				nativeCallID = nativeCallIDs[callID]
			}
			out := object{
				"type":    encoded("function_call_output"),
				"call_id": encoded(nativeCallID),
				"output":  item["output"],
				"id":      encoded(functionItemID(nativeCallID)),
			}
			restored = append(restored, encoded(out))
		case "reasoning":
			// store:false is mandatory, and the upstream rejects bare reasoning
			// items in that mode. Only encrypted content is replayable.
			if encrypted := stringValue(item["encrypted_content"]); encrypted != "" {
				restored = append(restored, encoded(object{
					"type":              encoded("reasoning"),
					"summary":           encoded([]any{}),
					"encrypted_content": item["encrypted_content"],
				}))
			}
		case "additional_tools":
			// Responses Lite tool declarations are folded into the catalog
			// prompt; the carrier item itself is not forwarded.
		case "item_reference":
			// Server-side item references cannot resolve without upstream state.
		case "image_generation":
			// Generated images live server-side and are not persisted when
			// store is false: replaying a thin item (id only) 404s the whole
			// request. Keep only self-contained items that still carry data.
			for _, field := range []string{"result", "output", "data", "image"} {
				if value := item[field]; len(value) > 0 && string(value) != "null" {
					restored = append(restored, cleanItem(raw))
					break
				}
			}
		default:
			restored = append(restored, cleanItem(raw))
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
		prefix := cleanedPrefix(input, lastUser)
		var canonical any
		decoder := json.NewDecoder(bytes.NewReader(encoded(prefix)))
		decoder.UseNumber()
		_ = decoder.Decode(&canonical)
		r.Turn.ID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(digest(scope, string(encoded(canonical))))).String()
		// Foreign tool rounds carry no replay record; derive the iteration from
		// the completed rounds after the last user message so the upstream plan
		// state advances instead of restarting.
		r.Turn.Iteration = toolRoundsAfter(input, lastUser)
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
		return nil, errors.New("工具往返达到 512 轮上限；请开始新的 turn")
	}
	effort, err := normalizeReasoningEffort(root)
	if err != nil {
		return nil, err
	}
	// Catalog is ordinary developer text, never an upstream tool declaration.
	var prologue []json.RawMessage
	if instructions := stringValue(root["instructions"]); strings.TrimSpace(instructions) != "" {
		prologue = append(prologue, encoded(messageItem("developer", instructions)))
	}
	prologue = append(prologue, encoded(messageItem("developer", cat.prompt())))
	out := object{}
	if model := stringValue(root["model"]); model != "" {
		if mapped, ok := modelMapping[model]; ok {
			out["model"] = encoded(mapped)
		} else {
			out["model"] = encoded(strings.TrimSuffix(model, "-excel"))
		}
	}
	// Mark picker choices as explicit so the backend does not apply its own
	// automatic model routing to a valid slug.
	out["model_selection"] = encoded("explicit")
	var stream bool
	_ = json.Unmarshal(root["stream"], &stream)
	out["stream"] = encoded(stream)
	out["store"] = encoded(false)
	out["input"] = encoded(append(prologue, restored...))
	if cacheKey := stringValue(root["prompt_cache_key"]); cacheKey != "" {
		out["prompt_cache_key"] = encoded(cacheKey)
	}
	out["reasoning_effort"] = encoded(effort)
	if raw := root["context_management"]; len(raw) > 0 && raw[0] == '[' {
		out["context_management"] = raw
	} else {
		out["context_management"] = encoded([]any{map[string]any{"type": "compaction", "compact_threshold": 200000}})
	}
	meta := map[string]string{
		"task_id":         scope,
		"turn_id":         r.Turn.ID,
		"agent_iteration": strconv.Itoa(r.Turn.Iteration),
	}
	if raw := root["metadata"]; len(raw) > 0 && raw[0] == '{' {
		var extra map[string]json.RawMessage
		if json.Unmarshal(raw, &extra) == nil {
			for key, value := range extra {
				if _, reserved := meta[key]; reserved {
					continue
				}
				text := scalarText(value)
				if text == "" {
					continue
				}
				if len(key) > 64 {
					key = key[:64]
				}
				if len(text) > 512 {
					text = text[:512]
				}
				meta[key] = text
			}
		}
	}
	out["metadata"] = encoded(meta)
	r.Body, err = json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(r.Body) > MaxBodyBytes {
		return nil, errors.New("转换后请求体超过桥接大小限制")
	}
	return r, nil
}

func messageItem(role, text string) map[string]any {
	return map[string]any{
		"type":    "message",
		"role":    role,
		"content": []any{map[string]any{"type": "input_text", "text": text}},
	}
}

// rebuildTransportCall wraps a non-bridge tool call from client history in a
// run_officejs transport envelope. Only the client's own name/arguments are
// used; no code is executed and no upstream identity is invented beyond a
// deterministic item id.
func rebuildTransportCall(item object, typ, name string) json.RawMessage {
	callID := stringValue(item["call_id"])
	var args any
	if typ == "custom_tool_call" {
		args = stringValue(item["input"])
	} else {
		rawArgs := item["arguments"]
		if s := stringValue(rawArgs); s != "" {
			rawArgs = []byte(s)
		}
		if len(rawArgs) == 0 || json.Unmarshal(rawArgs, &args) != nil {
			args = map[string]any{}
		}
		if _, ok := args.(map[string]any); !ok {
			args = map[string]any{}
		}
	}
	envelope := map[string]any{"tool": name, "args": args}
	outer := map[string]any{
		"summary":     "Run client tool " + name,
		"code":        string(encoded(envelope)),
		"destructive": false,
		"references":  []string{},
	}
	return encoded(object{
		"type":      encoded("function_call"),
		"id":        encoded(functionItemID(callID)),
		"call_id":   encoded(callID),
		"name":      encoded("run_officejs"),
		"arguments": encoded(string(encoded(outer))),
		"status":    encoded("completed"),
	})
}

// cleanItem drops caller transport metadata that destabilizes prompt caching
// and is not part of the upstream item vocabulary.
func cleanItem(raw json.RawMessage) json.RawMessage {
	item, err := parseObject(raw)
	if err != nil {
		return raw
	}
	if _, ok := item["internal_chat_message_metadata_passthrough"]; !ok {
		return raw
	}
	delete(item, "internal_chat_message_metadata_passthrough")
	return encoded(item)
}

func cleanedPrefix(input []json.RawMessage, lastUser int) []json.RawMessage {
	end := len(input)
	if lastUser >= 0 {
		end = lastUser + 1
	}
	prefix := make([]json.RawMessage, 0, end)
	for _, raw := range input[:end] {
		prefix = append(prefix, cleanItem(raw))
	}
	return prefix
}

// toolRoundsAfter counts completed tool rounds after the last user message.
// Consecutive call items form one round (parallel calls); each output closes
// its round so the next call starts a new one.
func toolRoundsAfter(input []json.RawMessage, lastUser int) int {
	start := 0
	if lastUser >= 0 {
		start = lastUser + 1
	}
	rounds, closed := 0, false
	for _, raw := range input[start:] {
		item, err := parseObject(raw)
		if err != nil {
			continue
		}
		switch stringValue(item["type"]) {
		case "function_call", "custom_tool_call":
			if rounds == 0 || closed {
				rounds++
				closed = false
			}
		case "function_call_output", "custom_tool_call_output":
			closed = true
		}
	}
	return rounds
}

func normalizeReasoningEffort(root object) (string, error) {
	raw := root["reasoning_effort"]
	if len(root["reasoning"]) > 0 && string(root["reasoning"]) != "null" {
		if reasoning, err := parseObject(root["reasoning"]); err == nil {
			if effort := reasoning["effort"]; len(effort) > 0 {
				raw = effort
			}
		}
	}
	effort := strings.ToLower(strings.TrimSpace(stringValue(raw)))
	if effort == "" {
		return "medium", nil
	}
	switch effort {
	case "low", "medium", "high", "xhigh":
		return effort, nil
	case "ultra":
		// Upstream keeps this tier; forward it unchanged.
		return "ultra", nil
	case "max", "x-high", "extra-high", "extra_high", "extrahigh":
		// max is a client-side alias for the highest standard tier.
		return "xhigh", nil
	default:
		return "", errors.New("不支持的 reasoning effort；支持 low/medium/high/xhigh/ultra，max 映射为 xhigh")
	}
}

func scalarText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch {
	case raw[0] == '"':
		return stringValue(raw)
	case raw[0] == 't' || raw[0] == 'f' || raw[0] == '-' || (raw[0] >= '0' && raw[0] <= '9'):
		return string(raw)
	}
	return ""
}
