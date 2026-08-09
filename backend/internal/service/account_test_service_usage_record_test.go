package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountTestUsageWriterStub struct {
	log *UsageLog
	err error
}

func (s *accountTestUsageWriterStub) Create(_ context.Context, log *UsageLog) (bool, error) {
	if log != nil {
		copy := *log
		s.log = &copy
	}
	return s.err == nil, s.err
}

func newOpenAIAccountTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/42/test", nil)
	return c, recorder
}

func newOpenAIAccountTestServiceForRecording(upstream *httpUpstreamRecorder, writer *accountTestUsageWriterStub) *AccountTestService {
	billing := NewBillingService(&config.Config{}, nil)
	return &AccountTestService{
		httpUpstream:    upstream,
		settingService:  newOpenAICodexUASettingService("codex_vscode/1.0"),
		usageLogWriter:  writer,
		billingService:  billing,
		pricingResolver: NewModelPricingResolver(nil, billing),
		cfg:             &config.Config{},
	}
}

func TestAccountTestService_OpenAISuccessForcesSystemIdentityAndRecordsModelCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test_42\",\"usage\":{\"input_tokens\":120,\"output_tokens\":30,\"input_tokens_details\":{\"cached_tokens\":20}}}}\n\n",
		)),
	}}
	usageWriter := &accountTestUsageWriterStub{}
	billing := NewBillingService(&config.Config{}, nil)
	svc := &AccountTestService{
		httpUpstream:    upstream,
		settingService:  newOpenAICodexUASettingService("codex_vscode/1.0"),
		usageLogWriter:  usageWriter,
		billingService:  billing,
		pricingResolver: NewModelPricingResolver(nil, billing),
	}
	account := &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-42",
			"user_agent":   "codex-tui/9.9",
		},
	}

	err := svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "", AccountTestModeDefault)
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "codex_vscode/"+codexCLIVersion, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "codex_vscode", upstream.lastReq.Header.Get("Originator"))

	require.NotNil(t, usageWriter.log)
	require.Equal(t, UsageSourceAccountTest, usageWriter.log.Source)
	require.Zero(t, usageWriter.log.UserID)
	require.Zero(t, usageWriter.log.APIKeyID)
	require.Equal(t, int64(42), usageWriter.log.AccountID)
	require.Equal(t, "resp_test_42", usageWriter.log.RequestID)
	require.Equal(t, "gpt-5.4", usageWriter.log.Model)
	require.Equal(t, 100, usageWriter.log.InputTokens)
	require.Equal(t, 30, usageWriter.log.OutputTokens)
	require.Equal(t, 20, usageWriter.log.CacheReadTokens)
	require.Equal(t, 1.0, usageWriter.log.RateMultiplier)
	require.Positive(t, usageWriter.log.TotalCost)
	require.Zero(t, usageWriter.log.ActualCost)
	require.Equal(t, RequestTypeStream, usageWriter.log.RequestType)
	require.NotNil(t, usageWriter.log.UserAgent)
	require.Equal(t, "codex_vscode/"+codexCLIVersion, *usageWriter.log.UserAgent)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestService_OpenAIChatCompletionsPreservesAPIKeyUAOverrideAndRecordsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			"data: {\"id\":\"chatcmpl_42\",\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":3}}\n\n" +
				"data: [DONE]\n\n",
		)),
	}}
	writer := &accountTestUsageWriterStub{}
	svc := newOpenAIAccountTestServiceForRecording(upstream, writer)
	account := &Account{
		ID:       44,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                 "key-44",
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"User-Agent": "account-override/9.9",
			},
		},
	}

	err := svc.testOpenAIChatCompletionsConnection(c, account, "gpt-5.4", "hi", "https://api.openai.com", "key-44")
	require.NoError(t, err)
	require.Equal(t, "account-override/9.9", upstream.lastReq.Header.Get("User-Agent"))
	require.NotNil(t, writer.log)
	require.Equal(t, 9, writer.log.InputTokens)
	require.Equal(t, 3, writer.log.OutputTokens)
	require.Equal(t, RequestTypeStream, writer.log.RequestType)
}

func TestAccountTestService_OpenAICompactForcesSystemUAAndRecordsZeroUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"id":"compact_45"}`)),
	}}
	writer := &accountTestUsageWriterStub{}
	svc := newOpenAIAccountTestServiceForRecording(upstream, writer)
	account := &Account{
		ID:       45,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                 "key-45",
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"User-Agent": "account-override/9.9",
			},
		},
	}

	err := svc.testOpenAICompactConnection(c, account, "gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, codexCLIUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.NotNil(t, writer.log)
	require.Equal(t, "compact_45", writer.log.RequestID)
	require.Zero(t, writer.log.TotalTokens())
	require.Equal(t, RequestTypeSync, writer.log.RequestType)
}

func TestAccountTestService_OpenAIImageAPIKeyPreservesUAOverrideAndRecordsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"id":"image_46","usage":{"input_tokens":11,"output_tokens":7},"data":[{"b64_json":"aGVsbG8="}]}`,
		)),
	}}
	writer := &accountTestUsageWriterStub{}
	svc := newOpenAIAccountTestServiceForRecording(upstream, writer)
	account := &Account{
		ID:       46,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":                 "key-46",
			"header_override_enabled": true,
			"header_overrides": map[string]any{
				"User-Agent": "account-override/9.9",
			},
		},
	}

	err := svc.testOpenAIImageAPIKey(c, context.Background(), account, "gpt-5.4", "draw")
	require.NoError(t, err)
	require.Equal(t, "account-override/9.9", upstream.lastReq.Header.Get("User-Agent"))
	require.NotNil(t, writer.log)
	require.Equal(t, 1, writer.log.ImageCount)
	require.Equal(t, 11, writer.log.InputTokens)
	require.Equal(t, 7, writer.log.OutputTokens)
	require.Equal(t, RequestTypeSync, writer.log.RequestType)
}

func TestAccountTestService_OpenAIImageOAuthForcesSystemIdentityAndRecordsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_47\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\"}}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_image_47\",\"usage\":{\"input_tokens\":13,\"output_tokens\":8},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n",
		)),
	}}
	writer := &accountTestUsageWriterStub{}
	svc := newOpenAIAccountTestServiceForRecording(upstream, writer)
	account := &Account{
		ID:       47,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-47",
			"user_agent":   "codex-tui/9.9",
		},
	}

	err := svc.testOpenAIImageOAuth(c, context.Background(), account, "gpt-5.4", "draw")
	require.NoError(t, err)
	require.Equal(t, "codex_vscode/"+codexCLIVersion, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "codex_vscode", upstream.lastReq.Header.Get("Originator"))
	require.NotNil(t, writer.log)
	require.Equal(t, "resp_image_47", writer.log.RequestID)
	require.Equal(t, 1, writer.log.ImageCount)
	require.Equal(t, 13, writer.log.InputTokens)
	require.Equal(t, 8, writer.log.OutputTokens)
	require.Equal(t, RequestTypeStream, writer.log.RequestType)
}

func TestAccountTestService_OpenAIRecordingFailurePreventsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newOpenAIAccountTestContext()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		)),
	}}
	svc := &AccountTestService{
		httpUpstream:   upstream,
		settingService: newOpenAICodexUASettingService("codex-tui/0.144.1"),
		usageLogWriter: &accountTestUsageWriterStub{err: errors.New("insert failed")},
	}
	account := &Account{
		ID:          43,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token-43"},
	}

	err := svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "", AccountTestModeDefault)
	require.ErrorContains(t, err, "record usage")
	require.NotContains(t, recorder.Body.String(), `"success":true`)
}

func TestAccountTestService_BackgroundContextSkipsUsageRecording(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder := newOpenAIAccountTestContext()
	c.Request = c.Request.WithContext(withoutAccountTestUsageRecording(c.Request.Context()))
	svc := &AccountTestService{
		usageLogWriter: &accountTestUsageWriterStub{err: errors.New("must not be called")},
	}

	err := svc.completeOpenAIAccountTest(c, newAccountTestMetrics(), &openAIAccountTestUsage{
		account: &Account{ID: 48},
		model:   "gpt-5.4",
	})
	require.NoError(t, err)
	require.Contains(t, recorder.Body.String(), `"success":true`)
}
