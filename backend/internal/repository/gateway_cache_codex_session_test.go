package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheCodexSessionIdentityExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "epoch", "first", 2*time.Hour)
	require.NoError(t, err)
	require.True(t, created)
	server.FastForward(time.Hour)
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, "epoch", "losing-writer", 7*24*time.Hour)
	require.NoError(t, err)
	require.False(t, created)
	value, err := cache.GetCodexSessionIdentity(ctx, "epoch")
	require.NoError(t, err)
	require.Equal(t, "first", value)
	require.Equal(t, time.Hour, server.TTL(openAICodexSessionIdentityPrefix+"epoch"))
	server.FastForward(time.Hour)
	_, err = cache.GetCodexSessionIdentity(ctx, "epoch")
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, "v2", "durable", 0)
	require.NoError(t, err)
	require.True(t, created)
	server.FastForward(30 * 24 * time.Hour)
	value, err = cache.GetCodexSessionIdentity(ctx, "v2")
	require.NoError(t, err)
	require.Equal(t, "durable", value, "device/off v2 mappings retain their existing lifetime")
}

func TestGatewayCacheCodexSessionIdentityCompareAndSwapLifetime(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	key := "thread-history"
	redisKey := openAICodexSessionIdentityPrefix + key

	updated, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "missing", "unexpected", 0)
	require.NoError(t, err)
	require.False(t, updated)
	require.False(t, server.Exists(redisKey))

	updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, "", "epoch-one", 2*time.Hour)
	require.NoError(t, err)
	require.True(t, updated)
	server.FastForward(time.Hour)
	for _, expected := range []string{"", "wrong-epoch", " epoch-one "} {
		updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, expected, "losing-writer", 7*24*time.Hour)
		require.NoError(t, err)
		require.False(t, updated)
		value, err := cache.GetCodexSessionIdentity(ctx, key)
		require.NoError(t, err)
		require.Equal(t, "epoch-one", value)
		require.Equal(t, time.Hour, server.TTL(redisKey), "failed comparison must not refresh expiry")
	}

	updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, "epoch-one", "epoch-two", 3*time.Hour)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, 3*time.Hour, server.TTL(redisKey))
	updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, "epoch-two", "durable", 0)
	require.NoError(t, err)
	require.True(t, updated)
	require.Zero(t, server.TTL(redisKey))
	server.FastForward(365 * 24 * time.Hour)
	value, err := cache.GetCodexSessionIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "durable", value)

	updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, "durable", "short-lived", time.Microsecond)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, time.Millisecond, server.TTL(redisKey))
	server.FastForward(time.Millisecond)
	require.False(t, server.Exists(redisKey))
	updated, err = cache.CompareAndSwapCodexSessionIdentity(ctx, key, "", "recreated", 0)
	require.NoError(t, err)
	require.True(t, updated)
}

func TestGatewayCacheCodexSessionIdentityCompareAndSwapConcurrentWriters(t *testing.T) {
	for _, expected := range []string{"", "previous-epoch"} {
		t.Run(fmt.Sprintf("expected=%q", expected), func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			cache := &gatewayCache{rdb: client}
			ctx := context.Background()
			if expected != "" {
				created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "history", expected, 0)
				require.NoError(t, err)
				require.True(t, created)
			}
			type result struct {
				value   string
				updated bool
				err     error
			}
			const writers = 16
			start := make(chan struct{})
			results := make(chan result, writers)
			for i := range writers {
				go func() {
					<-start
					value := fmt.Sprintf("candidate-%d", i)
					updated, err := cache.CompareAndSwapCodexSessionIdentity(ctx, "history", expected, value, 0)
					results <- result{value: value, updated: updated, err: err}
				}()
			}
			close(start)
			var winner string
			winners := 0
			for range writers {
				got := <-results
				require.NoError(t, got.err)
				if got.updated {
					winner = got.value
					winners++
				}
			}
			require.Equal(t, 1, winners)
			value, err := cache.GetCodexSessionIdentity(ctx, "history")
			require.NoError(t, err)
			require.Equal(t, winner, value)
		})
	}
}

func TestGatewayCacheCodexSessionIdentityCompareAndSwapInvalidInput(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()
	for _, tc := range []struct {
		key, value string
		ttl        time.Duration
	}{
		{key: " ", value: "value"},
		{key: "key", value: " "},
		{key: "key", value: "value", ttl: -time.Second},
	} {
		updated, err := cache.CompareAndSwapCodexSessionIdentity(ctx, tc.key, "", tc.value, tc.ttl)
		require.Error(t, err)
		require.False(t, updated)
	}
	require.Empty(t, server.Keys())
	for _, unavailable := range []*gatewayCache{nil, {}} {
		updated, err := unavailable.CompareAndSwapCodexSessionIdentity(ctx, "key", "", "value", 0)
		require.Error(t, err)
		require.False(t, updated)
	}
}
