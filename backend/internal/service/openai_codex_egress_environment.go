package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"golang.org/x/sync/singleflight"
)

const (
	codexEgressProbeTimeout   = 3 * time.Second
	codexEgressFailureBackoff = time.Minute
)

// Successful entries live for this process's lifetime, until the proxy's actual
// connection settings change. Dates must never be cached alongside locations.
type codexEgressTimezoneEntry struct {
	fingerprint [32]byte
	location    *time.Location
	err         error
	retryAt     time.Time
}

type codexEgressEnvironmentResolver struct {
	prober  ProxyExitInfoProber
	now     func() time.Time
	mu      sync.Mutex
	entries map[int64]*codexEgressTimezoneEntry // 0 is the server's direct egress
	flight  singleflight.Group
}

func (r *codexEgressEnvironmentResolver) currentTime() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *codexEgressEnvironmentResolver) resolve(ctx context.Context, account *Account) (*time.Location, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var proxyID int64
	proxyURL := ""
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
		if proxyID <= 0 || account.Proxy == nil || account.Proxy.ID != proxyID {
			return nil, errors.New("codex egress proxy unavailable")
		}
		proxyURL = account.Proxy.URL()
	}
	fingerprint := sha256.Sum256([]byte(proxyURL))
	lookup := func() (*time.Location, error, bool) {
		entry := r.entries[proxyID]
		if entry != nil && entry.fingerprint == fingerprint {
			if entry.location != nil || r.currentTime().Before(entry.retryAt) {
				return entry.location, entry.err, true
			}
		}
		return nil, nil, false
	}
	r.mu.Lock()
	location, err, found := lookup()
	r.mu.Unlock()
	if found {
		return location, err
	}
	result := r.flight.DoChan(fmt.Sprintf("%d:%x", proxyID, fingerprint), func() (any, error) {
		r.mu.Lock()
		if location, err, found := lookup(); found {
			r.mu.Unlock()
			return location, err
		}
		if r.entries == nil {
			r.entries = make(map[int64]*codexEgressTimezoneEntry)
		}
		entry := &codexEgressTimezoneEntry{fingerprint: fingerprint}
		r.entries[proxyID] = entry
		r.mu.Unlock()

		// One cancelled waiter must not cancel a probe shared by other requests.
		probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codexEgressProbeTimeout)
		defer cancel()
		location, err := r.probe(probeCtx, proxyURL)
		r.mu.Lock()
		// A slow probe for old connection settings cannot replace newer settings.
		if r.entries[proxyID] == entry {
			entry.location, entry.err = location, err
			if err != nil {
				entry.retryAt = r.currentTime().Add(codexEgressFailureBackoff)
			}
		}
		r.mu.Unlock()
		if err != nil {
			// Probe errors can contain URLs/credentials or response bodies. Only
			// report our fixed error categories, never the underlying probe error.
			slog.WarnContext(probeCtx, "Codex egress timezone unavailable; preserving environment", "proxy_id", proxyID, "reason", err.Error())
		}
		return location, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case value := <-result:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if value.Err != nil {
			return nil, value.Err
		}
		return value.Val.(*time.Location), nil
	}
}

func (r *codexEgressEnvironmentResolver) probe(ctx context.Context, proxyURL string) (*time.Location, error) {
	if r.prober == nil {
		return nil, errors.New("probe not configured")
	}
	info, _, err := r.prober.ProbeProxy(ctx, proxyURL)
	if err != nil {
		return nil, errors.New("exit probe failed")
	}
	if info == nil || strings.TrimSpace(info.Timezone) == "" {
		return nil, errors.New("exit timezone missing")
	}
	zone := strings.TrimSpace(info.Timezone)
	if zone == "Local" {
		return nil, errors.New("exit timezone invalid")
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, errors.New("exit timezone invalid")
	}
	return location, nil
}

// Only the selected message's last environment block is considered. A malformed
// latest block must not cause us to rewrite an older turn instead.
func latestCodexEnvironmentText(body []byte) (path, text string) {
	// The marker also matches JSON-escaped angle brackets (\u003c / \u003e).
	if !bytes.Contains(body, []byte("environment_context")) || !gjson.ValidBytes(body) {
		return "", ""
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return "", ""
	}
	items := input.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Get("role").String() != "user" {
			continue
		}
		if kind := item.Get("type"); kind.Exists() && kind.String() != "message" {
			continue
		}
		content := item.Get("content")
		if content.Type == gjson.String && strings.Contains(content.Str, "<environment_context") {
			return fmt.Sprintf("input.%d.content", i), content.Str
		}
		if !content.IsArray() {
			continue
		}
		parts := content.Array()
		for j := len(parts) - 1; j >= 0; j-- {
			part := parts[j]
			text := part.Get("text")
			if part.Get("type").String() == "input_text" && text.Type == gjson.String && strings.Contains(text.Str, "<environment_context") {
				return fmt.Sprintf("input.%d.content.%d.text", i, j), text.Str
			}
		}
	}
	return "", ""
}

// Locate the two complete, non-overlapping value spans within the last block.
// Fixed delimiters deliberately avoid interpreting the surrounding text as XML.
func codexEnvironmentValueSpans(text string) (dateStart, dateEnd, zoneStart, zoneEnd int, ok bool) {
	const open = "<environment_context>"
	const close = "</environment_context>"
	start := strings.LastIndex(text, "<environment_context")
	if start < 0 || !strings.HasPrefix(text[start:], open) {
		return
	}
	start += len(open)
	end := strings.Index(text[start:], close)
	if end < 0 {
		return
	}
	block := text[start : start+end]
	span := func(tag string) (int, int, bool) {
		op, cl := "<"+tag+">", "</"+tag+">"
		if strings.Count(block, op) != 1 || strings.Count(block, cl) != 1 {
			return 0, 0, false
		}
		a, b := strings.Index(block, op)+len(op), strings.Index(block, cl)
		if b < a || strings.ContainsAny(block[a:b], "<>") {
			return 0, 0, false
		}
		return start + a, start + b, true
	}
	dateStart, dateEnd, ok = span("current_date")
	if !ok {
		return
	}
	zoneStart, zoneEnd, ok = span("timezone")
	return
}

func (s *OpenAIGatewayService) rewriteCodexEgressEnvironment(ctx context.Context, c *gin.Context, account *Account, body []byte) ([]byte, error) {
	if account == nil || !account.IsOpenAI() || account.Type != AccountTypeOAuth || GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		return body, nil
	}
	path, text := latestCodexEnvironmentText(body)
	ds, de, zs, ze, ok := codexEnvironmentValueSpans(text)
	if !ok {
		return body, nil
	}
	location, err := s.codexEgressEnvironment.resolve(ctx, account)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return body, nil
	}
	date, zone := s.codexEgressEnvironment.currentTime().In(location).Format("2006-01-02"), location.String()
	updated := ""
	if ds < zs {
		updated = text[:ds] + date + text[de:zs] + zone + text[ze:]
	} else {
		updated = text[:zs] + zone + text[ze:ds] + date + text[de:]
	}
	if updated == text {
		return body, nil
	}
	// SetBytes allocates; never use ReplaceInPlace on the cross-account body.
	next, err := sjson.SetBytes(body, path, updated)
	if err != nil {
		slog.WarnContext(ctx, "Codex environment rewrite failed; preserving environment", "account_id", account.ID)
		return body, nil
	}
	return next, nil
}
