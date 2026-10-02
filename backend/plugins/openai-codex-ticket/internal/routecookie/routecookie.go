// Package routecookie owns the small, deliberately allow-listed cookie jar
// used by the Codex edge router.  These are routing credentials, not a
// general-purpose browser cookie jar: accepting any other cookie here would
// make it too easy to accidentally forward session or tracking material.
package routecookie

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	CFLB  = "__cflb"
	OAILB = "__oailb"
)

var gatewayPattern = regexp.MustCompile(`(?i)(unified[-_.]?\d+|gateway[-_.][a-z0-9-]+)`)

// Pair is a complete routing-cookie pair.  ExpiresAt is the earliest
// trustworthy deadline observed for either cookie; a JWT exp claim wins over
// the less precise Set-Cookie attributes because it is the credential's own
// expiry declaration.
type Pair struct {
	Values    map[string]string `json:"values"`
	SeenAt    time.Time         `json:"seen_at"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
	Gateway   string            `json:"gateway,omitempty"`
	Via       string            `json:"via,omitempty"`
	GoodAt    time.Time         `json:"good_at,omitempty"`
	BadAt     time.Time         `json:"bad_at,omitempty"`
}

func (p Pair) Clone() Pair {
	values := make(map[string]string, len(p.Values))
	for k, v := range p.Values {
		values[k] = v
	}
	p.Values = values
	return p
}

func (p Pair) Complete() bool {
	return p.Values[CFLB] != "" && p.Values[OAILB] != ""
}

// HasValue reports whether the set contains at least one allow-listed route
// cookie.  The reference runtime deliberately pools partial observations too:
// an edge may issue only one of the two routing cookies, and replaying that
// known value is still useful while the companion is learned later.  Complete
// remains the stricter predicate used for relay seeds and gateway validation.
func (p Pair) HasValue() bool {
	return p.Values[CFLB] != "" || p.Values[OAILB] != ""
}

func (p Pair) Usable(now time.Time, ttl time.Duration) bool {
	if !p.HasValue() || p.SeenAt.IsZero() || p.SeenAt.After(now) {
		return false
	}
	if ttl <= 0 || !now.Before(p.SeenAt.Add(ttl)) {
		return false
	}
	return p.ExpiresAt.IsZero() || now.Before(p.ExpiresAt)
}

func (p Pair) Header() string {
	if !p.HasValue() {
		return ""
	}
	return Header(p.Values)
}

// SetCookieLines returns a redacted, route-only snapshot suitable for ticket
// persistence and diagnostics. It intentionally drops attributes and every
// non-routing cookie; the host-owned session jar must never cross the plugin
// boundary or be written to plugin KV.
func SetCookieLines(p *Pair) []string {
	if p == nil || !p.HasValue() {
		return nil
	}
	lines := make([]string, 0, 2)
	for _, name := range []string{CFLB, OAILB} {
		if value := p.Values[name]; value != "" && safeValue(value) {
			lines = append(lines, name+"="+value)
		}
	}
	return lines
}

// Key identifies one route credential without exposing its values in logs or
// status payloads. It is stable across process restarts and safe as a KV key.
func (p Pair) Key() string { return Fingerprint(p.Values) }

// Score prefers a pair that has most recently produced a healthy response;
// before the first outcome, observation time is the neutral score.
func (p Pair) Score() time.Time {
	if p.GoodAt.After(p.SeenAt) {
		return p.GoodAt
	}
	return p.SeenAt
}

func (p Pair) Penalized(now time.Time, penalty time.Duration) bool {
	return !p.BadAt.IsZero() && penalty > 0 && now.Sub(p.BadAt) < penalty
}

func Header(values map[string]string) string {
	if values == nil {
		return ""
	}
	parts := []string{}
	for _, name := range []string{CFLB, OAILB} {
		if value := values[name]; value != "" {
			parts = append(parts, name+"="+value)
		}
	}
	return strings.Join(parts, "; ")
}

// MergeInto merges a complete or partial pair into an existing Cookie header.
// Existing non-route cookies are preserved verbatim; route names are replaced
// by the new values.  Invalid names/values are ignored rather than serialized.
func MergeInto(existing string, values map[string]string) string {
	if len(values) == 0 {
		return existing
	}
	type item struct{ name, value string }
	items := make([]item, 0, 8)
	positions := make(map[string]int, 8)
	for _, raw := range strings.Split(existing, ";") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		name, value, _ := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || !safeName(name) || !safeValue(value) {
			continue
		}
		if _, seen := positions[name]; seen {
			continue
		}
		positions[name] = len(items)
		items = append(items, item{name: name, value: value})
	}
	for _, name := range []string{CFLB, OAILB} {
		value := values[name]
		if value == "" || !safeValue(value) {
			continue
		}
		if at, ok := positions[name]; ok {
			items[at].value = value
		} else {
			positions[name] = len(items)
			items = append(items, item{name: name, value: value})
		}
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.name+"="+item.value)
	}
	return strings.Join(parts, "; ")
}

// ParseHeaders extracts only the allow-listed route cookies from Set-Cookie
// response headers. A deletion or an expired value is ignored. The returned
// Pair may be partial; callers decide whether a complete pair is required for
// their operation.
func ParseHeaders(headers http.Header, now time.Time) Pair {
	if now.IsZero() {
		now = time.Now()
	}
	pair := Pair{Values: map[string]string{}, SeenAt: now}
	for key, values := range headers {
		if !strings.EqualFold(key, "Set-Cookie") {
			continue
		}
		for _, line := range values {
			name, value, expiry, ok := ParseLine(line, now)
			if !ok || (name != CFLB && name != OAILB) {
				continue
			}
			if !expiry.IsZero() && !expiry.After(now) {
				// A deletion is represented by an empty value or an expired
				// deadline.  Do not let it poison a previously good pair.
				continue
			}
			pair.Values[name] = value
			if !expiry.IsZero() && (pair.ExpiresAt.IsZero() || expiry.Before(pair.ExpiresAt)) {
				pair.ExpiresAt = expiry
			}
		}
	}
	if !pair.Complete() {
		return pair
	}
	pair.Gateway = Gateway(pair.Values)
	return pair
}

// ParseLine parses one Set-Cookie line without accepting control characters or
// an unbounded value.  It is intentionally independent of net/http's cookie
// parser because only the two route names are valid here and JWT expiry is
// useful even when the upstream omits Max-Age/Expires.
func ParseLine(line string, now time.Time) (name, value string, expiry time.Time, ok bool) {
	segments := strings.Split(line, ";")
	if len(segments) == 0 {
		return "", "", time.Time{}, false
	}
	first := strings.TrimSpace(segments[0])
	name, value, found := strings.Cut(first, "=")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	name = canonicalRouteName(name)
	if !found || name == "" || !safeValue(value) {
		return "", "", time.Time{}, false
	}
	for _, raw := range segments[1:] {
		raw = strings.TrimSpace(raw)
		lower := strings.ToLower(raw)
		switch {
		case strings.HasPrefix(lower, "max-age="):
			seconds, err := strconv.ParseInt(strings.TrimSpace(raw[len("max-age="):]), 10, 64)
			if err == nil {
				candidate := maxAgeDeadline(now, seconds)
				if expiry.IsZero() || candidate.Before(expiry) {
					expiry = candidate
				}
			}
		case strings.HasPrefix(lower, "expires="):
			candidate, err := http.ParseTime(strings.TrimSpace(raw[len("expires="):]))
			if err == nil && (expiry.IsZero() || candidate.Before(expiry)) {
				expiry = candidate
			}
		}
	}
	if jwtExpiry := jwtExpiresAt(value); !jwtExpiry.IsZero() {
		// The signed route credential is authoritative when present.  The edge
		// commonly emits a short browser-cookie Max-Age while the JWT carries
		// the actual routing lifetime; matching the reference runtime avoids
		// prematurely discarding an otherwise valid pair.
		expiry = jwtExpiry
	}
	return name, value, expiry, true
}

func maxAgeDeadline(now time.Time, seconds int64) time.Time {
	const maxDuration = int64((1<<63 - 1) / int64(time.Second))
	if seconds > maxDuration {
		return now.Add(time.Duration(1<<63 - 1))
	}
	if seconds < -maxDuration {
		return now.Add(-time.Duration(1<<63 - 1))
	}
	return now.Add(time.Duration(seconds) * time.Second)
}

// HasDeletion reports whether a response explicitly invalidates either route
// cookie. Callers should discard the complete pair because the edge treats the
// two values as one routing credential.
func HasDeletion(headers http.Header, now time.Time) bool {
	for key, values := range headers {
		if !strings.EqualFold(key, "Set-Cookie") {
			continue
		}
		for _, line := range values {
			name, value, expiry, ok := ParseLine(line, now)
			if !ok || (name != CFLB && name != OAILB) {
				continue
			}
			if value == "" || (!expiry.IsZero() && !expiry.After(now)) {
				return true
			}
		}
	}
	return false
}

func Gateway(values map[string]string) string {
	for _, name := range []string{OAILB, CFLB} {
		if decoded := jwtPayload(values[name]); decoded != "" {
			if match := gatewayPattern.FindString(decoded); match != "" {
				return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(match, "_", "-"), ".", "-"))
			}
		}
		if match := gatewayPattern.FindString(values[name]); match != "" {
			return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(match, "_", "-"), ".", "-"))
		}
	}
	if len(values) == 0 {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(Header(values)))
	return fmt.Sprintf("lb-%08x", h.Sum32())
}

func GatewayAllowed(actual, expected string) bool {
	actual, expected = strings.TrimSpace(strings.ToLower(actual)), strings.TrimSpace(strings.ToLower(expected))
	return expected == "" || expected == "any" || actual == "" || actual == expected || strings.HasPrefix(actual, expected+"-")
}

func canonicalRouteName(name string) string {
	switch strings.ToLower(strings.TrimLeft(strings.TrimSpace(name), "_")) {
	case "cflb":
		return CFLB
	case "oailb":
		return OAILB
	default:
		return ""
	}
}

func Fingerprint(values map[string]string) string {
	value := Header(values)
	if value == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%08x", h.Sum32())
}

func safeName(value string) bool {
	return value != "" && len(value) <= 64 && !strings.ContainsAny(value, "=; \t\r\n,")
}

func safeValue(value string) bool {
	return len(value) <= 4096 && !strings.ContainsAny(value, "; \t\r\n,")
}

func jwtExpiresAt(value string) time.Time {
	payload := jwtPayload(value)
	if payload == "" {
		return time.Time{}
	}
	var claims struct {
		Exp *int64 `json:"exp"`
	}
	if json.Unmarshal([]byte(payload), &claims) != nil || claims.Exp == nil {
		return time.Time{}
	}
	return time.Unix(*claims.Exp, 0).UTC()
}

func jwtPayload(value string) string {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	return string(payload)
}

// ValidateSeed accepts only a complete, live route pair from an inbound
// Cookie header.  It is used before a cloud mint request so arbitrary cookies
// never cross the mint boundary.
func ValidateSeed(raw, expectedGateway string, now time.Time) (Pair, error) {
	values := map[string]string{}
	for _, segment := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(segment), "=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		name = canonicalRouteName(name)
		if name == "" {
			continue
		}
		if !ok || value == "" || !safeValue(value) || strings.IndexFunc(value, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			return Pair{}, errors.New("invalid route cookie pair")
		}
		if _, exists := values[name]; exists {
			return Pair{}, errors.New("duplicate route cookie")
		}
		values[name] = value
	}
	pair := Pair{Values: values, SeenAt: now, Gateway: Gateway(values)}
	if !pair.Complete() || !GatewayAllowed(pair.Gateway, expectedGateway) || !jwtExpiresAt(values[OAILB]).After(now) {
		return Pair{}, errors.New("incomplete, expired or off-target route cookie pair")
	}
	pair.ExpiresAt = jwtExpiresAt(values[OAILB])
	return pair, nil
}

// ParseCookieHeader extracts allow-listed route cookies from a normal request
// Cookie header (or from a relay response that serializes them in one string).
// Unlike ValidateSeed it permits a partial pair and does not require a JWT
// expiry claim; harvested test/relay responses can legitimately carry opaque
// edge values with a separate TTL.
func ParseCookieHeader(raw string, now time.Time) (Pair, error) {
	if now.IsZero() {
		now = time.Now()
	}
	values := map[string]string{}
	for _, segment := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(segment), "=")
		name, value = canonicalRouteName(name), strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if !ok || value == "" || !safeValue(value) || strings.IndexFunc(value, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
			return Pair{}, errors.New("invalid route cookie header")
		}
		if _, exists := values[name]; exists {
			return Pair{}, errors.New("duplicate route cookie")
		}
		values[name] = value
	}
	pair := Pair{Values: values, SeenAt: now, Gateway: Gateway(values)}
	if !pair.HasValue() {
		return Pair{}, errors.New("route cookie header is empty")
	}
	if expiry := jwtExpiresAt(values[OAILB]); !expiry.IsZero() {
		pair.ExpiresAt = expiry
	}
	return pair, nil
}

// SortedNames is useful for deterministic status output and tests.
func SortedNames(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
