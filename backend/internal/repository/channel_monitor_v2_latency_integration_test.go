//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2LatencyOnlyIncludesOAuth(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	// CTEs provide source records without creating real accounts or credentials.
	const sources = `WITH accounts(id, type, platform) AS (VALUES
 (1, 'oauth', 'openai'), (2, 'apikey', 'openai'),
 (3, 'setup-token', 'openai'), (4, 'upstream', 'openai'),
 (5, 'bedrock', 'openai'), (6, 'service_account', 'openai')
), groups(id, platform) AS (VALUES (0, 'openai')),
usage_logs AS (
 SELECT id, 'latency-oauth-' || id AS request_id, $1::timestamptz AS created_at,
        account_id, 0::bigint AS group_id, 42::bigint AS user_id,
        'latency-oauth-only'::text AS model, ''::text AS requested_model,
        0 AS request_type, cost AS actual_cost, 1 AS input_tokens,
        1 AS output_tokens, 0 AS cache_creation_tokens, 0 AS cache_read_tokens,
        ttft AS first_token_ms, duration AS duration_ms
 FROM (VALUES
   (1, 1, 100, 1000, 1), (2, 1, 300, 3000, 1),
   (3, 2, 9000, 90000, 1), (4, 3, 9000, 90000, 1),
   (5, 4, 9000, 90000, 1), (6, 5, 9000, 90000, 1),
   (7, 6, 9000, 90000, 1), (8, 999, 9000, 90000, 1),
   (9, 1, 9000, 90000, 0), (10, 1, NULL, NULL, 1)
 ) v(id, account_id, ttft, duration, cost)
)
`
	for _, statement := range []string{
		fmt.Sprintf(channelMonitorV2UsageMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL),
		fmt.Sprintf(channelMonitorV2UserMetricsSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL),
		fmt.Sprintf(channelMonitorV2HistogramSQL, channelMonitorV2PlatformSQL, channelMonitorV2ModelSQL, channelMonitorV2HistogramBoundSQL("latency.value_ms")),
	} {
		_, err := tx.ExecContext(ctx, sources+statement, start, start.Add(time.Minute))
		require.NoError(t, err)
	}
	for _, table := range []string{"channel_monitor_v2_metrics_1m", "channel_monitor_v2_user_metrics_1m"} {
		var ttftSum, ttftCount, durationSum, durationCount, requests int64
		err := tx.QueryRowContext(ctx, `SELECT ttft_sum_ms, ttft_count, duration_sum_ms, duration_count, success_requests FROM `+table+` WHERE bucket_start=$1 AND model='latency-oauth-only'`, start).
			Scan(&ttftSum, &ttftCount, &durationSum, &durationCount, &requests)
		require.NoError(t, err)
		require.Equal(t, int64(400), ttftSum)
		require.Equal(t, int64(2), ttftCount)
		require.Equal(t, int64(4000), durationSum)
		require.Equal(t, int64(2), durationCount)
		require.Equal(t, int64(8), requests, "non-latency metrics retain their existing account scope")
	}
	for _, userID := range []int64{0, 42} {
		var samples int64
		err := tx.QueryRowContext(ctx, `SELECT SUM(sample_count) FROM channel_monitor_v2_latency_histograms_1m WHERE bucket_start=$1 AND model='latency-oauth-only' AND user_id=$2`, start, userID).Scan(&samples)
		require.NoError(t, err)
		require.Equal(t, int64(4), samples, "two OAuth requests each contribute TTFT and duration")
	}
}
