// Package responsesstate identifies opaque Responses history without inspecting
// user text, tool arguments, or the contents of tool results.
package responsesstate

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/tidwall/gjson"
)

func key(kind, value string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

func present(v gjson.Result) bool { return v.Exists() && v.Type != gjson.Null && v.String() != "" }

// ItemKeys covers ciphertext on items and protocol content parts, BPS aliases
// (including result-only replay), and explicit server-side item references.
func ItemKeys(item gjson.Result) []string {
	var keys []string
	add := func(kind, value string) {
		if value != "" {
			keys = append(keys, key(kind, value))
		}
	}
	cipher := func(obj gjson.Result) {
		if v := obj.Get("encrypted_content"); present(v) {
			add("encrypted", v.String())
		}
	}
	cipher(item)
	if v := item.Get("encrypted_function_args"); v.Exists() && v.Type != gjson.Null && strings.TrimSpace(v.Raw) != "" && strings.TrimSpace(v.Raw) != "[]" {
		add("encrypted-function-args", v.Raw)
	}
	for _, field := range []string{"content", "output", "summary"} {
		parts := item.Get(field)
		if parts.IsArray() {
			for _, part := range parts.Array() {
				cipher(part)
			}
		}
	}
	for _, field := range []string{"call_id", "tool_call_id", "id"} {
		id := item.Get(field).String()
		if strings.HasPrefix(id, "call_bps_") {
			add("bps-call", id)
		}
		if strings.HasPrefix(id, "msg_bps_") {
			add("bps-item", id)
		}
	}
	if item.Get("type").String() == "item_reference" {
		add("item", item.Get("id").String())
	}
	return keys
}

func InputKeys(body []byte) []string {
	var keys []string
	input := gjson.GetBytes(body, "input")
	if input.IsArray() {
		for _, item := range input.Array() {
			keys = append(keys, ItemKeys(item)...)
		}
	}
	return unique(keys)
}

// OutputKeys accepts a JSON response or a decoded SSE data event. The owning
// account must be recorded before the last bytes of the item reach the client.
func OutputKeys(body []byte) []string {
	root := gjson.ParseBytes(body)
	var keys []string
	appendItem := func(item gjson.Result) {
		found := ItemKeys(item)
		keys = append(keys, found...)
		if id := item.Get("id").String(); id != "" {
			keys = append(keys, key("item", id))
		}
	}
	if item := root.Get("item"); item.IsObject() {
		appendItem(item)
	}
	if part := root.Get("part"); part.IsObject() {
		appendItem(part)
	}
	for _, field := range []string{"output", "response.output"} {
		if output := root.Get(field); output.IsArray() {
			for _, item := range output.Array() {
				appendItem(item)
			}
		}
	}
	return unique(keys)
}

func unique(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	result := make([]string, 0, len(keys))
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			result = append(result, k)
		}
	}
	return result
}
