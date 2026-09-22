package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func codexOwnershipTestCache(t *testing.T) (*gatewayCache, *miniredis.Miniredis, time.Time) {
	t.Helper()
	server := miniredis.RunT(t)
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}, server, now
}

func codexOwnershipTestContext(account, user string, observed time.Time) context.Context {
	return service.WithCodexIdentityOwnership(context.Background(), service.CodexIdentityOwnership{
		AccountOwner: account, UserOwner: user, ObservedAtMs: observed.UnixMilli(),
	})
}

func TestGatewayCacheCodexOwnershipAccountAndUserCleanup(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	for _, identity := range []struct{ account, user, key string }{
		{"account:a", "user:1", "v3:side-session:a1"},
		{"account:a", "user:1", "v4:side-fork:a1"},
		{"account:a", "user:1", "v4:thread-current:a1"},
		{"account:a", "user:2", "v3:side-session:a2"},
		{"account:b", "user:1", "v3:side-session:b1"},
		{"account:b", "user:2", "v3:side-session:b2"},
	} {
		ctx := codexOwnershipTestContext(identity.account, identity.user, now)
		created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, identity.key, "identity", 0)
		require.NoError(t, err)
		require.True(t, created)
	}
	deleted, err := cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
	require.NoError(t, err)
	require.EqualValues(t, 4, deleted)
	for _, key := range []string{"v3:side-session:a1", "v4:side-fork:a1", "v4:thread-current:a1", "v3:side-session:a2"} {
		require.False(t, server.Exists(openAICodexSessionIdentityPrefix+key))
		require.False(t, server.Exists(openAICodexSessionIdentityPrefix+key+codexIdentityReverseSuffix))
	}
	require.False(t, server.Exists(codexIdentityOwnerPrefix+"account:a"))
	require.EqualValues(t, 1, cache.rdb.ZCard(context.Background(), codexIdentityOwnerPrefix+"user:1").Val())
	require.EqualValues(t, 1, cache.rdb.ZCard(context.Background(), codexIdentityOwnerPrefix+"user:2").Val())
	require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:b1"))
	deleted, err = cache.DeleteCodexIdentityOwner(context.Background(), "user:1")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.False(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:b1"))
	require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:b2"))
	counts, err := cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, counts["side_session"])
	require.Zero(t, counts["side_fork"])
	require.Zero(t, counts["side_thread_current"])
	deleted, err = cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
	require.NoError(t, err)
	require.Zero(t, deleted, "owner cleanup is idempotent")
}

func TestGatewayCacheCodexOwnershipHardExpiryAndLazyAdoption(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	key := "v4:thread-current:period"
	physical := openAICodexSessionIdentityPrefix + key
	created, err := cache.SetCodexSessionIdentityIfAbsent(context.Background(), key, "existing", 2*time.Hour)
	require.NoError(t, err)
	require.True(t, created)
	counts, err := cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.Zero(t, counts["thread_current"], "unobserved pre-upgrade keys are not guessed or scanned")
	server.FastForward(time.Hour)
	server.SetTime(now.Add(time.Hour))
	value, err := cache.GetCodexSessionIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "existing", value)
	require.Equal(t, time.Hour, server.TTL(physical))
	require.Equal(t, time.Hour, server.TTL(physical+codexIdentityReverseSuffix))
	require.Zero(t, server.TTL(codexIdentityOwnerPrefix+"account:a"), "indexes keep owner links until pruning removes all counterpart memberships")
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, key, "loser", 7*24*time.Hour)
	require.NoError(t, err)
	require.False(t, created)
	updated, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "wrong", "loser", 7*24*time.Hour)
	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, time.Hour, server.TTL(physical))
	server.FastForward(time.Hour)
	server.SetTime(now.Add(2 * time.Hour))
	_, err = cache.GetCodexSessionIdentity(ctx, key)
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	require.False(t, server.Exists(physical+codexIdentityReverseSuffix))
	_, err = cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.False(t, server.Exists(codexIdentityOwnerPrefix+"account:a"))
	require.False(t, server.Exists(codexIdentityOwnerPrefix+"user:1"))
	require.False(t, server.Exists(codexIdentityKindPrefix+"thread_current"))
}

func TestGatewayCacheCodexOwnershipAbsoluteExpiry(t *testing.T) {
	cache, server, observed := codexOwnershipTestCache(t)
	owner := service.CodexIdentityOwnership{
		AccountOwner: "account:a", UserOwner: "user:1", ObservedAtMs: observed.UnixMilli(),
		ExpiresAtMs: observed.Add(2 * time.Hour).UnixMilli(),
	}
	ctx := service.WithCodexIdentityOwnership(context.Background(), owner)
	server.SetTime(observed.Add(time.Hour))
	created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "period", "value", 2*time.Hour)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, time.Hour, server.TTL(openAICodexSessionIdentityPrefix+"period"), "queued writes must not extend epoch deadline")
	server.FastForward(time.Hour)
	server.SetTime(observed.Add(2 * time.Hour))
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, "period", "expired", 2*time.Hour)
	require.ErrorContains(t, err, "expiry elapsed")
	require.False(t, created)
	require.False(t, server.Exists(openAICodexSessionIdentityPrefix+"period"))
	_, err = cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.Empty(t, server.Keys(), "expired queued requests must not create durable keys or indexes")
}

func TestGatewayCacheCodexOwnershipHistoryReadsDoNotRefresh(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	key := "v4:thread-history:root"
	created, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "", "observed-100", 180*24*time.Hour)
	require.NoError(t, err)
	require.True(t, created)
	server.FastForward(90 * 24 * time.Hour)
	server.SetTime(now.Add(90 * 24 * time.Hour))
	for range 3 {
		value, err := cache.GetCodexSessionIdentity(ctx, key)
		require.NoError(t, err)
		require.Equal(t, "observed-100", value)
	}
	require.Equal(t, 90*24*time.Hour, server.TTL(openAICodexSessionIdentityPrefix+key))
	advanced, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "observed-100", "observed-200", 180*24*time.Hour)
	require.NoError(t, err)
	require.True(t, advanced)
	stale, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "observed-100", "stale", 365*24*time.Hour)
	require.NoError(t, err)
	require.False(t, stale)
	require.Equal(t, 180*24*time.Hour, server.TTL(openAICodexSessionIdentityPrefix+key))
	value, err := cache.GetCodexSessionIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "observed-200", value)
}

func TestGatewayCacheCodexOwnershipAdoptsDurableHistoryDeadline(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	key := "v4:thread-history:old"
	physical := openAICodexSessionIdentityPrefix + key
	observed := now.Add(-90 * 24 * time.Hour)
	record, err := json.Marshal(map[string]any{"observed_at_ms": observed.UnixMilli(), "thread_id": "old-thread"})
	require.NoError(t, err)
	_, err = cache.SetCodexSessionIdentityIfAbsent(context.Background(), key, string(record), 0)
	require.NoError(t, err)
	owner := service.CodexIdentityOwnership{AccountOwner: "account:a", UserOwner: "user:1", ObservedAtMs: now.UnixMilli(), HistoryRetentionMs: (180 * 24 * time.Hour).Milliseconds()}
	ctx := service.WithCodexIdentityOwnership(context.Background(), owner)
	value, err := cache.GetCodexSessionIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, string(record), value, "adoption must not rewrite the stored observation")
	require.Equal(t, 90*24*time.Hour, server.TTL(physical))
	server.FastForward(24 * time.Hour)
	server.SetTime(now.Add(24 * time.Hour))
	owner.ObservedAtMs = now.Add(24 * time.Hour).UnixMilli()
	ctx = service.WithCodexIdentityOwnership(context.Background(), owner)
	for range 3 {
		value, err = cache.GetCodexSessionIdentity(ctx, key)
		require.NoError(t, err)
		require.Equal(t, string(record), value)
	}
	require.Equal(t, 89*24*time.Hour, server.TTL(physical), "later lookups never extend the historical deadline")
	require.Equal(t, 89*24*time.Hour, server.TTL(physical+codexIdentityReverseSuffix))

	finiteKey := "v4:thread-history:finite"
	_, err = cache.SetCodexSessionIdentityIfAbsent(ctx, finiteKey, string(record), time.Hour)
	require.NoError(t, err)
	_, err = cache.GetCodexSessionIdentity(ctx, finiteKey)
	require.NoError(t, err)
	require.Equal(t, time.Hour, server.TTL(openAICodexSessionIdentityPrefix+finiteKey), "adoption must never lengthen a finite TTL")
}

func TestGatewayCacheCodexOwnershipExpiresOldDurableHistoryOnAdoption(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	key := "v4:thread-history:expired-old"
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	record := fmt.Sprintf(`{"observed_at_ms":%d}`, now.Add(-181*24*time.Hour).UnixMilli())
	_, err := cache.SetCodexSessionIdentityIfAbsent(ctx, key, record, 0)
	require.NoError(t, err)
	owner, _ := service.CodexIdentityOwnershipFromContext(ctx)
	owner.HistoryRetentionMs = (180 * 24 * time.Hour).Milliseconds()
	ctx = service.WithCodexIdentityOwnership(context.Background(), owner)
	_, err = cache.GetCodexSessionIdentity(ctx, key)
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	require.Empty(t, server.Keys(), "expired old history is removed with all its existing index links")

	_, err = cache.SetCodexSessionIdentityIfAbsent(context.Background(), key, "invalid-json", 0)
	require.NoError(t, err)
	value, err := cache.GetCodexSessionIdentity(ctx, key)
	require.NoError(t, err)
	require.Equal(t, "invalid-json", value, "invalid history must reach service validation instead of being silently treated as absent")
	require.Zero(t, server.TTL(openAICodexSessionIdentityPrefix+key))
}

func TestGatewayCacheCodexOwnershipRetirementFences(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	require.NoError(t, cache.ActivateCodexIdentityOwner(context.Background(), "account:a"))
	require.Empty(t, server.Keys(), "activating a never-retired owner must not create metadata")
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "v3:side-session:root", "side", 0)
	require.NoError(t, err)
	require.True(t, created)
	_, err = cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
	require.NoError(t, err)
	_, err = cache.GetCodexSessionIdentity(ctx, "v3:side-session:root")
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired)
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, "v3:side-session:root", "orphan", 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired)
	require.False(t, created)
	updated, err := cache.CompareAndSwapCodexSessionIdentity(ctx, "v4:thread-history:root", "", "orphan", time.Hour)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired)
	require.False(t, updated)
	require.False(t, server.Exists(codexIdentityOwnerPrefix+"account:a"))
	server.SetTime(now.Add(time.Second))
	future := codexOwnershipTestContext("account:a", "user:1", now.Add(time.Second))
	created, err = cache.SetCodexSessionIdentityIfAbsent(future, "v3:side-session:root", "fresh", 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "a newer observation from stale auth or account cache cannot revive a retired owner")
	require.False(t, created)
	require.NoError(t, cache.ActivateCodexIdentityOwner(context.Background(), "account:a"))
	created, err = cache.SetCodexSessionIdentityIfAbsent(future, "v3:side-session:root", "fresh", 0)
	require.NoError(t, err, "only database-confirmed reactivation enables a legitimately reused namespace")
	require.True(t, created)
	created, err = cache.SetCodexSessionIdentityIfAbsent(ctx, "v3:side-session:old-request", "orphan", 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "reactivation must retain cutoff protection against pre-retirement requests")
	require.False(t, created)
	_, err = beginCodexIdentityCleanupScript.Run(context.Background(), cache.rdb, []string{codexIdentityFencePrefix + "user:1"}).Result()
	require.NoError(t, err)
	require.ErrorIs(t, cache.ActivateCodexIdentityOwner(context.Background(), "user:1"), service.ErrCodexIdentityOwnerRetired, "reactivation cannot bypass an unfinished cleanup")
	newer := codexOwnershipTestContext("account:a", "user:1", now.Add(time.Hour))
	created, err = cache.SetCodexSessionIdentityIfAbsent(newer, "v3:side-session:other", "half-cleaned", 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "all observations are fenced while cleanup is incomplete")
	require.False(t, created)
	deleted, err := cache.DeleteCodexIdentityOwner(context.Background(), "user:1")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
}

func TestGatewayCacheCodexOwnershipCountsPruneExpiredMembers(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "v3:side-session:durable", "side", 0)
	require.NoError(t, err)
	require.True(t, created)
	for i := range 300 {
		_, err := cache.SetCodexSessionIdentityIfAbsent(ctx, fmt.Sprintf("v4:thread-history:%d", i), "history", time.Hour)
		require.NoError(t, err)
	}
	counts, err := cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 300, counts["thread_history"])
	require.EqualValues(t, 1, counts["side_session"])
	server.FastForward(time.Hour)
	server.SetTime(now.Add(time.Hour))
	counts, err = cache.CountCodexIdentityKeys(context.Background())
	require.NoError(t, err)
	require.Zero(t, counts["thread_history"])
	// A bounded global-kind prune also removes the expired tuples from both
	// owner indexes, even though their reverse metadata has already expired.
	require.EqualValues(t, 45, cache.rdb.ZCard(context.Background(), codexIdentityOwnerPrefix+"account:a").Val())
	_, err = cache.GetCodexSessionIdentity(ctx, "v3:side-session:durable")
	require.NoError(t, err)
	require.EqualValues(t, 1, cache.rdb.ZCard(context.Background(), codexIdentityOwnerPrefix+"account:a").Val())
	require.EqualValues(t, 1, cache.rdb.ZCard(context.Background(), codexIdentityOwnerPrefix+"user:1").Val())
	deleted, err := cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.False(t, server.Exists(codexIdentityOwnerPrefix+"user:1"))
}

func TestGatewayCacheCodexOwnershipExpiredCounterpartsDoNotOutliveCleanup(t *testing.T) {
	for _, cleanup := range []string{"owner", "metrics"} {
		t.Run(cleanup, func(t *testing.T) {
			cache, server, now := codexOwnershipTestCache(t)
			userIndex := codexIdentityOwnerPrefix + "user:1"
			accountA := codexOwnershipTestContext("account:a", "user:1", now)
			accountB := codexOwnershipTestContext("account:b", "user:1", now)
			_, err := cache.SetCodexSessionIdentityIfAbsent(accountB, "v3:side-session:b", "side", 0)
			require.NoError(t, err)
			_, err = cache.SetCodexSessionIdentityIfAbsent(accountA, "v4:thread-history:a", "history", time.Hour)
			require.NoError(t, err)
			server.FastForward(time.Hour)
			server.SetTime(now.Add(time.Hour))
			require.False(t, server.Exists(openAICodexSessionIdentityPrefix+"v4:thread-history:a"+codexIdentityReverseSuffix))
			require.EqualValues(t, 2, cache.rdb.ZCard(context.Background(), userIndex).Val())
			if cleanup == "owner" {
				deleted, err := cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
				require.NoError(t, err)
				require.Zero(t, deleted, "data already expired; cleanup still removes ownership debris")
			} else {
				counts, err := cache.CountCodexIdentityKeys(context.Background())
				require.NoError(t, err)
				require.Zero(t, counts["thread_history"])
			}
			require.EqualValues(t, 1, cache.rdb.ZCard(context.Background(), userIndex).Val())
			require.False(t, server.Exists(codexIdentityOwnerPrefix+"account:a"))
			require.False(t, server.Exists(codexIdentityKindPrefix+"thread_history"))
			require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:b"))
		})
	}
}

func TestGatewayCacheCodexOwnershipCleanupMultipleBatches(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	for i := range 300 {
		_, err := cache.SetCodexSessionIdentityIfAbsent(ctx, fmt.Sprintf("v4:thread-current:%d", i), "thread", 0)
		require.NoError(t, err)
	}
	deleted, err := cache.ResumeCodexIdentityOwnerCleanup(context.Background(), "account:a")
	require.NoError(t, err)
	require.Zero(t, deleted, "healthy ownership must never be retired by a resume")
	_, err = beginCodexIdentityCleanupScript.Run(context.Background(), cache.rdb, []string{codexIdentityFencePrefix + "account:a"}).Result()
	require.NoError(t, err)
	firstBatch, err := deleteCodexIdentityOwnerBatchScript.Run(context.Background(), cache.rdb, []string{
		codexIdentityOwnerPrefix + "account:a", codexIdentityFencePrefix + "account:a",
	}).Int64Slice()
	require.NoError(t, err)
	require.Equal(t, []int64{256, 0}, firstBatch)
	deleted, err = cache.ResumeCodexIdentityOwnerCleanup(context.Background(), "account:a")
	require.NoError(t, err)
	require.EqualValues(t, 44, deleted)
	require.Equal(t, []string{codexIdentityFencePrefix + "account:a"}, server.Keys(), "full graph and all reverse/counterpart indexes are removed")
	server.SetTime(now.Add(time.Second))
	fresh := codexOwnershipTestContext("account:a", "user:1", now.Add(time.Second))
	_, err = cache.SetCodexSessionIdentityIfAbsent(fresh, "v3:side-session:fresh", "fresh", 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "resuming cleanup alone must never reactivate the owner")
	require.NoError(t, cache.ActivateCodexIdentityOwner(context.Background(), "account:a"))
	_, err = cache.SetCodexSessionIdentityIfAbsent(fresh, "v3:side-session:fresh", "fresh", 0)
	require.NoError(t, err)
	deleted, err = cache.ResumeCodexIdentityOwnerCleanup(context.Background(), "account:a")
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:fresh"))
}

func TestGatewayCacheCodexOwnershipLegacyAndAPIKeyBoundaries(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	_, err := cache.SetCodexSessionIdentityIfAbsent(ctx, "v3:side-session:user", "side", 0)
	require.NoError(t, err)
	deleted, err := cache.DeleteCodexIdentityOwner(context.Background(), "api-key:77")
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"v3:side-session:user"), "API-key cleanup cannot target authenticated user's identities")
	for _, tc := range []struct {
		key    string
		legacy bool
		kind   string
	}{
		{"hash-period", false, "period_session"},
		{"hash-v2", true, "legacy_v2"},
		{"v3:thread:historical", false, "legacy_v3"},
		{"v4:side-fork:root", false, "side_fork"},
		{"v4:side-lifecycle:root", false, "side_lifecycle"},
	} {
		owner, _ := service.CodexIdentityOwnershipFromContext(ctx)
		owner.Legacy = tc.legacy
		ownedCtx := service.WithCodexIdentityOwnership(context.Background(), owner)
		_, err := cache.SetCodexSessionIdentityIfAbsent(ownedCtx, tc.key, "mapping", 0)
		require.NoError(t, err)
		counts, err := cache.CountCodexIdentityKeys(context.Background())
		require.NoError(t, err)
		require.EqualValues(t, 1, counts[tc.kind])
	}
	_, err = cache.SetCodexSessionIdentityIfAbsent(context.Background(), "unowned-v2", "device", 0)
	require.NoError(t, err)
	require.False(t, server.Exists(openAICodexSessionIdentityPrefix+"unowned-v2"+codexIdentityReverseSuffix))
	_, err = cache.DeleteCodexIdentityOwner(context.Background(), "user:1")
	require.NoError(t, err)
	require.True(t, server.Exists(openAICodexSessionIdentityPrefix+"unowned-v2"), "paths without HTTP ownership retain original behavior")
}

func TestGatewayCacheCodexOwnershipConcurrentCleanupFencesWriters(t *testing.T) {
	cache, server, now := codexOwnershipTestCache(t)
	ctx := codexOwnershipTestContext("account:a", "user:1", now)
	start := make(chan struct{})
	errorsCh := make(chan error, 64)
	var writers sync.WaitGroup
	for i := range 64 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			key := fmt.Sprintf("v4:thread-history:%d", i)
			_, err := cache.CompareAndSwapCodexSessionIdentity(ctx, key, "", "history", time.Hour)
			if err != nil && !errors.Is(err, service.ErrCodexIdentityOwnerRetired) {
				errorsCh <- err
			}
		}()
	}
	close(start)
	_, err := cache.DeleteCodexIdentityOwner(context.Background(), "account:a")
	require.NoError(t, err)
	writers.Wait()
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	require.Equal(t, []string{codexIdentityFencePrefix + "account:a"}, server.Keys(), "requests observed before retirement cannot race cleanup and recreate data or ownership indexes")
}
