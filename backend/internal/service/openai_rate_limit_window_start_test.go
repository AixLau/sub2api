package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestSendOpenAIRateLimitWindowStart_UsesCheapestModelThenFallsBackOnce(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{}
	repo.accounts = []Account{newWindowStartReadyAccount(123)}
	starter := &accountUsageWindowStarter{
		models: []OpenAIWindowStartModel{
			{Requested: "cheap", Upstream: "cheap-upstream", CostPerToken: 1e-7},
			{Requested: "mid", Upstream: "mid-upstream", CostPerToken: 1e-6},
			{Requested: "pricey", Upstream: "pricey-upstream", CostPerToken: 1e-5},
		},
		results: []*ScheduledTestResult{{Status: "failed"}, {Status: "failed"}, {Status: "success"}},
	}
	svc := &AccountUsageService{accountRepo: repo, openAIWindowStarter: starter}

	now := time.Now()
	svc.sendOpenAIRateLimitWindowStart(123, now, now)

	if got, want := starter.modelIDs, []string{"cheap", "mid"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("models tried = %v, want %v (cheapest first, at most %d attempts)", got, want, openAIWindowStartMaxAttempts)
	}
	if got := starter.failedModels; len(got) != 2 || got[0] != "cheap-upstream" || got[1] != "mid-upstream" {
		t.Fatalf("recorded failures = %v, want both rejected upstream models", got)
	}
	for i, marked := range starter.windowStartProbe {
		if !marked {
			t.Fatalf("attempt %d missing window-start context marker", i+1)
		}
	}
}

func TestSendOpenAIRateLimitWindowStart_StopsAfterSuccess(t *testing.T) {
	t.Parallel()

	repo := &accountUsageCodexProbeRepo{}
	repo.accounts = []Account{newWindowStartReadyAccount(123)}
	starter := &accountUsageWindowStarter{
		models: []OpenAIWindowStartModel{
			{Requested: "cheap", Upstream: "cheap", CostPerToken: 1e-7},
			{Requested: "mid", Upstream: "mid", CostPerToken: 1e-6},
		},
		result: &ScheduledTestResult{Status: "success"},
	}
	svc := &AccountUsageService{accountRepo: repo, openAIWindowStarter: starter}

	now := time.Now()
	svc.sendOpenAIRateLimitWindowStart(123, now, now)

	if len(starter.modelIDs) != 1 || starter.modelIDs[0] != "cheap" {
		t.Fatalf("models tried = %v, want only the cheapest", starter.modelIDs)
	}
	if len(starter.failedModels) != 0 {
		t.Fatalf("a successful start must not record failures, got %v", starter.failedModels)
	}
}

func TestSendOpenAIRateLimitWindowStart_SkipsWhenRequestWouldBeWasted(t *testing.T) {
	t.Parallel()

	now := time.Now()
	future := now.Add(time.Hour)
	usedAfter := now.Add(time.Second)
	usedBefore := now.Add(-time.Second)
	tests := []struct {
		name    string
		mutate  func(*Account)
		models  []OpenAIWindowStartModel
		wantRun bool
	}{
		{name: "ready", mutate: func(*Account) {}, wantRun: true},
		{name: "traffic before cooldown end", mutate: func(a *Account) { a.LastUsedAt = &usedBefore }, wantRun: true},
		{name: "traffic already opened window", mutate: func(a *Account) { a.LastUsedAt = &usedAfter }},
		{name: "back in cooldown", mutate: func(a *Account) { a.RateLimitResetAt = &future }},
		{name: "unschedulable", mutate: func(a *Account) { a.Schedulable = false }},
		{name: "disabled", mutate: func(a *Account) { a.Status = StatusDisabled }},
		{name: "shadow", mutate: func(a *Account) { parent := int64(1); a.ParentAccountID = &parent }},
		{name: "no usable model", mutate: func(*Account) {}, models: []OpenAIWindowStartModel{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := newWindowStartReadyAccount(123)
			tt.mutate(&account)
			repo := &accountUsageCodexProbeRepo{}
			repo.accounts = []Account{account}
			starter := &accountUsageWindowStarter{models: tt.models, result: &ScheduledTestResult{Status: "success"}}
			svc := &AccountUsageService{accountRepo: repo, openAIWindowStarter: starter}

			svc.sendOpenAIRateLimitWindowStart(123, now, now)

			if ran := len(starter.accountIDs) > 0; ran != tt.wantRun {
				t.Fatalf("window-start request sent = %v, want %v", ran, tt.wantRun)
			}
		})
	}
}

func TestOpenAIWindowStartModels_CheapestUsableTextModelsFirst(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{billingService: NewBillingService(&config.Config{}, nil)}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	models := svc.OpenAIWindowStartModels(t.Context(), account)
	if len(models) == 0 {
		t.Fatal("expected priced OpenAI text models to be candidates")
	}
	for i, m := range models {
		if !isOpenAIWindowStartTextModel(m.Upstream) {
			t.Fatalf("candidate %q must be a non-image, non-Spark text model", m.Upstream)
		}
		if m.CostPerToken <= 0 {
			t.Fatalf("candidate %q has non-positive cost %v", m.Upstream, m.CostPerToken)
		}
		if i > 0 && models[i-1].CostPerToken > m.CostPerToken {
			t.Fatalf("candidates not sorted by cost: %q (%v) before %q (%v)", models[i-1].Upstream, models[i-1].CostPerToken, m.Upstream, m.CostPerToken)
		}
	}

	cheapest := models[0].Upstream
	svc.RecordOpenAIWindowStartModelFailure(account.ID, cheapest)
	for _, m := range svc.OpenAIWindowStartModels(t.Context(), account) {
		if m.Upstream == cheapest {
			t.Fatalf("model %q failed recently and must be skipped", cheapest)
		}
	}
	other := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	if got := svc.OpenAIWindowStartModels(t.Context(), other); len(got) == 0 || got[0].Upstream != cheapest {
		t.Fatalf("a failure on one account must not affect another, got %v", got)
	}

	resetAt := time.Now().Add(time.Hour).Format(time.RFC3339)
	limited := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		modelRateLimitsKey: map[string]any{cheapest: map[string]any{"rate_limit_reset_at": resetAt}},
	}}
	for _, m := range svc.OpenAIWindowStartModels(t.Context(), limited) {
		if m.Upstream == cheapest {
			t.Fatalf("model-rate-limited %q must be skipped", cheapest)
		}
	}
}

func TestOpenAIWindowStartModels_NoPricingMeansNoCandidates(t *testing.T) {
	t.Parallel()

	svc := &AccountTestService{}
	account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	if got := svc.OpenAIWindowStartModels(t.Context(), account); len(got) != 0 {
		t.Fatalf("without pricing no model can be ranked, got %v", got)
	}
}
