package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestContentModerationOutboxRepositoryEnqueueEventsUsesDecisionEventDedup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationOutboxRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("ON CONFLICT (decision_id, event_type, event_key) DO NOTHING")).
		WithArgs(
			"decision-1",
			service.ContentModerationOutboxEventLogWrite,
			"",
			service.ContentModerationOutboxPriorityStrong,
			sqlmock.AnyArg(),
			service.ContentModerationOutboxDefaultMaxRetries(service.ContentModerationOutboxPriorityStrong),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = repo.EnqueueEvents(context.Background(), []service.ContentModerationOutboxEvent{{
		DecisionID: "decision-1",
		EventType:  service.ContentModerationOutboxEventLogWrite,
		Priority:   service.ContentModerationOutboxPriorityStrong,
		Payload:    map[string]any{"ok": true},
	}})

	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationOutboxRepositoryClaimDueEventsLocksDueRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationOutboxRepository(db)
	now := time.Now()
	created := now.Add(-time.Minute)
	nextRetry := now.Add(-time.Second)
	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE SKIP LOCKED")).
		WithArgs(now, 10, "120000 milliseconds").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "decision_id", "event_type", "event_key", "priority", "payload", "retry_count", "max_retries", "next_retry_at", "created_at",
		}).AddRow(
			int64(7),
			"decision-1",
			service.ContentModerationOutboxEventEmail,
			"violation",
			service.ContentModerationOutboxPriorityWeak,
			[]byte(`{"email_kind":"violation"}`),
			1,
			5,
			nextRetry,
			created,
		))

	events, err := repo.ClaimDueEvents(context.Background(), now, 10, 2*time.Minute)

	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(7), events[0].ID)
	require.Equal(t, service.ContentModerationOutboxEventEmail, events[0].EventType)
	require.Equal(t, "violation", events[0].Payload["email_kind"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationOutboxRepositoryRetryAndDeadLetterUpdatesStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationOutboxRepository(db)
	next := time.Now().Add(time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("SET status = 'retry'")).
		WithArgs(int64(7), 2, next, "temporary").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("SET status = 'dead_letter'")).
		WithArgs(int64(7), "permanent").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.ScheduleEventRetry(context.Background(), 7, 2, next, "temporary"))
	require.NoError(t, repo.MarkEventDeadLetter(context.Background(), 7, "permanent"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationOutboxRepositoryStatusListReplayAndCleanup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationOutboxRepository(db)
	now := time.Now()
	oldest := now.Add(-15 * time.Minute)
	deadAt := now.Add(-time.Minute)
	mock.ExpectQuery(regexp.QuoteMeta("COUNT(*) FILTER (WHERE status = 'pending')")).
		WillReturnRows(sqlmock.NewRows([]string{
			"pending", "retry", "processing", "succeeded", "dead_letter", "oldest_pending_at", "last_dead_letter_at", "last_error",
		}).AddRow(int64(1), int64(2), int64(0), int64(9), int64(1), oldest, deadAt, "boom"))

	status, err := repo.GetStatus(context.Background(), now)
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.False(t, status.Healthy)
	require.Equal(t, int64(1), status.Pending)
	require.Equal(t, int64(2), status.Retry)
	require.Equal(t, int64(1), status.DeadLetter)
	require.Equal(t, "boom", status.LastError)
	require.GreaterOrEqual(t, status.OldestPendingAgeSeconds, int64(900))

	mock.ExpectQuery(regexp.QuoteMeta("WHERE status = 'dead_letter'")).
		WithArgs(25).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "decision_id", "event_type", "event_key", "priority", "payload", "retry_count", "max_retries", "next_retry_at", "created_at", "last_error",
		}).AddRow(
			int64(7), "decision-1", service.ContentModerationOutboxEventEmail, "violation",
			service.ContentModerationOutboxPriorityWeak, []byte(`{"email_kind":"violation"}`),
			5, 5, now, oldest, "smtp down",
		))
	dead, err := repo.ListDeadLetters(context.Background(), 25)
	require.NoError(t, err)
	require.Len(t, dead, 1)
	require.Equal(t, "smtp down", dead[0].LastError)

	mock.ExpectExec(regexp.QuoteMeta("WHERE id = $1 AND status = 'dead_letter'")).
		WithArgs(int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	replayed, err := repo.ReplayDeadLetter(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, replayed)

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM content_moderation_outbox")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 100).
		WillReturnResult(sqlmock.NewResult(0, 3))
	deleted, err := repo.Cleanup(context.Background(), now.Add(-7*24*time.Hour), now.Add(-90*24*time.Hour), 100)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)

	require.NoError(t, mock.ExpectationsWereMet())
}
