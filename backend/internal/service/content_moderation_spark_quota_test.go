package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSemanticReviewRouterDoesNotUseGlobalQuotaForSpark(t *testing.T) {
	for _, refreshErr := range []error{nil, errors.New("quota probe unavailable")} {
		name := "missing_spark_window"
		if refreshErr != nil {
			name = "probe_failed"
		}
		t.Run(name, func(t *testing.T) {
			account := exhaustedSemanticReviewAccount(1)
			account.Extra["codex_usage_dimension"] = "global"
			backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
				ContentModerationSemanticReviewPrimaryModel:  {account},
				ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(2)},
			}}
			quota := &semanticReviewQuotaStub{err: refreshErr}
			router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

			result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})

			require.NoError(t, err)
			require.Equal(t, ContentModerationSemanticReviewPrimaryModel, result.Model)
			require.Equal(t, []int64{account.ID}, quota.snapshotCalls())
			require.Equal(t, "global", account.Extra["codex_usage_dimension"])
		})
	}
}

func TestSemanticReviewRouterCachesSparkQuotaSeparatelyFromGlobal(t *testing.T) {
	account := freshSemanticReviewAccount(1)
	account.Extra["codex_usage_dimension"] = "global"
	account.Extra["codex_5h_used_percent"] = 0.0
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel:  {account},
		ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(2)},
	}}
	quota := &semanticReviewQuotaStub{updates: map[int64]map[string]any{account.ID: {
		"codex_usage_dimension": "spark", "codex_usage_updated_at": time.Now().Format(time.RFC3339),
		"codex_5h_used_percent": 100.0, "codex_5h_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil)

	for range 2 {
		result, err := router.Review(context.Background(), semanticReviewTestConfig(), ContentModerationSemanticReviewInput{Text: "test"})
		require.NoError(t, err)
		require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	}
	require.Equal(t, []int64{account.ID}, quota.snapshotCalls(), "reuse the independent Spark snapshot")
	require.Equal(t, "global", account.Extra["codex_usage_dimension"])
	require.Equal(t, 0.0, account.Extra["codex_5h_used_percent"])
}

func TestSemanticReviewSparkQuotaCacheRefreshesAfterExpiry(t *testing.T) {
	account := freshSemanticReviewAccount(1)
	account.Extra["codex_usage_dimension"] = "global"
	backend := &semanticReviewBackendStub{accountsByModel: map[string][]*Account{
		ContentModerationSemanticReviewPrimaryModel:  {account},
		ContentModerationSemanticReviewFallbackModel: {freshSemanticReviewAccount(2)},
	}}
	quota := &semanticReviewQuotaStub{updates: map[int64]map[string]any{account.ID: {
		"codex_usage_dimension": "spark", "codex_usage_updated_at": time.Now().Format(time.RFC3339),
		"codex_5h_used_percent": 0.0,
	}}}
	router := NewOpenAIContentModerationSemanticReviewRouter(backend, quota, nil).(*openAIContentModerationSemanticReviewRouter)
	input := ContentModerationSemanticReviewInput{Text: "test"}
	_, err := router.Review(context.Background(), semanticReviewTestConfig(), input)
	require.NoError(t, err)
	quota.updates[account.ID] = map[string]any{
		"codex_usage_dimension": "spark", "codex_usage_updated_at": time.Now().Format(time.RFC3339),
		"codex_5h_used_percent": 100.0, "codex_5h_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}
	router.quotaSnapshots.Set("1", quota.updates[account.ID], time.Nanosecond)
	require.Eventually(t, func() bool {
		_, exists := router.quotaSnapshots.Get("1")
		return !exists
	}, time.Second, time.Millisecond)

	result, err := router.Review(context.Background(), semanticReviewTestConfig(), input)

	require.NoError(t, err)
	require.Equal(t, ContentModerationSemanticReviewFallbackModel, result.Model)
	require.Equal(t, []int64{1, 1}, quota.snapshotCalls())
}

func TestSemanticReviewSparkSnapshotDoesNotInheritMissingWindows(t *testing.T) {
	account := freshSemanticReviewAccount(1)
	account.Extra["codex_usage_dimension"] = "global"
	account.Extra["codex_7d_used_percent"] = 100.0
	account.Extra["codex_7d_reset_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	account.Extra["privacy_mode"] = PrivacyModeTrainingOff
	updates := map[string]any{
		"codex_usage_dimension": "spark", "codex_usage_updated_at": time.Now().Format(time.RFC3339),
		"codex_5h_used_percent": 5.0, "codex_5h_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339),
	}

	snapshot := semanticReviewAccountSnapshotWithExtra(account, updates)

	require.False(t, semanticReviewQuotaExhausted(snapshot, ContentModerationSemanticReviewPrimaryModel, time.Now()))
	require.NotContains(t, snapshot.Extra, "codex_7d_used_percent")
	require.Equal(t, PrivacyModeTrainingOff, snapshot.Extra["privacy_mode"])
	require.Equal(t, 100.0, account.Extra["codex_7d_used_percent"])
}

func TestSemanticReviewQuotaRefreshOnlyPersistsSparkShadow(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal_pro", true: "spark_shadow"}[shadow], func(t *testing.T) {
			parent := &Account{
				ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"chatgpt_account_id": "quota-test", "plan_type": "pro"},
			}
			account := parent
			if shadow {
				account = &Account{ID: 2, ParentAccountID: &parent.ID, Platform: PlatformOpenAI,
					Type: AccountTypeOAuth, Status: StatusActive, QuotaDimension: QuotaDimensionSpark}
			}
			repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{parent.ID: parent, account.ID: account}}
			tokenCache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(parent): "test-token"}}
			tokenProvider := NewOpenAITokenProvider(repo, tokenCache, nil)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(OpenAIQuotaUsage{AdditionalRateLimits: []OpenAIAdditionalRateLimit{{
					MeteredFeature: "codex_bengalfox", RateLimit: &OpenAIRateLimit{Allowed: true,
						PrimaryWindow: &OpenAIRateLimitWindow{UsedPercent: 25, LimitWindowSeconds: 18000, ResetAfterSeconds: 3600}},
				}}})
			}))
			defer server.Close()
			quota := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(server))
			refresher := NewOpenAIContentModerationSemanticReviewQuotaRefresher(quota, repo)

			updates, err := refresher.RefreshSemanticReviewQuota(context.Background(), account.ID)

			require.NoError(t, err)
			require.Equal(t, "spark", updates["codex_usage_dimension"])
			require.Equal(t, 25.0, updates["codex_5h_used_percent"])
			if shadow {
				require.Equal(t, 1, repo.extraUpdateCalls)
				require.Equal(t, updates, repo.extraUpdates[account.ID])
			} else {
				require.Zero(t, repo.extraUpdateCalls, "Spark probing must not overwrite the normal account's global quota")
			}
			require.Equal(t, int32(1), requests.Load(), "quota review does not need reset-credit details")
		})
	}
}
