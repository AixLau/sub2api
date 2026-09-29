package bridge

import (
	"encoding/json"
	"strings"
)

// NormalizeCompactionToolIDs applies the same client wire identity used when
// delivering tool calls to plugin-owned items in a checkpoint. Persisted BPS
// histories can contain function IDs on custom calls, which native Responses
// rejects before inference. Call IDs, payloads and encrypted history stay intact.
// Bodies with no invalid plugin-owned IDs are returned byte-for-byte unchanged.
func NormalizeCompactionToolIDs(body []byte) ([]byte, error) {
	root, err := parseObject(body)
	if err != nil {
		return nil, err
	}
	input := inputItems(root)
	renamed := map[string]string{}
	for i, raw := range input {
		item, err := parseObject(raw)
		if err != nil || !strings.HasPrefix(stringValue(item["call_id"]), callPrefix) {
			continue
		}
		kind, id := stringValue(item["type"]), stringValue(item["id"])
		if id == "" || (kind != "custom_tool_call" && kind != "function_call") {
			continue
		}
		canonical := clientToolItemID(id, kind == "custom_tool_call")
		if canonical == id {
			continue
		}
		renamed[id] = canonical
		item["id"] = encoded(canonical)
		input[i] = encoded(item)
	}
	if len(renamed) == 0 {
		return body, nil
	}
	for i, raw := range input {
		item, err := parseObject(raw)
		if err != nil || stringValue(item["type"]) != "item_reference" {
			continue
		}
		if canonical, ok := renamed[stringValue(item["id"])]; ok {
			item["id"] = encoded(canonical)
			input[i] = encoded(item)
		}
	}
	root["input"] = encoded(input)
	return json.Marshal(root)
}

// IsContextCompaction identifies Codex context compaction using structured turn
// metadata. The original Responses compaction request has an empty catalog,
// while remote compaction v2 appends a compaction_trigger item and may retain
// the model-visible tool catalog. Both are protocol control requests: the BPS
// endpoint's injected Office suite must not handle either form.
func IsContextCompaction(body []byte, turnMetadata string) bool {
	root, err := parseObject(body)
	return err == nil && hasContextCompaction(root, turnMetadata)
}

// IsRemoteCompactionV2 reports the newer compaction request shape. It is kept
// separate from IsContextCompaction because only this shape has the strict
// exactly-one-compaction output contract.
func IsRemoteCompactionV2(body []byte, turnMetadata string) bool {
	root, err := parseObject(body)
	return err == nil && hasRemoteCompactionV2(root, turnMetadata)
}

func hasContextCompaction(root object, turnMetadata string) bool {
	// The host retains x-codex-turn-metadata in client_metadata for Lite
	// requests. Native Codex clients may provide the same JSON in the header.
	metadata, _ := parseObject(root["client_metadata"])
	if turnMetadata == "" {
		turnMetadata = stringValue(metadata["x-codex-turn-metadata"])
	}
	var turn struct {
		RequestKind string `json:"request_kind"`
	}
	if json.Unmarshal([]byte(turnMetadata), &turn) != nil || turn.RequestKind != "compaction" {
		return false
	}
	if hasRemoteCompactionV2(root, turnMetadata) {
		return true
	}
	cat, err := readCatalog(root["tools"], root["tool_choice"], inputItems(root))
	return err == nil && len(cat.tools) == 0 && len(cat.omitted) == 0
}

func hasRemoteCompactionV2(root object, turnMetadata string) bool {
	metadata, _ := parseObject(root["client_metadata"])
	if turnMetadata == "" {
		turnMetadata = stringValue(metadata["x-codex-turn-metadata"])
	}
	var turn struct {
		RequestKind string `json:"request_kind"`
	}
	if json.Unmarshal([]byte(turnMetadata), &turn) != nil || turn.RequestKind != "compaction" {
		return false
	}
	for _, raw := range inputItems(root) {
		item, err := parseObject(raw)
		if err == nil && stringValue(item["type"]) == "compaction_trigger" {
			return true
		}
	}
	return false
}
