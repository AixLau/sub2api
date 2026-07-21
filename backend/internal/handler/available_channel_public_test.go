package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildSystemPublicModelCatalog_PublicContract(t *testing.T) {
	pricing := &publicPricingStub{byModel: map[string]*service.LiteLLMModelPricing{
		"gpt-5.5": {
			InputCostPerToken:       0.000005,
			OutputCostPerToken:      0.00003,
			CacheReadInputTokenCost: 0.0000005,
			SupportsPromptCaching:   true,
		},
	}}
	sets := []service.SystemAvailableModelSet{
		{Platform: "openai", Models: []string{"gpt-5.5", "unknown-model", "gpt-5.5"}},
	}

	models := buildSystemPublicModelCatalog(sets, pricing)
	require.Len(t, models, 2)
	require.Equal(t, "gpt-5.5", models[0].Name)
	require.Equal(t, "openai", models[0].Platform)
	require.Equal(t, 0.000005, *models[0].Pricing.InputPrice)
	require.Equal(t, 0.00003, *models[0].Pricing.OutputPrice)
	require.NotNil(t, models[0].Pricing.CacheWritePrice)
	require.Zero(t, *models[0].Pricing.CacheWritePrice)
	require.Equal(t, 0.0000005, *models[0].Pricing.CacheReadPrice)
	require.Nil(t, models[1].Pricing)
}

type publicPricingStub struct {
	byModel map[string]*service.LiteLLMModelPricing
}

func (s *publicPricingStub) GetModelPricing(modelName string) *service.LiteLLMModelPricing {
	return s.byModel[modelName]
}
