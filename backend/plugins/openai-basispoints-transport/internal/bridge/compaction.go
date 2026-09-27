package bridge

import "encoding/json"

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
