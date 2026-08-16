package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

type openAISummaryAccountRepoStub struct {
	AccountRepository
	accounts []Account
	updates  []OpenAICapacityHistoryUpdate
}

func (r *openAISummaryAccountRepoStub) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	return r.accounts, nil
}

func (r *openAISummaryAccountRepoStub) BatchUpdateOpenAICapacityHistory(_ context.Context, updates []OpenAICapacityHistoryUpdate) error {
	r.updates = updates
	return nil
}

type openAISummaryUsageRepoStub struct {
	UsageLogRepository
	costs   map[int64]OpenAIWindowCosts
	queries []OpenAIWindowCostQuery
}

func (r *openAISummaryUsageRepoStub) GetOpenAIWindowCostsBatch(_ context.Context, queries []OpenAIWindowCostQuery, _ time.Time) (map[int64]OpenAIWindowCosts, error) {
	r.queries = queries
	return r.costs, nil
}

type openAISummarySettingRepoStub struct {
	SettingRepository
	values map[string]string
}

func (r *openAISummarySettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *openAISummarySettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestOpenAIOAuthUsageSummaryScopeAndWeightedEstimate(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	reset5h := now.Add(2 * time.Hour)
	reset7d := now.Add(4 * 24 * time.Hour)
	main := summaryAccount(80, "active", "plus", map[string]any{
		"codex_5h_used_percent": 54.0, "codex_5h_window_minutes": 300.0,
		"codex_5h_reset_at":     reset5h.Format(time.RFC3339),
		"codex_7d_used_percent": 25.0, "codex_7d_window_minutes": 10080.0,
		"codex_7d_reset_at":      reset7d.Format(time.RFC3339),
		"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
	})
	zero := summaryAccount(81, "active", "plus", map[string]any{
		"codex_5h_used_percent": 0.0, "codex_5h_window_minutes": 0.0,
		"codex_7d_used_percent": 0.0, "codex_7d_window_minutes": 0.0,
	})
	parentID := int64(80)
	shadow := summaryAccount(82, "active", "plus", main.Extra)
	shadow.ParentAccountID = &parentID
	shadow.QuotaDimension = QuotaDimensionSpark
	excluded := summaryAccount(83, "inactive", "plus", zero.Extra)

	accountRepo := &openAISummaryAccountRepoStub{accounts: []Account{main, zero, shadow, excluded}}
	usageRepo := &openAISummaryUsageRepoStub{costs: map[int64]OpenAIWindowCosts{
		80: {FiveHour: 1200, SevenDay: 500},
	}}
	settings := &openAISummarySettingRepoStub{values: map[string]string{}}
	service := &AccountUsageService{accountRepo: accountRepo, usageLogRepo: usageRepo, settingRepo: settings}

	summary, err := service.computeOpenAIOAuthUsageSummary(context.Background(), now)
	if err != nil {
		t.Fatalf("compute summary: %v", err)
	}
	if summary.AccountCount != 3 || summary.IncludedAccountCount != 2 || summary.ExcludedAccountCount != 1 {
		t.Fatalf("unexpected account counts: %+v", summary)
	}
	assertNear(t, summary.FiveHour.Used, 1200)
	assertNear(t, *summary.FiveHour.EstimatedCapacity, 2400/0.54)
	assertNear(t, *summary.FiveHour.EstimatedRemaining, 2400/0.54-1200)
	assertNear(t, summary.FiveHour.UsagePercent, 27)
	if summary.FiveHour.EstimatedAccountCount != 2 || summary.FiveHour.UnestimatedAccountCount != 0 {
		t.Fatalf("unexpected estimate counts: %+v", summary.FiveHour)
	}
	if len(usageRepo.queries) != 2 || usageRepo.queries[0].AccountID != 80 || usageRepo.queries[1].AccountID != 81 {
		t.Fatalf("summary query scope = %+v", usageRepo.queries)
	}
	if len(accountRepo.updates) != 1 || accountRepo.updates[0].AccountID != 80 {
		t.Fatalf("history updates = %+v", accountRepo.updates)
	}
}

func TestOpenAIOAuthUsageSummaryKeepsHistoryAtZeroPercent(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account := summaryAccount(1, "active", "team", map[string]any{
		"codex_5h_used_percent": 0.0, "codex_5h_window_minutes": 0.0,
		openAICapacity5hKey:     2000.0,
		"codex_7d_used_percent": 0.0, "codex_7d_window_minutes": 0.0,
	})
	stored := openAIStoredCapacityReference{Global: openAIFloat64Ptr(1800), ByPlan: map[string]float64{"team": 1900}}
	raw, _ := json.Marshal(stored)
	accountRepo := &openAISummaryAccountRepoStub{accounts: []Account{account}}
	service := &AccountUsageService{
		accountRepo:  accountRepo,
		usageLogRepo: &openAISummaryUsageRepoStub{costs: map[int64]OpenAIWindowCosts{}},
		settingRepo:  &openAISummarySettingRepoStub{values: map[string]string{openAICapacityReference5hKey: string(raw)}},
	}

	summary, err := service.computeOpenAIOAuthUsageSummary(context.Background(), now)
	if err != nil {
		t.Fatalf("compute summary: %v", err)
	}
	assertNear(t, *summary.FiveHour.EstimatedCapacity, 2000)
	assertNear(t, *summary.FiveHour.EstimatedRemaining, 2000)
	if summary.FiveHour.ReferenceSource != "historical" || len(accountRepo.updates) != 0 {
		t.Fatalf("zero percent overwrote history: summary=%+v updates=%+v", summary.FiveHour, accountRepo.updates)
	}
	if summary.SevenDay.EstimatedCapacity != nil || summary.SevenDay.UnestimatedAccountCount != 1 {
		t.Fatalf("first zero-percent 7d window should remain unestimated: %+v", summary.SevenDay)
	}
}

func TestResolveOpenAIWindowStateAfterReset(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	reset := now.Add(-time.Hour)
	percent, start, known, current, pending := resolveOpenAIWindowState(map[string]any{
		"codex_5h_used_percent":   70.0,
		"codex_5h_window_minutes": 300.0,
		"codex_5h_reset_at":       reset.Format(time.RFC3339),
		"codex_usage_updated_at":  reset.Add(-time.Minute).Format(time.RFC3339),
	}, openAIFiveHourWindow, now)
	if percent != 0 || start == nil || !start.Equal(reset) || !known || current || !pending {
		t.Fatalf("unexpected just-reset state: percent=%v start=%v known=%v current=%v pending=%v", percent, start, known, current, pending)
	}

	oldReset := now.Add(-6 * time.Hour)
	_, start, known, _, pending = resolveOpenAIWindowState(map[string]any{
		"codex_5h_used_percent":   70.0,
		"codex_5h_window_minutes": 300.0,
		"codex_5h_reset_at":       oldReset.Format(time.RFC3339),
	}, openAIFiveHourWindow, now)
	if start != nil || known || !pending {
		t.Fatalf("stale window must not bill old usage: start=%v known=%v pending=%v", start, known, pending)
	}
}

func TestAggregateOpenAIWindowFiltersOutliersAndPrefersPlan(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	inputs := []*openAIAccountWindowInput{
		{account: Account{ID: 1}, plan: "plus", percent: 50, used: 500, candidate: 1000, historySample: true, sampleCurrent: true},
		{account: Account{ID: 2}, plan: "plus", percent: 50, used: 550, candidate: 1100, historySample: true, sampleCurrent: true},
		{account: Account{ID: 3}, plan: "plus", percent: 50, used: 5000, candidate: 10000, historySample: true, sampleCurrent: true},
		{account: Account{ID: 4}, plan: "plus", history: 900},
		{account: Account{ID: 5}, plan: "team"},
	}

	_, updates, _ := aggregateOpenAIWindow(inputs, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	if inputs[2].accepted || inputs[2].capacity != 1050 {
		t.Fatalf("outlier should use current plan median: %+v", inputs[2])
	}
	if inputs[3].capacity != 900 || inputs[3].source != "historical" {
		t.Fatalf("own history must precede plan reference: %+v", inputs[3])
	}
	if inputs[4].capacity != 1050 || inputs[4].source != "current" {
		t.Fatalf("missing plan should use current global median: %+v", inputs[4])
	}
	if len(updates) != 2 {
		t.Fatalf("only accepted current samples should update history: %+v", updates)
	}
}

func TestOpenAIWindowPercentIsClampedAndFullUsageHasNoRemaining(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	percent, start, known, current, pending := resolveOpenAIWindowState(map[string]any{
		"codex_5h_used_percent":   154.0,
		"codex_5h_window_minutes": 300.0,
		"codex_5h_reset_at":       reset.Format(time.RFC3339),
		"codex_usage_updated_at":  now.Format(time.RFC3339),
	}, openAIFiveHourWindow, now)
	if percent != 100 || start == nil || !known || !current || pending {
		t.Fatalf("invalid clamped state: percent=%v start=%v known=%v current=%v pending=%v", percent, start, known, current, pending)
	}
	input := &openAIAccountWindowInput{account: Account{ID: 1}, plan: "plus", percent: percent, start: start, costKnown: true, sampleCurrent: true}
	applyOpenAIWindowCosts([]*openAIAccountWindowInput{input}, map[int64]OpenAIWindowCosts{1: {FiveHour: 1200}}, true)
	summary, _, _ := aggregateOpenAIWindow([]*openAIAccountWindowInput{input}, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	assertNear(t, *summary.EstimatedCapacity, 1200)
	assertNear(t, *summary.EstimatedRemaining, 0)
	assertNear(t, summary.UsagePercent, 100)
}

func TestOpenAIWindowInvalidNumbersDoNotPolluteSummary(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	for _, value := range []float64{-5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		percent, _, _, _, _ := resolveOpenAIWindowState(map[string]any{
			"codex_5h_used_percent": value, "codex_5h_window_minutes": 300.0,
			"codex_5h_reset_at": reset.Format(time.RFC3339), "codex_usage_updated_at": now.Format(time.RFC3339),
		}, openAIFiveHourWindow, now)
		if percent != 0 {
			t.Fatalf("invalid percent %v was not clamped to zero: %v", value, percent)
		}
	}

	start := now.Add(-5 * time.Hour)
	inputs := []*openAIAccountWindowInput{
		{account: Account{ID: 1}, percent: 50, start: &start, costKnown: true, sampleCurrent: true},
		{account: Account{ID: 2}, percent: 50, start: &start, costKnown: true, sampleCurrent: true},
	}
	applyOpenAIWindowCosts(inputs, map[int64]OpenAIWindowCosts{
		1: {FiveHour: math.NaN()},
		2: {FiveHour: -10},
	}, true)
	for _, input := range inputs {
		if input.used != 0 || input.candidate != 0 {
			t.Fatalf("invalid cost polluted input: %+v", input)
		}
	}
}

func TestOpenAIWindowDynamicCapacityRequiresTenPercentUsage(t *testing.T) {
	start := time.Date(2026, 8, 16, 7, 0, 0, 0, time.UTC)
	belowThreshold := &openAIAccountWindowInput{
		account: Account{ID: 1}, percent: 9.99, start: &start, costKnown: true, sampleCurrent: true,
	}
	atThreshold := &openAIAccountWindowInput{
		account: Account{ID: 2}, percent: 10, start: &start, costKnown: true, sampleCurrent: true,
	}
	inputs := []*openAIAccountWindowInput{belowThreshold, atThreshold}
	costs := map[int64]OpenAIWindowCosts{
		1: {FiveHour: 199.8, SevenDay: 199.8},
		2: {FiveHour: 210, SevenDay: 210},
	}

	applyOpenAIWindowCosts(inputs, costs, true)
	if belowThreshold.candidate != 0 {
		t.Fatalf("9.99%% usage generated a capacity candidate: %v", belowThreshold.candidate)
	}
	assertNear(t, belowThreshold.used, 199.8)
	assertNear(t, atThreshold.candidate, 2100)

	belowThreshold.candidate = 0
	atThreshold.candidate = 0
	applyOpenAIWindowCosts(inputs, costs, false)
	if belowThreshold.candidate != 0 {
		t.Fatalf("9.99%% seven-day usage generated a capacity candidate: %v", belowThreshold.candidate)
	}
	assertNear(t, belowThreshold.used, 199.8)
	assertNear(t, atThreshold.candidate, 2100)
}

func TestAggregateOpenAIWindowUsesTransientDynamicEstimateBeforePoolFallback(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	start := now.Add(-7 * 24 * time.Hour)
	inputs := []*openAIAccountWindowInput{
		{account: Account{ID: 1}, plan: "pro", percent: 50, used: 500, candidate: 1000, historySample: true, sampleCurrent: true, start: &start},
		{account: Account{ID: 2}, plan: "pro", percent: 50, used: 550, candidate: 1100, historySample: true, sampleCurrent: true, start: &start},
		{account: Account{ID: 3}, plan: "pro", percent: 100, used: 200, candidate: 200, sampleCurrent: true, start: &start},
	}

	summary, updates, _ := aggregateOpenAIWindow(inputs, openAISevenDayWindow, openAIStoredCapacityReference{}, now)
	if inputs[2].accepted {
		t.Fatalf("outlier must not be accepted for history: %+v", inputs[2])
	}
	assertNear(t, inputs[2].capacity, 200)
	assertNear(t, *summary.EstimatedCapacity, 2300)
	assertNear(t, *summary.EstimatedRemaining, 1050)
	if len(updates) != 2 {
		t.Fatalf("transient outlier must not be persisted: %+v", updates)
	}
}

func TestAggregateOpenAIWindowDoesNotRewriteUnchangedHistory(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	start := now.Add(-5 * time.Hour)
	capacity := 2000.0
	input := &openAIAccountWindowInput{
		account: Account{ID: 1, Extra: map[string]any{
			openAICapacity5hKey:            capacity,
			openAICapacity5hWindowStartKey: start.Format(time.RFC3339),
			openAICapacity5hPlanKey:        "plus",
		}},
		plan: "plus", percent: 50, start: &start, costKnown: true, sampleCurrent: true,
		used: 1000, candidate: capacity, historySample: true, history: capacity,
	}
	stored := openAIStoredCapacityReference{
		Global: openAIFloat64Ptr(capacity),
		ByPlan: map[string]float64{"plus": capacity},
	}

	_, updates, reference := aggregateOpenAIWindow([]*openAIAccountWindowInput{input}, openAIFiveHourWindow, stored, now)
	if len(updates) != 0 || reference != nil {
		t.Fatalf("unchanged derived cache should not be rewritten: updates=%+v reference=%+v", updates, reference)
	}
}

func TestAggregateOpenAIWindowUsesCapacityWeightedPercent(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	inputs := []*openAIAccountWindowInput{
		{account: Account{ID: 1}, plan: "plus", percent: 20, used: 20, candidate: 100, historySample: true},
		{account: Account{ID: 2}, plan: "plus", percent: 80, used: 160, candidate: 200, historySample: true},
	}

	summary, _, _ := aggregateOpenAIWindow(inputs, openAISevenDayWindow, openAIStoredCapacityReference{}, now)
	assertNear(t, summary.Used, 180)
	assertNear(t, *summary.EstimatedCapacity, 300)
	assertNear(t, *summary.EstimatedRemaining, 120)
	assertNear(t, summary.UsagePercent, 60)
	assertNear(t, summary.RemainingPercent, 40)
}

func TestAggregateOpenAIWindowZeroPercentUsesHistoryAndCurrentCost(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	input := &openAIAccountWindowInput{
		account: Account{ID: 1}, plan: "plus", percent: 0, used: 0.5, history: 100,
	}

	summary, updates, _ := aggregateOpenAIWindow([]*openAIAccountWindowInput{input}, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	assertNear(t, summary.Used, 0.5)
	assertNear(t, *summary.EstimatedCapacity, 100)
	assertNear(t, *summary.EstimatedRemaining, 99.5)
	if len(updates) != 0 {
		t.Fatalf("zero-percent observation updated history: %+v", updates)
	}
}

func TestOpenAIWindowZeroPercentKeepsCurrentWindowCost(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	account := summaryAccount(1, StatusActive, "plus", map[string]any{
		"codex_5h_used_percent": 0.0, "codex_5h_window_minutes": 300.0,
		"codex_5h_reset_at": reset.Format(time.RFC3339), "codex_usage_updated_at": now.Format(time.RFC3339),
		openAICapacity5hKey: 100.0, openAICapacity5hPlanKey: "plus",
	})
	inputs, _ := buildOpenAIWindowInputs([]Account{account}, openAIFiveHourWindow, now)
	applyOpenAIWindowCosts(inputs, map[int64]OpenAIWindowCosts{1: {FiveHour: 0.5}}, true)

	summary, _, _ := aggregateOpenAIWindow(inputs, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	assertNear(t, summary.Used, 0.5)
	assertNear(t, *summary.EstimatedCapacity, 100)
	assertNear(t, *summary.EstimatedRemaining, 99.5)
}

func TestAggregateOpenAIWindowFirstAllZeroRemainsUnestimated(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	inputs := []*openAIAccountWindowInput{
		{account: Account{ID: 1}, plan: "plus"},
		{account: Account{ID: 2}, plan: "plus"},
	}

	summary, updates, reference := aggregateOpenAIWindow(inputs, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	if summary.EstimatedCapacity != nil || summary.EstimatedRemaining != nil {
		t.Fatalf("first all-zero window was estimated: %+v", summary)
	}
	assertNear(t, summary.UsagePercent, 0)
	assertNear(t, summary.RemainingPercent, 100)
	if summary.UnestimatedAccountCount != 2 || len(updates) != 0 || reference != nil {
		t.Fatalf("unexpected all-zero result: summary=%+v updates=%+v reference=%+v", summary, updates, reference)
	}
}

func TestBuildOpenAIWindowInputsRejectsHistoryFromAnotherPlan(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	account := summaryAccount(1, StatusActive, "team", map[string]any{
		"codex_5h_used_percent": 0.0, "codex_5h_window_minutes": 0.0,
		openAICapacity5hKey: 100.0, openAICapacity5hPlanKey: "plus",
	})

	inputs, _ := buildOpenAIWindowInputs([]Account{account}, openAIFiveHourWindow, now)
	if len(inputs) != 1 || inputs[0].history != 0 {
		t.Fatalf("history from another plan remained eligible: %+v", inputs)
	}
}

func TestOpenAIWindowHighUsageEstimateIsTransient(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	start := now.Add(-5 * time.Hour)
	input := &openAIAccountWindowInput{
		account: Account{ID: 1}, plan: "plus", percent: 95, start: &start, costKnown: true, sampleCurrent: true,
	}
	applyOpenAIWindowCosts([]*openAIAccountWindowInput{input}, map[int64]OpenAIWindowCosts{1: {FiveHour: 95}}, true)

	summary, updates, reference := aggregateOpenAIWindow([]*openAIAccountWindowInput{input}, openAIFiveHourWindow, openAIStoredCapacityReference{}, now)
	assertNear(t, *summary.EstimatedCapacity, 100)
	if input.accepted || len(updates) != 0 || reference != nil || summary.CurrentSampleAccountCount != 0 {
		t.Fatalf("high-usage estimate polluted trusted history: input=%+v updates=%+v reference=%+v summary=%+v", input, updates, reference, summary)
	}
}

func TestNormalizeOpenAIPlanRejectsAnomalousValues(t *testing.T) {
	if got := normalizeOpenAIPlan(" PLUS "); got != "plus" {
		t.Fatalf("normalized plan = %q", got)
	}
	for _, value := range []string{"", "abnormal", "123", "future-plan?"} {
		if got := normalizeOpenAIPlan(value); got != "unknown" {
			t.Fatalf("normalizeOpenAIPlan(%q) = %q, want unknown", value, got)
		}
	}
}

func summaryAccount(id int64, status, plan string, extra map[string]any) Account {
	return Account{
		ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: status,
		QuotaDimension: QuotaDimensionGlobal, Schedulable: true,
		Credentials: map[string]any{"plan_type": plan},
		Extra:       extra,
	}
}

func assertNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("got %v, want %v", got, want)
	}
}
