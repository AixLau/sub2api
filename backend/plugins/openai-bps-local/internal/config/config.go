// Package config parses the administrator configuration of the bps-local
// transport plugin. Credentials never appear here: the host supplies the
// OAuth identity of the bound account on every request.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-bps-local/internal/basispoints"
)

const (
	TransportWS  = "ws"
	TransportSSE = "sse"

	ProxyAccount  = "account"
	ProxyDisabled = "disabled"

	maxExtraHeaders = 32
)

// Config is the normalized plugin configuration.
type Config struct {
	// UpstreamURL is the BPS Responses endpoint (https/wss).
	UpstreamURL string `json:"upstream_url"`
	// UpstreamTransport is "ws" (bps-local default) or the legacy "sse".
	UpstreamTransport string `json:"upstream_transport"`
	// WSIdleTimeoutSeconds bounds the wait for the next upstream WS message.
	WSIdleTimeoutSeconds int `json:"ws_idle_timeout_seconds"`
	// ToolsVersion optionally pins metadata.bps_tools_version_id.
	ToolsVersion string `json:"tools_version"`
	// ARCTextSession enables bps-local's experimental ARC lifecycle for
	// text-only requests. Requests with client tools are rejected while on.
	ARCTextSession bool `json:"arc_text_session"`
	// ProxyMode is "account" (use the bound account's proxy) or "disabled".
	ProxyMode string `json:"proxy_mode"`
	// NativePassthrough forwards endpoints other than POST /responses (for
	// example /responses/compact) unchanged to the host's original URL.
	NativePassthrough *bool `json:"native_passthrough,omitempty"`
	// ExtraHeaders are added to BPS requests; identity headers are ignored.
	ExtraHeaders map[string]string `json:"extra_headers"`
}

// Default returns the configuration used when the administrator saved nothing.
func Default() Config {
	enabled := true
	return Config{
		UpstreamURL:          basispoints.ResponsesURL,
		UpstreamTransport:    TransportWS,
		WSIdleTimeoutSeconds: 120,
		ProxyMode:            ProxyAccount,
		NativePassthrough:    &enabled,
		ExtraHeaders:         map[string]string{},
	}
}

// NativePassthroughEnabled defaults to true when the field is absent.
func (c Config) NativePassthroughEnabled() bool {
	return c.NativePassthrough == nil || *c.NativePassthrough
}

// WSIdleTimeout returns the idle budget as a duration.
func (c Config) WSIdleTimeout() time.Duration {
	return time.Duration(c.WSIdleTimeoutSeconds) * time.Second
}

// Parse validates raw JSON (empty means defaults) and returns the normalized
// configuration together with its canonical JSON encoding.
func Parse(raw []byte) (Config, []byte, error) {
	cfg := Default()
	if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, nil, fmt.Errorf("配置 JSON 无效: %v", err)
		}
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, nil, err
	}
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, nil, err
	}
	return cfg, normalized, nil
}

func (c *Config) normalize() error {
	c.UpstreamURL = strings.TrimSpace(c.UpstreamURL)
	if c.UpstreamURL == "" {
		c.UpstreamURL = basispoints.ResponsesURL
	}
	parsed, err := url.Parse(c.UpstreamURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
		return errors.New("上游地址无效：需要不含凭据、查询参数和片段的完整 URL")
	}
	switch parsed.Scheme {
	case "https", "wss":
	default:
		return errors.New("上游地址必须使用 https 或 wss")
	}

	c.UpstreamTransport = strings.ToLower(strings.TrimSpace(c.UpstreamTransport))
	if c.UpstreamTransport == "" {
		c.UpstreamTransport = TransportWS
	}
	if c.UpstreamTransport != TransportWS && c.UpstreamTransport != TransportSSE {
		return errors.New("上游传输方式只能是 ws 或 sse")
	}

	if c.WSIdleTimeoutSeconds == 0 {
		c.WSIdleTimeoutSeconds = 120
	}
	if c.WSIdleTimeoutSeconds < 5 || c.WSIdleTimeoutSeconds > 3600 {
		return errors.New("WebSocket 空闲超时须在 5 到 3600 秒之间")
	}

	c.ToolsVersion = strings.TrimSpace(c.ToolsVersion)
	if len(c.ToolsVersion) > 128 || strings.ContainsAny(c.ToolsVersion, "\r\n\t ") {
		return errors.New("工具版本 ID 不能包含空白且不超过 128 字符")
	}

	c.ProxyMode = strings.ToLower(strings.TrimSpace(c.ProxyMode))
	if c.ProxyMode == "" {
		c.ProxyMode = ProxyAccount
	}
	if c.ProxyMode != ProxyAccount && c.ProxyMode != ProxyDisabled {
		return errors.New("代理模式只能是 account 或 disabled")
	}

	if c.NativePassthrough == nil {
		enabled := true
		c.NativePassthrough = &enabled
	}

	if len(c.ExtraHeaders) > maxExtraHeaders {
		return fmt.Errorf("额外请求头最多 %d 个", maxExtraHeaders)
	}
	headers := make(map[string]string, len(c.ExtraHeaders))
	keys := make([]string, 0, len(c.ExtraHeaders))
	for key := range c.ExtraHeaders {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		value := strings.TrimSpace(c.ExtraHeaders[key])
		if name == "" || !validHeaderName(name) || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("额外请求头 %q 无效", key)
		}
		if ProtectedHeader(name) {
			return fmt.Errorf("额外请求头 %q 属于身份或协议头，不能覆盖", key)
		}
		if _, exists := headers[name]; exists {
			return fmt.Errorf("额外请求头 %q 重复", key)
		}
		headers[name] = value
	}
	c.ExtraHeaders = headers
	return nil
}

// ProtectedHeader reports identity/protocol headers the plugin manages itself.
// It mirrors bps-local's applyCustomHeaders filter so the error surfaces at
// save time rather than being silently dropped per request.
func ProtectedHeader(name string) bool {
	k := strings.ToLower(name)
	switch k {
	case "authorization", "host", "cookie", "origin", "connection", "upgrade", "content-length", "content-type", "accept", "chatgpt-account-id":
		return true
	}
	return strings.HasPrefix(k, "sec-") || strings.HasPrefix(k, "proxy-") || strings.HasPrefix(k, "x-openai-") || strings.HasPrefix(k, "x-basispoints-")
}

func validHeaderName(name string) bool {
	for _, r := range name {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && !strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			return false
		}
	}
	return true
}
