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
