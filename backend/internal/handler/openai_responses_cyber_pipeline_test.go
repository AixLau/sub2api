package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesHTTP_CyberBlockedByPipelineBeforeRoutingBillingSlotsAndForward(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"model":"gpt-5.1","prompt_cache_key":"responses-cyber-session","input":"hello"}`
	w, c := newOpenAIResponsesCyberPipelineContext(t, body)
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)

	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	cyberChecker := &openAIResponsesCyberPipelineCheckerSpy{enabled: true, blocked: true}
	h := newOpenAIResponsesCyberPipelineHandler(t, guard, cyberChecker, func(context.Context, int64, int, string) (bool, error) {
		t.Fatalf("cyber-blocked responses request must not acquire a user slot")
		return false, nil
	})

	h.Responses(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Contains(t, w.Body.String(), cyberSessionBlockedClientMsg)
	require.Len(t, guard.calls, 1)
	require.Equal(t, service.ContentModerationProtocolOpenAIResponses, guard.calls[0].Protocol)
	require.Equal(t, "gpt-5.1", guard.calls[0].Model)
	require.Equal(t, []byte(body), guard.calls[0].Body)
	require.Equal(t, 1, cyberChecker.runtimeCalls)
	require.Equal(t, []string{service.CyberSessionBlockKey(apiKey.ID, c, []byte(body))}, cyberChecker.checkedKeys)
}

func TestOpenAIResponsesHTTP_ModerationBlockSkipsCyberPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"model":"gpt-5.1","prompt_cache_key":"responses-cyber-session","input":"risk"}`
	w, c := newOpenAIResponsesCyberPipelineContext(t, body)
	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Blocked:    true,
		StatusCode: http.StatusForbidden,
		Message:    "moderation blocked before cyber",
		Action:     service.ContentModerationActionBlock,
	}}
	cyberChecker := &openAIResponsesCyberPipelineCheckerSpy{enabled: true, blocked: true}
	h := newOpenAIResponsesCyberPipelineHandler(t, guard, cyberChecker, func(context.Context, int64, int, string) (bool, error) {
		t.Fatalf("moderation-blocked responses request must not acquire a user slot")
		return false, nil
	})

	h.Responses(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), "moderation blocked before cyber")
	require.Len(t, guard.calls, 1)
	require.Zero(t, cyberChecker.runtimeCalls)
	require.Empty(t, cyberChecker.checkedKeys)
}

func TestOpenAIResponsesHTTP_ImagePermissionBlockBeforeCyberPipelineStage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"model":"gpt-5.1","prompt_cache_key":"responses-image-session","input":"draw","tools":[{"type":"image_generation"}]}`
	w, c := newOpenAIResponsesCyberPipelineContext(t, body)
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	apiKey.Group.AllowImageGeneration = false

	guard := &moderationGuardSpy{decision: &service.ContentModerationDecision{
		Allowed: true,
		Action:  service.ContentModerationActionAllow,
	}}
	cyberChecker := &openAIResponsesCyberPipelineCheckerSpy{enabled: true, blocked: true}
	h := newOpenAIResponsesCyberPipelineHandler(t, guard, cyberChecker, func(context.Context, int64, int, string) (bool, error) {
		t.Fatalf("image-permission-blocked responses request must not acquire a user slot")
		return false, nil
	})

	h.Responses(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), service.ImageGenerationPermissionMessage())
	require.NotContains(t, w.Body.String(), "session_blocked_by_cyber_policy")
	require.Len(t, guard.calls, 1)
	require.Zero(t, cyberChecker.runtimeCalls)
	require.Empty(t, cyberChecker.checkedKeys)
}

func newOpenAIResponsesCyberPipelineContext(t *testing.T, body string) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	setGatewayAuthContextForModerationTest(c)
	return w, c
}

func newOpenAIResponsesCyberPipelineHandler(
	t *testing.T,
	guard *moderationGuardSpy,
	cyberChecker *openAIResponsesCyberPipelineCheckerSpy,
	acquireUserSlotFn func(context.Context, int64, int, string) (bool, error),
) *OpenAIGatewayHandler {
	t.Helper()

	concurrencyCache := &concurrencyCacheMock{acquireUserSlotFn: acquireUserSlotFn}
	channelService := service.NewChannelService(&openAIResponsesChannelMustNotRunRepo{t: t}, nil, nil, nil)
	return &OpenAIGatewayHandler{
		moderationGuard:          guard,
		pipeline:                 &OpenAIGatewayPipeline{moderationGuard: guard, cyberSessionChecker: cyberChecker},
		contentModerationService: newDisabledContentModerationServiceForHandlerTest(t),
		gatewayService: service.NewOpenAIGatewayService(
			nil, // accountRepo
			nil, // usageLogRepo
			nil, // usageBillingRepo
			nil, // userRepo
			nil, // userSubRepo
			nil, // userGroupRateRepo
			nil, // cache
			&config.Config{RunMode: config.RunModeSimple}, // cfg
			nil,            // schedulerSnapshot
			nil,            // concurrencyService
			nil,            // billingService
			nil,            // rateLimitService
			nil,            // billingCacheService
			nil,            // httpUpstream
			nil,            // deferredService
			nil,            // openAITokenProvider
			nil,            // grokTokenProvider
			nil,            // resolver
			channelService, // channelService
			nil,            // balanceNotifyService
			nil,            // settingService
			nil,            // userPlatformQuotaRepo
		),
		billingCacheService: service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil),
		apiKeyService:       &service.APIKeyService{},
		concurrencyHelper:   NewConcurrencyHelper(service.NewConcurrencyService(concurrencyCache), SSEPingFormatNone, time.Second),
		maxAccountSwitches:  1,
	}
}

type openAIResponsesChannelMustNotRunRepo struct {
	service.ChannelRepository
	t *testing.T
}

func (r *openAIResponsesChannelMustNotRunRepo) ListAll(context.Context) ([]service.Channel, error) {
	if r != nil && r.t != nil {
		r.t.Helper()
		r.t.Fatalf("cyber-blocked responses request must not resolve channel mapping")
	}
	return nil, nil
}

type openAIResponsesCyberPipelineCheckerSpy struct {
	enabled      bool
	blocked      bool
	runtimeCalls int
	checkedKeys  []string
}

func (s *openAIResponsesCyberPipelineCheckerSpy) CyberSessionBlockRuntime(context.Context) (bool, time.Duration) {
	s.runtimeCalls++
	return s.enabled, time.Minute
}

func (s *openAIResponsesCyberPipelineCheckerSpy) IsCyberSessionBlocked(_ context.Context, key string) bool {
	s.checkedKeys = append(s.checkedKeys, key)
	return s.blocked
}
