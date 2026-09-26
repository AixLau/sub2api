package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/harvest"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/store"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
)

const (
	PluginID        = "local.sub2api.openai-codex-ticket"
	PluginVersion   = "1.0.1"
	Capability      = "openai.oauth.codex_ticket_hook.v1"
	turnStateHeader = "x-codex-turn-state"
)

type Runtime struct {
	cfg       config.Config
	rev       string
	harvester *harvest.Harvester
	store     *store.Store
}
type Plugin struct {
	pluginv1.UnimplementedTransportPluginServer
	mu        sync.RWMutex
	state     atomic.Pointer[Runtime]
	brokerMu  sync.RWMutex
	broker    *plugin.GRPCBroker
	hostConn  *grpc.ClientConn
	host      pluginv1.HostServiceClient
	ctx       context.Context
	cancel    context.CancelFunc
	running   atomic.Bool
	probes    atomic.Uint64
	successes atomic.Uint64
	failures  atomic.Uint64
	lastError atomic.Value
	lastProbe atomic.Value
}

func New() *Plugin {
	p := &Plugin{}
	c := config.Defaults()
	p.state.Store(&Runtime{cfg: c, rev: c.Revision(), harvester: harvest.New(c), store: store.New(nil)})
	return p
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
	return map[string]any{"enabled": r.cfg.Enabled, "revision": r.rev, "host_kv": p.hostClient() != nil, "running": p.running.Load(), "probes": p.probes.Load(), "successes": p.successes.Load(), "failures": p.failures.Load(), "last_error": p.lastError.Load(), "last_probe": p.lastProbe.Load(), "accounts_total": len(tickets), "ready_tickets": ready, "blocked_tickets": blocked, "config": map[string]any{"target_length": r.cfg.TargetLength, "ttl_seconds": r.cfg.TTLSeconds, "refresh_before_seconds": r.cfg.RefreshBeforeSeconds, "models": r.cfg.Models, "transport": r.cfg.Transport, "target_gateway": r.cfg.TargetGateway, "fail_closed": r.cfg.FailClosed, "harvest_proxy_url": maskProxy(r.cfg.HarvestProxyURL)}, "tickets": tickets}
}
func maskProxy(raw string) string {
	if raw == "" || raw == "ip-pool" {
		return raw
	}
	if i := strings.Index(raw, "@"); i >= 0 {
		if j := strings.Index(raw, "://"); j >= 0 {
			return raw[:j+3] + "***@" + raw[i+1:]
		}
	}
	return raw
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
	r := &Runtime{cfg: c, rev: c.Revision(), harvester: harvest.New(c), store: store.New(nil)}
	if old != nil && old.store != nil {
		r.store = old.store
	}
	if kv := p.hostKV(); kv != nil {
		r.store = store.New(kv)
	}
	p.state.Store(r)
	p.mu.Unlock()
	if old != nil && old.cfg.Enabled && !c.Enabled {
		p.stopWorker()
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
	pr := PrepareRequest{AccountID: req.AccountId, Model: req.Model, Transport: req.Transport, Platform: req.Platform, AccountType: req.AccountType, Headers: headerMap(req.Headers)}
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
	if req.Platform != "" && req.Platform != "openai" || req.AccountType != "" && req.AccountType != "oauth" {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "ACCOUNT_SCOPE"}, nil
	}
	if !contains(r.cfg.Models, req.Model) {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "MODEL_SCOPE"}, nil
	}
	if req.StatusCode == 401 || req.StatusCode == 403 {
		old, _ := r.store.Get(ctx, req.AccountId, req.Model)
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
	state := firstHeader(req.Headers, turnStateHeader)
	if state == "" {
		return &pluginv1.ObserveOutboundResponseResponse{Observed: false, ReasonCode: "NO_TICKET"}, nil
	}
	now := time.Now()
	t := &ticket.Ticket{AccountID: req.AccountId, Model: req.Model, State: state, CapturedAt: now, ExpiresAt: now.Add(time.Duration(r.cfg.TTLSeconds) * time.Second), Transport: req.Transport, Gateway: r.cfg.TargetGateway}
	old, _ := r.store.Get(ctx, req.AccountId, req.Model)
	if req.SentTicketState != "" && req.SentTicketState == state {
		if old != nil {
			if cookies := headerValues(req.Headers, "set-cookie"); len(cookies) > 0 {
				old.HarvestCookies = mergeCookies(old.HarvestCookies, cookies)
				old.HarvestCookiesAt = now
				_ = r.store.Put(ctx, old, time.Duration(r.cfg.TTLSeconds)*time.Second)
				return &pluginv1.ObserveOutboundResponseResponse{Observed: true, ReasonCode: "COOKIES_UPDATED"}, nil
			}
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
	if old != nil {
		t.Identity = old.Identity
		t.HarvestProxyURL = old.HarvestProxyURL
		t.HarvestSessionID = old.HarvestSessionID
		t.HarvestCookies = old.HarvestCookies
		t.HarvestCookiesAt = old.HarvestCookiesAt
		t.Gateway = old.Gateway
	}
	if cookies := headerValues(req.Headers, "set-cookie"); len(cookies) > 0 {
		t.HarvestCookies = mergeCookies(t.HarvestCookies, cookies)
		t.HarvestCookiesAt = now
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

// mergeCookies keeps a bounded, name-keyed cookie jar. Set-Cookie values are
// treated as opaque after extracting the cookie name; the plugin never exposes
// them in status output. This prevents response feedback from growing state
// without bound while preserving the latest value for each cookie.
func mergeCookies(existing, incoming []string) []string {
	values := make(map[string]string, 32)
	order := make([]string, 0, 32)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(raw) > 8192 {
			return
		}
		name := raw
		if i := strings.IndexByte(raw, '='); i > 0 {
			name = strings.TrimSpace(raw[:i])
		}
		if name == "" {
			return
		}
		if _, ok := values[name]; !ok {
			order = append(order, name)
		}
		values[name] = raw
	}
	for _, raw := range existing {
		add(raw)
	}
	for _, raw := range incoming {
		add(raw)
	}
	if len(order) > 32 {
		order = order[len(order)-32:]
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		if value, ok := values[name]; ok {
			out = append(out, value)
		}
	}
	return out
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
		r := p.state.Load()
		if r != nil {
			r.store = store.New(kv)
		}
	}
	return &pluginv1.InitHostServicesResponse{Ready: true, Message: "宿主 KV/账号服务已连接"}, nil
}

func (p *Plugin) startWorker() {
	if !p.running.CompareAndSwap(false, true) {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	ctx := p.ctx
	p.mu.Unlock()
	go p.worker(ctx)
}
func (p *Plugin) stopWorker() {
	if !p.running.Swap(false) {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
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
func (p *Plugin) probeRound(ctx context.Context) {
	r := p.state.Load()
	if r == nil || !r.cfg.Enabled {
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
		for _, model := range r.cfg.Models {
			if count >= r.cfg.MaxProbesPerRound {
				return
			}
			now := time.Now()
			old, _ := r.store.Get(ctx, a.Id, model)
			if old != nil {
				if _, ok := old.Select(now, r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); ok && !old.NeedsRefresh(now, time.Duration(r.cfg.RefreshBeforeSeconds)*time.Second) {
					continue
				}
			}
			p.probes.Add(1)
			count++
			id, err := p.resolveIdentity(ctx, a.Id, meta)
			if err != nil {
				p.recordError(err)
				continue
			}
			t, err := r.harvester.Harvest(ctx, id, a.Id, model)
			if err != nil {
				p.recordError(err)
				continue
			}
			// The host's outbound identity RPC deliberately withholds email. Bind
			// tickets to the stable ChatGPT account id that is present in its
			// scoped headers; this remains verifiable on every outbound hook.
			identity := ticket.IdentityHash(id.ChatGPTAccountID, "")
			t.Identity = identity
			if err := t.Validate(time.Now(), r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
				p.recordError(err)
				continue
			}
			if err := r.store.PutWithStandby(ctx, t, time.Duration(r.cfg.TTLSeconds)*time.Second, r.cfg.TargetLength, r.cfg.Transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation); err != nil {
				p.recordError(err)
				continue
			}
			p.successes.Add(1)
			p.lastProbe.Store(time.Now().UTC().Format(time.RFC3339))
		}
	}
}
func (p *Plugin) resolveIdentity(ctx context.Context, id int64, meta map[string]any) (harvest.Identity, error) {
	h := p.hostClient()
	if h == nil {
		return harvest.Identity{}, errors.New("host service unavailable")
	}
	r, e := h.ResolveOutboundIdentity(ctx, &pluginv1.ResolveOutboundIdentityRequest{AccountId: id})
	if e != nil || !r.Found {
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
		p.lastError.Store(e.Error())
	}
}

// PrepareRequest/PrepareResponse are the stable, capability-local shape used
// by the generated PrepareOutbound adapter. Keeping policy here lets the host
// enforce the only permitted mutation (x-codex-turn-state).
type PrepareRequest struct {
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
	// Re-resolve the short-lived outbound identity before using a cached ticket.
	// This binds injection to the current ChatGPT account identity and prevents a
	// stale ticket surviving an OAuth credential replacement.
	if identity, err := p.resolveIdentity(ctx, req.AccountID, nil); err != nil {
		if r.cfg.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "IDENTITY_UNAVAILABLE", Message: "codex account identity unavailable"}, nil
		}
		return PrepareResponse{Action: "CONTINUE", ReasonCode: "IDENTITY_UNAVAILABLE_FAIL_OPEN"}, nil
	} else if identity.ChatGPTAccountID != "" {
		identityHash := ticket.IdentityHash(identity.ChatGPTAccountID, "")
		if t, err := r.store.Get(ctx, req.AccountID, req.Model); err == nil && t != nil && !t.IdentityMatches(identityHash) {
			if r.cfg.FailClosed {
				return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_IDENTITY_MISMATCH", Message: "codex ticket identity mismatch"}, nil
			}
			return PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_IDENTITY_MISMATCH_FAIL_OPEN"}, nil
		}
	}
	now := time.Now()
	t, e := r.store.Get(ctx, req.AccountID, req.Model)
	if e != nil {
		p.recordError(e)
		if r.cfg.FailClosed {
			return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_STORE_ERROR", Message: e.Error()}, nil
		}
		return PrepareResponse{Action: "CONTINUE"}, nil
	}
	transport := req.Transport
	if transport == "" {
		transport = r.cfg.Transport
	}
	selected, ok := t.Select(now, r.cfg.TargetLength, transport, r.cfg.TargetGateway, r.cfg.CookieValidation, r.cfg.GatewayValidation)
	if ok && selected.Identity != "" {
		identity, err := p.resolveIdentity(ctx, req.AccountID, nil)
		if err != nil || ticket.IdentityHash(identity.ChatGPTAccountID, "") != selected.Identity {
			ok = false
		}
	}
	if ok {
		return PrepareResponse{Action: "CONTINUE", HeadersToSet: map[string][]string{turnStateHeader: {selected.State}}}, nil
	}
	if r.cfg.FailClosed {
		return PrepareResponse{Action: "REJECT", ReasonCode: "TICKET_UNAVAILABLE", Message: "codex ticket unavailable", RetryAfterMs: int64(time.Second / time.Millisecond)}, nil
	}
	return PrepareResponse{Action: "CONTINUE", ReasonCode: "TICKET_MISSING_FAIL_OPEN"}, nil
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == strings.TrimSpace(s) {
			return true
		}
	}
	return false
}
func (p *Plugin) Forward(grpc.BidiStreamingServer[pluginv1.ForwardRequest, pluginv1.ForwardResponse]) error {
	return errors.New("Codex ticket plugin does not implement transport forwarding")
}
func (p *Plugin) Shutdown() {
	p.stopWorker()
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
