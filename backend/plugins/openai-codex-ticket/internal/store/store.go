package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
)

const Namespace = "codex-ticket-v1"

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
	kv     KV
	mu     sync.RWMutex
	memory map[string]*ticket.Ticket
}

func New(kv KV) *Store { return &Store{kv: kv, memory: map[string]*ticket.Ticket{}} }
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
		cp := *mem
		return &cp, nil
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
	return t, nil
}
func (s *Store) Put(ctx context.Context, t *ticket.Ticket, ttl time.Duration) error {
	if t == nil {
		return fmt.Errorf("nil ticket")
	}
	t.Normalize()
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	k := key(t.AccountID, t.Model)
	s.mu.Lock()
	s.memory[k] = t
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
			cp := *t
			out = append(out, &cp)
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
			out = append(out, &t)
		}
	}
	return out, nil
}
func (s *Store) PutWithStandby(ctx context.Context, incoming *ticket.Ticket, ttl time.Duration, target int, transport, gateway string, requireCookies, requireGateway bool) error {
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
