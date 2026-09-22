package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexIdentityCleanupGuard_LocksAndRechecksLiveOwners(t *testing.T) {
	for _, isAccount := range []bool{false, true} {
		for _, live := range []bool{false, true} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			repo := NewAuthCacheInvalidationOutboxRepository(db).(*authCacheInvalidationOutboxRepository)
			owner := "user:17"
			if isAccount {
				owner = service.CodexIdentityAccountOwner("chatgpt:shared")
			}
			mock.ExpectBegin()
			mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(owner).WillReturnResult(sqlmock.NewResult(0, 1))
			if isAccount {
				mock.ExpectQuery("(?s)SELECT EXISTS.*FROM accounts.*codex_identity_account_owner").WithArgs(owner).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(live))
			} else {
				mock.ExpectQuery("(?s)SELECT EXISTS.*FROM users.*deleted_at IS NULL").WithArgs(int64(17)).
					WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(live))
			}
			mock.ExpectCommit()
			calls := 0
			require.NoError(t, repo.WithCodexIdentityOwnerCleanup(context.Background(), owner, func(_ context.Context, observedLive bool) error {
				require.Equal(t, live, observedLive)
				calls++
				return nil
			}))
			require.Equal(t, 1, calls, "caller receives live state under the lock to resume interrupted cleanup safely")
			require.NoError(t, mock.ExpectationsWereMet())
			_ = db.Close()
		}
	}
}

func TestCodexIdentityCleanupGuard_FailedCleanupReleasesNamespaceLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAuthCacheInvalidationOutboxRepository(db).(*authCacheInvalidationOutboxRepository)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs("user:17").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("(?s)SELECT EXISTS.*FROM users").WithArgs(int64(17)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()
	failure := errors.New("redis unavailable")
	err = repo.WithCodexIdentityOwnerCleanup(context.Background(), "user:17", func(_ context.Context, observedLive bool) error { return failure })
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexIdentityCleanupGuard_RejectsMalformedAndAPIKeyOwners(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAuthCacheInvalidationOutboxRepository(db).(*authCacheInvalidationOutboxRepository)
	for _, owner := range []string{"", "account:raw", "user:0", "user:-1", "user:01", "api-key:17"} {
		require.Error(t, repo.WithCodexIdentityOwnerCleanup(context.Background(), owner, func(_ context.Context, observedLive bool) error {
			t.Fatal("invalid owner reached Redis")
			return nil
		}))
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexIdentityCleanupOutboxRepository_ClaimsTypedCleanup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewAuthCacheInvalidationOutboxRepository(db)
	owner := service.CodexIdentityAccountOwner("chatgpt:retired")
	mock.ExpectQuery("(?s)UPDATE auth_cache_invalidation_outbox.*RETURNING.*event_type").
		WithArgs("worker", 10, int64(30)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cache_key", "attempts", "delivery_stage", "created_at", "event_type", "owner_token"}).
			AddRow(9, "unused-hash", 2, 1, time.Now(), service.AuthInvalidationEventCodexIdentityOwner, owner))
	events, err := repo.Claim(context.Background(), "worker", 10, 30*time.Second)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, service.AuthInvalidationEventCodexIdentityOwner, events[0].EventType)
	require.Equal(t, owner, events[0].OwnerToken)
	require.NoError(t, mock.ExpectationsWereMet())
}
