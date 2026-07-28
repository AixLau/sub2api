package repository

import (
	"context"
	"encoding/json"
	"fmt"
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
	require.Contains(t, sql, "l.action IN ('block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked')")
	require.NotContains(t, sql, "l.action = 'block'")
}

func TestBuildContentModerationLogWhere_HitExcludesBlockedAndPendingReview(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "hit"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.flagged = TRUE")
	require.Contains(t, sql, "l.action NOT IN ('block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked')")
	require.Contains(t, sql, "l.action NOT IN ('keyword_review', 'prompt_filter_review', 'semantic_review_review')")
	require.Contains(t, sql, "COALESCE(l.review_status, '') <> 'pending'")
	require.Contains(t, sql, "l.error = ''")
}

func TestBuildContentModerationLogWhere_FiltersDecisionSource(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{DecisionSource: "semantic_review"})

	require.Equal(t, []any{"semantic_review"}, args)
	require.Contains(t, strings.Join(where, " AND "), "l.decision_source = $1")
}

func TestBuildContentModerationLogWhere_ErrorExcludesBlockedAndPendingReview(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "error"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.error <> ''")
	require.Contains(t, sql, "l.action NOT IN ('block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked')")
	require.Contains(t, sql, "l.action NOT IN ('keyword_review', 'prompt_filter_review', 'semantic_review_review')")
	require.Contains(t, sql, "COALESCE(l.review_status, '') <> 'pending'")
	require.NotContains(t, sql, "metadata")
}

func TestBuildContentModerationLogWhere_PassExcludesBlockedAndPendingReview(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "pass"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.flagged = FALSE AND l.error = ''")
	require.Contains(t, sql, "l.action NOT IN ('block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked')")
	require.Contains(t, sql, "l.action NOT IN ('keyword_review', 'prompt_filter_review', 'semantic_review_review')")
	require.Contains(t, sql, "COALESCE(l.review_status, '') <> 'pending'")
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

func TestBuildContentModerationLogWhere_ReviewIncludesSemanticReviews(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{
		Result:       "review",
		ReviewStatus: service.ContentModerationReviewStatusPending,
	})

	require.Equal(t, []any{service.ContentModerationReviewStatusPending}, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action IN ('keyword_review', 'prompt_filter_review', 'semantic_review_review')")
	require.Contains(t, sql, "l.review_status = 'pending'")
	require.Contains(t, sql, "l.action NOT IN ('block', 'hash_block', 'keyword_block', 'prompt_filter_block', 'semantic_review_reject', 'semantic_review_deferred', 'semantic_review_unavailable', 'semantic_review_incomplete', 'cyber_policy', 'cyber_policy_session_blocked')")
	require.Contains(t, sql, "l.review_status = $1")
}

func TestBuildContentModerationLogWhere_KeywordReviewAliasRemainsNarrow(t *testing.T) {
	where, args := buildContentModerationLogWhere(service.ContentModerationLogFilter{Result: "keyword_review"})

	require.Empty(t, args)
	sql := strings.Join(where, " AND ")
	require.Contains(t, sql, "l.action = 'keyword_review'")
	require.NotContains(t, sql, "prompt_filter_review")
	require.NotContains(t, sql, "semantic_review_review")
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

func TestContentModerationRepositoryCountFlaggedByUserSince_RequiresCandidateViolationEligibility(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "content_moderation_count_candidate_eligible_violations" {
			return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
		}
		normalizedSQL := strings.Join(strings.Fields(actualSQL), " ")
		eligibilityClause := "COALESCE(source_origin, '') = '' OR user_violation_eligible = TRUE OR review_status = 'confirmed_violation'"
		if count := strings.Count(normalizedSQL, eligibilityClause); count != 2 {
			return fmt.Errorf("expected candidate eligibility clause in both last_auto_ban and main count, found %d occurrences in: %s", count, actualSQL)
		}
		observeClause := "COALESCE(mode, '') <> 'observe' OR review_status = 'confirmed_violation'"
		if count := strings.Count(normalizedSQL, observeClause); count != 2 {
			return fmt.Errorf("expected observe exclusion in both last_auto_ban and main count, found %d occurrences in: %s", count, actualSQL)
		}
		return nil
	})

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery("content_moderation_count_candidate_eligible_violations").
		WithArgs(int64(1001), since, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, false)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCountFlaggedByUserSince_ExcludesUnconfirmedReviews(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "content_moderation_count_confirmed_violations" {
			return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
		}
		required := []string{
			"AND (action <> 'semantic_review_deferred' OR review_status = 'confirmed_violation')",
			"AND COALESCE(review_status, '') NOT IN ('pending', 'false_positive')",
		}
		for _, fragment := range required {
			if !strings.Contains(actualSQL, fragment) {
				return fmt.Errorf("expected query to contain %q, got: %s", fragment, actualSQL)
			}
		}
		return nil
	})

	tests := []struct {
		name  string
		count int
	}{
		{name: "pending review excluded", count: 0},
		{name: "false positive excluded", count: 0},
		{name: "deferred pending review excluded", count: 0},
		{name: "deferred legacy empty status excluded", count: 0},
		{name: "deferred confirmed violation retained", count: 1},
		{name: "ordinary confirmed violation retained", count: 1},
		{name: "legacy empty review status retained", count: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			repo := NewContentModerationRepository(db)
			since := time.Now().Add(-time.Hour)
			mock.ExpectQuery("content_moderation_count_confirmed_violations").
				WithArgs(int64(1001), since, false).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tt.count))

			count, err := repo.CountFlaggedByUserSince(context.Background(), 1001, since, false)

			require.NoError(t, err)
			require.Equal(t, tt.count, count)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestContentModerationRepositoryViolationUpdatesAreMonotonic(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("SET violation_count = GREATEST(COALESCE(violation_count, 0), $1)")).
		WithArgs(7, "decision-count").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("auto_banned = COALESCE(auto_banned, FALSE) OR $2")).
		WithArgs(9, false, "decision-ban").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateLogViolationCountByDecisionID(context.Background(), "decision-count", 7))
	require.NoError(t, repo.UpdateLogAccountActionByDecisionID(context.Background(), "decision-ban", 9, false))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryDecisionUpdatesRequireExistingAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	mock.ExpectExec(regexp.QuoteMeta("SET violation_count = GREATEST(COALESCE(violation_count, 0), $1)")).
		WithArgs(7, "missing-count").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("auto_banned = COALESCE(auto_banned, FALSE) OR $2")).
		WithArgs(9, true, "missing-ban").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SET email_sent = $1 WHERE decision_id = $2")).
		WithArgs(true, "missing-email").
		WillReturnResult(sqlmock.NewResult(0, 0))

	require.ErrorContains(t, repo.UpdateLogViolationCountByDecisionID(context.Background(), "missing-count", 7), "expected 1 row")
	require.ErrorContains(t, repo.UpdateLogAccountActionByDecisionID(context.Background(), "missing-ban", 9, true), "expected 1 row")
	require.ErrorContains(t, repo.UpdateLogEmailSentByDecisionID(context.Background(), "missing-email", true), "expected 1 row")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryGetsAutoBanStateByDecisionID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE decision_id = $1")).
		WithArgs("decision-ban-state").
		WillReturnRows(sqlmock.NewRows([]string{"auto_banned"}).AddRow(true))

	autoBanned, err := repo.(*contentModerationRepository).GetLogAutoBannedByDecisionID(context.Background(), "decision-ban-state")

	require.NoError(t, err)
	require.True(t, autoBanned)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryTracksNotificationDeliveryByKind(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db).(*contentModerationRepository)
	mock.ExpectQuery(regexp.QuoteMeta("metadata->'notification_deliveries'->>$2")).
		WithArgs("decision-notification", "email_account_disabled").
		WillReturnRows(sqlmock.NewRows([]string{"delivered"}).AddRow(true))
	mock.ExpectExec(regexp.QuoteMeta("jsonb_build_object($1, TRUE)")).
		WithArgs("email_account_disabled", true, "decision-notification").
		WillReturnResult(sqlmock.NewResult(0, 1))

	delivered, err := repo.GetLogNotificationDeliveredByDecisionID(context.Background(), "decision-notification", "email_account_disabled")
	require.NoError(t, err)
	require.True(t, delivered)
	require.NoError(t, repo.MarkLogNotificationDeliveredByDecisionID(context.Background(), "decision-notification", "email_account_disabled", true))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryNormalizesMalformedNotificationDeliveryMetadata(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "content_moderation_notification_delivery_jsonb_guards" {
			return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
		}
		normalized := strings.Join(strings.Fields(actualSQL), " ")
		for _, clause := range []string{
			"WHEN jsonb_typeof(metadata) = 'object' THEN metadata",
			"WHEN jsonb_typeof(metadata->'notification_deliveries') = 'object'",
		} {
			if !strings.Contains(normalized, clause) {
				return fmt.Errorf("expected query to contain %q, got: %s", clause, actualSQL)
			}
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	mock.ExpectExec("content_moderation_notification_delivery_jsonb_guards").
		WithArgs("admin_alert", false, "decision-malformed-metadata").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.(*contentModerationRepository).MarkLogNotificationDeliveredByDecisionID(
		context.Background(),
		"decision-malformed-metadata",
		"admin_alert",
		false,
	))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryReplacesOnlySemanticDeadLetterAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db).(*contentModerationRepository)
	now := time.Now()
	latency := 24
	log := &service.ContentModerationLog{
		DecisionID:            "decision-semantic-replay",
		Action:                service.ContentModerationActionSemanticReviewAllow,
		HighestCategory:       "benign_context",
		HighestScore:          0.96,
		CategoryScores:        map[string]float64{"benign_context": 0.96},
		ThresholdSnapshot:     map[string]float64{},
		InputExcerpt:          "bounded text",
		UpstreamLatencyMS:     &latency,
		Metadata:              json.RawMessage(`{"semantic_review_verdict":"allow"}`),
		RiskContextType:       "semantic_review",
		RiskContextReason:     "model=review-model;intent=defensive",
		DecisionSource:        "semantic_review",
		ModerationProvider:    "platform_openai",
		ModerationModel:       "review-model",
		UserViolationEligible: true,
	}
	casPattern := `(?s)review_status = CASE WHEN reviewed_at IS NULL THEN \$16 ELSE review_status END.*AND action = 'error'.*AND decision_source = 'semantic_review'.*AND reviewed_at IS NULL`
	mock.ExpectQuery(casPattern).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(77), now))

	replaced, err := repo.ReplaceSemanticReviewDeadLetterByDecisionID(context.Background(), log)
	require.NoError(t, err)
	require.True(t, replaced)
	require.Equal(t, int64(77), log.ID)
	require.Equal(t, now, log.CreatedAt)

	log.DecisionID = "decision-already-successful"
	mock.ExpectQuery(casPattern).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}))
	replaced, err = repo.ReplaceSemanticReviewDeadLetterByDecisionID(context.Background(), log)
	require.NoError(t, err)
	require.False(t, replaced)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCreateLog_UsesValidUpsertReturningSQL(t *testing.T) {
	queryMatcher := sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		if expectedSQL != "content_moderation_create_log" {
			return sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
		}
		if !strings.Contains(actualSQL, "ON CONFLICT (decision_id) WHERE decision_id <> '' DO UPDATE SET") {
			return fmt.Errorf("expected partial-index upsert clause, got: %s", actualSQL)
		}
		if !strings.Contains(actualSQL, "truncate_reasons = CASE") || !strings.Contains(actualSQL, "END\nRETURNING id, created_at") {
			return fmt.Errorf("expected RETURNING to follow the final assignment, got: %s", actualSQL)
		}
		return nil
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(queryMatcher))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := NewContentModerationRepository(db)
	now := time.Now()
	userID := int64(292)
	apiKeyID := int64(373)
	groupID := int64(2)
	accountID := int64(42)
	latency := 25
	queueDelay := 7
	reviewedBy := int64(1)
	reviewedAt := now.Add(-time.Minute)
	log := &service.ContentModerationLog{
		DecisionID:             "cm_req_hash",
		RequestID:              "req-1",
		UserID:                 &userID,
		UserEmail:              "user@example.com",
		APIKeyID:               &apiKeyID,
		APIKeyName:             "codex",
		GroupID:                &groupID,
		GroupName:              "Codex高速专线",
		AccountID:              &accountID,
		AccountName:            "oauth-primary",
		AccountType:            service.AccountTypeOAuth,
		Endpoint:               "/v1/chat/completions",
		Provider:               "openai",
		Model:                  "gpt-5.4",
		Mode:                   "pre_block",
		Action:                 "keyword_block",
		Flagged:                true,
		HighestCategory:        "keyword",
		HighestScore:           1,
		CategoryScores:         map[string]float64{"keyword": 1},
		ThresholdSnapshot:      map[string]float64{"keyword": 1},
		InputExcerpt:           "developer message",
		TruncateReasons:        []string{"max_total_runes"},
		UpstreamLatencyMS:      &latency,
		Error:                  "blocked",
		MatchedKeyword:         "developer message",
		KeywordCategory:        "jailbreak",
		KeywordSeverity:        "high",
		KeywordAction:          "block",
		EffectiveKeywordAction: "block",
		RiskContextType:        "actual_request",
		RiskContextReason:      "request_intent_marker",
		ReviewStatus:           "pending",
		ReviewNote:             "note",
		ReviewedBy:             &reviewedBy,
		ReviewedAt:             &reviewedAt,
		ViolationCount:         1,
		AutoBanned:             false,
		EmailSent:              false,
		QueueDelayMS:           &queueDelay,
	}

	mock.ExpectQuery("content_moderation_create_log").
		WithArgs(
			log.DecisionID, log.RequestID, userID, log.UserEmail, apiKeyID, log.APIKeyName, groupID, log.GroupName,
			accountID, log.AccountName, log.AccountType,
			log.Endpoint, log.Provider, log.Model, log.Mode, log.Action, log.Flagged, log.HighestCategory, log.HighestScore,
			`{"keyword":1}`, `{"keyword":1}`, log.InputExcerpt, latency, log.Error, `{}`,
			log.MatchedKeyword, log.KeywordCategory, log.KeywordSeverity, log.KeywordAction, log.EffectiveKeywordAction,
			log.RiskContextType, log.RiskContextReason, log.ReviewStatus, log.ReviewNote, reviewedBy, reviewedAt,
			log.ViolationCount, log.AutoBanned, log.EmailSent, queueDelay,
			log.DecisionSource, log.ModerationProvider, log.ModerationModel, log.SourceOrigin,
			log.SelectedSource, log.SelectedSourceRole, log.SelectedFragmentRunes,
			log.DecisionCacheHit, log.DuplicateRetryCount, log.UserViolationEligible, `["max_total_runes"]`,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(42), now))

	require.NoError(t, repo.CreateLog(context.Background(), log))
	require.Equal(t, int64(42), log.ID)
	require.Equal(t, now, log.CreatedAt)
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
	mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN content_moderation_raw_request_snapshots rs ON rs.log_id = l.id LEFT JOIN content_moderation_evidence_snapshots es ON es.log_id = l.id WHERE l.id IS NOT NULL")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "request_id", "user_id", "user_email", "api_key_id", "api_key_name", "group_id", "group_name",
			"account_id", "account_name", "account_type",
			"endpoint", "provider", "model", "mode", "action", "flagged", "highest_category", "highest_score",
			"category_scores", "threshold_snapshot", "input_excerpt", "upstream_latency_ms", "error", "metadata",
			"matched_keyword", "keyword_category", "keyword_severity", "keyword_action", "effective_keyword_action",
			"risk_context_type", "risk_context_reason", "review_status", "review_note", "reviewed_by", "reviewed_at",
			"violation_count", "auto_banned", "email_sent", "user_status", "queue_delay_ms",
			"raw_request_available", "raw_request_bytes", "raw_request_truncated",
			"decision_source", "moderation_provider", "moderation_model", "source_origin", "selected_source", "selected_source_role",
			"selected_fragment_runes", "decision_cache_hit", "duplicate_retry_count", "user_violation_eligible", "truncate_reasons", "evidence_available", "created_at",
		}).AddRow(
			int64(42), "req-raw", nil, "u@example.com", nil, "H", nil, "Default",
			int64(77), "oauth-primary", service.AccountTypeOAuth,
			"/v1/responses", "openai", "gpt-5", "post_upstream", "cyber_policy", true, "cyber_policy", 1.0,
			[]byte(`{}`), []byte(`{}`), "", nil, "flagged", []byte(`{}`),
			"", "", "", "", "",
			"", "", "", "", nil, nil,
			0, false, false, "active", nil,
			true, 128, true,
			"ordinary_api", "openai", "omni-moderation-latest", "user_turn", "responses.input", "user",
			240, true, 2, true, []byte(`["max_total_runes"]`), true, now,
		))

	items, page, err := repo.ListLogs(context.Background(), service.ContentModerationLogFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.True(t, items[0].RawRequestAvailable)
	require.Equal(t, 128, items[0].RawRequestBytes)
	require.True(t, items[0].RawRequestTruncated)
	require.Equal(t, []string{"max_total_runes"}, items[0].TruncateReasons)
	require.NotNil(t, items[0].AccountID)
	require.Equal(t, int64(77), *items[0].AccountID)
	require.Equal(t, "oauth-primary", items[0].AccountName)
	require.Equal(t, service.AccountTypeOAuth, items[0].AccountType)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositorySemanticReviewUsageStats(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := &contentModerationRepository{db: db}
	since := time.Now().UTC().Add(-24 * time.Hour)
	mock.ExpectQuery(`(?s)SELECT\s+COUNT\(\*\).*FROM usage_logs.*source = 'content_moderation'`).
		WithArgs(since, service.ContentModerationSemanticReviewPrimaryModel, service.ContentModerationSemanticReviewFallbackModel).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_calls", "primary_calls", "fallback_calls", "other_calls", "input_tokens", "output_tokens", "avg_latency_ms",
		}).AddRow(12, 9, 3, 0, 4800, 600, 742))

	stats, err := repo.GetSemanticReviewUsageStats(context.Background(), since)
	require.NoError(t, err)
	require.True(t, stats.Available)
	require.Equal(t, int64(12), stats.TotalCalls)
	require.Equal(t, int64(9), stats.PrimaryCalls)
	require.Equal(t, int64(3), stats.FallbackCalls)
	require.Equal(t, int64(4800), stats.InputTokens)
	require.Equal(t, int64(600), stats.OutputTokens)
	require.Equal(t, int64(742), stats.AvgLatencyMS)
	require.NoError(t, mock.ExpectationsWereMet())
}
