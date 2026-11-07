package service

import (
	"context"
	"time"
)

// GeminiTokenCache stores short-lived access tokens.
type GeminiTokenCache interface {
	// cacheKey should be stable for the token scope; for GeminiCli OAuth we primarily use project_id.
	GetAccessToken(ctx context.Context, cacheKey string) (string, error)
	SetAccessToken(ctx context.Context, cacheKey string, token string, ttl time.Duration) error
	DeleteAccessToken(ctx context.Context, cacheKey string) error
}

// OAuthRefreshLockCache is an optional capability of a token cache: a
// cross-instance refresh lock. owner identifies the holder so a lock that
// outlived its TTL is never released by another instance.
type OAuthRefreshLockCache interface {
	AcquireRefreshLock(ctx context.Context, cacheKey, owner string, ttl time.Duration) (bool, error)
	ReleaseRefreshLock(ctx context.Context, cacheKey, owner string) error
}
