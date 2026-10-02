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

func newCodexTurnStateCache(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}, server
}

func TestGatewayCacheCodexTurnStateOriginRoundTrip(t *testing.T) {
	cache, _ := newCodexTurnStateCache(t)
	ctx := context.Background()

	// 未命中返回哨兵错误，使 service 层能区分"无记录"（守卫放行）与真实失败。
	_, err := cache.GetCodexTurnStateOrigin(ctx, "missing")
	require.ErrorIs(t, err, service.ErrCodexTurnStateOriginNotFound)

	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess-a", `{"account_id":42}`, time.Hour))
	got, err := cache.GetCodexTurnStateOrigin(ctx, "sess-a")
	require.NoError(t, err)
	require.Equal(t, `{"account_id":42}`, got)

	// 记录互相隔离：另一个下游会话不受影响。
	_, err = cache.GetCodexTurnStateOrigin(ctx, "sess-b")
	require.ErrorIs(t, err, service.ErrCodexTurnStateOriginNotFound)
}

// 写入必须是覆盖语义：同一会话在 failover 后换了铸造账号，记录要跟着走，
// 否则守卫会剥离客户端合法持有的新 blob。
func TestGatewayCacheCodexTurnStateOriginOverwrites(t *testing.T) {
	cache, _ := newCodexTurnStateCache(t)
	ctx := context.Background()

	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess", `{"account_id":42}`, time.Hour))
	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess", `{"account_id":43}`, time.Hour))

	got, err := cache.GetCodexTurnStateOrigin(ctx, "sess")
	require.NoError(t, err)
	require.Equal(t, `{"account_id":43}`, got)
}

// 过期由 Redis TTL 承担，替代了此前的进程内机会式清扫。
func TestGatewayCacheCodexTurnStateOriginExpires(t *testing.T) {
	cache, server := newCodexTurnStateCache(t)
	ctx := context.Background()

	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess", `{"account_id":42}`, 2*time.Hour))
	require.Equal(t, 2*time.Hour, server.TTL(codexTurnStateOriginPrefix+"sess"))

	server.FastForward(2*time.Hour + time.Minute)

	_, err := cache.GetCodexTurnStateOrigin(ctx, "sess")
	require.ErrorIs(t, err, service.ErrCodexTurnStateOriginNotFound)
}

func TestGatewayCacheCodexTurnStateOriginIgnoresEmptyAndNonPositiveTTL(t *testing.T) {
	cache, server := newCodexTurnStateCache(t)
	ctx := context.Background()

	// 空键/空值/非正 TTL 都是"无可记录内容"，属正常情况而非错误。
	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "", `{"account_id":42}`, time.Hour))
	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess", "", time.Hour))
	require.NoError(t, cache.SetCodexTurnStateOrigin(ctx, "sess", `{"account_id":42}`, 0))
	require.False(t, server.Exists(codexTurnStateOriginPrefix+"sess"))

	_, err := cache.GetCodexTurnStateOrigin(ctx, "")
	require.ErrorIs(t, err, service.ErrCodexTurnStateOriginNotFound)
}

func TestGatewayCacheCodexTurnStateOriginWithoutRedis(t *testing.T) {
	cache := &gatewayCache{}
	require.Error(t, cache.SetCodexTurnStateOrigin(context.Background(), "sess", `{}`, time.Hour))
	_, err := cache.GetCodexTurnStateOrigin(context.Background(), "sess")
	require.Error(t, err)
	require.NotErrorIs(t, err, service.ErrCodexTurnStateOriginNotFound,
		"存储不可用不得伪装成未命中，否则守卫会静默放行")
}