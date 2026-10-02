package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// CloudMintConfig describes the optional remote mint gateway. It is kept
// separate from the local harvester so a deployment can choose either path
// without changing the host transport.
type CloudMintConfig struct {
	Enabled      bool   `json:"enabled"`
	URL          string `json:"url"`
	ProxyURL     string `json:"proxy_url"`
	ProxyEnv     string `json:"proxy_env"`
	KeyEnv       string `json:"key_env"`
	Transport    string `json:"transport"`
	Gateway      string `json:"gateway"`
	TicketLength int    `json:"ticket_length"`
	TTLSeconds   int    `json:"ttl_seconds"`
	WaitMS       int    `json:"wait_ms"`
	TimeoutMS    int    `json:"timeout_ms"`
	FailClosed   bool   `json:"fail_closed"`
	MintModel    string `json:"mint_model"`
}

func defaultCloudMint() CloudMintConfig {
	return CloudMintConfig{
		KeyEnv: "CPA_RELAY_KEY", Transport: "sse", Gateway: "any", TicketLength: 780,
		TTLSeconds: 240, WaitMS: 2000, TimeoutMS: 90000, FailClosed: true,
		MintModel: "gpt-6-astra",
	}
}

func (c CloudMintConfig) normalized() CloudMintConfig {
	d := defaultCloudMint()
	if c.KeyEnv == "" {
		c.KeyEnv = d.KeyEnv
	}
	c.URL = strings.TrimSpace(c.URL)
	c.ProxyURL = strings.TrimSpace(c.ProxyURL)
	c.ProxyEnv = strings.TrimSpace(c.ProxyEnv)
	c.KeyEnv = strings.TrimSpace(c.KeyEnv)
	c.MintModel = strings.TrimSpace(c.MintModel)
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	c.Gateway = strings.ToLower(strings.TrimSpace(c.Gateway))
	if c.Transport == "" {
		c.Transport = d.Transport
	}
	if c.Gateway == "" {
		c.Gateway = d.Gateway
	}
	if c.TicketLength == 0 {
		c.TicketLength = d.TicketLength
	}
	if c.TTLSeconds == 0 {
		c.TTLSeconds = d.TTLSeconds
	}
	if c.WaitMS == 0 {
		c.WaitMS = d.WaitMS
	}
	if c.TimeoutMS == 0 {
		c.TimeoutMS = d.TimeoutMS
	}
	if c.MintModel == "" {
		c.MintModel = d.MintModel
	}
	return c
}

// Normalized applies the cloud-mint defaults without requiring callers to
// construct the parent plugin configuration.
func (c CloudMintConfig) Normalized() CloudMintConfig { return c.normalized() }

func (c CloudMintConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	c = c.normalized()
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("cloud_mint.url must be an HTTPS URL without credentials/query/fragment")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(scheme == "http" && loopback) {
		return errors.New("cloud_mint.url requires HTTPS (HTTP only on loopback)")
	}
	if c.ProxyURL != "" && c.ProxyEnv != "" {
		return errors.New("cloud_mint.proxy_url and proxy_env are mutually exclusive")
	}
	if c.ProxyURL == "ip-pool" {
		return errors.New("cloud_mint.proxy_url does not support ip-pool; use a concrete proxy or proxy_env")
	}
	if c.ProxyURL != "" {
		if err := ValidateProxyURL(c.ProxyURL); err != nil {
			return fmt.Errorf("cloud_mint: %w", err)
		}
	}
	if c.ProxyEnv != "" && !namePattern.MatchString(c.ProxyEnv) {
		return errors.New("cloud_mint.proxy_env is not a valid environment variable name")
	}
	if !namePattern.MatchString(c.KeyEnv) || !modelPattern.MatchString(c.MintModel) {
		return errors.New("cloud_mint.key_env or mint_model is invalid")
	}
	if c.Transport != "sse" && c.Transport != "websocket" {
		return errors.New("cloud_mint.transport must be sse or websocket")
	}
	if c.Gateway != "any" && !gatewayPattern.MatchString(c.Gateway) {
		return errors.New("cloud_mint.gateway is invalid")
	}
	if c.TicketLength < 1 || c.TicketLength > 4096 || c.TTLSeconds < 1 || c.TTLSeconds > 3600 || c.WaitMS < 1 || c.WaitMS > 10000 || c.TimeoutMS < c.WaitMS || c.TimeoutMS > 180000 {
		return errors.New("cloud_mint ticket or timing limits are invalid")
	}
	return nil
}

// Validate exposes the same strict checks used by Config.Parse to the
// standalone cloud-mint service.
func (c CloudMintConfig) Validate() error { return c.validate() }

var (
	namePattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,95}$`)
	modelPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	gatewayPattern = regexp.MustCompile(`^unified-[0-9]+$`)
)

// Config controls Codex turn-state collection and injection. Values are kept
// in the plugin so the host never needs to understand ticket internals.
type Config struct {
	Enabled                      bool            `json:"enabled"`
	CloudMint                    CloudMintConfig `json:"cloud_mint"`
	Role                         string          `json:"role"`
	DryRun                       bool            `json:"dry_run"`
	LogDecisions                 bool            `json:"log_decisions"`
	BlockDegraded                bool            `json:"block_degraded"`
	TemplateLength               int             `json:"template_length"`
	ReplaceLength                int             `json:"replace_length"`
	TargetLength                 int             `json:"target_length"`
	TTLSeconds                   int             `json:"ttl_seconds"`
	RouteCookieTTLSeconds        int             `json:"route_cookie_ttl_seconds"`
	RefreshBeforeSeconds         int             `json:"refresh_before_seconds"`
	HarvestProbeIntervalSeconds  int             `json:"harvest_probe_interval_seconds"`
	HarvestCooldownSeconds       int             `json:"harvest_cooldown_seconds"`
	MaxProbesPerRound            int             `json:"max_probes_per_round"`
	HarvestAttemptTimeoutSeconds int             `json:"harvest_attempt_timeout_seconds"`
	Models                       []string        `json:"models"`
	HarvestProxyURL              string          `json:"harvest_proxy_url"`
	HarvestProxyPool             []string        `json:"harvest_proxy_pool"`
	HarvestProxyMode             string          `json:"harvest_proxy_mode"`
	HarvestProxyAPIURL           string          `json:"harvest_proxy_api_url"`
	FailClosed                   bool            `json:"fail_closed"`
	TargetGateway                string          `json:"target_gateway"`
	Transport                    string          `json:"transport"`
	TicketURL                    string          `json:"ticket_url"`
	HarvestURL                   string          `json:"harvest_url"`
	CookieValidation             bool            `json:"cookie_validation"`
	GatewayValidation            bool            `json:"gateway_validation"`
	TeamPlanBlocked              bool            `json:"team_plan_blocked"`
}

const (
	DefaultTicketURL             = "https://chatgpt.com/backend-api/codex/responses"
	DefaultTargetGateway         = "unified-88"
	DefaultTransport             = "sse"
	DefaultTTLSeconds            = 240
	DefaultRefreshBeforeSeconds  = 60
	DefaultProbeIntervalSeconds  = 180
	DefaultCooldownSeconds       = 180
	DefaultMaxProbes             = 6
	DefaultAttemptTimeoutSeconds = 25
)

func Defaults() Config {
	return Config{Enabled: false, CloudMint: defaultCloudMint(), Role: "business", LogDecisions: true, BlockDegraded: false, TemplateLength: 292, ReplaceLength: 312, TargetLength: 780, TTLSeconds: 240, RouteCookieTTLSeconds: 3900,
		RefreshBeforeSeconds: 60, HarvestProbeIntervalSeconds: DefaultProbeIntervalSeconds,
		HarvestCooldownSeconds: DefaultCooldownSeconds, MaxProbesPerRound: DefaultMaxProbes,
		HarvestAttemptTimeoutSeconds: DefaultAttemptTimeoutSeconds,
		Models:                       []string{"gpt-6-astra", "gpt-5.6-sol"},
		TargetGateway:                DefaultTargetGateway, Transport: DefaultTransport, TicketURL: DefaultTicketURL,
		CookieValidation: true, GatewayValidation: true, HarvestProxyMode: "static"}
}

func (c Config) Normalized() Config {
	d := Defaults()
	if c.TemplateLength <= 0 {
		c.TemplateLength = d.TemplateLength
	}
	if c.ReplaceLength <= 0 {
		c.ReplaceLength = d.ReplaceLength
	}
	if c.TargetLength == 0 {
		c.TargetLength = d.TargetLength
	}
	if c.TTLSeconds <= 0 {
		c.TTLSeconds = d.TTLSeconds
	}
	if c.RouteCookieTTLSeconds <= 0 {
		c.RouteCookieTTLSeconds = d.RouteCookieTTLSeconds
	}
	if c.RefreshBeforeSeconds <= 0 {
		c.RefreshBeforeSeconds = d.RefreshBeforeSeconds
	}
	if c.HarvestProbeIntervalSeconds < 30 {
		c.HarvestProbeIntervalSeconds = d.HarvestProbeIntervalSeconds
	}
	if c.HarvestCooldownSeconds <= 0 {
		c.HarvestCooldownSeconds = d.HarvestCooldownSeconds
	}
	if c.MaxProbesPerRound <= 0 {
		c.MaxProbesPerRound = d.MaxProbesPerRound
	}
	if c.HarvestAttemptTimeoutSeconds <= 0 {
		c.HarvestAttemptTimeoutSeconds = d.HarvestAttemptTimeoutSeconds
	}
	if len(c.Models) == 0 {
		c.Models = append([]string(nil), d.Models...)
	}
	c.CloudMint = c.CloudMint.normalized()
	c.Role = strings.ToLower(strings.TrimSpace(c.Role))
	if c.Role == "" {
		c.Role = d.Role
	}
	seen := make(map[string]struct{}, len(c.Models))
	models := c.Models[:0]
	for _, m := range c.Models {
		m = strings.TrimSpace(m)
		if m != "" {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				models = append(models, m)
			}
		}
	}
	c.Models = models
	if strings.TrimSpace(c.TargetGateway) == "" {
		c.TargetGateway = d.TargetGateway
	}
	c.TargetGateway = strings.TrimSpace(c.TargetGateway)
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = d.Transport
	}
	if strings.TrimSpace(c.TicketURL) == "" {
		c.TicketURL = d.TicketURL
	}
	c.TicketURL = strings.TrimSpace(c.TicketURL)
	if strings.TrimSpace(c.HarvestURL) == "" {
		c.HarvestURL = c.TicketURL
	}
	c.HarvestURL = strings.TrimSpace(c.HarvestURL)
	c.HarvestProxyMode = strings.ToLower(strings.TrimSpace(c.HarvestProxyMode))
	if strings.EqualFold(strings.TrimSpace(c.HarvestProxyURL), "ip-pool") && c.HarvestProxyMode == "static" {
		c.HarvestProxyMode = "pool"
	}
	if c.HarvestProxyMode == "" {
		if c.HarvestProxyAPIURL != "" {
			c.HarvestProxyMode = "api"
		} else {
			c.HarvestProxyMode = "static"
		}
	}
	pool := make([]string, 0, len(c.HarvestProxyPool))
	seenProxy := make(map[string]struct{}, len(c.HarvestProxyPool))
	for _, raw := range c.HarvestProxyPool {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, seen := seenProxy[raw]; seen {
			continue
		}
		seenProxy[raw] = struct{}{}
		pool = append(pool, raw)
	}
	c.HarvestProxyPool = pool
	return c
}

func Parse(raw []byte) (Config, []byte, error) {
	if len(raw) == 0 {
		c := Defaults()
		b, _ := json.Marshal(c)
		return c, b, nil
	}
	// Decode over the complete default value so omitted booleans retain their
	// documented defaults (notably cookie/gateway validation, decision logging,
	// and Relay fail-closed).  Explicit false values still overwrite them.
	c := Defaults()
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, nil, fmt.Errorf("invalid config: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, nil, errors.New("invalid config: multiple JSON values")
		}
		return Config{}, nil, fmt.Errorf("invalid config trailing data: %w", err)
	}
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return Config{}, nil, err
	}
	b, err := json.Marshal(c)
	return c, b, err
}

func (c Config) Validate() error {
	if err := c.CloudMint.validate(); err != nil {
		return err
	}
	if c.TemplateLength < 1 || c.TemplateLength > 4096 || c.ReplaceLength < 1 || c.ReplaceLength > 4096 || c.TemplateLength == c.ReplaceLength {
		return errors.New("template_length and replace_length must be distinct values between 1 and 4096")
	}
	if c.TargetLength != 292 && c.TargetLength != 332 && c.TargetLength != 780 {
		return errors.New("target_length must be 292, 332 or 780")
	}
	if c.TTLSeconds < 60 || c.TTLSeconds > 86400*7 {
		return errors.New("ttl_seconds must be between 60 and 604800")
	}
	if c.RouteCookieTTLSeconds < 60 || c.RouteCookieTTLSeconds > 86400*7 {
		return errors.New("route_cookie_ttl_seconds must be between 60 and 604800")
	}
	if c.Role != "business" && c.Role != "probe" {
		return errors.New("role must be business or probe")
	}
	if c.RefreshBeforeSeconds < 0 || c.RefreshBeforeSeconds >= c.TTLSeconds {
		return errors.New("refresh_before_seconds must be less than ttl_seconds")
	}
	if c.HarvestProbeIntervalSeconds < 30 || c.HarvestCooldownSeconds < 0 || c.MaxProbesPerRound < 1 || c.MaxProbesPerRound > 1000 || c.HarvestAttemptTimeoutSeconds < 1 {
		return errors.New("invalid harvest timing or limits")
	}
	if c.Transport != "sse" && c.Transport != "websocket" {
		return errors.New("transport must be sse or websocket")
	}
	for _, raw := range []string{c.TicketURL, c.HarvestURL} {
		u, err := url.Parse(raw)
		if err != nil || strings.ToLower(u.Scheme) != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return errors.New("ticket_url and harvest_url must be HTTPS URLs without credentials")
		}
	}
	if c.HarvestProxyURL != "" && c.HarvestProxyURL != "ip-pool" {
		if err := ValidateProxyURL(c.HarvestProxyURL); err != nil {
			return err
		}
	}
	for _, proxyURL := range c.HarvestProxyPool {
		if err := ValidateProxyURL(proxyURL); err != nil {
			return fmt.Errorf("harvest_proxy_pool: %w", err)
		}
	}
	if c.HarvestProxyMode != "static" && c.HarvestProxyMode != "api" && c.HarvestProxyMode != "pool" {
		return errors.New("harvest_proxy_mode must be static, api or pool")
	}
	if c.HarvestProxyMode == "api" {
		c.HarvestProxyAPIURL = strings.TrimSpace(c.HarvestProxyAPIURL)
		u, err := url.Parse(c.HarvestProxyAPIURL)
		if err != nil || strings.ToLower(u.Scheme) != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return errors.New("harvest_proxy_api_url must be an HTTPS URL without credentials")
		}
	}
	if c.HarvestProxyMode == "pool" && len(c.HarvestProxyPool) == 0 {
		return errors.New("harvest_proxy_pool must contain at least one proxy when harvest_proxy_mode is pool")
	}
	return nil
}

func ValidateProxyURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("harvest proxy must be an HTTP(S) or SOCKS5(h) URL with a host and no path, query or fragment")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "socks5" && scheme != "socks5h" {
		return errors.New("harvest proxy scheme must be http, https, socks5 or socks5h")
	}
	return nil
}

func (c Config) Revision() string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ParseNumeric accepts JSON numbers or strings used by older admin clients.
func ParseNumeric(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return n
}
