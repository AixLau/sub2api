package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	ipwhoisTimezoneResponse = `{"success":true,"ip":"203.0.113.1","city":"Los Angeles","region":"California","country":"United States","country_code":"US","timezone":{"id":"America/Los_Angeles"}}`
	ipapiCoTimezoneResponse = `{"ip":"2001:db8::1","city":"Tokyo","region":"Tokyo","country":"JP","country_name":"Japan","timezone":"Asia/Tokyo"}`
)

func TestProxyTimezoneParsers(t *testing.T) {
	s := &proxyProbeService{}
	who, latency, err := s.parseIPWhois([]byte(ipwhoisTimezoneResponse), 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), latency)
	require.Equal(t, "203.0.113.1", who.IP)
	require.Equal(t, "America/Los_Angeles", who.Timezone)
	require.Equal(t, "United States", who.Country)
	require.Equal(t, "US", who.CountryCode)
	require.Equal(t, "Los Angeles", who.City)
	require.Equal(t, "California", who.Region)

	co, latency, err := s.parseIPAPICo([]byte(ipapiCoTimezoneResponse), 24)
	require.NoError(t, err)
	require.Equal(t, int64(24), latency)
	require.Equal(t, "2001:db8::1", co.IP)
	require.Equal(t, "Asia/Tokyo", co.Timezone)
	require.Equal(t, "Japan", co.Country)
	require.Equal(t, "JP", co.CountryCode)
	require.Equal(t, "Tokyo", co.City)
	require.Equal(t, "Tokyo", co.Region)

	for _, body := range []string{`not json`, `{}`, `null`, `{"success":false,"ip":"203.0.113.1","message":"quota exceeded"}`, `{"success":true,"ip":""}`} {
		_, _, err := s.parseIPWhois([]byte(body), 0)
		require.Error(t, err, body)
	}
	for _, body := range []string{`not json`, `{}`, `null`, `{"error":true,"ip":"203.0.113.1","reason":"RateLimited"}`, `{"ip":""}`} {
		_, _, err := s.parseIPAPICo([]byte(body), 0)
		require.Error(t, err, body)
	}
}

func TestProxyTimezoneFallbackUsesSameEgress(t *testing.T) {
	for _, route := range []string{"proxy", "direct"} {
		for _, tc := range []struct {
			name      string
			primary   string
			backup    string
			wantZone  string
			wantPaths []string
		}{
			{"primary succeeds", `{"status":"success","query":"203.0.113.2","timezone":"Europe/Paris"}`, ipwhoisTimezoneResponse, "Europe/Paris", []string{"/primary"}},
			{"missing timezone", `{"status":"success","query":"203.0.113.2"}`, ipwhoisTimezoneResponse, "America/Los_Angeles", []string{"/primary", "/who"}},
			{"invalid timezone", `{"status":"success","query":"203.0.113.2","timezone":"invalid/zone"}`, ipwhoisTimezoneResponse, "America/Los_Angeles", []string{"/primary", "/who"}},
			{"local is not geographic", `{"status":"success","query":"203.0.113.2","timezone":"Local"}`, ipwhoisTimezoneResponse, "America/Los_Angeles", []string{"/primary", "/who"}},
			{"missing IP", `{"status":"success","timezone":"Europe/Paris"}`, ipwhoisTimezoneResponse, "America/Los_Angeles", []string{"/primary", "/who"}},
			{"second backup", `{"status":"fail","message":"rate limited"}`, `{"success":false,"message":"rate limited"}`, "Asia/Tokyo", []string{"/primary", "/who", "/co"}},
			{"backup missing timezone", `{}`, `{"success":true,"ip":"203.0.113.1"}`, "Asia/Tokyo", []string{"/primary", "/who", "/co"}},
			{"backup invalid timezone", `{}`, strings.Replace(ipwhoisTimezoneResponse, "America/Los_Angeles", "bad/zone", 1), "Asia/Tokyo", []string{"/primary", "/who", "/co"}},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				var mu sync.Mutex
				var paths []string
				srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					paths = append(paths, r.URL.Path)
					mu.Unlock()
					if route == "proxy" && r.Host != "egress-probe.invalid" {
						t.Errorf("unexpected probe target: %s", r.Host)
					}
					switch r.URL.Path {
					case "/primary":
						_, _ = io.WriteString(w, tc.primary)
					case "/who":
						_, _ = io.WriteString(w, tc.backup)
					case "/co":
						_, _ = io.WriteString(w, ipapiCoTimezoneResponse)
					default:
						t.Error("IP-only provider must not be called for timezone lookup")
					}
				}))
				defer srv.Close()
				baseURL, proxyURL := srv.URL, ""
				if route == "proxy" {
					baseURL, proxyURL = "http://egress-probe.invalid", srv.URL
				}
				s := &proxyProbeService{allowPrivateHosts: true, configuredProbeURLs: []configuredProbeTarget{
					{baseURL + "/iponly", "ipify"},
					{baseURL + "/primary", "ip-api"},
					{baseURL + "/trace", "chatgpt-trace"},
					{baseURL + "/who", "ipwhois"},
					{baseURL + "/co", "ipapi-co"},
				}}
				info, _, err := s.ProbeProxyTimezone(context.Background(), proxyURL)
				require.NoError(t, err)
				require.Equal(t, tc.wantZone, info.Timezone)
				mu.Lock()
				defer mu.Unlock()
				require.Equal(t, tc.wantPaths, paths)
			})
		}
	}
}

func TestProxyTimezoneDefaultProviderOrder(t *testing.T) {
	var mu sync.Mutex
	var hosts []string
	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTTPS backups use CONNECT through this same proxy. Rejecting CONNECT
		// avoids any real external traffic and still verifies endpoint selection.
		mu.Lock()
		hosts = append(hosts, r.Host)
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, _, err := (&proxyProbeService{allowPrivateHosts: true}).ProbeProxyTimezone(context.Background(), srv.URL)
	require.Error(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"ip-api.com", "ipwho.is:443", "ipapi.co:443"}, hosts)
}

func TestProxyTimezoneReservesBudgetForBothBackups(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.URL.Path != "/co" {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, ipapiCoTimezoneResponse)
	}))
	defer srv.Close()
	s := &proxyProbeService{allowPrivateHosts: true, configuredProbeURLs: []configuredProbeTarget{
		{srv.URL + "/primary", "ip-api"},
		{srv.URL + "/who", "ipwhois"},
		{srv.URL + "/co", "ipapi-co"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	info, _, err := s.ProbeProxyTimezone(ctx, "")
	require.NoError(t, err)
	require.NoError(t, ctx.Err())
	require.Equal(t, "Asia/Tokyo", info.Timezone)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"/primary", "/who", "/co"}, paths)
}

func TestProxyTimezoneCancellationAndUnsupportedTargets(t *testing.T) {
	started := make(chan struct{})
	srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started) // A second request would panic; cancellation must stop fallback.
		<-r.Context().Done()
	}))
	defer srv.Close()
	s := &proxyProbeService{allowPrivateHosts: true, configuredProbeURLs: []configuredProbeTarget{
		{srv.URL, "ip-api"}, {srv.URL, "ipwhois"}, {srv.URL, "ipapi-co"},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := s.ProbeProxyTimezone(ctx, ""); done <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	s.configuredProbeURLs = []configuredProbeTarget{{srv.URL, "ipify"}, {srv.URL, "chatgpt-trace"}}
	_, _, err := s.ProbeProxyTimezone(context.Background(), "")
	require.ErrorContains(t, err, "no timezone-capable")
	_, _, err = s.ProbeProxyTimezone(ctx, "")
	require.ErrorIs(t, err, context.Canceled)
}

func TestProxyTimezoneRejectsLastProviderErrors(t *testing.T) {
	for _, body := range []string{
		`{"error":true,"ip":"203.0.113.1","timezone":"Asia/Tokyo"}`,
		`{"ip":"203.0.113.1","timezone":""}`,
		`{"ip":"203.0.113.1","timezone":"Local"}`,
		`{"ip":"203.0.113.1","timezone":"invalid/zone"}`,
		`{"ip":"not-an-ip","timezone":"Asia/Tokyo"}`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := newLocalTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer srv.Close()
			s := &proxyProbeService{allowPrivateHosts: true, configuredProbeURLs: []configuredProbeTarget{{srv.URL, "ipapi-co"}}}
			info, _, err := s.ProbeProxyTimezone(context.Background(), "")
			require.Error(t, err)
			require.Nil(t, info)
		})
	}
}
