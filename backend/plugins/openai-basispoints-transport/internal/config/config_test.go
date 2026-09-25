package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
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
	if !strings.Contains(string(normalized), `"model_mapping":{}`) {
		t.Fatalf("normalized config omitted model_mapping: %s", normalized)
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
		`{"model_mapping":{"":"gpt-6-astra"}}`,
		`{"model_mapping":{"gpt-6-astra-basispoints":""}}`,
		`{"model_mapping":{"gpt-6-astra-basispoints":42}}`,
		`{"model_mapping":["gpt-6-astra"]}`,
	} {
		if _, _, err := Parse([]byte(raw)); err == nil {
			t.Fatalf("invalid config was accepted: %s", raw)
		}
	}
	mapped, _, err := Parse([]byte(`{"model_mapping":{" gpt-6-astra-basispoints ":"gpt-6-astra"}}`))
	if err != nil {
		t.Fatalf("model_mapping was rejected: %v", err)
	}
	if mapped.ModelMapping["gpt-6-astra-basispoints"] != "gpt-6-astra" {
		t.Fatalf("model_mapping was not trimmed and normalized: %+v", mapped.ModelMapping)
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

func TestNativeFallbackDefaultAndOverrides(t *testing.T) {
	t.Run("默认启用", func(t *testing.T) {
		cfg, normalized, err := Parse([]byte(`{}`))
		require.NoError(t, err)
		require.True(t, cfg.NativeFallbackEnabled())
		require.NotNil(t, cfg.NativeFallback)
		require.True(t, *cfg.NativeFallback)
		require.Contains(t, string(normalized), `"native_fallback":true`)
		require.Contains(t, string(normalized), `"native_upstream_base_url":""`)
	})

	t.Run("零值配置视为启用", func(t *testing.T) {
		var zero Config
		require.True(t, zero.NativeFallbackEnabled())
	})

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "显式关闭被保留", raw: `{"native_fallback":false}`, want: false},
		{name: "显式开启被保留", raw: `{"native_fallback":true}`, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, normalized, err := Parse([]byte(tc.raw))
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.NativeFallbackEnabled())
			require.NotNil(t, cfg.NativeFallback)
			require.Equal(t, tc.want, *cfg.NativeFallback)
			require.Contains(t, string(normalized), fmt.Sprintf(`"native_fallback":%t`, tc.want))
		})
	}
}

func TestNativeUpstreamBaseURL(t *testing.T) {
	valid := []struct {
		name string
		raw  string
		want string
	}{
		{name: "留空表示使用宿主原始 URL", raw: `{"native_upstream_base_url":""}`, want: ""},
		{name: "空白视为留空", raw: `{"native_upstream_base_url":"   "}`, want: ""},
		{name: "去除空白与末尾斜杠", raw: `{"native_upstream_base_url":" https://codex.example.com/v1/ "}`, want: "https://codex.example.com/v1"},
		{name: "http 亦可", raw: `{"native_upstream_base_url":"http://127.0.0.1:8080/v1/"}`, want: "http://127.0.0.1:8080/v1"},
	}
	for _, tc := range valid {
		t.Run("有效/"+tc.name, func(t *testing.T) {
			cfg, normalized, err := Parse([]byte(tc.raw))
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.NativeUpstreamBaseURL)
			require.Contains(t, string(normalized), fmt.Sprintf(`"native_upstream_base_url":%q`, tc.want))
		})
	}

	invalid := []struct {
		name string
		raw  string
	}{
		{name: "含用户信息", raw: `{"native_upstream_base_url":"https://user:pass@codex.example.com/v1"}`},
		{name: "含查询参数", raw: `{"native_upstream_base_url":"https://codex.example.com/v1?key=1"}`},
		{name: "含片段", raw: `{"native_upstream_base_url":"https://codex.example.com/v1#frag"}`},
		{name: "相对路径", raw: `{"native_upstream_base_url":"/codex/v1"}`},
		{name: "缺少 scheme", raw: `{"native_upstream_base_url":"codex.example.com/v1"}`},
		{name: "非法 scheme", raw: `{"native_upstream_base_url":"ftp://codex.example.com/v1"}`},
	}
	for _, tc := range invalid {
		t.Run("无效/"+tc.name, func(t *testing.T) {
			_, _, err := Parse([]byte(tc.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), "native_upstream_base_url")
		})
	}
}

func TestToolsViaNativeDefaultAndOverride(t *testing.T) {
	cfg, normalized, err := Parse([]byte(`{}`))
	require.NoError(t, err)
	require.False(t, cfg.ToolsViaNativeEnabled(), "tool sessions stay on the BPS bridge by default")
	require.Contains(t, string(normalized), `"tools_via_native":false`)
	on, _, err := Parse([]byte(`{"tools_via_native":true}`))
	require.NoError(t, err)
	require.True(t, on.ToolsViaNativeEnabled())
	var zero Config
	require.False(t, zero.ToolsViaNativeEnabled())
}
