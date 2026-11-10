//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func setStoredBackupSchedule(t *testing.T, repo *mockSettingRepo, cfg BackupScheduleConfig) {
	t.Helper()
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, repo.Set(context.Background(), settingKeyBackupSchedule, string(data)))
}

func registeredBackupCron(svc *BackupService) (cron.EntryID, string) {
	svc.cronMu.Lock()
	defer svc.cronMu.Unlock()
	return svc.cronEntryID, svc.cronExpr
}

func TestBackupScheduler_DisabledInstanceNeverRegistersCron(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	svc.schedulerEnabled = false
	svc.cronSched = cron.New()

	setStoredBackupSchedule(t, repo, BackupScheduleConfig{Enabled: true, CronExpr: "0 4 * * *", RetainDays: 14, RetainCount: 3})
	svc.syncCronScheduleFromStore()
	id, _ := registeredBackupCron(svc)
	require.Zero(t, id, "未启用定时备份的实例不应注册定时任务")

	// 在该实例上修改计划：只保存，不注册
	saved, err := svc.UpdateSchedule(context.Background(), BackupScheduleConfig{Enabled: true, CronExpr: "0 2 * * *", RetainDays: 14, RetainCount: 3})
	require.NoError(t, err)
	require.Equal(t, "0 2 * * *", saved.CronExpr)
	id, _ = registeredBackupCron(svc)
	require.Zero(t, id)
	stored, err := svc.GetSchedule(context.Background())
	require.NoError(t, err)
	require.Equal(t, "0 2 * * *", stored.CronExpr)
}

func TestBackupScheduler_EnabledInstanceFollowsStoredSchedule(t *testing.T) {
	repo := newMockSettingRepo()
	svc := newTestBackupService(repo, &mockDumper{}, newMockObjectStore())
	svc.schedulerEnabled = true
	svc.cronSched = cron.New()

	// 计划由另一台实例写入数据库
	setStoredBackupSchedule(t, repo, BackupScheduleConfig{Enabled: true, CronExpr: "0 4 * * *", RetainDays: 14, RetainCount: 3})
	svc.syncCronScheduleFromStore()
	id1, expr := registeredBackupCron(svc)
	require.NotZero(t, id1)
	require.Equal(t, "0 4 * * *", expr)
	require.Len(t, svc.cronSched.Entries(), 1)

	// 计划未变：不重复注册
	svc.syncCronScheduleFromStore()
	id, _ := registeredBackupCron(svc)
	require.Equal(t, id1, id)

	// 时间被改：替换旧任务，而不是两个都留着
	setStoredBackupSchedule(t, repo, BackupScheduleConfig{Enabled: true, CronExpr: "0 2 * * *", RetainDays: 14, RetainCount: 3})
	svc.syncCronScheduleFromStore()
	id2, expr := registeredBackupCron(svc)
	require.NotEqual(t, id1, id2)
	require.Equal(t, "0 2 * * *", expr)
	require.Len(t, svc.cronSched.Entries(), 1)

	// 被关闭：移除任务
	setStoredBackupSchedule(t, repo, BackupScheduleConfig{Enabled: false, CronExpr: "0 2 * * *", RetainDays: 14, RetainCount: 3})
	svc.syncCronScheduleFromStore()
	id, expr = registeredBackupCron(svc)
	require.Zero(t, id)
	require.Empty(t, expr)
	require.Empty(t, svc.cronSched.Entries())
}
