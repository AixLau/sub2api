package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexCleanupOutboxStub struct {
	authInvalidationRepoStub
	liveOwner bool
	guardErr  error
	guarded   []string
}

func (r *codexCleanupOutboxStub) WithCodexIdentityOwnerCleanup(ctx context.Context, owner string, cleanup func(context.Context, bool) error) error {
	r.guarded = append(r.guarded, owner)
	if r.guardErr != nil {
		return r.guardErr
	}
	return cleanup(ctx, r.liveOwner)
}

type codexOwnerCleanupStub struct {
	owners    []string
	resumed   []string
	activated []string
	err       error
}

func (s *codexOwnerCleanupStub) ActivateCodexIdentityOwner(_ context.Context, owner string) error {
	s.activated = append(s.activated, owner)
	return s.err
}

func (s *codexOwnerCleanupStub) ResumeCodexIdentityOwnerCleanup(_ context.Context, owner string) (int64, error) {
	s.resumed = append(s.resumed, owner)
	return 0, s.err
}

func (s *codexOwnerCleanupStub) DeleteCodexIdentityOwner(_ context.Context, owner string) (int64, error) {
	s.owners = append(s.owners, owner)
	return 4, s.err
}

func TestCodexIdentityCleanupOutbox_ReusesDurableDeliveryAndSafetyPass(t *testing.T) {
	for _, owner := range []string{CodexIdentityAccountOwner("chatgpt:account-a"), "user:27"} {
		t.Run(owner, func(t *testing.T) {
			repo := &codexCleanupOutboxStub{}
			cache := &authInvalidationCacheStub{}
			cleanup := &codexOwnerCleanupStub{}
			worker := NewAuthCacheInvalidationWorker(repo, cache)
			worker.codexIdentityCleanup = cleanup
			event := AuthCacheInvalidationEvent{ID: 19, EventType: AuthInvalidationEventCodexIdentityOwner, OwnerToken: owner}
			worker.processEvent(context.Background(), event)
			require.Equal(t, []int64{19}, repo.scheduled)
			require.Empty(t, repo.deleted)
			event.Stage = 1
			worker.processEvent(context.Background(), event)
			require.Equal(t, []int64{19}, repo.deleted)
			require.Equal(t, []string{owner, owner}, cleanup.owners)
			require.Empty(t, cleanup.activated, "deleted database owners remain fenced")
			require.Equal(t, cleanup.owners, repo.guarded)
			require.Empty(t, cache.deleted, "identity cleanup must not masquerade as an API-key invalidation")
			require.Empty(t, cache.published)
		})
	}
}

func TestCodexIdentityCleanupOutbox_LiveSharedAccountNamespaceIsPreserved(t *testing.T) {
	repo := &codexCleanupOutboxStub{liveOwner: true}
	cleanup := &codexOwnerCleanupStub{}
	worker := NewAuthCacheInvalidationWorker(repo, &authInvalidationCacheStub{})
	worker.codexIdentityCleanup = cleanup
	worker.processEvent(context.Background(), AuthCacheInvalidationEvent{
		ID: 21, Stage: 1, EventType: AuthInvalidationEventCodexIdentityOwner,
		OwnerToken: CodexIdentityAccountOwner("chatgpt:shared"),
	})
	require.Empty(t, cleanup.owners)
	require.Equal(t, []string{CodexIdentityAccountOwner("chatgpt:shared")}, cleanup.resumed)
	require.Equal(t, cleanup.resumed, cleanup.activated)
	require.Equal(t, []int64{21}, repo.deleted)
}

func TestCodexIdentityCleanupOutbox_RedisOrGuardFailureIsRetried(t *testing.T) {
	for _, failGuard := range []bool{false, true} {
		repo := &codexCleanupOutboxStub{}
		cleanup := &codexOwnerCleanupStub{}
		if failGuard {
			repo.guardErr = errors.New("database unavailable")
		} else {
			cleanup.err = errors.New("redis unavailable")
		}
		worker := NewAuthCacheInvalidationWorker(repo, &authInvalidationCacheStub{})
		worker.codexIdentityCleanup = cleanup
		worker.processEvent(context.Background(), AuthCacheInvalidationEvent{
			ID: 23, Stage: 1, EventType: AuthInvalidationEventCodexIdentityOwner, OwnerToken: "user:27",
		})
		require.Equal(t, []int64{23}, repo.retried)
		require.Empty(t, repo.deleted)
		require.Empty(t, repo.scheduled)
	}
}

func TestCodexIdentityCleanupOutbox_ReimportCannotActivateBeforeInterruptedCleanupFinishes(t *testing.T) {
	repo := &codexCleanupOutboxStub{liveOwner: true}
	cleanup := &codexOwnerCleanupStub{err: errors.New("Redis unavailable during cleanup resume")}
	worker := NewAuthCacheInvalidationWorker(repo, &authInvalidationCacheStub{})
	worker.codexIdentityCleanup = cleanup
	worker.processEvent(context.Background(), AuthCacheInvalidationEvent{
		ID: 24, Stage: 1, EventType: AuthInvalidationEventCodexIdentityOwner, OwnerToken: "user:27",
	})
	require.Equal(t, []string{"user:27"}, cleanup.resumed)
	require.Empty(t, cleanup.activated)
	require.Equal(t, []int64{24}, repo.retried)
	require.Empty(t, repo.deleted)
}

func TestCodexIdentityCleanupOutbox_APIKeyInvalidationNeverCleansUserIdentity(t *testing.T) {
	repo := &codexCleanupOutboxStub{}
	cleanup := &codexOwnerCleanupStub{}
	cache := &authInvalidationCacheStub{}
	worker := NewAuthCacheInvalidationWorker(repo, cache)
	worker.codexIdentityCleanup = cleanup
	worker.processEvent(context.Background(), AuthCacheInvalidationEvent{ID: 25, Stage: 1, CacheKey: "api-key-hash"})
	require.Empty(t, cleanup.owners)
	require.Empty(t, repo.guarded)
	require.Equal(t, []string{"api-key-hash"}, cache.deleted)
}

type codexIndexMaintenanceStub struct {
	codexOwnerCleanupStub
	calls int
	err   error
	count func(context.Context)
}

func (s *codexIndexMaintenanceStub) CountCodexIdentityKeys(ctx context.Context) (map[string]int64, error) {
	s.calls++
	if s.count != nil {
		s.count(ctx)
	}
	return map[string]int64{}, s.err
}

func TestCodexIdentityCleanupOutbox_IndexMaintenanceIsBoundedAndIndependentOfScrapes(t *testing.T) {
	worker := NewAuthCacheInvalidationWorker(&codexCleanupOutboxStub{}, &authInvalidationCacheStub{})
	store := &codexIndexMaintenanceStub{count: func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), authInvalidationRedisTimeout)
	}}
	worker.codexIdentityCleanup = store
	now := time.Unix(200, 0)
	worker.maintainCodexIdentityIndexes(context.Background(), now)
	worker.maintainCodexIdentityIndexes(context.Background(), now.Add(30*time.Second))
	require.Equal(t, 1, store.calls)
	worker.maintainCodexIdentityIndexes(context.Background(), now.Add(time.Minute))
	require.Equal(t, 2, store.calls)
	require.Empty(t, store.owners, "maintenance cannot begin owner deletion")
	require.Empty(t, store.resumed)
}

func TestCodexIdentityCleanupOutbox_IndexMaintenanceFailureDoesNotHotLoop(t *testing.T) {
	worker := NewAuthCacheInvalidationWorker(&codexCleanupOutboxStub{}, &authInvalidationCacheStub{})
	store := &codexIndexMaintenanceStub{err: errors.New("Redis unavailable")}
	worker.codexIdentityCleanup = store
	now := time.Unix(200, 0)
	worker.maintainCodexIdentityIndexes(context.Background(), now)
	worker.maintainCodexIdentityIndexes(context.Background(), now.Add(time.Second))
	require.Equal(t, 1, store.calls)
	require.Equal(t, uint64(1), worker.failures.Load())
	worker.maintainCodexIdentityIndexes(context.Background(), now.Add(time.Minute))
	require.Equal(t, 2, store.calls)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	worker.maintainCodexIdentityIndexes(cancelled, now.Add(2*time.Minute))
	require.Equal(t, 2, store.calls, "stopping the existing worker also stops identity maintenance")
}
