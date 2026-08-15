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

	models := buildSystemPublicModelCatalog(sets, pricing, nil)
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

func TestBuildSystemPublicModelCatalog_CustomPricingOverridesPreset(t *testing.T) {
	customInput := 0.000009
	system := &publicPricingStub{byModel: map[string]*service.LiteLLMModelPricing{
		"gpt-5.5": {
			InputCostPerToken:       0.000005,
			OutputCostPerToken:      0.00003,
			CacheReadInputTokenCost: 0.0000005,
		},
	}}
	sets := []service.SystemAvailableModelSet{{Platform: "openai", Models: []string{"gpt-5.5"}}}
	custom := map[publicModelKey]*service.ChannelModelPricing{
		newPublicModelKey("openai", "gpt-5.5"): {
			BillingMode: service.BillingModeToken,
			InputPrice:  &customInput,
		},
	}

	models := buildSystemPublicModelCatalog(sets, system, custom)
	require.Len(t, models, 1)
	require.Equal(t, customInput, *models[0].Pricing.InputPrice, "custom field must win")
	require.Equal(t, 0.00003, *models[0].Pricing.OutputPrice, "missing custom field must use preset")
	require.Equal(t, 0.0000005, *models[0].Pricing.CacheReadPrice)
}

func TestMergePublicModelPricing_ExplicitZeroCustomPriceWins(t *testing.T) {
	systemInput := 0.000005
	free := 0.0

	merged := mergePublicModelPricing(
		&userSupportedModelPricing{
			BillingMode: string(service.BillingModeToken),
			InputPrice:  &systemInput,
		},
		&userSupportedModelPricing{
			BillingMode: string(service.BillingModeToken),
			InputPrice:  &free,
		},
	)

	require.NotNil(t, merged.InputPrice)
	require.Zero(t, *merged.InputPrice)
}

func TestCollectPublicModelPricing_UsesActivePublicChannel(t *testing.T) {
	privatePrice := 0.000001
	inactivePrice := 0.000002
	publicPrice := 0.000003
	channels := []service.AvailableChannel{
		{
			Name:   "a-private",
			Status: service.StatusActive,
			Groups: []service.AvailableGroupRef{{Platform: "openai", IsExclusive: true}},
			SupportedModels: []service.SupportedModel{{
				Name: "gpt-5.5", Platform: "openai",
				Pricing: &service.ChannelModelPricing{InputPrice: &privatePrice},
			}},
		},
		{
			Name:   "b-inactive",
			Status: "inactive",
			Groups: []service.AvailableGroupRef{{Platform: "openai"}},
			SupportedModels: []service.SupportedModel{{
				Name: "gpt-5.5", Platform: "openai",
				Pricing: &service.ChannelModelPricing{InputPrice: &inactivePrice},
			}},
		},
		{
			Name:   "c-public",
			Status: service.StatusActive,
			Groups: []service.AvailableGroupRef{{Platform: service.PlatformComposite}},
			SupportedModels: []service.SupportedModel{{
				Name: "gpt-5.5", Platform: "openai",
				Pricing: &service.ChannelModelPricing{InputPrice: &publicPrice},
			}},
		},
	}

	pricing := collectPublicModelPricing(channels)
	got := pricing[newPublicModelKey("OPENAI", "GPT-5.5")]
	require.NotNil(t, got)
	require.Equal(t, publicPrice, *got.InputPrice)
}

type publicPricingStub struct {
	byModel map[string]*service.LiteLLMModelPricing
}

func (s *publicPricingStub) GetModelPricing(modelName string) *service.LiteLLMModelPricing {
	return s.byModel[modelName]
}
