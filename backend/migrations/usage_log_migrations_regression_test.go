package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration173SplitsConstraintValidationAndAdminIndexOnline(t *testing.T) {
	t.Run("base migration adds the source check without validating it", func(t *testing.T) {
		content, err := FS.ReadFile("173_add_account_test_usage_source.sql")
		require.NoError(t, err)

		sql := string(content)
		require.Contains(t, sql, "ADD CONSTRAINT usage_logs_source_check")
		require.Contains(t, sql, "CHECK (source IN ('gateway', 'account_test'))")
		require.Contains(t, sql, "NOT VALID")
		require.NotContains(t, sql, "VALIDATE CONSTRAINT")
		require.NotContains(t, sql, "CREATE INDEX")
	})

	t.Run("follow-up migration validates the source check", func(t *testing.T) {
		content, err := FS.ReadFile("173a_validate_account_test_usage_source.sql")
		require.NoError(t, err)

		sql := string(content)
		require.Contains(t, sql, "ALTER TABLE usage_logs")
		require.Contains(t, sql, "VALIDATE CONSTRAINT usage_logs_source_check")
		require.NotContains(t, sql, "CREATE INDEX")
	})

	t.Run("non-transactional follow-up builds the admin index concurrently", func(t *testing.T) {
		content, err := FS.ReadFile("173b_account_test_usage_admin_index_notx.sql")
		require.NoError(t, err)

		sql := string(content)
		require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_admin_visible_id")
		require.Contains(t, sql, "ON usage_logs (id DESC)")
		require.Contains(t, sql, "WHERE actual_cost > 0 OR source = 'account_test'")
	})
}
