package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	openAICapacity5hKey             = "openai_capacity_5h_last_known"
	openAICapacity5hUpdatedAtKey    = "openai_capacity_5h_updated_at"
	openAICapacity5hWindowStartKey  = "openai_capacity_5h_window_start"
	openAICapacity5hPlanKey         = "openai_capacity_5h_plan_type"
	openAICapacity7dKey             = "openai_capacity_7d_last_known"
	openAICapacity7dUpdatedAtKey    = "openai_capacity_7d_updated_at"
	openAICapacity7dWindowStartKey  = "openai_capacity_7d_window_start"
	openAICapacity7dPlanKey         = "openai_capacity_7d_plan_type"
	openAICapacityReference5hKey    = "openai_oauth_capacity_reference_5h"
	openAICapacityReference7dKey    = "openai_oauth_capacity_reference_7d"
	openAICapacityMinimumSamplePct  = 10.0
	openAICapacityMaximumTrustedPct = 90.0
	openAICapacityOutlierLowerRatio = 0.5
	openAICapacityOutlierUpperRatio = 2.0
)

var openAICapacityManagedExtraKeys = []string{
	openAICapacity5hKey,
	openAICapacity5hUpdatedAtKey,
	openAICapacity5hWindowStartKey,
	openAICapacity5hPlanKey,
	openAICapacity7dKey,
	openAICapacity7dUpdatedAtKey,
	openAICapacity7dWindowStartKey,
	openAICapacity7dPlanKey,
}

type OpenAIWindowCostQuery struct {
	AccountID     int64      `json:"account_id"`
	FiveHourStart *time.Time `json:"five_hour_start,omitempty"`
	SevenDayStart *time.Time `json:"seven_day_start,omitempty"`
}

type OpenAIWindowCosts struct {
	FiveHour float64
	SevenDay float64
}

type OpenAICapacityHistoryUpdate struct {
	AccountID int64          `json:"account_id"`
	Updates   map[string]any `json:"updates"`
}

type openAIWindowCostBatchReader interface {
	GetOpenAIWindowCostsBatch(ctx context.Context, queries []OpenAIWindowCostQuery, end time.Time) (map[int64]OpenAIWindowCosts, error)
}

type openAICapacityHistoryBatchWriter interface {
	BatchUpdateOpenAICapacityHistory(ctx context.Context, updates []OpenAICapacityHistoryUpdate) error
}

type OpenAIOAuthUsageSummary struct {
	AccountCount         int                      `json:"account_count"`
	IncludedAccountCount int                      `json:"included_account_count"`
	ExcludedAccountCount int                      `json:"excluded_account_count"`
	GeneratedAt          time.Time                `json:"generated_at"`
	FiveHour             OpenAIUsageWindowSummary `json:"five_hour"`
	SevenDay             OpenAIUsageWindowSummary `json:"seven_day"`
}

type OpenAIUsageWindowSummary struct {
	Used                      float64  `json:"used"`
	EstimatedRemaining        *float64 `json:"estimated_remaining"`
	EstimatedCapacity         *float64 `json:"estimated_capacity"`
	UsagePercent              float64  `json:"usage_percent"`
	RemainingPercent          float64  `json:"remaining_percent"`
	ReferenceCapacity         *float64 `json:"reference_capacity"`
	ReferenceSource           string   `json:"reference_source"`
	CurrentSampleAccountCount int      `json:"current_sample_account_count"`
	EstimatedAccountCount     int      `json:"estimated_account_count"`
	UnestimatedAccountCount   int      `json:"unestimated_account_count"`
	PendingSyncAccountCount   int      `json:"pending_sync_account_count"`
}

type openAIStoredCapacityReference struct {
	Global    *float64           `json:"global,omitempty"`
	ByPlan    map[string]float64 `json:"by_plan,omitempty"`
	UpdatedAt string             `json:"updated_at,omitempty"`
}

type openAIWindowDefinition struct {
	name             string
	duration         time.Duration
	usedPercentKey   string
	resetAtKey       string
	windowMinutesKey string
	historyKey       string
	historyUpdated   string
	historyStart     string
	historyPlan      string
	settingKey       string
}

var (
	openAIFiveHourWindow = openAIWindowDefinition{
		name: "5h", duration: 5 * time.Hour,
		usedPercentKey: "codex_5h_used_percent", resetAtKey: "codex_5h_reset_at", windowMinutesKey: "codex_5h_window_minutes",
		historyKey: openAICapacity5hKey, historyUpdated: openAICapacity5hUpdatedAtKey, historyStart: openAICapacity5hWindowStartKey, historyPlan: openAICapacity5hPlanKey,
		settingKey: openAICapacityReference5hKey,
	}
	openAISevenDayWindow = openAIWindowDefinition{
		name: "7d", duration: 7 * 24 * time.Hour,
		usedPercentKey: "codex_7d_used_percent", resetAtKey: "codex_7d_reset_at", windowMinutesKey: "codex_7d_window_minutes",
		historyKey: openAICapacity7dKey, historyUpdated: openAICapacity7dUpdatedAtKey, historyStart: openAICapacity7dWindowStartKey, historyPlan: openAICapacity7dPlanKey,
		settingKey: openAICapacityReference7dKey,
	}
)

type openAIAccountWindowInput struct {
	account       Account
	plan          string
	percent       float64
	start         *time.Time
	costKnown     bool
	sampleCurrent bool
	pending       bool
	used          float64
	history       float64
	candidate     float64
	historySample bool
	accepted      bool
	capacity      float64
	source        string
}

// GetOpenAIOAuthUsageSummary computes a full-pool summary from persisted quota
// snapshots and account-billed usage logs. Every undeleted OpenAI OAuth global
// main account is included regardless of status or schedulability. Valid
// estimates are cached as read-through history; zero-percent observations never
// overwrite that history.
func (s *AccountUsageService) GetOpenAIOAuthUsageSummary(ctx context.Context) (*OpenAIOAuthUsageSummary, error) {
	if s == nil || s.accountRepo == nil || s.usageLogRepo == nil {
		return nil, fmt.Errorf("openai oauth usage summary is not configured")
	}
	value, err, _ := s.openAISummaryFlight.Do("openai-oauth-usage-summary", func() (any, error) {
		return s.computeOpenAIOAuthUsageSummary(ctx, time.Now())
	})
	if err != nil {
		return nil, err
	}
	return value.(*OpenAIOAuthUsageSummary), nil
}

func (s *AccountUsageService) computeOpenAIOAuthUsageSummary(ctx context.Context, now time.Time) (*OpenAIOAuthUsageSummary, error) {
	accounts, err := s.accountRepo.ListAllWithFilters(ctx, PlatformOpenAI, AccountTypeOAuth, "", "", 0, "")
	if err != nil {
		return nil, fmt.Errorf("list openai oauth accounts: %w", err)
	}
	poolAccounts := make([]Account, 0, len(accounts))
	for i := range accounts {
		if !accounts[i].IsShadow() && accounts[i].QuotaDimensionOrDefault() == QuotaDimensionGlobal {
			poolAccounts = append(poolAccounts, accounts[i])
		}
	}
	// This is an accounting view, not a scheduler availability view. Accounts
	// that are inactive, manually unschedulable, or currently rate limited still
	// contribute to the pool's historical usage and capacity estimate.
	included := poolAccounts

	refs, err := s.loadOpenAICapacityReferences(ctx)
	if err != nil {
		return nil, err
	}
	fiveInputs, fiveQueries := buildOpenAIWindowInputs(included, openAIFiveHourWindow, now)
	sevenInputs, sevenQueries := buildOpenAIWindowInputs(included, openAISevenDayWindow, now)
	queries := mergeOpenAIWindowCostQueries(fiveQueries, sevenQueries)
	costReader, ok := s.usageLogRepo.(openAIWindowCostBatchReader)
	if !ok {
		return nil, fmt.Errorf("usage repository does not support openai window cost batches")
	}
	costs, err := costReader.GetOpenAIWindowCostsBatch(ctx, queries, now)
	if err != nil {
		return nil, fmt.Errorf("get openai window costs: %w", err)
	}
	applyOpenAIWindowCosts(fiveInputs, costs, true)
	applyOpenAIWindowCosts(sevenInputs, costs, false)

	fiveSummary, fiveUpdates, fiveRef := aggregateOpenAIWindow(fiveInputs, openAIFiveHourWindow, refs[openAIFiveHourWindow.settingKey], now)
	sevenSummary, sevenUpdates, sevenRef := aggregateOpenAIWindow(sevenInputs, openAISevenDayWindow, refs[openAISevenDayWindow.settingKey], now)

	if writer, ok := s.accountRepo.(openAICapacityHistoryBatchWriter); ok {
		updates := mergeOpenAICapacityUpdates(fiveUpdates, sevenUpdates)
		if err := writer.BatchUpdateOpenAICapacityHistory(ctx, updates); err != nil {
			return nil, fmt.Errorf("persist openai capacity history: %w", err)
		}
	} else if len(fiveUpdates)+len(sevenUpdates) > 0 {
		return nil, fmt.Errorf("account repository does not support openai capacity history batches")
	}
	if s.settingRepo != nil {
		values := make(map[string]string, 2)
		if fiveRef != nil {
			raw, _ := json.Marshal(fiveRef)
			values[openAIFiveHourWindow.settingKey] = string(raw)
		}
		if sevenRef != nil {
			raw, _ := json.Marshal(sevenRef)
			values[openAISevenDayWindow.settingKey] = string(raw)
		}
		if len(values) > 0 {
			if err := s.settingRepo.SetMultiple(ctx, values); err != nil {
				return nil, fmt.Errorf("persist openai capacity references: %w", err)
			}
		}
	}

	return &OpenAIOAuthUsageSummary{
		AccountCount:         len(poolAccounts),
		IncludedAccountCount: len(included),
		ExcludedAccountCount: len(poolAccounts) - len(included),
		GeneratedAt:          now,
		FiveHour:             fiveSummary,
		SevenDay:             sevenSummary,
	}, nil
}

func (s *AccountUsageService) loadOpenAICapacityReferences(ctx context.Context) (map[string]openAIStoredCapacityReference, error) {
	result := map[string]openAIStoredCapacityReference{
		openAICapacityReference5hKey: {ByPlan: map[string]float64{}},
		openAICapacityReference7dKey: {ByPlan: map[string]float64{}},
	}
	if s.settingRepo == nil {
		return result, nil
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{openAICapacityReference5hKey, openAICapacityReference7dKey})
	if err != nil {
		return nil, fmt.Errorf("load openai capacity references: %w", err)
	}
	for key, raw := range values {
		var ref openAIStoredCapacityReference
		if json.Unmarshal([]byte(raw), &ref) == nil {
			if ref.ByPlan == nil {
				ref.ByPlan = map[string]float64{}
			}
			result[key] = ref
		}
	}
	return result, nil
}

func buildOpenAIWindowInputs(accounts []Account, def openAIWindowDefinition, now time.Time) ([]*openAIAccountWindowInput, []OpenAIWindowCostQuery) {
	inputs := make([]*openAIAccountWindowInput, 0, len(accounts))
	queries := make([]OpenAIWindowCostQuery, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		percent, start, costKnown, sampleCurrent, pending := resolveOpenAIWindowState(account.Extra, def, now)
		plan := normalizeOpenAIPlan(account.GetCredential("plan_type"))
		history := positiveFinite(parseExtraFloat64(account.Extra[def.historyKey]))
		if !openAICapacityHistoryMatchesPlan(account.Extra, def, plan) {
			history = 0
		}
		input := &openAIAccountWindowInput{
			account: account, plan: plan, percent: percent,
			start: start, costKnown: costKnown, sampleCurrent: sampleCurrent, pending: pending,
			history: history,
		}
		inputs = append(inputs, input)
		query := OpenAIWindowCostQuery{AccountID: account.ID}
		if def.name == "5h" {
			query.FiveHourStart = start
		} else {
			query.SevenDayStart = start
		}
		queries = append(queries, query)
	}
	return inputs, queries
}

func resolveOpenAIWindowState(extra map[string]any, def openAIWindowDefinition, now time.Time) (float64, *time.Time, bool, bool, bool) {
	if extra == nil {
		return 0, nil, false, false, true
	}
	raw, exists := extra[def.usedPercentKey]
	if !exists {
		return 0, nil, false, false, true
	}
	percent := clampPercent(parseExtraFloat64(raw))
	windowMinutes := int(parseExtraFloat64(extra[def.windowMinutesKey]))
	resetAt := parseExtraTime(extra[def.resetAtKey])
	updatedAt := parseExtraTime(extra["codex_usage_updated_at"])
	if percent == 0 && windowMinutes == 0 {
		return 0, nil, true, true, false
	}
	if resetAt.IsZero() {
		return percent, nil, false, false, true
	}
	if now.Before(resetAt) {
		start := resetAt.Add(-def.duration)
		if updatedAt.IsZero() || updatedAt.Before(start) || updatedAt.After(now.Add(time.Minute)) {
			return percent, nil, false, false, true
		}
		return percent, &start, true, true, false
	}
	if now.Sub(resetAt) <= def.duration {
		// The cached quota belongs to the old window. Billing after resetAt is
		// current, but the percentage must wait for the next quota observation.
		start := resetAt
		return 0, &start, true, false, true
	}
	return 0, nil, false, false, true
}

func mergeOpenAIWindowCostQueries(five, seven []OpenAIWindowCostQuery) []OpenAIWindowCostQuery {
	merged := make(map[int64]OpenAIWindowCostQuery, len(five)+len(seven))
	for _, query := range append(five, seven...) {
		current := merged[query.AccountID]
		current.AccountID = query.AccountID
		if query.FiveHourStart != nil {
			current.FiveHourStart = query.FiveHourStart
		}
		if query.SevenDayStart != nil {
			current.SevenDayStart = query.SevenDayStart
		}
		merged[query.AccountID] = current
	}
	result := make([]OpenAIWindowCostQuery, 0, len(merged))
	for _, query := range merged {
		result = append(result, query)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AccountID < result[j].AccountID })
	return result
}

func applyOpenAIWindowCosts(inputs []*openAIAccountWindowInput, costs map[int64]OpenAIWindowCosts, fiveHour bool) {
	for _, input := range inputs {
		if !input.costKnown {
			continue
		}
		if input.start == nil {
			input.used = 0
			continue
		}
		cost := costs[input.account.ID]
		if fiveHour {
			input.used = nonNegativeFinite(cost.FiveHour)
		} else {
			input.used = nonNegativeFinite(cost.SevenDay)
		}
		if input.sampleCurrent && input.percent >= openAICapacityMinimumSamplePct && input.used > 0 {
			candidate, _ := decimal.NewFromFloat(input.used).
				Mul(decimal.NewFromInt(100)).
				Div(decimal.NewFromFloat(input.percent)).
				Float64()
			input.candidate = positiveFinite(candidate)
			input.historySample = input.percent <= openAICapacityMaximumTrustedPct
		}
	}
}

func aggregateOpenAIWindow(inputs []*openAIAccountWindowInput, def openAIWindowDefinition, stored openAIStoredCapacityReference, now time.Time) (OpenAIUsageWindowSummary, []OpenAICapacityHistoryUpdate, *openAIStoredCapacityReference) {
	rawByPlan := make(map[string][]float64)
	for _, input := range inputs {
		if input.candidate > 0 && input.historySample {
			rawByPlan[input.plan] = append(rawByPlan[input.plan], input.candidate)
		}
	}
	for _, input := range inputs {
		if input.candidate <= 0 || !input.historySample {
			continue
		}
		accept := true
		if input.history > 0 {
			accept = withinCapacityRange(input.candidate, input.history)
		}
		if samples := rawByPlan[input.plan]; accept && len(samples) >= 3 {
			accept = withinCapacityRange(input.candidate, median(samples))
		}
		input.accepted = accept
	}

	currentByPlan := make(map[string][]float64)
	currentAll := make([]float64, 0, len(inputs))
	for _, input := range inputs {
		if input.accepted {
			currentByPlan[input.plan] = append(currentByPlan[input.plan], input.candidate)
			currentAll = append(currentAll, input.candidate)
		}
	}
	currentPlanMedian := make(map[string]float64, len(currentByPlan))
	for plan, samples := range currentByPlan {
		currentPlanMedian[plan] = median(samples)
	}
	currentGlobal := median(currentAll)

	updates := make([]OpenAICapacityHistoryUpdate, 0, len(currentAll))
	for _, input := range inputs {
		switch {
		case input.accepted:
			input.capacity, input.source = input.candidate, "current"
			if openAICapacityHistoryChanged(input.account.Extra, def, input.candidate, input.start, input.plan) {
				update := map[string]any{
					def.historyKey: input.candidate, def.historyUpdated: now.UTC().Format(time.RFC3339), def.historyPlan: input.plan,
				}
				if input.start != nil {
					update[def.historyStart] = input.start.UTC().Format(time.RFC3339)
				}
				updates = append(updates, OpenAICapacityHistoryUpdate{AccountID: input.account.ID, Updates: update})
			}
		case input.sampleCurrent && input.candidate > 0 && !input.historySample:
			// High-usage estimates remain useful for the current response, but do
			// not replace trusted history or pool references. Outliers inside the
			// trusted sample range continue through the fallback chain below.
			input.capacity, input.source = input.candidate, "current"
		case input.history > 0:
			input.capacity, input.source = input.history, "historical"
		case currentPlanMedian[input.plan] > 0:
			input.capacity, input.source = currentPlanMedian[input.plan], "current"
		case positiveFinite(stored.ByPlan[input.plan]) > 0:
			input.capacity, input.source = stored.ByPlan[input.plan], "historical"
		case currentGlobal > 0:
			input.capacity, input.source = currentGlobal, "current"
		case stored.Global != nil && positiveFinite(*stored.Global) > 0:
			input.capacity, input.source = *stored.Global, "historical"
		}
	}

	var summary OpenAIUsageWindowSummary
	usedTotal := decimal.Zero
	capacityTotal := decimal.Zero
	remainingTotal := decimal.Zero
	summary.ReferenceSource = "unavailable"
	sources := map[string]bool{}
	for _, input := range inputs {
		usedTotal = usedTotal.Add(decimal.NewFromFloat(input.used))
		if input.accepted {
			summary.CurrentSampleAccountCount++
		}
		if input.pending {
			summary.PendingSyncAccountCount++
		}
		if input.capacity <= 0 {
			summary.UnestimatedAccountCount++
			continue
		}
		summary.EstimatedAccountCount++
		capacity := math.Max(input.capacity, input.used)
		remaining := math.Max(0, capacity-input.used)
		capacityTotal = capacityTotal.Add(decimal.NewFromFloat(capacity))
		remainingTotal = remainingTotal.Add(decimal.NewFromFloat(remaining))
		sources[input.source] = true
	}
	summary.Used, _ = usedTotal.Float64()
	if capacityTotal.IsPositive() {
		capacity, _ := capacityTotal.Float64()
		remaining, _ := remainingTotal.Float64()
		summary.EstimatedCapacity = openAIFloat64Ptr(capacity)
		summary.EstimatedRemaining = openAIFloat64Ptr(remaining)
		summary.UsagePercent = clampPercent(summary.Used / *summary.EstimatedCapacity * 100)
		summary.RemainingPercent = 100 - summary.UsagePercent
	} else {
		summary.UsagePercent = 0
		summary.RemainingPercent = 100
	}
	switch {
	case sources["current"] && sources["historical"]:
		summary.ReferenceSource = "mixed"
	case sources["current"]:
		summary.ReferenceSource = "current"
	case sources["historical"]:
		summary.ReferenceSource = "historical"
	}
	if currentGlobal > 0 {
		summary.ReferenceCapacity = openAIFloat64Ptr(currentGlobal)
	} else if stored.Global != nil && positiveFinite(*stored.Global) > 0 {
		summary.ReferenceCapacity = openAIFloat64Ptr(*stored.Global)
	}

	if len(currentAll) == 0 {
		return summary, updates, nil
	}
	referenceChanged := stored.Global == nil || !openAIFloatNearlyEqual(positiveFinite(*stored.Global), currentGlobal)
	for plan, value := range currentPlanMedian {
		if !openAIFloatNearlyEqual(positiveFinite(stored.ByPlan[plan]), value) {
			referenceChanged = true
			break
		}
	}
	if !referenceChanged {
		return summary, updates, nil
	}
	ref := stored
	ref.ByPlan = make(map[string]float64, len(stored.ByPlan)+len(currentPlanMedian))
	for plan, value := range stored.ByPlan {
		ref.ByPlan[plan] = value
	}
	ref.Global = openAIFloat64Ptr(currentGlobal)
	for plan, value := range currentPlanMedian {
		ref.ByPlan[plan] = value
	}
	ref.UpdatedAt = now.UTC().Format(time.RFC3339)
	return summary, updates, &ref
}

func mergeOpenAICapacityUpdates(groups ...[]OpenAICapacityHistoryUpdate) []OpenAICapacityHistoryUpdate {
	merged := make(map[int64]map[string]any)
	for _, group := range groups {
		for _, update := range group {
			if merged[update.AccountID] == nil {
				merged[update.AccountID] = map[string]any{}
			}
			for key, value := range update.Updates {
				merged[update.AccountID][key] = value
			}
		}
	}
	result := make([]OpenAICapacityHistoryUpdate, 0, len(merged))
	for id, updates := range merged {
		result = append(result, OpenAICapacityHistoryUpdate{AccountID: id, Updates: updates})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].AccountID < result[j].AccountID })
	return result
}

func normalizeOpenAIPlan(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "free", "plus", "pro", "team", "business", "enterprise", "edu", "self_serve_business_usage_based":
		return value
	default:
		return "unknown"
	}
}

func median(values []float64) float64 {
	filtered := make([]float64, 0, len(values))
	for _, value := range values {
		if value = positiveFinite(value); value > 0 {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		return 0
	}
	sort.Float64s(filtered)
	mid := len(filtered) / 2
	if len(filtered)%2 == 1 {
		return filtered[mid]
	}
	return (filtered[mid-1] + filtered[mid]) / 2
}

func withinCapacityRange(value, reference float64) bool {
	return value > 0 && reference > 0 && value >= reference*openAICapacityOutlierLowerRatio && value <= reference*openAICapacityOutlierUpperRatio
}

func openAICapacityHistoryMatchesPlan(extra map[string]any, def openAIWindowDefinition, plan string) bool {
	if extra == nil {
		return true
	}
	stored, ok := extra[def.historyPlan].(string)
	if !ok || strings.TrimSpace(stored) == "" {
		return true
	}
	return normalizeOpenAIPlan(stored) == plan
}

func openAICapacityHistoryChanged(extra map[string]any, def openAIWindowDefinition, capacity float64, start *time.Time, plan string) bool {
	if !openAIFloatNearlyEqual(positiveFinite(parseExtraFloat64(extra[def.historyKey])), capacity) {
		return true
	}
	storedPlan, _ := extra[def.historyPlan].(string)
	if strings.TrimSpace(storedPlan) == "" || normalizeOpenAIPlan(storedPlan) != plan {
		return true
	}
	storedStart := parseExtraTime(extra[def.historyStart])
	return start == nil || storedStart.IsZero() || !storedStart.Equal(*start)
}

func openAIFloatNearlyEqual(left, right float64) bool {
	if left == right {
		return true
	}
	scale := math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
	return math.Abs(left-right) <= scale*1e-9
}

func clampPercent(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0
	}
	return math.Min(100, value)
}

func positiveFinite(value float64) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func nonNegativeFinite(value float64) float64 {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func openAIFloat64Ptr(value float64) *float64 { return &value }
