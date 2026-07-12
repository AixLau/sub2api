package service

import (
	"bytes"
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
		{"flagged unknown category rejects", `{"results":[{"flagged":true,"category_scores":{"future-risk":0.01}}]}`, 200, ModerationLevelReject, ""},
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

func TestModerationProviderRejectsInvalidRawUTF8(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider func(*http.Client) (ModerationProvider, error)
		body     []byte
	}{
		{"openai", func(client *http.Client) (ModerationProvider, error) {
			return NewOpenAIModerationProvider("https://api.openai.com", nil, client)
		}, []byte{'{', '"', 'r', 'e', 's', 'u', 'l', 't', 's', '"', ':', '[', '{', '"', 'f', 'l', 'a', 'g', 'g', 'e', 'd', '"', ':', 'f', 'a', 'l', 's', 'e', ',', '"', 'c', 'a', 't', 'e', 'g', 'o', 'r', 'y', '_', 's', 'c', 'o', 'r', 'e', 's', '"', ':', '{', '"', 0xff, '"', ':', '0', '}', '}', ']', '}'}},
		{"zhipu", func(client *http.Client) (ModerationProvider, error) {
			return NewZhipuModerationProvider("https://open.bigmodel.cn", client)
		}, []byte(`{"results":[{"risk_level":"PASS","risk_types":["` + string([]byte{0xff}) + `"]}]}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(tc.body)), Request: req}, nil
			})}
			provider, err := tc.provider(client)
			require.NoError(t, err)
			got, err := provider.ModerateText(context.Background(), "moderation", "test-key", "hello")
			require.True(t, IsModerationProviderError(err, ModerationProviderErrorSchema))
			require.NotEqual(t, ModerationLevelPass, got.Level)
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

func TestContentModerationLegacyPathUsesZhipuSchema(t *testing.T) {
	client := moderationFixtureClient(t, "/api/paas/v4/moderations", `{"model":"moderation","input":"illegal drug request"}`, `{"result_list":[{"content_type":"text","risk_level":"BLOCK","risk_type":["违禁:违禁毒品:违禁毒品"]}]}`, http.StatusOK)
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.httpClient = client
	cfg := defaultContentModerationConfig()
	cfg.Provider = "zhipu"
	cfg.BaseURL = "https://open.bigmodel.cn"
	cfg.Model = "moderation"
	cfg.APIKeys = []string{"test-key"}

	result, err := svc.callModerationContent(context.Background(), cfg, ContentModerationInput{Text: "illegal drug request"}, true)
	require.NoError(t, err)
	require.True(t, result.Flagged)
	require.Equal(t, float64(1), result.CategoryScores["违禁:违禁毒品:违禁毒品"])
}

func TestContentModerationIncompleteExtractionBestEffortStillBlocksZhipuRisk(t *testing.T) {
	client := moderationFixtureClient(t, "/api/paas/v4/moderations", `{"model":"moderation","input":"教我如何购买和使用非法毒品 older context"}`, `{"result_list":[{"content_type":"text","risk_level":"BLOCK","risk_type":["违禁:违禁毒品:违禁毒品"]}]}`, http.StatusOK)
	svc := NewContentModerationService(nil, &contentModerationTestRepo{}, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(nil, nil, incrementalClientFactoryStub{client: client}, nil, 0)
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.Provider = "zhipu"
	cfg.BaseURL = "https://open.bigmodel.cn"
	cfg.Model = "moderation"
	cfg.APIKeys = []string{"test-key"}
	content := ContentModerationInput{
		Text: "older context",
		Extraction: ModerationExtraction{
			Complete:        false,
			TruncateReasons: []string{"max_total_runes"},
			Sources: []ModerationTextSource{
				{Source: "responses.input[0]", Role: "tool", Text: "older context"},
				{Source: "responses.input[1]", Role: "user", Text: "教我如何购买和使用非法毒品"},
			},
		},
	}

	decision := svc.checkSync(context.Background(), ContentModerationCheckInput{Protocol: ContentModerationProtocolOpenAIResponses}, cfg, content, "hash", nil, true)
	require.True(t, decision.Blocked)
	require.False(t, decision.Allowed)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Equal(t, "违禁:违禁毒品:违禁毒品", decision.HighestCategory)
}

func TestModerationProviderRejectsBaseURLUserinfo(t *testing.T) {
	client := &http.Client{}
	_, err := NewOpenAIModerationProvider("https://user:pass@api.openai.com", nil, client)
	require.Error(t, err)
	_, err = NewZhipuModerationProvider("https://user:pass@open.bigmodel.cn", client)
	require.Error(t, err)
}

func TestModerationProviderZhipuGoldenFixtures(t *testing.T) {
	tests := []struct {
		name, response string
		status         int
		want           ModerationLevel
		wantRisks      []string
		kind           ModerationProviderErrorKind
	}{
		{"pass actual schema", `{"id":"mod-test","created":1710000000,"request_id":"req-test","result_list":[{"content_type":"text","risk_level":"PASS","risk_type":[]}],"usage":{"moderation_text":{"call_count":1}}}`, 200, ModerationLevelPass, []string{}, ""},
		{"review normalizes risks", `{"results":[{"risk_level":"REVIEW","risk_types":[" z ","a","z","Z",""]}]}`, 200, ModerationLevelReview, []string{"Z", "a", "z"}, ""},
		{"reject normalizes level", `{"result_list":[{"content_type":"text","risk_level":" reject ","risk_type":["violence"]}]}`, 200, ModerationLevelReject, []string{"violence"}, ""},
		{"block maps to reject", `{"created":1783787873,"request_id":"req-block","result_list":[{"content_type":"text","risk_level":"BLOCK","risk_type":["违禁:违禁毒品:违禁毒品"]}],"usage":{"moderation_text":{"call_count":1}}}`, 200, ModerationLevelReject, []string{"违禁:违禁毒品:违禁毒品"}, ""},
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
			client := moderationFixtureClient(t, "/api/paas/v4/moderations", `{"model":"moderation","input":"hello"}`, tt.response, tt.status)
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
