package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationDecisionCacheStoresDecisionsAndMaintainsOwnerLease(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := NewContentModerationDecisionCache(rdb)
	require.NotNil(t, cache)

	entry := service.ContentModerationCachedDecision{
		Decision: service.ContentModerationDecision{
			Allowed:        true,
			Action:         service.ContentModerationActionAllow,
			MatchedKeyword: "configured-rule",
			CategoryScores: map[string]float64{"violence": 0.01},
		},
		DecisionID: "cm_cached_decision",
	}
	require.NoError(t, cache.Store(ctx, "opaque-hmac", entry, 30*time.Second))

	raw, err := mr.Get("moderation:decision:v1:opaque-hmac")
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &stored))
	require.Equal(t, float64(1), stored["schema_version"])
	require.NotContains(t, raw, "candidate payload")

	got, err := cache.Get(ctx, "opaque-hmac")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, entry.DecisionID, got.DecisionID)
	require.True(t, got.Decision.Allowed)
	require.Equal(t, service.ContentModerationActionAllow, got.Decision.Action)

	acquired, err := cache.TryAcquire(ctx, "opaque-hmac", "owner-a", 2*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = cache.TryAcquire(ctx, "opaque-hmac", "owner-b", 2*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)

	renewed, err := cache.Renew(ctx, "opaque-hmac", "owner-b", 30*time.Second)
	require.NoError(t, err)
	require.False(t, renewed)
	renewed, err = cache.Renew(ctx, "opaque-hmac", "owner-a", 30*time.Second)
	require.NoError(t, err)
	require.True(t, renewed)
	require.Greater(t, mr.TTL("moderation:decision-lock:v1:opaque-hmac"), 20*time.Second)

	require.NoError(t, cache.Release(ctx, "opaque-hmac", "owner-b"))
	require.True(t, mr.Exists("moderation:decision-lock:v1:opaque-hmac"))
	require.NoError(t, cache.Release(ctx, "opaque-hmac", "owner-a"))
	require.False(t, mr.Exists("moderation:decision-lock:v1:opaque-hmac"))
}

func TestNewContentModerationDecisionCacheWithoutRedisIsUnavailable(t *testing.T) {
	require.Nil(t, NewContentModerationDecisionCache(nil))
}
