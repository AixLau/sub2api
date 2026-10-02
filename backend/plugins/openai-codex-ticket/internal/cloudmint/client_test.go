package cloudmint

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/stretchr/testify/require"
)

func TestMintValidatesModelAndRoutePair(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := testState(now)
	oailb := jwtWithGateway(now.Add(20*time.Minute), "unified-88")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "secret", r.Header.Get("X-Relay-Key"))
		require.Equal(t, "__cflb=edge; __oailb="+oailb, r.Header.Get("Cookie"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"transport": "sse", "gateway": "unified-88",
			"cookies":    map[string]string{routecookie.CFLB: "edge", routecookie.OAILB: oailb},
			"expires_at": now.Add(20 * time.Minute),
			"tickets": map[string]any{"gpt-6-astra": map[string]any{
				"turn_state": state, "ticket_len": len(state), "served_model": "gpt-6-astra",
				"issued_at": now, "expires_at": now.Add(20 * time.Minute),
			}},
		})
	}))
	defer server.Close()
	cfg := config.CloudMintConfig{Enabled: true, URL: server.URL, Transport: "sse", Gateway: "any", TicketLength: 780, TTLSeconds: 240}
	client, err := NewClient("")
	require.NoError(t, err)
	pair := "session=secret; __cflb=edge; __oailb=" + oailb
	ticketValue, err := client.Mint(t.Context(), cfg, Credentials{AccessToken: "token", AccountID: "account"}, "gpt-6-astra", "secret", pair)
	require.NoError(t, err)
	require.Equal(t, state, ticketValue.State)
	require.Equal(t, "__cflb=edge; __oailb="+oailb, ticketValue.RouteCookieHeader())
}

func TestMintRejectsMissingPair(t *testing.T) {
	client, err := NewClient("")
	require.NoError(t, err)
	cfg := config.CloudMintConfig{Enabled: true, URL: "http://127.0.0.1:1", Transport: "sse", Gateway: "any", TicketLength: 780, TTLSeconds: 240}
	_, err = client.Mint(t.Context(), cfg, Credentials{AccessToken: "token"}, "gpt-6-astra", "secret", "__cflb=edge")
	require.Error(t, err)
	require.Contains(t, err.Error(), "route cookie")
}

func TestMintRequiresDeclaredAndCookieGatewayToMatch(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := testState(now)
	oailb := jwtWithGateway(now.Add(20*time.Minute), "unified-88")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"transport": "sse", "gateway": "unified-88",
			"cookies":    map[string]string{routecookie.CFLB: "edge", routecookie.OAILB: jwtWithGateway(now.Add(20*time.Minute), "unified-97")},
			"expires_at": now.Add(20 * time.Minute),
			"tickets": map[string]any{"gpt-6-astra": map[string]any{
				"turn_state": state, "ticket_len": len(state), "served_model": "gpt-6-astra", "issued_at": now, "expires_at": now.Add(20 * time.Minute),
			}},
		})
	}))
	defer server.Close()
	cfg := config.CloudMintConfig{Enabled: true, URL: server.URL, Transport: "sse", Gateway: "unified-88", TicketLength: 780, TTLSeconds: 240}
	client, err := NewClient("")
	require.NoError(t, err)
	_, err = client.Mint(t.Context(), cfg, Credentials{AccessToken: "token"}, "gpt-6-astra", "secret", "__cflb=edge; __oailb="+oailb)
	require.Error(t, err)
}

func testState(now time.Time) string {
	raw := make([]byte, 57+33*16)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(now.Unix()))
	for i := 9; i < len(raw); i++ {
		raw[i] = byte(i * 31)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func jwt(exp time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + formatUnix(exp.Unix()) + `}`))
	return "eyJhbGciOiJub25lIn0." + payload + ".sig"
}

func jwtWithGateway(exp time.Time, gateway string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + formatUnix(exp.Unix()) + `,"gateway":"` + gateway + `"}`))
	return "eyJhbGciOiJub25lIn0." + payload + ".sig"
}

func formatUnix(value int64) string {
	return strings.TrimSpace(jsonNumber(value))
}

func jsonNumber(value int64) string {
	return string(mustJSON(value))
}

func mustJSON(value int64) []byte {
	b, _ := json.Marshal(value)
	return b
}
