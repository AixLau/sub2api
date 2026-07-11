package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationPassCache(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := NewContentModerationPassCache(rdb)
	opts := service.ContentModerationPassCacheOptions{Enabled: true, KeyVersion: 7}

	t.Run("pass schema ttl all or nothing and rotation", func(t *testing.T) {
		cache.StorePASS(ctx, opts, []string{"digest-a", "digest-b"})
		value, err := mr.Get("moderation:pass:v1:7:digest-a")
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(value), &fields))
		require.ElementsMatch(t, []string{"schema_version", "expires_at"}, mapKeys(fields))
		require.InDelta(t, (24 * time.Hour).Seconds(), mr.TTL("moderation:pass:v1:7:digest-a").Seconds(), 1)
		hits, err := cache.LookupPASS(ctx, opts, []string{"digest-a", "digest-b"})
		require.NoError(t, err)
		require.Equal(t, map[string]bool{"digest-a": true, "digest-b": true}, hits)
		hits, err = cache.LookupPASS(ctx, service.ContentModerationPassCacheOptions{Enabled: true, KeyVersion: 8}, []string{"digest-a"})
		require.NoError(t, err)
		require.Empty(t, hits)
		mr.Set("moderation:pass:v1:7:digest-b", "bad-json")
		hits, err = cache.LookupPASS(ctx, opts, []string{"digest-a", "digest-b"})
		require.Error(t, err)
		require.Empty(t, hits)

		mr.Set("moderation:pass:v1:7:digest-b", `{"schema_version":1,"expires_at":4102444800,"unexpected":true}`)
		hits, err = cache.LookupPASS(ctx, opts, []string{"digest-a", "digest-b"})
		require.Error(t, err)
		require.Empty(t, hits, "one malformed reply must discard every hit")
	})

	t.Run("pass ttl clamps", func(t *testing.T) {
		cache.StorePASS(ctx, service.ContentModerationPassCacheOptions{Enabled: true, KeyVersion: 7, TTL: time.Second}, []string{"ttl-min"})
		require.InDelta(t, time.Minute.Seconds(), mr.TTL("moderation:pass:v1:7:ttl-min").Seconds(), 1)
		cache.StorePASS(ctx, service.ContentModerationPassCacheOptions{Enabled: true, KeyVersion: 7, TTL: 365 * 24 * time.Hour}, []string{"ttl-max"})
		require.InDelta(t, (30 * 24 * time.Hour).Seconds(), mr.TTL("moderation:pass:v1:7:ttl-max").Seconds(), 1)
	})

	t.Run("pass command error discards every hit", func(t *testing.T) {
		cache.StorePASS(ctx, opts, []string{"command-good"})
		mr.HSet("moderation:pass:v1:7:command-bad", "field", "value")
		hits, err := cache.LookupPASS(ctx, opts, []string{"command-good", "command-bad"})
		require.Error(t, err)
		require.Empty(t, hits)
	})

	t.Run("disabled issues no commands", func(t *testing.T) {
		before := mr.CommandCount()
		disabled := service.ContentModerationPassCacheOptions{}
		_, err := cache.LookupPASS(ctx, disabled, []string{"x"})
		require.NoError(t, err)
		cache.StorePASS(ctx, disabled, []string{"x"})
		require.NoError(t, cache.DeletePASS(ctx, disabled, []string{"x"}))
		require.Equal(t, before, mr.CommandCount())
	})

	t.Run("quarantine and comparison", func(t *testing.T) {
		entry := service.ContentModerationQuarantineEntry{SchemaVersion: 1}
		require.NoError(t, cache.StoreQuarantine(ctx, opts, map[string]service.ContentModerationQuarantineEntry{"request-digest": entry}))
		got, err := cache.LookupQuarantine(ctx, opts, []string{"request-digest", "missing"})
		require.NoError(t, err)
		require.Contains(t, got, "request-digest")
		meta := service.ContentModerationComparisonMetadata{RequestID: "req-correlation", DecisionID: "decision-correlation", ChunkKeys: []string{"digest-a"}}
		require.NoError(t, cache.StoreComparisonMetadata(ctx, "req-correlation", meta))
		stored, err := cache.GetComparisonMetadata(ctx, "req-correlation")
		require.NoError(t, err)
		require.Equal(t, meta.RequestID, stored.RequestID)
		comparisonRedisKey := comparisonKey("req-correlation")
		require.NotContains(t, comparisonRedisKey, "req-correlation")
		require.InDelta(t, (30 * 24 * time.Hour).Seconds(), mr.TTL(comparisonRedisKey).Seconds(), 1)

		mr.Set("moderation:quarantine:v1:7:bad", "bad-json")
		got, err = cache.LookupQuarantine(ctx, opts, []string{"request-digest", "bad"})
		require.Error(t, err)
		require.Empty(t, got, "one malformed quarantine reply must discard every hit")

		mr.HSet("moderation:quarantine:v1:7:wrong-type", "field", "value")
		got, err = cache.LookupQuarantine(ctx, opts, []string{"request-digest", "missing", "wrong-type"})
		require.Error(t, err)
		require.Empty(t, got, "a command error in mixed hit/miss/error replies must discard every hit")

		tooManyChunks := meta
		tooManyChunks.ChunkKeys = make([]string, maxComparisonChunkKeys+1)
		require.Error(t, cache.StoreComparisonMetadata(ctx, "oversized", tooManyChunks))
		tooManyRiskTypes := meta
		tooManyRiskTypes.RiskTypes = make([]string, maxComparisonRiskTypes+1)
		require.Error(t, cache.StoreComparisonMetadata(ctx, "oversized", tooManyRiskTypes))
		tooLarge := meta
		tooLarge.RequestHMAC = strings.Repeat("a", maxComparisonMetadataBytes)
		require.Error(t, cache.StoreComparisonMetadata(ctx, "oversized", tooLarge))

		oversizedJSON, err := json.Marshal(tooManyChunks)
		require.NoError(t, err)
		mr.Set(comparisonKey("corrupt-oversized"), string(oversizedJSON))
		_, err = cache.GetComparisonMetadata(ctx, "corrupt-oversized")
		require.Error(t, err)
	})

	t.Run("pass deletion reports failure", func(t *testing.T) {
		deadRedis := miniredis.RunT(t)
		deadClient := redis.NewClient(&redis.Options{Addr: deadRedis.Addr(), MaxRetries: -1})
		deadCache := NewContentModerationPassCache(deadClient)
		deadRedis.Close()
		deleteCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		require.Error(t, deadCache.DeletePASS(deleteCtx, opts, []string{"digest"}))
	})

	t.Run("cache values contain no content or identity fields", func(t *testing.T) {
		cache.StorePASS(ctx, opts, []string{"privacy-digest"})
		require.NoError(t, cache.StoreQuarantine(ctx, opts, map[string]service.ContentModerationQuarantineEntry{"privacy-request": {}}))
		metadata := service.ContentModerationComparisonMetadata{RequestID: "request-opaque", DecisionID: "decision-opaque", RequestHMAC: "request-hmac", ChunkKeys: []string{"chunk-hmac"}}
		require.NoError(t, cache.StoreComparisonMetadata(ctx, "correlation-secret", metadata))
		comparisonRedisKey := comparisonKey("correlation-secret")
		require.NotContains(t, comparisonRedisKey, "correlation-secret")
		for _, key := range []string{"moderation:pass:v1:7:privacy-digest", "moderation:quarantine:v1:7:privacy-request", comparisonRedisKey} {
			value, err := mr.Get(key)
			require.NoError(t, err)
			for _, forbidden := range []string{"prompt", "content", "verdict", "email", "user_id", "1914823683@qq.com"} {
				require.NotContains(t, strings.ToLower(key+value), forbidden)
			}
		}
		comparisonValue, err := mr.Get(comparisonRedisKey)
		require.NoError(t, err)
		var comparisonFields map[string]any
		require.NoError(t, json.Unmarshal([]byte(comparisonValue), &comparisonFields))
		require.Contains(t, comparisonFields, "schema_version")
		comparisonFields["unexpected"] = true
		corrupt, err := json.Marshal(comparisonFields)
		require.NoError(t, err)
		mr.Set(comparisonRedisKey, string(corrupt))
		_, err = cache.GetComparisonMetadata(ctx, "correlation-secret")
		require.Error(t, err)
	})

	t.Run("connection failure is not a cache miss", func(t *testing.T) {
		deadRedis := miniredis.RunT(t)
		deadClient := redis.NewClient(&redis.Options{Addr: deadRedis.Addr(), MaxRetries: -1})
		deadCache := NewContentModerationPassCache(deadClient)
		deadRedis.Close()
		lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		hits, err := deadCache.LookupPASS(lookupCtx, opts, []string{"digest"})
		require.Error(t, err)
		require.Empty(t, hits)
		quarantine, err := deadCache.LookupQuarantine(lookupCtx, opts, []string{"digest"})
		require.Error(t, err)
		require.Empty(t, quarantine)
	})
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
