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

func TestOpenAIChatCompletions_UsesModerationGuardBeforeForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "guard blocked chat",
		Action:     service.ContentModerationActionBlock,
	}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","messages":[{"role":"user","content":"guard-risk"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	h := &OpenAIGatewayHandler{
		moderationGuard:     guard,
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
	require.Contains(t, w.Body.String(), "guard blocked chat")
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIChat, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.1", guard.calls[0].Model)
	require.Equal(t, []byte(body), guard.calls[0].Body)
}

func TestOpenAIChatCompletions_ChecksModerationGuardBeforeCyberSessionBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"model":"gpt-5.1","prompt_cache_key":"chat-session","messages":[{"role":"user","content":"hello"}]}`
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)

	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	key := service.CyberSessionBlockKey(apiKey.ID, c, []byte(body))
	cache := &openAIChatModerationGuardCyberCache{blocked: map[string]bool{key: true}}
	settingService := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{
		service.SettingKeyCyberSessionBlockEnabled:    "true",
		service.SettingKeyCyberSessionBlockTTLSeconds: "60",
	}}, nil)

	h := &OpenAIGatewayHandler{
		moderationGuard:     guard,
		gatewayService:      service.NewOpenAIGatewayService(nil, nil, nil, nil, nil, nil, cache, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, settingService, nil),
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
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIChat, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.1", guard.calls[0].Model)
	require.Equal(t, []byte(body), guard.calls[0].Body)
}

type openAIChatModerationGuardCyberCache struct {
	blocked map[string]bool
}

var _ service.GatewayCache = (*openAIChatModerationGuardCyberCache)(nil)
var _ service.CyberSessionBlockStore = (*openAIChatModerationGuardCyberCache)(nil)

func (c *openAIChatModerationGuardCyberCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (c *openAIChatModerationGuardCyberCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) SetUserAccountCooldown(context.Context, int64, int64, time.Duration) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) GetUserAccountCooldowns(context.Context, int64) (map[int64]struct{}, error) {
	return nil, nil
}

func (c *openAIChatModerationGuardCyberCache) SetReasoningContent(context.Context, string, string, time.Duration) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) GetReasoningContent(context.Context, string) (string, error) {
	return "", service.ErrReasoningContentNotFound
}

func (c *openAIChatModerationGuardCyberCache) SetGrokVideoPendingBilling(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) GetGrokVideoPendingBilling(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (c *openAIChatModerationGuardCyberCache) ClaimGrokVideoBilled(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (c *openAIChatModerationGuardCyberCache) ReleaseGrokVideoBilled(context.Context, string) error {
	return nil
}

func (c *openAIChatModerationGuardCyberCache) SetCyberSessionBlocked(_ context.Context, key string, _ time.Duration) error {
	if c.blocked == nil {
		c.blocked = map[string]bool{}
	}
	c.blocked[key] = true
	return nil
}

func (c *openAIChatModerationGuardCyberCache) IsCyberSessionBlocked(_ context.Context, key string) (bool, error) {
	return c.blocked[key], nil
}
