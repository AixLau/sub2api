package service

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type cyberRuntimeSettingRepo struct {
	*contentModerationTestSettingRepo
	mu               sync.Mutex
	getMultipleCalls int
	getMultipleErr   error
}

func newCyberRuntimeSettingRepo(values map[string]string) *cyberRuntimeSettingRepo {
	return &cyberRuntimeSettingRepo{contentModerationTestSettingRepo: &contentModerationTestSettingRepo{values: values}}
}

func (r *cyberRuntimeSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	r.getMultipleCalls++
	err := r.getMultipleErr
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return r.contentModerationTestSettingRepo.GetMultiple(ctx, keys)
}

func (r *cyberRuntimeSettingRepo) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getMultipleCalls
}

func (r *cyberRuntimeSettingRepo) failMultiple(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getMultipleErr = err
}

// cyberOrderingTestRepo records the sequence of repo calls to verify F7 ordering.
type cyberOrderingTestRepo struct {
	mu         sync.Mutex
	calls      []string
	emailSents []bool // EmailSent value captured at each CreateLog call
}

func (r *cyberOrderingTestRepo) CreateLog(ctx context.Context, log *ContentModerationLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "create")
	if log != nil {
		r.emailSents = append(r.emailSents, log.EmailSent)
		log.ID = 1 // simulate DB-assigned ID so UpdateLogEmailSent guard passes
	}
	return nil
}

func (r *cyberOrderingTestRepo) UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "update_email_sent")
	return nil
}

func (r *cyberOrderingTestRepo) UpdateLogViolationCountByDecisionID(ctx context.Context, decisionID string, count int) error {
	return nil
}

func (r *cyberOrderingTestRepo) UpdateLogAccountActionByDecisionID(ctx context.Context, decisionID string, violationCount int, autoBanned bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "update_account_action")
	return nil
}

func (r *cyberOrderingTestRepo) UpdateLogEmailSentByDecisionID(ctx context.Context, decisionID string, sent bool) error {
	return nil
}

func (r *cyberOrderingTestRepo) ReviewLog(ctx context.Context, id int64, input ContentModerationLogReviewInput) (*ContentModerationLog, error) {
	return &ContentModerationLog{ID: id, ReviewStatus: input.Status, ReviewNote: input.Note}, nil
}

func (r *cyberOrderingTestRepo) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *cyberOrderingTestRepo) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "count")
	return 0, nil
}

func (r *cyberOrderingTestRepo) CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*ContentModerationCleanupResult, error) {
	return &ContentModerationCleanupResult{}, nil
}

func (r *cyberOrderingTestRepo) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *cyberOrderingTestRepo) snapshotEmailSents() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]bool, len(r.emailSents))
	copy(out, r.emailSents)
	return out
}

func TestRecordCyberPolicyEvent_DisabledWhenRiskControlOff(t *testing.T) {
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "false",
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID:          1,
		UserEmail:       "u@x.com",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		UpstreamMessage: "flagged",
		UpstreamBody:    `{"error":{"code":"cyber_policy"}}`,
		UpstreamStatus:  400,
	})

	require.Empty(t, repo.snapshotLogs(), "CreateLog must NOT be called when risk_control_enabled is off")
}

func TestRecordCyberPolicyEvent_WritesLogWhenEnabled(t *testing.T) {
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true",
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil, // emailService=nil: email path safely skipped
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID:          1,
		UserEmail:       "u@x.com",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		UpstreamMessage: "flagged",
		UpstreamBody:    `{"error":{"code":"cyber_policy"}}`,
		UpstreamStatus:  400,
	})

	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	log := logs[0]

	require.Equal(t, "cyber_policy", log.Action)
	require.True(t, log.Flagged)
	require.Equal(t, "cyber_policy", log.HighestCategory)
	require.Empty(t, log.Error)
	require.Contains(t, string(log.Metadata), `"upstream_message":"flagged"`)
	require.Contains(t, string(log.Metadata), `"upstream_body_excerpt"`)
	require.NotEmpty(t, log.DecisionID)
	require.False(t, log.AutoBanned)
	// emailService is nil, so EmailSent must be false
	require.False(t, log.EmailSent)

	// UserID pointer must be set
	require.NotNil(t, log.UserID)
	require.Equal(t, int64(1), *log.UserID)

	// score for cyber_policy is always 1.0
	require.Equal(t, 1.0, log.HighestScore)

	// mode must be post_upstream
	require.Equal(t, "post_upstream", log.Mode)

	// provider
	require.Equal(t, "openai", log.Provider)

	// model
	require.Equal(t, "gpt-5", log.Model)

	// endpoint
	require.Equal(t, "/v1/responses", log.Endpoint)

	// violation count >= 1 (side-effects ran)
	require.GreaterOrEqual(t, log.ViolationCount, 1)

	require.Equal(t, "flagged", contentModerationCyberUpstreamMessage(&log))
}

func TestRecordCyberPolicyEvent_StoresEncryptedRawRequestSnapshot(t *testing.T) {
	repo := &contentModerationRawSnapshotTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true",
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetRawRequestSnapshotStore(repo, contentModerationTestEncryptor{})

	rawBody := []byte(`{"model":"gpt-5","input":"show the exact user prompt","secret":"sk-raw-user-secret"}`)
	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		RequestID:       "req-raw",
		UserID:          1,
		UserEmail:       "u@x.com",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		UpstreamMessage: "flagged",
		UpstreamStatus:  400,
		RequestBody:     rawBody,
	})

	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.True(t, logs[0].RawRequestAvailable)
	require.Equal(t, len(rawBody), logs[0].RawRequestBytes)
	require.False(t, logs[0].RawRequestTruncated)
	require.NotContains(t, logs[0].Error, "show the exact user prompt", "plain request body must not be copied into the regular audit error")

	stored, ok := repo.snapshotRaw(logs[0].ID)
	require.True(t, ok)
	require.NotContains(t, stored.BodyEncrypted, "show the exact user prompt", "snapshot storage must be encrypted")
	require.NotContains(t, stored.BodyEncrypted, "sk-raw-user-secret", "snapshot storage must not contain plaintext secrets")

	view, err := svc.GetRawRequestSnapshot(context.Background(), logs[0].ID)
	require.NoError(t, err)
	require.Equal(t, logs[0].ID, view.LogID)
	require.Equal(t, "req-raw", view.RequestID)
	require.Equal(t, string(rawBody), view.Body)
	require.Equal(t, len(rawBody), view.BodyBytes)
	require.False(t, view.Truncated)
}

func TestRecordCyberSessionBlockedEvent_WritesRiskAuditWithoutBanCount(t *testing.T) {
	repo := &cyberSessionBlockedRawSnapshotTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true",
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetRawRequestSnapshotStore(repo, contentModerationTestEncryptor{})

	rawBody := []byte(`{"model":"gpt-5","prompt_cache_key":"sess-1","input":"retry after cyber policy"}`)
	svc.RecordCyberSessionBlockedEvent(context.Background(), CyberSessionBlockedRecordInput{
		RequestID:       "req-session-blocked",
		UserID:          1,
		UserEmail:       "u@x.com",
		APIKeyID:        9,
		APIKeyName:      "H",
		Endpoint:        "/v1/responses",
		Model:           "gpt-5",
		SessionBlockKey: "abc123",
		RequestBody:     rawBody,
	})

	require.Empty(t, repo.snapshotCountCalls(), "session-block follow-up rows must not run auto-ban counting")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionCyberPolicySessionBlocked, logs[0].Action)
	require.True(t, logs[0].Flagged)
	require.Equal(t, "pre_upstream", logs[0].Mode)
	require.Equal(t, "cyber_policy_session_blocked", logs[0].HighestCategory)
	require.Empty(t, logs[0].Error)
	require.NotContains(t, string(logs[0].Metadata), "abc123")
	require.NotEmpty(t, logs[0].DecisionID)
	require.Equal(t, 0, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.True(t, logs[0].RawRequestAvailable)

	view, err := svc.GetRawRequestSnapshot(context.Background(), logs[0].ID)
	require.NoError(t, err)
	require.Equal(t, string(rawBody), view.Body)
	require.Equal(t, len(rawBody), view.BodyBytes)
}

func TestRecordCyberPolicyEvent_RespectsContentModerationScope(t *testing.T) {
	groupID := int64(7)
	tests := []struct {
		name       string
		config     string
		groupID    *int64
		model      string
		wantCalls  []bool
		wantLogs   int
		wantBanned bool
	}{
		{
			name:     "excluded group",
			config:   `{"all_groups":false,"group_ids":[8],"ban_threshold":1}`,
			groupID:  &groupID,
			model:    "gpt-5",
			wantLogs: 0,
		},
		{
			name:     "ungrouped excluded by selected groups",
			config:   `{"all_groups":false,"group_ids":[7],"ban_threshold":1}`,
			groupID:  nil,
			model:    "gpt-5",
			wantLogs: 0,
		},
		{
			name:     "excluded model",
			config:   `{"all_groups":true,"model_filter":{"type":"include","models":["gpt-4o"]},"ban_threshold":1}`,
			groupID:  &groupID,
			model:    "gpt-5",
			wantLogs: 0,
		},
		{
			name:       "included group and model",
			config:     `{"enabled":false,"mode":"off","sample_rate":0,"all_groups":false,"group_ids":[7],"model_filter":{"type":"include","models":["gpt-5"]},"ban_threshold":1}`,
			groupID:    &groupID,
			model:      "gpt-5",
			wantCalls:  []bool{false},
			wantLogs:   1,
			wantBanned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &banCountArgsTestRepo{}
			userRepo := &contentModerationTestUserRepo{user: &User{ID: 1, Role: RoleUser, Status: StatusActive}}
			svc := NewContentModerationService(
				&contentModerationTestSettingRepo{values: map[string]string{
					SettingKeyRiskControlEnabled:      "true",
					SettingKeyContentModerationConfig: tt.config,
				}},
				repo, nil, nil, userRepo, nil, nil, nil,
			)

			svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
				UserID:  1,
				GroupID: tt.groupID,
				Model:   tt.model,
			})

			if tt.wantCalls == nil {
				require.Empty(t, repo.snapshotCountCalls())
			} else {
				require.Equal(t, tt.wantCalls, repo.snapshotCountCalls())
			}
			require.Len(t, repo.snapshotLogs(), tt.wantLogs)
			require.Equal(t, tt.wantBanned, userRepo.user.Status == StatusDisabled)
			if tt.wantBanned {
				require.Len(t, userRepo.updated, 1)
			} else {
				require.Empty(t, userRepo.updated)
			}
		})
	}
}

func TestRecordCyberPolicyEvent_InitialRuntimeSnapshotLoadFailureSkipsEvent(t *testing.T) {
	repo := &banCountArgsTestRepo{}
	settingRepo := newCyberRuntimeSettingRepo(map[string]string{
		SettingKeyRiskControlEnabled:      "true",
		SettingKeyContentModerationConfig: `{invalid`,
	})
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil, nil)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID: 1,
		Model:  "gpt-5",
	})

	require.Empty(t, repo.snapshotCountCalls())
	require.Empty(t, repo.snapshotLogs())
	require.GreaterOrEqual(t, settingRepo.calls(), 1)
}

func TestRecordCyberPolicyEvent_RuntimeSnapshotRefreshFailureKeepsStaleScope(t *testing.T) {
	repo := &banCountArgsTestRepo{}
	settingRepo := newCyberRuntimeSettingRepo(map[string]string{
		SettingKeyRiskControlEnabled:      "true",
		SettingKeyContentModerationConfig: `{"all_groups":true,"model_filter":{"type":"include","models":["gpt-5"]}}`,
	})
	svc := NewContentModerationService(settingRepo, repo, nil, nil, nil, nil, nil, nil)
	svc.runtimeCacheTTL = time.Minute

	_, err := svc.loadRuntimeSnapshot(context.Background())
	require.NoError(t, err)
	current := svc.runtimeSnapshot.Load()
	require.NotNil(t, current)
	expired := *current
	expired.loadedAt = time.Now().Add(-2 * time.Minute)
	svc.runtimeSnapshot.Store(&expired)
	settingRepo.failMultiple(errors.New("database unavailable"))

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID: 1,
		Model:  "gpt-5",
	})

	require.Len(t, repo.snapshotLogs(), 1)
	require.Eventually(t, func() bool {
		return settingRepo.calls() == 2
	}, time.Second, time.Millisecond)
	require.Equal(t, 2, settingRepo.calls())
}

// TestRecordCyberPolicyEvent_CreateLogBeforeEmail verifies F7: the moderation
// log is persisted BEFORE email delivery, and EmailSent is patched afterwards —
// SMTP hangs can no longer swallow the audit record.
//
// Note on email ordering: EmailService is a concrete type with no injectable
// send interface, so SMTP-success cannot be simulated in unit tests.
// With emailService=nil the email block is skipped and UpdateLogEmailSent is not
// called (correct: logPersisted && emailSent guard). The test therefore asserts
// the two invariants that ARE observable without real SMTP:
//  1. CreateLog runs first (calls[0]=="create").
//  2. The log is stored with EmailSent=false (not pre-set to true).
//
// The update_email_sent path is covered by integration/e2e tests where a real
// (or test-double) SMTP endpoint is available.
func TestRecordCyberPolicyEvent_CreateLogBeforeEmail(t *testing.T) {
	repo := &cyberOrderingTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true",
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil, // emailService=nil: email path safely skipped; see doc comment above
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		RequestID:       "req-1",
		UserID:          7,
		UserEmail:       "u@example.com",
		Model:           "gpt-5",
		UpstreamMessage: "blocked",
	})

	calls := repo.snapshot()
	require.GreaterOrEqual(t, len(calls), 2, "CreateLog and violation counting must be called")
	require.Equal(t, "create", calls[0], "CreateLog must run first (F7: log-before-email)")
	require.Equal(t, "count", calls[1], "violation counting must run only after the audit log exists")

	// EmailSent must be false when the log is first persisted (new code sets it
	// false before CreateLog; email result is patched via UpdateLogEmailSent).
	emailSents := repo.snapshotEmailSents()
	require.NotEmpty(t, emailSents, "CreateLog must have captured EmailSent value")
	require.False(t, emailSents[0], "log must be stored with EmailSent=false initially (F7)")

	// With emailService=nil, no email is sent, so UpdateLogEmailSent must NOT
	// be called (logPersisted && emailSent guard correctly suppresses the patch).
	require.NotContains(t, calls, "update_email_sent",
		"UpdateLogEmailSent must not be called when no email was sent")
}

func TestRecordCyberPolicyEvent_PersistenceFailureSuppressesEnforcement(t *testing.T) {
	repo := &contentModerationFailingPersistenceRepo{}
	userRepo := &contentModerationTestUserRepo{
		user: &User{ID: 7, Email: "u@example.com", Role: RoleUser, Status: StatusActive},
	}
	emailProbe := &contentModerationEmailSideEffectProbe{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: `{"auto_ban_enabled":true,"ban_threshold":1}`,
		}},
		repo,
		nil,
		nil,
		userRepo,
		nil,
		NewEmailService(emailProbe, nil),
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		RequestID:       "cyber-persistence-failure",
		UserID:          7,
		UserEmail:       "u@example.com",
		Model:           "gpt-5",
		UpstreamMessage: "blocked",
	})

	require.Equal(t, int64(1), repo.createCalls.Load())
	require.Empty(t, userRepo.updated)
	require.Equal(t, StatusActive, userRepo.user.Status)
	require.Zero(t, emailProbe.getMultipleCalls.Load())
}

// banCountArgsTestRepo 在 contentModerationTestRepo 基础上记录
// CountFlaggedByUserSince 收到的 excludeCyberPolicy 参数，供透传断言。
type banCountArgsTestRepo struct {
	contentModerationTestRepo
	argsMu     sync.Mutex
	countCalls []bool
}

func (r *banCountArgsTestRepo) CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error) {
	r.argsMu.Lock()
	r.countCalls = append(r.countCalls, excludeCyberPolicy)
	r.argsMu.Unlock()
	return r.contentModerationTestRepo.CountFlaggedByUserSince(ctx, userID, since, excludeCyberPolicy)
}

func (r *banCountArgsTestRepo) snapshotCountCalls() []bool {
	r.argsMu.Lock()
	defer r.argsMu.Unlock()
	out := make([]bool, len(r.countCalls))
	copy(out, r.countCalls)
	return out
}

func TestApplyFlaggedAccountSideEffects_PassesExcludeCyberFlag(t *testing.T) {
	repo := &banCountArgsTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{}},
		repo, nil, nil, nil, nil, nil, nil,
	)
	userID := int64(42)

	cfgExclude := defaultContentModerationConfig()
	cfgExclude.CyberPolicyExcludeFromBanCount = true
	svc.applyFlaggedAccountSideEffects(context.Background(), cfgExclude, &ContentModerationLog{Flagged: true, UserID: &userID})

	cfgDefault := defaultContentModerationConfig() // 默认 false
	svc.applyFlaggedAccountSideEffects(context.Background(), cfgDefault, &ContentModerationLog{Flagged: true, UserID: &userID})

	require.Equal(t, []bool{true, false}, repo.snapshotCountCalls(),
		"applyFlaggedAccountSideEffects 必须把 cfg.CyberPolicyExcludeFromBanCount 透传给 COUNT 查询")
}

func TestRecordCyberPolicyEvent_ExcludeFromBanCount_SkipsBanJudgment(t *testing.T) {
	repo := &banCountArgsTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: `{"cyber_policy_exclude_from_ban_count":true}`,
		}},
		repo, nil, nil, nil, nil, nil, nil,
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID:          1,
		UserEmail:       "u@x.com",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		UpstreamMessage: "flagged",
		UpstreamStatus:  400,
	})

	require.Empty(t, repo.snapshotCountCalls(), "开关开时不得执行封号计数查询")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1, "风控日志必须照记")
	require.True(t, logs[0].Flagged, "日志仍标记 Flagged=true（列表可见可筛）")
	require.Equal(t, "cyber_policy", logs[0].Action)
	require.Equal(t, 0, logs[0].ViolationCount, "不参与计数时 ViolationCount 保持 0")
	require.False(t, logs[0].AutoBanned)
}

func TestRecordCyberPolicyEvent_DefaultCountsTowardBan(t *testing.T) {
	repo := &banCountArgsTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true",
		}},
		repo, nil, nil, nil, nil, nil, nil,
	)

	svc.RecordCyberPolicyEvent(context.Background(), CyberPolicyRecordInput{
		UserID:          1,
		UserEmail:       "u@x.com",
		Model:           "gpt-5",
		Endpoint:        "/v1/responses",
		UpstreamMessage: "flagged",
		UpstreamStatus:  400,
	})

	require.Equal(t, []bool{false}, repo.snapshotCountCalls(),
		"默认配置必须执行计数查询且不排除 cyber 行")
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.GreaterOrEqual(t, logs[0].ViolationCount, 1, "默认路径行为不变（现状回归）")
}

type contentModerationRawSnapshotTestRepo struct {
	contentModerationTestRepo
	rawMu sync.Mutex
	raw   map[int64]ContentModerationRawRequestSnapshot
}

func (r *contentModerationRawSnapshotTestRepo) CreateRawRequestSnapshot(ctx context.Context, snapshot *ContentModerationRawRequestSnapshot) error {
	r.rawMu.Lock()
	defer r.rawMu.Unlock()
	if r.raw == nil {
		r.raw = map[int64]ContentModerationRawRequestSnapshot{}
	}
	cp := *snapshot
	r.raw[cp.LogID] = cp

	r.mu.Lock()
	defer r.mu.Unlock()
	for idx := range r.logs {
		if r.logs[idx].ID == cp.LogID {
			r.logs[idx].RawRequestAvailable = true
			r.logs[idx].RawRequestBytes = cp.BodyBytes
			r.logs[idx].RawRequestTruncated = cp.Truncated
			break
		}
	}
	return nil
}

func (r *contentModerationRawSnapshotTestRepo) GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*ContentModerationRawRequestSnapshot, error) {
	r.rawMu.Lock()
	defer r.rawMu.Unlock()
	snapshot, ok := r.raw[logID]
	if !ok {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
	}
	return &snapshot, nil
}

func (r *contentModerationRawSnapshotTestRepo) snapshotRaw(logID int64) (ContentModerationRawRequestSnapshot, bool) {
	r.rawMu.Lock()
	defer r.rawMu.Unlock()
	snapshot, ok := r.raw[logID]
	return snapshot, ok
}

type cyberSessionBlockedRawSnapshotTestRepo struct {
	banCountArgsTestRepo
	rawMu sync.Mutex
	raw   map[int64]ContentModerationRawRequestSnapshot
}

func (r *cyberSessionBlockedRawSnapshotTestRepo) CreateRawRequestSnapshot(ctx context.Context, snapshot *ContentModerationRawRequestSnapshot) error {
	r.rawMu.Lock()
	defer r.rawMu.Unlock()
	if r.raw == nil {
		r.raw = map[int64]ContentModerationRawRequestSnapshot{}
	}
	cp := *snapshot
	r.raw[cp.LogID] = cp

	r.mu.Lock()
	defer r.mu.Unlock()
	for idx := range r.logs {
		if r.logs[idx].ID == cp.LogID {
			r.logs[idx].RawRequestAvailable = true
			r.logs[idx].RawRequestBytes = cp.BodyBytes
			r.logs[idx].RawRequestTruncated = cp.Truncated
			break
		}
	}
	return nil
}

func (r *cyberSessionBlockedRawSnapshotTestRepo) GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*ContentModerationRawRequestSnapshot, error) {
	r.rawMu.Lock()
	defer r.rawMu.Unlock()
	snapshot, ok := r.raw[logID]
	if !ok {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
	}
	return &snapshot, nil
}

type contentModerationTestEncryptor struct{}

func (contentModerationTestEncryptor) Encrypt(plaintext string) (string, error) {
	return "enc:" + base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (contentModerationTestEncryptor) Decrypt(ciphertext string) (string, error) {
	encoded := strings.TrimPrefix(ciphertext, "enc:")
	plain, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
