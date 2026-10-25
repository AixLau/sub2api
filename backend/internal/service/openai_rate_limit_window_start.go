package service

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const (
	openAIWindowStartRequestTimeout = 30 * time.Second
	openAIWindowStartQuotaTimeout   = 20 * time.Second
	// A second, pricier model is tried only when the cheapest one was rejected
	// without re-arming a cooldown; rejected requests are not billed upstream.
	openAIWindowStartMaxAttempts = 2
)

// sendOpenAIRateLimitWindowStart sends the one request that opens the next
// sliding window after a 429 cooldown ended at cooldownEndedAt. Callers must
// already have cleared the cooldown they observed. Nothing is sent unless the
// account is still idle and usable, /wham/usage confirms the quota is
// available, and a model the account can serve right now is found; the
// cheapest such model is used.
func (s *AccountUsageService) sendOpenAIRateLimitWindowStart(accountID int64, resetAt, cooldownEndedAt time.Time) {
	if s == nil || s.openAIWindowStarter == nil || s.accountRepo == nil {
		return
	}
	ctx := context.WithValue(context.Background(), openAIWindowStartProbeContextKey{}, true)

	account, reason := s.loadOpenAIWindowStartAccount(ctx, accountID, cooldownEndedAt)
	if reason != "" {
		slog.Info("openai_rate_limit_window_start_skipped", "account_id", accountID, "reset_at", resetAt, "reason", reason)
		return
	}
	if reason := s.confirmOpenAIWindowStartQuota(ctx, account); reason != "" {
		slog.Info("openai_rate_limit_window_start_skipped", "account_id", accountID, "reset_at", resetAt, "reason", reason)
		return
	}
	models := s.openAIWindowStarter.OpenAIWindowStartModels(ctx, account)
	if len(models) == 0 {
		slog.Warn("openai_rate_limit_window_start_skipped", "account_id", accountID, "reset_at", resetAt, "reason", "no_usable_model")
		return
	}

	for attempt, model := range models[:min(len(models), openAIWindowStartMaxAttempts)] {
		if attempt > 0 {
			// The previous attempt may have re-armed a cooldown (429) or the
			// account may have picked up traffic; either makes another request
			// pointless.
			if _, reason := s.loadOpenAIWindowStartAccount(ctx, accountID, cooldownEndedAt); reason != "" {
				slog.Info("openai_rate_limit_window_start_skipped", "account_id", accountID, "reset_at", resetAt, "reason", reason)
				return
			}
		}
		slog.Info("openai_rate_limit_window_start_request",
			"account_id", accountID,
			"reset_at", resetAt,
			"model", model.Upstream,
			"cost_per_token", model.CostPerToken,
			"attempt", attempt+1,
		)
		probeCtx, probeCancel := context.WithTimeout(ctx, openAIWindowStartRequestTimeout)
		result, probeErr := s.openAIWindowStarter.RunTestBackground(probeCtx, accountID, model.Requested)
		probeCancel()
		if probeErr == nil && result != nil && result.Status == "success" {
			slog.Info("openai_rate_limit_window_started", "account_id", accountID, "reset_at", resetAt, "model", model.Upstream)
			return
		}
		var resultStatus, resultError string
		if result != nil {
			resultStatus = result.Status
			resultError = truncate(strings.TrimSpace(result.ErrorMessage), 240)
		}
		slog.Warn(
			"openai_rate_limit_window_start_failed",
			"account_id", accountID,
			"model", model.Upstream,
			"error", probeErr,
			"result_status", resultStatus,
			"result_error", resultError,
		)
		s.openAIWindowStarter.RecordOpenAIWindowStartModelFailure(accountID, model.Upstream)
	}
}

// loadOpenAIWindowStartAccount re-reads the account and returns a skip reason
// when a window-start request would be wasted: the account is gone, disabled
// or unschedulable, back in a cooldown, or already served traffic after the
// cooldown ended (which opened the window on its own).
func (s *AccountUsageService) loadOpenAIWindowStartAccount(ctx context.Context, accountID int64, cooldownEndedAt time.Time) (*Account, string) {
	loadCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	account, err := s.accountRepo.GetByID(loadCtx, accountID)
	cancel()
	switch {
	case err != nil || account == nil:
		return nil, "account_unavailable"
	case !account.IsOpenAIOAuth() || account.IsShadow():
		return nil, "not_openai_oauth_parent"
	case !account.IsActive() || !account.Schedulable:
		return nil, "account_not_schedulable"
	case account.IsRateLimited():
		return nil, "rate_limited"
	case account.LastUsedAt != nil && account.LastUsedAt.After(cooldownEndedAt):
		return nil, "window_already_active"
	}
	return account, ""
}

// confirmOpenAIWindowStartQuota reads /wham/usage (which does not consume
// quota) and refuses the window start unless upstream reports the quota as
// available. The fresh snapshot is persisted like any other probe.
func (s *AccountUsageService) confirmOpenAIWindowStartQuota(ctx context.Context, account *Account) string {
	if s.openAIQuotaService == nil {
		return ""
	}
	quotaCtx, cancel := context.WithTimeout(ctx, openAIWindowStartQuotaTimeout)
	usage, err := s.openAIQuotaService.queryUsage(quotaCtx, account.ID, false)
	cancel()
	if err != nil {
		slog.Warn("openai_rate_limit_window_start_quota_check_failed", "account_id", account.ID, "error", err)
		return "quota_check_failed"
	}
	if updates := buildCodexGlobalWindowExtraUpdates(usage, time.Now()); len(updates) != 0 {
		mergeAccountExtra(account, updates)
		s.persistOpenAICodexProbeSnapshot(account.ID, updates)
	}
	if !openAIQuotaShowsAvailable(usage) {
		return "quota_not_available"
	}
	return ""
}
