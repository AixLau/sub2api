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
