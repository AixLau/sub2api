package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Validate ambiguous JSON before any identity adapter can choose a duplicate key.
func validateCredentialHTTPInput(header http.Header, body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var read func(int) error
	read = func(depth int) error {
		if depth > 128 {
			return errors.New("GROUPED_JSON_TOO_DEEP")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		d, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if d == '{' {
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name := key.(string)
				if seen[name] {
					return errors.New("AMBIGUOUS_JSON_KEY")
				}
				seen[name] = true
				if err = read(depth + 1); err != nil {
					return err
				}
			}
		} else if d == '[' {
			for decoder.More() {
				if err = read(depth + 1); err != nil {
					return err
				}
			}
		} else {
			return errors.New("INVALID_JSON")
		}
		_, err = decoder.Token()
		return err
	}
	if err := read(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("INVALID_JSON_TRAILING_DATA")
	}
	seen := map[string]bool{}
	for key, values := range header {
		lower := strings.ToLower(key)
		switch lower {
		case "authorization", "session-id", "session_id", "x-codex-turn-metadata", "x-codex-installation-id":
			if seen[lower] || len(values) > 1 {
				return errors.New("AMBIGUOUS_IDENTITY_HEADER")
			}
			seen[lower] = true
		}
	}
	return nil
}

// Existing shared identity code also serves WS. Preserve unrelated HTTP metadata
// at the grouped adapter boundary without changing WS or session/full behavior.
// Only the established identity fields may come from the transformed object.
func preserveCredentialHTTPMetadata(original, transformed []byte) ([]byte, error) {
	before := gjson.GetBytes(original, "client_metadata")
	after := gjson.GetBytes(transformed, "client_metadata")
	if !before.Exists() {
		return transformed, nil
	}
	if !before.IsObject() || !after.IsObject() {
		return nil, errors.New("INVALID_CLIENT_METADATA_TYPE")
	}
	var source, target map[string]json.RawMessage
	if err := json.Unmarshal([]byte(before.Raw), &source); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(after.Raw), &target); err != nil {
		return nil, err
	}
	identity := map[string]bool{"session_id": true, "session-id": true, "thread_id": true, "thread-id": true, "turn_id": true, "turn-id": true, "window_id": true, "x-codex-window-id": true, "installation_id": true, "x-codex-installation-id": true, "x-client-request-id": true, "x-codex-turn-metadata": true}
	for key, value := range source {
		if !identity[key] {
			target[key] = value
		}
	}
	for key := range target {
		if !identity[key] {
			if _, ok := source[key]; !ok {
				delete(target, key)
			}
		}
	}
	const embedded = "x-codex-turn-metadata"
	if source[embedded] != nil {
		var oldJSON, newJSON string
		if json.Unmarshal(source[embedded], &oldJSON) != nil || json.Unmarshal(target[embedded], &newJSON) != nil {
			return nil, errors.New("INVALID_EMBEDDED_METADATA")
		}
		var oldFields, newFields map[string]json.RawMessage
		if json.Unmarshal([]byte(oldJSON), &oldFields) != nil || json.Unmarshal([]byte(newJSON), &newFields) != nil {
			return nil, errors.New("INVALID_EMBEDDED_METADATA")
		}
		for key, value := range oldFields {
			if !identity[key] && key != "turn_started_at_unix_ms" {
				newFields[key] = value
			}
		}
		encoded, err := json.Marshal(newFields)
		if err != nil {
			return nil, err
		}
		target[embedded], err = json.Marshal(string(encoded))
		if err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(target)
	if err != nil {
		return nil, err
	}
	return sjson.SetRawBytes(transformed, "client_metadata", encoded)
}
