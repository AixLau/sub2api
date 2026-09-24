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
)

const (
	DefaultUpstreamBaseURL = "https://bps.openai.com/basispoints/api"
	DefaultAuthMode        = "chatgpt"
	DefaultProxyMode       = "account"
	DefaultTLSMinVersion   = "1.2"

	maxExtraHeaders = 32
	maxHeaderValue  = 8 * 1024
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
	}
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
	var cfg Config
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
}

func validate(cfg *Config) error {
	cfg.UpstreamBaseURL = strings.TrimRight(strings.TrimSpace(cfg.UpstreamBaseURL), "/")
	parsed, err := url.Parse(cfg.UpstreamBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("upstream_base_url 必须是无用户信息、无查询参数的绝对 URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("upstream_base_url 仅支持 http 或 https")
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
	return nil
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
