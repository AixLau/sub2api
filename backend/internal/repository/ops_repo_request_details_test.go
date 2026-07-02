package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsRepositoryListRequestDetails_SortsByFirstToken(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &opsRepository{db: db}

	start := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectQuery(`SELECT COUNT\(1\) FROM combined`).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))

	rows := sqlmock.NewRows([]string{
		"kind",
		"created_at",
		"request_id",
		"platform",
		"model",
		"duration_ms",
		"first_token_ms",
		"status_code",
		"error_id",
		"phase",
		"severity",
		"message",
		"user_id",
		"api_key_id",
		"account_id",
		"group_id",
		"stream",
	}).
		AddRow("success", start.Add(2*time.Minute), "req-slow-ttft", "openai", "gpt-5.5", 200000, 120000, nil, nil, nil, nil, nil, int64(1), int64(2), int64(3), int64(4), true).
		AddRow("success", start.Add(time.Minute), "req-fast-ttft", "openai", "gpt-5.5", 210000, 1000, nil, nil, nil, nil, nil, int64(1), int64(2), int64(3), int64(4), true)

	mock.ExpectQuery(`ORDER BY first_token_ms DESC NULLS LAST, created_at DESC`).
		WithArgs(start, end, 50, 0).
		WillReturnRows(rows)

	items, total, err := repo.ListRequestDetails(context.Background(), &service.OpsRequestDetailFilter{
		StartTime: &start,
		EndTime:   &end,
		Sort:      "first_token_desc",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	require.Equal(t, "req-slow-ttft", items[0].RequestID)
	require.NotNil(t, items[0].FirstTokenMs)
	require.Equal(t, 120000, *items[0].FirstTokenMs)
	require.NotNil(t, items[1].FirstTokenMs)
	require.Equal(t, 1000, *items[1].FirstTokenMs)
	require.NoError(t, mock.ExpectationsWereMet())
}
