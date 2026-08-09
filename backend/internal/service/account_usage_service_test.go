package service

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type accountUsageCodexProbeRepo struct {
	stubOpenAIAccountRepo
	updateExtraCh           chan map[string]any
	rateLimitCh             chan time.Time
	clearRateLimitIDs       []int64
	recoveryCandidateIDs    []int64
	recoveryCandidateAfter  []int64
	windowStartClaimed      bool
	windowStartClaimIDs     []int64
	windowStartClaimStarted chan struct{}
	windowStartClaimRelease <-chan struct{}
}

func (r *accountUsageCodexProbeRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.updateExtraCh != nil {
		copied := make(map[string]any, len(updates))
		for k, v := range updates {
			copied[k] = v
		}
		r.updateExtraCh <- copied
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	if r.rateLimitCh != nil {
		r.rateLimitCh <- resetAt
	}
	return nil
}

func (r *accountUsageCodexProbeRepo) ClearRateLimit(_ context.Context, accountID int64) error {
	r.clearRateLimitIDs = append(r.clearRateLimitIDs, accountID)
	return nil
}

func (r *accountUsageCodexProbeRepo) ListOpenAIRateLimitRecoveryCandidateIDs(_ context.Context, afterID int64, limit int, _ time.Time) ([]int64, error) {
	r.recoveryCandidateAfter = append(r.recoveryCandidateAfter, afterID)
	out := make([]int64, 0, limit)
	for _, id := range r.recoveryCandidateIDs {
		if id <= afterID {
			continue
		}
		out = append(out, id)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *accountUsageCodexProbeRepo) ClaimOpenAIExpiredRateLimit(_ context.Context, accountID int64, _, _ time.Time) (bool, error) {
	r.windowStartClaimIDs = append(r.windowStartClaimIDs, accountID)
	if r.windowStartClaimStarted != nil {
		select {
		case r.windowStartClaimStarted <- struct{}{}:
		default:
		}
	}
	if r.windowStartClaimRelease != nil {
		<-r.windowStartClaimRelease
	}
	return r.windowStartClaimed, nil
}

type accountUsageWindowStarter struct {
	accountIDs       []int64
	windowStartProbe []bool
	result           *ScheduledTestResult
}

func (s *accountUsageWindowStarter) RunTestBackground(ctx context.Context, accountID int64, _ string) (*ScheduledTestResult, error) {
	s.accountIDs = append(s.accountIDs, accountID)
	s.windowStartProbe = append(s.windowStartProbe, isOpenAIWindowStartProbe(ctx))
	return s.result, nil
}

type accountUsageRuntimeBlocker struct {
	clearedIDs []int64
}

func (b *accountUsageRuntimeBlocker) BlockAccountScheduling(_ *Account, _ time.Time, _ string) {}

func (b *accountUsageRuntimeBlocker) ClearAccountSchedulingBlock(accountID int64) {
	b.clearedIDs = append(b.clearedIDs, accountID)
}

func TestOpenAIQuotaShowsAvailableRequiresExplicitAllowedState(t *testing.T) {
	t.Parallel()

	if !openAIQuotaShowsAvailable(&OpenAIQuotaUsage{
		RateLimit: &OpenAIRateLimit{Allowed: true},
	}) {
		t.Fatal("expected explicit allowed quota state to prove account availability")
	}
	if openAIQuotaShowsAvailable(&OpenAIQuotaUsage{
		RateLimit: &OpenAIRateLimit{Allowed: true, LimitReached: true},
	}) {
		t.Fatal("limit_reached quota state must not recover the account")
	}
}

func TestAccountUsageService_SuccessfulProbeClearsGlobal429State(t *testing.T) {
	t.Parallel()

	now := time.Now()
	resetAt := now.Add(time.Hour)
	account := &Account{
		ID:               456,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		RateLimitedAt:    &now,
		RateLimitResetAt: &resetAt,
	}
	repo := &accountUsageCodexProbeRepo{}
	blocker := &accountUsageRuntimeBlocker{}
	svc := &AccountUsageService{
		accountRepo:    repo,
		runtimeBlocker: blocker,
	}

	svc.recoverOpenAIGlobalRateLimitAfterProbe(context.Background(), account)

	if len(repo.clearRateLimitIDs) != 1 || repo.clearRateLimitIDs[0] != account.ID {
		t.Fatalf("ClearRateLimit calls = %v, want [%d]", repo.clearRateLimitIDs, account.ID)
	}
	if account.RateLimitedAt != nil || account.RateLimitResetAt != nil {
		t.Fatalf("expected in-memory 429 state to be cleared, got limited=%v reset=%v", account.RateLimitedAt, account.RateLimitResetAt)
	}
	if len(blocker.clearedIDs) != 1 || blocker.clearedIDs[0] != account.ID {
		t.Fatalf("runtime blocker clears = %v, want [%d]", blocker.clearedIDs, account.ID)
	}
}

func TestAccountUsageService_RateLimitRecoveryCyclePagesAndWraps(t *testing.T) {
	now := time.Now()
	resetAt := now.Add(time.Hour)
	repo := &accountUsageCodexProbeRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, RateLimitResetAt: &resetAt},
			{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeOAuth, RateLimitResetAt: &resetAt},
		}},
		recoveryCandidateIDs: []int64{10, 20},
	}
	cache := NewUsageCache()
	cache.openAIProbeCache.Store(int64(10), now.Add(openAIProbeMaxInterval))
	cache.openAIProbeCache.Store(int64(20), now.Add(openAIProbeMaxInterval))
	svc := &AccountUsageService{accountRepo: repo, cache: cache}

	svc.runOpenAIRateLimitRecoveryCycle()
	if svc.openAIRecoveryCursor != 20 {
		t.Fatalf("recovery cursor = %d, want 20", svc.openAIRecoveryCursor)
	}
	svc.runOpenAIRateLimitRecoveryCycle()
	if svc.openAIRecoveryCursor != 0 {
		t.Fatalf("wrapped recovery cursor = %d, want 0", svc.openAIRecoveryCursor)
	}
	if len(repo.recoveryCandidateAfter) != 2 || repo.recoveryCandidateAfter[0] != 0 || repo.recoveryCandidateAfter[1] != 20 {
		t.Fatalf("candidate cursors = %v, want [0 20]", repo.recoveryCandidateAfter)
	}
}

func TestRandomOpenAIProbeIntervalWithinRange(t *testing.T) {
	t.Parallel()

	for range 100 {
		interval := randomOpenAIProbeInterval()
		if interval < openAIProbeMinInterval || interval > openAIProbeMaxInterval {
			t.Fatalf(
				"random probe interval = %s, want within [%s, %s]",
				interval,
				openAIProbeMinInterval,
				openAIProbeMaxInterval,
			)
		}
	}
}

func TestRandomOpenAIWindowStartDelayWithinRange(t *testing.T) {
	t.Parallel()

	for range 100 {
		delay := randomOpenAIWindowStartDelay()
		if delay < openAIWindowStartMinDelay || delay > openAIWindowStartMaxDelay {
			t.Fatalf(
				"random window start delay = %s, want within [%s, %s]",
				delay,
				openAIWindowStartMinDelay,
				openAIWindowStartMaxDelay,
			)
		}
	}
}

func TestOpenAIWindowStartDueAtPreservesRandomDelayForExpiredRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	delay := 37 * time.Second

	futureReset := now.Add(5 * time.Minute)
	if got, want := openAIWindowStartDueAt(futureReset, now, delay), futureReset.Add(delay); !got.Equal(want) {
		t.Fatalf("future reset due at = %v, want %v", got, want)
	}

	pastReset := now.Add(-2 * time.Hour)
	if got, want := openAIWindowStartDueAt(pastReset, now, delay), now.Add(delay); !got.Equal(want) {
		t.Fatalf("expired reset due at = %v, want %v", got, want)
	}
}

func TestCreateOpenAITestPayloadHasProbeInputAndInstructions(t *testing.T) {
	t.Parallel()

	payload := createOpenAITestPayload("gpt-5.4", true)
	input, ok := payload["input"].([]map[string]any)
	if !ok || len(input) != 1 {
		t.Fatalf("probe input = %#v, want one input message", payload["input"])
	}
	content, ok := input[0]["content"].([]map[string]any)
	if !ok || len(content) != 1 || content[0]["text"] != "hi" {
		t.Fatalf("probe content = %#v, want one hi input", input[0]["content"])
	}
	instructions, ok := payload["instructions"].(string)
	if !ok || instructions == "" {
		t.Fatal("probe request must include non-empty Codex instructions")
	}
}

func TestAccountUsageService_PendingWindowStartIsVisible(t *testing.T) {
	t.Parallel()

	task := &openAIRateLimitWindowStartTask{}
	svc := &AccountUsageService{
		openAIWindowStartTasks: map[int64]*openAIRateLimitWindowStartTask{123: task},
	}
	if !svc.hasPendingOpenAIRateLimitWindowStart(123) {
		t.Fatal("expected pending window-start task")
	}
	if svc.hasPendingOpenAIRateLimitWindowStart(456) {
		t.Fatal("unexpected pending window-start task")
	}
}

func TestAccountUsageService_WindowStartRequiresSuccessfulClaim(t *testing.T) {
	t.Parallel()

	now := time.Now()
	resetAt := now.Add(-time.Minute)
	tests := []struct {
		name        string
		claimed     bool
		wantStarted bool
	}{
		{name: "claimed", claimed: true, wantStarted: true},
		{name: "stale generation", claimed: false, wantStarted: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountUsageCodexProbeRepo{windowStartClaimed: tt.claimed}
			starter := &accountUsageWindowStarter{result: &ScheduledTestResult{Status: "success"}}
			task := &openAIRateLimitWindowStartTask{limitedAt: now, resetAt: resetAt}
			svc := &AccountUsageService{
				accountRepo:            repo,
				openAIWindowStarter:    starter,
				openAIWindowStartTasks: map[int64]*openAIRateLimitWindowStartTask{123: task},
			}

			svc.runOpenAIRateLimitWindowStart(123, task)

			if len(repo.windowStartClaimIDs) != 1 || repo.windowStartClaimIDs[0] != 123 {
				t.Fatalf("claim calls = %v, want [123]", repo.windowStartClaimIDs)
			}
			started := len(starter.accountIDs) > 0
			if started != tt.wantStarted {
				t.Fatalf("window start request sent = %v, want %v", started, tt.wantStarted)
			}
			if started && (len(starter.windowStartProbe) != 1 || !starter.windowStartProbe[0]) {
				t.Fatalf("window start request context marker = %v, want [true]", starter.windowStartProbe)
			}
		})
	}
}

func TestAccountUsageService_WindowStartRemainsPendingDuringClaim(t *testing.T) {
	t.Parallel()

	now := time.Now()
	task := &openAIRateLimitWindowStartTask{limitedAt: now.Add(-2 * time.Minute), resetAt: now.Add(-time.Minute)}
	claimStarted := make(chan struct{}, 1)
	claimRelease := make(chan struct{})
	repo := &accountUsageCodexProbeRepo{
		windowStartClaimed:      false,
		windowStartClaimStarted: claimStarted,
		windowStartClaimRelease: claimRelease,
	}
	starter := &accountUsageWindowStarter{result: &ScheduledTestResult{Status: "success"}}
	svc := &AccountUsageService{
		accountRepo:            repo,
		openAIWindowStarter:    starter,
		openAIWindowStartTasks: map[int64]*openAIRateLimitWindowStartTask{123: task},
	}

	done := make(chan struct{})
	go func() {
		svc.runOpenAIRateLimitWindowStart(123, task)
		close(done)
	}()
	select {
	case <-claimStarted:
	case <-time.After(time.Second):
		t.Fatal("window-start claim did not begin")
	}
	if !svc.hasPendingOpenAIRateLimitWindowStart(123) {
		t.Fatal("window-start task must remain pending while claim is in flight")
	}
	close(claimRelease)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("window-start claim did not finish")
	}
	if svc.hasPendingOpenAIRateLimitWindowStart(123) {
		t.Fatal("window-start task should be removed after claim result")
	}
}

func TestShouldProbeOpenAICodexSnapshotUsesRandomNextProbeTime(t *testing.T) {
	t.Parallel()

	const accountID = int64(987)
	now := time.Now()
	cache := NewUsageCache()
	svc := &AccountUsageService{cache: cache}

	if !svc.shouldProbeOpenAICodexSnapshot(accountID, now) {
		t.Fatal("expected first probe to be allowed")
	}
	cached, ok := cache.openAIProbeCache.Load(accountID)
	if !ok {
		t.Fatal("expected next probe time to be cached")
	}
	nextAt, ok := cached.(time.Time)
	if !ok {
		t.Fatalf("cached next probe value has type %T, want time.Time", cached)
	}
	interval := nextAt.Sub(now)
	if interval < openAIProbeMinInterval || interval > openAIProbeMaxInterval {
		t.Fatalf(
			"cached probe interval = %s, want within [%s, %s]",
			interval,
			openAIProbeMinInterval,
			openAIProbeMaxInterval,
		)
	}
	if svc.shouldProbeOpenAICodexSnapshot(accountID, nextAt.Add(-time.Nanosecond)) {
		t.Fatal("expected probe before next scheduled time to be blocked")
	}
	if !svc.shouldProbeOpenAICodexSnapshot(accountID, nextAt) {
		t.Fatal("expected probe at next scheduled time to be allowed")
	}
}

func TestShouldProbeOpenAICodexSnapshotForceReschedules(t *testing.T) {
	t.Parallel()

	const accountID = int64(988)
	now := time.Now()
	cache := NewUsageCache()
	cache.openAIProbeCache.Store(accountID, now.Add(time.Hour))
	svc := &AccountUsageService{cache: cache}

	if !svc.shouldProbeOpenAICodexSnapshot(accountID, now, true) {
		t.Fatal("expected forced probe to bypass existing schedule")
	}
	cached, ok := cache.openAIProbeCache.Load(accountID)
	if !ok {
		t.Fatal("expected forced probe to cache a new schedule")
	}
	nextAt, ok := cached.(time.Time)
	if !ok {
		t.Fatalf("cached next probe value has type %T, want time.Time", cached)
	}
	interval := nextAt.Sub(now)
	if interval < openAIProbeMinInterval || interval > openAIProbeMaxInterval {
		t.Fatalf(
			"forced probe interval = %s, want within [%s, %s]",
			interval,
			openAIProbeMinInterval,
			openAIProbeMaxInterval,
		)
	}
}

func TestShouldRefreshOpenAICodexSnapshot(t *testing.T) {
	t.Parallel()

	rateLimitedUntil := time.Now().Add(5 * time.Minute)
	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{RateLimitResetAt: &rateLimitedUntil}, usage, now) {
		t.Fatal("expected rate-limited account to force codex snapshot refresh")
	}

	if shouldRefreshOpenAICodexSnapshot(&Account{}, usage, now) {
		t.Fatal("expected complete non-rate-limited usage to skip codex snapshot refresh")
	}

	if !shouldRefreshOpenAICodexSnapshot(&Account{}, &UsageInfo{FiveHour: nil, SevenDay: &UsageProgress{}}, now) {
		t.Fatal("expected missing 5h snapshot to require refresh")
	}

	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	if !shouldRefreshOpenAICodexSnapshot(&Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"openai_oauth_responses_websockets_v2_enabled": true,
			"codex_usage_updated_at":                       staleAt,
		},
	}, usage, now) {
		t.Fatal("expected stale ws snapshot to trigger refresh")
	}
}

// TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2 外审第9轮 P1:spark 影子用量走
// QueryUsage(/wham/usage,与 WSv2 无关),staleness 不得被 WSv2 门控,否则首刷后窗口永久冻结。
func TestShouldRefreshOpenAICodexSnapshot_SparkShadowIgnoresWSv2(t *testing.T) {
	t.Parallel()

	now := time.Now()
	usage := &UsageInfo{
		FiveHour: &UsageProgress{Utilization: 0},
		SevenDay: &UsageProgress{Utilization: 0},
	}
	staleAt := now.Add(-(openAIProbeCacheTTL + time.Minute)).Format(time.RFC3339)
	freshAt := now.Add(-time.Minute).Format(time.RFC3339)
	parentID := int64(7001)

	// 影子无 WSv2,但首刷后窗口已存在;过期 codex_usage_updated_at 必须触发再刷新。
	shadowStale := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": staleAt},
	}
	if !shouldRefreshOpenAICodexSnapshot(shadowStale, usage, now) {
		t.Fatal("expected stale spark shadow (no WSv2) to trigger refresh")
	}

	// 影子时间戳仍新鲜→不刷(TTL 生效)。
	shadowFresh := &Account{
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		QuotaDimension:  QuotaDimensionSpark,
		Extra:           map[string]any{"codex_usage_updated_at": freshAt},
	}
	if shouldRefreshOpenAICodexSnapshot(shadowFresh, usage, now) {
		t.Fatal("expected fresh spark shadow to skip refresh (TTL not elapsed)")
	}

	// 反向对照:普通账号无 WSv2 + 过期时间戳→仍不刷(WSv2 门控普通账号的 probe 刷新)。
	normalNoWS := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_usage_updated_at": staleAt},
	}
	if shouldRefreshOpenAICodexSnapshot(normalNoWS, usage, now) {
		t.Fatal("expected non-WSv2 normal account to skip codex probe refresh")
	}
}

func TestExtractOpenAICodexProbeUpdatesAccepts429WithCodexHeaders(t *testing.T) {
	t.Parallel()

	headers := make(http.Header)
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "604800")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-reset-after-seconds", "18000")
	headers.Set("x-codex-secondary-window-minutes", "300")

	updates, err := extractOpenAICodexProbeUpdates(&http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
	if err != nil {
		t.Fatalf("extractOpenAICodexProbeUpdates() error = %v", err)
	}
	if len(updates) == 0 {
		t.Fatal("expected codex probe updates from 429 headers")
	}
	if got := updates["codex_5h_used_percent"]; got != 100.0 {
		t.Fatalf("codex_5h_used_percent = %v, want 100", got)
	}
	if got := updates["codex_7d_used_percent"]; got != 100.0 {
		t.Fatalf("codex_7d_used_percent = %v, want 100", got)
	}
}

func TestAccountUsageService_PersistOpenAICodexProbeSnapshotOnlyUpdatesExtra(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{
		updateExtraCh: make(chan map[string]any, 1),
		rateLimitCh:   make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	svc.persistOpenAICodexProbeSnapshot(321, map[string]any{
		"codex_7d_used_percent": 100.0,
		"codex_7d_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
	})

	select {
	case updates := <-repo.updateExtraCh:
		if got := updates["codex_7d_used_percent"]; got != 100.0 {
			t.Fatalf("codex_7d_used_percent = %v, want 100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待 codex 探测快照写入 extra 超时")
	}

	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将探测快照写入运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAccountUsageService_GetOpenAIUsage_DoesNotPromoteCodexExtraToRateLimit(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(6 * 24 * time.Hour).UTC().Truncate(time.Second)
	repo := &accountUsageCodexProbeRepo{
		rateLimitCh: make(chan time.Time, 1),
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 1.0,
			"codex_5h_reset_at":     time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339),
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}

	usage, err := svc.getOpenAIUsage(context.Background(), account, false)
	if err != nil {
		t.Fatalf("getOpenAIUsage() error = %v", err)
	}
	if usage.SevenDay == nil || usage.SevenDay.Utilization != 100.0 {
		t.Fatalf("预期 7 天用量仍然可见，实际为 %#v", usage.SevenDay)
	}
	if account.RateLimitResetAt != nil {
		t.Fatalf("不应让已耗尽的 codex extra 改写运行时限流状态: %v", account.RateLimitResetAt)
	}
	select {
	case got := <-repo.rateLimitCh:
		t.Fatalf("不应将已耗尽的 codex extra 持久化为运行时限流状态: %v", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestBuildCodexUsageProgressFromExtra_ZerosExpiredWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC)

	t.Run("expired 5h window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     "2026-03-16T10:00:00Z", // 2h ago
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired window, got %v", progress.Utilization)
		}
		if progress.RemainingSeconds != 0 {
			t.Fatalf("expected RemainingSeconds=0, got %v", progress.RemainingSeconds)
		}
	})

	t.Run("active 5h window keeps utilization", func(t *testing.T) {
		resetAt := now.Add(2 * time.Hour).Format(time.RFC3339)
		extra := map[string]any{
			"codex_5h_used_percent": 42.0,
			"codex_5h_reset_at":     resetAt,
		}
		progress := buildCodexUsageProgressFromExtra(extra, "5h", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 42.0 {
			t.Fatalf("expected Utilization=42, got %v", progress.Utilization)
		}
	})

	t.Run("expired 7d window zeroes utilization", func(t *testing.T) {
		extra := map[string]any{
			"codex_7d_used_percent": 88.0,
			"codex_7d_reset_at":     "2026-03-15T00:00:00Z", // yesterday
		}
		progress := buildCodexUsageProgressFromExtra(extra, "7d", now)
		if progress == nil {
			t.Fatal("expected non-nil progress")
		}
		if progress.Utilization != 0 {
			t.Fatalf("expected Utilization=0 for expired 7d window, got %v", progress.Utilization)
		}
	})
}
