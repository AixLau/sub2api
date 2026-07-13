DROP INDEX CONCURRENTLY IF EXISTS idx_usage_logs_admin_visible_id;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_admin_visible_id
    ON usage_logs (id DESC)
    WHERE actual_cost > 0 OR source IN ('account_test', 'content_moderation');
