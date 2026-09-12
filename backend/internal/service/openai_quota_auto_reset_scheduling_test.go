package service

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func autoResetSchedulingAccount(now time.Time) *Account {
	return &Account{
		ID: 940001, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Extra: map[string]any{
			OpenAIAutoResetCreditEnabledExtraKey:     true,
			OpenAIAutoResetCredit5hThresholdExtraKey: 1.0,
			OpenAIAutoResetCredit7dThresholdExtraKey: 1.0,
			"auto_pause_5h_threshold":                0.8, "auto_pause_7d_threshold": 0.8,
			"codex_5h_used_percent": 90.0, "codex_7d_used_percent": 10.0,
			"codex_usage_updated_at": now.Format(time.RFC3339),
			"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
			"codex_7d_reset_at":      now.Add(24 * time.Hour).Format(time.RFC3339),
		},
	}
}

// Exercise both scheduler entry paths with one candidate. A pause must prevent
// slot acquisition; a usable credit only permits reaching the remaining gates.
func assertAutoResetSchedulingEligibility(t *testing.T, account *Account, model string, eligible bool) {
	t.Helper()
	for _, advanced := range []bool{false, true} {
		t.Run("advanced="+strconv.FormatBool(advanced), func(t *testing.T) {
			var acquired []int64
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*account}},
				cache:       &schedulerTestGatewayCache{}, cfg: &config.Config{},
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}),
			}
			result, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "", model, nil, OpenAIUpstreamTransportAny, false)
			if !eligible {
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, result)
				require.Empty(t, acquired)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			t.Cleanup(result.ReleaseFunc)
			require.Equal(t, account.ID, result.Account.ID)
			require.Equal(t, []int64{account.ID}, acquired)
		})
	}
}

func TestOpenAIAutoResetScheduling_CreditStates(t *testing.T) {
	now := time.Now().UTC()
	for _, window := range []string{"5h", "7d"} {
		t.Run(window, func(t *testing.T) {
			states := []struct {
				name     string
				state    any
				eligible bool
			}{
				{name: "unknown"},
				{name: "malformed", state: "unreadable"},
				{name: "available", state: OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}, eligible: true},
				{name: "empty_available", state: OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, CheckedAt: now.Format(time.RFC3339)}},
				{name: "stale_available", state: OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Add(-openAIAutoResetSnapshotTTL - time.Second).Format(time.RFC3339)}},
				{name: "undated_available", state: OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1}},
			}
			for _, status := range []string{OpenAIAutoResetStatusChecking, OpenAIAutoResetStatusResetting, OpenAIAutoResetStatusSuccess, OpenAIAutoResetStatusNoCredit, OpenAIAutoResetStatusFailed} {
				// Checking/resetting/failed may retain a previously observed count.
				states = append(states, struct {
					name     string
					state    any
					eligible bool
				}{name: status, state: OpenAIAutoResetCreditState{Status: status, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}})
			}
			for _, tt := range states {
				t.Run(tt.name, func(t *testing.T) {
					// Queue only: no worker or upstream quota client is started.
					notifier := NewOpenAIQuotaAutoResetService(nil, nil, nil, nil, nil, nil, nil)
					setOpenAIAutoResetNotifier(notifier)
					t.Cleanup(notifier.Stop)
					account := autoResetSchedulingAccount(now)
					account.Extra["codex_5h_used_percent"] = 10.0
					account.Extra["codex_"+window+"_used_percent"] = 90.0
					account.Extra[OpenAIAutoResetCreditStateExtraKey] = tt.state
					paused, decision := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
					require.Equal(t, !tt.eligible, paused)
					if paused {
						require.Len(t, notifier.queue, 1)
						require.Equal(t, account.ID, <-notifier.queue)
						require.Equal(t, "quota_auto_reset_credit_check_"+window, decision.reason)
						require.Equal(t, window, decision.window)
						require.Equal(t, 0.8, decision.threshold)
						require.Equal(t, 0.9, decision.utilization)
					} else {
						require.Empty(t, notifier.queue)
						require.Equal(t, openAIQuotaAutoPauseDecision{}, decision)
					}
					assertAutoResetSchedulingEligibility(t, account, "gpt-5.1", tt.eligible)
				})
			}
		})
	}
}

func TestOpenAIAutoResetScheduling_IndependentResetThresholds(t *testing.T) {
	now := time.Now().UTC()
	for _, window := range []string{"5h", "7d"} {
		for _, pauseDisabled := range []bool{false, true} {
			t.Run(window+"/pause_disabled="+strconv.FormatBool(pauseDisabled), func(t *testing.T) {
				account := autoResetSchedulingAccount(now)
				account.Extra["codex_5h_used_percent"] = 90.0 // must not hide a 7d reset threshold
				account.Extra["codex_"+window+"_used_percent"] = 95.0
				account.Extra["auto_reset_credit_"+window+"_threshold"] = 0.95
				account.Extra["auto_pause_"+window+"_disabled"] = pauseDisabled
				account.Extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}
				paused, decision := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
				require.True(t, paused)
				require.Equal(t, "quota_auto_reset_pending_"+window, decision.reason)
				require.Equal(t, 0.95, decision.threshold)
				require.Equal(t, 0.95, decision.utilization)
				assertAutoResetSchedulingEligibility(t, account, "gpt-5.1", false)
			})
		}
	}
}

func TestOpenAIAutoResetScheduling_DisabledKeepsQuotaProtection(t *testing.T) {
	now := time.Now().UTC()
	for _, used := range []float64{75, 90, 100} {
		for _, pauseDisabled := range []bool{false, true} {
			t.Run(strconv.FormatFloat(used, 'f', 0, 64)+"/pause_disabled="+strconv.FormatBool(pauseDisabled), func(t *testing.T) {
				account := autoResetSchedulingAccount(now)
				account.Extra[OpenAIAutoResetCreditEnabledExtraKey] = false
				account.Extra["codex_5h_used_percent"] = used
				account.Extra["auto_pause_5h_disabled"] = pauseDisabled
				account.Extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}
				wantPaused := used >= 80 && !pauseDisabled
				paused, decision := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
				require.Equal(t, wantPaused, paused)
				require.Empty(t, decision.reason)
				if paused {
					require.Equal(t, "5h", decision.window)
					require.Equal(t, 0.8, decision.threshold)
				}
				assertAutoResetSchedulingEligibility(t, account, "gpt-5.1", !wantPaused)
			})
		}
	}
}

func TestOpenAIAutoResetScheduling_AvailableCreditCannotOverrideAccountGates(t *testing.T) {
	now := time.Now().UTC()
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	for _, tt := range []struct {
		name   string
		change func(*Account)
		model  string
	}{
		{name: "manual_pause", change: func(a *Account) { a.Schedulable = false }},
		{name: "account_error", change: func(a *Account) { a.Status = StatusError }},
		{name: "expired", change: func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }},
		{name: "rate_limited", change: func(a *Account) { a.RateLimitResetAt = &future }},
		{name: "overloaded", change: func(a *Account) { a.OverloadUntil = &future }},
		{name: "temporary_block", change: func(a *Account) { a.TempUnschedulableUntil = &future }},
		{name: "unsupported_model", change: func(*Account) {}, model: "grok-4"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := autoResetSchedulingAccount(now)
			account.Extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}
			tt.change(account)
			paused, _ := shouldAutoPauseOpenAIAccountByQuota(context.Background(), account)
			require.False(t, paused, "the independent gate, not quota, must reject this account")
			model := tt.model
			if model == "" {
				model = "gpt-5.1"
			}
			assertAutoResetSchedulingEligibility(t, account, model, false)
		})
	}
}

func TestOpenAIAutoResetScheduling_StateRefreshAndConcurrentNotifications(t *testing.T) {
	now := time.Now().UTC()
	ctx := context.Background()
	account := autoResetSchedulingAccount(now)
	account.Extra[OpenAIAutoResetCreditStateExtraKey] = OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1, CheckedAt: now.Format(time.RFC3339)}
	repo := &autoResetTestAccountRepo{account: account}
	stale, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	// Register only the existing deduplicated queue. No worker is started and no
	// quota implementation is supplied, so this test cannot consume any credit.
	notifier := NewOpenAIQuotaAutoResetService(repo, nil, nil, nil, nil, nil, nil)
	setOpenAIAutoResetNotifier(notifier)
	t.Cleanup(notifier.Stop)
	svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{}}
	for _, state := range []OpenAIAutoResetCreditState{
		{Status: OpenAIAutoResetStatusChecking, AvailableCount: 1},
		{Status: OpenAIAutoResetStatusResetting, AvailableCount: 1},
		{Status: OpenAIAutoResetStatusFailed, AvailableCount: 1},
		{Status: OpenAIAutoResetStatusSuccess, AvailableCount: 1},
		{Status: OpenAIAutoResetStatusAvailable, AvailableCount: 1},
	} {
		state.CheckedAt = now.Format(time.RFC3339)
		require.NoError(t, notifier.persistState(ctx, account.ID, &state))
		wantEligible := state.Status == OpenAIAutoResetStatusAvailable
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				fresh, err := svc.RefreshSelectedAccountBeforeUse(ctx, stale, "gpt-5.1", false, "", "")
				if wantEligible {
					if err != nil || fresh == nil {
						t.Errorf("fresh available state rejected: %v", err)
					}
				} else if err == nil || fresh != nil {
					t.Error("stale available snapshot bypassed current state")
				}
			}()
		}
		wg.Wait()
		fresh := svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, stale, nil, PlatformOpenAI, "gpt-5.1", false, "")
		require.Equal(t, wantEligible, fresh != nil)
	}
	require.Equal(t, OpenAIAutoResetStatusAvailable, openAIAutoResetStateFromExtra(stale.Extra).Status, "state persistence must not mutate cached snapshots")
	require.Len(t, notifier.queue, 1, "concurrent pauses must deduplicate the account notification")
	require.Equal(t, account.ID, <-notifier.queue)
	// A successful reset only releases the quota gate after the usage snapshot
	// reflects recovery (or the existing natural-window reset rule applies).
	require.NoError(t, notifier.persistState(ctx, account.ID, &OpenAIAutoResetCreditState{Status: OpenAIAutoResetStatusSuccess, CheckedAt: now.Format(time.RFC3339)}))
	require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{"codex_5h_used_percent": 0.0}))
	fresh, err := svc.RefreshSelectedAccountBeforeUse(ctx, stale, "gpt-5.1", false, "", "")
	require.NoError(t, err)
	require.NotNil(t, fresh)
}
