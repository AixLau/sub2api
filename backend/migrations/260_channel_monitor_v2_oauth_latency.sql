-- Existing latency aggregates cannot distinguish account types. Restart the
-- regular bounded backfill so averages and histograms use OAuth-only samples.
-- Coverage hides old aggregates until each interval has been recomputed.
UPDATE channel_monitor_v2_watermarks
SET usage_coverage_start = NULL,
    error_coverage_start = NULL,
    data_through = NULL,
    last_successful_at = NULL,
    backfill_cursor = NULL,
    updated_at = NOW()
WHERE id = 1;
