package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIChatCompletions_CyberBlockedByPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","prompt_cache_key":"chat-session","messages":[{"role":"user","content":"hello"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			t.Fatalf("cyber-blocked chat request must not acquire a user slot")
			return false, nil
		},
	}
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	cyberChecker := &openAIChatCyberPipelineCheckerSpy{enabled: true, blocked: true}
	h := &OpenAIGatewayHandler{
		moderationGuard:     guard,
		pipeline:            &OpenAIGatewayPipeline{moderationGuard: guard, cyberSessionChecker: cyberChecker},
		gatewayService:      &service.OpenAIGatewayService{},
		billingCacheService: &service.BillingCacheService{},
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(concurrencyCache), SSEPingFormatNone, time.Second),
	}

	result := h.EnterOpenAIHTTPGatewayPipeline(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/chat/completions",
		Handler:            "OpenAIGatewayHandler.ChatCompletions",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIChat,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
	})

	require.True(t, result.Stop)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIChat, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.1", guard.calls[0].Model)
	require.Equal(t, []byte(body), guard.calls[0].Body)
	require.Equal(t, 1, cyberChecker.runtimeCalls)
	require.Equal(t, []string{service.CyberSessionExplicitBlockKey(apiKey.ID, c, []byte(body))}, cyberChecker.checkedKeys)
}

func TestOpenAIChatCompletions_ModerationBlockSkipsCyberPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "moderation blocked before cyber",
		Action:     service.ContentModerationActionBlock,
	}}
	cyberChecker := &openAIChatCyberPipelineCheckerSpy{enabled: true, blocked: true}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","prompt_cache_key":"chat-session","messages":[{"role":"user","content":"risk"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	h := &OpenAIGatewayHandler{
		pipeline:            &OpenAIGatewayPipeline{moderationGuard: guard, cyberSessionChecker: cyberChecker},
		gatewayService:      &service.OpenAIGatewayService{},
		billingCacheService: &service.BillingCacheService{},
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(&concurrencyCacheMock{}), SSEPingFormatNone, time.Second),
	}

	result := h.EnterOpenAIHTTPGatewayPipeline(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/chat/completions",
		Handler:            "OpenAIGatewayHandler.ChatCompletions",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIChat,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
	})

	require.True(t, result.Stop)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "moderation blocked before cyber")
	require.Len(t, guard.calls, 1)
	require.Zero(t, cyberChecker.runtimeCalls)
	require.Empty(t, cyberChecker.checkedKeys)
}

func TestOpenAIChat_GatewayPipelineEntrypointNilPipelineCyberFallbackBlocksBeforeScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","prompt_cache_key":"chat-session","messages":[{"role":"user","content":"hello"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key := service.CyberSessionExplicitBlockKey(apiKey.ID, c, []byte(body))
	cache := &openAIChatModerationGuardCyberCache{blocked: map[string]bool{key: true}}
	settingService := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{
		service.SettingKeyCyberSessionBlockEnabled:    "true",
		service.SettingKeyCyberSessionBlockTTLSeconds: "60",
	}}, nil)
	concurrencyCache := &concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			t.Fatalf("nil-pipeline cyber fallback must not acquire a user slot")
			return false, nil
		},
	}
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	h := &OpenAIGatewayHandler{
		moderationGuard:     guard,
		gatewayService:      service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, cache, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settingService, nil),
		billingCacheService: &service.BillingCacheService{},
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(concurrencyCache), SSEPingFormatNone, time.Second),
	}

	result := h.EnterOpenAIHTTPGatewayPipeline(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/chat/completions",
		Handler:            "OpenAIGatewayHandler.ChatCompletions",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           service.ContentModerationProtocolOpenAIChat,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
	})

	require.True(t, result.Stop)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Len(t, guard.calls, 1)
}

type openAIChatCyberPipelineCheckerSpy struct {
	enabled      bool
	blocked      bool
	runtimeCalls int
	checkedKeys  []string
}

func (s *openAIChatCyberPipelineCheckerSpy) FindCyberSessionBlockedForRequest(_ context.Context, apiKeyID int64, c *gin.Context, body []byte, _, _ string) string {
	s.runtimeCalls++
	key := service.CyberSessionExplicitBlockKey(apiKeyID, c, body)
	s.checkedKeys = append(s.checkedKeys, key)
	if s.enabled && s.blocked {
		return key
	}
	return ""
}
