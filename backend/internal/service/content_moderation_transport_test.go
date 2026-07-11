package service

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModerationTransportRejectsUnsafeConfiguration(t *testing.T) {
	resolver := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("93.184.216.34")}, nil }
	for _, tc := range []struct {
		name, endpoint string
		hosts          []string
	}{
		{"http", "http://api.example.com", []string{"api.example.com"}}, {"credentials", "https://u:p@api.example.com", []string{"api.example.com"}},
		{"missing allowlist", "https://api.example.com", nil}, {"host absent", "https://api.example.com", []string{"other.example.com"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newRestrictedModerationClientFactory(tc.hosts, resolver, nil).Client(tc.endpoint, time.Second)
			require.Error(t, err)
		})
	}
	for _, timeout := range []time.Duration{0, -time.Second} {
		_, err := newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, nil).Client("https://api.example.com", timeout)
		require.Error(t, err)
	}
}

func TestModerationTransportRejectsUnsafeAddressesAndMixedDNS(t *testing.T) {
	unsafe := []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.0.1", "169.254.1.1", "169.254.169.254", "224.0.0.1", "0.0.0.0",
		"100.64.0.1", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "240.0.0.1",
		"::1", "fc00::1", "fe80::1", "ff02::1", "::", "64:ff9b::7f00:1", "64:ff9b::a00:1", "2001:db8::1", "2001:2::1", "3fff::1", "5f00::1",
	}
	for _, raw := range unsafe {
		t.Run(raw, func(t *testing.T) {
			resolver := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP(raw)}, nil }
			_, err := newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, nil).Client("https://api.example.com", time.Second)
			require.Error(t, err)
		})
	}
	resolver := func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, nil
	}
	_, err := newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, nil).Client("https://api.example.com", time.Second)
	require.Error(t, err)
	resolver = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("2606:4700:4700::1111"), net.ParseIP("64:ff9b::c0a8:1")}, nil
	}
	_, err = newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, nil).Client("https://api.example.com", time.Second)
	require.Error(t, err)
}

func TestModerationTransportAcceptsPublicAddresses(t *testing.T) {
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "2001:4860:4860::8888"} {
		t.Run(raw, func(t *testing.T) {
			resolver := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP(raw)}, nil }
			_, err := newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, nil).Client("https://api.example.com", time.Second)
			require.NoError(t, err)
		})
	}
}

func TestModerationTransportPinsConstructionResolution(t *testing.T) {
	resolveCalls := 0
	resolver := func(context.Context, string) ([]net.IP, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		}
		return []net.IP{net.ParseIP("127.0.0.1")}, nil
	}
	var address string
	dial := func(_ context.Context, _ string, gotAddress string) (net.Conn, error) {
		address = gotAddress
		return nil, errors.New("stop after dial")
	}
	client, err := newRestrictedModerationClientFactory([]string{"api.example.com"}, resolver, dial).Client("https://api.example.com", time.Second)
	require.NoError(t, err)
	_, err = client.Get("https://api.example.com/v1/moderations")
	require.Error(t, err)
	transport := client.Transport.(*http.Transport)
	require.Equal(t, "api.example.com", transport.TLSClientConfig.ServerName)
	require.Equal(t, "93.184.216.34:443", address)
	require.Equal(t, 1, resolveCalls)
}

func TestModerationTransportRejectsRedirectsWithoutCredentialForwarding(t *testing.T) {
	client := &http.Client{}
	applyRestrictedModerationRedirectPolicy(client)
	req, err := http.NewRequest(http.MethodGet, "https://other.example/path", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer secret")
	err = client.CheckRedirect(req, []*http.Request{{URL: mustParseModerationURL(t, "https://api.example/start"), Header: http.Header{"Authorization": []string{"Bearer secret"}}}})
	require.Error(t, err)
	require.Empty(t, req.Header.Get("Authorization"))
}

func TestModerationTransportSharedFactoryContract(t *testing.T) {
	var _ RestrictedModerationClientFactory = NewRestrictedModerationClientFactory([]string{"api.example.com"})
	var _ RestrictedModerationClientFactory = NewModerationKeyTestClientFactory([]string{"api.example.com"})
	require.IsType(t, NewRestrictedModerationClientFactory(nil), NewModerationKeyTestClientFactory(nil))
	_ = tls.VersionTLS12
}

func mustParseModerationURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
