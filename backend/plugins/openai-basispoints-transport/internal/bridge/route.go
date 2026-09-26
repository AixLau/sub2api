package bridge

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Route reasons are fixed identifiers, never caller-controlled text.
const (
	RouteImageInput       = "image_input"
	RouteImageGeneration  = "image_generation"
	RouteHostedToolChoice = "hosted_tool_choice"
	RouteStructuredOutput = "structured_output"
	RouteEncryptedAgent   = "encrypted_agent_message"
)

// NeedsNativeUpstream reports whether a raw Responses request must bypass
// the BPS tool bridge and go to the native Codex upstream instead.
//
// An empty reason means the request stays on the BPS path. Triggers are
// evaluated in a fixed order (image_input, image_generation,
// hosted_tool_choice, structured_output) and the first match is reported. The
// routing decision depends only on whether the reason is empty, so the
// priority never changes which upstream serves the request.
//
// The body is parsed defensively. Malformed JSON is not a routing signal here
// (the bridge reports protocol errors itself later), so a bad body simply
// stays on the BPS path and returns "".
func NeedsNativeUpstream(body []byte) (reason string) {
	root, err := parseObject(body)
	if err != nil {
		return ""
	}
	switch {
	case hasImageInput(root):
		reason = RouteImageInput
	case hasImageGeneration(root):
		reason = RouteImageGeneration
	case hasHostedToolChoice(root):
		reason = RouteHostedToolChoice
	case hasStructuredOutput(root):
		reason = RouteStructuredOutput
	case hasEncryptedAgentMessage(root):
		reason = RouteEncryptedAgent
	}
	return reason
}

// hasEncryptedAgentMessage routes opaque agent history to the native Codex
// endpoint, which can preserve and decrypt the original item. The BPS bridge
// must never guess whether an encrypted_content part is plaintext.
func hasEncryptedAgentMessage(root object) bool {
	for _, raw := range inputItems(root) {
		item, err := parseObject(raw)
		if err != nil || stringValue(item["type"]) != "agent_message" {
			continue
		}
		if hasCiphertext(item["encrypted_content"]) {
			return true
		}
		var parts []json.RawMessage
		if json.Unmarshal(item["content"], &parts) != nil {
			continue
		}
		for _, rawPart := range parts {
			part, err := parseObject(rawPart)
			if err == nil && stringValue(part["type"]) == "encrypted_content" && hasCiphertext(part["encrypted_content"]) {
				return true
			}
		}
	}
	return false
}

// hasImageInput reports whether an input_image content part appears anywhere
// in the request input. The reference form (data URL, https URL, url dict, or
// file_id) is irrelevant; only the part type matters.
func hasImageInput(root object) bool {
	return walkJSON(root["input"], func(item object) bool {
		return stringValue(item["type"]) == "input_image"
	})
}

// hasImageGeneration reports whether the request uses image generation: either
// a declared image_generation tool (at the top level, in an additional_tools
// carrier, or nested one namespace), or history carrying a generated image (an
// image_generation item or any item id in the ig_ namespace).
func hasImageGeneration(root object) bool {
	if declaresImageGeneration(root["tools"]) {
		return true
	}
	for _, raw := range inputItems(root) {
		item, err := parseObject(raw)
		if err != nil {
			continue
		}
		switch stringValue(item["type"]) {
		case "image_generation":
			return true
		case "additional_tools":
			if declaresImageGeneration(item["tools"]) {
				return true
			}
		}
		if strings.HasPrefix(stringValue(item["id"]), "ig_") {
			return true
		}
	}
	return false
}

// hasHostedToolChoice reports whether tool_choice forces a specific tool name
// that is not declared as a function/custom tool in this request. Hosted tools
// (web_search, image_generation, ...) and undeclared names cannot be executed
// by the downstream client, so those requests go native. Forcing a declared
// client function (namespace-qualified) is served by the bridge and does not
// route.
func hasHostedToolChoice(root object) bool {
	forced, ok := forcedToolName(root["tool_choice"])
	if !ok {
		return false
	}
	declared := map[string]bool{}
	collectClientToolNames(root["tools"], "", declared)
	for _, raw := range inputItems(root) {
		if item, err := parseObject(raw); err == nil && stringValue(item["type"]) == "additional_tools" {
			collectClientToolNames(item["tools"], "", declared)
		}
	}
	return !declared[forced]
}

// RequestsClientTools reports whether the request declares function/custom
// client tools, either in the top-level tools array or in additional_tools
// carrier items. Such requests work natively on the Codex upstream, where the
// tools are first-class, while the BPS executor suite cannot carry them.
func RequestsClientTools(body []byte) bool {
	root, err := parseObject(body)
	if err != nil {
		return false
	}
	if hasClientTools(root["tools"]) {
		return true
	}
	for _, raw := range inputItems(root) {
		if item, err := parseObject(raw); err == nil && stringValue(item["type"]) == "additional_tools" {
			if hasClientTools(item["tools"]) {
				return true
			}
		}
	}
	return false
}

func hasClientTools(raw json.RawMessage) bool {
	for _, entry := range toolEntries(raw) {
		switch stringValue(entry["type"]) {
		case "function", "custom":
			return true
		case "namespace":
			if hasClientTools(namespaceTools(entry)) {
				return true
			}
		}
	}
	return false
}

// inputItems decodes the request input array; a missing or malformed input
// simply carries no items.
func inputItems(root object) []json.RawMessage {
	var input []json.RawMessage
	if json.Unmarshal(root["input"], &input) != nil {
		return nil
	}
	return input
}

// hasStructuredOutput reports whether the response asks for a non-text format
// via text.format.type or response_format.type. A present type that is not
// "text" means structured output, which the BPS bridge does not model.
func hasStructuredOutput(root object) bool {
	if text, err := parseObject(root["text"]); err == nil {
		if format, err := parseObject(text["format"]); err == nil && isStructuredType(format) {
			return true
		}
	}
	if responseFormat, err := parseObject(root["response_format"]); err == nil {
		return isStructuredType(responseFormat)
	}
	return false
}

// isStructuredType reports whether a format object declares a type other than
// the plain "text" default.
func isStructuredType(format object) bool {
	raw, ok := format["type"]
	if !ok {
		return false
	}
	return stringValue(raw) != "text"
}

// forcedToolName resolves the tool name that tool_choice forces, if any. It
// returns the namespace-qualified name and whether a specific tool is forced.
// String choices "auto"/"none"/"required" (and an empty choice) force nothing.
func forcedToolName(choice json.RawMessage) (string, bool) {
	choice = json.RawMessage(bytes.TrimSpace(choice))
	if len(choice) == 0 || string(choice) == "null" {
		return "", false
	}
	if choice[0] == '"' {
		name := stringValue(choice)
		switch name {
		case "", "auto", "none", "required":
			return "", false
		}
		return name, true
	}
	if choice[0] != '{' {
		return "", false
	}
	obj, err := parseObject(choice)
	if err != nil {
		return "", false
	}
	if name := stringValue(obj["name"]); name != "" {
		if ns := stringValue(obj["namespace"]); ns != "" {
			return ns + "." + name, true
		}
		return name, true
	}
	// No explicit name: hosted-tool choices carry the tool in the type field
	// (e.g. {"type":"web_search"} or {"type":"image_generation"}). Generic
	// control types force nothing.
	switch typ := stringValue(obj["type"]); typ {
	case "", "auto", "none", "required", "function", "custom", "allowed_tools":
		return "", false
	default:
		return typ, true
	}
}

// declaresImageGeneration reports whether tools declares an image_generation
// tool, scanning nested namespace tools as well.
func declaresImageGeneration(raw json.RawMessage) bool {
	for _, entry := range toolEntries(raw) {
		switch stringValue(entry["type"]) {
		case "image_generation":
			return true
		case "namespace":
			if declaresImageGeneration(namespaceTools(entry)) {
				return true
			}
		}
	}
	return false
}

// collectClientToolNames records the namespace-qualified names of every
// function/custom tool declared in tools, including nested namespace tools.
// Hosted tools are skipped: they are not executable by the downstream client.
func collectClientToolNames(raw json.RawMessage, ns string, out map[string]bool) {
	for _, entry := range toolEntries(raw) {
		typ := stringValue(entry["type"])
		name := stringValue(entry["name"])
		if typ == "namespace" {
			collectClientToolNames(namespaceTools(entry), name, out)
			continue
		}
		if typ != "function" && typ != "custom" {
			continue
		}
		if name == "" {
			continue
		}
		key := name
		if ns != "" {
			key = ns + "." + name
		}
		out[key] = true
	}
}

// namespaceTools returns a namespace entry's nested tool list. Both "tools"
// and the legacy "children" spelling are accepted.
func namespaceTools(entry object) json.RawMessage {
	if children := entry["tools"]; len(children) > 0 {
		return children
	}
	return entry["children"]
}

// toolEntries decodes a tools array into objects, skipping malformed entries
// so one bad element cannot hide a routing signal in the others.
func toolEntries(raw json.RawMessage) []object {
	var raws []json.RawMessage
	if json.Unmarshal(raw, &raws) != nil {
		return nil
	}
	entries := make([]object, 0, len(raws))
	for _, r := range raws {
		if entry, err := parseObject(r); err == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

// walkJSON visits every JSON object reachable from raw (arrays and nested
// object values) and reports whether any satisfies visit.
func walkJSON(raw json.RawMessage, visit func(object) bool) bool {
	raw = json.RawMessage(bytes.TrimSpace(raw))
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '{':
		obj, err := parseObject(raw)
		if err != nil {
			return false
		}
		if visit(obj) {
			return true
		}
		for _, value := range obj {
			if walkJSON(value, visit) {
				return true
			}
		}
	case '[':
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		for _, item := range items {
			if walkJSON(item, visit) {
				return true
			}
		}
	}
	return false
}
