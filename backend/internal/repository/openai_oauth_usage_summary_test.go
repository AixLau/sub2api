package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type jsonArrayLengthMatcher int

func (want jsonArrayLengthMatcher) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var items []json.RawMessage
	return json.Unmarshal(raw, &items) == nil && len(items) == int(want)
}

func TestGetOpenAIWindowCostsBatchUsesOneAccountBilledQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	fiveStart := now.Add(-5 * time.Hour)
	sevenStart := now.Add(-7 * 24 * time.Hour)

	mock.ExpectQuery(`(?s)jsonb_to_recordset.*account_stats_cost.*account_rate_multiplier`).
		WithArgs(jsonArrayLengthMatcher(2), now).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "five_hour", "seven_day"}).
			AddRow(int64(80), 1200.0, 1400.0).
			AddRow(int64(81), 50.0, 300.0))

	repo := newUsageLogRepositoryWithSQL(nil, db)
	costs, err := repo.GetOpenAIWindowCostsBatch(context.Background(), []service.OpenAIWindowCostQuery{
		{AccountID: 80, FiveHourStart: &fiveStart, SevenDayStart: &sevenStart},
		{AccountID: 81, FiveHourStart: &fiveStart, SevenDayStart: &sevenStart},
	}, now)
	if err != nil {
		t.Fatalf("get batch costs: %v", err)
	}
	if costs[80].FiveHour != 1200 || costs[81].SevenDay != 300 {
		t.Fatalf("unexpected costs: %+v", costs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetAccountWindowStatsByWindowsBatchUsesOneQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	fiveStart := now.Add(-5 * time.Hour)
	sevenStart := now.Add(-7 * 24 * time.Hour)

	mock.ExpectQuery(`(?s)jsonb_to_recordset.*input_tokens.*five_hour_start.*seven_day_start`).
		WithArgs(jsonArrayLengthMatcher(2)).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id", "five_requests", "five_tokens", "five_cost", "five_standard_cost", "five_user_cost",
			"seven_requests", "seven_tokens", "seven_cost", "seven_standard_cost", "seven_user_cost",
		}).
			AddRow(int64(80), int64(3), int64(120), 1.2, 1.0, 0.8, int64(8), int64(320), 3.2, 3.0, 2.4).
			AddRow(int64(81), int64(0), int64(0), 0.0, 0.0, 0.0, int64(2), int64(40), 0.4, 0.4, 0.3))

	repo := newUsageLogRepositoryWithSQL(nil, db)
	stats, err := repo.GetAccountWindowStatsByWindowsBatch(context.Background(), []service.AccountWindowStatsQuery{
		{AccountID: 80, FiveHourStart: &fiveStart, SevenDayStart: &sevenStart},
		{AccountID: 81, FiveHourStart: &fiveStart, SevenDayStart: &sevenStart},
	})
	if err != nil {
		t.Fatalf("get account window stats batch: %v", err)
	}
	if stats[80].FiveHour.Requests != 3 || stats[80].SevenDay.Tokens != 320 || stats[81].SevenDay.Cost != 0.4 {
		t.Fatalf("unexpected account window stats: %+v", stats)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBatchUpdateOpenAICapacityHistoryUsesOneJSONBUpdate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectExec(`(?s)jsonb_to_recordset.*UPDATE accounts`).
		WithArgs(jsonArrayLengthMatcher(2)).
		WillReturnResult(sqlmock.NewResult(0, 2))

	repo := &accountRepository{sql: db}
	err = repo.BatchUpdateOpenAICapacityHistory(context.Background(), []service.OpenAICapacityHistoryUpdate{
		{AccountID: 80, Updates: map[string]any{"openai_capacity_5h_last_known": 2200.0}},
		{AccountID: 81, Updates: map[string]any{"openai_capacity_7d_last_known": 4000.0}},
	})
	if err != nil {
		t.Fatalf("batch update history: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
