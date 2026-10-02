package harvest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
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
	Client     *http.Client
	Config     config.Config
	proxyIndex atomic.Uint64
}

func New(c config.Config) *Harvester {
	return &Harvester{Client: newClient(c.HarvestProxyURL), Config: c}
}

func newClient(proxyURL string) *http.Client {
	// Harvest exits are explicit configuration.  In particular, an empty
	// harvest_proxy_url means a direct connection and must not silently inherit
	// the host process's HTTP(S)_PROXY environment (which belongs to neither
	// the account business route nor this plugin's probe scope).
	tr := &http.Transport{Proxy: nil}
	if proxyURL != "" && proxyURL != "ip-pool" {
		if u, err := url.Parse(proxyURL); err == nil {
			if strings.HasPrefix(strings.ToLower(u.Scheme), "socks5") {
				var auth *proxy.Auth
				if u.User != nil {
					password, _ := u.User.Password()
					auth = &proxy.Auth{User: u.User.Username(), Password: password}
				}
				if d, e := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct); e == nil {
					tr.Proxy = nil
					tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
						return dialProxyContext(ctx, d, network, address)
					}
				}
			} else {
				tr.Proxy = http.ProxyURL(u)
			}
		}
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func newWebSocketDialer(proxyURL string) (*websocket.Dialer, error) {
	dialer := *websocket.DefaultDialer
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" || proxyURL == "ip-pool" {
		return &dialer, nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(strings.ToLower(u.Scheme), "socks5") {
		var auth *proxy.Auth
		if u.User != nil {
			password, _ := u.User.Password()
			auth = &proxy.Auth{User: u.User.Username(), Password: password}
		}
		d, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		dialer.Proxy = nil
		dialer.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialProxyContext(ctx, d, network, address)
		}
		return &dialer, nil
	}
	dialer.Proxy = http.ProxyURL(u)
	return &dialer, nil
}

func dialProxyContext(ctx context.Context, d proxy.Dialer, network, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := d.Dial(network, address)
		ch <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-ch:
		return out.conn, out.err
	}
}

func (h *Harvester) Harvest(ctx context.Context, id Identity, accountID int64, model string) (*ticket.Ticket, error) {
	if h == nil {
		return nil, errors.New("harvester unavailable")
	}
	c := h.Config.Normalized()
	proxyURL, err := h.selectProxy(ctx, c)
	if err != nil {
		return nil, err
	}
	if proxyURL != c.HarvestProxyURL || c.HarvestProxyMode == "pool" || c.HarvestProxyMode == "api" {
		c.HarvestProxyURL = proxyURL
		next := &Harvester{Client: newClient(proxyURL), Config: c}
		next.proxyIndex.Store(h.proxyIndex.Load())
		h = next
	} else {
		client := h.Client
		if client == nil {
			client = newClient(c.HarvestProxyURL)
		}
		next := &Harvester{Client: client, Config: c}
		next.proxyIndex.Store(h.proxyIndex.Load())
		h = next
	}
	if c.Transport == "websocket" {
		return h.harvestWebSocket(ctx, id, accountID, model)
	}
	return h.harvestSSE(ctx, id, accountID, model)
}

func (h *Harvester) selectProxy(ctx context.Context, c config.Config) (string, error) {
	switch c.HarvestProxyMode {
	case "api":
		return fetchProxy(ctx, c.HarvestProxyAPIURL)
	case "pool":
		if len(c.HarvestProxyPool) == 0 {
			return "", errors.New("harvest proxy pool is empty")
		}
		index := h.proxyIndex.Add(1) - 1
		return c.HarvestProxyPool[index%uint64(len(c.HarvestProxyPool))], nil
	default:
		return c.HarvestProxyURL, nil
	}
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
	const maxHarvestBody = 2 << 20
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHarvestBody+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body can contain the OAuth account's ticket, cookies, or an
		// upstream diagnostic that echoes Authorization.  Keep the worker's
		// error surface bounded and secret-free; callers already record the
		// status and retry/cooldown decision.
		return nil, fmt.Errorf("harvest upstream status %d", resp.StatusCode)
	}
	if readErr != nil {
		return nil, readErr
	}
	if len(raw) > maxHarvestBody {
		return nil, errors.New("harvest response invalid or too large")
	}
	hs := map[string][]string{}
	for k, v := range resp.Header {
		hs[k] = v
	}
	now := time.Now()
	t, err := ticket.ParseHarvest(hs, raw, accountID, model, now, time.Duration(h.Config.TTLSeconds)*time.Second)
	if err != nil {
		state, stateErr := ConsumeSSEState(resp.Header, bytes.NewReader(raw))
		if stateErr != nil {
			return nil, err
		}
		t = &ticket.Ticket{AccountID: accountID, Model: model, State: state, CapturedAt: now, ExpiresAt: now.Add(time.Duration(h.Config.TTLSeconds) * time.Second)}
	}
	t.Transport = "sse"
	t.HarvestProxyURL = h.Config.HarvestProxyURL
	t.Gateway = h.Config.TargetGateway
	t.HarvestCookiesAt = now
	_ = t.MergeRouteCookies(resp.Header, now)
	for _, metadataHeaders := range scanSSEMetadataHeaders(raw) {
		_ = t.MergeRouteCookies(metadataHeaders, now)
	}
	t.HarvestCookies = routecookie.SetCookieLines(t.RoutePair)
	if sh := resp.Request.Header.Get("session_id"); sh != "" {
		t.HarvestSessionID = sh
	}
	return t, nil
}

func (h *Harvester) harvestWebSocket(ctx context.Context, id Identity, accountID int64, model string) (*ticket.Ticket, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(h.Config.HarvestAttemptTimeoutSeconds)*time.Second)
	defer cancel()
	u, err := url.Parse(h.Config.HarvestURL)
	if err != nil {
		return nil, err
	}
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	if u.Scheme == "" {
		return nil, errors.New("harvest_url must use http or https")
	}
	dialer, err := newWebSocketDialer(h.Config.HarvestProxyURL)
	if err != nil {
		return nil, err
	}
	header := baseHeaders(id, model)
	header.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	header.Set("Content-Type", "application/json")
	conn, resp, err := dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetReadLimit(2 << 20)
	if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("websocket harvest status %d", resp.StatusCode)
	}
	if err := conn.WriteJSON(map[string]any{"type": "response.create", "model": model, "input": []any{map[string]any{"role": "user", "content": "ping"}}, "instructions": "Reply with a short acknowledgement.", "stream": true, "store": false}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(time.Duration(h.Config.HarvestAttemptTimeoutSeconds) * time.Second)
	_ = conn.SetReadDeadline(deadline)
	var state string
	sawTerminal := false
	metadataResponseHeaders := make(http.Header)
	for i := 0; i < 32; i++ {
		_, b, e := conn.ReadMessage()
		if e != nil {
			break
		}
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		metadataHeaders := extractMetadataHeaders(m)
		for key, values := range metadataHeaders {
			for _, value := range values {
				metadataResponseHeaders.Add(key, value)
			}
		}
		if s := metadataHeaders.Get("x-codex-turn-state"); s != "" {
			state = s
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
		if strings.Contains(strings.ToLower(string(b)), "response.completed") || strings.Contains(strings.ToLower(string(b)), "response.done") {
			sawTerminal = true
		}
		if state != "" && sawTerminal {
			break
		}
	}
	if state == "" && resp != nil {
		state = resp.Header.Get("x-codex-turn-state")
	}
	if state == "" {
		return nil, errors.New("websocket harvest response missing x-codex-turn-state")
	}
	now := time.Now()
	t := &ticket.Ticket{AccountID: accountID, Model: model, State: state, CapturedAt: now, ExpiresAt: now.Add(time.Duration(h.Config.TTLSeconds) * time.Second), Transport: "websocket", Gateway: h.Config.TargetGateway, HarvestProxyURL: h.Config.HarvestProxyURL}
	if resp != nil {
		t.HarvestCookiesAt = now
		_ = t.MergeRouteCookies(resp.Header, now)
	}
	_ = t.MergeRouteCookies(metadataResponseHeaders, now)
	t.HarvestCookies = routecookie.SetCookieLines(t.RoutePair)
	t.Normalize()
	return t, nil
}

// ConsumeSSEState is exported for tests and callers that already buffer a
// response body. It scans both response headers and metadata events.
func ConsumeSSEState(headers http.Header, body io.Reader) (string, error) {
	if s := headers.Get("x-codex-turn-state"); s != "" {
		return s, nil
	}
	const maxHarvestBody = 2 << 20
	raw, err := io.ReadAll(io.LimitReader(body, maxHarvestBody+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxHarvestBody {
		return "", errors.New("harvest response invalid or too large")
	}
	scan := bufio.NewScanner(bytes.NewReader(raw))
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "data:") {
			var v map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &v) == nil {
				if h := extractMetadataHeaders(v); h.Get("x-codex-turn-state") != "" {
					return h.Get("x-codex-turn-state"), nil
				}
			}
		}
	}
	if err := scan.Err(); err != nil {
		return "", err
	}
	return "", errors.New("x-codex-turn-state not found")
}

func scanSSEMetadataHeaders(body []byte) []http.Header {
	scan := bufio.NewScanner(bytes.NewReader(body))
	scan.Buffer(make([]byte, 64<<10), 2<<20)
	var out []http.Header
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var value map[string]any
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &value) != nil {
			continue
		}
		if headers := extractMetadataHeaders(value); len(headers) > 0 {
			out = append(out, headers)
		}
	}
	return out
}

// extractMetadataHeaders accepts the response metadata shapes emitted by the
// Codex WebSocket protocol while returning only the two ticket/cookie headers.
func extractMetadataHeaders(root map[string]any) http.Header {
	out := make(http.Header)
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 6 {
			return
		}
		switch node := value.(type) {
		case map[string]any:
			for key, raw := range node {
				if strings.EqualFold(key, "x-codex-turn-state") || strings.EqualFold(key, "set-cookie") {
					values := []string{}
					switch typed := raw.(type) {
					case string:
						values = []string{typed}
					case []any:
						for _, item := range typed {
							if text, ok := item.(string); ok {
								values = append(values, text)
							}
						}
					}
					for _, item := range values {
						if strings.EqualFold(key, "x-codex-turn-state") {
							out.Set("x-codex-turn-state", item)
						} else {
							out.Add("Set-Cookie", item)
						}
					}
					continue
				}
				if strings.EqualFold(key, "headers") || strings.EqualFold(key, "response") || strings.EqualFold(key, "metadata") || strings.EqualFold(key, "codex.response.metadata") || strings.EqualFold(key, "data") || strings.EqualFold(key, "event") || strings.EqualFold(key, "payload") || strings.EqualFold(key, "response_metadata") {
					walk(raw, depth+1)
				}
			}
		case []any:
			for _, item := range node {
				walk(item, depth+1)
			}
		}
	}
	walk(root, 0)
	if out.Get("x-codex-turn-state") == "" && len(out.Values("Set-Cookie")) == 0 {
		return nil
	}
	return out
}
