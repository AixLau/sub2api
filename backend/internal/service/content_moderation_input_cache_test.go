package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationInputCacheConcurrentIdentityExtractsOnce(t *testing.T) {
	ctx := withContentModerationInputCache(context.Background())
	cache := contentModerationInputCacheFromContext(ctx)
	var extractCalls atomic.Int64
	cache.extractObserver = func() {
		extractCalls.Add(1)
	}

	body := []byte(`{"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	start := make(chan struct{})
	errs := make(chan error, 32)
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	ready.Add(32)
	workers.Add(32)
	for range 32 {
		go func() {
			defer workers.Done()
			ready.Done()
			<-start
			input := extractContentModerationInputCached(
				ctx,
				ContentModerationProtocolOpenAIResponses,
				body,
				ContentModerationAuditScopeUserOnly,
			)
			if input.Text != "hello" {
				errs <- fmt.Errorf("unexpected extracted text %q", input.Text)
			}
		}()
	}
	ready.Wait()
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int64(1), extractCalls.Load(), "the same request identity must run the real extractor once")
	cache.mu.Lock()
	require.Len(t, cache.entries, 1)
	cache.mu.Unlock()
}

func TestContentModerationInputCacheBypassesNewIdentitiesWhenFull(t *testing.T) {
	ctx := withContentModerationInputCache(context.Background())
	cache := contentModerationInputCacheFromContext(ctx)
	var extractCalls atomic.Int64
	cache.extractObserver = func() {
		extractCalls.Add(1)
	}

	for i := range maxContentModerationInputCacheEntries {
		body := []byte(fmt.Sprintf(`{"input":"cached-%d"}`, i))
		input := extractContentModerationInputCached(
			ctx,
			ContentModerationProtocolOpenAIResponses,
			body,
			ContentModerationAuditScopeUserOnly,
		)
		require.Equal(t, fmt.Sprintf("cached-%d", i), input.Text)
	}
	require.Equal(t, int64(maxContentModerationInputCacheEntries), extractCalls.Load())

	firstBody := []byte(`{"input":"cached-0"}`)
	first := extractContentModerationInputCached(
		ctx,
		ContentModerationProtocolOpenAIResponses,
		firstBody,
		ContentModerationAuditScopeUserOnly,
	)
	require.Equal(t, "cached-0", first.Text)
	require.Equal(t, int64(maxContentModerationInputCacheEntries), extractCalls.Load(), "full caches must still serve existing identities")

	overflowBody := []byte(`{"input":"overflow"}`)
	for range 2 {
		overflow := extractContentModerationInputCached(
			ctx,
			ContentModerationProtocolOpenAIResponses,
			overflowBody,
			ContentModerationAuditScopeUserOnly,
		)
		require.Equal(t, "overflow", overflow.Text)
	}
	require.Equal(
		t,
		int64(maxContentModerationInputCacheEntries+2),
		extractCalls.Load(),
		"each overflow identity lookup must bypass the bounded cache",
	)

	cache.mu.Lock()
	require.Len(t, cache.entries, maxContentModerationInputCacheEntries)
	cache.mu.Unlock()
}
