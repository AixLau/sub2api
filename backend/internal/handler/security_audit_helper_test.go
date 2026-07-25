package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCachesSecurityAuditCompletionSkipsWebSocketStages(t *testing.T) {
	require.True(t, cachesSecurityAuditCompletion("http"))
	require.True(t, cachesSecurityAuditCompletion(""))
	require.False(t, cachesSecurityAuditCompletion("first_turn"))
	require.False(t, cachesSecurityAuditCompletion("subsequent_turn"))
}

func TestRunSecurityAuditDoesNotSkipSubsequentWebSocketTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(nil, engine)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	subject := middleware2.AuthSubject{UserID: 7, Concurrency: 1}
	first := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"benign"}}`), "first_turn")
	require.NotNil(t, first)
	require.True(t, first.AllowNextStage)
	require.Equal(t, int64(1), engine.enqueues.Load())
	_, cached := c.Get(securityAuditCompletedContextKey)
	require.False(t, cached, "WebSocket stages must not set the HTTP completion cache")

	// Even if an HTTP path previously cached completion on this Context, WS turns
	// must still audit every response.create payload.
	c.Set(securityAuditCompletedContextKey, true)

	second := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"malicious follow-up"}}`), "subsequent_turn")
	require.NotNil(t, second)
	require.Equal(t, int64(2), engine.enqueues.Load(), "subsequent WebSocket turns must be audited again")
}

func TestRunSecurityAuditReusesPreForwardLegacyDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := &countingLegacyAuditEngine{}
	prompt := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(legacy, prompt)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"input":"benign"}`)
	cacheContentModerationDecision(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body, &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	})

	decision := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7},
		service.ContentModerationProtocolOpenAIResponses, "gpt-test", body, "http")

	require.NotNil(t, decision)
	require.True(t, decision.AllowNextStage)
	require.Zero(t, legacy.calls.Load())
	require.Equal(t, int64(1), prompt.enqueues.Load())
}

func TestRunSecurityAuditDoesNotInferSelectedAccountDeferral(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := &countingLegacyAuditEngine{}
	prompt := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(legacy, prompt)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	decision := runSecurityAudit(c, nil, coordinator, &service.ContentModerationService{}, nil,
		middleware2.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIImages,
		"gpt-image-2", []byte(`{"prompt":"benign"}`), "http")

	require.NotNil(t, decision)
	require.True(t, decision.AllowNextStage)
	require.Equal(t, int64(1), legacy.calls.Load())
	require.Equal(t, int64(1), prompt.enqueues.Load())
}

func TestRunSecurityAuditDefersLegacyOnlyForMarkedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := &countingLegacyAuditEngine{}
	prompt := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(legacy, prompt)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := []byte(`{"input":"benign"}`)
	markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)

	decision := runSecurityAudit(c, nil, coordinator, &service.ContentModerationService{}, nil,
		middleware2.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIResponses,
		"gpt-test", body, "http")

	require.NotNil(t, decision)
	require.True(t, decision.AllowNextStage)
	require.Zero(t, legacy.calls.Load())
	require.Equal(t, int64(1), prompt.enqueues.Load())
}

func TestRunSecurityAuditDoesNotReuseSelectedAccountDeferralForDifferentBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := &countingLegacyAuditEngine{}
	prompt := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(legacy, prompt)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", []byte(`{"input":"first"}`))

	decision := runSecurityAudit(c, nil, coordinator, &service.ContentModerationService{}, nil,
		middleware2.AuthSubject{UserID: 7}, service.ContentModerationProtocolOpenAIResponses,
		"gpt-test", []byte(`{"input":"second"}`), "http")

	require.NotNil(t, decision)
	require.True(t, decision.AllowNextStage)
	require.Equal(t, int64(1), legacy.calls.Load())
	require.Equal(t, int64(1), prompt.enqueues.Load())
}

func TestRunSecurityAuditUsesHandlerIdentityAndPublicAuditModelForCompositeRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"input":"benign"}`)
	for _, selectedAccount := range []bool{false, true} {
		name := "cached_decision"
		if selectedAccount {
			name = "selected_account"
		}
		t.Run(name, func(t *testing.T) {
			legacy := &countingLegacyAuditEngine{}
			prompt := newBlockingRecordingPromptEngine()
			coordinator := securityaudit.NewCoordinator(legacy, prompt)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
				Matched: true, PublicModel: "public-model", UpstreamModel: "upstream-model", TargetPlatform: service.PlatformOpenAI,
			}))
			if selectedAccount {
				markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "upstream-model", body)
			} else {
				cacheContentModerationDecision(c, service.ContentModerationProtocolOpenAIResponses, "upstream-model", body,
					&service.ContentModerationDecision{Allowed: true, Action: service.ContentModerationActionAllow})
			}

			decision := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7},
				service.ContentModerationProtocolOpenAIResponses, "upstream-model", body, "http")

			require.NotNil(t, decision)
			require.False(t, decision.AllowNextStage)
			require.Zero(t, legacy.calls.Load())
			request := requireRecordedPromptRequest(t, prompt)
			require.Equal(t, "public-model", request.Model)
			require.Equal(t, service.ContentModerationProtocolOpenAIResponses, request.Protocol)
			require.Equal(t, body, request.Body)
		})
	}
}

func TestOpenAIMessagesHandlerReusesPreForwardModerationIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-test","system":"system policy","messages":[{"role":"user","content":"benign"}]}`)
	legacy := &countingLegacyAuditEngine{}
	prompt := newBlockingRecordingPromptEngine()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	setSecurityAuditHandlerAuthContext(c, &service.Group{Platform: service.PlatformOpenAI, AllowMessagesDispatch: true})
	h := newSecurityAuditIdentityHandler(securityaudit.NewCoordinator(legacy, prompt))
	moderationService := newDisabledContentModerationServiceForHandlerTest(t)
	h.contentModerationService = moderationService
	h.moderationGuard = newContentModerationGuard(moderationService)
	entry := h.EnterOpenAIHTTPGatewayPipeline(c, openAIMessagesHTTPRouteMetaForTest())
	require.False(t, entry.Stop)
	_, cached := contentModerationDecisionFromCache(c, service.ContentModerationProtocolOpenAIMessages, "gpt-test", body)
	require.True(t, cached)

	h.Messages(c)

	require.Zero(t, legacy.calls.Load())
	request := requireRecordedPromptRequest(t, prompt)
	require.Equal(t, service.ContentModerationProtocolOpenAIMessages, request.Protocol)
	require.Equal(t, body, request.Body)
}

func TestOpenAIResponsesHandlerKeepsOriginalModerationBodyAfterReasoningRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-test","stream":false,"input":"benign","reasoning":{"effort":"high"}}`)
	capped, changed := service.ApplyOpenAIReasoningEffortPolicy(body, "low", nil)
	require.True(t, changed)
	require.NotEqual(t, body, capped)

	for _, selectedAccount := range []bool{false, true} {
		name := "cached_decision"
		if selectedAccount {
			name = "selected_account"
		}
		t.Run(name, func(t *testing.T) {
			legacy := &countingLegacyAuditEngine{}
			prompt := newBlockingRecordingPromptEngine()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			setSecurityAuditHandlerAuthContext(c, &service.Group{Platform: service.PlatformOpenAI, MaxReasoningEffort: "low"})
			h := newSecurityAuditIdentityHandler(securityaudit.NewCoordinator(legacy, prompt))
			moderationService := newDisabledContentModerationServiceForHandlerTest(t)
			h.contentModerationService = moderationService
			h.moderationGuard = newContentModerationGuard(moderationService)
			entry := h.EnterOpenAIHTTPGatewayPipeline(c, moderationcoverage.Entry{
				Method: http.MethodPost, Path: "/v1/responses", Handler: "OpenAIGatewayHandler.Responses",
				Upstream: true, ModerationRequired: true, Protocol: service.ContentModerationProtocolOpenAIResponses,
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			})
			require.False(t, entry.Stop)
			_, cached := contentModerationDecisionFromCache(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
			require.True(t, cached)
			if selectedAccount {
				markSelectedAccountModerationRequired(c, service.ContentModerationProtocolOpenAIResponses, "gpt-test", body)
			}

			h.Responses(c)

			require.Zero(t, legacy.calls.Load())
			request := requireRecordedPromptRequest(t, prompt)
			require.Equal(t, service.ContentModerationProtocolOpenAIResponses, request.Protocol)
			require.Equal(t, body, request.Body)
		})
	}
}

func setSecurityAuditHandlerAuthContext(c *gin.Context, group *service.Group) {
	groupID := int64(2)
	if group == nil {
		group = &service.Group{}
	}
	group.ID = groupID
	apiKey := &service.APIKey{
		ID: 101, Name: "test-key", GroupID: &groupID, Group: group,
		User: &service.User{ID: 7, Email: "user@example.com", Concurrency: 1},
	}
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 1})
}

func newSecurityAuditIdentityHandler(coordinator *securityaudit.Coordinator) *OpenAIGatewayHandler {
	return &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		concurrencyHelper:        &ConcurrencyHelper{concurrencyService: &service.ConcurrencyService{}},
		securityAuditCoordinator: coordinator,
	}
}

type countingLegacyAuditEngine struct {
	calls atomic.Int64
}

func (e *countingLegacyAuditEngine) Check(context.Context, securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	e.calls.Add(1)
	return &securityaudit.LegacyDecision{Allowed: true}, nil
}

type turnCountingEngine struct {
	mode     securityaudit.Mode
	enqueues atomic.Int64
}

func (e *turnCountingEngine) EffectiveMode() securityaudit.Mode { return e.mode }
func (e *turnCountingEngine) Enqueue(context.Context, securityaudit.Request) error {
	e.enqueues.Add(1)
	return nil
}
func (e *turnCountingEngine) Evaluate(context.Context, securityaudit.Request) (*securityaudit.PromptDecision, error) {
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}

type blockingRecordingPromptEngine struct {
	requests chan securityaudit.Request
}

func newBlockingRecordingPromptEngine() *blockingRecordingPromptEngine {
	return &blockingRecordingPromptEngine{requests: make(chan securityaudit.Request, 1)}
}

func requireRecordedPromptRequest(t *testing.T, engine *blockingRecordingPromptEngine) securityaudit.Request {
	t.Helper()
	select {
	case request := <-engine.requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for prompt audit request")
		return securityaudit.Request{}
	}
}

func (e *blockingRecordingPromptEngine) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}

func (e *blockingRecordingPromptEngine) Enqueue(context.Context, securityaudit.Request) error {
	return nil
}

func (e *blockingRecordingPromptEngine) Evaluate(_ context.Context, request securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.requests <- request
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock}, nil
}
