package harvest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

type Identity struct {
	Token            string
	Headers          map[string][]string
	ProxyURL         string
	ChatGPTAccountID string
	Email            string
}
type Harvester struct {
	Client *http.Client
	Config config.Config
}

func New(c config.Config) *Harvester {
	return &Harvester{Client: newClient(c.HarvestProxyURL), Config: c}
}

func newClient(proxyURL string) *http.Client {
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment}
	if proxyURL != "" && proxyURL != "ip-pool" {
		if u, err := url.Parse(proxyURL); err == nil {
			if strings.HasPrefix(u.Scheme, "socks5") {
				if d, e := proxy.SOCKS5("tcp", u.Host, nil, proxy.Direct); e == nil {
					tr.Proxy = nil
					tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) { return d.Dial(network, address) }
				}
			} else {
				tr.Proxy = http.ProxyURL(u)
			}
		}
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (h *Harvester) Harvest(ctx context.Context, id Identity, accountID int64, model string) (*ticket.Ticket, error) {
	if h == nil {
		return nil, errors.New("harvester unavailable")
	}
	c := h.Config.Normalized()
	if c.HarvestProxyMode == "api" {
		proxyURL, err := fetchProxy(ctx, c.HarvestProxyAPIURL)
		if err != nil {
			return nil, err
		}
		c.HarvestProxyURL = proxyURL
		h = &Harvester{Client: newClient(proxyURL), Config: c}
	}
	if c.HarvestProxyMode == "pool" {
		return nil, errors.New("harvest proxy pool requires a host-managed proxy lease")
	}
	if c.Transport == "websocket" {
		return h.harvestWebSocket(ctx, id, accountID, model)
	}
	return h.harvestSSE(ctx, id, accountID, model)
}

func fetchProxy(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxy API status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10+1))
	if err != nil {
		return "", err
	}
	if len(b) > 8<<10 || strings.ContainsAny(strings.TrimSpace(string(b)), "\r\n") {
		return "", errors.New("proxy API response must be one line and <= 8 KiB")
	}
	var obj struct {
		URL      string `json:"url"`
		ProxyURL string `json:"proxy_url"`
		Proxy    string `json:"proxy"`
	}
	value := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &obj) == nil {
		for _, v := range []string{obj.URL, obj.ProxyURL, obj.Proxy} {
			if strings.TrimSpace(v) != "" {
				value = strings.TrimSpace(v)
				break
			}
		}
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	if err := config.ValidateProxyURL(value); err != nil {
		return "", err
	}
	return value, nil
}

func baseHeaders(id Identity, model string) http.Header {
	h := make(http.Header)
	for k, v := range id.Headers {
		for _, x := range v {
			h.Add(k, x)
		}
	}
	if id.Token != "" && h.Get("Authorization") == "" {
		h.Set("Authorization", "Bearer "+id.Token)
	}
	if id.ChatGPTAccountID != "" && h.Get("ChatGPT-Account-Id") == "" {
		h.Set("ChatGPT-Account-Id", id.ChatGPTAccountID)
	}
	if h.Get("user-agent") == "" {
		h.Set("User-Agent", "Codex/0.153.4")
	}
	if h.Get("originator") == "" {
		h.Set("originator", "codex_cli_rs")
	}
	if strings.Contains(strings.ToLower(model), "gpt-6") && h.Get("version") == "" {
		h.Set("version", "0.153.4")
	}
	return h
}
func probeBody(model string) []byte {
	b, _ := json.Marshal(map[string]any{"model": model, "instructions": "Reply with a short acknowledgement.", "input": []any{map[string]any{"role": "user", "content": "ping"}}, "stream": true, "store": false})
	return b
}

func (h *Harvester) harvestSSE(ctx context.Context, id Identity, accountID int64, model string) (*ticket.Ticket, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(h.Config.HarvestAttemptTimeoutSeconds)*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.Config.HarvestURL, strings.NewReader(string(probeBody(model))))
	if err != nil {
		return nil, err
	}
	headers := baseHeaders(id, model)
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
	req.Header = headers
	resp, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("harvest upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if readErr != nil {
		return nil, readErr
	}
	hs := map[string][]string{}
	for k, v := range resp.Header {
		hs[k] = v
	}
	t, err := ticket.ParseHarvest(hs, raw, accountID, model, time.Now(), time.Duration(h.Config.TTLSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	t.Transport = "sse"
	t.HarvestProxyURL = h.Config.HarvestProxyURL
	t.Gateway = h.Config.TargetGateway
	t.HarvestCookies = append([]string(nil), resp.Header.Values("Set-Cookie")...)
	t.HarvestCookiesAt = time.Now()
	if sh := resp.Request.Header.Get("session_id"); sh != "" {
		t.HarvestSessionID = sh
	}
	return t, nil
}

func (h *Harvester) harvestWebSocket(ctx context.Context, id Identity, accountID int64, model string) (*ticket.Ticket, error) {
	u, err := url.Parse(h.Config.HarvestURL)
	if err != nil {
		return nil, err
	}
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	if u.Scheme == "" {
		return nil, errors.New("harvest_url must use http or https")
	}
	dialer := websocket.DefaultDialer
	header := baseHeaders(id, model)
	header.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	header.Set("Content-Type", "application/json")
	conn, resp, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("websocket harvest status %d", resp.StatusCode)
	}
	if err := conn.WriteJSON(map[string]any{"type": "response.create", "model": model, "input": []any{map[string]any{"role": "user", "content": "ping"}}, "instructions": "Reply with a short acknowledgement.", "stream": true, "store": false}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(h.Config.HarvestAttemptTimeoutSeconds) * time.Second)
	_ = conn.SetReadDeadline(deadline)
	var state string
	for i := 0; i < 32; i++ {
		_, b, e := conn.ReadMessage()
		if e != nil {
			break
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if v, ok := m["headers"].(map[string]any); ok {
			if s, ok := v["x-codex-turn-state"].(string); ok {
				state = s
			}
		}
		if s, ok := m["x-codex-turn-state"].(string); ok {
			state = s
		}
		if state == "" {
			if md, ok := m["response"].(map[string]any); ok {
				if hh, ok := md["headers"].(map[string]any); ok {
					if s, ok := hh["x-codex-turn-state"].(string); ok {
						state = s
					}
				}
			}
		}
		if state != "" {
			break
		}
	}
	if state == "" && resp != nil {
		state = resp.Header.Get("x-codex-turn-state")
	}
	if state == "" {
		return nil, errors.New("websocket harvest response missing x-codex-turn-state")
	}
	t := &ticket.Ticket{AccountID: accountID, Model: model, State: state, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Duration(h.Config.TTLSeconds) * time.Second), Transport: "websocket", Gateway: h.Config.TargetGateway, HarvestProxyURL: h.Config.HarvestProxyURL}
	if resp != nil {
		t.HarvestCookies = append([]string(nil), resp.Header.Values("Set-Cookie")...)
		t.HarvestCookiesAt = time.Now()
	}
	t.Normalize()
	return t, nil
}

// ConsumeSSEState is exported for tests and callers that already buffer a
// response body. It scans both response headers and metadata events.
func ConsumeSSEState(headers http.Header, body io.Reader) (string, error) {
	if s := headers.Get("x-codex-turn-state"); s != "" {
		return s, nil
	}
	scan := bufio.NewScanner(io.LimitReader(body, 2<<20))
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data:") {
			var v map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) == nil {
				if s, ok := v["x-codex-turn-state"].(string); ok && s != "" {
					return s, nil
				}
				if h, ok := v["headers"].(map[string]any); ok {
					if s, ok := h["x-codex-turn-state"].(string); ok && s != "" {
						return s, nil
					}
				}
			}
		}
	}
	return "", errors.New("x-codex-turn-state not found")
}
