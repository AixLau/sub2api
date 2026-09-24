package config

import (
	"strings"
	"testing"
)

func TestParseDefaultsAndRejectsUnknownFields(t *testing.T) {
	cfg, normalized, err := Parse([]byte(`{}`))
	if err != nil {
		t.Fatalf("Parse defaults: %v", err)
	}
	if cfg.UpstreamBaseURL != DefaultUpstreamBaseURL || cfg.AuthMode != DefaultAuthMode || cfg.ProxyMode != DefaultProxyMode {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if !cfg.EnableHTTP2 {
		t.Fatal("empty configuration must enable HTTP/2")
	}
	disabled, _, err := Parse([]byte(`{"enable_http2":false}`))
	if err != nil || disabled.EnableHTTP2 {
		t.Fatal("explicit HTTP/2 opt-out must be preserved")
	}
	if !strings.Contains(string(normalized), `"extra_headers":{}`) {
		t.Fatalf("normalized config omitted extra_headers: %s", normalized)
	}
	if _, _, err := Parse([]byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestParseRejectsProtectedHeadersAndInvalidLimits(t *testing.T) {
	for _, raw := range []string{
		`{"extra_headers":{"Authorization":"Bearer bad"}}`,
		`{"extra_headers":{"x-openai-account-id":"spoof"}}`,
		`{"request_timeout_seconds":-1}`,
		`{"request_timeout_seconds":601}`,
		`{"upstream_base_url":"https://user:pass@example.test/api"}`,
	} {
		if _, _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("invalid config was accepted: %s", raw)
		}
	}
}

func TestTLSVersion(t *testing.T) {
	if got, err := TLSVersion("1.2"); err != nil || got != 0x0303 {
		t.Fatalf("TLS 1.2: got %x, err %v", got, err)
	}
	if _, err := TLSVersion("1.1"); err == nil {
		t.Fatal("TLS 1.1 was accepted")
	}
}
