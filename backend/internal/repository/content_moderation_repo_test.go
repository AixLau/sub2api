package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildContentModerationLogWhere_BlockedIncludesAllBlockActions(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "blocked"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action IN ('block', 'keyword_block', 'hash_block')")
	require.NotContains(t, sql, "l.action = 'block'")
}

func TestBuildContentModerationLogWhere_SearchIncludesKeywordMetadata(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Search: "privacy"})

	require.Len(t, args, 10)
	sql := strings.Join(where, " AND ")
	require.NotContains(t, sql, "l.input_excerpt ILIKE")
	require.Contains(t, sql, "l.matched_keyword ILIKE")
	require.Contains(t, sql, "l.keyword_category ILIKE")
	require.Contains(t, sql, "l.keyword_action ILIKE")
	require.Contains(t, sql, "l.effective_keyword_action ILIKE")
	require.Contains(t, sql, "l.risk_context_type ILIKE")
	require.Contains(t, sql, "l.review_status ILIKE")
}

func TestBuildContentModerationLogWhere_ReviewFiltersKeywordReviews(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{
		Result:       "review",
		ReviewStatus: service.ContentModerationReviewStatusPending,
	})

	require.Equal(t, []any{service.ContentModerationReviewStatusPending}, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action = 'keyword_review'")
	require.Contains(t, sql, "l.review_status = $1")
}

func TestBuildContentModerationLogWhere_SearchCanIncludeInputExcerpt(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{
		Search:             "privacy",
		SearchInputExcerpt: true,
	})

	require.Len(t, args, 11)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.input_excerpt ILIKE")
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesHashBlock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND action <> 'hash_block'")).
		WithArgs(int64(1001), since, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, false)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesCyberPolicyWhenRequested(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND ($3::bool IS FALSE OR action <> 'cyber_policy')")).
		WithArgs(int64(1001), since, true).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, true)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryRawRequestSnapshotRoundTrip(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &contentModerationRepository{db: db}
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO content_moderation_raw_request_snapshots")).
		WithArgs(int64(42), "req-raw", "encrypted-body", 128, true).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(9), now))

	snapshot := &service.ContentModerationRawRequestSnapshot{
		LogID:         42,
		RequestID:     "req-raw",
		BodyEncrypted: "encrypted-body",
		BodyBytes:     128,
		Truncated:     true,
	}
	require.NoError(t, repo.CreateRawRequestSnapshot(context.Background(), snapshot))
	require.Equal(t, int64(9), snapshot.ID)
	require.Equal(t, now, snapshot.CreatedAt)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, log_id, request_id, body_encrypted, body_bytes, truncated, created_at")).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "log_id", "request_id", "body_encrypted", "body_bytes", "truncated", "created_at",
		}).AddRow(int64(9), int64(42), "req-raw", "encrypted-body", 128, true, now))

	got, err := repo.GetRawRequestSnapshotByLogID(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(9), got.ID)
	require.Equal(t, int64(42), got.LogID)
	require.Equal(t, "req-raw", got.RequestID)
	require.Equal(t, "encrypted-body", got.BodyEncrypted)
	require.Equal(t, 128, got.BodyBytes)
	require.True(t, got.Truncated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryListLogsIncludesRawRequestMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM content_moderation_logs l WHERE l.id IS NOT NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN content_moderation_raw_request_snapshots rs ON rs.log_id = l.id WHERE l.id IS NOT NULL")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_id", "user_id", "user_email", "api_key_id", "api_key_name", "group_id", "group_name",
			"endpoint", "provider", "model", "mode", "action", "flagged", "highest_category", "highest_score",
			"category_scores", "threshold_snapshot", "input_excerpt", "upstream_latency_ms", "error",
			"matched_keyword", "keyword_category", "keyword_severity", "keyword_action", "effective_keyword_action",
			"risk_context_type", "risk_context_reason", "review_status", "review_note", "reviewed_by", "reviewed_at",
			"violation_count", "auto_banned", "email_sent", "user_status", "queue_delay_ms",
			"raw_request_available", "raw_request_bytes", "raw_request_truncated", "created_at",
		}).AddRow(
			int64(42), "req-raw", nil, "u@example.com", nil, "H", nil, "Default",
			"/v1/responses", "openai", "gpt-5", "post_upstream", "cyber_policy", true, "cyber_policy", 1.0,
			[]byte(`{}`), []byte(`{}`), "", nil, "flagged",
			"", "", "", "", "",
			"", "", "", "", nil, nil,
			0, false, false, "active", nil,
			true, 128, true, now,
		))

	items, page, err := repo.ListLogs(context.Background(), service.ContentModerationLogFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.True(t, items[0].RawRequestAvailable)
	require.Equal(t, 128, items[0].RawRequestBytes)
	require.True(t, items[0].RawRequestTruncated)
	require.NoError(t, mock.ExpectationsWereMet())
}
