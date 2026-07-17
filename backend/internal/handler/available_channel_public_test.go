package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildPublicModelCatalog_PublicContract(t *testing.T) {
	inputPrice := 0.000003
	outputPrice := 0.000015
	cacheWritePrice := 0.0
	cacheReadPrice := 0.0000003
	channels := []service.AvailableChannel{
		{
			Status: service.StatusActive,
			Groups: []service.AvailableGroupRef{{Platform: "anthropic"}},
			SupportedModels: []service.SupportedModel{{
				Name:     "claude-sonnet-5",
				Platform: "anthropic",
				Pricing: &service.ChannelModelPricing{
					BillingMode:     service.BillingModeToken,
					InputPrice:      &inputPrice,
					OutputPrice:     &outputPrice,
					CacheWritePrice: &cacheWritePrice,
					CacheReadPrice:  &cacheReadPrice,
				},
			}},
		},
		{
			Status:          service.StatusActive,
			Groups:          []service.AvailableGroupRef{{Platform: "openai", IsExclusive: true}},
			SupportedModels: []service.SupportedModel{{Name: "gpt-private", Platform: "openai"}},
		},
	}

	models := buildPublicModelCatalog(channels)
	require.Len(t, models, 1)
	require.Equal(t, "claude-sonnet-5", models[0].Name)
	require.Equal(t, "anthropic", models[0].Platform)
	require.Equal(t, inputPrice, *models[0].Pricing.InputPrice)
	require.Equal(t, outputPrice, *models[0].Pricing.OutputPrice)
	require.NotNil(t, models[0].Pricing.CacheWritePrice)
	require.Zero(t, *models[0].Pricing.CacheWritePrice)
	require.Equal(t, cacheReadPrice, *models[0].Pricing.CacheReadPrice)
}
