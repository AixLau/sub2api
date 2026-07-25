package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type semanticReviewBackendStub struct {
	accountsByModel map[string][]*Account
	selectCalls     []string
	selectGroupIDs  []*int64
	reviewCalls     []string
	review          func(context.Context, *Account, string) (ContentModerationSemanticReviewResult, error)
}

func (s *semanticReviewBackendStub) SelectSemanticReviewAccount(_ context.Context, groupID *int64, model string, excludedIDs map[int64]struct{}) (*AccountSelectionResult, error) {
	s.selectCalls = append(s.selectCalls, model)
	s.selectGroupIDs = append(s.selectGroupIDs, cloneInt64Ptr(groupID))
	for _, account := range s.accountsByModel[model] {
		if _, excluded := excludedIDs[account.ID]; excluded {
			continue
		}
		return &AccountSelectionResult{Account: account, ReleaseFunc: func() {}}, nil
	}
	return nil, nil
}

func (s *semanticReviewBackendStub) ReviewSemanticContent(ctx context.Context, account *Account, model string, _ ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	s.reviewCalls = append(s.reviewCalls, model)
	if s.review != nil {
		return s.review(ctx, account, model)
	}
	return ContentModerationSemanticReviewResult{Verdict: "allow", Confidence: 0.99}, nil
}

type semanticReviewQuotaStub struct {
	updates map[int64]map[string]any
	mu      sync.Mutex
	calls   []int64
	err     error
}

type blockingSemanticReviewQuotaStub struct {
	started chan int64
	release chan struct{}
}

func (s *blockingSemanticReviewQuotaStub) RefreshSemanticReviewQuota(_ context.Context, accountID int64) (map[string]any, error) {
	s.started <- accountID
	<-s.release
	return nil, nil
}

type contextBlockingSemanticReviewQuotaStub struct {
	started chan int64
	calls   int
	mu      sync.Mutex
}

func (s *contextBlockingSemanticReviewQuotaStub) RefreshSemanticReviewQuota(ctx context.Context, accountID int64) (map[string]any, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case s.started <- accountID:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *contextBlockingSemanticReviewQuotaStub) snapshotCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

type semanticReviewUsageRecorderStub struct {
	records []PlatformUsageRecord
	err     error
}

type semanticReviewCooldownRepo struct {
	AccountRepository
	contextErr error
	calls      []semanticReviewModelRateLimitCall
}

type semanticReviewModelRateLimitCall struct {
	accountID int64
	scope     string
	reason    string
}

func (r *semanticReviewCooldownRepo) SetModelRateLimit(ctx context.Context, id int64, scope string, _ time.Time, reason ...string) error {
	r.contextErr = ctx.Err()
	call := semanticReviewModelRateLimitCall{accountID: id, scope: scope}
	if len(reason) > 0 {
		call.reason = reason[0]
	}
	r.calls = append(r.calls, call)
	return nil
}

func (s *semanticReviewUsageRecorderStub) Record(_ context.Context, record PlatformUsageRecord) error {
	s.records = append(s.records, record)
	return s.err
}

func (s *semanticReviewQuotaStub) RefreshSemanticReviewQuota(_ context.Context, accountID int64) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, accountID)
	if s.err != nil {
		return nil, s.err
	}
	return s.updates[accountID], nil
}

func (s *semanticReviewQuotaStub) snapshotCalls() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.calls...)
}

type semanticReviewRouterStub struct{}

func (semanticReviewRouterStub) Review(context.Context, ContentModerationSemanticReviewConfig, ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
}

type semanticReviewTerminalReader struct {
	data []byte
	err  error
}

func (r *semanticReviewTerminalReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
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

func TestNormalizeContentModerationSemanticReviewConfigReplacesLegacyMini(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		PrimaryModel:   ContentModerationSemanticReviewPrimaryModel,
		FallbackModels: []string{"gpt-5-mini"},
	})

	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, cfg.FallbackModels)
}

func TestNormalizeContentModerationSemanticReviewConfigAppliesBoundedInferenceDefaults(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		TimeoutMS: ContentModerationSemanticReviewLegacyTimeoutMS,
	})

	require.Equal(t, ContentModerationSemanticReviewDefaultTimeoutMS, cfg.TimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewPrimaryTimeoutMS, cfg.PrimaryTimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewFallbackTimeoutMS, cfg.FallbackTimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewDefaultModelAttempts, cfg.MaxAttemptsPerModel)
	require.Equal(t, ContentModerationSemanticReviewDefaultOutputTokens, cfg.MaxOutputTokens)
	require.Equal(t, ContentModerationSemanticReviewDefaultReasoning, cfg.ReasoningEffort)
}

func TestNormalizeContentModerationSemanticReviewConfigMigratesPreviousDefaults(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		TimeoutMS:         8_000,
		PrimaryTimeoutMS:  5_000,
		FallbackTimeoutMS: 3_000,
	})

	require.Equal(t, ContentModerationSemanticReviewDefaultTimeoutMS, cfg.TimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewPrimaryTimeoutMS, cfg.PrimaryTimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewFallbackTimeoutMS, cfg.FallbackTimeoutMS)
}

func TestNormalizeContentModerationSemanticReviewConfigForcesLowReasoning(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		ReasoningEffort: "minimal",
	})

	require.Equal(t, "low", cfg.ReasoningEffort)
}

func TestNormalizeContentModerationSemanticReviewConfigPreservesExplicitTwentySecondBudget(t *testing.T) {
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{
		TimeoutMS:           ContentModerationSemanticReviewLegacyTimeoutMS,
		PrimaryTimeoutMS:    12_000,
		FallbackTimeoutMS:   8_000,
		MaxAttemptsPerModel: 1,
		MaxOutputTokens:     99_999,
		ReasoningEffort:     "high",
	})

	require.Equal(t, ContentModerationSemanticReviewLegacyTimeoutMS, cfg.TimeoutMS)
	require.Equal(t, 12_000, cfg.PrimaryTimeoutMS)
	require.Equal(t, 8_000, cfg.FallbackTimeoutMS)
	require.Equal(t, ContentModerationSemanticReviewMaxOutputTokens, cfg.MaxOutputTokens)
	require.Equal(t, ContentModerationSemanticReviewDefaultReasoning, cfg.ReasoningEffort)
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
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, "allow", result.Verdict)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
	require.Equal(t, []int64{11}, quota.snapshotCalls())
}

func TestSemanticReviewRouterUsesStaleSnapshotWhenSynchronousQuotaRefreshFails(t *testing.T) {
	spark := &Account{ID: 13, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel: {spark},
	}}
	quota := &semanticReviewQuotaStub{err: errors.New("quota service unavailable")}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
	require.Equal(t, []int64{spark.ID}, quota.snapshotCalls())
}

func TestSemanticReviewRouterRefreshesIndependentSparkQuotaForGloballyRateLimitedAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	spark := freshSemanticReviewAccount(15)
	spark.RateLimitResetAt = &resetAt
	spark.Extra["codex_5h_used_percent"] = 100.0
	spark.Extra["codex_5h_reset_at"] = resetAt.Format(time.RFC3339)
	spark.Extra["codex_usage_dimension"] = "global"
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel: {spark},
	}}
	quota := &semanticReviewQuotaStub{updates: map[int64]map[string]any{
		spark.ID: {
			"codex_5h_used_percent":  25.0,
			"codex_5h_reset_at":      resetAt.Format(time.RFC3339),
			"codex_usage_updated_at": time.Now().Format(time.RFC3339),
			"codex_usage_dimension":  "spark",
		},
	}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
	require.Equal(t, []int64{spark.ID}, quota.snapshotCalls())

	mergeAccountExtra(spark, quota.updates[spark.ID])
	_, err = router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test again"})

	require.NoError(t, err)
	require.Equal(t, []int64{spark.ID}, quota.snapshotCalls(), "fresh spark quota snapshot should be reused")
}

func TestSemanticReviewRouterBoundsSynchronousQuotaRefreshTimeout(t *testing.T) {
	spark := &Account{ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel: {spark},
	}}
	quota := &contextBlockingSemanticReviewQuotaStub{started: make(chan int64, 1)}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	started := time.Now()
	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.GreaterOrEqual(t, elapsed, contentModerationSemanticReviewQuotaSyncTimeout-50*time.Millisecond)
	require.Less(t, elapsed, contentModerationSemanticReviewQuotaSyncTimeout+300*time.Millisecond)
	require.Equal(t, 1, quota.snapshotCalls())
}

func TestSemanticReviewRouterSingleflightsConcurrentSynchronousQuotaRefresh(t *testing.T) {
	quota := &contextBlockingSemanticReviewQuotaStub{started: make(chan int64, 2)}
	router := NewOpenAIContentModerationSemanticReviewRouter(&semanticReviewBackendStub{}, quota, nil).(*openAIContentModerationSemanticReviewRouter)
	results := make(chan error, 2)
	go func() {
		_, err := router.refreshSemanticReviewQuotaSync(context.Background(), 15)
		results <- err
	}()
	require.Equal(t, int64(15), <-quota.started)
	go func() {
		_, err := router.refreshSemanticReviewQuotaSync(context.Background(), 15)
		results <- err
	}()

	for i := 0; i < 2; i++ {
		require.ErrorIs(t, <-results, context.DeadlineExceeded)
	}
	require.Equal(t, 1, quota.snapshotCalls())
}

func TestSemanticReviewRouterBoundsBackgroundQuotaRefreshConcurrency(t *testing.T) {
	quota := &blockingSemanticReviewQuotaStub{
		started: make(chan int64, 10),
		release: make(chan struct{}),
	}
	router := NewOpenAIContentModerationSemanticReviewRouter(&semanticReviewBackendStub{}, quota, nil).(*openAIContentModerationSemanticReviewRouter)

	for accountID := int64(1); accountID <= 10; accountID++ {
		router.refreshSemanticReviewQuotaAsync(accountID)
	}

	for i := 0; i < contentModerationSemanticReviewQuotaRefreshWorkers; i++ {
		select {
		case <-quota.started:
		case <-time.After(time.Second):
			t.Fatal("background quota refresh did not start")
		}
	}
	select {
	case accountID := <-quota.started:
		t.Fatalf("unexpected unbounded quota refresh for account %d", accountID)
	case <-time.After(50 * time.Millisecond):
	}
	close(quota.release)
	require.Eventually(t, func() bool {
		return len(router.refreshSlots) == 0
	}, time.Second, 10*time.Millisecond)
}

func TestSemanticReviewRouterFallsBackAfterOnePrimaryAccountAttempt(t *testing.T) {
	first := freshSemanticReviewAccount(21)
	second := freshSemanticReviewAccount(22)
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {first, second},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(23)},
		},
		review: func(_ context.Context, account *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			if account.ID == first.ID {
				return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUpstreamError{
					HTTPStatus: httpStatusTooManyRequestsForTest, Code: "quota_exhausted", QuotaExhausted: true, Retryable: true,
				}
			}
			return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
		},
	}
	quota := &semanticReviewQuotaStub{}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, int64(23), result.AccountID)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Eventually(t, func() bool {
		return len(quota.snapshotCalls()) == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, []int64{first.ID}, quota.snapshotCalls())
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterFallsBackWhenPrimaryModelIsUnsupported(t *testing.T) {
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {freshSemanticReviewAccount(24)},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(25)},
		},
		review: func(_ context.Context, _ *Account, model string) (ContentModerationSemanticReviewResult, error) {
			if model == ContentModerationSemanticReviewPrimaryModel {
				return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUpstreamError{
					HTTPStatus: http.StatusBadRequest,
					Code:       "model_unsupported",
					Message:    "model is not supported when using Codex with a ChatGPT account",
				}
			}
			return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
		},
	}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterTriesAnotherPrimaryAccountWhenModelIsUnsupported(t *testing.T) {
	first := freshSemanticReviewAccount(26)
	second := freshSemanticReviewAccount(27)
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {first, second},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(28)},
		},
		review: func(_ context.Context, account *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			if account.ID == first.ID {
				return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUpstreamError{
					HTTPStatus: http.StatusBadRequest,
					Code:       "model_unsupported",
					Message:    "model is not supported when using Codex with a ChatGPT account",
				}
			}
			return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
		},
	}
	cfg := semanticReviewTestConfig()
	cfg.MaxAttemptsPerModel = 2
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	result, err := router.Review(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "test"})

	require.NoError(t, err)
	require.Equal(t, second.ID, result.AccountID)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterRejectDoesNotDowngradeToFallbackModel(t *testing.T) {
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {freshSemanticReviewAccount(31)},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(32)},
		},
		review: func(_ context.Context, _ *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			return ContentModerationSemanticReviewResult{Verdict: "reject", Confidence: 0.98}, nil
		},
	}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "reverse"})

	require.NoError(t, err)
	require.Equal(t, "reject", result.Verdict)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.reviewCalls)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel}, backend.selectCalls)
}

func TestSemanticReviewRouterReturnsUnavailableWhenAllModelsHaveNoAccount(t *testing.T) {
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	_, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

	var unavailable *ContentModerationSemanticReviewUnavailableError
	require.ErrorAs(t, err, &unavailable)
}

func TestSemanticReviewRouterSharesOneBudgetAcrossPrimaryAndFallback(t *testing.T) {
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel:  {freshSemanticReviewAccount(33)},
			ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(34)},
		},
		review: func(ctx context.Context, _ *Account, model string) (ContentModerationSemanticReviewResult, error) {
			if model == ContentModerationSemanticReviewFallbackModel {
				return ContentModerationSemanticReviewResult{Verdict: "allow"}, nil
			}
			<-ctx.Done()
			return ContentModerationSemanticReviewResult{}, ctx.Err()
		},
	}
	cfg := semanticReviewTestConfig()
	cfg.TimeoutMS = 120
	cfg.PrimaryTimeoutMS = 70
	cfg.FallbackTimeoutMS = 50
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	started := time.Now()
	result, err := router.Review(context.Background(), cfg, ContentModerationSemanticReviewInput{Text: "test"})
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Less(t, elapsed, 200*time.Millisecond)
	require.GreaterOrEqual(t, elapsed, 60*time.Millisecond)
	require.Equal(t, []string{ContentModerationSemanticReviewPrimaryModel, ContentModerationSemanticReviewFallbackModel}, backend.reviewCalls)
}

func TestSemanticReviewRouterKeepsRequestGroupDuringAccountSelection(t *testing.T) {
	groupID := int64(17)
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel: {freshSemanticReviewAccount(41)},
	}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, nil)

	_, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{
		Text:    "review this candidate",
		GroupID: &groupID,
	})

	require.NoError(t, err)
	require.Len(t, backend.selectGroupIDs, 1)
	require.NotNil(t, backend.selectGroupIDs[0])
	require.Equal(t, groupID, *backend.selectGroupIDs[0])
}

func TestSemanticReviewFallbackGroupIDsCrossesBusinessGroups(t *testing.T) {
	preferredGroupID := int64(9)
	accounts := []Account{
		{ID: 1, GroupIDs: []int64{17}, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
		{ID: 2, AccountGroups: []AccountGroup{{GroupID: 12}}, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
		{ID: 3, GroupIDs: []int64{9}, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
		{ID: 4, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
	}

	groupIDs := semanticReviewFallbackGroupIDs(&preferredGroupID, ContentModerationSemanticReviewPrimaryModel, accounts, nil)

	require.Len(t, groupIDs, 3)
	require.Equal(t, int64(12), *groupIDs[0])
	require.Equal(t, int64(17), *groupIDs[1])
	require.Nil(t, groupIDs[2])
}

func TestSemanticReviewFallbackGroupIDsHonorsModelAndExclusions(t *testing.T) {
	preferredGroupID := int64(9)
	accounts := []Account{
		{ID: 1, GroupIDs: []int64{12}, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
		{ID: 2, GroupIDs: []int64{17}, Credentials: map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}},
		{ID: 3, GroupIDs: []int64{21}, Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}}},
	}

	groupIDs := semanticReviewFallbackGroupIDs(&preferredGroupID, ContentModerationSemanticReviewPrimaryModel, accounts, map[int64]struct{}{1: {}})

	require.Len(t, groupIDs, 1)
	require.Equal(t, int64(21), *groupIDs[0])
}

func TestSemanticReviewFallbackGroupIDsDoesNotRetryUngroupedScope(t *testing.T) {
	accounts := []Account{{
		ID:          1,
		Credentials: map[string]any{"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel}},
	}}

	require.Empty(t, semanticReviewFallbackGroupIDs(nil, ContentModerationSemanticReviewPrimaryModel, accounts, nil))
}

func TestSelectSemanticReviewAccountFallsBackAcrossBusinessGroups(t *testing.T) {
	businessGroupID := int64(9)
	auditGroupID := int64(17)
	account := Account{
		ID:            71,
		Platform:      PlatformOpenAI,
		Type:          AccountTypeAPIKey,
		Status:        StatusActive,
		Schedulable:   true,
		Concurrency:   1,
		AccountGroups: []AccountGroup{{GroupID: auditGroupID}},
		Credentials: map[string]any{
			"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel},
		},
	}
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{accountRepo: repo}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), &businessGroupID, ContentModerationSemanticReviewPrimaryModel, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSelectSemanticReviewAccountUsesNormalOAuthSparkQuotaDuringGlobal429(t *testing.T) {
	groupID := int64(29)
	resetAt := time.Now().Add(time.Hour)
	account := Account{
		ID:               75,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		Status:           StatusActive,
		Schedulable:      true,
		Concurrency:      1,
		RateLimitResetAt: &resetAt,
		AccountGroups:    []AccountGroup{{GroupID: groupID}},
		Credentials: map[string]any{
			"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel},
		},
	}
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{accountRepo: repo}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), &groupID, ContentModerationSemanticReviewPrimaryModel, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.False(t, selection.Account.IsShadow())
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSelectSemanticReviewAccountFallsBackToUngroupedAccount(t *testing.T) {
	businessGroupID := int64(9)
	account := Account{
		ID:          72,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel},
		},
	}
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{accountRepo: repo}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), &businessGroupID, ContentModerationSemanticReviewPrimaryModel, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestSelectSemanticReviewAccountKeepsExclusionsAcrossGroups(t *testing.T) {
	businessGroupID := int64(9)
	auditGroupID := int64(17)
	account := Account{
		ID:            73,
		Platform:      PlatformOpenAI,
		Type:          AccountTypeAPIKey,
		Status:        StatusActive,
		Schedulable:   true,
		Concurrency:   1,
		AccountGroups: []AccountGroup{{GroupID: auditGroupID}},
		Credentials: map[string]any{
			"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel},
		},
	}
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{accountRepo: repo}

	selection, err := svc.SelectSemanticReviewAccount(
		context.Background(),
		&businessGroupID,
		ContentModerationSemanticReviewPrimaryModel,
		map[int64]struct{}{account.ID: {}},
	)

	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Nil(t, selection)
}

func TestSelectSemanticReviewAccountDoesNotConsumeConcurrency(t *testing.T) {
	businessGroupID := int64(9)
	auditGroupID := int64(17)
	account := Account{
		ID:            74,
		Platform:      PlatformOpenAI,
		Type:          AccountTypeAPIKey,
		Status:        StatusActive,
		Schedulable:   true,
		Concurrency:   1,
		AccountGroups: []AccountGroup{{GroupID: auditGroupID}},
		Credentials: map[string]any{
			"model_mapping": map[string]any{ContentModerationSemanticReviewPrimaryModel: ContentModerationSemanticReviewPrimaryModel},
		},
	}
	repo := groupAwareStubOpenAIAccountRepo{stubOpenAIAccountRepo{accounts: []Account{account}}}
	concurrencyCache := stubConcurrencyCache{
		acquireResults: map[int64]bool{account.ID: false},
		loadMap: map[int64]*AccountLoadInfo{
			account.ID: {AccountID: account.ID, CurrentConcurrency: 1, LoadRate: 1},
		},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        repo,
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}

	selection, err := svc.SelectSemanticReviewAccount(context.Background(), &businessGroupID, ContentModerationSemanticReviewPrimaryModel, nil)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, account.ID, selection.Account.ID)
	require.True(t, selection.Acquired)
	require.NotNil(t, selection.ReleaseFunc)
	selection.ReleaseFunc()
}

func TestSemanticReviewRouterRecordsPlatformUsage(t *testing.T) {
	groupID := int64(23)
	account := freshSemanticReviewAccount(42)
	backend := &semanticReviewBackendStub{
		accountsByModel: map[string][]*Account{
			ContentModerationSemanticReviewPrimaryModel: {account},
		},
		review: func(_ context.Context, _ *Account, _ string) (ContentModerationSemanticReviewResult, error) {
			return ContentModerationSemanticReviewResult{
				Verdict:       "allow",
				UpstreamModel: "gpt-5.3-codex-spark-upstream",
				Usage: OpenAIUsage{
					InputTokens:              120,
					OutputTokens:             12,
					CacheReadInputTokens:     20,
					CacheCreationInputTokens: 10,
				},
			}, nil
		},
	}
	recorder := &semanticReviewUsageRecorderStub{}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, nil, recorder)

	_, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{
		Text:          "candidate",
		GroupID:       &groupID,
		UsageRecordID: "usage-cm-decision-42",
	})

	require.NoError(t, err)
	require.Len(t, recorder.records, 1)
	record := recorder.records[0]
	require.Equal(t, UsageSourceContentModeration, record.Source)
	require.Same(t, account, record.Account)
	require.Equal(t, "usage-cm-decision-42", record.RequestID)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, record.Model)
	require.Equal(t, "gpt-5.3-codex-spark-upstream", record.UpstreamModel)
	require.NotNil(t, record.GroupID)
	require.Equal(t, groupID, *record.GroupID)
	require.Equal(t, 120, record.Usage.InputTokens)
}

func TestReviewSemanticContentSupportsOpenAIAPIKeyAccounts(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"id":"resp_semantic_api_key",
			"output_text":"{\"verdict\":\"allow\"}",
			"usage":{"input_tokens":44,"output_tokens":5}
		}`)),
	}}
	svc := &OpenAIGatewayService{
		httpUpstream:   upstream,
		cfg:            &config.Config{},
		settingService: newOpenAICodexUASettingService("codex_vscode/9.9.9"),
	}
	account := &Account{
		ID:       51,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":       "semantic-api-key",
			"model_mapping": map[string]any{"gpt-5.4-mini": "gpt-5.4-mini-upstream"},
		},
	}

	result, err := svc.ReviewSemanticContent(context.Background(), account, "gpt-5.4-mini", ContentModerationSemanticReviewInput{Text: "candidate", ReasoningEffort: "minimal"})

	require.NoError(t, err)
	require.Equal(t, "allow", result.Verdict)
	require.Equal(t, "gpt-5.4-mini-upstream", result.UpstreamModel)
	require.Equal(t, "resp_semantic_api_key", result.RequestID)
	require.Equal(t, 44, result.Usage.InputTokens)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, openaiPlatformAPIURL, upstream.lastReq.URL.String())
	require.Equal(t, "Bearer semantic-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "codex_vscode/9.9.9", upstream.lastReq.Header.Get("User-Agent"))
	var requestBody map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &requestBody))
	require.Equal(t, "gpt-5.4-mini-upstream", requestBody["model"])
	require.Equal(t, float64(ContentModerationSemanticReviewDefaultOutputTokens), requestBody["max_output_tokens"])
	require.Equal(t, semanticReviewInstructions, requestBody["instructions"])
	require.Equal(t, "semantic-review-instructions-v2", semanticReviewInstructionsRevision)
	require.Contains(t, semanticReviewInstructions, "Use review only as the final fallback")
	require.Contains(t, semanticReviewInstructions, "Review is forbidden solely because of low confidence")
	require.Contains(t, semanticReviewInstructions, "Otherwise set authorization=not_applicable")
	require.Contains(t, semanticReviewInstructions, "Insufficient evidence of a violation is not by itself a reason to review or reject")
	reasoning, ok := requestBody["reasoning"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, ContentModerationSemanticReviewDefaultReasoning, reasoning["effort"])
	textConfig, ok := requestBody["text"].(map[string]any)
	require.True(t, ok)
	format, ok := textConfig["format"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "json_schema", format["type"])
	require.Equal(t, "semantic_review_v3", format["name"])
	require.Equal(t, true, format["strict"])
}

func TestReviewSemanticContentHonorsEffectiveInputLimitAcrossTransports(t *testing.T) {
	tests := []struct {
		name           string
		accountType    string
		contentType    string
		response       string
		effectiveLimit int
	}{
		{
			name:           "api-key-json",
			accountType:    AccountTypeAPIKey,
			contentType:    "application/json",
			response:       `{"id":"resp_json","output_text":"{\"verdict\":\"allow\"}"}`,
			effectiveLimit: 6_000,
		},
		{
			name:           "oauth-sse",
			accountType:    AccountTypeOAuth,
			contentType:    "text/event-stream",
			response:       "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_sse\",\"output_text\":\"{\\\"verdict\\\":\\\"allow\\\"}\"}}\n\n",
			effectiveLimit: 12_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix, suffix := "head-canary ", " tail-canary"
			longText := prefix + strings.Repeat("界", tt.effectiveLimit-len([]rune(prefix))-len([]rune(suffix))) + suffix
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{tt.contentType}},
				Body:       io.NopCloser(strings.NewReader(tt.response)),
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{
				ID:       52,
				Platform: PlatformOpenAI,
				Type:     tt.accountType,
				Credentials: map[string]any{
					"api_key":       "semantic-api-key",
					"access_token":  "semantic-oauth-token",
					"model_mapping": map[string]any{"review-model": "review-model-upstream"},
				},
			}

			_, err := svc.ReviewSemanticContent(context.Background(), account, "review-model", ContentModerationSemanticReviewInput{
				Text:          longText,
				MaxInputRunes: tt.effectiveLimit,
			})

			require.NoError(t, err)
			var requestBody map[string]any
			require.NoError(t, json.Unmarshal(upstream.lastBody, &requestBody))
			inputItems := requestBody["input"].([]any)
			content := inputItems[0].(map[string]any)["content"].([]any)
			sentText := content[0].(map[string]any)["text"].(string)
			require.Equal(t, longText, sentText)
			require.Contains(t, sentText, "head-canary")
			require.Contains(t, sentText, "tail-canary")
			require.Equal(t, tt.accountType == AccountTypeOAuth, requestBody["stream"])
		})
	}
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

func TestParseSemanticReviewResponseExtractsFieldsFromJSONRoot(t *testing.T) {
	tests := []struct {
		name              string
		body              string
		wantText          string
		wantRequestID     string
		wantInputTokens   int
		wantOutputTokens  int
		wantCachedTokens  int
		wantCreatedTokens int
	}{
		{
			name: "root response fields with legacy token names",
			body: `{
				"id":"resp_root",
				"output_text":"{\"verdict\":\"allow\"}",
				"usage":{
					"prompt_tokens":11,
					"completion_tokens":4,
					"prompt_tokens_details":{"cached_tokens":3,"cache_write_tokens":2}
				}
			}`,
			wantText:          `{"verdict":"allow"}`,
			wantRequestID:     "resp_root",
			wantInputTokens:   11,
			wantOutputTokens:  4,
			wantCachedTokens:  3,
			wantCreatedTokens: 2,
		},
		{
			name: "nested response output parts",
			body: `{
				"type":"response.completed",
				"response":{
					"id":"resp_nested",
					"output":[
						{"content":[{"type":"output_text","text":"{\"verdict\":"}]},
						{"content":[{"type":"output_text","text":"\"review\"}"}]}
					],
					"usage":{"input_tokens":7,"output_tokens":2}
				}
			}`,
			wantText:         `{"verdict":"review"}`,
			wantRequestID:    "resp_nested",
			wantInputTokens:  7,
			wantOutputTokens: 2,
		},
		{
			name:     "typed output text event",
			body:     `{"type":"response.output_text.done","text":"{\"verdict\":\"reject\"}"}`,
			wantText: `{"verdict":"reject"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, err := parseSemanticReviewResponse([]byte(tt.body), "application/json")

			require.NoError(t, err)
			require.Equal(t, tt.wantText, response.Text)
			require.Equal(t, tt.wantRequestID, response.RequestID)
			require.Equal(t, tt.wantInputTokens, response.Usage.InputTokens)
			require.Equal(t, tt.wantOutputTokens, response.Usage.OutputTokens)
			require.Equal(t, tt.wantCachedTokens, response.Usage.CacheReadInputTokens)
			require.Equal(t, tt.wantCreatedTokens, response.Usage.CacheCreationInputTokens)
		})
	}
}

func TestParseSemanticReviewResponseRejectsMalformedJSON(t *testing.T) {
	_, err := parseSemanticReviewResponse([]byte(`{"output_text":"ok"`), "application/json")

	require.EqualError(t, err, "parse semantic review response: invalid JSON")
}

func TestParseSemanticReviewResponseRejectsNonArrayOutputContent(t *testing.T) {
	_, err := parseSemanticReviewResponse([]byte(`{"output":[{"content":{"nested":{"text":"unexpected"}}}]}`), "application/json")

	require.EqualError(t, err, "semantic review response contained no text")
}

func TestParseSemanticReviewSSEReusesFrameFields(t *testing.T) {
	response, err := parseSemanticReviewSSE(strings.NewReader(`data: {not-json}

data: {"type":"response.output_text.delta","id":"resp_delta","delta":"{\"verdict\":","usage":{"prompt_tokens":5,"completion_tokens":1}}

data: {"type":"response.output_text.delta","delta":"\"allow\"}"}

data: {"type":"response.completed","response":{"id":"resp_completed","output_text":"ignored because deltas take precedence","usage":{"input_tokens":8,"output_tokens":3}}}

`), time.Time{})

	require.NoError(t, err)
	require.Equal(t, `{"verdict":"allow"}`, response.Text)
	require.Equal(t, "resp_completed", response.RequestID)
	require.Equal(t, 8, response.Usage.InputTokens)
	require.Equal(t, 3, response.Usage.OutputTokens)
}

func TestParseSemanticReviewSSEReadsRootErrorFields(t *testing.T) {
	_, err := parseSemanticReviewSSE(strings.NewReader(`data: {"type":"error","error":{"type":"invalid_parameter","message":"bad schema"}}

`), time.Time{})

	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusBadRequest, upstreamErr.HTTPStatus)
	require.Equal(t, "bad schema", upstreamErr.Message)
	require.False(t, upstreamErr.Retryable)
}

func BenchmarkParseSemanticReviewResponse(b *testing.B) {
	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "json",
			contentType: "application/json",
			body:        []byte(`{"id":"resp_bench","output":[{"type":"message","content":[{"type":"output_text","text":"{\"verdict\":\"allow\",\"confidence\":0.99}"}]}],"usage":{"input_tokens":128,"output_tokens":24,"input_tokens_details":{"cached_tokens":64}}}`),
		},
		{
			name:        "sse",
			contentType: "text/event-stream",
			body: []byte(`data: {"type":"response.output_text.delta","delta":"{\"verdict\":\"allow\","}

data: {"type":"response.output_text.delta","delta":"\"confidence\":0.99}"}

data: {"type":"response.completed","response":{"id":"resp_bench","usage":{"input_tokens":128,"output_tokens":24,"input_tokens_details":{"cached_tokens":64}}}}

`),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				response, err := parseSemanticReviewResponse(tt.body, tt.contentType)
				if err != nil || response.Text == "" || response.RequestID != "resp_bench" {
					b.Fatalf("unexpected parse result: response=%+v err=%v", response, err)
				}
			}
		})
	}
}

func TestParseSemanticReviewSSECapturesFirstOutputTokenLatency(t *testing.T) {
	started := time.Now().Add(-50 * time.Millisecond)
	response, err := parseSemanticReviewSSE(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"{\"verdict\":\"allow\"}"}

data: [DONE]
`), started)

	require.NoError(t, err)
	require.NotNil(t, response.FirstTokenMS)
	require.GreaterOrEqual(t, *response.FirstTokenMS, 40)
}

func TestParseSemanticReviewSSEReturnsAtDoneSentinelWithoutWaitingForEOF(t *testing.T) {
	reader := &semanticReviewTerminalReader{
		data: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"{\\\"verdict\\\":\\\"allow\\\"}\"}\n\ndata: [DONE]\n\n"),
		err:  context.DeadlineExceeded,
	}

	response, err := parseSemanticReviewSSE(reader, time.Time{})

	require.NoError(t, err)
	require.Equal(t, `{"verdict":"allow"}`, response.Text)
}

func TestParseSemanticReviewSSEReturnsAtCompletedWithoutWaitingForEOF(t *testing.T) {
	reader := &semanticReviewTerminalReader{
		data: []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_completed\",\"output_text\":\"{\\\"verdict\\\":\\\"review\\\"}\",\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"),
		err:  context.DeadlineExceeded,
	}

	response, err := parseSemanticReviewSSE(reader, time.Time{})

	require.NoError(t, err)
	require.Equal(t, `{"verdict":"review"}`, response.Text)
	require.Equal(t, "resp_completed", response.RequestID)
	require.Equal(t, 7, response.Usage.InputTokens)
}

func TestParseSemanticReviewSSEReturnsAtDoneWithoutWaitingForEOF(t *testing.T) {
	reader := &semanticReviewTerminalReader{
		data: []byte("data: {\"type\":\"response.done\",\"response\":{\"id\":\"resp_done\",\"output_text\":\"{\\\"verdict\\\":\\\"allow\\\"}\",\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}\n\n"),
		err:  context.DeadlineExceeded,
	}

	response, err := parseSemanticReviewSSE(reader, time.Time{})

	require.NoError(t, err)
	require.Equal(t, `{"verdict":"allow"}`, response.Text)
	require.Equal(t, "resp_done", response.RequestID)
	require.Equal(t, 5, response.Usage.InputTokens)
}

func TestParseSemanticReviewSSEReturnsUnsupportedModelFailureWithoutWaitingForEOF(t *testing.T) {
	reader := &semanticReviewTerminalReader{
		data: []byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"invalid_request_error\",\"message\":\"The 'gpt-5.3-codex-spark' model is not supported when using Codex with a ChatGPT account.\"}}}\n\n"),
		err:  context.DeadlineExceeded,
	}

	_, err := parseSemanticReviewSSE(reader, time.Time{})

	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, "model_unsupported", upstreamErr.Code)
	require.False(t, upstreamErr.Retryable)
}

func TestParseSemanticReviewSSEReturnsIncompleteWithoutWaitingForEOF(t *testing.T) {
	reader := &semanticReviewTerminalReader{
		data: []byte("data: {\"type\":\"response.incomplete\",\"response\":{\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"),
		err:  context.DeadlineExceeded,
	}

	_, err := parseSemanticReviewSSE(reader, time.Time{})

	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, "incomplete_response", upstreamErr.Code)
	require.True(t, upstreamErr.Retryable)
}

func TestParseSemanticReviewSSEClassifiesDeterministicAndTransientFailures(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		retryable bool
	}{
		{name: "invalid request", code: "invalid_request_error", retryable: false},
		{name: "invalid schema", code: "invalid_schema", retryable: false},
		{name: "invalid parameter", code: "invalid_parameter", retryable: false},
		{name: "server error", code: "server_error", retryable: true},
		{name: "rate limit", code: "rate_limit_exceeded", retryable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := map[string]any{
				"type": "response.failed",
				"response": map[string]any{
					"error": map[string]any{"code": tt.code, "message": "upstream failure"},
				},
			}
			payload, err := json.Marshal(event)
			require.NoError(t, err)

			_, err = parseSemanticReviewSSE(strings.NewReader("data: "+string(payload)+"\n\n"), time.Time{})

			var upstreamErr *ContentModerationSemanticReviewUpstreamError
			require.ErrorAs(t, err, &upstreamErr)
			require.Equal(t, tt.retryable, upstreamErr.Retryable)
			if tt.retryable {
				require.Zero(t, upstreamErr.HTTPStatus)
			} else {
				require.Equal(t, http.StatusBadRequest, upstreamErr.HTTPStatus)
			}
		})
	}
}

func TestSemanticReviewUnsupportedModelCooldownSurvivesAttemptDeadline(t *testing.T) {
	repo := &semanticReviewCooldownRepo{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := freshSemanticReviewAccount(29)
	attemptCtx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-attemptCtx.Done()
	body := []byte(`{"detail":"The 'gpt-5.3-codex-spark' model is not supported when using Codex with a ChatGPT account."}`)

	svc.persistSemanticReviewUnsupportedModelCooldown(attemptCtx, account, http.StatusBadRequest, http.Header{}, body, ContentModerationSemanticReviewPrimaryModel)

	require.NoError(t, repo.contextErr)
	require.Len(t, repo.calls, 1)
	require.Equal(t, account.ID, repo.calls[0].accountID)
	require.Equal(t, ContentModerationSemanticReviewPrimaryModel, repo.calls[0].scope)
	require.Equal(t, upstreamCodexPlanGatedModelReason, repo.calls[0].reason)
}

func TestParseSemanticReviewRiskDimensions(t *testing.T) {
	result, err := parseSemanticReviewModelOutput(`{"verdict":"review","intent":"harmful","target":"third_party","authorization":"unauthorized","severity":"critical","confidence":0.97,"operationality":"actionable","executability":"direct","categories":["unauthorized_access"],"reason_codes":["no_authorization"]}`)
	require.NoError(t, err)
	require.Equal(t, "harmful", result.Intent)
	require.Equal(t, "third_party", result.Target)
	require.Equal(t, "unauthorized", result.Authorization)
	require.Equal(t, "actionable", result.Operationality)
	require.Equal(t, "direct", result.Executability)
}

func TestSemanticReviewPolicyRejectsExplicitUnauthorizedExecutableAbuse(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:        "review",
		Intent:         "harmful",
		Target:         "third_party",
		Authorization:  "unauthorized",
		HarmMechanism:  "unauthorized_access",
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"unauthorized_access"},
	})

	require.True(t, overridden)
	require.Equal(t, "reject", result.Verdict)
	require.Contains(t, result.ReasonCodes, "semantic_policy_reject")
}

func TestSemanticReviewPolicyCapsInconsistentRejects(t *testing.T) {
	tests := []struct {
		name     string
		result   ContentModerationSemanticReviewResult
		expected string
	}{
		{
			name:     "unclear authorization",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unclear", HarmMechanism: "unauthorized_access", Operationality: "actionable", Executability: "direct"},
			expected: "review",
		},
		{
			name:     "ambiguous intent",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "ambiguous", Target: "third_party", Authorization: "unauthorized", HarmMechanism: "unauthorized_access", Operationality: "actionable", Executability: "direct"},
			expected: "review",
		},
		{
			name:     "no harm mechanism",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized", HarmMechanism: "none", Operationality: "actionable", Executability: "direct"},
			expected: "review",
		},
		{
			name:     "generic other harm mechanism",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized", HarmMechanism: "other", Operationality: "actionable", Executability: "direct"},
			expected: "review",
		},
		{
			name:     "missing harm mechanism",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized", Operationality: "actionable", Executability: "direct"},
			expected: "review",
		},
		{
			name:     "self owned operations",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "self_owned", Authorization: "authorized", HarmMechanism: "none", Operationality: "actionable", Executability: "direct"},
			expected: "allow",
		},
		{
			name:     "reason says no request",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "harmful", Target: "third_party", Authorization: "unauthorized", HarmMechanism: "unauthorized_access", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"no_user_request_provided"}},
			expected: "review",
		},
		{
			name:     "reason and dimensions say no request",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "benign", Target: "none", Authorization: "not_applicable", HarmMechanism: "none", Operationality: "none", Executability: "none", ReasonCodes: []string{"no_user_request_provided"}},
			expected: "allow",
		},
		{
			name:     "defensive label conflicts with unauthorized harm",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "self_owned", Authorization: "unauthorized", HarmMechanism: "credential_theft", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"benign_context"}},
			expected: "review",
		},
		{
			name:     "authorized defensive testing remains allowed",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "authorized_lab", Authorization: "authorized", HarmMechanism: "none", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"authorized_testing"}},
			expected: "allow",
		},
		{
			name:     "authorized self owned target conflicts with credential theft",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "self_owned", Authorization: "authorized", HarmMechanism: "credential_theft", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"authorized_testing"}},
			expected: "review",
		},
		{
			name:     "authorized lab conflicts with exploit delivery",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "authorized_lab", Authorization: "authorized", HarmMechanism: "exploit_delivery", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"authorized_testing"}},
			expected: "review",
		},
		{
			name:     "authorized label does not excuse third party credential theft",
			result:   ContentModerationSemanticReviewResult{Verdict: "reject", Intent: "defensive", Target: "third_party", Authorization: "authorized", HarmMechanism: "credential_theft", Operationality: "actionable", Executability: "direct", ReasonCodes: []string{"authorized_testing"}},
			expected: "review",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, overridden := applySemanticReviewPolicy(tt.result)
			require.True(t, overridden)
			require.Equal(t, tt.expected, result.Verdict)
			require.Contains(t, result.ReasonCodes, "semantic_policy_reject_inconsistent")
		})
	}
}

func TestSemanticReviewPolicyCapsInconsistentAllows(t *testing.T) {
	tests := []struct {
		name   string
		result ContentModerationSemanticReviewResult
	}{
		{
			name: "unclear authorization with credential theft",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "harmful", Target: "third_party", Authorization: "unclear",
				HarmMechanism: "credential_theft", Severity: "critical",
				Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
			},
		},
		{
			name: "harmless reason conflicts with harmful dimensions",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "harmful", Target: "third_party", Authorization: "unauthorized",
				HarmMechanism: "exploit_delivery", Severity: "high",
				Operationality: "actionable", Executability: "direct", Categories: []string{"exploit_delivery"},
				ReasonCodes: []string{"benign_context"},
			},
		},
		{
			name: "generic harm does not support allow",
			result: ContentModerationSemanticReviewResult{
				Verdict: "allow", Intent: "benign", Target: "none", Authorization: "not_applicable",
				HarmMechanism: "other", Severity: "low", Operationality: "none", Executability: "none",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, overridden := applySemanticReviewPolicy(tt.result)
			require.True(t, overridden)
			require.Equal(t, "review", result.Verdict)
			require.Contains(t, result.ReasonCodes, "semantic_policy_allow_inconsistent")
		})
	}
}

func TestSemanticReviewPolicyAllowsAuthorizedCTFLabRequest(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:        "allow",
		Intent:         "defensive",
		Target:         "authorized_lab",
		Authorization:  "authorized",
		HarmMechanism:  "none",
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"cyber"},
	})

	require.False(t, overridden)
	require.Equal(t, "allow", result.Verdict)
}

func TestSemanticReviewPolicyAllowsBenignExternalServiceWithoutAuthorization(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:           "allow",
		Intent:            "benign",
		Target:            "external_service",
		Authorization:     "not_applicable",
		InformationAccess: "public",
		HarmMechanism:     "none",
		Confidence:        0.42,
		Operationality:    "actionable",
		Executability:     "direct",
		Categories:        []string{"software_development"},
	})

	require.False(t, overridden)
	require.Equal(t, "allow", result.Verdict)
}

func TestSemanticReviewPolicyPreservesOutcomeChangingAuthorizationReview(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:        "review",
		Intent:         "harmful",
		Target:         "third_party",
		Authorization:  "unclear",
		HarmMechanism:  "unauthorized_access",
		Confidence:     0.98,
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"unauthorized_access"},
	})

	require.False(t, overridden)
	require.Equal(t, "review", result.Verdict)
}

func TestSemanticReviewPolicyDowngradesPublicDataWithoutHarmMechanism(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:           "reject",
		Intent:            "harmful",
		Target:            "third_party",
		Authorization:     "unauthorized",
		InformationAccess: "public",
		HarmMechanism:     "none",
		Operationality:    "actionable",
		Executability:     "indirect",
		Categories:        []string{"fraud", "competitive_abuse"},
	})

	require.True(t, overridden)
	require.Equal(t, "review", result.Verdict)
	require.Contains(t, result.ReasonCodes, "semantic_policy_public_harmless")
}

func TestSemanticReviewPolicyDowngradesIndirectPublicMarketManipulation(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:           "reject",
		Intent:            "harmful",
		Target:            "external_service",
		Authorization:     "not_applicable",
		InformationAccess: "public",
		HarmMechanism:     "market_manipulation",
		Operationality:    "actionable",
		Executability:     "indirect",
		Categories:        []string{"fraud"},
	})

	require.True(t, overridden)
	require.Equal(t, "review", result.Verdict)
	require.Contains(t, result.ReasonCodes, "semantic_policy_reject_inconsistent")
}

func TestParseSemanticReviewPublicInformationDimensions(t *testing.T) {
	result, err := parseSemanticReviewModelOutput(`{"verdict":"allow","intent":"benign","target":"external_service","authorization":"not_applicable","information_access":"public","harm_mechanism":"none","severity":"low","confidence":0.96,"operationality":"actionable","executability":"indirect","categories":["financial_research"],"reason_codes":["public_market_data"]}`)

	require.NoError(t, err)
	require.Equal(t, "public", result.InformationAccess)
	require.Equal(t, "none", result.HarmMechanism)
	require.Equal(t, "allow", result.Verdict)
}

func TestEnqueueSemanticReviewEncryptsInputAndStripsAPIKeys(t *testing.T) {
	outbox := &contentModerationTestOutboxRepo{}
	svc := &ContentModerationService{
		outboxRepo:           outbox,
		rawRequestEncryptor:  contentModerationTestEncryptor{},
		semanticReviewRouter: semanticReviewRouterStub{},
	}
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
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
	require.NotContains(t, string(raw), "user_email")
	require.NotContains(t, string(raw), "api_key_name")
	require.Contains(t, string(raw), "text_encrypted")
}

func TestProcessSemanticReviewAllowPersistsAuditableCategory(t *testing.T) {
	repo := &contentModerationTestRepo{}
	encryptor := contentModerationTestEncryptor{}
	encrypted, err := encryptor.Encrypt("bounded review text")
	require.NoError(t, err)
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.rawRequestEncryptor = encryptor
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict:           "allow",
		Model:             "review-model",
		Confidence:        0.91,
		Intent:            "defensive",
		Target:            "self_owned",
		Authorization:     "authorized",
		InformationAccess: "provided_by_user",
		HarmMechanism:     "none",
		Categories:        []string{"benign_context"},
		ReasonCodes:       []string{"authorized_testing"},
	}}
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.SemanticReview = semanticReviewTestConfig()
	payload := contentModerationOutboxPayload{
		Config: cfg,
		SemanticReview: &contentModerationSemanticReviewOutboxPayload{
			DecisionID:    "decision-allow-audit",
			InputHash:     "hash-allow-audit",
			Input:         contentModerationSemanticReviewOutboxInput{RequestID: "request-allow-audit", UserID: 17},
			TextEncrypted: encrypted,
		},
	}

	require.NoError(t, svc.processContentModerationSemanticReviewEvent(context.Background(), payload))
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewAllow, logs[0].Action)
	require.False(t, logs[0].Flagged)
	require.Equal(t, "benign_context", logs[0].HighestCategory)
	require.Equal(t, 0.91, logs[0].CategoryScores[logs[0].HighestCategory])
	require.Equal(t, contentModerationDecisionSourceSemantic, logs[0].DecisionSource)
	require.Equal(t, "platform_openai", logs[0].ModerationProvider)
	require.Equal(t, "review-model", logs[0].ModerationModel)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(logs[0].Error), &metadata))
	require.Equal(t, "defensive", metadata["semantic_review_intent"])
	require.Equal(t, "authorized", metadata["semantic_review_authorization"])
	require.Equal(t, "provided_by_user", metadata["semantic_review_information_access"])
	require.Equal(t, "none", metadata["semantic_review_harm_mechanism"])
	require.Equal(t, false, metadata["semantic_review_policy_override"])
	require.Len(t, metadata["reviewed_text_sha256"], 64)
	require.NotContains(t, logs[0].Error, "bounded review text")
}

func TestProcessSemanticReviewContextOnlyRejectRemainsPendingWithoutViolation(t *testing.T) {
	repo := &contentModerationTestRepo{}
	encryptor := contentModerationTestEncryptor{}
	encrypted, err := encryptor.Encrypt("[source=responses.input[0] role=tool evidence=context_only]\nhistorical credential theft evidence")
	require.NoError(t, err)
	svc := NewContentModerationService(nil, repo, nil, nil, nil, nil, nil)
	svc.rawRequestEncryptor = encryptor
	svc.semanticReviewRouter = &contentModerationSemanticReviewRouterStub{result: ContentModerationSemanticReviewResult{
		Verdict: "reject", Intent: "harmful", Target: "external_service", Authorization: "unauthorized",
		HarmMechanism: "credential_theft", Severity: "critical", Confidence: 0.98,
		Operationality: "actionable", Executability: "direct", Categories: []string{"credential_theft"},
	}}
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.SemanticReview = semanticReviewTestConfig()
	payload := contentModerationOutboxPayload{
		Config: cfg,
		SemanticReview: &contentModerationSemanticReviewOutboxPayload{
			DecisionID:    "decision-context-only",
			InputHash:     "hash-context-only",
			Input:         contentModerationSemanticReviewOutboxInput{RequestID: "request-context-only", UserID: 17},
			TextEncrypted: encrypted,
			ContextOnly:   true,
		},
	}

	require.NoError(t, svc.processContentModerationSemanticReviewEvent(context.Background(), payload))
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, ContentModerationActionSemanticReviewReview, logs[0].Action)
	require.Equal(t, ContentModerationReviewStatusPending, logs[0].ReviewStatus)
	require.False(t, logs[0].UserViolationEligible)
	require.Zero(t, logs[0].ViolationCount)
	require.False(t, logs[0].AutoBanned)
	require.False(t, logs[0].EmailSent)
	require.Contains(t, logs[0].Error, `"evidence_context_only":true`)
	require.Contains(t, logs[0].Error, "semantic_policy_context_only")
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

func TestBuildSemanticReviewInputAllUsesLatestUserOnly(t *testing.T) {
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

	require.Contains(t, got, "latest user request")
	require.NotContains(t, got, "latest tool result")
	require.NotContains(t, got, "old user request")
	require.NotContains(t, got, "private system context")
	require.NotContains(t, got, "assistant response")
}

func TestBuildSemanticReviewInputAllSkipsAmbientContextWithoutUserRequest(t *testing.T) {
	cfg := semanticReviewTestConfig()
	cfg.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ContentModerationInput{
		Text: "browser state tool output assistant handoff",
		Sources: []ContentModerationInputSource{
			{Source: "messages[0]", Role: "assistant", Text: "assistant handoff"},
			{Source: "messages[1]", Role: "tool", Text: "browser state tool output"},
		},
	}

	require.Empty(t, buildContentModerationSemanticReviewInput(cfg, content, ""))
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

	cfg.Enabled = true
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

func TestEnqueueSemanticReviewToolOnlyEvidenceCarriesContextOnlyProvenance(t *testing.T) {
	outbox := &contentModerationTestOutboxRepo{}
	encryptor := contentModerationTestEncryptor{}
	svc := &ContentModerationService{outboxRepo: outbox, rawRequestEncryptor: encryptor, semanticReviewRouter: semanticReviewRouterStub{}}
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.SemanticReview = semanticReviewTestConfig()
	cfg.SemanticReview.Trigger = ContentModerationSemanticReviewTriggerAll
	content := ExtractContentModerationInput(
		ContentModerationProtocolOpenAIResponses,
		[]byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"deployment complete"}]}`),
	)

	require.True(t, svc.enqueueSemanticReviewAfterRules(
		context.Background(),
		ContentModerationCheckInput{RequestID: "tool-only"},
		cfg,
		content,
		"hash-tool-only",
		&ContentModerationDecision{Allowed: true},
	))
	events := outbox.snapshotEvents()
	require.Len(t, events, 1)
	raw, err := json.Marshal(events[0].Payload)
	require.NoError(t, err)
	var payload contentModerationOutboxPayload
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.NotNil(t, payload.SemanticReview)
	require.True(t, payload.SemanticReview.ContextOnly)
}

func TestSemanticReviewUpstreamErrorClassification(t *testing.T) {
	err := classifySemanticReviewUpstreamHTTPError(httpStatusTooManyRequestsForTest, []byte(`{"error":"insufficient_quota"}`))
	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.True(t, errors.As(err, &upstreamErr))
	require.True(t, upstreamErr.Retryable)
	require.True(t, upstreamErr.QuotaExhausted)
}

func TestSemanticReviewUpstreamErrorClassifiesCodexPlanGatedModel(t *testing.T) {
	err := classifySemanticReviewUpstreamHTTPError(http.StatusBadRequest, []byte(`{"detail":"The 'gpt-5.3-codex-spark' model is not supported when using Codex with a ChatGPT account."}`))

	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, "model_unsupported", upstreamErr.Code)
	require.False(t, upstreamErr.Retryable)
}

const httpStatusTooManyRequestsForTest = 429
