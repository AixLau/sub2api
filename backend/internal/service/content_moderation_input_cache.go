package service

import (
	"context"
	"crypto/sha256"
	"sync"
)

const maxContentModerationInputCacheEntries = 4

type contentModerationInputCacheContextKey struct{}

type contentModerationInputCacheKey struct {
	protocol   string
	auditScope string
	bodyDigest [sha256.Size]byte
}

type contentModerationInputCache struct {
	mu              sync.Mutex
	entries         map[contentModerationInputCacheKey]ContentModerationInput
	extractObserver func()
}

func withContentModerationInputCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if contentModerationInputCacheFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, contentModerationInputCacheContextKey{}, &contentModerationInputCache{
		entries: make(map[contentModerationInputCacheKey]ContentModerationInput, 1),
	})
}

// WithFreshContentModerationInputCache starts a new logical moderation request
// on a long-lived transport such as a WebSocket connection.
func WithFreshContentModerationInputCache(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contentModerationInputCacheContextKey{}, &contentModerationInputCache{
		entries: make(map[contentModerationInputCacheKey]ContentModerationInput, 1),
	})
}

func contentModerationInputCacheFromContext(ctx context.Context) *contentModerationInputCache {
	if ctx == nil {
		return nil
	}
	cache, _ := ctx.Value(contentModerationInputCacheContextKey{}).(*contentModerationInputCache)
	return cache
}

func extractContentModerationInputCached(ctx context.Context, protocol string, body []byte, auditScope string) ContentModerationInput {
	auditScope = normalizeContentModerationAuditScope(auditScope)
	cache := contentModerationInputCacheFromContext(ctx)
	if cache == nil {
		return ExtractContentModerationInput(protocol, body, auditScope)
	}
	key := contentModerationInputCacheKey{
		protocol:   protocol,
		auditScope: auditScope,
		bodyDigest: sha256.Sum256(body),
	}
	cache.mu.Lock()
	if input, ok := cache.entries[key]; ok {
		cache.mu.Unlock()
		return input
	}
	if len(cache.entries) >= maxContentModerationInputCacheEntries {
		cache.mu.Unlock()
		return cache.extract(protocol, body, auditScope)
	}
	input := cache.extract(protocol, body, auditScope)
	cache.entries[key] = input
	cache.mu.Unlock()
	return input
}

func (c *contentModerationInputCache) extract(protocol string, body []byte, auditScope string) ContentModerationInput {
	if c.extractObserver != nil {
		c.extractObserver()
	}
	return ExtractContentModerationInput(protocol, body, auditScope)
}
