package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newAccountGateTestService(t *testing.T, cfg *ContentModerationConfig) *ContentModerationService {
	t.Helper()
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		&contentModerationTestRepo{}, nil, nil, nil, nil, nil,
	)
}

func TestCheckAccountAttemptUnavailableFailsOpen(t *testing.T) {
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		AccountID:   9,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.NotNil(t, result.Decision)
	require.True(t, result.Decision.Allowed)
	require.False(t, result.Decision.Blocked)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
}

func TestCheckAccountAttemptSkipsOutOfScopeAccount(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeOAuth
	svc := newAccountGateTestService(t, cfg)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:      1,
		GroupID:     nil,
		AccountID:   9,
		AccountType: AccountTypeAPIKey,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionOutOfScope, result.Disposition)
	require.NotNil(t, result.Decision)
	require.True(t, result.Decision.Allowed)
	require.Nil(t, result.NextState)
}

func TestCheckAccountAttemptTreatsSetupTokenAsOAuth(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeOAuth
	svc := newAccountGateTestService(t, cfg)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:      1,
		AccountID:   9,
		AccountType: AccountTypeSetupToken,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionDeterministicAllow, result.Disposition)
	require.NotNil(t, result.NextState)
	require.True(t, result.NextState.Reusable)
}

func TestCheckAccountAttemptPreservesReusableStateAcrossOutOfScopeFailover(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeOAuth
	svc := newAccountGateTestService(t, cfg)
	input := ContentModerationCheckInput{
		UserID:      1,
		AccountID:   9,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}

	first, err := svc.CheckAccountAttempt(context.Background(), input, nil)
	require.NoError(t, err)
	require.NotNil(t, first.NextState)

	input.AccountID = 10
	input.AccountType = AccountTypeAPIKey
	middle, err := svc.CheckAccountAttempt(context.Background(), input, first.NextState)
	require.NoError(t, err)
	require.Same(t, first.NextState, middle.NextState)

	input.AccountID = 11
	input.AccountType = AccountTypeOAuth
	last, err := svc.CheckAccountAttempt(context.Background(), input, middle.NextState)
	require.NoError(t, err)
	require.True(t, last.Reused)
	require.Same(t, first.NextState, last.NextState)
}

func TestCheckAccountAttemptDoesNotReuseDisabledPolicyAfterEnablement(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeAll
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRiskControlEnabled:      "false",
		SettingKeyContentModerationConfig: string(raw),
	}}
	svc := &ContentModerationService{settingRepo: settings, repo: &contentModerationTestRepo{}}
	input := ContentModerationCheckInput{
		AccountID: 1, AccountType: AccountTypeAPIKey, Model: "gpt-5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}

	first, err := svc.CheckAccountAttempt(context.Background(), input, nil)
	require.NoError(t, err)
	require.NotNil(t, first.NextState)
	settings.values[SettingKeyRiskControlEnabled] = "true"
	second, err := svc.CheckAccountAttempt(context.Background(), input, first.NextState)
	require.NoError(t, err)
	require.False(t, second.Reused)
	require.NotEqual(t, first.PolicyRevision, second.PolicyRevision)
}

func TestCheckAccountAttemptReusesEnabledPolicyAcrossFailover(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	cfg.AccountScope = ContentModerationAccountScopeAll
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyRiskControlEnabled:      "true",
		SettingKeyContentModerationConfig: string(raw),
	}}
	svc := &ContentModerationService{settingRepo: settings, repo: &contentModerationTestRepo{}}
	input := ContentModerationCheckInput{
		AccountID: 1, AccountType: AccountTypeAPIKey, Model: "gpt-5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}

	first, err := svc.CheckAccountAttempt(context.Background(), input, nil)
	require.NoError(t, err)
	require.NotNil(t, first.NextState)
	settings.values[SettingKeyRiskControlEnabled] = "false"

	second, err := svc.CheckAccountAttempt(context.Background(), input, first.NextState)
	require.NoError(t, err)
	require.True(t, second.Reused)
	require.Equal(t, first.PolicyRevision, second.PolicyRevision)
}

func TestCheckAccountAttemptLoadsPolicyAfterRequestCancellation(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	svc := newAccountGateTestService(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := svc.CheckAccountAttempt(ctx, ContentModerationCheckInput{
		AccountID: 1, AccountType: AccountTypeAPIKey, Model: "gpt-5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionDeterministicAllow, result.Disposition)
}

func TestCheckAccountAttemptPolicyLoadFailurePersistsError(t *testing.T) {
	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{
			values: map[string]string{},
			errors: map[string]error{SettingKeyRiskControlEnabled: errors.New("settings unavailable")},
		},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:    17,
		AccountID: 9,
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Error, "load moderation policy")
	require.Empty(t, logs[0].InputExcerpt)
}

func TestCheckLoadsPolicyAfterRequestCancellation(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	svc := newAccountGateTestService(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	decision, err := svc.Check(ctx, ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	})

	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, ContentModerationActionAllow, decision.Action)
}

func TestCheckAccountAttemptObserveDropClearsPriorReusableState(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.APIKeys = []string{"audit-key"}
	cfg.AccountScope = ContentModerationAccountScopeAll
	cfg.QueueSize = 1
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw),
		}},
		repo:       &contentModerationTestRepo{},
		asyncQueue: make(chan contentModerationTask, 1),
	}
	svc.asyncQueue <- contentModerationTask{}
	prior := &ContentModerationAttemptState{Reusable: true, InputHash: "old", PolicyRevision: "old"}

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		AccountID: 1, AccountType: AccountTypeAPIKey, Model: "gpt-5",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, prior)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionObserveDropped, result.Disposition)
	require.Nil(t, result.NextState)
}

func TestCheckAccountAttemptObserveMissingProviderKeyPersistsError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.APIKey = ""
	cfg.APIKeys = nil
	cfg.SemanticReview.Enabled = false
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	outbox := &contentModerationTestOutboxRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSemanticReviewRouter(&contentModerationSemanticReviewRouterStub{})
	svc.SetOutboxRepository(outbox)
	svc.SetRawRequestSnapshotStore(nil, contentModerationTestEncryptor{})

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:      17,
		AccountID:   9,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Contains(t, logs[0].Error, "ordinary moderation API key unavailable")
	events := outbox.snapshotEvents()
	require.Len(t, events, 1)
	require.Equal(t, ContentModerationOutboxEventSemanticReview, events[0].EventType)
}

func TestCheckAccountAttemptObserveMissingProviderKeyPersistsErrorWithoutSemanticRouter(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeHybrid
	cfg.APIKey = ""
	cfg.APIKeys = nil
	cfg.SemanticReview.Enabled = true
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:      17,
		AccountID:   9,
		AccountType: AccountTypeOAuth,
		Model:       "gpt-5",
		Protocol:    ContentModerationProtocolOpenAIChat,
		Body:        []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionError, logs[0].Action)
	require.Contains(t, logs[0].Error, "ordinary moderation API key unavailable")
}

func TestCheckAccountAttemptObserveUnexpectedEmptyExtractionReportsProviderError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeRuleOnly
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:    17,
		AccountID: 9,
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"unknown_text_field":"unsupported content"}`),
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Error, "no auditable content")
}

func TestCheckAccountAttemptObserveCandidateOversizedPayloadWritesSingleError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModeObserve
	cfg.EngineMode = ContentModerationEngineModeCandidateOnly
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(raw),
		}},
		repo,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	body := []byte(`{"messages":[
		{"role":"user","content":"ordinary text","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{\"base64\":\"` + strings.Repeat("A", maxBase64DecodeInputBytes+4) + `\"}"}}]}
	]}`)

	result, err := svc.CheckAccountAttempt(context.Background(), ContentModerationCheckInput{
		UserID:    17,
		AccountID: 9,
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      body,
	}, nil)

	require.NoError(t, err)
	require.Equal(t, ContentModerationDispositionProviderErrorOpen, result.Disposition)
	require.Equal(t, ContentModerationActionError, result.Decision.Action)
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Error, "oversized encoded payload skipped")
}
