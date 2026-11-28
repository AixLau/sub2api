package service

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrOpenAIOverloadRetryQueueFull means that the shared overload retry lane is
// already at its bounded waiting capacity. The handler uses the request's
// normal retry delay as a controlled fallback instead of returning this local
// scheduling condition to the client.
var ErrOpenAIOverloadRetryQueueFull = errors.New("openai overload retry queue is full")

const (
	defaultOpenAIOverloadRetryQueueSize = 256
	defaultOpenAIOverloadRetryJitter    = 125 * time.Millisecond
	defaultOpenAIOverloadRetryQueueWait = 5 * time.Second
	defaultOpenAIOverloadRetrySpacing   = 50 * time.Millisecond
)

type openAIOverloadRetryLane struct {
	nextAt  time.Time
	pending int
}

// OpenAIOverloadRetryScheduler coordinates recognized provider-wide overload
// retries. Each provider/model lane keeps only a bounded number of pending
// retry reservations. A reservation ends when its delay has elapsed, before
// the next upstream request starts, so a slow upstream response never occupies
// scheduler capacity.
type OpenAIOverloadRetryScheduler struct {
	mu           sync.Mutex
	lanes        map[string]*openAIOverloadRetryLane
	maxQueued    int
	maxQueueWait time.Duration
	jitterFunc   func() time.Duration
}

func NewOpenAIOverloadRetryScheduler(maxQueued int) *OpenAIOverloadRetryScheduler {
	if maxQueued <= 0 {
		maxQueued = defaultOpenAIOverloadRetryQueueSize
	}
	return &OpenAIOverloadRetryScheduler{
		lanes:        make(map[string]*openAIOverloadRetryLane),
		maxQueued:    maxQueued,
		maxQueueWait: defaultOpenAIOverloadRetryQueueWait,
		jitterFunc: func() time.Duration {
			return time.Duration(time.Now().UnixNano() % int64(defaultOpenAIOverloadRetryJitter))
		},
	}
}

func NewDefaultOpenAIOverloadRetryScheduler() *OpenAIOverloadRetryScheduler {
	return NewOpenAIOverloadRetryScheduler(defaultOpenAIOverloadRetryQueueSize)
}

// newOpenAIOverloadRetryScheduler is kept package-private for deterministic
// unit tests. Production callers use NewOpenAIOverloadRetryScheduler.
func newOpenAIOverloadRetryScheduler(maxQueued int, jitter func() time.Duration) *OpenAIOverloadRetryScheduler {
	s := NewOpenAIOverloadRetryScheduler(maxQueued)
	if jitter != nil {
		s.jitterFunc = jitter
	}
	return s
}

// Acquire reserves a bounded retry start time for a provider/model lane. The
// requested provider delay is preserved, while concurrent retries are spread
// by a small interval. Only the additional queue wait is bounded, so an
// upstream Retry-After value capped by the caller remains the primary delay.
func (s *OpenAIOverloadRetryScheduler) Acquire(ctx context.Context, key string, baseDelay time.Duration) error {
	if s == nil {
		return ErrOpenAIOverloadRetryQueueFull
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		key = "openai/unknown"
	}

	if baseDelay < 0 {
		baseDelay = 0
	}
	scheduledAt := time.Now().Add(baseDelay)
	if s.jitterFunc != nil {
		if jitter := s.jitterFunc(); jitter > 0 {
			scheduledAt = scheduledAt.Add(jitter)
		}
	}

	s.mu.Lock()
	lane := s.lanes[key]
	if lane == nil {
		lane = &openAIOverloadRetryLane{}
		s.lanes[key] = lane
	}
	if lane.pending >= s.maxQueued {
		s.mu.Unlock()
		return ErrOpenAIOverloadRetryQueueFull
	}
	if lane.nextAt.After(scheduledAt) {
		scheduledAt = lane.nextAt
	}
	queueWait := time.Until(scheduledAt) - baseDelay
	if queueWait > s.maxQueueWait {
		s.mu.Unlock()
		return ErrOpenAIOverloadRetryQueueFull
	}
	lane.pending++
	lane.nextAt = scheduledAt.Add(defaultOpenAIOverloadRetrySpacing)
	s.mu.Unlock()

	finish := func() {
		s.mu.Lock()
		if lane.pending > 0 {
			lane.pending--
		}
		if lane.pending == 0 {
			delete(s.lanes, key)
		}
		s.mu.Unlock()
	}
	defer finish()

	timer := time.NewTimer(time.Until(scheduledAt))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
