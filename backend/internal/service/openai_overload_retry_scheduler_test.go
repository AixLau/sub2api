package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIOverloadRetrySchedulerAppliesDelayAndJitter(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(2, func() time.Duration { return 5 * time.Millisecond })
	started := time.Now()
	if err := scheduler.Acquire(context.Background(), "openai/gpt-test", 10*time.Millisecond); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 12*time.Millisecond {
		t.Fatalf("retry delay = %s, want at least base delay plus jitter", elapsed)
	}
}

func TestOpenAIOverloadRetrySchedulerBoundsPendingQueueAndHonorsCancellation(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, func() time.Duration { return 0 })
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.Acquire(firstCtx, "openai/gpt-test", 200*time.Millisecond)
	}()
	waitForPendingRetry(t, scheduler, "openai/gpt-test")

	if err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0); !errors.Is(err, ErrOpenAIOverloadRetryQueueFull) {
		t.Fatalf("second Acquire() error = %v, want ErrOpenAIOverloadRetryQueueFull", err)
	}

	cancelFirst()
	select {
	case err := <-firstDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first Acquire() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first Acquire() did not observe cancellation")
	}
}

func TestOpenAIOverloadRetrySchedulerBoundsAdditionalQueueWait(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(2, func() time.Duration { return 0 })
	scheduler.maxQueueWait = 10 * time.Millisecond
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.Acquire(context.Background(), "openai/gpt-test", 200*time.Millisecond)
	}()
	waitForPendingRetry(t, scheduler, "openai/gpt-test")

	started := time.Now()
	err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0)
	if !errors.Is(err, ErrOpenAIOverloadRetryQueueFull) {
		t.Fatalf("queued Acquire() error = %v, want ErrOpenAIOverloadRetryQueueFull", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("queued Acquire() waited %s, want bounded wait", elapsed)
	}
	if err := <-firstDone; err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
}

func TestOpenAIOverloadRetrySchedulerDoesNotRetainCapacityAfterReservation(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, func() time.Duration { return 0 })
	if err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0); err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	started := time.Now()
	if err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0); err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("second Acquire() waited %s, want no retained active slot", elapsed)
	}
}

func TestOpenAIOverloadRetrySchedulerSeparatesKeys(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, func() time.Duration { return 0 })
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.Acquire(context.Background(), "openai/model-a", 200*time.Millisecond)
	}()
	waitForPendingRetry(t, scheduler, "openai/model-a")

	var acquired atomic.Bool
	done := make(chan struct{})
	go func() {
		if err := scheduler.Acquire(context.Background(), "openai/model-b", 0); err == nil {
			acquired.Store(true)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("independent model lane was blocked")
	}
	if !acquired.Load() {
		t.Fatal("independent model lane did not acquire")
	}

	if err := <-firstDone; err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
}

func waitForPendingRetry(t *testing.T, scheduler *OpenAIOverloadRetryScheduler, key string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		scheduler.mu.Lock()
		pending := scheduler.lanes[key] != nil && scheduler.lanes[key].pending > 0
		scheduler.mu.Unlock()
		if pending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("retry lane %q did not become pending", key)
		}
		time.Sleep(time.Millisecond)
	}
}
