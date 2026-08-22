package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIListSemanticReviewModelsUsesAccountSettings(t *testing.T) {
	parentID := int64(10)
	repo := stubOpenAIAccountRepo{accounts: []Account{
		{
			ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"custom-review-model": "custom-review-model",
				"gpt-5.5":             "gpt-5.5",
			}},
		},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, ParentAccountID: &parentID,
			Credentials: map[string]any{"model_mapping": map[string]any{"shadow-only": "shadow-only"}}},
		{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"foreign-model": "foreign-model"}}},
	}}

	models, err := (&OpenAIGatewayService{accountRepo: repo}).ListSemanticReviewModels(context.Background())
	require.NoError(t, err)
	require.Contains(t, models, "custom-review-model")
	require.Contains(t, models, "gpt-5.5")
	require.Contains(t, models, "gpt-5.3-codex-spark")
	require.NotContains(t, models, "shadow-only")
	require.NotContains(t, models, "foreign-model")
	require.NotContains(t, models, "gpt-image-1")
}
