package cloudmint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/routecookie"
	"github.com/Wei-Shaw/sub2api/plugins/openai-codex-ticket/internal/ticket"
)

const (
	maxWorkers     = 4
	maxCache       = 256
	failureCooling = 30 * time.Second
)

type job struct {
	done   chan struct{}
	value  *ticket.Ticket
	err    error
	key    string
	client *Client
}

type cached struct {
	value *ticket.Ticket
	err   error
	until time.Time
}

// Service deduplicates concurrent mint requests and leaves a completed job in
// cache for the ticket's remaining lifetime. A short negative cache prevents a
// failing relay from being hammered by every request in a burst.
type Service struct {
	mu     sync.Mutex
	ctx    context.Context
	stop   context.CancelFunc
	cache  map[string]cached
	jobs   map[string]*job
	groups map[string]bool
}

func NewService() *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{ctx: ctx, stop: cancel, cache: map[string]cached{}, jobs: map[string]*job{}, groups: map[string]bool{}}
}

func (s *Service) Close() {
	if s != nil && s.stop != nil {
		s.stop()
	}
}

func (s *Service) Get(ctx context.Context, cfg config.CloudMintConfig, creds Credentials, model, seedCookie string) (*ticket.Ticket, error) {
	if s == nil {
		return nil, errors.New("cloud mint service unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg = cfg.Normalized()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, errors.New("cloud mint disabled")
	}
	if strings.TrimSpace(seedCookie) != "" {
		pair, err := routecookie.ValidateSeed(seedCookie, cfg.Gateway, time.Now())
		if err != nil {
			return nil, err
		}
		// Normalize before deriving the cache key as well as before the HTTP
		// request.  Equivalent Cookie headers must share one in-flight mint and
		// arbitrary browser cookies must never become part of relay state.
		seedCookie = pair.Header()
	}
	// Resolve the proxy before deriving the cache key.  A deployment may rotate
	// the environment-backed exit while keeping the variable name unchanged;
	// the concrete URL therefore has to participate in dedupe/cache identity.
	if strings.TrimSpace(cfg.ProxyURL) == "" && strings.TrimSpace(cfg.ProxyEnv) != "" {
		cfg.ProxyURL = strings.TrimSpace(getenv(cfg.ProxyEnv))
		if cfg.ProxyURL == "" {
			return nil, errors.New("cloud mint proxy environment variable is unset")
		}
	}
	relayKey := RelayKey(cfg)
	if strings.TrimSpace(relayKey) == "" {
		return nil, errors.New("cloud mint relay key environment variable is unset")
	}
	key := cacheKey(cfg, creds, model, relayKey, seedCookie)
	group := credentialKey(creds)
	s.mu.Lock()
	if hit, ok := s.cache[key]; ok && hit.until.After(time.Now().Add(time.Second)) {
		value := cloneTicket(hit.value)
		err := hit.err
		s.mu.Unlock()
		return value, err
	}
	if current := s.jobs[key]; current != nil {
		s.mu.Unlock()
		return waitJob(ctx, current, cfg.WaitMS)
	}
	if len(s.jobs) >= maxWorkers || s.groups[group] {
		s.mu.Unlock()
		return nil, errors.New("cloud mint busy; retry later")
	}
	jobValue := &job{done: make(chan struct{}), key: key}
	s.jobs[key] = jobValue
	s.groups[group] = true
	s.mu.Unlock()
	go s.run(jobValue, cfg, creds, model, seedCookie, group)
	return waitJob(ctx, jobValue, cfg.WaitMS)
}

func (s *Service) run(j *job, cfg config.CloudMintConfig, creds Credentials, model, seedCookie, group string) {
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	proxyURL := strings.TrimSpace(cfg.ProxyURL)
	if proxyURL == "" && strings.TrimSpace(cfg.ProxyEnv) != "" {
		proxyURL = strings.TrimSpace(getenv(cfg.ProxyEnv))
		if proxyURL == "" {
			j.err = errors.New("cloud mint proxy environment variable is unset")
			s.finish(j, group, nil, j.err)
			return
		}
	}
	client, err := NewClient(proxyURL)
	if err == nil {
		j.value, err = client.Mint(ctx, cfg, creds, model, RelayKey(cfg), seedCookie)
	}
	if ctx.Err() != nil && err == nil {
		err = errors.New("cloud mint timed out")
	}
	j.err = err
	s.finish(j, group, j.value, err)
}

func (s *Service) finish(j *job, group string, value *ticket.Ticket, err error) {
	s.mu.Lock()
	delete(s.jobs, j.key)
	delete(s.groups, group)
	if s.ctx.Err() == nil {
		if len(s.cache) >= maxCache {
			var oldestKey string
			var oldest time.Time
			for key, item := range s.cache {
				if oldestKey == "" || item.until.Before(oldest) {
					oldestKey, oldest = key, item.until
				}
			}
			if oldestKey != "" {
				delete(s.cache, oldestKey)
			}
		}
		until := time.Now().Add(failureCooling)
		if value != nil && !value.ExpiresAt.IsZero() {
			until = value.ExpiresAt
		}
		s.cache[j.key] = cached{value: cloneTicket(value), err: err, until: until}
	}
	close(j.done)
	s.mu.Unlock()
}

func waitJob(ctx context.Context, j *job, waitMS int) (*ticket.Ticket, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-j.done:
		return cloneTicket(j.value), j.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, errors.New("cloud mint pending; retry later")
	}
}

func cacheKey(cfg config.CloudMintConfig, creds Credentials, model, relayKey, seed string) string {
	h := sha256.New()
	for _, value := range []string{cfg.URL, cfg.ProxyURL, cfg.ProxyEnv, cfg.Transport, cfg.Gateway, fmt.Sprint(cfg.TicketLength), fmt.Sprint(cfg.TTLSeconds), relayKey, creds.AccountID, creds.AccessToken, model, seed} {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func credentialKey(creds Credentials) string {
	h := sha256.Sum256([]byte(creds.AccountID + "\x00" + creds.AccessToken))
	return hex.EncodeToString(h[:])
}

func cloneTicket(value *ticket.Ticket) *ticket.Ticket {
	if value == nil {
		return nil
	}
	copy := *value
	copy.HarvestCookies = append([]string(nil), value.HarvestCookies...)
	if value.RoutePair != nil {
		pair := value.RoutePair.Clone()
		copy.RoutePair = &pair
	}
	return &copy
}

var getenv = func(key string) string { return os.Getenv(key) }
