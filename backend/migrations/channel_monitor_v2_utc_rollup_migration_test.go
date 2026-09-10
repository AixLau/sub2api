//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration238RebuildsCoarseRollupsFromHourlySource(t *testing.T) {
	content, err := FS.ReadFile("238_channel_monitor_v2_utc_rollup_alignment.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "TIMESTAMPTZ '1970-01-01 00:00:00+00'")
	require.Contains(t, sql, "WHERE bucket_seconds = 3600")
	require.Contains(t, sql, "INTERVAL '12 hours'")
	require.Contains(t, sql, "INTERVAL '1 day'")
	require.Contains(t, sql, "channel_monitor_v2_metrics_rollup")
	require.Contains(t, sql, "channel_monitor_v2_user_metrics_rollup")
	require.Contains(t, sql, "channel_monitor_v2_error_metrics_rollup")
	require.Contains(t, sql, "channel_monitor_v2_latency_histograms_rollup")
}
