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

func TestParseSemanticReviewSSECapturesFirstOutputTokenLatency(t *testing.T) {
	started := time.Now().Add(-50 * time.Millisecond)
	response, err := parseSemanticReviewSSE(strings.NewReader(`data: {"type":"response.output_text.delta","delta":"{\"verdict\":\"allow\"}"}

data: [DONE]
`), started)

	require.NoError(t, err)
	require.NotNil(t, response.FirstTokenMS)
	require.GreaterOrEqual(t, *response.FirstTokenMS, 40)
}

func TestParseSemanticReviewSSEReturnsAtDoneWithoutWaitingForEOF(t *testing.T) {
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
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"unauthorized_access"},
	})

	require.True(t, overridden)
	require.Equal(t, "reject", result.Verdict)
	require.Contains(t, result.ReasonCodes, "semantic_policy_reject")
}

func TestSemanticReviewPolicyAllowsAuthorizedCTFLabRequest(t *testing.T) {
	result, overridden := applySemanticReviewPolicy(ContentModerationSemanticReviewResult{
		Verdict:        "allow",
		Intent:         "defensive",
		Target:         "authorized_lab",
		Authorization:  "authorized",
		Operationality: "actionable",
		Executability:  "direct",
		Categories:     []string{"cyber"},
	})

	require.False(t, overridden)
	require.Equal(t, "allow", result.Verdict)
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

func TestSemanticReviewPolicyKeepsPublicMarketManipulationRejected(t *testing.T) {
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

	require.False(t, overridden)
	require.Equal(t, "reject", result.Verdict)
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
		Verdict:    "allow",
		Model:      "review-model",
		Confidence: 0.91,
		Categories: []string{"benign_context"},
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

func TestSemanticReviewUpstreamErrorClassification(t *testing.T) {
	err := classifySemanticReviewUpstreamHTTPError(httpStatusTooManyRequestsForTest, []byte(`{"error":"insufficient_quota"}`))
	var upstreamErr *ContentModerationSemanticReviewUpstreamError
	require.True(t, errors.As(err, &upstreamErr))
	require.True(t, upstreamErr.Retryable)
	require.True(t, upstreamErr.QuotaExhausted)
}

const httpStatusTooManyRequestsForTest = 429
