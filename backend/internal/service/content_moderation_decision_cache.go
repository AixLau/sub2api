package service

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"
)

// ContentModerationCachedDecision is the short-lived, per-subject result used
// to collapse client retries. It contains no prompt text; the cache key is a
// keyed digest of the exact selected provider payload.
type ContentModerationCachedDecision struct {
	SchemaVersion int                       `json:"schema_version"`
	Decision      ContentModerationDecision `json:"decision"`
	DecisionID    string                    `json:"decision_id,omitempty"`
	ExpiresAt     int64                     `json:"expires_at"`
}

// ContentModerationDecisionCache is deliberately separate from the long-lived
// PASS chunk cache. The latter proves reusable provider PASS evidence, while
// this cache suppresses a short burst of identical automatic client retries
// for both allow and block decisions.
type ContentModerationDecisionCache interface {
	Get(ctx context.Context, key string) (*ContentModerationCachedDecision, error)
	TryAcquire(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	Renew(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	Store(ctx context.Context, key string, entry ContentModerationCachedDecision, ttl time.Duration) error
	Release(ctx context.Context, key, owner string) error
}

// ContentModerationRetryDedupeRepository is optional so existing repository
// test doubles do not need retry bookkeeping support.
type ContentModerationRetryDedupeRepository interface {
	IncrementDuplicateRetryCount(ctx context.Context, decisionID string) error
}

const maxContentModerationCandidateMemoryDecisions = 4096

type contentModerationCandidateMemoryDecisionCache struct {
	mu      sync.Mutex
	entries map[string]ContentModerationCachedDecision
}

// contentModerationCandidateDecisionCoordinator is a local, context-aware
// counterpart to the Redis lease. Unlike singleflight it distinguishes the
// caller that performs the review from callers that joined it, so retry
// counters remain accurate and a cancelled waiter never cancels the review.
type contentModerationCandidateDecisionCoordinator struct {
	mu      sync.Mutex
	entries map[string]*contentModerationCandidateDecisionFlight
}

type contentModerationCandidateDecisionFlight struct {
	done    chan struct{}
	outcome contentModerationCandidateOutcome
}

func newContentModerationCandidateDecisionCoordinator() *contentModerationCandidateDecisionCoordinator {
	return &contentModerationCandidateDecisionCoordinator{entries: make(map[string]*contentModerationCandidateDecisionFlight)}
}

// Do runs fn once per key. The boolean reports whether this caller joined an
// already-running decision. A false completion means the caller's own context
// ended while another request was still reviewing the candidate.
func (c *contentModerationCandidateDecisionCoordinator) Do(
	ctx context.Context,
	key string,
	fn func() contentModerationCandidateOutcome,
) (contentModerationCandidateOutcome, bool, bool) {
	if c == nil || key == "" {
		return fn(), false, true
	}

	c.mu.Lock()
	if existing := c.entries[key]; existing != nil {
		c.mu.Unlock()
		select {
		case <-existing.done:
			return cloneContentModerationCandidateOutcome(existing.outcome), true, true
		case <-ctx.Done():
			return contentModerationCandidateOutcome{}, true, false
		}
	}
	flight := &contentModerationCandidateDecisionFlight{done: make(chan struct{})}
	c.entries[key] = flight
	c.mu.Unlock()

	outcome := fn()
	c.mu.Lock()
	flight.outcome = cloneContentModerationCandidateOutcome(outcome)
	delete(c.entries, key)
	close(flight.done)
	c.mu.Unlock()
	return outcome, false, true
}

func cloneContentModerationCandidateOutcome(in contentModerationCandidateOutcome) contentModerationCandidateOutcome {
	out := in
	if in.Decision != nil {
		decision := cloneContentModerationDecision(*in.Decision)
		out.Decision = &decision
	}
	return out
}

func newContentModerationCandidateMemoryDecisionCache() *contentModerationCandidateMemoryDecisionCache {
	return &contentModerationCandidateMemoryDecisionCache{entries: make(map[string]ContentModerationCachedDecision)}
}

func (c *contentModerationCandidateMemoryDecisionCache) Get(key string) (*ContentModerationCachedDecision, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if entry.ExpiresAt <= time.Now().Unix() {
		delete(c.entries, key)
		return nil, false
	}
	entry.Decision = cloneContentModerationDecision(entry.Decision)
	return &entry, true
}

func (c *contentModerationCandidateMemoryDecisionCache) Store(key string, entry ContentModerationCachedDecision, ttl time.Duration) {
	if c == nil || key == "" || ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxContentModerationCandidateMemoryDecisions {
		now := time.Now().Unix()
		for existingKey, existing := range c.entries {
			if existing.ExpiresAt <= now {
				delete(c.entries, existingKey)
			}
		}
		if len(c.entries) >= maxContentModerationCandidateMemoryDecisions {
			for existingKey := range c.entries {
				delete(c.entries, existingKey)
				break
			}
		}
	}
	entry.ExpiresAt = time.Now().Add(ttl).Unix()
	entry.Decision = cloneContentModerationDecision(entry.Decision)
	c.entries[key] = entry
}

func (s *ContentModerationService) SetDecisionCache(cache ContentModerationDecisionCache) {
	if s == nil {
		return
	}
	s.decisionCache = cache
}

// SetDecisionCacheKey configures the key used for cache keys and evidence
// HMACs. It is intentionally separate from the long-lived PASS cache key so
// retry collapsing remains available when that optional optimization is off.
func (s *ContentModerationService) SetDecisionCacheKey(key []byte) {
	if s == nil {
		return
	}
	if len(key) != sha256.Size {
		s.decisionCacheHMACKey = nil
		return
	}
	s.decisionCacheHMACKey = append([]byte(nil), key...)
}

func (s *ContentModerationService) candidateDecisionHMACKey() []byte {
	if s == nil {
		return nil
	}
	if len(s.decisionCacheHMACKey) == sha256.Size {
		return s.decisionCacheHMACKey
	}
	if len(s.moderationCacheHMACKey) == sha256.Size {
		return s.moderationCacheHMACKey
	}
	return nil
}
