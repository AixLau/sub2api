package service

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"
)

type RestrictedModerationClientFactory interface {
	Client(endpoint string, timeout time.Duration) (*http.Client, error)
}
type moderationResolver func(context.Context, string) ([]net.IP, error)
type moderationDialer func(context.Context, string, string) (net.Conn, error)

type restrictedModerationClientFactory struct {
	allowed map[string]struct{}
	resolve moderationResolver
	dial    moderationDialer
}

func NewRestrictedModerationClientFactory(allowedHosts []string) RestrictedModerationClientFactory {
	return newRestrictedModerationClientFactory(allowedHosts, nil, nil)
}
func NewModerationKeyTestClientFactory(allowedHosts []string) RestrictedModerationClientFactory {
	return NewRestrictedModerationClientFactory(allowedHosts)
}

func newRestrictedModerationClientFactory(allowedHosts []string, resolve moderationResolver, dial moderationDialer) *restrictedModerationClientFactory {
	allowed := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			allowed[host] = struct{}{}
		}
	}
	if resolve == nil {
		r := &net.Resolver{}
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			addrs, err := r.LookupIPAddr(ctx, host)
			ips := make([]net.IP, len(addrs))
			for i := range addrs {
				ips[i] = addrs[i].IP
			}
			return ips, err
		}
	}
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}
	return &restrictedModerationClientFactory{allowed: allowed, resolve: resolve, dial: dial}
}

func (f *restrictedModerationClientFactory) Client(endpoint string, timeout time.Duration) (*http.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, errors.New("moderation endpoint must be HTTPS")
	}
	if u.User != nil {
		return nil, errors.New("moderation endpoint credentials are forbidden")
	}
	host := strings.ToLower(u.Hostname())
	if len(f.allowed) == 0 {
		return nil, errors.New("moderation host allowlist is required")
	}
	if _, ok := f.allowed[host]; !ok {
		return nil, errors.New("moderation endpoint host is not allowed")
	}
	initial, err := f.resolve(context.Background(), host)
	if err != nil {
		return nil, fmt.Errorf("resolve moderation endpoint: %w", err)
	}
	initialSet, err := validatedModerationIPs(initial)
	if err != nil {
		return nil, err
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig, ForceAttemptHTTP2: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		current, err := f.resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		currentSet, err := validatedModerationIPs(current)
		if err != nil {
			return nil, err
		}
		if strings.Join(initialSet, ",") != strings.Join(currentSet, ",") {
			return nil, errors.New("moderation endpoint DNS changed after validation")
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		return f.dial(ctx, network, net.JoinHostPort(initialSet[0], port))
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	applyRestrictedModerationRedirectPolicy(client)
	return client, nil
}

func validatedModerationIPs(ips []net.IP) ([]string, error) {
	if len(ips) == 0 {
		return nil, errors.New("moderation endpoint resolved to no addresses")
	}
	out := make([]string, 0, len(ips))
	seen := map[string]struct{}{}
	for _, ip := range ips {
		if !isSafeModerationIP(ip) {
			return nil, fmt.Errorf("unsafe moderation endpoint address %v", ip)
		}
		value := ip.String()
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out, nil
}

func isSafeModerationIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicModerationPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var nonPublicModerationPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func applyRestrictedModerationRedirectPolicy(client *http.Client) {
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Authorization")
		req.Header.Del("Proxy-Authorization")
		return errors.New("moderation redirects are forbidden")
	}
}
