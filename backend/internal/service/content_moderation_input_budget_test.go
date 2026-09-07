package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSemanticReviewInputBudgetHasNoConfiguredMaximum(t *testing.T) {
	const budget = 100_000
	cfg := normalizeContentModerationSemanticReviewConfig(ContentModerationSemanticReviewConfig{MaxInputRunes: budget})
	require.Equal(t, budget, cfg.MaxInputRunes)

	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			text := strings.Repeat("界", budget-4) + "tail"
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_budget","output_text":"{\"verdict\":\"allow\"}"}`)),
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{}}
			account := &Account{ID: 52, Platform: PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{"api_key": "test-key", "access_token": "test-token"}}
			_, err := svc.ReviewSemanticContent(context.Background(), account, "review-model", ContentModerationSemanticReviewInput{
				Text: text, MaxInputRunes: cfg.MaxInputRunes,
			})
			require.NoError(t, err)
			var body struct {
				Input []struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"input"`
			}
			require.NoError(t, json.Unmarshal(upstream.lastBody, &body))
			require.Equal(t, text, body.Input[0].Content[0].Text)
		})
	}
}
