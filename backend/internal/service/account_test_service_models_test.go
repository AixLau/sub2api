package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchOpenAIAccountModelsOAuthProvidesDisplayNames(t *testing.T) {
	_, calls := newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"gpt-6-astra","display_name":"GPT-6 Astra"},{"slug":"custom-live-model"}]}`)
	gateway := &OpenAIGatewayService{}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := newCodexModelsTestAccount()

	models, err := svc.FetchOpenAIAccountModels(context.Background(), account)
	require.NoError(t, err)
	// The OAuth picker also appends locally supported image choices. Assert the
	// upstream models and their display-name normalization without depending on
	// the number of locally supported image models.
	require.GreaterOrEqual(t, len(models), 2)
	require.Equal(t, "gpt-6-astra", models[0].ID)
	require.Equal(t, "gpt-6-astra", models[0].DisplayName)
	require.Equal(t, "custom-live-model", models[1].ID)
	require.Equal(t, "custom-live-model", models[1].DisplayName)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	require.Contains(t, ids, "gpt-image-2.5-flare")
	require.Contains(t, ids, "gpt-image-2.5-sunburst")

	// Admin display labels must not alter the shared standard model catalog.
	response, err := gateway.FetchOpenAIModelsList(context.Background(), account)
	require.NoError(t, err)
	require.NotContains(t, string(response.Body), "display_name")
	require.EqualValues(t, 1, calls.Load())
}

func TestFetchOpenAIAccountModelsAPIKeyNormalizesMissingDisplayNames(t *testing.T) {
	gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{
		do: func(*http.Request, string, int64, int) (*http.Response, error) {
			return ordinaryModelsUpstreamResponse(`{"data":[{"id":"id-only"},{"id":"empty-name","display_name":""},{"id":"blank-name","display_name":"  "},{"id":"custom-name","display_name":"My Custom Model"}]}`), nil
		},
	})
	svc := &AccountTestService{openaiGatewayService: gateway}
	models, err := svc.FetchOpenAIAccountModels(context.Background(), newCodexModelsAPIKeyTestAccount("https://models.example/v1"))
	require.NoError(t, err)
	require.Len(t, models, 4)
	for i, want := range []string{"id-only", "empty-name", "blank-name", "My Custom Model"} {
		require.Equal(t, want, models[i].DisplayName)
	}
}
