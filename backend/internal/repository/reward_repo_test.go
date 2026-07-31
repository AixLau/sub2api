package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestChooseAffordableRewardTierFiltersUnaffordableAmounts(t *testing.T) {
	t.Parallel()

	tiers := []service.RewardAmountTier{
		{Amount: 1, Weight: 1},
		{Amount: 3, Weight: 1_000_000},
		{Amount: 5, Weight: 1_000_000},
	}
	for attempt := 0; attempt < 100; attempt++ {
		tier, ok := chooseAffordableRewardTier(tiers, 2.99)
		if !ok {
			t.Fatal("chooseAffordableRewardTier() returned no tier with an affordable option")
		}
		if tier.Amount != 1 {
			t.Fatalf("chooseAffordableRewardTier() selected %v with only 2.99 available", tier.Amount)
		}
	}

	if _, ok := chooseAffordableRewardTier(tiers, 0.99); ok {
		t.Fatal("chooseAffordableRewardTier() selected a tier above the available budget")
	}
}

func TestSecureWeightedIndexIgnoresNonPositiveWeights(t *testing.T) {
	t.Parallel()

	items := []service.RewardAmountTier{
		{Amount: 1, Weight: 0},
		{Amount: 2, Weight: -1},
		{Amount: 3, Weight: 5},
	}
	for attempt := 0; attempt < 100; attempt++ {
		index, ok := secureWeightedIndex(items, func(tier service.RewardAmountTier) int {
			return tier.Weight
		})
		if !ok {
			t.Fatal("secureWeightedIndex() returned no index with one positive weight")
		}
		if index != 2 {
			t.Fatalf("secureWeightedIndex() = %d, want the only positively weighted index 2", index)
		}
	}
}
