package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrOpenAIOverloadRetryQueueFull means that the shared overload retry lane is
// already at its bounded waiting capacity. Callers should retain their
// request-scoped retry behavior and use their normal delay as a safe fallback.
var ErrOpenAIOverloadRetryQueueFull = errors.New("openai overload retry queue is full")

const (
	defaultOpenAIOverloadRetryConcurrency = 8
	defaultOpenAIOverloadRetryQueueSize   = 256
	defaultOpenAIOverloadRetryJitter      = 125 * time.Millisecond
)

type openAIOverloadRetryLane struct {
	slots  chan struct{}
	queued int
}

// OpenAIOverloadRetryLease is held from the end of the retry delay until the
// next attempt has been routed (and, where possible, handed to transport).
// Release is idempotent so cancellation and early routing exits cannot leak a
// lane slot.
type OpenAIOverloadRetryLease struct {
	once    sync.Once
	release func()
}

func (l *OpenAIOverloadRetryLease) Release() {
	if l == nil || l.release == nil {
		return
	}
	l.once.Do(l.release)
}

// OpenAIOverloadRetryScheduler coordinates only recognized provider-wide
// overload retries. Lanes are keyed by provider/model so unrelated models do
// not block one another. The queue is bounded; a full queue returns an error
// and leaves the caller's existing retry delay as the fallback behavior.
type OpenAIOverloadRetryScheduler struct {
	mu         sync.Mutex
	lanes      map[string]*openAIOverloadRetryLane
	maxActive  int
	maxQueued  int
	jitterFunc func() time.Duration
}

func NewOpenAIOverloadRetryScheduler(maxActive, maxQueued int) *OpenAIOverloadRetryScheduler {
	if maxActive <= 0 {
		maxActive = defaultOpenAIOverloadRetryConcurrency
	}
	if maxQueued <= 0 {
		maxQueued = defaultOpenAIOverloadRetryQueueSize
	}
	return &OpenAIOverloadRetryScheduler{
		lanes:     make(map[string]*openAIOverloadRetryLane),
		maxActive: maxActive,
		maxQueued: maxQueued,
		jitterFunc: func() time.Duration {
			return time.Duration(time.Now().UnixNano() % int64(defaultOpenAIOverloadRetryJitter))
		},
	}
}

func NewDefaultOpenAIOverloadRetryScheduler() *OpenAIOverloadRetryScheduler {
	return NewOpenAIOverloadRetryScheduler(defaultOpenAIOverloadRetryConcurrency, defaultOpenAIOverloadRetryQueueSize)
}

// newOpenAIOverloadRetryScheduler is kept package-private for deterministic
// unit tests. Production callers use NewOpenAIOverloadRetryScheduler.
func newOpenAIOverloadRetryScheduler(maxActive, maxQueued int, jitter func() time.Duration) *OpenAIOverloadRetryScheduler {
	s := NewOpenAIOverloadRetryScheduler(maxActive, maxQueued)
	if jitter != nil {
		s.jitterFunc = jitter
	}
	return s
}

// Acquire waits for a provider/model retry lane, applies the requested base
// delay plus a small jitter, and returns a lease for the next attempt. It does
// not hold an account slot while waiting.
func (s *OpenAIOverloadRetryScheduler) Acquire(ctx context.Context, key string, baseDelay time.Duration) (*OpenAIOverloadRetryLease, error) {
	if s == nil {
		return nil, ErrOpenAIOverloadRetryQueueFull
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if key == "" {
		key = "openai/unknown"
	}

	s.mu.Lock()
	lane := s.lanes[key]
	if lane == nil {
		lane = &openAIOverloadRetryLane{slots: make(chan struct{}, s.maxActive)}
		s.lanes[key] = lane
	}
	if lane.queued >= s.maxQueued {
		s.mu.Unlock()
		return nil, ErrOpenAIOverloadRetryQueueFull
	}
	lane.queued++
	s.mu.Unlock()

	decrementQueued := func() {
		s.mu.Lock()
		if lane.queued > 0 {
			lane.queued--
		}
		if lane.queued == 0 && len(lane.slots) == 0 {
			delete(s.lanes, key)
		}
		s.mu.Unlock()
	}

	select {
	case lane.slots <- struct{}{}:
		decrementQueued()
	case <-ctx.Done():
		decrementQueued()
		return nil, ctx.Err()
	}

	delay := baseDelay
	if delay < 0 {
		delay = 0
	}
	if s.jitterFunc != nil {
		if jitter := s.jitterFunc(); jitter > 0 {
			delay += jitter
		}
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			<-lane.slots
			s.mu.Lock()
			if lane.queued == 0 && len(lane.slots) == 0 {
				delete(s.lanes, key)
			}
			s.mu.Unlock()
			return nil, ctx.Err()
		}
	}

	return &OpenAIOverloadRetryLease{release: func() {
		<-lane.slots
		s.mu.Lock()
		if lane.queued == 0 && len(lane.slots) == 0 {
			delete(s.lanes, key)
		}
		s.mu.Unlock()
	}}, nil
}
