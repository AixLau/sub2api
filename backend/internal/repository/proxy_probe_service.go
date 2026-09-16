package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewProxyExitInfoProber(cfg *config.Config) service.ProxyExitInfoProber {
	insecure := false
	allowPrivate := false
	validateResolvedIP := true
	maxResponseBytes := defaultProxyProbeResponseMaxBytes
	if cfg != nil {
		insecure = cfg.Security.ProxyProbe.InsecureSkipVerify
		allowPrivate = cfg.Security.URLAllowlist.AllowPrivateHosts
		validateResolvedIP = cfg.Security.URLAllowlist.Enabled
		if cfg.Gateway.ProxyProbeResponseReadMaxBytes > 0 {
			maxResponseBytes = cfg.Gateway.ProxyProbeResponseReadMaxBytes
		}
	}
	if insecure {
		log.Printf("[ProxyProbe] Warning: insecure_skip_verify is not allowed and will cause probe failure.")
	}
	// 构建探测 URL 列表：配置存在时覆盖内置默认列表。
	var configuredTargets []configuredProbeTarget
	if cfg != nil && len(cfg.Security.ProxyProbe.URLs) > 0 {
		configuredTargets = make([]configuredProbeTarget, 0, len(cfg.Security.ProxyProbe.URLs))
		for _, u := range cfg.Security.ProxyProbe.URLs {
			configuredTargets = append(configuredTargets, configuredProbeTarget{
				url:    u.URL,
				parser: u.Parser,
			})
		}
	}

	return &proxyProbeService{
		insecureSkipVerify:  insecure,
		allowPrivateHosts:   allowPrivate,
		validateResolvedIP:  validateResolvedIP,
		maxResponseBytes:    maxResponseBytes,
		configuredProbeURLs: configuredTargets,
	}
}

const (
	defaultProxyProbeTimeout          = 10 * time.Second
	defaultProxyTimezoneProbeTimeout  = 3 * time.Second
	defaultProxyProbeResponseMaxBytes = int64(1024 * 1024)
)

// probeURLs 按优先级排列的内置探测 URL 列表。
// 某些 AI API 专用代理只允许访问特定域名，因此需要多个备选。
var probeURLs = []configuredProbeTarget{
	{"http://ip-api.com/json/?lang=zh-CN", "ip-api"},
	{"https://ipwho.is/", "ipwhois"},
	{"https://ipapi.co/json/", "ipapi-co"},
	{"http://api64.ipify.org?format=json", "ipify"},
}

type configuredProbeTarget struct {
	url    string
	parser string
}

type proxyProbeService struct {
	insecureSkipVerify  bool
	allowPrivateHosts   bool
	validateResolvedIP  bool
	maxResponseBytes    int64
	configuredProbeURLs []configuredProbeTarget
}

func (s *proxyProbeService) ProbeProxy(ctx context.Context, proxyURL string) (*service.ProxyExitInfo, int64, error) {
	return s.probeProxy(ctx, proxyURL, false)
}

// ProbeProxyTimezone only accepts an IP together with a usable geographic
// timezone. IP-only providers cannot short-circuit this fallback chain.
func (s *proxyProbeService) ProbeProxyTimezone(ctx context.Context, proxyURL string) (*service.ProxyExitInfo, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultProxyTimezoneProbeTimeout)
	defer cancel()
	return s.probeProxy(ctx, proxyURL, true)
}

func (s *proxyProbeService) probeProxy(ctx context.Context, proxyURL string, requireTimezone bool) (*service.ProxyExitInfo, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	targets := s.configuredProbeURLs
	if len(targets) == 0 {
		targets = probeURLs
	}
	if requireTimezone {
		capable := make([]configuredProbeTarget, 0, len(targets))
		for _, target := range targets {
			switch target.parser {
			case "ip-api", "ipwhois", "ipapi-co":
				capable = append(capable, target)
			}
		}
		targets = capable
		if len(targets) == 0 {
			return nil, 0, fmt.Errorf("no timezone-capable probe URLs configured")
		}
	}
	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:           proxyURL,
		Timeout:            defaultProxyProbeTimeout,
		InsecureSkipVerify: s.insecureSkipVerify,
		ValidateResolvedIP: s.validateResolvedIP,
		AllowPrivateHosts:  s.allowPrivateHosts,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create proxy client: %w", err)
	}

	var lastErr error
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		attemptCtx := ctx
		cancel := func() {}
		if requireTimezone {
			// Divide the remaining overall budget among untried providers.
			// A stalled primary must leave time for BOTH backup endpoints.
			deadline, _ := ctx.Deadline() // Set by ProbeProxyTimezone.
			budget := time.Until(deadline) / time.Duration(len(targets)-i)
			attemptCtx, cancel = context.WithTimeout(ctx, budget)
		}
		info, latencyMs, err := s.probeWithURL(attemptCtx, client, target.url, target.parser)
		cancel()
		if err == nil && requireTimezone {
			if _, ipErr := netip.ParseAddr(info.IP); ipErr != nil {
				err = fmt.Errorf("probe returned an invalid exit IP")
			} else {
				info.Timezone = strings.TrimSpace(info.Timezone)
				if info.Timezone == "" || info.Timezone == "Local" {
					err = fmt.Errorf("probe returned no geographic timezone")
				} else if _, tzErr := time.LoadLocation(info.Timezone); tzErr != nil {
					err = fmt.Errorf("probe returned an invalid geographic timezone")
				}
			}
		}
		if err == nil {
			return info, latencyMs, nil
		}
		lastErr = err
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	return nil, 0, fmt.Errorf("all probe URLs failed, last error: %w", lastErr)
}

func (s *proxyProbeService) probeWithURL(ctx context.Context, client *http.Client, url string, parser string) (*service.ProxyExitInfo, int64, error) {
	startTime := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("proxy connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	latencyMs := time.Since(startTime).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		return nil, latencyMs, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	maxResponseBytes := s.maxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = defaultProxyProbeResponseMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, latencyMs, fmt.Errorf("failed to read response: %w", err)
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, latencyMs, fmt.Errorf("proxy probe response exceeds limit: %d", maxResponseBytes)
	}

	switch parser {
	case "ip-api":
		return s.parseIPAPI(body, latencyMs)
	case "ipwhois":
		return s.parseIPWhois(body, latencyMs)
	case "ipapi-co":
		return s.parseIPAPICo(body, latencyMs)
	case "ipify":
		return s.parseIPify(body, latencyMs)
	case "chatgpt-trace":
		return s.parseChatGPTTrace(body, latencyMs)
	default:
		return nil, latencyMs, fmt.Errorf("unknown parser: %s", parser)
	}
}

func (s *proxyProbeService) parseIPAPI(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var ipInfo struct {
		Status      string `json:"status"`
		Message     string `json:"message"`
		Query       string `json:"query"`
		City        string `json:"city"`
		Region      string `json:"region"`
		RegionName  string `json:"regionName"`
		Country     string `json:"country"`
		CountryCode string `json:"countryCode"`
		Timezone    string `json:"timezone"`
	}

	if err := json.Unmarshal(body, &ipInfo); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, latencyMs, fmt.Errorf("failed to parse response: %w (body: %s)", err, preview)
	}
	if strings.ToLower(ipInfo.Status) != "success" {
		if ipInfo.Message == "" {
			ipInfo.Message = "ip-api request failed"
		}
		return nil, latencyMs, fmt.Errorf("ip-api request failed: %s", ipInfo.Message)
	}

	region := ipInfo.RegionName
	if region == "" {
		region = ipInfo.Region
	}
	return &service.ProxyExitInfo{
		IP:          ipInfo.Query,
		City:        ipInfo.City,
		Region:      region,
		Country:     ipInfo.Country,
		CountryCode: ipInfo.CountryCode,
		Timezone:    ipInfo.Timezone,
	}, latencyMs, nil
}

func (s *proxyProbeService) parseIPify(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var result struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, latencyMs, fmt.Errorf("failed to parse ipify response: %w", err)
	}
	if result.IP == "" {
		return nil, latencyMs, fmt.Errorf("ipify: no IP found in response")
	}
	return &service.ProxyExitInfo{
		IP: result.IP,
	}, latencyMs, nil
}

// parseChatGPTTrace 解析 Cloudflare trace 端点（如 chatgpt.com/cdn-cgi/trace）的纯文本响应。
// 响应按行给出键值对，其中 ip= 为出口 IP，loc= 为国家代码。
func (s *proxyProbeService) parseChatGPTTrace(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var ip, loc string
	for _, line := range strings.Split(string(body), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "ip":
			ip = strings.TrimSpace(value)
		case "loc":
			loc = strings.TrimSpace(value)
		}
	}
	if ip == "" {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, latencyMs, fmt.Errorf("chatgpt-trace: no ip= found in response (body: %s)", preview)
	}
	info := &service.ProxyExitInfo{
		IP: ip,
	}
	if loc != "" {
		info.CountryCode = loc
	}
	return info, latencyMs, nil
}

// IPWhois documents timezone.id at https://ipwhois.io/documentation.
func (s *proxyProbeService) parseIPWhois(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var result struct {
		IP          string `json:"ip"`
		Success     bool   `json:"success"`
		Message     string `json:"message"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		Timezone    struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, latencyMs, fmt.Errorf("failed to parse ipwhois response: %w", err)
	}
	if !result.Success || strings.TrimSpace(result.IP) == "" {
		return nil, latencyMs, fmt.Errorf("ipwhois lookup failed: %s", result.Message)
	}
	return &service.ProxyExitInfo{
		IP: result.IP, City: result.City, Region: result.Region,
		Country: result.Country, CountryCode: result.CountryCode, Timezone: result.Timezone.ID,
	}, latencyMs, nil
}

// ipapi.co uses a top-level timezone and may report error=true even with HTTP 200.
// Response schema: https://ipapi.co/api/.
func (s *proxyProbeService) parseIPAPICo(body []byte, latencyMs int64) (*service.ProxyExitInfo, int64, error) {
	var result struct {
		IP          string `json:"ip"`
		Error       bool   `json:"error"`
		Reason      string `json:"reason"`
		City        string `json:"city"`
		Region      string `json:"region"`
		Country     string `json:"country"`
		CountryName string `json:"country_name"`
		CountryCode string `json:"country_code"`
		Timezone    string `json:"timezone"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, latencyMs, fmt.Errorf("failed to parse ipapi.co response: %w", err)
	}
	if result.Error || strings.TrimSpace(result.IP) == "" {
		return nil, latencyMs, fmt.Errorf("ipapi.co lookup failed: %s", result.Reason)
	}
	if result.CountryCode == "" {
		result.CountryCode = result.Country
	}
	return &service.ProxyExitInfo{
		IP: result.IP, City: result.City, Region: result.Region,
		Country: result.CountryName, CountryCode: result.CountryCode, Timezone: result.Timezone,
	}, latencyMs, nil
}
