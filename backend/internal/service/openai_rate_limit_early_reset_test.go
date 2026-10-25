package service

import (
	"context"
	"testing"
	"time"
)

type openAIEarlyResetClearCall struct {
	accountID int64
	limitedAt *time.Time
	resetAt   time.Time
}

type openAIEarlyResetRepo struct {
	accountUsageCodexProbeRepo
	observedClearResult bool
	observedClearCalls  []openAIEarlyResetClearCall
}

func (r *openAIEarlyResetRepo) ClearOpenAIRateLimitIfObserved(_ context.Context, id int64, observedLimitedAt *time.Time, observedResetAt time.Time) (bool, error) {
	r.observedClearCalls = append(r.observedClearCalls, openAIEarlyResetClearCall{accountID: id, limitedAt: observedLimitedAt, resetAt: observedResetAt})
	return r.observedClearResult, nil
}

func newOpenAIEarlyResetAccount(id int64, now time.Time) *Account {
	limitedAt := now.Add(-time.Hour)
	resetAt := now.Add(72 * time.Hour)
	return &Account{
		ID:               id,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		RateLimitedAt:    &limitedAt,
		RateLimitResetAt: &resetAt,
	}
}

func TestRecoverOpenAIRateLimit_EarlyResetReplacesPendingWindowStart(t *testing.T) {
	t.Parallel()

	now := time.Now()
	account := newOpenAIEarlyResetAccount(123, now)
	limitedAt, resetAt := *account.RateLimitedAt, *account.RateLimitResetAt
	timer := time.AfterFunc(time.Hour, func() {})
	task := &openAIRateLimitWindowStartTask{limitedAt: limitedAt, resetAt: resetAt, timer: timer}
	repo := &openAIEarlyResetRepo{observedClearResult: true}
	blocker := &accountUsageRuntimeBlocker{}
	svc := &AccountUsageService{
		accountRepo:            repo,
		runtimeBlocker:         blocker,
		openAIWindowStartTasks: map[int64]*openAIRateLimitWindowStartTask{account.ID: task},
	}

	svc.recoverOpenAIGlobalRateLimitAfterProbe(context.Background(), account)

	if len(repo.observedClearCalls) != 1 {
		t.Fatalf("observed clear calls = %d, want 1", len(repo.observedClearCalls))
	}
	call := repo.observedClearCalls[0]
	if call.accountID != account.ID || call.limitedAt == nil || !call.limitedAt.Equal(limitedAt) || !call.resetAt.Equal(resetAt) {
		t.Fatalf("observed clear call = %+v, want account %d limited %v reset %v", call, account.ID, limitedAt, resetAt)
	}
	if len(repo.clearRateLimitIDs) != 0 {
		t.Fatalf("unconditional ClearRateLimit must not be used when the observed clear is available, got %v", repo.clearRateLimitIDs)
	}
	if svc.hasPendingOpenAIRateLimitWindowStart(account.ID) {
		t.Fatal("window-start timer for the advertised reset must be cancelled after an early reset")
	}
	if timer.Stop() {
		t.Fatal("window-start timer for the advertised reset was left running")
	}
	if account.RateLimitedAt != nil || account.RateLimitResetAt != nil {
		t.Fatalf("expected in-memory 429 state to be cleared, got limited=%v reset=%v", account.RateLimitedAt, account.RateLimitResetAt)
	}
	if len(blocker.clearedIDs) != 1 || blocker.clearedIDs[0] != account.ID {
		t.Fatalf("runtime blocker clears = %v, want [%d]", blocker.clearedIDs, account.ID)
	}
}

func TestRecoverOpenAIRateLimit_KeepsNewerCooldownGeneration(t *testing.T) {
	t.Parallel()

	now := time.Now()
	account := newOpenAIEarlyResetAccount(123, now)
	timer := time.AfterFunc(time.Hour, func() {})
	defer timer.Stop()
	task := &openAIRateLimitWindowStartTask{limitedAt: *account.RateLimitedAt, resetAt: *account.RateLimitResetAt, timer: timer}
	repo := &openAIEarlyResetRepo{observedClearResult: false}
	blocker := &accountUsageRuntimeBlocker{}
	cache := NewUsageCache()
	cache.openAIProbeCache.Store(int64(456), now.Add(time.Hour))
	svc := &AccountUsageService{
		accountRepo:            repo,
		runtimeBlocker:         blocker,
		cache:                  cache,
		openAIWindowStartTasks: map[int64]*openAIRateLimitWindowStartTask{account.ID: task},
	}

	svc.recoverOpenAIGlobalRateLimitAfterProbe(context.Background(), account)

	if account.RateLimitedAt == nil || account.RateLimitResetAt == nil {
		t.Fatal("a cooldown that changed after the probe must stay in place")
	}
	if !svc.hasPendingOpenAIRateLimitWindowStart(account.ID) {
		t.Fatal("window-start timer must stay armed when nothing was cleared")
	}
	if len(blocker.clearedIDs) != 0 {
		t.Fatalf("runtime block must not be cleared, got %v", blocker.clearedIDs)
	}
	if _, ok := cache.openAIProbeCache.Load(int64(456)); !ok {
		t.Fatal("no early reset was confirmed, so other probe throttles must be kept")
	}
}

func TestRecoverOpenAIRateLimit_IgnoresAccountsOutsideCooldown(t *testing.T) {
	t.Parallel()

	now := time.Now()
	expired := newOpenAIEarlyResetAccount(123, now)
	past := now.Add(-time.Minute)
	expired.RateLimitResetAt = &past
	shadow := newOpenAIEarlyResetAccount(124, now)
	parentID := int64(1)
	shadow.ParentAccountID = &parentID
	repo := &openAIEarlyResetRepo{observedClearResult: true}
	svc := &AccountUsageService{accountRepo: repo}

	svc.recoverOpenAIGlobalRateLimitAfterProbe(context.Background(), expired)
	svc.recoverOpenAIGlobalRateLimitAfterProbe(context.Background(), shadow)

	if len(repo.observedClearCalls) != 0 {
		t.Fatalf("expired cooldowns belong to the window-start timer and shadows to their parent, got clears %+v", repo.observedClearCalls)
	}
}

func TestBroadcastOpenAIEarlyReset_ClearsOtherProbeThrottlesOncePerCooldown(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cache := NewUsageCache()
	for _, id := range []int64{1, 2, 3} {
		cache.openAIProbeCache.Store(id, now.Add(10*time.Minute))
	}
	svc := &AccountUsageService{cache: cache}

	svc.broadcastOpenAIEarlyReset(1, now)

	if _, ok := cache.openAIProbeCache.Load(int64(1)); !ok {
		t.Fatal("the account that detected the reset keeps its freshly stored throttle")
	}
	for _, id := range []int64{2, 3} {
		if _, ok := cache.openAIProbeCache.Load(id); ok {
			t.Fatalf("probe throttle for account %d should be cleared by the broadcast", id)
		}
	}

	cache.openAIProbeCache.Store(int64(2), now.Add(10*time.Minute))
	svc.broadcastOpenAIEarlyReset(3, now.Add(time.Minute))
	if _, ok := cache.openAIProbeCache.Load(int64(2)); !ok {
		t.Fatal("a second detection inside the cooldown must not clear throttles again")
	}

	svc.broadcastOpenAIEarlyReset(3, now.Add(openAIEarlyResetBroadcastCooldown))
	if _, ok := cache.openAIProbeCache.Load(int64(2)); ok {
		t.Fatal("a detection after the cooldown should broadcast again")
	}
}
