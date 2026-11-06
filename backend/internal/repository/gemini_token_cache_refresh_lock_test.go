//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGeminiTokenCacheRefreshLockIsOwnerSafe(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache, ok := NewGeminiTokenCache(rdb).(service.OAuthRefreshLockCache)
	require.True(t, ok, "the Redis token cache must provide the cross-instance refresh lock")
	ctx := context.Background()

	acquired, err := cache.AcquireRefreshLock(ctx, "grok:1", "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.AcquireRefreshLock(ctx, "grok:1", "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "a held lock must not be acquired by another instance")

	// A non-owner release (e.g. a refresh that outlived its TTL) leaves the lock alone.
	require.NoError(t, cache.ReleaseRefreshLock(ctx, "grok:1", "owner-b"))
	require.True(t, mr.Exists(oauthRefreshLockKeyPrefix+"grok:1"))

	require.NoError(t, cache.ReleaseRefreshLock(ctx, "grok:1", "owner-a"))
	require.False(t, mr.Exists(oauthRefreshLockKeyPrefix+"grok:1"))

	acquired, err = cache.AcquireRefreshLock(ctx, "grok:1", "owner-c", time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	mr.FastForward(2 * time.Second)
	acquired, err = cache.AcquireRefreshLock(ctx, "grok:1", "owner-d", time.Second)
	require.NoError(t, err)
	require.True(t, acquired, "an expired lock can be taken over")
}
