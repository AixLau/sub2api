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

func TestMigration176AddsContentModerationUsageSource(t *testing.T) {
	content, err := FS.ReadFile("176_add_content_moderation_usage_source.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS usage_logs_source_check")
	require.Contains(t, sql, "CHECK (source IN ('gateway', 'account_test', 'content_moderation'))")
	require.Contains(t, sql, "VALIDATE CONSTRAINT usage_logs_source_check")

	indexContent, err := FS.ReadFile("176a_content_moderation_usage_admin_index_notx.sql")
	require.NoError(t, err)
	indexSQL := string(indexContent)
	require.Contains(t, indexSQL, "DROP INDEX CONCURRENTLY IF EXISTS idx_usage_logs_admin_visible_id")
	require.Contains(t, indexSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_admin_visible_id")
	require.Contains(t, indexSQL, "source IN ('account_test', 'content_moderation')")
}

func TestMigration177PersistsContentModerationTruncationReasons(t *testing.T) {
	content, err := FS.ReadFile("177_content_moderation_truncate_reasons.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS truncate_reasons JSONB")
	require.Contains(t, sql, "jsonb_typeof(truncate_reasons) = 'array'")
	require.Contains(t, sql, "VALIDATE CONSTRAINT content_moderation_logs_truncate_reasons_array_check")
	require.Contains(t, sql, "idx_content_moderation_logs_truncate_reasons")
}

func TestMigration178AddsPendingReviewSLAAlertAndOnlineIndex(t *testing.T) {
	alertContent, err := FS.ReadFile("178_content_moderation_pending_review_sla_alert.sql")
	require.NoError(t, err)

	alertSQL := string(alertContent)
	require.Contains(t, alertSQL, "content_moderation_pending_review_age_seconds")
	require.Contains(t, alertSQL, "86400")
	require.Contains(t, alertSQL, "ON CONFLICT (name) DO NOTHING")
	require.NotContains(t, alertSQL, "CREATE INDEX")

	indexContent, err := FS.ReadFile("178a_content_moderation_pending_review_index_notx.sql")
	require.NoError(t, err)

	indexSQL := string(indexContent)
	require.Contains(t, indexSQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_content_moderation_logs_pending_review_created_at")
	require.Contains(t, indexSQL, "ON content_moderation_logs (created_at)")
	require.Contains(t, indexSQL, "WHERE review_status = 'pending'")
}
