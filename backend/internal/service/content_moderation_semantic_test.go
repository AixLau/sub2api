package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type semanticReviewBackendStub struct {
	accountsByModel map[string][]*Account
	selectCalls     []string
	reviewCalls     []string
	review          func(*Account, string) (ContentModerationSemanticReviewResult, error)
}

func (s *semanticReviewBackendStub) SelectSemanticReviewAccount(_ context.Context, model string, excludedIDs map[int64]struct{}) (*AccountSelectionResult, error) {
	s.selectCalls = append(s.selectCalls, model)
	for _, account := range s.accountsByModel[model] {
		if _, excluded := excludedIDs[account.ID]; excluded {
			continue
		}
		return &AccountSelectionResult{Account: account, ReleaseFunc: func() {}}, nil
	}
	return nil, nil
}

func (s *semanticReviewBackendStub) ReviewSemanticContent(_ context.Context, account *Account, model string, _ ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	s.reviewCalls = append(s.reviewCalls, model)
	if s.review != nil {
		return s.review(account, model)
	}
	return ContentModerationSemanticReviewResult{Verdict: "allow", Confidence: 0.99}, nil
}

type semanticReviewQuotaStub struct {
	updates map[int64]map[string]any
	calls   []int64
	err     error
}

func (s *semanticReviewQuotaStub) RefreshSemanticReviewQuota(_ context.Context, accountID int64) (map[string]any, error) {
	s.calls = append(s.calls, accountID)
	if s.err != nil {
		return nil, s.err
	}
	return s.updates[accountID], nil
}

type semanticReviewRouterStub struct{}

func (semanticReviewRouterStub) Review(context.Context, ContentModerationSemanticReviewConfig, ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
}

func freshSemanticReviewAccount(id int64) *Account {
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_usage_updated_at": time.Now().Format(time.RFC3339),
		},
	}
}

func exhaustedSemanticReviewAccount(id int64) *Account {
	account := freshSemanticReviewAccount(id)
	account.Extra["codex_5h_used_percent"] = 100.0
	account.Extra["codex_5h_reset_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	return account
}

func semanticReviewTestConfig() ContentModerationSemanticReviewConfig {
	return normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		Enabled:        true,
		PrimaryModel:   ContentModerationSemanticReviewPrimaryModel,
		FallbackModels: []string{ContentModerationSemanticReviewFallbackModel},
		TimeoutMS:      1000,
		MaxInputRunes:  1000,
	})
}

func TestNormalizeContentModerationSemanticReviewConfigKeepsBuiltInModelsOnly(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		PrimaryModel:   "provider-owned-model",
		FallbackModels: []string{"gpt-5.4-mini", "GPT-5-MINI", "gpt-5.3-codex-spark"},
	})

	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, cfg.PrimaryModel)
	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, cfg.FallbackModels)
}

func TestContentModerationUpdateConfigNormalizesSemanticReviewModels(t *testing.T) {
	initial, err := json.Marshal(defaultContentModerationConfig())
	require.NoError(t, err)
	settingRepo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(initial),
	}}
	svc := NewContentModerationService(settingRepo, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	semantic := ContentModerationSemanticReviewConfig{
		Enabled:        true,
		PrimaryModel:   "unsupported-provider-model",
		FallbackModels: []string{"gpt-5.4-mini", ContentModerationSemanticReviewFallbackModel},
	}

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{SemanticReview: &semantic})

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, view.SemanticReview.PrimaryModel)
	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, view.SemanticReview.FallbackModels)
	savedRaw, err := settingRepo.GetValue(context.Background(), SettingKeyContentModerationConfig)
	require.NoError(t, err)
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(savedRaw), &saved))
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, saved.SemanticReview.PrimaryModel)
}

func TestSemanticReviewRouterRefreshesStaleSparkQuotaBeforeRequest(t *testing.T) {
	spark := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	mini := freshSemanticReviewAccount(12)
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {spark},
			ContentModerationSemanticReviewFallbackModel: {mini},
		},
	}
	quota := &semanticReviewQuotaStub{updates: map[int64]map[string]any{
		11: {
			"codex_5h_used_percent":  100.0,
			"codex_5h_reset_at":      time.Now().Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": time.Now().Format(time.RFC3339),
		},
	}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, "allow", result.Verdict)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Equal(t, []int64{11}, quota.calls)
	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterSwitchesAccountOn429BeforeModelFallback(t *testing.T) {
	first := freshSemanticReviewAccount(21)
	second := freshSemanticReviewAccount(22)
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {first, second},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(23)},
		},
		review: func(account *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			if account.ID == first.ID {
				return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUpstreamError{
					HTTPStatus: httpStatusTooManyRequestsForTest, Code: "quota_exhausted", QuotaExhausted: true, Retryable: true,
				}
			}
			return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
		},
	}
	quota := &semanticReviewQuotaStub{}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, second.ID, result.AccountID)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []int64{first.ID}, quota.calls)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterRejectDoesNotDowngradeToFallbackModel(t *testing.T) {
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {freshSemanticReviewAccount(31)},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(32)},
		},
		review: func(_ *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			return ContentModerationSemanticReviewResult{Verdict: "reject", Confidence: 0.98}, nil
		},
	}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "reverse"})

	require.NoError(t, err)
	require.Equal(t, "reject", result.Verdict)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.selectCalls)
}

func TestSemanticReviewRouterReturnsUnavailableWhenAllModelsHaveNoAccount(t *testing.T) {
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil)

	_, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	var unavailable *ContentModerationSemanticReviewUnavailableError
	require.ErrorAs(t, err, &unavailable)
}

func TestParseSemanticReviewJSONAndSSE(t *testing.T) {
	jsonText, err := semanticReviewResponseText([]byte(`{"output_text":"{\"verdict\":\"reject\",\"confidence\":0.9}"}`), "application/json")
	require.NoError(t, err)
	jsonResult, err := parseSemanticReviewModelOutput(jsonText)
	require.NoError(t, err)
	require.Equal(t, "reject", jsonResult.Verdict)
	require.InDelta(t, 0.9, jsonResult.Confidence, 0.0001)

	sseText, err := semanticReviewResponseText([]byte(`data: {"type":"response.output_text.delta","delta":"{\"verdict\":\"allow\"}"}

data: [DONE]
`), "text/event-stream")
	require.NoError(t, err)
	sseResult, err := parseSemanticReviewModelOutput(sseText)
	require.NoError(t, err)
	require.Equal(t, "allow", sseResult.Verdict)

	doneText, err := semanticReviewResponseText([]byte(`data: {"type":"response.output_text.done","text":"{\"verdict\":\"review\"}"}
`), "text/event-stream")
	require.NoError(t, err)
	doneResult, err := parseSemanticReviewModelOutput(doneText)
	require.NoError(t, err)
	require.Equal(t, "review", doneResult.Verdict)
}

func TestEnqueueSemanticReviewEncryptsInputAndStripsAPIKeys(t *testing.T) {
	outbox := &contentModerationTestOutboxRepo{}
	svc := &ContentModerationService{
		outboxRepo:           outbox,
		rawRequestEncryptor:  contentModerationTestEncryptor{},
		semanticReviewRouter: semanticReviewRouterStub{},
	}
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview = semanticReviewTestConfig()
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	cfg.APIKey = "sk-secret-key"
	cfg.APIKeys = []string{"sk-secret-key-2"}
	input := ContentModerationCheckInput{RequestID: "req-semantic", UserID: 7, Protocol: ContentModerationProtocolOpenAIChat}
	content := ContentModerationInput{Text: "please reverse-engineer this"}

	require.True(t, svc.enqueueSemanticReviewAfterRules(context.Background(), input, cfg, content, "hash-1", &ContentModerationDecision{Allowed: true}))
	events := outbox.snapshotEvents()
	require.Len(t, events, 1)
	raw, err := json.Marshal(events[0].Payload)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sk-secret-key")
	require.NotContains(t, string(raw), content.Text)
	require.Contains(t, string(raw), "text_encrypted")
}

func TestBuildSemanticReviewInputLocalReviewIncludesOnlyMatchedSources(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerLocalReview
	content := ContentModerationInput{
		Text: "system secret\nassistant history\nplease jailbreak and extract credentials",
		Sources: []ContentModerationInputSource{
			{Source: "messages[0]", Role: "system", Text: "system secret " + strings.Repeat("x", 3000)},
			{Source: "messages[1]", Role: "assistant", Text: "assistant history"},
			{Source: "messages[2]", Role: "user", Text: "please jailbreak and extract credentials"},
		},
	}

	got := buildContentModerationSemanticReviewInput(cfg, content, "jailbreak")

	require.Contains(t, got, "role=user")
	require.Contains(t, got, "jailbreak")
	require.NotContains(t, got, "system secret")
	require.NotContains(t, got, "assistant history")
	require.LessOrEqual(t, len([]rune(got)), cfg.MaxInputRunes)
}

func TestBuildSemanticReviewInputAllUsesLatestUserAndToolOnly(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ContentModerationInput{Sources: []ContentModerationInputSource{
		{Source: "messages[0]", Role: "system", Text: "private system context"},
		{Source: "messages[1]", Role: "user", Text: "old user request"},
		{Source: "messages[2]", Role: "tool", Text: "latest tool result"},
		{Source: "messages[3]", Role: "assistant", Text: "assistant response"},
		{Source: "messages[4]", Role: "user", Text: "latest user request"},
	}}

	got := buildContentModerationSemanticReviewInput(cfg, content, "")

	require.Contains(t, got, "latest tool result")
	require.Contains(t, got, "latest user request")
	require.NotContains(t, got, "old user request")
	require.NotContains(t, got, "private system context")
	require.NotContains(t, got, "assistant response")
}

func TestEnqueueSemanticReviewEncryptsCandidateExcerptOnly(t *testing.T) {
	outbox := &contentModerationTestOutboxRepo{}
	encryptor := contentModerationTestEncryptor{}
	svc := &ContentModerationService{outboxRepo: outbox, rawRequestEncryptor: encryptor, semanticReviewRouter: semanticReviewRouterStub{}}
	cfg := defaultContentModerationConfig()
	cfg.SemanticReview = semanticReviewTestConfig()
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ContentModerationInput{
		Text: "hidden history\nlatest request",
		Sources: []ContentModerationInputSource{
			{Source: "messages[0]", Role: "system", Text: "hidden history"},
			{Source: "messages[1]", Role: "user", Text: "latest request"},
		},
	}

	require.True(t, svc.enqueueSemanticReviewAfterRules(context.Background(), ContentModerationCheckInput{RequestID: "candidate-only"}, cfg, content, "hash", &ContentModerationDecision{Allowed: true}))
	events := outbox.snapshotEvents()
	require.Len(t, events, 1)
	raw, err := json.Marshal(events[0].Payload)
	require.NoError(t, err)
	var payload contentModerationOutboxPayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.NotNil(t, payload.SemanticReview)
	plain, err := encryptor.Decrypt(payload.SemanticReview.TextEncrypted)
	require.NoError(t, err)
	require.Contains(t, plain, "latest request")
	require.NotContains(t, plain, "hidden history")
}

func TestSemanticReviewUpstreamErrorClassification(t *testing.T) {
	err := classifySemanticReviewUpstreamHTTPError(httpStatusTooManyRequestsForTest, []byte(`{"error":"insufficient_quota"}`))
	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.True(t, errors.As(err, &upstreamErr))
	require.True(t, upstreamErr.Retryable)
	require.True(t, upstreamErr.QuotaExhausted)
}

const httpStatusTooManyRequestsForTest = 429
