package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositoryGetOldestPendingContentModerationReviewAgeSeconds(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	mock.ExpectQuery(`(?s)MIN\(created_at\).*WHERE review_status = 'pending'`).
		WillReturnRows(sqlmock.NewRows([]string{"age_seconds"}).AddRow(90001.5))

	got, err := repo.GetOldestPendingContentModerationReviewAgeSeconds(context.Background())
	require.NoError(t, err)
	require.Equal(t, 90001.5, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsRepositoryGetOldestPendingContentModerationReviewAgeSecondsFailure(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	mock.ExpectQuery(`(?s)MIN\(created_at\).*WHERE review_status = 'pending'`).
		WillReturnError(errors.New("database unavailable"))

	got, err := repo.GetOldestPendingContentModerationReviewAgeSeconds(context.Background())
	require.ErrorContains(t, err, "get oldest pending content moderation review age")
	require.Zero(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
