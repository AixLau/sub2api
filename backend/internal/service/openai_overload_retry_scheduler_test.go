package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIOverloadRetrySchedulerAppliesDelayAndJitter(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, 2, func() time.Duration { return 5 * time.Millisecond })
	started := time.Now()
	lease, err := scheduler.Acquire(context.Background(), "openai/gpt-test", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed < 12*time.Millisecond {
		t.Fatalf("retry delay = %s, want at least base delay plus jitter", elapsed)
	}
	lease.Release()
	lease.Release()
}

func TestOpenAIOverloadRetrySchedulerBoundsQueueAndHonorsCancellation(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, 1, func() time.Duration { return 0 })
	first, err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer first.Release()

	queuedCtx, cancelQueued := context.WithCancel(context.Background())
	dequeued := make(chan error, 1)
	go func() {
		_, err := scheduler.Acquire(queuedCtx, "openai/gpt-test", 0)
		dequeued <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		scheduler.mu.Lock()
		queued := scheduler.lanes["openai/gpt-test"] != nil && scheduler.lanes["openai/gpt-test"].queued >= 1
		scheduler.mu.Unlock()
		if queued || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0); !errors.Is(err, ErrOpenAIOverloadRetryQueueFull) {
		t.Fatalf("third Acquire() error = %v, want ErrOpenAIOverloadRetryQueueFull", err)
	}
	cancelQueued()
	select {
	case err := <-dequeued:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued Acquire() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued Acquire() did not observe cancellation")
	}
}

func TestOpenAIOverloadRetrySchedulerSeparatesKeys(t *testing.T) {
	scheduler := newOpenAIOverloadRetryScheduler(1, 1, func() time.Duration { return 0 })
	first, err := scheduler.Acquire(context.Background(), "openai/model-a", 0)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer first.Release()
	var acquired atomic.Bool
	done := make(chan struct{})
	go func() {
		lease, err := scheduler.Acquire(context.Background(), "openai/model-b", 0)
		if err == nil {
			acquired.Store(true)
			lease.Release()
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
}
