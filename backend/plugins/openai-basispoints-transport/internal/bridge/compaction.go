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

// IsContextCompaction identifies Codex's tool-free checkpoint generation using
// its structured turn metadata, never a substring in user text or old history.
// Codex core/src/compact.rs builds Prompt with Default::default() tools and
// session/session.rs marks this exchange request_kind=compaction. It is not an
// agent tool turn: the BPS endpoint's injected Office suite changes its contract.
func IsContextCompaction(body []byte, turnMetadata string) bool {
	root, err := parseObject(body)
	return err == nil && hasContextCompaction(root, turnMetadata)
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
	cat, err := readCatalog(root["tools"], root["tool_choice"], inputItems(root))
	return err == nil && len(cat.tools) == 0 && len(cat.omitted) == 0
}
