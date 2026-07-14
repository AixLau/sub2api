//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type usageLogCallRecorder struct {
	calls int
}

func (r *usageLogCallRecorder) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	r.calls++
	return nil, errors.New("unexpected usage log exec")
}

func (r *usageLogCallRecorder) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	r.calls++
	return nil, errors.New("unexpected usage log query")
}

func TestSafeDateFormat(t *testing.T) {
	tests := []struct {
		name        string
		granularity string
		expected    string
	}{
		// 合法值
		{"hour", "hour", "YYYY-MM-DD HH24:00"},
		{"day", "day", "YYYY-MM-DD"},
		{"week", "week", "IYYY-IW"},
		{"month", "month", "YYYY-MM"},

		// 非法值回退到默认
		{"空字符串", "", "YYYY-MM-DD"},
		{"未知粒度 year", "year", "YYYY-MM-DD"},
		{"未知粒度 minute", "minute", "YYYY-MM-DD"},

		// 恶意字符串
		{"SQL 注入尝试", "'; DROP TABLE users; --", "YYYY-MM-DD"},
		{"带引号", "day'", "YYYY-MM-DD"},
		{"带括号", "day)", "YYYY-MM-DD"},
		{"Unicode", "日", "YYYY-MM-DD"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDateFormat(tc.granularity)
			require.Equal(t, tc.expected, got, "safeDateFormat(%q)", tc.granularity)
		})
	}
}

func TestAppendUsageLogSourceWhereCondition(t *testing.T) {
	conditions, args := appendUsageLogSourceWhereCondition(nil, nil, "content_moderation")
	require.Equal(t, []string{"source = $1"}, conditions)
	require.Equal(t, []any{"content_moderation"}, args)

	conditions, args = appendUsageLogSourceWhereCondition([]string{"user_id = $1"}, []any{int64(7)}, "")
	require.Equal(t, []string{"user_id = $1"}, conditions)
	require.Equal(t, []any{int64(7)}, args)
}

func TestUsageLogRepositoryGetActiveUsersTrendUsesGatewayAPIUsers(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE source = 'gateway' AND user_id IS NOT NULL AND api_key_id IS NOT NULL")).WithArgs(start, end).WillReturnRows(
		sqlmock.NewRows([]string{"date", "active_users"}).AddRow("2025-01-01", int64(2)),
	)

	trend, err := repo.GetActiveUsersTrend(context.Background(), start, end, "day")
	require.NoError(t, err)
	require.Equal(t, []usagestats.ActiveUsersTrendPoint{{Date: "2025-01-01", ActiveUsers: 2}}, trend)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetPerformanceStatsCountsGatewayAPIUsers(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	mock.ExpectQuery(regexp.QuoteMeta("COUNT(DISTINCT CASE WHEN source = 'gateway' AND user_id IS NOT NULL AND api_key_id IS NOT NULL THEN user_id END) as active_users")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"request_count", "token_count", "active_users"}).
			AddRow(int64(15), int64(300), int64(2)))

	rpm, tpm, activeUsers, err := repo.getPerformanceStats(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), rpm)
	require.Equal(t, int64(60), tpm)
	require.Equal(t, int64(2), activeUsers)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepositoryGetUserGrowthRetentionUsesMatureCohorts(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	today := timezone.Today()
	start := today.AddDate(0, 0, -40)
	end := today.AddDate(0, 0, 1)
	mock.ExpectQuery("(?s)WITH cohort_dates AS.*po\\.order_type = 'balance'").
		WithArgs(start, end, timezone.Name()).
		WillReturnRows(sqlmock.NewRows([]string{
			"date", "registrations", "d1_retained", "d7_retained", "d30_retained", "paid_users", "repeat_buyers",
		}).
			AddRow(today.AddDate(0, 0, -31).Format("2006-01-02"), int64(10), int64(8), int64(5), int64(2), int64(4), int64(2)).
			AddRow(today.AddDate(0, 0, -8).Format("2006-01-02"), int64(5), int64(3), int64(2), int64(0), int64(1), int64(1)).
			AddRow(today.AddDate(0, 0, -1).Format("2006-01-02"), int64(4), int64(1), int64(0), int64(0), int64(1), int64(0)).
			AddRow(today.AddDate(0, 0, -2).Format("2006-01-02"), int64(0), int64(0), int64(0), int64(0), int64(0), int64(0)))

	result, err := repo.GetUserGrowthRetention(context.Background(), start, end)
	require.NoError(t, err)
	require.Len(t, result.Cohorts, 4)
	require.InDelta(t, 80, *result.Cohorts[0].D1Rate, 0.001)
	require.InDelta(t, 40, *result.Cohorts[1].D7Rate, 0.001)
	require.Nil(t, result.Cohorts[1].D30Rate)
	require.Nil(t, result.Cohorts[2].D1Rate, "today's partial D1 observation must not be included")
	require.Nil(t, result.Cohorts[3].D1Rate, "empty cohorts have no retention rate")
	require.InDelta(t, 11.0/15.0*100, *result.Summary.D1Rate, 0.001)
	require.InDelta(t, 7.0/15.0*100, *result.Summary.D7Rate, 0.001)
	require.InDelta(t, 20, *result.Summary.D30Rate, 0.001)
	require.InDelta(t, 40, *result.Summary.PaidRate, 0.001)
	require.InDelta(t, 50, *result.Summary.RepeatBuyRate, 0.001)
	require.Nil(t, result.Cohorts[1].PaidRate, "immature 30-day payment cohorts must be excluded")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBuildUsageLogBatchInsertQuery_UsesConflictDoNothing(t *testing.T) {
	log := &service.UsageLog{
		UserID:       1,
		APIKeyID:     2,
		AccountID:    3,
		RequestID:    "req-batch-no-update",
		Model:        "gpt-5",
		InputTokens:  10,
		OutputTokens: 5,
		TotalCost:    1.2,
		ActualCost:   1.2,
		CreatedAt:    time.Now().UTC(),
	}
	prepared := prepareUsageLogInsert(log)

	query, _ := buildUsageLogBatchInsertQuery([]string{usageLogBatchKey(log.RequestID, log.APIKeyID)}, map[string]usageLogInsertPrepared{
		usageLogBatchKey(log.RequestID, log.APIKeyID): prepared,
	})

	require.Contains(t, query, "ON CONFLICT (request_id, api_key_id) DO NOTHING")
	require.NotContains(t, strings.ToUpper(query), "DO UPDATE")
}

func TestPrepareUsageLogInsertAccountTestActors(t *testing.T) {
	createdAt := time.Date(2026, 7, 10, 8, 30, 0, 0, time.UTC)

	accountTest := prepareUsageLogInsert(&service.UsageLog{
		Source:    service.UsageSourceAccountTest,
		AccountID: 7,
		RequestID: uuid.NewString(),
		Model:     "gpt-5.4",
		CreatedAt: createdAt,
	})
	require.Nil(t, accountTest.args[0])
	require.Nil(t, accountTest.args[1])
	require.Equal(t, int64(7), accountTest.args[2])
	require.Equal(t, service.UsageSourceAccountTest, accountTest.args[len(accountTest.args)-2])
	require.Equal(t, createdAt, accountTest.args[len(accountTest.args)-1])

	contentModeration := prepareUsageLogInsert(&service.UsageLog{
		Source:    service.UsageSourceContentModeration,
		AccountID: 8,
		RequestID: uuid.NewString(),
		Model:     "gpt-5.4-mini",
		CreatedAt: createdAt,
	})
	require.Nil(t, contentModeration.args[0])
	require.Nil(t, contentModeration.args[1])
	require.Equal(t, int64(8), contentModeration.args[2])
	require.Equal(t, service.UsageSourceContentModeration, contentModeration.args[len(contentModeration.args)-2])

	gateway := prepareUsageLogInsert(&service.UsageLog{
		Source:    service.UsageSourceGateway,
		UserID:    1,
		APIKeyID:  2,
		AccountID: 3,
		RequestID: uuid.NewString(),
		Model:     "gpt-5.4",
		CreatedAt: createdAt,
	})
	require.Equal(t, int64(1), gateway.args[0])
	require.Equal(t, int64(2), gateway.args[1])
	require.Equal(t, int64(3), gateway.args[2])
	require.Equal(t, service.UsageSourceGateway, gateway.args[len(gateway.args)-2])
	require.Equal(t, createdAt, gateway.args[len(gateway.args)-1])

	require.Len(t, accountTest.args, len(usageLogInsertArgTypes))
	require.Equal(t, "text", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-2])
	require.Equal(t, "timestamptz", usageLogInsertArgTypes[len(usageLogInsertArgTypes)-1])
}

func TestUsageLogRepositoryRejectsInvalidActors(t *testing.T) {
	tests := []struct {
		name    string
		log     service.UsageLog
		wantErr string
	}{
		{
			name:    "gateway missing user",
			log:     service.UsageLog{Source: service.UsageSourceGateway, APIKeyID: 2, AccountID: 3},
			wantErr: "user_id",
		},
		{
			name:    "gateway missing api key",
			log:     service.UsageLog{Source: service.UsageSourceGateway, UserID: 1, AccountID: 3},
			wantErr: "api_key_id",
		},
		{
			name:    "gateway missing account",
			log:     service.UsageLog{Source: service.UsageSourceGateway, UserID: 1, APIKeyID: 2},
			wantErr: "account_id",
		},
		{
			name:    "account test with user",
			log:     service.UsageLog{Source: service.UsageSourceAccountTest, UserID: 1, AccountID: 7},
			wantErr: "account_test",
		},
		{
			name:    "account test with api key",
			log:     service.UsageLog{Source: service.UsageSourceAccountTest, APIKeyID: 2, AccountID: 7},
			wantErr: "account_test",
		},
		{
			name:    "account test missing account",
			log:     service.UsageLog{Source: service.UsageSourceAccountTest},
			wantErr: "account_id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("Create/"+tt.name, func(t *testing.T) {
			exec := &usageLogCallRecorder{}
			repo := &usageLogRepository{sql: exec}
			log := tt.log
			log.RequestID = uuid.NewString()

			inserted, err := repo.Create(context.Background(), &log)
			require.False(t, inserted)
			require.ErrorContains(t, err, tt.wantErr)
			require.Zero(t, exec.calls, "invalid actors must be rejected before synchronous fallback")
		})

		t.Run("CreateBestEffort/"+tt.name, func(t *testing.T) {
			exec := &usageLogCallRecorder{}
			repo := &usageLogRepository{sql: exec}
			log := tt.log
			log.RequestID = uuid.NewString()

			err := repo.CreateBestEffort(context.Background(), &log)
			require.ErrorContains(t, err, tt.wantErr)
			require.Zero(t, exec.calls, "invalid actors must be rejected before synchronous fallback")
		})
	}

	t.Run("rejects before batch queues", func(t *testing.T) {
		repo := &usageLogRepository{
			db:                &sql.DB{},
			createBatchCh:     make(chan usageLogCreateRequest, 1),
			bestEffortBatchCh: make(chan usageLogBestEffortRequest, 1),
		}
		repo.createBatchOnce.Do(func() {})
		repo.bestEffortBatchOnce.Do(func() {})
		invalid := &service.UsageLog{
			Source:    service.UsageSourceAccountTest,
			UserID:    1,
			AccountID: 7,
			RequestID: uuid.NewString(),
		}

		createCtx, cancelCreate := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := repo.Create(createCtx, invalid)
		cancelCreate()
		require.ErrorContains(t, err, "account_test")
		require.Empty(t, repo.createBatchCh)

		bestEffortCtx, cancelBestEffort := context.WithTimeout(context.Background(), 20*time.Millisecond)
		err = repo.CreateBestEffort(bestEffortCtx, invalid)
		cancelBestEffort()
		require.ErrorContains(t, err, "account_test")
		require.Empty(t, repo.bestEffortBatchCh)
	})
}

func TestUsageLogRepositoryAccountTestUsesCreateSingle(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newUsageLogRepositoryWithSQL(nil, db)
	repo.createBatchCh = make(chan usageLogCreateRequest, 1)
	repo.createBatchOnce.Do(func() {})

	createdAt := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	log := &service.UsageLog{
		Source:    service.UsageSourceAccountTest,
		AccountID: 7,
		RequestID: uuid.NewString(),
		Model:     "gpt-5.4",
		CreatedAt: createdAt,
	}
	prepared := prepareUsageLogInsert(log)
	mock.ExpectQuery(`(?s)^\s*INSERT INTO usage_logs.*\$55\s*\)`).
		WithArgs(anySliceToDriverValues(prepared.args)...).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), createdAt))

	inserted, err := repo.Create(context.Background(), log)
	require.NoError(t, err)
	require.True(t, inserted)
	require.Equal(t, int64(101), log.ID)
	require.Empty(t, repo.createBatchCh, "account-test logs must bypass the request-ID batch queue")
	require.NoError(t, mock.ExpectationsWereMet())
}
