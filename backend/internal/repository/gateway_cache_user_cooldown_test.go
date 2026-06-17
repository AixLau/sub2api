package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheUserAccountCooldownsArePerUserAndExpire(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })

	cache := NewGatewayCache(rdb)
	ctx := context.Background()

	require.NoError(t, cache.SetUserAccountCooldown(ctx, 11, 101, time.Minute))
	require.NoError(t, cache.SetUserAccountCooldown(ctx, 22, 202, time.Minute))

	user11, err := cache.GetUserAccountCooldowns(ctx, 11)
	require.NoError(t, err)
	require.Contains(t, user11, int64(101))
	require.NotContains(t, user11, int64(202))

	user22, err := cache.GetUserAccountCooldowns(ctx, 22)
	require.NoError(t, err)
	require.Contains(t, user22, int64(202))
	require.NotContains(t, user22, int64(101))

	mr.FastForward(time.Minute + time.Second)

	expired, err := cache.GetUserAccountCooldowns(ctx, 11)
	require.NoError(t, err)
	require.Empty(t, expired)
}
