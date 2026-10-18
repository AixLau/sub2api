-- Make user dashboard history append-only and independent from usage_logs retention.
--
-- The bucket timezone is deliberately UTC.  Dashboard activity must use the same
-- fixed timezone forever; changing it would require rebuilding the historical
-- buckets and would change the meaning of existing dates.

CREATE TABLE IF NOT EXISTS usage_dashboard_user_platform_daily (
    bucket_date DATE NOT NULL,
    user_id BIGINT NOT NULL,
    platform TEXT NOT NULL,
    total_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    total_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    actual_cost DECIMAL(20, 10) NOT NULL DEFAULT 0,
    total_duration_ms BIGINT NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_date, user_id, platform)
);

CREATE INDEX IF NOT EXISTS idx_usage_dashboard_user_platform_daily_user_date
    ON usage_dashboard_user_platform_daily (user_id, bucket_date);

COMMENT ON TABLE usage_dashboard_user_platform_daily IS
    'Per-user, per-platform daily usage buckets (UTC, lifetime store, never TTL-trimmed).';

CREATE TABLE IF NOT EXISTS usage_dashboard_user_daily_backfill_state (
    id BOOLEAN PRIMARY KEY CHECK (id),
    backfilled_at TIMESTAMPTZ NOT NULL
);

DO $$
BEGIN
    -- The table was introduced by migration 249.  Keeping this guard makes the
    -- migration safe for databases upgraded from an intermediate build.
    IF to_regclass('public.usage_dashboard_user_daily') IS NULL THEN
        RAISE EXCEPTION 'usage_dashboard_user_daily must exist before migration 250';
    END IF;
END $$;

-- Capture the boundary before installing the trigger.  Rows with a larger id are
-- handled by the trigger and excluded from the backfill, so the two paths cannot
-- count the same log twice even if a caller supplies an old created_at value.
CREATE TEMP TABLE usage_dashboard_user_daily_cutover (
    cutover_id BIGINT NOT NULL
) ON COMMIT DROP;
INSERT INTO usage_dashboard_user_daily_cutover (cutover_id)
SELECT COALESCE(MAX(id), 0)
FROM usage_logs;

CREATE OR REPLACE FUNCTION append_usage_dashboard_user_daily()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO usage_dashboard_user_daily (
        bucket_date,
        user_id,
        total_requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        total_duration_ms,
        computed_at
    )
    SELECT
        (created_at AT TIME ZONE 'UTC')::date,
        user_id,
        COUNT(*),
        COALESCE(SUM(input_tokens), 0),
        COALESCE(SUM(output_tokens), 0),
        COALESCE(SUM(cache_creation_tokens), 0),
        COALESCE(SUM(cache_read_tokens), 0),
        COALESCE(SUM(total_cost), 0),
        COALESCE(SUM(actual_cost), 0),
        COALESCE(SUM(COALESCE(duration_ms, 0)), 0),
        NOW()
    FROM inserted_usage_logs
    WHERE user_id IS NOT NULL
    GROUP BY 1, 2
    ON CONFLICT (bucket_date, user_id) DO UPDATE SET
        total_requests = usage_dashboard_user_daily.total_requests + EXCLUDED.total_requests,
        input_tokens = usage_dashboard_user_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = usage_dashboard_user_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = usage_dashboard_user_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = usage_dashboard_user_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = usage_dashboard_user_daily.total_cost + EXCLUDED.total_cost,
        actual_cost = usage_dashboard_user_daily.actual_cost + EXCLUDED.actual_cost,
        total_duration_ms = usage_dashboard_user_daily.total_duration_ms + EXCLUDED.total_duration_ms,
        computed_at = NOW();

    INSERT INTO usage_dashboard_user_platform_daily (
        bucket_date,
        user_id,
        platform,
        total_requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        total_duration_ms,
        computed_at
    )
    SELECT
        (ul.created_at AT TIME ZONE 'UTC')::date,
        ul.user_id,
        CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
        END,
        COUNT(*),
        COALESCE(SUM(ul.input_tokens), 0),
        COALESCE(SUM(ul.output_tokens), 0),
        COALESCE(SUM(ul.cache_creation_tokens), 0),
        COALESCE(SUM(ul.cache_read_tokens), 0),
        COALESCE(SUM(ul.total_cost), 0),
        COALESCE(SUM(ul.actual_cost), 0),
        COALESCE(SUM(COALESCE(ul.duration_ms, 0)), 0),
        NOW()
    FROM inserted_usage_logs ul
    LEFT JOIN groups g ON g.id = ul.group_id
    LEFT JOIN accounts a ON a.id = ul.account_id
    WHERE ul.user_id IS NOT NULL
      AND ul.actual_cost > 0
      AND CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
          END IS NOT NULL
      AND CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
          END <> ''
    GROUP BY 1, 2, 3
    ON CONFLICT (bucket_date, user_id, platform) DO UPDATE SET
        total_requests = usage_dashboard_user_platform_daily.total_requests + EXCLUDED.total_requests,
        input_tokens = usage_dashboard_user_platform_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = usage_dashboard_user_platform_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = usage_dashboard_user_platform_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = usage_dashboard_user_platform_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = usage_dashboard_user_platform_daily.total_cost + EXCLUDED.total_cost,
        actual_cost = usage_dashboard_user_platform_daily.actual_cost + EXCLUDED.actual_cost,
        total_duration_ms = usage_dashboard_user_platform_daily.total_duration_ms + EXCLUDED.total_duration_ms,
        computed_at = NOW();

    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS usage_logs_user_dashboard_daily_append ON usage_logs;
CREATE TRIGGER usage_logs_user_dashboard_daily_append
AFTER INSERT ON usage_logs
REFERENCING NEW TABLE AS inserted_usage_logs
FOR EACH STATEMENT
EXECUTE FUNCTION append_usage_dashboard_user_daily();

-- Backfill rows that existed before the trigger.  The cutover timestamp excludes
-- rows written after the trigger was installed; those rows are appended by the trigger.
DO $$
DECLARE
    cutover_id BIGINT;
BEGIN
    SELECT c.cutover_id
    INTO cutover_id
    FROM usage_dashboard_user_daily_cutover c;

    IF EXISTS (
        SELECT 1
        FROM usage_dashboard_user_daily_backfill_state
        WHERE id
    ) THEN
        RETURN;
    END IF;

    INSERT INTO usage_dashboard_user_daily_backfill_state (id, backfilled_at)
    VALUES (TRUE, NOW());

    INSERT INTO usage_dashboard_user_daily (
        bucket_date,
        user_id,
        total_requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        total_duration_ms,
        computed_at
    )
    SELECT
        (created_at AT TIME ZONE 'UTC')::date,
        user_id,
        COUNT(*),
        COALESCE(SUM(input_tokens), 0),
        COALESCE(SUM(output_tokens), 0),
        COALESCE(SUM(cache_creation_tokens), 0),
        COALESCE(SUM(cache_read_tokens), 0),
        COALESCE(SUM(total_cost), 0),
        COALESCE(SUM(actual_cost), 0),
        COALESCE(SUM(COALESCE(duration_ms, 0)), 0),
        NOW()
    FROM usage_logs
    WHERE user_id IS NOT NULL
      AND id <= cutover_id
    GROUP BY 1, 2
    ON CONFLICT (bucket_date, user_id) DO UPDATE SET
        total_requests = usage_dashboard_user_daily.total_requests + EXCLUDED.total_requests,
        input_tokens = usage_dashboard_user_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = usage_dashboard_user_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = usage_dashboard_user_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = usage_dashboard_user_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = usage_dashboard_user_daily.total_cost + EXCLUDED.total_cost,
        actual_cost = usage_dashboard_user_daily.actual_cost + EXCLUDED.actual_cost,
        total_duration_ms = usage_dashboard_user_daily.total_duration_ms + EXCLUDED.total_duration_ms,
        computed_at = NOW();

    INSERT INTO usage_dashboard_user_platform_daily (
        bucket_date,
        user_id,
        platform,
        total_requests,
        input_tokens,
        output_tokens,
        cache_creation_tokens,
        cache_read_tokens,
        total_cost,
        actual_cost,
        total_duration_ms,
        computed_at
    )
    SELECT
        (ul.created_at AT TIME ZONE 'UTC')::date,
        ul.user_id,
        CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
        END,
        COUNT(*),
        COALESCE(SUM(ul.input_tokens), 0),
        COALESCE(SUM(ul.output_tokens), 0),
        COALESCE(SUM(ul.cache_creation_tokens), 0),
        COALESCE(SUM(ul.cache_read_tokens), 0),
        COALESCE(SUM(ul.total_cost), 0),
        COALESCE(SUM(ul.actual_cost), 0),
        COALESCE(SUM(COALESCE(ul.duration_ms, 0)), 0),
        NOW()
    FROM usage_logs ul
    LEFT JOIN groups g ON g.id = ul.group_id
    LEFT JOIN accounts a ON a.id = ul.account_id
    WHERE ul.user_id IS NOT NULL
      AND ul.id <= cutover_id
      AND ul.actual_cost > 0
      AND CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
          END IS NOT NULL
      AND CASE
            WHEN g.platform = 'composite' THEN a.platform
            ELSE COALESCE(NULLIF(g.platform, ''), a.platform)
          END <> ''
    GROUP BY 1, 2, 3
    ON CONFLICT (bucket_date, user_id, platform) DO UPDATE SET
        total_requests = usage_dashboard_user_platform_daily.total_requests + EXCLUDED.total_requests,
        input_tokens = usage_dashboard_user_platform_daily.input_tokens + EXCLUDED.input_tokens,
        output_tokens = usage_dashboard_user_platform_daily.output_tokens + EXCLUDED.output_tokens,
        cache_creation_tokens = usage_dashboard_user_platform_daily.cache_creation_tokens + EXCLUDED.cache_creation_tokens,
        cache_read_tokens = usage_dashboard_user_platform_daily.cache_read_tokens + EXCLUDED.cache_read_tokens,
        total_cost = usage_dashboard_user_platform_daily.total_cost + EXCLUDED.total_cost,
        actual_cost = usage_dashboard_user_platform_daily.actual_cost + EXCLUDED.actual_cost,
        total_duration_ms = usage_dashboard_user_platform_daily.total_duration_ms + EXCLUDED.total_duration_ms,
        computed_at = NOW();

END;
$$;
