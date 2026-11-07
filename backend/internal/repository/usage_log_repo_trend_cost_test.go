package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageTrendAccountCost(t *testing.T) {
	for _, source := range []string{"filtered", "user", "hour", "day"} {
		t.Run(source, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
			end := start.Add(24 * time.Hour)
			pattern := `COALESCE\(SUM\(COALESCE\(account_stats_cost, total_cost\) \* COALESCE\(account_rate_multiplier, 1\)\), 0\) as account_cost`
			if source == "hour" || source == "day" {
				pattern = `actual_cost,\s+account_cost\s+FROM usage_dashboard_`
			}
			// Bucket labels follow the granularity (TO_CHAR 'YYYY-MM-DD HH24:00' for hour).
			bucket, wantBuckets := "2026-08-01", 1
			if source == "hour" {
				bucket, wantBuckets = "2026-08-01 00:00", 24
			}
			mock.ExpectQuery(pattern).WillReturnRows(sqlmock.NewRows([]string{
				"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
			}).AddRow(bucket, 1, 100, 20, 0, 0, 120, 2.0, 3.0, 0.5))
			var trend []TrendDataPoint
			var err error
			switch source {
			case "filtered":
				trend, err = repo.GetUsageTrendWithFilters(context.Background(), start, end, "day", 42, 0, 0, 0, "", nil, nil, nil)
			case "user":
				trend, err = repo.GetUserUsageTrendByUserID(context.Background(), 42, start, end, "day")
			default:
				trend, err = repo.GetUsageTrendWithFilters(context.Background(), start, end, source, 0, 0, 0, 0, "", nil, nil, nil)
			}
			require.NoError(t, err)
			// fillTrendGaps returns every bucket in the range; the remaining ones are zero.
			require.Len(t, trend, wantBuckets)
			require.Equal(t, bucket, trend[0].Date)
			for _, empty := range trend[1:] {
				require.Zero(t, empty.Requests)
				require.Zero(t, empty.AccountCost)
			}
			require.Equal(t, 0.5, trend[0].AccountCost)
			require.Equal(t, 2.0, trend[0].Cost)
			require.Equal(t, 3.0, trend[0].ActualCost)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
