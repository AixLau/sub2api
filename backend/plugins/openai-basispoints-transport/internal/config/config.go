package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const (
	DefaultUpstreamBaseURL = "https://bps.openai.com/basispoints/api"
	DefaultAuthMode        = "chatgpt"
	DefaultProxyMode       = "account"
	DefaultTLSMinVersion   = "1.2"
	BPSModelModeAll        = "all"
	BPSModelModeSelected   = "selected"

	maxExtraHeaders = 32
	maxHeaderValue  = 8 * 1024
	maxModelMapping = 64
	maxModelName    = 128
	maxBPSModels    = 64
)

// Config is deliberately limited to transport concerns. OAuth credentials are
// resolved by the Sub2API host for the selected account and never persisted by
// this plugin.
type Config struct {
	UpstreamBaseURL              string            `json:"upstream_base_url"`
	AuthMode                     string            `json:"auth_mode"`
	ProxyMode                    string            `json:"proxy_mode"`
	RequestTimeoutSeconds        int               `json:"request_timeout_seconds"`
	ResponseHeaderTimeoutSeconds int               `json:"response_header_timeout_seconds"`
	IdleConnectionTimeoutSeconds int               `json:"idle_connection_timeout_seconds"`
	MaxIdleConnections           int               `json:"max_idle_connections"`
	MaxIdleConnectionsPerHost    int               `json:"max_idle_connections_per_host"`
	EnableHTTP2                  bool              `json:"enable_http2"`
	TLSMinVersion                string            `json:"tls_min_version"`
	ExtraHeaders                 map[string]string `json:"extra_headers"`
	// ModelMapping translates the requested model to the upstream slug before
	// the request leaves the plugin. Requests for models that are not listed
	// keep the default pass-through behavior.
	ModelMapping map[string]string `json:"model_mapping"`
	// BPSModelMode selects all models or only exact pre-mapping model names.
	// An empty selected list sends every request to the native upstream.
	BPSModelMode string   `json:"bps_model_mode"`
	BPSModels    []string `json:"bps_models"`
	// NativeFallback enables the native-channel fallback: requests the Basis
	// Points upstream cannot serve are forwarded verbatim to the native Codex
	// endpoint. A missing value means enabled.
	NativeFallback *bool `json:"native_fallback"`
	// NativeUpstreamBaseURL optionally overrides the native Codex endpoint the
	// fallback path forwards to. Empty means the host-supplied request URL is
	// used verbatim.
	NativeUpstreamBaseURL string `json:"native_upstream_base_url"`
	// ToolsViaNative routes every request that declares client function/custom
	// tools to the native Codex endpoint, where those tools are first-class.
	// Tool-carrying agent sessions belong on the BPS bridge by default, so a
	// missing value means disabled; enable only when the BPS executor suite
	// lacks the run_officejs transport.
	ToolsViaNative *bool `json:"tools_via_native"`
	// AutoDisableOn403 stops sending an account through BPS after an upstream
	// 403. The account remains available on the native channel.
	AutoDisableOn403 *bool `json:"auto_disable_bps_on_403"`
}

// NativeFallbackEnabled reports whether the native-channel fallback is active.
// An absent value means enabled.
func (c Config) NativeFallbackEnabled() bool {
	return c.NativeFallback == nil || *c.NativeFallback
}

// ToolsViaNativeEnabled reports whether client-tool requests go native.
// An absent value means disabled: tool sessions stay on the BPS bridge.
func (c Config) ToolsViaNativeEnabled() bool {
	return c.ToolsViaNative != nil && *c.ToolsViaNative
}

func (c Config) AutoDisableOn403Enabled() bool {
	return c.AutoDisableOn403 != nil && *c.AutoDisableOn403
}

// AllowsBPSModel is evaluated before capability routing and model mapping.
func (c Config) AllowsBPSModel(model string) bool {
	if c.BPSModelMode == BPSModelModeAll {
		return true
	}
	if c.BPSModelMode != BPSModelModeSelected {
		return false
	}
	for _, selected := range c.BPSModels {
		if model == selected {
			return true
		}
	}
	return false
}

func Defaults() Config {
	return Config{
		UpstreamBaseURL:              DefaultUpstreamBaseURL,
		AuthMode:                     DefaultAuthMode,
		ProxyMode:                    DefaultProxyMode,
		RequestTimeoutSeconds:        120,
		ResponseHeaderTimeoutSeconds: 30,
		IdleConnectionTimeoutSeconds: 90,
		MaxIdleConnections:           100,
		MaxIdleConnectionsPerHost:    20,
		EnableHTTP2:                  true,
		TLSMinVersion:                DefaultTLSMinVersion,
		ExtraHeaders:                 map[string]string{},
		ModelMapping:                 map[string]string{},
		BPSModelMode:                 BPSModelModeAll,
		BPSModels:                    []string{},
		NativeFallback:               boolPtr(true),
		NativeUpstreamBaseURL:        "",
		ToolsViaNative:               boolPtr(false),
		AutoDisableOn403:             boolPtr(true),
	}
}

func boolPtr(value bool) *bool {
	return &value
}

// Parse validates and normalizes a JSON object. Unknown fields are rejected so
// a typo cannot silently change the upstream behavior.
func Parse(raw []byte) (Config, []byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		return Config{}, nil, errors.New("配置必须是 JSON 对象")
	}
	cfg := Defaults()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, nil, fmt.Errorf("配置必须是 JSON 对象: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return Config{}, nil, errors.New("配置只能包含一个 JSON 值")
	} else if err != io.EOF {
		return Config{}, nil, errors.New("配置只能包含一个 JSON 值")
	}

	defaults := Defaults()
	applyDefaults(&cfg, defaults)
	if err := validate(&cfg); err != nil {
		return Config{}, nil, err
	}
	normalized, err := json.Marshal(cfg)
	if err != nil {
		return Config{}, nil, fmt.Errorf("序列化规范化配置失败: %w", err)
	}
	return cfg, normalized, nil
}

func applyDefaults(cfg *Config, defaults Config) {
	if strings.TrimSpace(cfg.UpstreamBaseURL) == "" {
		cfg.UpstreamBaseURL = defaults.UpstreamBaseURL
	}
	if strings.TrimSpace(cfg.AuthMode) == "" {
		cfg.AuthMode = defaults.AuthMode
	}
	if strings.TrimSpace(cfg.ProxyMode) == "" {
		cfg.ProxyMode = defaults.ProxyMode
	}
	if cfg.RequestTimeoutSeconds == 0 {
		cfg.RequestTimeoutSeconds = defaults.RequestTimeoutSeconds
	}
	if cfg.ResponseHeaderTimeoutSeconds == 0 {
		cfg.ResponseHeaderTimeoutSeconds = defaults.ResponseHeaderTimeoutSeconds
	}
	if cfg.IdleConnectionTimeoutSeconds == 0 {
		cfg.IdleConnectionTimeoutSeconds = defaults.IdleConnectionTimeoutSeconds
	}
	if cfg.MaxIdleConnections == 0 {
		cfg.MaxIdleConnections = defaults.MaxIdleConnections
	}
	if cfg.MaxIdleConnectionsPerHost == 0 {
		cfg.MaxIdleConnectionsPerHost = defaults.MaxIdleConnectionsPerHost
	}
	if strings.TrimSpace(cfg.TLSMinVersion) == "" {
		cfg.TLSMinVersion = defaults.TLSMinVersion
	}
	if cfg.ExtraHeaders == nil {
		cfg.ExtraHeaders = map[string]string{}
	}
	if cfg.ModelMapping == nil {
		cfg.ModelMapping = map[string]string{}
	}
	if cfg.BPSModels == nil {
		cfg.BPSModels = []string{}
	}
	if cfg.NativeFallback == nil {
		if defaults.NativeFallback != nil {
			cfg.NativeFallback = boolPtr(*defaults.NativeFallback)
		} else {
			cfg.NativeFallback = boolPtr(true)
		}
	}
	if cfg.ToolsViaNative == nil {
		if defaults.ToolsViaNative != nil {
			cfg.ToolsViaNative = boolPtr(*defaults.ToolsViaNative)
		} else {
			cfg.ToolsViaNative = boolPtr(false)
		}
	}
	if cfg.AutoDisableOn403 == nil {
		if defaults.AutoDisableOn403 != nil {
			cfg.AutoDisableOn403 = boolPtr(*defaults.AutoDisableOn403)
		} else {
			cfg.AutoDisableOn403 = boolPtr(true)
		}
	}
}

func validate(cfg *Config) error {
	normalizedUpstream, err := normalizeBaseURL("upstream_base_url", cfg.UpstreamBaseURL)
	if err != nil {
		return err
	}
	cfg.UpstreamBaseURL = normalizedUpstream
	if strings.TrimSpace(cfg.NativeUpstreamBaseURL) == "" {
		cfg.NativeUpstreamBaseURL = ""
	} else {
		normalizedNative, err := normalizeBaseURL("native_upstream_base_url", cfg.NativeUpstreamBaseURL)
		if err != nil {
			return err
		}
		cfg.NativeUpstreamBaseURL = normalizedNative
	}
	if cfg.AuthMode != "chatgpt" {
		return errors.New("auth_mode 目前必须是 chatgpt")
	}
	if cfg.ProxyMode != "disabled" && cfg.ProxyMode != "account" {
		return errors.New("proxy_mode 必须是 disabled 或 account")
	}
	if err := boundedInt("request_timeout_seconds", cfg.RequestTimeoutSeconds, 1, 600); err != nil {
		return err
	}
	if err := boundedInt("response_header_timeout_seconds", cfg.ResponseHeaderTimeoutSeconds, 1, 120); err != nil {
		return err
	}
	if err := boundedInt("idle_connection_timeout_seconds", cfg.IdleConnectionTimeoutSeconds, 1, 600); err != nil {
		return err
	}
	if err := boundedInt("max_idle_connections", cfg.MaxIdleConnections, 1, 1000); err != nil {
		return err
	}
	if err := boundedInt("max_idle_connections_per_host", cfg.MaxIdleConnectionsPerHost, 1, 100); err != nil {
		return err
	}
	if cfg.TLSMinVersion != "1.2" && cfg.TLSMinVersion != "1.3" {
		return errors.New("tls_min_version 必须是 1.2 或 1.3")
	}
	if len(cfg.ExtraHeaders) > maxExtraHeaders {
		return fmt.Errorf("extra_headers 最多允许 %d 个请求头", maxExtraHeaders)
	}
	for name, value := range cfg.ExtraHeaders {
		name = strings.TrimSpace(name)
		if name == "" || strings.ContainsAny(name, "\r\n") || !isValidHeaderName(name) {
			return fmt.Errorf("extra_headers 包含无效请求头名称: %q", name)
		}
		if len(value) > maxHeaderValue || strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("extra_headers[%q] 的值无效或过长", name)
		}
		if isProtectedHeader(name) {
			return fmt.Errorf("extra_headers 不允许覆盖受保护请求头: %s", name)
		}
	}
	if len(cfg.ModelMapping) > maxModelMapping {
		return fmt.Errorf("model_mapping 最多允许 %d 条映射", maxModelMapping)
	}
	normalizedMapping := make(map[string]string, len(cfg.ModelMapping))
	for requested, mapped := range cfg.ModelMapping {
		requested, mapped = strings.TrimSpace(requested), strings.TrimSpace(mapped)
		if requested == "" || mapped == "" {
			return errors.New("model_mapping 的键和值必须是非空模型名")
		}
		if len(requested) > maxModelName || len(mapped) > maxModelName {
			return errors.New("model_mapping 的模型名过长")
		}
		normalizedMapping[requested] = mapped
	}
	cfg.ModelMapping = normalizedMapping
	if cfg.BPSModelMode != BPSModelModeAll && cfg.BPSModelMode != BPSModelModeSelected {
		return errors.New("bps_model_mode 必须是 all 或 selected")
	}
	if len(cfg.BPSModels) > maxBPSModels {
		return fmt.Errorf("bps_models 最多允许 %d 个模型", maxBPSModels)
	}
	models := make([]string, 0, len(cfg.BPSModels))
	seen := make(map[string]bool, len(cfg.BPSModels))
	for _, model := range cfg.BPSModels {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > maxModelName || strings.IndexFunc(model, unicode.IsSpace) >= 0 || strings.ContainsAny(model, "*?") {
			return errors.New("bps_models 必须填写非空模型名，不得包含空白或通配符，每个模型名最多 128 字节")
		}
		if !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	cfg.BPSModels = models
	return nil
}

func normalizeBaseURL(name, raw string) (string, error) {
	normalized := strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%s 必须是无用户信息、无查询参数的绝对 URL", name)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("%s 仅支持 http 或 https", name)
	}
	return normalized, nil
}

func boundedInt(name string, value, min, max int) error {
	if value < min || value > max {
		return fmt.Errorf("%s 必须在 %d 到 %d 之间", name, min, max)
	}
	return nil
}

func isValidHeaderName(name string) bool {
	for _, r := range name {
		if r <= 0x20 || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={} \t", r) {
			return false
		}
	}
	return true
}

func isProtectedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "host", "content-length", "transfer-encoding", "connection",
		"chatgpt-account-id", "x-openai-account-id", "x-basispoints-auth-mode":
		return true
	default:
		return false
	}
}

func TLSVersion(value string) (uint16, error) {
	switch strings.TrimSpace(value) {
	case "1.2":
		return 0x0303, nil
	case "1.3":
		return 0x0304, nil
	default:
		return 0, fmt.Errorf("不支持的 TLS 版本: %s", strconv.Quote(value))
	}
}
