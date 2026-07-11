package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAIGatewayHandlerCheckWithModerationGuardUsesPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pipelineDecision := &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by pipeline",
		Action:     service.ContentModerationActionBlock,
	}
	pipelineGuard := &openAIGatewayPipelineIntegrationGuardSpy{decision: pipelineDecision}
	handlerGuard := &openAIGatewayPipelineIntegrationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	input := moderationGuardInput{
		Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","input":"pipeline-risk"}`),
	}
	reqLog := zap.NewNop()
	h := &OpenAIGatewayHandler{
		moderationGuard: handlerGuard,
		pipeline:        newOpenAIGatewayPipeline(pipelineGuard),
	}

	decision := h.checkWithModerationGuard(c, reqLog, input)

	require.Same(t, pipelineDecision, decision)
	require.Len(t, pipelineGuard.calls, 1)
	require.Equal(t, input, pipelineGuard.calls[0])
	require.Empty(t, handlerGuard.calls)
}

func TestOpenAIGatewayHandlerCheckWithModerationGuardNilPipelineFallsBackToHandlerGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	expectedDecision := &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "blocked by fallback guard",
		Action:     service.ContentModerationActionBlock,
	}
	guard := &openAIGatewayPipelineIntegrationGuardSpy{decision: expectedDecision}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	input := moderationGuardInput{
		Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
		Protocol: service.ContentModerationProtocolOpenAIChat,
		Model:    "gpt-5.1",
		Body:     []byte(`{"model":"gpt-5.1","messages":[{"role":"user","content":"risk"}]}`),
	}
	h := &OpenAIGatewayHandler{moderationGuard: guard}

	var decision *service.ContentModerationDecision
	require.NotPanics(t, func() {
		decision = h.checkWithModerationGuard(c, zap.NewNop(), input)
	})

	require.Same(t, expectedDecision, decision)
	require.Len(t, guard.calls, 1)
	require.Equal(t, input, guard.calls[0])
}

func TestOpenAIGatewayHandlerCheckWithModerationGuardNilPipelineFailsClosedWithoutGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	h := &OpenAIGatewayHandler{}

	var decision *service.ContentModerationDecision
	require.NotPanics(t, func() {
		decision = h.checkWithModerationGuard(c, zap.NewNop(), moderationGuardInput{
			Subject:  middleware2.AuthSubject{UserID: 7, Concurrency: 1},
			Protocol: service.ContentModerationProtocolOpenAIEmbeddings,
			Model:    "text-embedding-3-small",
			Body:     []byte(`{"model":"text-embedding-3-small","input":"hello"}`),
		})
	})

	require.NotNil(t, decision)
	require.True(t, decision.Blocked)
	require.False(t, decision.Allowed)
	require.Equal(t, http.StatusServiceUnavailable, decision.StatusCode)
	require.Equal(t, service.ContentModerationActionError, decision.Action)
}

func TestGrokTextModerationBlocksBeforeBillingAccountSelectionAndForward(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		meta moderationcoverage.Entry
		body string
	}{
		{
			name: "chat streaming",
			path: "/v1/chat/completions",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.ChatCompletions",
				Protocol: service.ContentModerationProtocolOpenAIChat,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":true,"messages":[{"role":"user","content":"blocked"}]}`,
		},
		{
			name: "chat non-streaming root alias",
			path: "/chat/completions",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.ChatCompletions",
				Protocol: service.ContentModerationProtocolOpenAIChat,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":false,"messages":[{"role":"user","content":"blocked"}]}`,
		},
		{
			name: "messages non-streaming",
			path: "/v1/messages",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.Messages",
				Protocol: service.ContentModerationProtocolOpenAIMessages,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":false,"messages":[{"role":"user","content":"blocked"}]}`,
		},
		{
			name: "messages streaming",
			path: "/v1/messages",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.Messages",
				Protocol: service.ContentModerationProtocolOpenAIMessages,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":true,"messages":[{"role":"user","content":"blocked"}]}`,
		},
		{
			name: "responses streaming",
			path: "/responses",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.Responses",
				Protocol: service.ContentModerationProtocolOpenAIResponses,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":true,"input":"blocked"}`,
		},
		{
			name: "responses non-streaming codex alias",
			path: "/backend-api/codex/responses",
			meta: moderationcoverage.Entry{
				Handler:  "OpenAIGatewayHandler.Responses",
				Protocol: service.ContentModerationProtocolOpenAIResponses,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			},
			body: `{"model":"grok-4","stream":false,"input":"blocked"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := []string{}
			guard := &openAIGatewayPipelineIntegrationGuardSpy{
				decision: &service.ContentModerationDecision{
					Blocked:    true,
					StatusCode: http.StatusForbidden,
					Message:    "blocked by Grok text moderation",
					Action:     service.ContentModerationActionBlock,
				},
				onCheck: func() { events = append(events, "moderation") },
			}

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			setGatewayAuthContextForModerationTest(c)
			apiKey, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			apiKey.Group.Platform = service.PlatformGrok
			apiKey.Group.AllowMessagesDispatch = true

			h := &OpenAIGatewayHandler{
				pipeline:       newOpenAIGatewayPipeline(guard),
				gatewayService: &service.OpenAIGatewayService{},
			}

			result := h.EnterOpenAIHTTPGatewayPipeline(c, tt.meta)
			billingCalls, accountSelectionCalls, forwardCalls := 0, 0, 0
			if !result.Stop {
				billingCalls++
				accountSelectionCalls++
				forwardCalls++
				events = append(events, "downstream")
			}

			require.True(t, result.Stop)
			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Equal(t, []string{"moderation"}, events)
			require.Len(t, guard.calls, 1, "each accepted Grok text request must be moderated exactly once")
			require.Equal(t, tt.meta.Protocol, guard.calls[0].Protocol)
			require.Zero(t, billingCalls)
			require.Zero(t, accountSelectionCalls)
			require.Zero(t, forwardCalls)
		})
	}
}

func TestGrokMediaModerationRemainsDedicatedAndBlocksBeforeDownstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		path   string
		handle func(*OpenAIGatewayHandler, *gin.Context)
	}{
		{name: "images", path: "/v1/images/generations", handle: func(h *OpenAIGatewayHandler, c *gin.Context) { h.GrokImages(c) }},
		{name: "video", path: "/v1/videos/generations", handle: func(h *OpenAIGatewayHandler, c *gin.Context) { h.GrokVideoGeneration(c) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moderationSvc, repo := newBlockingContentModerationServiceForHandlerTest(t, "grok-media-risk")
			userSlotCalls := 0
			cache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					userSlotCalls++
					return true, nil
				},
			}

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(`{"model":"grok-imagine","prompt":"grok-media-risk"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			setGatewayAuthContextForModerationTest(c)
			apiKey, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			apiKey.Group.Platform = service.PlatformGrok
			apiKey.Group.AllowImageGeneration = true

			h := &OpenAIGatewayHandler{
				contentModerationService: moderationSvc,
				gatewayService:           &service.OpenAIGatewayService{},
				billingCacheService:      &service.BillingCacheService{},
				apiKeyService:            &service.APIKeyService{},
				concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
			}

			tt.handle(h, c)

			require.Equal(t, http.StatusForbidden, rec.Code)
			require.Contains(t, rec.Body.String(), "内容审计测试阻断")
			require.Eventually(t, func() bool { return len(repo.logSnapshot()) == 1 }, time.Second, 10*time.Millisecond)
			require.Zero(t, userSlotCalls, "dedicated Grok media moderation must block before billing, account selection, and forwarding")
		})
	}
}

func TestCountTokensModerationAuditedBodyIsUsedForParsingAndForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		auditedBody   []byte
		requestBody   []byte
		wantForwarded string
		wantExcluded  string
	}{
		{
			name:          "admitted request uses cached audited bytes",
			auditedBody:   []byte(`{"model":"gpt-5.3","system":"audited-system","messages":[{"role":"user","content":"audited-request"}]}`),
			requestBody:   []byte(`{"model":"gpt-4.1","system":"tampered-system","messages":[{"role":"user","content":"tampered-request"}]}`),
			wantForwarded: "audited-request",
			wantExcluded:  "tampered-request",
		},
		{
			name:          "direct handler falls back to request body",
			requestBody:   []byte(`{"model":"gpt-4.1","messages":[{"role":"user","content":"direct-request"}]}`),
			wantForwarded: "direct-request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, accountRepo, upstream := newCountTokensAuditedBodyTestHandler()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", strings.NewReader(string(tt.requestBody)))
			c.Request.Header.Set("Content-Type", "application/json")
			setGatewayAuthContextForModerationTest(c)
			apiKey, ok := middleware2.GetAPIKeyFromContext(c)
			require.True(t, ok)
			apiKey.Group.AllowMessagesDispatch = true
			if len(tt.auditedBody) > 0 {
				setGatewayPreForwardRequest(c, gatewayPreForwardRequest{
					Protocol: service.ContentModerationProtocolAnthropicMessages,
					Model:    "gpt-5.3",
					Body:     tt.auditedBody,
					Parsed: &service.ParsedRequest{
						Model: "gpt-5.3",
						Body:  service.NewRequestBodyRef(tt.auditedBody),
					},
				})
				moderationcoverage.MarkPipelineAdmitted(c, moderationcoverage.PipelineGatewayPreForward, moderationcoverage.StagePreForward, "test audited CountTokens admission")
			}

			h.CountTokens(c)

			require.Equal(t, http.StatusOK, rec.Code)
			require.JSONEq(t, `{"input_tokens":17}`, rec.Body.String())
			require.Equal(t, 1, accountRepo.listCalls)
			require.Equal(t, 1, upstream.calls)
			require.Contains(t, string(upstream.body), tt.wantForwarded)
			if tt.wantExcluded != "" {
				require.NotContains(t, string(upstream.body), tt.wantExcluded)
			}
		})
	}
}

type countTokensAuditedBodyAccountRepo struct {
	service.AccountRepository
	account   service.Account
	listCalls int
}

func (r *countTokensAuditedBodyAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	r.listCalls++
	if r.account.Platform != platform {
		return nil, nil
	}
	return []service.Account{r.account}, nil
}

func (r *countTokensAuditedBodyAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.account.ID != id {
		return nil, nil
	}
	account := r.account
	return &account, nil
}

type countTokensAuditedBodyUpstream struct {
	calls int
	body  []byte
}

func (u *countTokensAuditedBodyUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.body = body
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":17}`)),
	}, nil
}

func (u *countTokensAuditedBodyUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newCountTokensAuditedBodyTestHandler() (*OpenAIGatewayHandler, *countTokensAuditedBodyAccountRepo, *countTokensAuditedBodyUpstream) {
	cfg := &config.Config{}
	cfg.RunMode = config.RunModeSimple
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true

	accountRepo := &countTokensAuditedBodyAccountRepo{account: service.Account{
		ID:          901,
		Name:        "count-tokens-openai",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "http://upstream.example",
		},
	}}
	upstream := &countTokensAuditedBodyUpstream{}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	gatewaySvc := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		billingCacheSvc,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	h := &OpenAIGatewayHandler{
		gatewayService:      gatewaySvc,
		billingCacheService: billingCacheSvc,
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper: NewConcurrencyHelper(
			service.NewConcurrencyService(&concurrencyCacheMock{}),
			SSEPingFormatNone,
			time.Second,
		),
	}
	return h, accountRepo, upstream
}

type openAIGatewayPipelineIntegrationGuardSpy struct {
	decision *service.ContentModerationDecision
	calls    []moderationGuardInput
	onCheck  func()
}

func (s *openAIGatewayPipelineIntegrationGuardSpy) Check(c *gin.Context, reqLog *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	if s.onCheck != nil {
		s.onCheck()
	}
	s.calls = append(s.calls, input)
	return s.decision
}
