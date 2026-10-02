package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/cloudmint"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/harvest"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/store"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const (
	PluginID                  = "local.sub2api.openai-codex-ticket"
	PluginVersion             = "1.1.0"
	Capability                = "openai.oauth.codex_ticket_hook.v1"
	turnStateHeader           = "x-codex-turn-state"
	routeCookieHeader         = "x-codex-route-cookie"
	routeCookieSeedHeader     = "x-codex-route-cookie-seed"
	observedServedModelHeader = "x-sub2api-served-model"
	observedEventHeader       = "x-sub2api-response-event"
	observedCompleteHeader    = "x-sub2api-body-complete"
)

type Runtime struct {
	cfg       config.Config
	rev       string
	harvester *harvest.Harvester
	store     *store.Store
	kv        store.KV
	cloud     *cloudmint.Service
}
type Plugin struct {
	pluginv1.UnimplementedTransportPluginServer
	mu               sync.RWMutex
	state            atomic.Pointer[Runtime]
	brokerMu         sync.RWMutex
	broker           *plugin.GRPCBroker
	hostConn         *grpc.ClientConn
	host             pluginv1.HostServiceClient
	ctx              context.Context
	cancel           context.CancelFunc
	running          atomic.Bool
	probes           atomic.Uint64
	successes        atomic.Uint64
	failures         atomic.Uint64
	harvestDecisions atomic.Uint64
	steerDecisions   atomic.Uint64
	passDecisions    atomic.Uint64
	skipDecisions    atomic.Uint64
	lastError        atomic.Value
	lastProbe        atomic.Value
	events           probeEventRing
	cooldownMu       sync.Mutex
	cooldowns        map[string]time.Time
	routeMu          sync.Mutex
	routes           map[string]routeRequest
	obsMu            sync.Mutex
	obsPersistMu     sync.Mutex
	observed         map[string]semanticObservation
	obsRecent        []observationEvent
	obsRev           string
}

type routeRequest struct {
	fingerprint string
	seenAt      time.Time
}

type semanticObservation struct {
	AccountID          int64                     `json:"account_id"`
	Model              string                    `json:"model"`
	LastServed         string                    `json:"last_served,omitempty"`
	LastEvent          string                    `json:"last_event,omitempty"`
	LastAt             time.Time                 `json:"last_at"`
	Matches            int64                     `json:"matches"`
	Degradations       int64                     `json:"degradations"`
	NaturalNormal      int64                     `json:"natural_normal"`
	NaturalLimited     int64                     `json:"natural_limited"`
	NaturalOther       int64                     `json:"natural_other"`
	InjectedSilent     int64                     `json:"injected_silent"`
	InjectedNormal     int64                     `json:"injected_normal"`
	InjectedLimited    int64                     `json:"injected_limited"`
	InjectedOther      int64                     `json:"injected_other"`
	LastTicketLength   int                       `json:"last_ticket_length,omitempty"`
	LastTicketKind     string                    `json:"last_ticket_kind,omitempty"`
	LastTicketInjected bool                      `json:"last_ticket_injected,omitempty"`
	LastSignedKind     string                    `json:"last_signed_kind,omitempty"`
	LastSignedAt       time.Time                 `json:"last_signed_at,omitempty"`
	LastSignedInjected bool                      `json:"last_signed_injected,omitempty"`
	LastNaturalKind    string                    `json:"last_natural_kind,omitempty"`
	LastNaturalAt      time.Time                 `json:"last_natural_at,omitempty"`
	SignedLens         map[int]int               `json:"signed_lens,omitempty"`
	Hourly             []observationHour         `json:"hourly,omitempty"`
	Recent24h          semanticObservationCounts `json:"recent_24h,omitempty"`
}

type semanticObservationCounts struct {
	NaturalNormal   int64 `json:"natural_normal"`
	NaturalLimited  int64 `json:"natural_limited"`
	NaturalOther    int64 `json:"natural_other"`
	InjectedSilent  int64 `json:"injected_silent"`
	InjectedLimited int64 `json:"injected_limited"`
	InjectedNormal  int64 `json:"injected_normal"`
	InjectedOther   int64 `json:"injected_other"`
}

const observationStoreKey = "observations"

type observationDocument struct {
	Version   int                   `json:"version"`
	UpdatedAt time.Time             `json:"updated_at"`
	Items     []semanticObservation `json:"items"`
	Recent    []observationEvent    `json:"recent,omitempty"`
}

type observationHour struct {
	Hour string `json:"hour"`
	semanticObservationCounts
}

type observationEvent struct {
	At        time.Time `json:"at"`
	AccountID int64     `json:"account_id"`
	Model     string    `json:"model"`
	Length    int       `json:"length"`
	Injected  bool      `json:"injected"`
	Kind      string    `json:"kind"`
	Served    string    `json:"served,omitempty"`
}

const (
	observationHourMax   = 48
	observationRecentMax = 100
)

func observationKey(item semanticObservation) string {
	return fmt.Sprintf("%d\x00%s", item.AccountID, strings.TrimSpace(item.Model))
}

func (p *Plugin) ensureObservations(r *Runtime) {
	if p == nil || r == nil || r.kv == nil {
		return
	}
	p.obsMu.Lock()
	if p.obsRev == r.rev {
		p.obsMu.Unlock()
		return
	}
	p.obsMu.Unlock()
	raw, found, err := r.kv.Get(context.Background(), store.Namespace, observationStoreKey)
	if err != nil {
		return
	}
	// Mark the revision only after the read succeeds.  A transient host-KV
	// outage must not permanently suppress the next status/response reload.
	p.obsMu.Lock()
	p.obsRev = r.rev
	p.obsMu.Unlock()
	if !found || len(raw) == 0 {
		return
	}
	var doc observationDocument
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 {
		return
	}
	p.obsMu.Lock()
	if p.observed == nil {
		p.observed = make(map[string]semanticObservation)
	}
	for _, item := range doc.Items {
		if item.AccountID <= 0 || strings.TrimSpace(item.Model) == "" {
			continue
		}
		if len(p.observed) >= 256 {
			break
		}
		if item.SignedLens != nil {
			item.SignedLens = cloneLens(item.SignedLens)
		}
		item.Hourly = append([]observationHour(nil), item.Hourly...)
		p.observed[observationKey(item)] = item
	}
	if len(doc.Recent) > observationRecentMax {
		doc.Recent = doc.Recent[len(doc.Recent)-observationRecentMax:]
	}
	p.obsRecent = append([]observationEvent(nil), doc.Recent...)
	p.obsMu.Unlock()
}

func (p *Plugin) persistObservations(r *Runtime) {
	if p == nil || r == nil || r.kv == nil {
		return
	}
	// Serialize the snapshot as well as the KV write. If the write lock were
	// acquired only after taking a snapshot, a slower older snapshot could
	// overtake a newer one and roll persisted observations backwards.
	p.obsPersistMu.Lock()
	defer p.obsPersistMu.Unlock()
	p.obsMu.Lock()
	items := make([]semanticObservation, 0, len(p.observed))
	for _, item := range p.observed {
		if item.AccountID <= 0 || strings.TrimSpace(item.Model) == "" {
			continue
		}
		if item.SignedLens != nil {
			item.SignedLens = cloneLens(item.SignedLens)
		}
		item.Hourly = append([]observationHour(nil), item.Hourly...)
		items = append(items, item)
	}
	recent := append([]observationEvent(nil), p.obsRecent...)
	p.obsMu.Unlock()
	raw, err := json.Marshal(observationDocument{Version: 1, UpdatedAt: time.Now().UTC(), Items: items, Recent: recent})
	if err != nil {
		return
	}
	_ = r.kv.Set(context.Background(), store.Namespace, observationStoreKey, raw, 7*24*time.Hour)
}

func cloneLens(in map[int]int) map[int]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type probeEventRing struct {
	mu      sync.Mutex
	entries []map[string]any
}

func (p *Plugin) learnRoutePair(ctx context.Context, r *Runtime, pair *routecookie.Pair, now time.Time) {
	if r == nil || r.store == nil || pair == nil || !pair.HasValue() {
		return
	}
	if !pair.Usable(now, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second) {
		return
	}
	if err := r.store.MergeRoutePair(ctx, pair, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second); err != nil {
		p.recordError(err)
	}
}

func (p *Plugin) mergeRoutePool(ctx context.Context, r *Runtime, headers http.Header, now time.Time, fingerprint string) (changed bool) {
	if r == nil || r.store == nil {
		return false
	}
	pool, err := r.store.GetRoutePair(ctx)
	if err != nil {
		p.recordError(err)
		return false
	}
	if routecookie.HasDeletion(headers, now) {
		if strings.TrimSpace(fingerprint) == "" && pool != nil {
			fingerprint = pool.Key()
		}
		if fingerprint != "" {
			if err := r.store.DeleteRoutePair(ctx, fingerprint, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second); err != nil {
				p.recordError(err)
			}
			changed = true
		}
		// A deletion wins over any other Set-Cookie line in the same response.
		// Do not parse a stale companion issuance and re-add the credential we
		// just revoked.
		return changed
	}
	base := &ticket.Ticket{RoutePair: pool}
	if base.MergeRouteCookies(headers, now) {
		changed = true
	}
	if base.RoutePair != nil && base.RoutePair.HasValue() {
		p.learnRoutePair(ctx, r, base.RoutePair, now)
	}
	return changed
}

func (r *probeEventRing) add(event map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) >= 200 {
		copy(r.entries, r.entries[len(r.entries)-199:])
		r.entries = r.entries[:199]
	}
	r.entries = append(r.entries, event)
}

func (r *probeEventRing) snapshot() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, 0, len(r.entries))
	for _, entry := range r.entries {
		copyEntry := make(map[string]any, len(entry))
		for key, value := range entry {
			copyEntry[key] = value
		}
		out = append(out, copyEntry)
	}
	return out
}

func New() *Plugin {
	p := &Plugin{}
	p.cooldowns = make(map[string]time.Time)
	p.routes = make(map[string]routeRequest)
	p.observed = make(map[string]semanticObservation)
	c := config.Defaults()
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), harvester: harvest.New(c), store: store.New(nil), cloud: cloudmint.NewService()})
	return p
}

func (p *Plugin) rememberRouteRequest(requestID, fingerprint string) {
	requestID, fingerprint = strings.TrimSpace(requestID), strings.TrimSpace(fingerprint)
	if requestID == "" || fingerprint == "" {
		return
	}
	now := time.Now()
	p.routeMu.Lock()
	if p.routes == nil {
		p.routes = make(map[string]routeRequest)
	}
	if len(p.routes) >= 4096 {
		for key, entry := range p.routes {
			if now.Sub(entry.seenAt) > 15*time.Minute {
				delete(p.routes, key)
			}
		}
	}
	p.routes[requestID] = routeRequest{fingerprint: fingerprint, seenAt: now}
	p.routeMu.Unlock()
}

func (p *Plugin) recallRouteRequest(requestID string) string {
	return p.takeRouteRequest(requestID)
}

func (p *Plugin) peekRouteRequest(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	p.routeMu.Lock()
	entry, ok := p.routes[requestID]
	p.routeMu.Unlock()
	if !ok || time.Since(entry.seenAt) > 15*time.Minute {
		if ok {
			p.forgetRouteRequest(requestID)
		}
		return ""
	}
	return entry.fingerprint
}

func (p *Plugin) takeRouteRequest(requestID string) string {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ""
	}
	p.routeMu.Lock()
	entry, ok := p.routes[requestID]
	if ok {
		delete(p.routes, requestID)
	}
	p.routeMu.Unlock()
	if !ok || time.Since(entry.seenAt) > 15*time.Minute {
		return ""
	}
	return entry.fingerprint
}

func (p *Plugin) forgetRouteRequest(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	p.routeMu.Lock()
	delete(p.routes, requestID)
	p.routeMu.Unlock()
}

func (p *Plugin) markRouteOutcome(ctx context.Context, r *Runtime, fingerprint string, healthy bool) {
	if r == nil || r.store == nil || strings.TrimSpace(fingerprint) == "" {
		return
	}
	if err := r.store.MarkRoutePair(ctx, fingerprint, healthy, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second); err != nil {
		p.recordError(err)
	}
}

// markObservedRouteOutcome mirrors the reference route-pool feedback rule:
// normal/other signed states make a steered pair healthy, the configured
// replacement length penalizes it, and a silent response leaves the score
// unchanged.  HTTP authorization failures are explicit negative signals even
// when the response carries no turn-state header.
func (p *Plugin) markObservedRouteOutcome(ctx context.Context, r *Runtime, fingerprint string, statusCode int, state string) {
	if strings.TrimSpace(fingerprint) == "" {
		return
	}
	switch {
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		p.markRouteOutcome(ctx, r, fingerprint, false)
	case statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices:
		kind := classifyTicketLengthWithConfig(len(strings.TrimSpace(state)), r.cfg.TargetLength, r.cfg.TemplateLength, r.cfg.ReplaceLength)
		if kind == "silent" {
			return
		}
		p.markRouteOutcome(ctx, r, fingerprint, kind != "limited")
	}
}

func probeCooldownKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(model))
}

func (p *Plugin) inProbeCooldown(accountID int64, model string, now time.Time) bool {
	key := probeCooldownKey(accountID, model)
	p.cooldownMu.Lock()
	defer p.cooldownMu.Unlock()
	until, ok := p.cooldowns[key]
	if !ok {
		return false
	}
	if !until.After(now) {
		delete(p.cooldowns, key)
		return false
	}
	return true
}

func (p *Plugin) setProbeCooldown(accountID int64, model string, until time.Time) {
	key := probeCooldownKey(accountID, model)
	p.cooldownMu.Lock()
	if len(p.cooldowns) > 4096 {
		now := time.Now()
		for k, expiry := range p.cooldowns {
			if !expiry.After(now) {
				delete(p.cooldowns, k)
			}
		}
	}
	p.cooldowns[key] = until
	p.cooldownMu.Unlock()
}

func (p *Plugin) clearProbeCooldown(accountID int64, model string) {
	p.cooldownMu.Lock()
	delete(p.cooldowns, probeCooldownKey(accountID, model))
	p.cooldownMu.Unlock()
}

func teamPlanMetadata(meta map[string]any) bool {
	for _, key := range []string{"plan_type", "plan", "subscription_plan", "account_plan"} {
		value, _ := meta[key].(string)
		value = strings.ToLower(strings.TrimSpace(value))
		if strings.Contains(value, "team") || strings.Contains(value, "enterprise") || strings.Contains(value, "business") {
			return true
		}
	}
	return false
}
func (p *Plugin) SetHostBroker(b *plugin.GRPCBroker) {
	p.brokerMu.Lock()
	p.broker = b
	p.brokerMu.Unlock()
}
func (p *Plugin) hostClient() pluginv1.HostServiceClient {
	p.brokerMu.RLock()
	defer p.brokerMu.RUnlock()
	return p.host
}

func (p *Plugin) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: PluginID, PluginVersion: PluginVersion, ProtocolVersion: pluginv1.ProtocolVersion, TransportApiVersion: pluginv1.TransportAPIVersion, Capabilities: []string{Capability}}, nil
}
func (p *Plugin) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	r := p.state.Load()
	if r == nil {
		return &pluginv1.HealthResponse{Healthy: false, Message: "ticket runtime unavailable"}, nil
	}
	status := p.status(r)
	b, _ := json.Marshal(status)
	return &pluginv1.HealthResponse{Healthy: true, Message: "Codex ticket 插件已就绪", StatusJson: string(b)}, nil
}
func (p *Plugin) status(r *Runtime) map[string]any {
	p.ensureObservations(r)
	tickets := []ticket.Summary{}
	if r.store != nil {
		if ts, e := r.store.List(context.Background()); e == nil {
			for _, t := range ts {
				tickets = append(tickets, t.Summary(time.Now(), r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation))
			}
		}
	}
	ready, blocked := 0, 0
	for _, s := range tickets {
		if s.Ready {
			ready++
		} else if r.cfg.FailClosed && !s.Revoked {
			blocked++
		}
	}
	accountIDs := make(map[int64]struct{})
	for _, item := range tickets {
		accountIDs[item.AccountID] = struct{}{}
	}
	routeSummaries := make([]map[string]any, 0)
	if r.store != nil {
		if pairs, err := r.store.GetRoutePairs(context.Background()); err == nil {
			now := time.Now()
			for _, pair := range pairs {
				if pair == nil || !pair.HasValue() {
					continue
				}
				remaining := int64(0)
				if pair.Usable(now, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second) {
					deadline := pair.SeenAt.Add(time.Duration(r.cfg.RouteCookieTTLSeconds) * time.Second)
					if !pair.ExpiresAt.IsZero() && pair.ExpiresAt.Before(deadline) {
						deadline = pair.ExpiresAt
					}
					remaining = maxInt64(0, int64(time.Until(deadline).Seconds()))
				}
				routeSummaries = append(routeSummaries, map[string]any{"fingerprint": pair.Key(), "gateway": pair.Gateway, "remaining_seconds": remaining, "good_at": pair.GoodAt, "bad_at": pair.BadAt, "via": maskProxy(pair.Via)})
			}
		}
	}
	p.obsMu.Lock()
	observations := make([]semanticObservation, 0, len(p.observed))
	for _, item := range p.observed {
		item.Hourly = append([]observationHour(nil), item.Hourly...)
		item.Recent24h = rollupObservation(item, time.Now().UTC(), 24*time.Hour)
		observations = append(observations, item)
	}
	recentObservations := append([]observationEvent(nil), p.obsRecent...)
	p.obsMu.Unlock()
	return map[string]any{"enabled": r.cfg.Enabled, "revision": r.rev, "running": p.running.Load(), "role": r.cfg.Role, "dry_run": r.cfg.DryRun, "log_decisions": r.cfg.LogDecisions, "host_kv": p.hostClient() != nil, "probes": p.probes.Load(), "successes": p.successes.Load(), "failures": p.failures.Load(), "last_error": p.lastError.Load(), "last_probe": p.lastProbe.Load(), "accounts_total": len(accountIDs), "ready_tickets": ready, "blocked_tickets": blocked, "route_pair_count": len(routeSummaries), "route_pairs": routeSummaries, "observations": observations, "observation_recent": recentObservations, "counters": map[string]uint64{"harvest": p.harvestDecisions.Load(), "steer": p.steerDecisions.Load(), "pass": p.passDecisions.Load(), "skip": p.skipDecisions.Load()}, "config": map[string]any{"target_length": r.cfg.TargetLength, "template_length": r.cfg.TemplateLength, "replace_length": r.cfg.ReplaceLength, "ttl_seconds": r.cfg.TTLSeconds, "route_cookie_ttl_seconds": r.cfg.RouteCookieTTLSeconds, "refresh_before_seconds": r.cfg.RefreshBeforeSeconds, "models": r.cfg.Models, "transport": r.cfg.Transport, "target_gateway": r.cfg.TargetGateway, "fail_closed": r.cfg.FailClosed, "block_degraded": r.cfg.BlockDegraded, "harvest_proxy_url": maskProxy(r.cfg.HarvestProxyURL), "cloud_mint_enabled": r.cfg.CloudMint.Enabled}, "tickets": tickets, "events": p.events.snapshot()}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func maskProxy(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "ip-pool" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return "<unparsable proxy url>"
	}
	if u.User == nil {
		return u.String()
	}
	stripped := *u
	stripped.User = nil
	out := stripped.String()
	marker := u.Scheme + "://"
	if !strings.HasPrefix(out, marker) {
		return "<unparsable proxy url>"
	}
	return marker + "***@" + out[len(marker):]
}

func (p *Plugin) ValidateConfig(_ context.Context, req *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	if req == nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: "配置请求为空"}, nil
	}
	_, b, e := config.Parse(req.ConfigJson)
	if e != nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: e.Error()}, nil
	}
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: b}, nil
}
func (p *Plugin) ApplyConfig(_ context.Context, req *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	if req == nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: "配置请求为空"}, nil
	}
	c, _, e := config.Parse(req.ConfigJson)
	if e != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: e.Error()}, nil
	}
	p.apply(c)
	return &pluginv1.ApplyConfigResponse{Applied: true, Message: "配置已应用"}, nil
}
func (p *Plugin) apply(c config.Config) {
	p.mu.Lock()
	old := p.state.Load()
	r := &Runtime{cfg: c, rev: c.Revision(), harvester: harvest.New(c), store: store.New(nil), cloud: cloudmint.NewService()}
	if old != nil && old.store != nil {
		r.store = old.store
		r.kv = old.kv
	}
	if kv := p.hostKV(); kv != nil {
		r.store = store.New(kv)
		r.kv = kv
	}
	p.state.Store(r)
	p.mu.Unlock()
	// A config apply may also attach a newly connected host KV service. Force
	// one reload even when the normalized revision happens to be unchanged.
	p.obsMu.Lock()
	p.obsRev = ""
	p.obsMu.Unlock()
	// The worker captures its runtime and interval at startup. Restart it when
	// an enabled configuration changes so a new probe scope, proxy, transport,
	// or refresh policy takes effect immediately instead of waiting for a
	// process restart.
	if old != nil && old.cfg.Enabled && (old.rev != r.rev || !c.Enabled) {
		p.stopWorker()
	}
	if old != nil && old.cloud != nil {
		old.cloud.Close()
	}
	if c.Enabled {
		p.startWorker()
	}
}
func (p *Plugin) TestConfig(_ context.Context, req *pluginv1.TestConfigRequest) (*pluginv1.TestConfigResponse, error) {
	started := time.Now()
	if req == nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: "配置请求为空"}, nil
	}
	c, _, e := config.Parse(req.ConfigJson)
	if e != nil {
		return &pluginv1.TestConfigResponse{Success: false, Message: e.Error(), LatencyMs: time.Since(started).Milliseconds()}, nil
	}
	b, _ := json.Marshal(map[string]any{"enabled": c.Enabled, "harvest_url": c.HarvestURL, "proxy_configured": c.HarvestProxyURL != "", "network_probe": false})
	return &pluginv1.TestConfigResponse{Success: true, Message: "配置有效；实际采票使用绑定 OAuth 身份", LatencyMs: time.Since(started).Milliseconds(), StatusJson: string(b)}, nil
}

func (p *Plugin) PrepareOutbound(ctx context.Context, req *pluginv1.PrepareOutboundRequest) (*pluginv1.PrepareOutboundResponse, error) {
	if req == nil {
		return &pluginv1.PrepareOutboundResponse{Action: pluginv1.HeaderHookAction_HEADER_HOOK_ACTION_CONTINUE}, nil
	}
	pr := PrepareRequest{RequestID: req.RequestId, AccountID: req.AccountId, Model: req.Model, Transport: req.Transport, Platform: req.Platform, AccountType: req.AccountType, Headers: headerMap(req.Headers)}
	result, err := p.Prepare(ctx, pr)
	if err != nil {
		return nil, err
	}
	resp := &pluginv1.PrepareOutboundResponse{ReasonCode: result.ReasonCode, Message: result.Message, RetryAfterMs: result.RetryAfterMs}
	if result.Action == "REJECT" {
		resp.Action = pluginv1.HeaderHookAction_HEADER_HOOK_ACTION_REJECT
	} else {
		resp.Action = pluginv1.HeaderHookAction_HEADER_HOOK_ACTION_CONTINUE
	}
	resp.HeadersToSet = make(map[string]*pluginv1.HeaderValues, len(result.HeadersToSet))
	for k, values := range result.HeadersToSet {
		resp.HeadersToSet[k] = &pluginv1.HeaderValues{Values: append([]string(nil), values...)}
	}
	resp.HeadersToDelete = append([]string(nil), result.HeadersToDelete...)
	return resp, nil
}

func (p *Plugin) ObserveOutboundResponse(ctx context.Context, req *pluginv1.ObserveOutboundResponseRequest) (*pluginv1.ObserveOutboundResponseResponse, error) {
	if req == nil || req.AccountId <= 0 {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "INVALID_REQUEST"}, nil
	}
	r := p.state.Load()
	if r == nil || !r.cfg.Enabled {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "DISABLED"}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	p.ensureObservations(r)
	// Health is intentionally passive; response observations are the write path
	// for the bounded semantic document. Persist once on every admitted signal,
	// including a semantic-only event that has no ticket header.
	defer p.persistObservations(r)
	if req.Platform != "" && req.Platform != "openai" || req.AccountType != "" && req.AccountType != "oauth" {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "ACCOUNT_SCOPE"}, nil
	}
	if !contains(r.cfg.Models, req.Model) {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "MODEL_SCOPE"}, nil
	}
	semanticSignal := firstHeader(req.Headers, observedServedModelHeader) != ""
	requestFingerprint := p.peekRouteRequest(req.RequestId)
	if semanticSignal {
		requestFingerprint = p.takeRouteRequest(req.RequestId)
	}
	// The host uses the existing response observer RPC for a second, bounded
	// semantic signal once it has parsed an OpenAI body/event. Handle that signal
	// independently from ticket/cookie rotation: a response can be degraded
	// even when its turn-state and route pair are perfectly valid.
	if served := firstHeader(req.Headers, observedServedModelHeader); served != "" {
		eventType := firstHeader(req.Headers, observedEventHeader)
		complete := strings.EqualFold(firstHeader(req.Headers, observedCompleteHeader), "true")
		if strings.EqualFold(strings.TrimSpace(served), strings.TrimSpace(req.Model)) {
			p.markRouteOutcome(ctx, r, requestFingerprint, true)
			p.recordSemanticEvent(req.AccountId, req.Model, served, eventType, complete, false)
			return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "MODEL_MATCH"}, nil
		}
		// A served-model mismatch is a degraded reading even when the upstream
		// response did not rotate a ticket header. Feed it through the same
		// bounded length classifier as the reference runtime so natural and
		// injected degradation remain distinguishable in the observation feed.
		p.recordTicketObservation(req.AccountId, req.Model, r.cfg.ReplaceLength, r.cfg.TargetLength, r.cfg.TemplateLength, r.cfg.ReplaceLength, strings.TrimSpace(req.SentTicketState) != "")
		p.recordSemanticEvent(req.AccountId, req.Model, served, eventType, complete, true)
		p.markRouteOutcome(ctx, r, requestFingerprint, false)
		message := fmt.Sprintf("upstream served %s instead of requested %s", strings.TrimSpace(served), strings.TrimSpace(req.Model))
		if r.cfg.BlockDegraded {
			return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "DEGRADED_MODEL", Message: message}, nil
		}
		return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "MODEL_DEGRADED_OBSERVED", Message: message}, nil
	}
	now := time.Now()
	if r.store == nil {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "STORE_UNAVAILABLE"}, nil
	}
	old, _ := r.store.Get(ctx, req.AccountId, req.Model)
	responseHeaders := make(http.Header, len(req.Headers))
	for key, values := range req.Headers {
		if values != nil {
			responseHeaders[key] = append([]string(nil), values.Values...)
		}
	}
	// Route cookies are learned independently from ticket rotation. The edge
	// can keep a turn-state stable while issuing a fresh pair.
	poolChanged := p.mergeRoutePool(ctx, r, responseHeaders, now, requestFingerprint)
	if !semanticSignal && routecookie.HasDeletion(responseHeaders, now) {
		// A deletion is terminal for the request's route attribution even when a
		// replacement ticket state is present in the same response. Do not leave
		// a stale fingerprint around for a later observer call.
		p.forgetRouteRequest(req.RequestId)
	}
	routeChanged := poolChanged
	if old != nil {
		routeChanged = old.MergeRouteCookies(responseHeaders, now) || routeChanged
		if routeChanged {
			old.HarvestCookies = routecookie.SetCookieLines(old.RoutePair)
			old.HarvestCookiesAt = now
		}
	}
	state := firstHeader(req.Headers, turnStateHeader)
	p.markObservedRouteOutcome(ctx, r, requestFingerprint, int(req.StatusCode), state)
	if req.StatusCode == 401 || req.StatusCode == 403 {
		if !semanticSignal {
			p.forgetRouteRequest(req.RequestId)
		}
		if old != nil && req.SentTicketState != "" {
			if old.State == req.SentTicketState {
				old.Revoked = true
			} else if old.Standby != nil && old.Standby.State == req.SentTicketState {
				old.Standby.Revoked = true
			} else {
				return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "TICKET_NOT_ADMITTED"}, nil
			}
			_ = r.store.Put(ctx, old, time.Duration(r.cfg.TTLSeconds)*time.Second)
		}
		return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "TICKET_REVOKED", Message: "upstream rejected the ticket"}, nil
	}
	p.recordTicketObservation(req.AccountId, req.Model, len(state), r.cfg.TargetLength, r.cfg.TemplateLength, r.cfg.ReplaceLength, strings.TrimSpace(req.SentTicketState) != "")
	if state == "" {
		if old != nil && routeChanged {
			if err := r.store.Put(ctx, old, time.Duration(r.cfg.TTLSeconds)*time.Second); err != nil {
				p.recordError(err)
				return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "STORE_ERROR", Message: err.Error()}, nil
			}
			return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "ROUTE_COOKIES_UPDATED"}, nil
		}
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "NO_TICKET"}, nil
	}
	t := &ticket.Ticket{AccountID: req.AccountId, Model: req.Model, State: state, CapturedAt: now, ExpiresAt: now.Add(time.Duration(r.cfg.TTLSeconds) * time.Second), Transport: req.Transport, Gateway: r.cfg.TargetGateway}
	if old != nil {
		t.Identity = old.Identity
		t.HarvestProxyURL = old.HarvestProxyURL
		t.HarvestSessionID = old.HarvestSessionID
		t.HarvestCookies = old.HarvestCookies
		t.HarvestCookiesAt = old.HarvestCookiesAt
		t.Gateway = old.Gateway
		if old.RoutePair != nil {
			pair := old.RoutePair.Clone()
			t.RoutePair = &pair
		}
	}
	if cookies := headerValues(req.Headers, "set-cookie"); len(cookies) > 0 {
		t.HarvestCookiesAt = now
		if t.MergeRouteCookies(responseHeaders, now) && t.RoutePair != nil && t.RoutePair.HasValue() {
			p.learnRoutePair(ctx, r, t.RoutePair, now)
		}
		t.HarvestCookies = routecookie.SetCookieLines(t.RoutePair)
	}
	if req.SentTicketState != "" && req.SentTicketState == state && old != nil {
		if routeChanged || len(headerValues(req.Headers, "set-cookie")) > 0 {
			if err := r.store.Put(ctx, old, time.Duration(r.cfg.TTLSeconds)*time.Second); err != nil {
				p.recordError(err)
				return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "STORE_ERROR", Message: err.Error()}, nil
			}
			return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "COOKIES_UPDATED"}, nil
		}
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "UNCHANGED"}, nil
	}
	if err := t.Validate(now, r.cfg.TargetLength, req.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
		if old != nil && req.SentTicketState != "" {
			if old.State == req.SentTicketState {
				old.Revoked = true
			} else if old.Standby != nil && old.Standby.State == req.SentTicketState {
				old.Standby.Revoked = true
			} else {
				return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "INVALID_STATE_UNADMITTED"}, nil
			}
			_ = r.store.Put(ctx, old, time.Duration(r.cfg.TTLSeconds)*time.Second)
		}
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "INVALID_STATE", Message: err.Error()}, nil
	}
	if err := r.store.PutWithStandby(ctx, t, time.Duration(r.cfg.TTLSeconds)*time.Second, r.cfg.TargetLength, req.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
		p.recordError(err)
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "STORE_ERROR", Message: err.Error()}, nil
	}
	p.successes.Add(1)
	return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "ROTATED", Message: "ticket state observed"}, nil
}

func headerMap(in map[string]*pluginv1.HeaderValues) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		if v != nil {
			out[k] = append([]string(nil), v.Values...)
		}
	}
	return out
}
func firstHeader(in map[string]*pluginv1.HeaderValues, name string) string {
	for k, v := range in {
		if strings.EqualFold(k, name) && v != nil && len(v.Values) > 0 {
			return strings.TrimSpace(v.Values[0])
		}
	}
	return ""
}
func headerValues(in map[string]*pluginv1.HeaderValues, name string) []string {
	for k, v := range in {
		if strings.EqualFold(k, name) && v != nil {
			return append([]string(nil), v.Values...)
		}
	}
	return nil
}

func (p *Plugin) hostKV() store.KV {
	h := p.hostClient()
	if h == nil {
		return nil
	}
	return &hostKV{client: h}
}

type hostKV struct{ client pluginv1.HostServiceClient }

func (s *hostKV) Get(c context.Context, n, k string) ([]byte, bool, error) {
	r, e := s.client.KVGet(c, &pluginv1.KVGetRequest{Namespace: n, Key: k})
	if e != nil {
		return nil, false, e
	}
	return r.Value, r.Found, nil
}
func (s *hostKV) Set(c context.Context, n, k string, b []byte, ttl time.Duration) error {
	_, e := s.client.KVSet(c, &pluginv1.KVSetRequest{Namespace: n, Key: k, Value: b, TtlSeconds: int64(ttl / time.Second)})
	return e
}
func (s *hostKV) Delete(c context.Context, n, k string) error {
	_, e := s.client.KVDelete(c, &pluginv1.KVDeleteRequest{Namespace: n, Key: k})
	return e
}
func (s *hostKV) List(c context.Context, n, p string, l int) ([]string, error) {
	r, e := s.client.KVList(c, &pluginv1.KVListRequest{Namespace: n, KeyPrefix: p, Limit: int32(l)})
	if e != nil {
		return nil, e
	}
	return r.Keys, nil
}

func (p *Plugin) InitHostServices(ctx context.Context, req *pluginv1.InitHostServicesRequest) (*pluginv1.InitHostServicesResponse, error) {
	if req == nil || req.HostServiceId == 0 || req.HostServiceApiVersion < 1 {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "宿主服务标识无效"}, nil
	}
	p.brokerMu.RLock()
	b := p.broker
	p.brokerMu.RUnlock()
	if b == nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "宿主 broker 未注入"}, nil
	}
	conn, e := b.Dial(req.HostServiceId)
	if e != nil {
		return &pluginv1.InitHostServicesResponse{Ready: false, Message: "连接宿主服务失败"}, nil
	}
	p.brokerMu.Lock()
	old := p.hostConn
	p.hostConn = conn
	p.host = pluginv1.NewHostServiceClient(conn)
	p.brokerMu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	if kv := p.hostKV(); kv != nil {
		// Publish a new immutable runtime snapshot instead of mutating the
		// currently served one in place.  Prepare/Observe can run concurrently
		// with host-service initialization, so in-place store replacement would
		// otherwise race with readers and could lose an observation write.
		p.mu.Lock()
		current := p.state.Load()
		if current != nil {
			r := *current
			r.store = store.New(kv)
			r.kv = kv
			p.state.Store(&r)
		}
		p.mu.Unlock()
		p.obsMu.Lock()
		p.obsRev = ""
		p.obsMu.Unlock()
	}
	return &pluginv1.InitHostServicesResponse{Ready: true, Message: "宿主 KV/账号服务已连接"}, nil
}

func (p *Plugin) startWorker() {
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	ctx := p.ctx
	p.running.Store(true)
	p.mu.Unlock()
	go p.worker(ctx)
}
func (p *Plugin) stopWorker() {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.ctx = nil
	p.running.Store(false)
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (p *Plugin) worker(ctx context.Context) {
	r := p.state.Load()
	if r == nil {
		return
	}
	interval := time.Duration(r.cfg.HarvestProbeIntervalSeconds) * time.Second
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			p.probeRound(ctx)
			timer.Reset(interval)
		}
	}
}

func (p *Plugin) recordProbeEvent(accountID int64, model, phase, result string, attempt int, started time.Time, extra map[string]any) {
	event := map[string]any{
		"time":        time.Now().UTC().Format(time.RFC3339Nano),
		"account_id":  accountID,
		"model":       model,
		"phase":       phase,
		"result":      result,
		"attempt":     attempt,
		"duration_ms": time.Since(started).Milliseconds(),
	}
	for key, value := range extra {
		event[key] = value
	}
	p.events.add(event)
}

func (p *Plugin) recordSemanticEvent(accountID int64, requested, served, eventType string, complete, degraded bool) {
	key := fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(requested))
	p.obsMu.Lock()
	if p.observed == nil {
		p.observed = make(map[string]semanticObservation)
	}
	item := p.observed[key]
	if item.LastAt.IsZero() && len(p.observed) >= 256 {
		oldestKey := ""
		var oldest time.Time
		for candidate, existing := range p.observed {
			if oldestKey == "" || existing.LastAt.Before(oldest) {
				oldestKey, oldest = candidate, existing.LastAt
			}
		}
		if oldestKey != "" {
			delete(p.observed, oldestKey)
		}
	}
	item.AccountID = accountID
	item.Model = strings.TrimSpace(requested)
	item.LastServed = strings.TrimSpace(served)
	item.LastEvent = strings.TrimSpace(eventType)
	item.LastAt = time.Now().UTC()
	if degraded {
		item.Degradations++
	} else {
		item.Matches++
	}
	p.observed[key] = item
	if degraded {
		for i := len(p.obsRecent) - 1; i >= 0; i-- {
			event := &p.obsRecent[i]
			if event.AccountID == accountID && event.Model == strings.TrimSpace(requested) && event.Kind == "limited" {
				event.Served = strings.TrimSpace(served)
				break
			}
		}
	}
	p.obsMu.Unlock()
	p.events.add(map[string]any{
		"time":       time.Now().UTC().Format(time.RFC3339Nano),
		"account_id": accountID,
		"model":      strings.TrimSpace(requested),
		"phase":      "response",
		"result":     map[bool]string{false: "model_observed", true: "model_degraded"}[degraded],
		"served":     strings.TrimSpace(served),
		"event":      strings.TrimSpace(eventType),
		"complete":   complete,
	})
}

func classifyTicketLength(length int, target int) string {
	return classifyTicketLengthWithConfig(length, target, 292, 312)
}

func classifyTicketLengthWithConfig(length, target, template, replace int) string {
	switch {
	case length <= 0:
		return "silent"
	case length == target || length == template:
		return "normal"
	case length == replace:
		return "limited"
	default:
		return "other"
	}
}

func (p *Plugin) recordTicketObservation(accountID int64, model string, length, target, template, replace int, injected bool) {
	model = strings.TrimSpace(model)
	if accountID <= 0 || model == "" {
		return
	}
	kind := classifyTicketLengthWithConfig(length, target, template, replace)
	if kind == "silent" && !injected {
		// A naturally missing ticket is not evidence about the upstream's
		// envelope shape. Keep the event stream quiet just like the reference
		// runtime; an injected silent result is still operationally meaningful.
		return
	}
	key := fmt.Sprintf("%d\x00%s", accountID, model)
	p.obsMu.Lock()
	if p.observed == nil {
		p.observed = make(map[string]semanticObservation)
	}
	item := p.observed[key]
	if item.LastAt.IsZero() && len(p.observed) >= 256 {
		oldestKey := ""
		var oldest time.Time
		for candidate, existing := range p.observed {
			if oldestKey == "" || existing.LastAt.Before(oldest) {
				oldestKey, oldest = candidate, existing.LastAt
			}
		}
		if oldestKey != "" {
			delete(p.observed, oldestKey)
		}
	}
	item.AccountID = accountID
	item.Model = model
	now := time.Now().UTC()
	item.LastAt = now
	item.LastTicketLength = length
	item.LastTicketKind = kind
	item.LastTicketInjected = injected
	if item.SignedLens == nil {
		item.SignedLens = make(map[int]int, 8)
	}
	if kind == "other" && length > 0 && length != replace {
		item.SignedLens[length]++
		if len(item.SignedLens) > 8 {
			oldestLength, oldestCount := 0, 0
			for candidate, count := range item.SignedLens {
				if candidate == length {
					continue
				}
				if oldestCount == 0 || count < oldestCount || (count == oldestCount && candidate < oldestLength) {
					oldestLength, oldestCount = candidate, count
				}
			}
			if oldestCount > 0 {
				delete(item.SignedLens, oldestLength)
			}
		}
		if item.SignedLens[length] >= 2 {
			kind = "normal"
			item.LastTicketKind = kind
		}
	}
	if kind != "silent" {
		item.LastSignedKind = kind
		item.LastSignedAt = item.LastAt
		item.LastSignedInjected = injected
		if !injected {
			item.LastNaturalKind = kind
			item.LastNaturalAt = item.LastAt
		}
	}
	itemHour := item.hourSlot(now)
	*itemHour = addObservationCount(*itemHour, kind, injected)
	item.Recent24h = rollupObservation(item, now, 24*time.Hour)
	if injected {
		switch kind {
		case "silent":
			item.InjectedSilent++
		case "normal":
			item.InjectedNormal++
		case "limited":
			item.InjectedLimited++
		default:
			item.InjectedOther++
		}
	} else {
		switch kind {
		case "normal":
			item.NaturalNormal++
		case "limited":
			item.NaturalLimited++
		default:
			item.NaturalOther++
		}
	}
	p.observed[key] = item
	appendObservationEventLocked(p, accountID, model, length, injected, kind, "", now)
	p.obsMu.Unlock()
	p.events.add(map[string]any{
		"time":        time.Now().UTC().Format(time.RFC3339Nano),
		"account_id":  accountID,
		"model":       model,
		"phase":       "response",
		"result":      "ticket_observed",
		"state_bytes": length,
		"kind":        kind,
		"injected":    injected,
	})
}

func addObservationCount(counts semanticObservationCounts, kind string, injected bool) semanticObservationCounts {
	if injected {
		switch kind {
		case "silent":
			counts.InjectedSilent++
		case "normal":
			counts.InjectedNormal++
		case "limited":
			counts.InjectedLimited++
		default:
			counts.InjectedOther++
		}
		return counts
	}
	switch kind {
	case "normal":
		counts.NaturalNormal++
	case "limited":
		counts.NaturalLimited++
	default:
		counts.NaturalOther++
	}
	return counts
}

func (o *semanticObservation) hourSlot(now time.Time) *semanticObservationCounts {
	hour := now.UTC().Truncate(time.Hour).Format(time.RFC3339)
	for i := len(o.Hourly) - 1; i >= 0; i-- {
		if o.Hourly[i].Hour == hour {
			return &o.Hourly[i].semanticObservationCounts
		}
	}
	o.Hourly = append(o.Hourly, observationHour{Hour: hour})
	if len(o.Hourly) > observationHourMax {
		o.Hourly = o.Hourly[len(o.Hourly)-observationHourMax:]
	}
	return &o.Hourly[len(o.Hourly)-1].semanticObservationCounts
}

func addObservationCounts(dst *semanticObservationCounts, src semanticObservationCounts) {
	if dst == nil {
		return
	}
	dst.NaturalNormal += src.NaturalNormal
	dst.NaturalLimited += src.NaturalLimited
	dst.NaturalOther += src.NaturalOther
	dst.InjectedSilent += src.InjectedSilent
	dst.InjectedLimited += src.InjectedLimited
	dst.InjectedNormal += src.InjectedNormal
	dst.InjectedOther += src.InjectedOther
}

func rollupObservation(o semanticObservation, now time.Time, window time.Duration) semanticObservationCounts {
	cutoff := now.UTC().Add(-window)
	var out semanticObservationCounts
	for _, slot := range o.Hourly {
		at, err := time.Parse(time.RFC3339, slot.Hour)
		if err != nil || at.Before(cutoff) {
			continue
		}
		addObservationCounts(&out, slot.semanticObservationCounts)
	}
	return out
}

func appendObservationEventLocked(p *Plugin, accountID int64, model string, length int, injected bool, kind, served string, now time.Time) {
	if p == nil {
		return
	}
	p.obsRecent = append(p.obsRecent, observationEvent{At: now.UTC(), AccountID: accountID, Model: model, Length: length, Injected: injected, Kind: kind, Served: strings.TrimSpace(served)})
	if len(p.obsRecent) > observationRecentMax {
		p.obsRecent = p.obsRecent[len(p.obsRecent)-observationRecentMax:]
	}
}

func (p *Plugin) probeRound(ctx context.Context) {
	r := p.state.Load()
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil || !r.cfg.Enabled || r.store == nil || r.harvester == nil {
		return
	}
	h := p.hostClient()
	if h == nil {
		return
	}
	accs, e := h.ListAccounts(ctx, &pluginv1.ListAccountsRequest{Platform: "openai", AccountType: "oauth"})
	if e != nil {
		p.recordError(e)
		return
	}
	count := 0
	roundID := time.Now().UTC().Format("20060102T150405.000Z")
	for _, a := range accs.Accounts {
		if count >= r.cfg.MaxProbesPerRound {
			return
		}
		if !a.Schedulable || a.IsShadow {
			continue
		}
		meta := map[string]any{}
		_ = json.Unmarshal(a.MetadataJson, &meta)
		if v, ok := meta["codex_skip_harvest"].(bool); ok && v {
			continue
		}
		if r.cfg.TeamPlanBlocked && teamPlanMetadata(meta) {
			p.recordProbeEvent(a.Id, "", "harvest", "team_plan_skipped", count, time.Now(), nil)
			continue
		}
		for _, model := range r.cfg.Models {
			if count >= r.cfg.MaxProbesPerRound {
				return
			}
			now := time.Now()
			attemptStarted := now
			if p.inProbeCooldown(a.Id, model, now) {
				continue
			}
			old, _ := r.store.Get(ctx, a.Id, model)
			if old != nil {
				if _, ok := old.Select(now, r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); ok && !old.NeedsRefresh(now, time.Duration(r.cfg.RefreshBeforeSeconds)*time.Second) {
					continue
				}
			}
			p.probes.Add(1)
			p.harvestDecisions.Add(1)
			count++
			attempt := count
			baseEvent := map[string]any{"round_id": roundID, "transport": r.cfg.Transport}
			id, err := p.resolveIdentity(ctx, a.Id, meta)
			if err != nil {
				p.setProbeCooldown(a.Id, model, now.Add(time.Duration(r.cfg.HarvestCooldownSeconds)*time.Second))
				p.recordError(err)
				baseEvent["error"] = sanitizeEventError(err.Error())
				p.recordProbeEvent(a.Id, model, "harvest", "identity_unavailable", attempt, attemptStarted, baseEvent)
				continue
			}
			t, err := r.harvester.Harvest(ctx, id, a.Id, model)
			if err != nil {
				p.setProbeCooldown(a.Id, model, time.Now().Add(time.Duration(r.cfg.HarvestCooldownSeconds)*time.Second))
				p.recordError(err)
				baseEvent["error"] = sanitizeEventError(err.Error())
				p.recordProbeEvent(a.Id, model, "harvest", "harvest_failed", attempt, attemptStarted, baseEvent)
				continue
			}
			if t.RoutePair != nil && t.RoutePair.HasValue() {
				p.learnRoutePair(ctx, r, t.RoutePair, time.Now())
			}
			// The host's outbound identity RPC deliberately withholds email. Bind
			// tickets to the stable ChatGPT account id that is present in its
			// scoped headers; this remains verifiable on every outbound hook.
			identity := ticket.IdentityHash(id.ChatGPTAccountID, "")
			t.Identity = identity
			if err := t.Validate(time.Now(), r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
				p.setProbeCooldown(a.Id, model, time.Now().Add(time.Duration(r.cfg.HarvestCooldownSeconds)*time.Second))
				p.recordError(err)
				baseEvent["error"] = sanitizeEventError(err.Error())
				baseEvent["state_bytes"] = t.Length
				p.recordProbeEvent(a.Id, model, "validate", "invalid_state", attempt, attemptStarted, baseEvent)
				continue
			}
			if err := r.store.PutWithStandby(ctx, t, time.Duration(r.cfg.TTLSeconds)*time.Second, r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
				p.setProbeCooldown(a.Id, model, time.Now().Add(time.Duration(r.cfg.HarvestCooldownSeconds)*time.Second))
				p.recordError(err)
				baseEvent["error"] = sanitizeEventError(err.Error())
				baseEvent["state_bytes"] = t.Length
				p.recordProbeEvent(a.Id, model, "restore", "ticket_persistence_failed", attempt, attemptStarted, baseEvent)
				continue
			}
			p.successes.Add(1)
			p.clearProbeCooldown(a.Id, model)
			p.lastProbe.Store(time.Now().UTC().Format(time.RFC3339))
			baseEvent["state_bytes"] = t.Length
			baseEvent["cookie_count"] = len(t.HarvestCookies)
			p.recordProbeEvent(a.Id, model, "harvest", "ready", attempt, attemptStarted, baseEvent)
		}
	}
}

func sanitizeEventError(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = proxyCredentialPattern.ReplaceAllString(raw, "$1***@")
	if len(raw) > 300 {
		raw = raw[:300]
	}
	return raw
}

var proxyCredentialPattern = regexp.MustCompile(`(?i)((?:https?|socks5h?):\/\/)[^\s\/@]+@`)

func (p *Plugin) resolveIdentity(ctx context.Context, id int64, meta map[string]any) (harvest.Identity, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	h := p.hostClient()
	if h == nil {
		return harvest.Identity{}, errors.New("host service unavailable")
	}
	r, e := h.ResolveOutboundIdentity(ctx, &pluginv1.ResolveOutboundIdentityRequest{AccountId: id})
	if e != nil || r == nil || !r.Found {
		if e == nil {
			e = errors.New("account identity not found")
		}
		return harvest.Identity{}, e
	}
	headers := map[string][]string{}
	for k, v := range r.Headers {
		headers[k] = append([]string(nil), v.Values...)
	}
	accountID := headers["ChatGPT-Account-Id"]
	if len(accountID) == 0 {
		accountID = headers["chatgpt-account-id"]
	}
	email, _ := meta["email"].(string)
	return harvest.Identity{Token: r.Token, Headers: headers, ProxyURL: r.ProxyUrl, ChatGPTAccountID: first(accountID), Email: email}, nil
}
func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}
func (p *Plugin) recordError(e error) {
	if e != nil {
		p.failures.Add(1)
		p.lastError.Store(sanitizeEventError(e.Error()))
	}
}

// PrepareRequest/PrepareResponse are the stable, capability-local shape used
// by the generated PrepareOutbound adapter. Keeping policy here lets the host
// enforce the only permitted mutation (x-codex-turn-state).
type PrepareRequest struct {
	RequestID   string
	AccountID   int64
	Model       string
	Transport   string
	Platform    string
	AccountType string
	Headers     map[string][]string
}
type PrepareResponse struct {
	Action          string
	HeadersToSet    map[string][]string
	HeadersToDelete []string
	ReasonCode      string
	Message         string
	RetryAfterMs    int64
}

func (p *Plugin) Prepare(ctx context.Context, req PrepareRequest) (PrepareResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r := p.state.Load()
	if r == nil || !r.cfg.Enabled {
		return PrepareResponse{Action: "CONTINUE"}, nil
	}
	if req.Platform != "" && req.Platform != "openai" || req.AccountType != "" && req.AccountType != "oauth" || req.AccountID <= 0 {
		return PrepareResponse{Action: "CONTINUE"}, nil
	}
	if !contains(r.cfg.Models, req.Model) {
		return PrepareResponse{Action: "CONTINUE"}, nil
	}
	if r.cfg.Role == "probe" {
		return PrepareResponse{Action: "CONTINUE", ReasonCode: "PROBE_ROLE_PASS_THROUGH"}, nil
	}
	if r.cfg.DryRun {
		return PrepareResponse{Action: "CONTINUE", ReasonCode: "DRY_RUN_PASS_THROUGH"}, nil
	}
	if r.store == nil {
		if r.cfg.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_STORE_UNAVAILABLE", Message: "codex ticket store unavailable"}, nil
		}
		p.skipDecisions.Add(1)
		return PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_STORE_UNAVAILABLE_FAIL_OPEN"}, nil
	}
	now := time.Now()
	// Route credentials are a process-wide steering pool, independent from an
	// account/model ticket.  This mirrors the original runtime: a healthy edge
	// pair can still steer a request while its account ticket is being refreshed
	// or while fail-open ticket policy lets the host continue natively.
	var pooledRoute *routecookie.Pair
	if pair, poolErr := r.store.SelectRoutePair(ctx, now, time.Duration(r.cfg.RouteCookieTTLSeconds)*time.Second); poolErr != nil {
		p.recordError(poolErr)
	} else {
		pooledRoute = pair
	}
	withPooledRoute := func(resp PrepareResponse) PrepareResponse {
		if pooledRoute == nil || !pooledRoute.HasValue() {
			return resp
		}
		if resp.HeadersToSet == nil {
			resp.HeadersToSet = make(map[string][]string)
		}
		resp.HeadersToSet[routeCookieHeader] = []string{pooledRoute.Header()}
		p.rememberRouteRequest(req.RequestID, pooledRoute.Key())
		p.steerDecisions.Add(1)
		return resp
	}
	// A live caller-supplied turn state owns the current turn. A stale or
	// malformed value must not shadow a fresh local/relay ticket: the reference
	// runtime clears it before trying recovery, and fail-open callers receive a
	// native request without the expired credential.
	clientTicket := firstPrepareHeader(req.Headers, turnStateHeader)
	clearClientTicket := false
	if clientTicket != "" {
		if clientTicketUsable(clientTicket, now) {
			return withPooledRoute(PrepareResponse{Action: "CONTINUE", ReasonCode: "CLIENT_TICKET_PRESERVED"}), nil
		}
		clearClientTicket = true
	}
	// Re-resolve the short-lived outbound identity before using a cached ticket.
	// This binds injection to the current ChatGPT account identity and prevents a
	// stale ticket surviving an OAuth credential replacement.
	identity, identityErr := p.resolveIdentity(ctx, req.AccountID, nil)
	if identityErr != nil {
		if r.cfg.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "IDENTITY_UNAVAILABLE", Message: "codex account identity unavailable"}, nil
		}
		return withPooledRoute(withDeletedTicket(PrepareResponse{Action: "CONTINUE", ReasonCode: "IDENTITY_UNAVAILABLE_FAIL_OPEN"}, clearClientTicket)), nil
	} else if identity.ChatGPTAccountID != "" {
		identityHash := ticket.IdentityHash(identity.ChatGPTAccountID, "")
		if t, err := r.store.Get(ctx, req.AccountID, req.Model); err == nil && t != nil && !t.IdentityMatches(identityHash) {
			if r.cfg.FailClosed {
				return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_IDENTITY_MISMATCH", Message: "codex ticket identity mismatch"}, nil
			}
			return withPooledRoute(withDeletedTicket(PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_IDENTITY_MISMATCH_FAIL_OPEN"}, clearClientTicket)), nil
		}
	}
	t, e := r.store.Get(ctx, req.AccountID, req.Model)
	if e != nil {
		p.recordError(e)
		if r.cfg.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_STORE_ERROR", Message: e.Error()}, nil
		}
		return withPooledRoute(withDeletedTicket(PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_STORE_ERROR_FAIL_OPEN"}, clearClientTicket)), nil
	}
	transport := req.Transport
	if transport == "" {
		transport = r.cfg.Transport
	}
	selected, ok := t.Select(now, r.cfg.TargetLength, transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation)
	if ok && selected.Identity != "" {
		if identityErr != nil || ticket.IdentityHash(identity.ChatGPTAccountID, "") != selected.Identity {
			ok = false
		}
	}
	if ok {
		p.passDecisions.Add(1)
		headers := map[string][]string{turnStateHeader: []string{selected.State}}
		cookieHeader := selected.RouteCookieHeader()
		var injectedPair *routecookie.Pair
		if selected.RoutePair != nil && selected.RoutePair.HasValue() {
			pair := selected.RoutePair.Clone()
			injectedPair = &pair
		}
		if pooledRoute != nil {
			cookieHeader = pooledRoute.Header()
			injectedPair = pooledRoute
		}
		if cookieHeader != "" {
			headers[routeCookieHeader] = []string{cookieHeader}
			if injectedPair != nil {
				p.rememberRouteRequest(req.RequestID, injectedPair.Key())
				p.steerDecisions.Add(1)
			}
		}
		return PrepareResponse{Action: "CONTINUE", HeadersToSet: headers}, nil
	}
	if r.cfg.CloudMint.Enabled && r.cloud != nil && identityErr == nil {
		seedCookie := firstPrepareHeader(req.Headers, routeCookieSeedHeader)
		// The relay contract requires a complete seed pair.  A partial route
		// observation is still useful for direct steering, but must not turn a
		// fresh mint into an avoidable "incomplete seed" failure.
		if seedCookie == "" && pooledRoute != nil && pooledRoute.Complete() {
			seedCookie = pooledRoute.Header()
		}
		minted, mintErr := r.cloud.Get(ctx, r.cfg.CloudMint,
			cloudmint.Credentials{AccessToken: identity.Token, AccountID: identity.ChatGPTAccountID},
			r.cfg.CloudMint.MintModel, seedCookie)
		if mintErr == nil && minted != nil {
			minted.AccountID = req.AccountID
			minted.Model = req.Model
			if identity.ChatGPTAccountID != "" {
				minted.Identity = ticket.IdentityHash(identity.ChatGPTAccountID, "")
			}
			if storeErr := r.store.PutWithStandby(ctx, minted, time.Duration(r.cfg.TTLSeconds)*time.Second,
				r.cfg.TargetLength, transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); storeErr == nil {
				if selected, ok = minted.Select(time.Now(), r.cfg.TargetLength, transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); ok {
					p.passDecisions.Add(1)
					if minted.RoutePair != nil && minted.RoutePair.Complete() {
						p.learnRoutePair(ctx, r, minted.RoutePair, time.Now())
					}
					headers := map[string][]string{turnStateHeader: []string{selected.State}}
					if cookieHeader := selected.RouteCookieHeader(); cookieHeader != "" {
						headers[routeCookieHeader] = []string{cookieHeader}
						if minted.RoutePair != nil && minted.RoutePair.Complete() {
							p.rememberRouteRequest(req.RequestID, minted.RoutePair.Key())
							p.steerDecisions.Add(1)
						}
					}
					return PrepareResponse{Action: "CONTINUE", HeadersToSet: headers, ReasonCode: "CLOUD_MINTED"}, nil
				}
			} else {
				p.recordError(storeErr)
			}
		}
		if r.cfg.CloudMint.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "CLOUD_MINT_UNAVAILABLE", Message: "cloud ticket unavailable", RetryAfterMs: 2000}, nil
		}
	}
	if r.cfg.CloudMint.Enabled && r.cfg.CloudMint.FailClosed && identityErr != nil {
		return PrepareResponse{Action: "REJECT", ReasonCode: "CLOUD_MINT_IDENTITY_UNAVAILABLE", Message: "cloud mint identity unavailable", RetryAfterMs: 2000}, nil
	}
	if r.cfg.FailClosed {
		return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_UNAVAILABLE", Message: "codex ticket unavailable", RetryAfterMs: int64(time.Second / time.Millisecond)}, nil
	}
	p.skipDecisions.Add(1)
	return withPooledRoute(withDeletedTicket(PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_MISSING_FAIL_OPEN"}, clearClientTicket)), nil
}

func withDeletedTicket(resp PrepareResponse, clear bool) PrepareResponse {
	if clear {
		resp.HeadersToDelete = append(resp.HeadersToDelete, turnStateHeader)
	}
	return resp
}

func clientTicketUsable(state string, now time.Time) bool {
	shape, err := ticket.ParseState(state)
	if err != nil || shape.IssuedAt.After(now.Add(30*time.Second)) {
		return false
	}
	lifetime := ticket.DefaultLifetime
	if len(strings.TrimSpace(state)) == 780 {
		lifetime = ticket.CredentialTTL
	}
	return now.Before(shape.IssuedAt.Add(lifetime))
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == strings.TrimSpace(s) {
			return true
		}
	}
	return false
}

func firstPrepareHeader(in map[string][]string, name string) string {
	for key, values := range in {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
	}
	return ""
}
func (p *Plugin) Forward(grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	return errors.New("Codex ticket plugin does not implement transport forwarding")
}
func (p *Plugin) Shutdown() {
	p.stopWorker()
	if r := p.state.Load(); r != nil {
		p.persistObservations(r)
		if r.cloud != nil {
			r.cloud.Close()
		}
	}
	p.brokerMu.Lock()
	if p.hostConn != nil {
		_ = p.hostConn.Close()
	}
	p.hostConn = nil
	p.host = nil
	p.brokerMu.Unlock()
}
func HashIdentity(accountID, email string) string {
	h := sha256.Sum256([]byte(accountID + "\x00" + email))
	return hex.EncodeToString(h[:])
}
func (p *Plugin) DebugString() string {
	r := p.state.Load()
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", PluginID, r.rev)
}
