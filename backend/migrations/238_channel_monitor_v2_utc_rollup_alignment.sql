-- Rebuild coarse channel monitor rollups with UTC-aligned buckets.
--
-- Before this migration, the date_bin origin omitted its timezone. On
-- installations whose PostgreSQL session timezone was Asia/Shanghai, 12-hour
-- and daily buckets were aligned to local midnight instead of UTC. The 1-hour
-- rollups are unaffected because the +08:00 offset is an integral number of
-- hours, so they are a sufficient source for the 7d/30d product windows.

DO $$
DECLARE
    source_start TIMESTAMPTZ;
BEGIN
    SELECT date_bin(
        INTERVAL '1 day',
        COALESCE(MIN(bucket_start), NOW() - INTERVAL '30 days'),
        TIMESTAMPTZ '1970-01-01 00:00:00+00'
    )
    INTO source_start
    FROM channel_monitor_v2_metrics_rollup
    WHERE bucket_seconds = 3600;

    DELETE FROM channel_monitor_v2_latency_histograms_rollup
    WHERE bucket_seconds IN (43200, 86400)
      AND bucket_start >= source_start;
    DELETE FROM channel_monitor_v2_error_metrics_rollup
    WHERE bucket_seconds IN (43200, 86400)
      AND bucket_start >= source_start;
    DELETE FROM channel_monitor_v2_user_metrics_rollup
    WHERE bucket_seconds IN (43200, 86400)
      AND bucket_start >= source_start;
    DELETE FROM channel_monitor_v2_metrics_rollup
    WHERE bucket_seconds IN (43200, 86400)
      AND bucket_start >= source_start;

    INSERT INTO channel_monitor_v2_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model,
        success_requests, error_requests, upstream_affected_requests,
        upstream_attempt_count, input_tokens, output_tokens,
        cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
        duration_sum_ms, duration_count, computed_at
    )
    SELECT date_bin(INTERVAL '12 hours', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           43200, platform, group_id, model,
           SUM(success_requests), SUM(error_requests), SUM(upstream_affected_requests),
           SUM(upstream_attempt_count), SUM(input_tokens), SUM(output_tokens),
           SUM(cache_creation_tokens), SUM(cache_read_tokens), SUM(ttft_sum_ms),
           SUM(ttft_count), SUM(duration_sum_ms), SUM(duration_count), NOW()
    FROM channel_monitor_v2_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5;

    INSERT INTO channel_monitor_v2_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model,
        success_requests, error_requests, upstream_affected_requests,
        upstream_attempt_count, input_tokens, output_tokens,
        cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
        duration_sum_ms, duration_count, computed_at
    )
    SELECT date_bin(INTERVAL '1 day', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           86400, platform, group_id, model,
           SUM(success_requests), SUM(error_requests), SUM(upstream_affected_requests),
           SUM(upstream_attempt_count), SUM(input_tokens), SUM(output_tokens),
           SUM(cache_creation_tokens), SUM(cache_read_tokens), SUM(ttft_sum_ms),
           SUM(ttft_count), SUM(duration_sum_ms), SUM(duration_count), NOW()
    FROM channel_monitor_v2_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5;

    INSERT INTO channel_monitor_v2_user_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model, user_id,
        success_requests, error_requests, input_tokens, output_tokens,
        cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
        duration_sum_ms, duration_count, computed_at
    )
    SELECT date_bin(INTERVAL '12 hours', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           43200, platform, group_id, model, user_id,
           SUM(success_requests), SUM(error_requests), SUM(input_tokens),
           SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
           SUM(ttft_sum_ms), SUM(ttft_count), SUM(duration_sum_ms),
           SUM(duration_count), NOW()
    FROM channel_monitor_v2_user_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6;

    INSERT INTO channel_monitor_v2_user_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model, user_id,
        success_requests, error_requests, input_tokens, output_tokens,
        cache_creation_tokens, cache_read_tokens, ttft_sum_ms, ttft_count,
        duration_sum_ms, duration_count, computed_at
    )
    SELECT date_bin(INTERVAL '1 day', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           86400, platform, group_id, model, user_id,
           SUM(success_requests), SUM(error_requests), SUM(input_tokens),
           SUM(output_tokens), SUM(cache_creation_tokens), SUM(cache_read_tokens),
           SUM(ttft_sum_ms), SUM(ttft_count), SUM(duration_sum_ms),
           SUM(duration_count), NOW()
    FROM channel_monitor_v2_user_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6;

    INSERT INTO channel_monitor_v2_error_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model,
        error_category, taxonomy_version, error_requests
    )
    SELECT date_bin(INTERVAL '12 hours', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           43200, platform, group_id, model, error_category,
           taxonomy_version, SUM(error_requests)
    FROM channel_monitor_v2_error_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6, 7;

    INSERT INTO channel_monitor_v2_error_metrics_rollup (
        bucket_start, bucket_seconds, platform, group_id, model,
        error_category, taxonomy_version, error_requests
    )
    SELECT date_bin(INTERVAL '1 day', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           86400, platform, group_id, model, error_category,
           taxonomy_version, SUM(error_requests)
    FROM channel_monitor_v2_error_metrics_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6, 7;

    INSERT INTO channel_monitor_v2_latency_histograms_rollup (
        bucket_start, bucket_seconds, platform, group_id, model, user_id,
        metric, upper_bound_ms, sample_count
    )
    SELECT date_bin(INTERVAL '12 hours', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           43200, platform, group_id, model, user_id, metric,
           upper_bound_ms, SUM(sample_count)
    FROM channel_monitor_v2_latency_histograms_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8;

    INSERT INTO channel_monitor_v2_latency_histograms_rollup (
        bucket_start, bucket_seconds, platform, group_id, model, user_id,
        metric, upper_bound_ms, sample_count
    )
    SELECT date_bin(INTERVAL '1 day', bucket_start, TIMESTAMPTZ '1970-01-01 00:00:00+00'),
           86400, platform, group_id, model, user_id, metric,
           upper_bound_ms, SUM(sample_count)
    FROM channel_monitor_v2_latency_histograms_rollup
    WHERE bucket_seconds = 3600
      AND bucket_start >= source_start
    GROUP BY 1, 2, 3, 4, 5, 6, 7, 8;
END $$;
