package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// OpenAI occasionally resets Codex usage windows ahead of the advertised reset
// time (for example during promotions), usually for every account at once.
// Accounts in a 429 cooldown are excluded from scheduling, so no gateway
// traffic brings back fresh rate-limit headers; only a /wham/usage probe can
// notice the reset.
const openAIEarlyResetBroadcastCooldown = 10 * time.Minute

type openAIRateLimitObservedClearer interface {
	ClearOpenAIRateLimitIfObserved(ctx context.Context, id int64, observedLimitedAt *time.Time, observedResetAt time.Time) (bool, error)
}

// recoverOpenAIGlobalRateLimitAfterProbe clears a 429 cooldown once /wham/usage
// reports the account available again. Reaching this point while
// RateLimitResetAt is still in the future means upstream reset the window
// early, so the window-start timer armed for the advertised reset is replaced
// by an immediate one and the other cooldown accounts are re-probed promptly.
func (s *AccountUsageService) recoverOpenAIGlobalRateLimitAfterProbe(ctx context.Context, account *Account) {
	if s == nil || s.accountRepo == nil || account == nil || account.IsShadow() || !account.IsRateLimited() {
		return
	}
	advertisedResetAt := *account.RateLimitResetAt

	cleared, err := s.clearObservedOpenAIRateLimit(ctx, account, advertisedResetAt)
	if err != nil {
		slog.Warn("openai_usage_probe_rate_limit_clear_failed", "account_id", account.ID, "error", err)
		return
	}
	if !cleared {
		slog.Info("openai_usage_probe_rate_limit_clear_skipped", "account_id", account.ID, "reason", "rate_limit_state_changed")
		return
	}
	account.RateLimitedAt = nil
	account.RateLimitResetAt = nil
	account.OverloadUntil = nil
	if s.runtimeBlocker != nil {
		s.runtimeBlocker.ClearAccountSchedulingBlock(account.ID)
	}
	slog.Info("openai_rate_limit_early_reset_recovered", "account_id", account.ID, "advertised_reset_at", advertisedResetAt)

	clearedAt := time.Now()
	s.replaceOpenAIRateLimitWindowStartAfterEarlyReset(account.ID, advertisedResetAt, clearedAt)
	s.broadcastOpenAIEarlyReset(account.ID, clearedAt)
}

// clearObservedOpenAIRateLimit only clears the generation the probe observed, so
// a 429 that lands between the probe and the write keeps its new cooldown.
func (s *AccountUsageService) clearObservedOpenAIRateLimit(ctx context.Context, account *Account, resetAt time.Time) (bool, error) {
	if clearer, ok := s.accountRepo.(openAIRateLimitObservedClearer); ok {
		return clearer.ClearOpenAIRateLimitIfObserved(ctx, account.ID, account.RateLimitedAt, resetAt)
	}
	if err := s.accountRepo.ClearRateLimit(ctx, account.ID); err != nil {
		return false, err
	}
	return true, nil
}

// replaceOpenAIRateLimitWindowStartAfterEarlyReset cancels the timer armed for
// the advertised reset (its claim could no longer match the cleared row) and
// sends the window-start request shortly instead, keeping the same random
// delay as the regular path.
func (s *AccountUsageService) replaceOpenAIRateLimitWindowStartAfterEarlyReset(accountID int64, advertisedResetAt, clearedAt time.Time) {
	s.openAIWindowStartMu.Lock()
	if existing := s.openAIWindowStartTasks[accountID]; existing != nil {
		if existing.timer != nil {
			existing.timer.Stop()
		}
		delete(s.openAIWindowStartTasks, accountID)
	}
	s.openAIWindowStartMu.Unlock()

	if s.openAIWindowStarter == nil {
		return
	}
	time.AfterFunc(randomOpenAIWindowStartDelay(), func() {
		s.sendOpenAIRateLimitWindowStart(accountID, advertisedResetAt, clearedAt)
	})
}

// broadcastOpenAIEarlyReset drops the per-account probe throttle so the
// recovery loop re-probes every other cooldown account on its next pass instead
// of waiting out the 7–13 minute interval. Promotions reset all accounts
// together; the cooldown keeps a burst of detections from re-clearing it.
func (s *AccountUsageService) broadcastOpenAIEarlyReset(sourceAccountID int64, now time.Time) {
	if s == nil || s.cache == nil {
		return
	}
	last := s.openAIEarlyResetBroadcastAt.Load()
	if last != 0 && now.Sub(time.Unix(0, last)) < openAIEarlyResetBroadcastCooldown {
		return
	}
	if !s.openAIEarlyResetBroadcastAt.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	s.cache.openAIProbeCache.Range(func(key, _ any) bool {
		if id, ok := key.(int64); ok && id != sourceAccountID {
			s.cache.openAIProbeCache.Delete(key)
		}
		return true
	})
	slog.Info("openai_rate_limit_early_reset_broadcast", "source_account_id", sourceAccountID)
}

var openAIRateLimitRecovererRegistry struct {
	sync.RWMutex
	service *AccountUsageService
}

func setOpenAIRateLimitRecoverer(service *AccountUsageService) {
	openAIRateLimitRecovererRegistry.Lock()
	openAIRateLimitRecovererRegistry.service = service
	openAIRateLimitRecovererRegistry.Unlock()
}

// RecoverOpenAIRateLimitAfterQuotaQuery lets an explicit admin quota query
// recover a cooldown account the same way a background /wham/usage probe does.
func RecoverOpenAIRateLimitAfterQuotaQuery(ctx context.Context, accountID int64, usage *OpenAIQuotaUsage) {
	if accountID <= 0 || !openAIQuotaShowsAvailable(usage) {
		return
	}
	openAIRateLimitRecovererRegistry.RLock()
	service := openAIRateLimitRecovererRegistry.service
	openAIRateLimitRecovererRegistry.RUnlock()
	if service == nil || service.accountRepo == nil {
		return
	}
	account, err := service.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return
	}
	service.recoverOpenAIGlobalRateLimitAfterProbe(ctx, account)
}
