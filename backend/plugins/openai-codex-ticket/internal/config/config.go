package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Config controls Codex turn-state collection and injection. Values are kept
// in the plugin so the host never needs to understand ticket internals.
type Config struct {
	Enabled                      bool     `json:"enabled"`
	TargetLength                 int      `json:"target_length"`
	TTLSeconds                   int      `json:"ttl_seconds"`
	RefreshBeforeSeconds         int      `json:"refresh_before_seconds"`
	HarvestProbeIntervalSeconds  int      `json:"harvest_probe_interval_seconds"`
	HarvestCooldownSeconds       int      `json:"harvest_cooldown_seconds"`
	MaxProbesPerRound            int      `json:"max_probes_per_round"`
	HarvestAttemptTimeoutSeconds int      `json:"harvest_attempt_timeout_seconds"`
	Models                       []string `json:"models"`
	HarvestProxyURL              string   `json:"harvest_proxy_url"`
	HarvestProxyMode             string   `json:"harvest_proxy_mode"`
	HarvestProxyAPIURL           string   `json:"harvest_proxy_api_url"`
	FailClosed                   bool     `json:"fail_closed"`
	TargetGateway                string   `json:"target_gateway"`
	Transport                    string   `json:"transport"`
	TicketURL                    string   `json:"ticket_url"`
	HarvestURL                   string   `json:"harvest_url"`
	CookieValidation             bool     `json:"cookie_validation"`
	GatewayValidation            bool     `json:"gateway_validation"`
	TeamPlanBlocked              bool     `json:"team_plan_blocked"`
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
	return Config{Enabled: false, TargetLength: 780, TTLSeconds: 240,
		RefreshBeforeSeconds: 60, HarvestProbeIntervalSeconds: DefaultProbeIntervalSeconds,
		HarvestCooldownSeconds: DefaultCooldownSeconds, MaxProbesPerRound: DefaultMaxProbes,
		HarvestAttemptTimeoutSeconds: DefaultAttemptTimeoutSeconds,
		Models:                       []string{"gpt-6-astra", "gpt-5.6-sol"},
		TargetGateway:                DefaultTargetGateway, Transport: DefaultTransport, TicketURL: DefaultTicketURL,
		CookieValidation: true, GatewayValidation: true, HarvestProxyMode: "static"}
}

func (c Config) Normalized() Config {
	d := Defaults()
	if c.TargetLength == 0 {
		c.TargetLength = d.TargetLength
	}
	if c.TTLSeconds <= 0 {
		c.TTLSeconds = d.TTLSeconds
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
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	if c.Transport == "" {
		c.Transport = d.Transport
	}
	if strings.TrimSpace(c.TicketURL) == "" {
		c.TicketURL = d.TicketURL
	}
	if strings.TrimSpace(c.HarvestURL) == "" {
		c.HarvestURL = c.TicketURL
	}
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
	return c
}

func Parse(raw []byte) (Config, []byte, error) {
	if len(raw) == 0 {
		c := Defaults()
		b, _ := json.Marshal(c)
		return c, b, nil
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, nil, fmt.Errorf("invalid config: %w", err)
	}
	c = c.Normalized()
	if err := c.Validate(); err != nil {
		return Config{}, nil, err
	}
	b, err := json.Marshal(c)
	return c, b, err
}

func (c Config) Validate() error {
	if c.TargetLength != 292 && c.TargetLength != 332 && c.TargetLength != 780 {
		return errors.New("target_length must be 292, 332 or 780")
	}
	if c.TTLSeconds < 60 || c.TTLSeconds > 86400*7 {
		return errors.New("ttl_seconds must be between 60 and 604800")
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
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("ticket_url and harvest_url must be HTTPS URLs without credentials")
		}
	}
	if c.HarvestProxyURL != "" && c.HarvestProxyURL != "ip-pool" {
		if err := ValidateProxyURL(c.HarvestProxyURL); err != nil {
			return err
		}
	}
	if c.HarvestProxyMode != "static" && c.HarvestProxyMode != "api" && c.HarvestProxyMode != "pool" {
		return errors.New("harvest_proxy_mode must be static, api or pool")
	}
	if c.HarvestProxyMode == "api" {
		u, err := url.Parse(c.HarvestProxyAPIURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("harvest_proxy_api_url must be an HTTPS URL without credentials")
		}
	}
	return nil
}

func ValidateProxyURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("harvest proxy must be an HTTP(S) or SOCKS5(h) URL with a host and no path, query or fragment")
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h" {
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
