package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

// Account filtering must not change actual_cost from user spending to upstream cost.
func TestAccountDistributionActualCostUsesUserSpending(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	for _, dimension := range []string{"model", "inbound", "upstream", "path"} {
		t.Run(dimension, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			expectation := mock.ExpectQuery(`(?s)COALESCE\(SUM\(actual_cost\), 0\) as actual_cost.*FROM usage_logs.*AND account_id = \$3`).WithArgs(start, end, int64(42))
			if dimension == "model" {
				expectation.WillReturnRows(sqlmock.NewRows([]string{
					"model", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
				}).AddRow("gpt-5", 1, 100, 20, 0, 0, 120, 2.0, 3.0, 0.5))
				rows, err := repo.GetModelStatsWithFilters(context.Background(), start, end, 0, 0, 42, 0, nil, nil, nil)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, 3.0, rows[0].ActualCost)
				require.Equal(t, 0.5, rows[0].AccountCost)
			} else {
				expectation.WillReturnRows(sqlmock.NewRows([]string{"endpoint", "requests", "total_tokens", "cost", "actual_cost"}).AddRow("/v1/responses", 1, 120, 2.0, 3.0))
				var rows []EndpointStat
				var err error
				switch dimension {
				case "inbound":
					rows, err = repo.GetEndpointStatsWithFilters(context.Background(), start, end, 0, 0, 42, 0, "", nil, nil, nil)
				case "upstream":
					rows, err = repo.GetUpstreamEndpointStatsWithFilters(context.Background(), start, end, 0, 0, 42, 0, "", nil, nil, nil)
				case "path":
					rows, err = repo.getEndpointPathStatsWithFilters(context.Background(), start, end, 0, 0, 42, 0, "", "", nil, nil, nil, "")
				}
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, 3.0, rows[0].ActualCost)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountEndpointGroupingActualCostUsesUserSpending(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery(`(?s)FROM usage_logs.*WHERE account_id = \$1.*GROUP BY GROUPING SETS`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"inbound_grouped", "upstream_grouped", "inbound_endpoint", "upstream_endpoint",
			"requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
			"cost", "actual_cost", "account_cost", "avg_duration_ms",
		}).
			AddRow(1, 1, nil, nil, 1, 100, 20, 0, 0, 2.0, 3.0, 0.5, 20).
			AddRow(0, 1, "/v1/responses", nil, 1, 100, 20, 0, 0, 2.0, 3.0, 0.5, 20).
			AddRow(1, 0, nil, "/v1/responses", 1, 100, 20, 0, 0, 2.0, 3.0, 0.5, 20).
			AddRow(0, 0, "/v1/responses", "/v1/responses", 1, 100, 20, 0, 0, 2.0, 3.0, 0.5, 20))
	stats, err := repo.GetStatsWithFilters(context.Background(), usagestats.UsageLogFilters{AccountID: 42})
	require.NoError(t, err)
	require.Equal(t, 3.0, stats.TotalActualCost)
	require.NotNil(t, stats.TotalAccountCost)
	require.Equal(t, 0.5, *stats.TotalAccountCost)
	for _, rows := range [][]EndpointStat{stats.Endpoints, stats.UpstreamEndpoints, stats.EndpointPaths} {
		require.Len(t, rows, 1)
		require.Equal(t, stats.TotalActualCost, rows[0].ActualCost)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
