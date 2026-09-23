package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationTypeSafeEngineIsSelectedByProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/systemone", r.URL.Path)
		var request struct {
			Questions map[string]json.RawMessage `json:"questions"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		answers := make(map[string]any, len(request.Questions))
		for category := range request.Questions {
			answers[category] = map[string]any{"type": "noul", "noul": 0.01}
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"model":   "jev-test",
			"answers": answers,
		}))
	}))
	defer server.Close()

	service := &ContentModerationService{httpClient: server.Client()}
	result, err := service.callModerationOnceWithInput(context.Background(), &ContentModerationConfig{
		Provider:   contentModerationProviderTypeSafe,
		BaseURL:    server.URL,
		Model:      "jev-test",
		TimeoutMS:  1000,
		Thresholds: ContentModerationDefaultThresholds(),
	}, "test-key", []moderationAPIInputPart{{Type: "text", Text: "hello"}}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Flagged)
	require.Len(t, result.CategoryScores, len(typeSafeModerationQuestions))
}
