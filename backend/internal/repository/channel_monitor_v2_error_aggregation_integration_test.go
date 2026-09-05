//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2ErrorAggregationIgnoresResponseMetadata(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	start := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	want := map[string]string{}
	for _, tc := range []struct {
		name, message, body, want string
		status                    int
	}{
		{"overload", "Our servers are currently overloaded. Please try again later.", `{"response":{"moderation":null,"tool_choice":"auto","max_tokens":null}}`, "rate_or_capacity", 503},
		{"server", "An error occurred while processing your request.", `{"response":{"moderation":null}}`, "upstream_5xx", 502},
		{"invalid", "Invalid schema for function 'probe'.", `{"moderation":null}`, "invalid_request", 400},
		{"policy", "Blocked by content policy", `{"response":{"moderation":null}}`, "content_policy", 403},
	} {
		want["metadata-regression-"+tc.name] = tc.want
		errorType := "upstream_error"
		if tc.name == "invalid" {
			errorType = "invalid_request_error"
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO ops_error_logs
			(request_id, platform, model, error_phase, error_type, error_owner, error_source,
			status_code, upstream_status_code, error_message, upstream_error_message,
			upstream_error_detail, error_body, created_at)
			VALUES ($1, 'openai', $1, 'upstream', $2, 'provider', 'upstream_http', $3, $3, $4, $4, $5, $5, $6)`,
			"metadata-regression-"+tc.name, errorType, tc.status, tc.message, tc.body, start)
		require.NoError(t, err)
	}
	_, err := tx.ExecContext(ctx, channelMonitorV2ErrorAggregationSQL, start, start.Add(time.Minute))
	require.NoError(t, err)
	rows, err := tx.QueryContext(ctx, `SELECT model, error_category, error_requests FROM channel_monitor_v2_error_metrics_1m
		WHERE bucket_start=$1 AND taxonomy_version=$2`, start, service.ChannelMonitorV2TaxonomyVersion)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	got := map[string]string{}
	for rows.Next() {
		var model, category string
		var count int
		require.NoError(t, rows.Scan(&model, &category, &count))
		require.Equal(t, 1, count)
		got[model] = category
	}
	require.NoError(t, rows.Err())
	require.Equal(t, want, got)
}
