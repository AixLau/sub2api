package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type moderationRoundTripFunc func(*http.Request) (*http.Response, error)

func (f moderationRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func moderationFixtureClient(t *testing.T, wantPath, wantBody, response string, status int) *http.Client {
	t.Helper()
	return &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, wantPath, req.URL.Path)
		require.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
		require.Equal(t, "application/json", req.Header.Get("Content-Type"))
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, wantBody, string(body))
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	})}
}

func TestModerationProviderOpenAIGoldenFixtures(t *testing.T) {
	thresholds := map[string]float64{"hate": .65, "violence": .95}
	tests := []struct {
		name, response string
		status         int
		want           ModerationLevel
		kind           ModerationProviderErrorKind
	}{
		{"pass", `{"results":[{"flagged":false,"category_scores":{"hate":0.64,"violence":0.1}}]}`, 200, ModerationLevelPass, ""},
		{"flagged always rejects", `{"results":[{"flagged":true,"category_scores":{"hate":0.01}}]}`, 200, ModerationLevelReject, ""},
		{"threshold hit rejects", `{"results":[{"flagged":false,"category_scores":{"hate":0.65}}]}`, 200, ModerationLevelReject, ""},
		{"empty scores", `{"results":[{"flagged":false,"category_scores":{}}]}`, 200, "", ModerationProviderErrorSchema},
		{"missing scores", `{"results":[{"flagged":false}]}`, 200, "", ModerationProviderErrorSchema},
		{"missing flagged", `{"results":[{"category_scores":{"hate":0}}]}`, 200, "", ModerationProviderErrorSchema},
		{"unknown category", `{"results":[{"flagged":false,"category_scores":{"future-risk":0}}]}`, 200, "", ModerationProviderErrorSchema},
		{"multiple results", `{"results":[{"flagged":false,"category_scores":{"hate":0}},{"flagged":false,"category_scores":{"hate":0}}]}`, 200, "", ModerationProviderErrorSchema},
		{"zero results", `{"results":[]}`, 200, "", ModerationProviderErrorSchema},
		{"unknown schema", `{"results":[{"flagged":false,"category_scores":{"hate":0},"future":true}]}`, 200, "", ModerationProviderErrorSchema},
		{"auth", `{}`, 401, "", ModerationProviderErrorAuth},
		{"rate limit", `{}`, 429, "", ModerationProviderErrorRateLimit},
		{"http", `{}`, 500, "", ModerationProviderErrorHTTP},
		{"malformed", `{`, 200, "", ModerationProviderErrorSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := moderationFixtureClient(t, "/v1/moderations", `{"model":"omni-moderation-latest","input":"hello"}`, tt.response, tt.status)
			provider, err := NewOpenAIModerationProvider("https://api.openai.com/api-ignored", thresholds, client)
			require.NoError(t, err)
			require.Equal(t, "openai", provider.Name())
			require.Equal(t, "openai-v1", provider.AdapterVersion())
			got, err := provider.ModerateText(context.Background(), "omni-moderation-latest", "test-key", "hello")
			if tt.kind != "" {
				require.Error(t, err)
				require.True(t, IsModerationProviderError(err, tt.kind))
				require.NotEqual(t, ModerationLevelPass, got.Level)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Level)
		})
	}
}

func TestModerationProviderOpenAITimeout(t *testing.T) {
	client := &http.Client{Transport: moderationRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	provider, err := NewOpenAIModerationProvider("https://api.openai.com", map[string]float64{"hate": .5}, client)
	require.NoError(t, err)
	_, err = provider.ModerateText(context.Background(), "model", "test-key", "hello")
	require.True(t, IsModerationProviderError(err, ModerationProviderErrorTimeout))
}

func TestModerationProviderZhipuGoldenFixtures(t *testing.T) {
	tests := []struct {
		name, response string
		status         int
		want           ModerationLevel
		wantRisks      []string
		kind           ModerationProviderErrorKind
	}{
		{"pass", `{"results":[{"risk_level":"PASS","risk_types":[]}]}`, 200, ModerationLevelPass, []string{}, ""},
		{"review normalizes risks", `{"results":[{"risk_level":"REVIEW","risk_types":[" z ","a","z","Z",""]}]}`, 200, ModerationLevelReview, []string{"Z", "a", "z"}, ""},
		{"reject", `{"results":[{"risk_level":"REJECT","risk_types":["违法"]}]}`, 200, ModerationLevelReject, []string{"违法"}, ""},
		{"non string", `{"results":[{"risk_level":"REJECT","risk_types":[1]}]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"unknown level", `{"results":[{"risk_level":"LOW","risk_types":[]}]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"missing risk types", `{"results":[{"risk_level":"PASS"}]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"unknown schema", `{"results":[{"risk_level":"PASS","risk_types":[],"future":1}]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"zero results", `{"results":[]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"multiple results", `{"results":[{"risk_level":"PASS","risk_types":[]},{"risk_level":"PASS","risk_types":[]}]}`, 200, "", nil, ModerationProviderErrorSchema},
		{"auth", `{}`, 403, "", nil, ModerationProviderErrorAuth},
		{"rate limit", `{}`, 429, "", nil, ModerationProviderErrorRateLimit},
		{"http", `{}`, 500, "", nil, ModerationProviderErrorHTTP},
		{"malformed", `{`, 200, "", nil, ModerationProviderErrorSchema},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := moderationFixtureClient(t, "/paas/v4/moderations", `{"model":"moderation","input":"hello"}`, tt.response, tt.status)
			provider, err := NewZhipuModerationProvider("https://open.bigmodel.cn/api", client)
			require.NoError(t, err)
			require.Equal(t, "zhipu", provider.Name())
			require.Equal(t, "zhipu-v1", provider.AdapterVersion())
			got, err := provider.ModerateText(context.Background(), "moderation", "test-key", "hello")
			if tt.kind != "" {
				require.Error(t, err)
				require.True(t, IsModerationProviderError(err, tt.kind))
				require.NotEqual(t, ModerationLevelPass, got.Level)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.Level)
			require.Equal(t, tt.wantRisks, got.RiskTypes)
		})
	}
}

func TestModerationProviderZhipuRiskBounds(t *testing.T) {
	risk32 := make([]string, 32)
	for i := range risk32 {
		risk32[i] = fmt.Sprintf("%02d%s", i, strings.Repeat("界", 62))
	}
	got, err := normalizeZhipuRiskTypes(risk32)
	require.NoError(t, err)
	require.Len(t, got, 32)
	_, err = normalizeZhipuRiskTypes(append(risk32, "overflow"))
	require.True(t, IsModerationProviderError(err, ModerationProviderErrorSchema))
	_, err = normalizeZhipuRiskTypes([]string{strings.Repeat("界", 65)})
	require.True(t, IsModerationProviderError(err, ModerationProviderErrorSchema))
}

func TestModerationProviderZhipuTimeout(t *testing.T) {
	client := &http.Client{Timeout: time.Millisecond, Transport: moderationRoundTripFunc(func(*http.Request) (*http.Response, error) { return nil, context.DeadlineExceeded })}
	provider, err := NewZhipuModerationProvider("https://open.bigmodel.cn", client)
	require.NoError(t, err)
	_, err = provider.ModerateText(context.Background(), "moderation", "test-key", "hello")
	require.True(t, IsModerationProviderError(err, ModerationProviderErrorTimeout))
	require.True(t, errors.Is(err, context.DeadlineExceeded))
}
