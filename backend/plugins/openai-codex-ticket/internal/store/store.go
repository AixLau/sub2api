package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
)

const Namespace = "codex-ticket-v1"

const routePoolKey = "route-pool"
const routePoolVersion = 1
const routePoolMax = 256
const routeBadPenalty = 90 * time.Second

type KV interface {
	Get(context.Context, string, string) ([]byte, bool, error)
	Set(context.Context, string, string, []byte, time.Duration) error
	Delete(context.Context, string, string) error
	List(context.Context, string, string, int) ([]string, error)
}

type MemoryKV struct {
	mu     sync.RWMutex
	values map[string][]byte
}

func NewMemoryKV() *MemoryKV { return &MemoryKV{values: map[string][]byte{}} }
func (m *MemoryKV) Get(_ context.Context, ns, key string) ([]byte, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.values[ns+"\x00"+key]
	return append([]byte(nil), b...), ok, nil
}
func (m *MemoryKV) Set(_ context.Context, ns, key string, b []byte, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[ns+"\x00"+key] = append([]byte(nil), b...)
	return nil
}
func (m *MemoryKV) Delete(_ context.Context, ns, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, ns+"\x00"+key)
	return nil
}
func (m *MemoryKV) List(_ context.Context, ns, prefix string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []string{}
	for k := range m.values {
		parts := strings.SplitN(k, "\x00", 2)
		if len(parts) == 2 && parts[0] == ns && strings.HasPrefix(parts[1], prefix) {
			out = append(out, parts[1])
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

type Store struct {
	kv          KV
	mu          sync.RWMutex
	routeOpMu   sync.Mutex
	memory      map[string]*ticket.Ticket
	routePairs  map[string]*routecookie.Pair
	routeLoaded bool
}

func New(kv KV) *Store {
	return &Store{kv: kv, memory: map[string]*ticket.Ticket{}, routePairs: map[string]*routecookie.Pair{}}
}

type routePoolDocument struct {
	Version int                `json:"version"`
	Pairs   []routecookie.Pair `json:"pairs"`
}

func clonePairs(in map[string]*routecookie.Pair) []*routecookie.Pair {
	out := make([]*routecookie.Pair, 0, len(in))
	for _, pair := range in {
		if pair == nil || !pair.HasValue() {
			continue
		}
		copy := pair.Clone()
		out = append(out, &copy)
	}
	return out
}

func pairMap(pairs []*routecookie.Pair) map[string]*routecookie.Pair {
	out := make(map[string]*routecookie.Pair, len(pairs))
	for _, pair := range pairs {
		if pair == nil || !pair.HasValue() {
			continue
		}
		copy := pair.Clone()
		key := copy.Key()
		if key != "" {
			out[key] = &copy
		}
	}
	return out
}

func (s *Store) loadRoutePairs(ctx context.Context) (map[string]*routecookie.Pair, error) {
	if s == nil {
		return map[string]*routecookie.Pair{}, nil
	}
	s.mu.RLock()
	if s.routeLoaded {
		pairs := pairMap(clonePairs(s.routePairs))
		s.mu.RUnlock()
		return pairs, nil
	}
	s.mu.RUnlock()
	pairs := map[string]*routecookie.Pair{}
	if s.kv != nil {
		raw, found, err := s.kv.Get(ctx, Namespace, routePoolKey)
		if err != nil {
			return nil, err
		}
		if found {
			var doc routePoolDocument
			if json.Unmarshal(raw, &doc) == nil && doc.Version == routePoolVersion {
				pairs = pairMap(func() []*routecookie.Pair {
					out := make([]*routecookie.Pair, 0, len(doc.Pairs))
					for i := range doc.Pairs {
						pair := doc.Pairs[i]
						out = append(out, &pair)
					}
					return out
				}())
			}
		}
	}
	s.mu.Lock()
	if !s.routeLoaded {
		s.routePairs = pairs
		s.routeLoaded = true
	}
	pairs = pairMap(clonePairs(s.routePairs))
	s.mu.Unlock()
	return pairs, nil
}

func (s *Store) persistRoutePairs(ctx context.Context, pairs map[string]*routecookie.Pair, ttl time.Duration) error {
	if s == nil || s.kv == nil {
		return nil
	}
	doc := routePoolDocument{Version: routePoolVersion, Pairs: make([]routecookie.Pair, 0, len(pairs))}
	for _, pair := range pairs {
		if pair != nil && pair.HasValue() {
			doc.Pairs = append(doc.Pairs, pair.Clone())
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return s.kv.Set(ctx, Namespace, routePoolKey, raw, ttl)
}

// GetRoutePairs returns a defensive copy of the process-wide route pool.
func (s *Store) GetRoutePairs(ctx context.Context) ([]*routecookie.Pair, error) {
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return nil, err
	}
	return clonePairs(pairs), nil
}

// MergeRoutePair records one or more allow-listed route cookies without
// discarding healthy pairs learned from other gateways. The pool is
// intentionally account-free; partial observations are useful and are
// completed by a later response when the edge issues the companion cookie.
func (s *Store) MergeRoutePair(ctx context.Context, incoming *routecookie.Pair, ttl time.Duration) error {
	if s == nil || incoming == nil || !incoming.HasValue() {
		return nil
	}
	s.routeOpMu.Lock()
	defer s.routeOpMu.Unlock()
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return err
	}
	copy := incoming.Clone()
	copy.Gateway = routecookie.Gateway(copy.Values)
	key := copy.Key()
	if key == "" {
		return nil
	}
	if current := pairs[key]; current != nil {
		newer := copy.SeenAt.After(current.SeenAt)
		if copy.SeenAt.After(current.SeenAt) {
			current.SeenAt = copy.SeenAt
		}
		if !copy.ExpiresAt.IsZero() && (current.ExpiresAt.IsZero() || newer || copy.ExpiresAt.Before(current.ExpiresAt)) {
			current.ExpiresAt = copy.ExpiresAt
		}
		if copy.Via != "" {
			current.Via = copy.Via
		}
		copy = current.Clone()
	}
	pairs[key] = &copy
	if len(pairs) > routePoolMax {
		// Keep the newest/most recently successful entries bounded. This is a
		// deterministic eviction policy and avoids unbounded KV growth.
		for len(pairs) > routePoolMax {
			oldestKey := ""
			var oldest time.Time
			for candidate, pair := range pairs {
				if oldestKey == "" || pair.Score().Before(oldest) {
					oldestKey, oldest = candidate, pair.Score()
				}
			}
			delete(pairs, oldestKey)
		}
	}
	s.mu.Lock()
	s.routePairs = pairMap(clonePairs(pairs))
	s.routeLoaded = true
	s.mu.Unlock()
	return s.persistRoutePairs(ctx, pairs, ttl)
}

// SelectRoutePair chooses the healthiest usable route. Recently degraded
// pairs are held as a fallback so a temporarily empty pool can still recover.
func (s *Store) SelectRoutePair(ctx context.Context, now time.Time, ttl time.Duration) (*routecookie.Pair, error) {
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return nil, err
	}
	var best, fallback *routecookie.Pair
	for _, pair := range pairs {
		if pair == nil || !pair.Usable(now, ttl) {
			continue
		}
		if pair.Penalized(now, routeBadPenalty) {
			if fallback == nil || pair.Score().After(fallback.Score()) {
				fallback = pair
			}
			continue
		}
		if best == nil || pair.Score().After(best.Score()) {
			best = pair
		}
	}
	if best == nil {
		best = fallback
	}
	if best == nil {
		return nil, nil
	}
	copy := best.Clone()
	return &copy, nil
}

// MarkRoutePair records the outcome of a steered request for pool scoring.
func (s *Store) MarkRoutePair(ctx context.Context, fingerprint string, healthy bool, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return nil
	}
	s.routeOpMu.Lock()
	defer s.routeOpMu.Unlock()
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return err
	}
	pair := pairs[fingerprint]
	if pair == nil {
		return nil
	}
	now := time.Now()
	if healthy {
		pair.GoodAt = now
	} else {
		pair.BadAt = now
	}
	s.mu.Lock()
	s.routePairs = pairMap(clonePairs(pairs))
	s.routeLoaded = true
	s.mu.Unlock()
	return s.persistRoutePairs(ctx, pairs, ttl)
}

func (s *Store) ClearRoutePairs(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.routeOpMu.Lock()
	defer s.routeOpMu.Unlock()
	s.mu.Lock()
	s.routePairs = map[string]*routecookie.Pair{}
	s.routeLoaded = true
	s.mu.Unlock()
	if s.kv != nil {
		return s.kv.Delete(ctx, Namespace, routePoolKey)
	}
	return nil
}

// DeleteRoutePair removes one route credential after the response that used it
// explicitly expires or deletes the edge cookie. Other gateways remain
// available to concurrent business requests.
func (s *Store) DeleteRoutePair(ctx context.Context, fingerprint string, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return nil
	}
	s.routeOpMu.Lock()
	defer s.routeOpMu.Unlock()
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return err
	}
	if _, exists := pairs[fingerprint]; !exists {
		return nil
	}
	delete(pairs, fingerprint)
	s.mu.Lock()
	s.routePairs = pairMap(clonePairs(pairs))
	s.routeLoaded = true
	s.mu.Unlock()
	return s.persistRoutePairs(ctx, pairs, ttl)
}

// GetRoutePair returns the process-wide routing pair. Unlike tickets, the pair
// is intentionally not keyed by account: the edge routing credential is a
// shared pool item and can be reused by any attributable Codex OAuth turn.
func (s *Store) GetRoutePair(ctx context.Context) (*routecookie.Pair, error) {
	pairs, err := s.loadRoutePairs(ctx)
	if err != nil {
		return nil, err
	}
	var best *routecookie.Pair
	for _, pair := range pairs {
		if pair != nil && (best == nil || pair.Score().After(best.Score())) {
			best = pair
		}
	}
	if best == nil {
		return nil, nil
	}
	copy := best.Clone()
	return &copy, nil
}

func (s *Store) PutRoutePair(ctx context.Context, pair *routecookie.Pair, ttl time.Duration) error {
	if pair == nil || !pair.HasValue() {
		return s.ClearRoutePairs(ctx)
	}
	return s.MergeRoutePair(ctx, pair, ttl)
}
func key(accountID int64, model string) string {
	// HostService KV keys allow only letters, digits, '.', '_' and '-'. Encode
	// the model so names containing other punctuation cannot make harvesting
	// fail after the ticket has already been obtained.
	encodedModel := base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(model)))
	return fmt.Sprintf("ticket.%d.%s", accountID, encodedModel)
}
func (s *Store) Get(ctx context.Context, accountID int64, model string) (*ticket.Ticket, error) {
	k := key(accountID, model)
	s.mu.RLock()
	mem := s.memory[k]
	s.mu.RUnlock()
	if mem != nil {
		return cloneTicket(mem), nil
	}
	if s.kv == nil {
		return nil, nil
	}
	raw, found, err := s.kv.Get(ctx, Namespace, k)
	if err != nil || !found {
		return nil, err
	}
	t, err := ticket.ParseJSON(raw, accountID, model)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.memory[k] = t
	s.mu.Unlock()
	return cloneTicket(t), nil
}

func cloneTicket(in *ticket.Ticket) *ticket.Ticket {
	if in == nil {
		return nil
	}
	out := *in
	out.HarvestCookies = append([]string(nil), in.HarvestCookies...)
	if in.HarvestCookieExpiries != nil {
		out.HarvestCookieExpiries = make(map[string]time.Time, len(in.HarvestCookieExpiries))
		for key, value := range in.HarvestCookieExpiries {
			out.HarvestCookieExpiries[key] = value
		}
	}
	if in.RoutePair != nil {
		pair := in.RoutePair.Clone()
		out.RoutePair = &pair
	}
	if in.Standby != nil {
		out.Standby = cloneTicket(in.Standby)
	}
	return &out
}
func (s *Store) Put(ctx context.Context, t *ticket.Ticket, ttl time.Duration) error {
	if t == nil {
		return fmt.Errorf("nil ticket")
	}
	stored := cloneTicket(t)
	stored.Normalize()
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	k := key(stored.AccountID, stored.Model)
	s.mu.Lock()
	s.memory[k] = stored
	s.mu.Unlock()
	if s.kv != nil {
		return s.kv.Set(ctx, Namespace, k, raw, ttl)
	}
	return nil
}
func (s *Store) Delete(ctx context.Context, accountID int64, model string) error {
	k := key(accountID, model)
	s.mu.Lock()
	delete(s.memory, k)
	s.mu.Unlock()
	if s.kv != nil {
		return s.kv.Delete(ctx, Namespace, k)
	}
	return nil
}
func (s *Store) List(ctx context.Context) ([]*ticket.Ticket, error) {
	if s.kv == nil {
		s.mu.RLock()
		defer s.mu.RUnlock()
		out := []*ticket.Ticket{}
		for _, t := range s.memory {
			out = append(out, cloneTicket(t))
		}
		return out, nil
	}
	keys, err := s.kv.List(ctx, Namespace, "ticket.", 10000)
	if err != nil {
		return nil, err
	}
	out := make([]*ticket.Ticket, 0, len(keys))
	for _, k := range keys {
		raw, found, e := s.kv.Get(ctx, Namespace, k)
		if e != nil || !found {
			continue
		}
		var t ticket.Ticket
		if json.Unmarshal(raw, &t) == nil {
			out = append(out, cloneTicket(&t))
		}
	}
	return out, nil
}
func (s *Store) PutWithStandby(ctx context.Context, incoming *ticket.Ticket, ttl time.Duration, target int, transport, gateway string, requireCookies, requireGateway bool) error {
	if incoming == nil {
		return fmt.Errorf("nil ticket")
	}
	current, _ := s.Get(ctx, incoming.AccountID, incoming.Model)
	now := time.Now()
	if current != nil {
		if selected, ok := current.Select(now, target, transport, gateway, requireCookies, requireGateway); ok && selected.State != incoming.State {
			cp := *current
			cp.Standby = incoming
			return s.Put(ctx, &cp, ttl)
		}
	}
	return s.Put(ctx, incoming, ttl)
}
