//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCodexIdentityCleanup_RejectsMissingOrMalformedOwner(t *testing.T) {
	for _, owner := range []any{nil, "", "account:raw", "user:0", "user:-1"} {
		_, err := integrationDB.ExecContext(context.Background(), `
			INSERT INTO auth_cache_invalidation_outbox (cache_key, event_type, owner_token)
			VALUES (repeat('a', 64), 'codex_identity_owner', $1)
		`, owner)
		var constraintErr *pq.Error
		require.ErrorAs(t, err, &constraintErr)
		require.Equal(t, pq.ErrorCode("23514"), constraintErr.Code)
		require.Equal(t, "auth_cache_invalidation_event_kind", constraintErr.Constraint)
	}
}

func TestCodexIdentityAccountOwnerProjection(t *testing.T) {
	tokenDigest := sha256.Sum256([]byte("openai-setup-token:test-token"))
	for _, tc := range []struct {
		name, platform, accountType string
		credentials, extra          map[string]any
		scope                       string
	}{
		{name: "ChatGPT account", credentials: map[string]any{"chatgpt_account_id": " upstream "}, scope: "chatgpt:upstream"},
		{name: "ChatGPT user", credentials: map[string]any{"chatgpt_account_id": "upstream", "chatgpt_user_id": " person "}, scope: "chatgpt:upstream:user:person"},
		{name: "Unicode whitespace", credentials: map[string]any{"chatgpt_account_id": "\u0085\u00a0upstream\u3000", "chatgpt_user_id": "\tperson\u202f"}, scope: "chatgpt:upstream:user:person"},
		{name: "numeric credentials", credentials: map[string]any{"chatgpt_account_id": 123.9, "chatgpt_user_id": 45}, scope: "chatgpt:123:user:45"},
		{name: "Setup token", accountType: service.AccountTypeSetupToken, credentials: map[string]any{"access_token": " test-token "}, scope: fmt.Sprintf("setup-token:%x", tokenDigest[:16])},
		{name: "seed", extra: map[string]any{"codex_fingerprint_seed": "01962ab3-93c7-79c9-87d0-ace695843015"}, scope: "seed:01962ab3-93c7-79c9-87d0-ace695843015"},
		{name: "invalid seed", extra: map[string]any{"codex_fingerprint_seed": "00000000-0000-0000-0000-000000000000"}, scope: "platform:openai:account:17"},
		{name: "wrong seed type", extra: map[string]any{"codex_fingerprint_seed": 123}, scope: "platform:openai:account:17"},
		{name: "missing metadata", scope: "platform:openai:account:17"},
		{name: "non OpenAI", platform: "anthropic", credentials: map[string]any{"chatgpt_account_id": "upstream"}},
		{name: "API key", accountType: "apikey", credentials: map[string]any{"chatgpt_account_id": "upstream"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.platform == "" {
				tc.platform = service.PlatformOpenAI
			}
			if tc.accountType == "" {
				tc.accountType = service.AccountTypeOAuth
			}
			credentials, err := json.Marshal(tc.credentials)
			require.NoError(t, err)
			extra, err := json.Marshal(tc.extra)
			require.NoError(t, err)
			var owner sql.NullString
			require.NoError(t, integrationDB.QueryRowContext(context.Background(),
				`SELECT codex_identity_account_owner(17, $1, $2, $3::jsonb, $4::jsonb)`, tc.platform, tc.accountType, string(credentials), string(extra)).Scan(&owner))
			if tc.scope == "" {
				require.False(t, owner.Valid)
			} else {
				require.Equal(t, service.CodexIdentityAccountOwner(tc.scope), owner.String)
			}
		})
	}
}

func TestCodexIdentityCleanup_AccountDeleteAndCredentialReplacement(t *testing.T) {
	ctx := context.Background()
	suffix := uuid.NewString()
	newAccount := func(label, upstream string) *service.Account {
		return mustCreateAccount(t, integrationEntClient, &service.Account{
			Name: "codex-cleanup-" + label + suffix, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Credentials: map[string]any{"chatgpt_account_id": upstream},
		})
	}
	upstreamA, upstreamB := "cleanup-a-"+suffix, "cleanup-b-"+suffix
	a, duplicate, b := newAccount("a", upstreamA), newAccount("duplicate", upstreamA), newAccount("b", upstreamB)
	ownerA, ownerB := service.CodexIdentityAccountOwner("chatgpt:"+upstreamA), service.CodexIdentityAccountOwner("chatgpt:"+upstreamB)
	require.Equal(t, 2, codexCleanupEventCount(t, ownerA), "both credential imports enqueue ownership activation")
	require.Equal(t, 1, codexCleanupEventCount(t, ownerB))
	clearCodexOwnerEvents(t, ownerA, ownerB)
	cache := NewGatewayCache(integrationRedis).(*gatewayCache)
	cleanup := cacheAsCodexIdentityCleaner(t, cache)
	guard := NewAuthCacheInvalidationOutboxRepository(integrationDB).(*authCacheInvalidationOutboxRepository)
	userOwner := "user:991001"
	keysA := []string{"v3:side-session:" + suffix, "v3:side-fork:" + suffix, "v3:thread-history:" + suffix}
	keyB := "v3:side-session:b:" + suffix
	for _, key := range keysA {
		createCodexOwnedMapping(t, cache, ownerA, userOwner, key)
	}
	createCodexOwnedMapping(t, cache, ownerB, userOwner, keyB)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id IN ($1,$2,$3)`, a.ID, duplicate.ID, b.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox WHERE owner_token IN ($1,$2)`, ownerA, ownerB)
		_, _ = cleanup.DeleteCodexIdentityOwner(ctx, ownerA)
		_, _ = cleanup.DeleteCodexIdentityOwner(ctx, ownerB)
	})

	deleteAccount := func(id int64) {
		require.NoError(t, NewAccountRepository(integrationEntClient, integrationDB, nil).Delete(ctx, id))
	}
	cleanOwner := func(owner string) {
		require.NoError(t, guard.WithCodexIdentityOwnerCleanup(ctx, owner, func(ctx context.Context, live bool) error {
			if live {
				if _, err := cleanup.ResumeCodexIdentityOwnerCleanup(ctx, owner); err != nil {
					return err
				}
				return cleanup.ActivateCodexIdentityOwner(ctx, owner)
			}
			_, err := cleanup.DeleteCodexIdentityOwner(ctx, owner)
			return err
		}))
	}
	deleteAccount(a.ID)
	require.Equal(t, 1, codexCleanupEventCount(t, ownerA))
	cleanOwner(ownerA)
	for _, key := range keysA {
		_, err := cache.GetCodexSessionIdentity(ctx, key)
		require.NoError(t, err, "live duplicate OAuth row still owns this complete side graph")
	}
	deleteAccount(duplicate.ID)
	cleanOwner(ownerA)
	for _, key := range keysA {
		_, err := cache.GetCodexSessionIdentity(ctx, key)
		require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	}
	staleAccount := service.WithCodexIdentityOwnership(ctx, service.CodexIdentityOwnership{
		AccountOwner: ownerA, UserOwner: userOwner, ObservedAtMs: time.Now().Add(time.Hour).UnixMilli(),
	})
	_, err := cache.SetCodexSessionIdentityIfAbsent(staleAccount, "v3:side-session:stale:"+suffix, uuid.NewString(), 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "fresh requests with stale scheduler snapshots cannot revive a deleted owner")
	_, err = cache.GetCodexSessionIdentity(ctx, keyB)
	require.NoError(t, err, "unrelated Account B identity survives Account A deletion")

	// Token refresh retains ownership; replacing the actual upstream account
	// retires the old namespace even when the local account row remains present.
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = credentials || '{"access_token":"rotated"}'::jsonb WHERE id = $1`, b.ID)
	require.NoError(t, err)
	require.Zero(t, codexCleanupEventCount(t, ownerB))
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials = jsonb_build_object('chatgpt_account_id', $2::text) WHERE id = $1`, b.ID, "replacement-"+suffix)
	require.NoError(t, err)
	require.Equal(t, 1, codexCleanupEventCount(t, ownerB))
	require.Equal(t, 1, codexCleanupEventCount(t, service.CodexIdentityAccountOwner("chatgpt:replacement-"+suffix)), "replacement activates its new namespace")
	cleanOwner(ownerB)
	_, err = cache.GetCodexSessionIdentity(ctx, keyB)
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
}

func TestCodexIdentityCleanup_UserDeleteAndAPIKeyIsolation(t *testing.T) {
	ctx := context.Background()
	suffix := uuid.NewString()
	u1 := mustCreateUser(t, integrationEntClient, &service.User{Email: "codex-cleanup-a-" + suffix + "@example.com"})
	u2 := mustCreateUser(t, integrationEntClient, &service.User{Email: "codex-cleanup-b-" + suffix + "@example.com"})
	owner1, owner2 := fmt.Sprintf("user:%d", u1.ID), fmt.Sprintf("user:%d", u2.ID)
	require.Equal(t, 1, codexCleanupEventCount(t, owner1))
	require.Equal(t, 1, codexCleanupEventCount(t, owner2))
	clearCodexOwnerEvents(t, owner1, owner2)
	accountOwner := service.CodexIdentityAccountOwner("chatgpt:user-cleanup:" + suffix)
	keyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	keyA := &service.APIKey{UserID: u1.ID, Key: "sk-cleanup-a-" + suffix, Name: "first", Status: service.StatusActive}
	keyB := &service.APIKey{UserID: u1.ID, Key: "sk-cleanup-b-" + suffix, Name: "second", Status: service.StatusActive}
	require.NoError(t, keyRepo.Create(ctx, keyA))
	require.NoError(t, keyRepo.Create(ctx, keyB))
	cache := NewGatewayCache(integrationRedis).(*gatewayCache)
	cleanup := cacheAsCodexIdentityCleaner(t, cache)
	first, second := "v3:side-session:user-a:"+suffix, "v3:side-session:user-b:"+suffix
	createCodexOwnedMapping(t, cache, accountOwner, owner1, first)
	createCodexOwnedMapping(t, cache, accountOwner, owner2, second)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM api_keys WHERE user_id IN ($1,$2)`, u1.ID, u2.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id IN ($1,$2)`, u1.ID, u2.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox WHERE owner_token IN ($1,$2)`, owner1, owner2)
		_, _ = cleanup.DeleteCodexIdentityOwner(ctx, accountOwner)
	})
	require.NoError(t, keyRepo.Delete(ctx, keyA.ID))
	require.Zero(t, codexCleanupEventCount(t, owner1), "API Key deletion does not retire its authenticated User")
	_, err := cache.GetCodexSessionIdentity(ctx, first)
	require.NoError(t, err, "other API Key inherits existing User side identity")
	require.NoError(t, NewUserRepository(integrationEntClient, integrationDB).Delete(ctx, u1.ID))
	require.Equal(t, 1, codexCleanupEventCount(t, owner1))
	guard := NewAuthCacheInvalidationOutboxRepository(integrationDB).(*authCacheInvalidationOutboxRepository)
	require.NoError(t, guard.WithCodexIdentityOwnerCleanup(ctx, owner1, func(ctx context.Context, live bool) error {
		if live {
			if _, err := cleanup.ResumeCodexIdentityOwnerCleanup(ctx, owner1); err != nil {
				return err
			}
			return cleanup.ActivateCodexIdentityOwner(ctx, owner1)
		}
		_, err := cleanup.DeleteCodexIdentityOwner(ctx, owner1)
		return err
	}))
	_, err = cache.GetCodexSessionIdentity(ctx, first)
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	_, err = cache.GetCodexSessionIdentity(ctx, second)
	require.NoError(t, err, "User B identity survives User A deletion")
	staleUser := service.WithCodexIdentityOwnership(ctx, service.CodexIdentityOwnership{
		AccountOwner: accountOwner, UserOwner: owner1, ObservedAtMs: time.Now().Add(time.Hour).UnixMilli(),
	})
	_, err = cache.SetCodexSessionIdentityIfAbsent(staleUser, first+":stale", uuid.NewString(), 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "fresh requests using stale auth data cannot revive a deleted User")
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET deleted_at=NULL WHERE id=$1`, u1.ID)
	require.NoError(t, err)
	require.Equal(t, 2, codexCleanupEventCount(t, owner1), "user restoration queues a DB-guarded activation")
}

func TestCodexIdentityCleanup_InterruptedDeletionThenAccountReimport(t *testing.T) {
	ctx := context.Background()
	suffix := uuid.NewString()
	upstream := "cleanup-reimport-" + suffix
	owner := service.CodexIdentityAccountOwner("chatgpt:" + upstream)
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: "cleanup-reimport-" + suffix, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": upstream},
	})
	clearCodexOwnerEvents(t, owner)
	cache := NewGatewayCache(integrationRedis).(*gatewayCache)
	cleanup := cacheAsCodexIdentityCleaner(t, cache)
	guard := NewAuthCacheInvalidationOutboxRepository(integrationDB).(*authCacheInvalidationOutboxRepository)
	key := "v3:side-session:reimport:" + suffix
	createCodexOwnedMapping(t, cache, owner, "user:991002", key)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, account.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM auth_cache_invalidation_outbox WHERE owner_token=$1`, owner)
		_, _ = cleanup.DeleteCodexIdentityOwner(ctx, owner)
	})

	// Simulate an outbox cleanup interrupted after establishing the write fence
	// and before draining the remaining owned keys.
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, account.ID)
	require.NoError(t, err)
	require.NoError(t, beginCodexIdentityCleanupScript.Run(ctx, integrationRedis, []string{codexIdentityFencePrefix + owner}).Err())
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET deleted_at=NULL WHERE id=$1`, account.ID)
	require.NoError(t, err)
	require.Equal(t, 2, codexCleanupEventCount(t, owner), "delete and restore each enqueue lifecycle reconciliation")
	blocked := service.WithCodexIdentityOwnership(ctx, service.CodexIdentityOwnership{
		AccountOwner: owner, UserOwner: "user:991002", ObservedAtMs: time.Now().Add(time.Hour).UnixMilli(),
	})
	_, err = cache.SetCodexSessionIdentityIfAbsent(blocked, key+":before-activation", uuid.NewString(), 0)
	require.ErrorIs(t, err, service.ErrCodexIdentityOwnerRetired, "the DB restore event must be delivered before an owner can write again")
	require.NoError(t, guard.WithCodexIdentityOwnerCleanup(ctx, owner, func(ctx context.Context, live bool) error {
		require.True(t, live)
		if _, err := cleanup.ResumeCodexIdentityOwnerCleanup(ctx, owner); err != nil {
			return err
		}
		return cleanup.ActivateCodexIdentityOwner(ctx, owner)
	}))
	_, err = cache.GetCodexSessionIdentity(ctx, key)
	require.ErrorIs(t, err, service.ErrCodexSessionIdentityNotFound)
	require.Equal(t, "0", integrationRedis.HGet(ctx, codexIdentityFencePrefix+owner, "cleaning").Val(), "reimport cannot leave the cleanup fence stuck")

	// Resume-only delivery after the namespace is healthy cannot start another
	// cleanup and cannot remove its newly created graph.
	newKey := key + ":new"
	cutoff, err := integrationRedis.HGet(ctx, codexIdentityFencePrefix+owner, "cutoff").Int64()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return time.Now().UnixMilli() > cutoff }, time.Second, time.Millisecond)
	observed := service.WithCodexIdentityOwnership(ctx, service.CodexIdentityOwnership{
		AccountOwner: owner, UserOwner: "user:991002", ObservedAtMs: time.Now().UnixMilli(),
	})
	created, err := cache.SetCodexSessionIdentityIfAbsent(observed, newKey, uuid.NewString(), 0)
	require.NoError(t, err)
	require.True(t, created)
	require.NoError(t, guard.WithCodexIdentityOwnerCleanup(ctx, owner, func(ctx context.Context, live bool) error {
		require.True(t, live)
		if _, err := cleanup.ResumeCodexIdentityOwnerCleanup(ctx, owner); err != nil {
			return err
		}
		return cleanup.ActivateCodexIdentityOwner(ctx, owner)
	}))
	_, err = cache.GetCodexSessionIdentity(ctx, newKey)
	require.NoError(t, err)
}

func createCodexOwnedMapping(t *testing.T, cache *gatewayCache, accountOwner, userOwner, key string) {
	t.Helper()
	ctx := service.WithCodexIdentityOwnership(context.Background(), service.CodexIdentityOwnership{
		AccountOwner: accountOwner, UserOwner: userOwner, ObservedAtMs: time.Now().UnixMilli(),
	})
	created, err := cache.SetCodexSessionIdentityIfAbsent(ctx, key, uuid.NewString(), 0)
	require.NoError(t, err)
	require.True(t, created)
}

func cacheAsCodexIdentityCleaner(t *testing.T, cache *gatewayCache) service.CodexIdentityOwnerCleaner {
	t.Helper()
	cleanup, ok := any(cache).(service.CodexIdentityOwnerCleaner)
	require.True(t, ok)
	return cleanup
}

func codexCleanupEventCount(t *testing.T, owner string) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE event_type='codex_identity_owner' AND owner_token=$1`, owner).Scan(&count))
	return count
}

func clearCodexOwnerEvents(t *testing.T, owners ...string) {
	t.Helper()
	_, err := integrationDB.ExecContext(context.Background(), `DELETE FROM auth_cache_invalidation_outbox WHERE owner_token = ANY($1)`, pq.Array(owners))
	require.NoError(t, err)
}
