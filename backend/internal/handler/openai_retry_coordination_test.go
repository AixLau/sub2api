package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func TestWaitForOpenAIOverloadRetryUsesControlledFallbackWhenQueueIsFull(t *testing.T) {
	scheduler := service.NewOpenAIOverloadRetryScheduler(1)
	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scheduler.Acquire(firstContext, "openai/gpt-test", time.Second)
	}()
	defer cancelFirst()

	deadline := time.Now().Add(time.Second)
	for {
		err := scheduler.Acquire(context.Background(), "openai/gpt-test", 0)
		if errors.Is(err, service.ErrOpenAIOverloadRetryQueueFull) {
			break
		}
		if err != nil {
			t.Fatalf("scheduler probe error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not become full before the deadline")
		}
		time.Sleep(time.Millisecond)
	}

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request = request.WithContext(context.Background())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request
	handler := &OpenAIGatewayHandler{overloadRetryScheduler: scheduler}

	started := time.Now()
	err := handler.waitForOpenAIOverloadRetry(c, "openai/gpt-test", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForOpenAIOverloadRetry() error = %v, want nil after controlled fallback", err)
	}
	if elapsed := time.Since(started); elapsed < 10*time.Millisecond {
		t.Fatalf("queue-full fallback waited %s, want at least the retry delay", elapsed)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("queue-full fallback waited %s, want bounded delay", elapsed)
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
