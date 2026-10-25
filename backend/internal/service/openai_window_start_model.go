package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// openAIWindowStartModelFailureTTL keeps a model the upstream rejected for an
// account out of the window-start selection for a while, so later activations
// do not spend a request on a model that is known not to work there.
const openAIWindowStartModelFailureTTL = 24 * time.Hour

// openAIWindowStartExcludedModels are text models whose quota attribution to
// the account's main Codex window is not established; using them could leave
// the window inactive even though the request succeeds.
var openAIWindowStartExcludedModels = map[string]struct{}{
	openai.CodexUsageProbeModel: {},
}

// OpenAIWindowStartModel is a window-start candidate: Requested is passed to
// the account test (which applies the account model mapping), Upstream is the
// mapped model that is priced and actually sent.
type OpenAIWindowStartModel struct {
	Requested    string
	Upstream     string
	CostPerToken float64
}

// OpenAIWindowStartModels lists the text models this account can serve right
// now, cheapest first. A model is skipped when the account does not support
// it, it is currently model-rate-limited, it recently failed a window start
// here, or it has no positive token price. Image and Codex Spark models are
// never candidates: Spark draws on its own quota and would not open the
// account's main window.
func (s *AccountTestService) OpenAIWindowStartModels(ctx context.Context, account *Account) []OpenAIWindowStartModel {
	if s == nil || account == nil {
		return nil
	}
	now := time.Now()
	seen := make(map[string]struct{})
	var candidates []OpenAIWindowStartModel
	for _, requested := range openai.DefaultModelIDs() {
		if !isOpenAIWindowStartTextModel(requested) || !account.IsModelSupported(requested) {
			continue
		}
		upstream := account.GetMappedModel(requested)
		if !isOpenAIWindowStartTextModel(upstream) {
			continue
		}
		if _, dup := seen[upstream]; dup {
			continue
		}
		if account.isModelRateLimitedWithContext(ctx, requested) || s.openAIWindowStartModelFailed(account.ID, upstream, now) {
			continue
		}
		cost, ok := s.openAIWindowStartModelCost(upstream)
		if !ok {
			continue
		}
		seen[upstream] = struct{}{}
		candidates = append(candidates, OpenAIWindowStartModel{Requested: requested, Upstream: upstream, CostPerToken: cost})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].CostPerToken < candidates[j].CostPerToken
	})
	return candidates
}

// RecordOpenAIWindowStartModelFailure excludes a model from window-start
// selection for this account until openAIWindowStartModelFailureTTL passes.
func (s *AccountTestService) RecordOpenAIWindowStartModelFailure(accountID int64, upstreamModel string) {
	if s == nil || accountID <= 0 || upstreamModel == "" {
		return
	}
	s.openAIWindowStartModelFailures.Store(openAIWindowStartModelFailureKey(accountID, upstreamModel), time.Now())
}

func (s *AccountTestService) openAIWindowStartModelFailed(accountID int64, upstreamModel string, now time.Time) bool {
	key := openAIWindowStartModelFailureKey(accountID, upstreamModel)
	raw, ok := s.openAIWindowStartModelFailures.Load(key)
	if !ok {
		return false
	}
	failedAt, ok := raw.(time.Time)
	if !ok || now.Sub(failedAt) >= openAIWindowStartModelFailureTTL {
		s.openAIWindowStartModelFailures.Delete(key)
		return false
	}
	return true
}

// openAIWindowStartModelCost ranks models by input+output token price; the
// window-start payload is a few tokens each way, so both sides matter.
func (s *AccountTestService) openAIWindowStartModelCost(upstreamModel string) (float64, bool) {
	if s.billingService == nil {
		return 0, false
	}
	pricing, err := s.billingService.GetModelPricing(upstreamModel)
	if err != nil || pricing == nil {
		return 0, false
	}
	cost := pricing.InputPricePerToken + pricing.OutputPricePerToken
	return cost, cost > 0
}

func isOpenAIWindowStartTextModel(model string) bool {
	if model == "" || isOpenAIImageModel(model) || isCodexSparkModel(model) {
		return false
	}
	_, excluded := openAIWindowStartExcludedModels[model]
	return !excluded
}

func openAIWindowStartModelFailureKey(accountID int64, upstreamModel string) string {
	return fmt.Sprintf("%d|%s", accountID, upstreamModel)
}
