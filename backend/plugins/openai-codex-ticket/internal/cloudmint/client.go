// Package cloudmint implements the bounded JSON protocol used by the
// SUCK_MY_ASTRA relay mint endpoint.  It deliberately has no knowledge of the
// host/plugin lifecycle; callers decide when to cache, retry, or fail closed.
package cloudmint

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
	"golang.org/x/net/proxy"
)

const responseLimit = 256 << 10

type Credentials struct {
	AccessToken string
	AccountID   string
}

type Client struct {
	HTTP *http.Client
}

type response struct {
	Transport string                    `json:"transport"`
	Gateway   string                    `json:"gateway"`
	Cookies   map[string]string         `json:"cookies"`
	ExpiresAt time.Time                 `json:"expires_at"`
	Tickets   map[string]responseTicket `json:"tickets"`
}

type responseTicket struct {
	TurnState   string    `json:"turn_state"`
	TicketLen   int       `json:"ticket_len"`
	ServedModel string    `json:"served_model"`
	IssuedAt    time.Time `json:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func NewClient(proxyURL string) (*Client, error) {
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("invalid cloud mint proxy")
		}
		switch strings.ToLower(u.Scheme) {
		case "socks5", "socks5h":
			var auth *proxy.Auth
			if u.User != nil {
				password, _ := u.User.Password()
				auth = &proxy.Auth{User: u.User.Username(), Password: password}
			}
			dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("cloud mint proxy: %w", err)
			}
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialContext(ctx, dialer, network, address)
			}
		case "http", "https":
			transport.Proxy = http.ProxyURL(u)
		default:
			return nil, errors.New("cloud mint proxy must use http, https, socks5 or socks5h")
		}
	}
	return &Client{HTTP: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// dialContext keeps the proxy implementation context-aware without bringing a
// second SOCKS library into the plugin.
func dialContext(ctx context.Context, d proxy.Dialer, network, address string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		conn net.Conn
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		conn, err := d.Dial(network, address)
		resultCh <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		if err := ctx.Err(); err != nil {
			_ = result.conn.Close()
			return nil, err
		}
		return result.conn, nil
	}
}

// Mint performs one relay attempt. The caller supplies an already-resolved
// short-lived OAuth token and may pass only the route-cookie pair as seed.
func (c *Client) Mint(ctx context.Context, cfg config.CloudMintConfig, creds Credentials, model, relayKey, seedCookie string) (*ticket.Ticket, error) {
	if c == nil || c.HTTP == nil {
		return nil, errors.New("cloud mint client unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(relayKey) == "" {
		return nil, errors.New("cloud mint URL or relay key is empty")
	}
	if strings.TrimSpace(creds.AccessToken) == "" || strings.TrimSpace(model) == "" {
		return nil, errors.New("cloud mint credentials or model are empty")
	}
	if seedCookie != "" {
		seedPair, err := routecookie.ValidateSeed(seedCookie, cfg.Gateway, time.Now())
		if err != nil {
			return nil, err
		}
		// Relay seed material is deliberately reduced to the canonical route
		// pair.  The caller may have received a complete Cookie header from a
		// browser, but session/auth/analytics cookies must never cross the
		// plugin boundary into the mint service.
		seedCookie = seedPair.Header()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, nil)
	if err != nil {
		return nil, errors.New("invalid cloud mint URL")
	}
	req.Header.Set("X-Relay-Key", relayKey)
	req.Header.Set("X-Relay-Mint", cfg.Gateway)
	req.Header.Set("X-Mint-Gateway", cfg.Gateway)
	req.Header.Set("X-Mint-Model", model)
	req.Header.Set("X-Mint-Transport", cfg.Transport)
	req.Header.Set("X-Mint-Len", fmt.Sprintf("%d", cfg.TicketLength))
	req.Header.Set("X-Mint-TTL", fmt.Sprintf("%d", cfg.TTLSeconds))
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	if creds.AccountID != "" {
		req.Header.Set("Chatgpt-Account-Id", creds.AccountID)
	}
	if seedCookie != "" {
		req.Header.Set("Cookie", seedCookie)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("cloud mint unavailable or timed out")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, responseLimit+1))
	if err != nil || len(raw) > responseLimit {
		return nil, errors.New("cloud mint response invalid or too large")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloud mint rejected with status %d", res.StatusCode)
	}
	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, errors.New("cloud mint response is not JSON")
	}
	item, ok := decoded.Tickets[model]
	if !ok || decoded.Transport != cfg.Transport || item.ServedModel != model || item.TicketLen != len(item.TurnState) || len(item.TurnState) != cfg.TicketLength {
		return nil, errors.New("cloud ticket model, transport or length mismatch")
	}
	now := time.Now()
	shape, err := ticket.ParseState(item.TurnState)
	if err != nil || shape.IssuedAt.After(now.Add(30*time.Second)) {
		return nil, errors.New("cloud ticket envelope is invalid")
	}
	if item.ExpiresAt.IsZero() || decoded.ExpiresAt.IsZero() {
		return nil, errors.New("cloud ticket expiry is missing")
	}
	if cfg.Gateway != "any" {
		// A relay declaration is only trustworthy when both the declaration and
		// the embedded route credential agree with the configured target.  Do not
		// accept a declared gateway while silently ignoring an off-target cookie.
		if strings.TrimSpace(decoded.Gateway) != strings.TrimSpace(cfg.Gateway) {
			return nil, errors.New("cloud target gateway mismatch")
		}
	}
	issued := shape.IssuedAt
	expires := issued.Add(time.Duration(cfg.TTLSeconds) * time.Second)
	for _, candidate := range []time.Time{item.ExpiresAt, decoded.ExpiresAt} {
		if !candidate.After(now) {
			return nil, errors.New("cloud ticket or pair is expired")
		}
		if candidate.Before(expires) {
			expires = candidate
		}
	}
	pair, err := validateResponsePair(decoded.Cookies, cfg.Gateway, now)
	if err != nil {
		return nil, err
	}
	if cfg.Gateway != "any" && strings.TrimSpace(pair.Gateway) != strings.TrimSpace(cfg.Gateway) {
		return nil, errors.New("cloud route cookie gateway mismatch")
	}
	if decoded.Gateway != "" {
		pair.Gateway = decoded.Gateway
	}
	if !pair.ExpiresAt.IsZero() && pair.ExpiresAt.Before(expires) {
		expires = pair.ExpiresAt
	}
	if !expires.After(now.Add(time.Second)) {
		return nil, errors.New("cloud ticket or route pair is expired")
	}
	return &ticket.Ticket{
		State: item.TurnState, Length: len(item.TurnState), CapturedAt: now,
		IssuedAt: issued, ExpiresAt: expires, Transport: cfg.Transport,
		Gateway: pair.Gateway, HarvestCookies: []string{routecookie.CFLB + "=" + pair.Values[routecookie.CFLB], routecookie.OAILB + "=" + pair.Values[routecookie.OAILB]},
		HarvestCookiesAt: now, RoutePair: &pair,
	}, nil
}

func validateResponsePair(values map[string]string, expected string, now time.Time) (routecookie.Pair, error) {
	if values == nil {
		return routecookie.Pair{}, errors.New("cloud response missing route cookie pair")
	}
	seed := routecookie.CFLB + "=" + values[routecookie.CFLB] + "; " + routecookie.OAILB + "=" + values[routecookie.OAILB]
	pair, err := routecookie.ValidateSeed(seed, expected, now)
	if err != nil {
		return routecookie.Pair{}, errors.New("cloud response route cookie pair is invalid")
	}
	return pair, nil
}

func RelayKey(cfg config.CloudMintConfig) string {
	return strings.TrimSpace(os.Getenv(cfg.KeyEnv))
}
