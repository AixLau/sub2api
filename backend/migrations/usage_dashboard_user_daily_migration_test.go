//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration249CreatesPermanentUserDailyBuckets(t *testing.T) {
	content, err := FS.ReadFile("249_usage_dashboard_user_daily.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_dashboard_user_daily")
	require.Contains(t, sql, "PRIMARY KEY (bucket_date, user_id)")
	require.Contains(t, sql, "UTC calendar date bucket")
}

func TestMigration250AppendsUserDailyBucketsFromSuccessfulInserts(t *testing.T) {
	content, err := FS.ReadFile("250_usage_dashboard_user_daily_append.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "usage_dashboard_user_daily_cutover")
	require.Contains(t, sql, "usage_dashboard_user_daily_backfill_state")
	require.Contains(t, sql, "WHERE id <= cutover_id")
	require.Contains(t, sql, "ON CONFLICT (bucket_date, user_id) DO UPDATE SET")
	require.Contains(t, sql, "usage_dashboard_user_daily.total_requests + EXCLUDED.total_requests")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS inserted_usage_logs")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_user_dashboard_daily_append")
	require.NotContains(t, sql, "AFTER DELETE ON usage_logs")
}
