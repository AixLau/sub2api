// Package basispoints defines the account-scoped client identity sent to BPS.
package basispoints

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// MaxClientHeaderBytes bounds the captured identity, not an entire credential.
const MaxClientHeaderBytes = 32 * 1024

// IsClientHeader excludes credentials, cookies, protocol and routing headers.
func IsClientHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "user-agent", "x-openai-account-user-id",
		"x-openai-internal-basispoints-browser-name",
		"x-openai-internal-basispoints-browser-ua-brands",
		"x-openai-internal-basispoints-browser-ua-mobile",
		"x-openai-internal-basispoints-browser-ua-platform",
		"x-openai-internal-basispoints-client-agent-profile",
		"x-openai-internal-basispoints-client-editor",
		"x-openai-internal-basispoints-client-host",
		"x-openai-internal-basispoints-client-platform",
		"x-openai-internal-basispoints-client-platform-class",
		"x-openai-internal-basispoints-client-product",
		"x-openai-internal-basispoints-client-runtime",
		"x-openai-internal-basispoints-office-host",
		"x-openai-internal-basispoints-office-platform",
		"x-stainless-arch", "x-stainless-lang", "x-stainless-os",
		"x-stainless-package-version", "x-stainless-retry-count",
		"x-stainless-runtime", "x-stainless-runtime-version":
		return true
	default:
		return false
	}
}

// CredentialHeaders takes a fresh snapshot of the selected account. Captured
// headers override headers, irrespective of casing. Neither access tokens nor
// HTTP request headers participate in this snapshot.
func CredentialHeaders(credentials map[string]any) (http.Header, error) {
	out := make(http.Header)
	for _, field := range []string{"headers", "captured_headers"} {
		value := credentials[field]
		if value == nil {
			continue
		}
		entries := make(map[string]any)
		switch v := value.(type) {
		case map[string]any:
			entries = v
		case map[string]string:
			for k, value := range v {
				entries[k] = value
			}
		case http.Header:
			for k, value := range v {
				entries[k] = value
			}
		default:
			return nil, fmt.Errorf("BPS credentials.%s must be a header object", field)
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		seen := make(map[string]bool)
		for _, key := range keys {
			if !IsClientHeader(key) {
				continue
			}
			name := http.CanonicalHeaderKey(strings.TrimSpace(key))
			if seen[name] {
				return nil, fmt.Errorf("BPS credentials.%s contains duplicate client header names", field)
			}
			seen[name] = true
			var values []string
			switch v := entries[key].(type) {
			case string:
				values = []string{v}
			case []string:
				values = v
			case []any:
				for _, value := range v {
					text, ok := value.(string)
					if !ok {
						return nil, errors.New("BPS captured client header values must be strings")
					}
					values = append(values, text)
				}
			default:
				return nil, errors.New("BPS captured client header values must be strings")
			}
			clean := make([]string, 0, len(values))
			for _, value := range values {
				for _, c := range []byte(value) {
					if c < 32 || c == 127 {
						return nil, errors.New("BPS captured client header contains control characters")
					}
				}
				if value = strings.TrimSpace(value); value != "" {
					clean = append(clean, value)
				}
			}
			if len(clean) > 0 {
				out[name] = clean
			}
		}
	}
	if headerBytes(out) > MaxClientHeaderBytes {
		return nil, errors.New("BPS captured client headers exceed the size limit")
	}
	return out, nil
}

func headerBytes(headers http.Header) int {
	size := 0
	for key, values := range headers {
		for _, value := range values {
			size += len(key) + len(value)
		}
	}
	return size
}
