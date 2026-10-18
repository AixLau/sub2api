-- usage_dashboard_user_daily: 用户级每日用量聚合桶。
--
-- 动机：usage_logs 有运行时保留清理（设置 ops_runtime_log_config.request_retention_days，
-- 默认 90 天），明细被批量删除后，用户 Dashboard 的「累计 Token」「Token 活动 /
-- 连续活跃天数 / 峰值」等全史指标必须有一张不做 TTL 裁剪的桶表支撑，
-- 否则会随清理静默回缩为"滚动窗口累计"。
--
-- 语义约定：
--   - 每一行 = (固定 UTC 日期, 用户) 的当日合计。新日志由后续迁移安装的
--     AFTER INSERT 触发器在同一事务内追加；本表不接受清理/重算回写；
--   - 本表永不按时间裁剪：行数 ≈ 活跃用户数 × 天数，量级可忽略，不分区；
--   - 管理员侧全局桶表 (usage_dashboard_hourly/daily) 继续独立保留。
-- 幂等：IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS usage_dashboard_user_daily (
    bucket_date DATE NOT NULL,
    user_id BIGINT NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_date, user_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_user_daily_user_date
    ON usage_dashboard_user_daily (user_id, bucket_date);

COMMENT ON TABLE usage_dashboard_user_daily IS
    'Per-user daily usage buckets (lifetime store, never TTL-trimmed). Source for user dashboard cumulative/activity metrics.';
COMMENT ON COLUMN usage_dashboard_user_daily.bucket_date IS 'UTC calendar date bucket of the day.';
COMMENT ON COLUMN usage_dashboard_user_daily.user_id IS 'Owner of the usage rows.';
COMMENT ON COLUMN usage_dashboard_user_daily.computed_at IS 'When the bucket was last appended.';
