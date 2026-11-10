package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Codex 用量快照每次响应都会更新 Extra。这些键只用于展示，不影响调度决策；
// 若漏进"影响调度"的判定，每次快照都会入队 account_changed，触发整桶重建并把
// 整份账号 JSON 重写进 Redis（codex_usage_dimension 曾漏列，导致每次快照更新都走这条路径）。
// 新增键却忘记登记中立列表时，本测试会失败。
func TestCodexUsageSnapshotExtraKeysAreSchedulerNeutral(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	snapshot := &service.OpenAICodexUsageSnapshot{
		PrimaryUsedPercent:          f(12.5),
		PrimaryResetAfterSeconds:    i(3600),
		PrimaryWindowMinutes:        i(300),
		SecondaryUsedPercent:        f(40),
		SecondaryResetAfterSeconds:  i(86400),
		SecondaryWindowMinutes:      i(10080),
		PrimaryOverSecondaryPercent: f(30),
	}

	updates := service.BuildCodexUsageExtraUpdates(snapshot, time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	require.NotEmpty(t, updates)
	require.Contains(t, updates, "codex_usage_dimension")

	for key := range updates {
		require.Truef(t, isSchedulerNeutralExtraKey(key),
			"Codex 快照键 %q 未登记为调度器中立键，会导致每次快照更新都触发 account_changed", key)
	}
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(updates),
		"整组 Codex 快照更新不应入队 scheduler outbox")
}

// 管理端按维度写入的 codex_usage_dimension 同样不能触发调度器重建。
func TestCodexUsageDimensionOnlyUpdateIsSchedulerNeutral(t *testing.T) {
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{"codex_usage_dimension": "global"}))
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{"codex_usage_dimension": "spark"}))
	// 对照：真正影响调度的键仍然入队。
	require.True(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{"codex_usage_dimension": "global", "model_mapping": map[string]any{}}))
}
