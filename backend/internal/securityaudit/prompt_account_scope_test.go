package securityaudit

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestPromptAuditSelectedAccountConfigRoundTrip(t *testing.T) {
	manager := &ConfigManager{}
	req := promptAuditUpdateRequest(1, 1, "")
	req.SelectedAccounts, req.AccountIDs = true, []int64{9, 3, 9}
	stored, err := manager.buildNextStorage(DefaultStorageConfig(), req, 1)
	require.NoError(t, err)
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	stored, err = ParseStorageConfig(string(raw))
	require.NoError(t, err)
	active, err := ActiveFromStorage(stored, true, prefixEncryptor{})
	require.NoError(t, err)
	require.True(t, active.SelectedAccounts)
	require.Equal(t, []int64{3, 9}, active.AccountIDs)
	public := PublicFromStorage(stored, true, nil)
	require.True(t, public.SelectedAccounts)
	require.Equal(t, active.AccountIDs, public.AccountIDs)
	cloned := cloneActiveConfig(active)
	cloned.AccountIDs[0] = 100
	require.Equal(t, int64(3), active.AccountIDs[0])
	req.AccountIDs = nil
	_, err = manager.buildNextStorage(stored, req, 1)
	require.ErrorContains(t, err, "至少需要选择")
	req.AccountIDs = []int64{-1}
	_, err = manager.buildNextStorage(stored, req, 1)
	require.ErrorContains(t, err, "账号 ID 无效")
}

func TestPromptAuditSaveValidatesOpenAIOAuthAccounts(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "reject missing deleted or other type", true: "accept OpenAI OAuth"}[valid], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			manager := NewConfigManager(db, staticSettingRepository{}, nil, prefixEncryptor{}, testTotpKeyConfig())
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectQuery(`SELECT value FROM settings`).WillReturnError(sql.ErrNoRows)
			mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM accounts WHERE id=\$1 AND platform='openai' AND type='oauth' AND deleted_at IS NULL\)`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(valid))
			if valid {
				mock.ExpectExec(`INSERT INTO settings`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			req := promptAuditUpdateRequest(1, 1, "")
			req.SelectedAccounts, req.AccountIDs = true, []int64{7}
			result, err := manager.Save(context.Background(), req, 1)
			if valid {
				require.NoError(t, err)
				require.Equal(t, []int64{7}, result.AccountIDs)
			} else {
				require.ErrorContains(t, err, "OpenAI OAuth")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestPromptAuditSelectedAccountBlockingAndAsyncScope(t *testing.T) {
	group := int64(4)
	for _, tc := range []struct {
		name           string
		id             int64
		platform, kind string
		group          int64
		match          bool
	}{
		{"before selection", 0, "", "", 4, false},
		{"selected", 7, "openai", "oauth", 4, true},
		{"unselected", 8, "openai", "oauth", 4, false},
		{"api key", 7, "openai", "apikey", 4, false},
		{"other platform", 7, "anthropic", "oauth", 4, false},
		{"other group", 7, "openai", "oauth", 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := asyncConfig()
			cfg.SelectedAccounts, cfg.AccountIDs = true, []int64{7}
			cfg.AllGroups, cfg.GroupIDs = false, []int64{group}
			req := asyncRequest()
			req.AccountID, req.AccountPlatform, req.AccountType, req.GroupID = tc.id, tc.platform, tc.kind, &tc.group
			repo := &fakeJobRepository{createJob: &Job{ID: 1}}
			require.NoError(t, NewEnqueuer(&fakeConfigStore{cfg: cfg, active: true}, repo, &fakePayloadStore{values: map[int64]string{}}).Enqueue(context.Background(), req))
			require.Equal(t, tc.match, repo.createdSnapshot.MessageCount > 0)
			cfg.BlockingEnabled = true
			calls := 0
			scanner := PromptScannerFunc(func(context.Context, ActiveEndpoint, string, []string) (*NormalizedResult, error) {
				calls++
				return integrationResult(EventCritical), nil
			})
			svc := &PromptService{config: &fakeConfigStore{cfg: cfg, active: true}, evaluator: NewGuardEvaluator(scanner, nil, nil)}
			decision, err := svc.Evaluate(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, tc.match, calls > 0)
			require.Equal(t, tc.match, decision.Kind == DecisionBlock)
		})
	}
}
