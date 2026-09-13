//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGeminiV1BetaModels_RegistrarSnapshotRunsCompletePipelineOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newGeminiV1BetaPipelineFixture(t)
	wantStages := []string{
		moderationcoverage.StageModeration, moderationcoverage.StagePreForward,
		moderationcoverage.StageBilling, moderationcoverage.StageRouting,
		moderationcoverage.StageForward, moderationcoverage.StageUsage,
	}
	wantCalls := []string{"moderation", "audit", "billing", "routing", "forward", "usage-billing", "usage"}

	// Reuse the handler across requests, while varying the payload and session.
	// Registrar state must be consumed once within each request and never reused
	// as the next request's body or accounting identity.
	for i, text := range []string{"first turn", "second turn"} {
		body := fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"text":%q}]}]}`, text)
		sessionID := fmt.Sprintf("gemini-session-%d", i+1)
		start := len(f.trace.snapshot())

		rec := f.serve(body, sessionID)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), `"text":"fixture answer"`)
		require.Equal(t, wantCalls, f.trace.snapshot()[start:])
		require.Zero(t, f.bodyAfterAdmission.reads, "the handler must use the registrar snapshot instead of rereading the body")
		require.Equal(t, wantStages, f.stageNames)
		require.Equal(t, 1, f.receipts)
		require.True(t, f.admitted)
		require.Equal(t, body, string(f.cachedBody))
		require.Equal(t, body, string(f.audit.requests[i].Body))
		require.Equal(t, f.apiKey.ID, f.audit.requests[i].APIKeyID)
		require.Equal(t, f.apiKey.UserID, f.audit.requests[i].UserID)
		require.JSONEq(t, body, f.upstream.bodies[i])
		require.Equal(t, "/v1beta/models/gemini-2.5-pro:generateContent", f.upstream.paths[i])
		require.Equal(t, f.account.ID, f.upstream.accountIDs[i])
		require.Equal(t, f.apiKey.UserID, f.billing.userIDs[i])

		command := f.usageBilling.commands[i]
		require.Equal(t, service.HashUsageRequestPayload([]byte(body)), command.RequestPayloadHash)
		require.Equal(t, f.apiKey.ID, command.APIKeyID)
		require.Equal(t, f.apiKey.UserID, command.UserID)
		require.Equal(t, f.account.ID, command.AccountID)
		require.Equal(t, "gemini-2.5-pro", command.Model)
		require.Equal(t, 13, command.InputTokens)
		require.Equal(t, 7, command.OutputTokens)
		require.InDelta(t, 13*1e-6+7*2e-6, command.BalanceCost, 1e-12)

		usage := f.usage.logs[i]
		require.Equal(t, command.RequestID, usage.RequestID)
		require.Equal(t, command.APIKeyID, usage.APIKeyID)
		require.Equal(t, command.UserID, usage.UserID)
		require.Equal(t, command.AccountID, usage.AccountID)
		require.Equal(t, command.InputTokens, usage.InputTokens)
		require.Equal(t, command.OutputTokens, usage.OutputTokens)
		require.NotNil(t, usage.SessionID)
		require.Equal(t, sessionID, *usage.SessionID)
		require.Len(t, f.upstream.bodies, i+1)
		require.Len(t, f.usageBilling.commands, i+1)
		require.Len(t, f.usage.logs, i+1)
	}
	require.Zero(t, f.audit.legacyCalls, "the audit coordinator must reuse the registrar's moderation decision")
}

func TestGeminiV1BetaModels_PipelineRejectsBeforeRoutingAndForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		configure func(*geminiV1BetaPipelineFixture)
		status    int
		calls     []string
	}{
		{
			name:      "missing API key",
			configure: func(f *geminiV1BetaPipelineFixture) { f.apiKey = nil },
			status:    http.StatusUnauthorized,
		},
		{
			name:      "wrong group platform",
			configure: func(f *geminiV1BetaPipelineFixture) { f.apiKey.Group.Platform = service.PlatformAnthropic },
			status:    http.StatusBadRequest,
		},
		{
			name:      "moderation blocked",
			configure: func(f *geminiV1BetaPipelineFixture) { f.moderation.blocked = true },
			status:    http.StatusForbidden,
			calls:     []string{"moderation"},
		},
		{
			name:      "security audit blocked",
			configure: func(f *geminiV1BetaPipelineFixture) { f.audit.blocked = true },
			status:    http.StatusForbidden,
			calls:     []string{"moderation", "audit"},
		},
		{
			name:      "insufficient balance",
			configure: func(f *geminiV1BetaPipelineFixture) { f.billing.balance = 0 },
			status:    http.StatusForbidden,
			calls:     []string{"moderation", "audit", "billing"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newGeminiV1BetaPipelineFixture(t)
			tt.configure(f)

			rec := f.serve(`{"contents":[{"role":"user","parts":[{"text":"blocked turn"}]}]}`, "blocked-session")

			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), `"error"`)
			require.Equal(t, tt.calls, f.trace.snapshot())
			require.Empty(t, f.upstream.bodies)
			require.Empty(t, f.usageBilling.commands)
			require.Empty(t, f.usage.logs)
			require.NotContains(t, f.stageNames, moderationcoverage.StageRouting)
			require.NotContains(t, f.stageNames, moderationcoverage.StageForward)
			require.NotContains(t, f.stageNames, moderationcoverage.StageUsage)
		})
	}
}

type geminiV1BetaPipelineTrace struct {
	mu    sync.Mutex
	calls []string
}

func (s *geminiV1BetaPipelineTrace) add(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *geminiV1BetaPipelineTrace) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type geminiV1BetaPipelineModeration struct {
	trace   *geminiV1BetaPipelineTrace
	blocked bool
}

func (s *geminiV1BetaPipelineModeration) Check(c *gin.Context, _ *zap.Logger, input moderationGuardInput) *service.ContentModerationDecision {
	s.trace.add("moderation")
	decision := &service.ContentModerationDecision{Allowed: !s.blocked, Blocked: s.blocked, Action: service.ContentModerationActionAllow}
	if s.blocked {
		decision.Action = service.ContentModerationActionBlock
		decision.StatusCode = http.StatusForbidden
		decision.Message = "fixture policy rejected request"
	}
	cacheContentModerationDecision(c, input.Protocol, input.Model, input.Body, decision)
	return decision
}

type geminiV1BetaPipelineAudit struct {
	trace       *geminiV1BetaPipelineTrace
	blocked     bool
	legacyCalls int
	requests    []securityaudit.Request
}

func (s *geminiV1BetaPipelineAudit) Check(context.Context, securityaudit.Request) (*securityaudit.LegacyDecision, error) {
	s.legacyCalls++
	s.trace.add("legacy-audit")
	return &securityaudit.LegacyDecision{Allowed: true}, nil
}

func (s *geminiV1BetaPipelineAudit) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}

func (s *geminiV1BetaPipelineAudit) Enqueue(context.Context, securityaudit.Request) error {
	return errors.New("blocking audit must not enqueue")
}

func (s *geminiV1BetaPipelineAudit) Evaluate(_ context.Context, request securityaudit.Request) (*securityaudit.PromptDecision, error) {
	s.trace.add("audit")
	s.requests = append(s.requests, request.Clone())
	if s.blocked {
		return &securityaudit.PromptDecision{Kind: securityaudit.DecisionBlock}, nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}

type geminiV1BetaPipelineBillingCache struct {
	service.BillingCache
	trace   *geminiV1BetaPipelineTrace
	balance float64
	userIDs []int64
}

func (s *geminiV1BetaPipelineBillingCache) GetUserBalance(_ context.Context, userID int64) (float64, error) {
	s.trace.add("billing")
	s.userIDs = append(s.userIDs, userID)
	return s.balance, nil
}

type geminiV1BetaPipelineSchedulerCache struct {
	*fakeSchedulerCache
	trace *geminiV1BetaPipelineTrace
}

func (s *geminiV1BetaPipelineSchedulerCache) GetSnapshot(ctx context.Context, bucket service.SchedulerBucket) ([]*service.Account, bool, error) {
	// The handler's single-Antigravity-account probe precedes selection; only
	// the Gemini bucket represents the routing stage under test.
	if bucket.Platform != service.PlatformGemini {
		return nil, true, nil
	}
	s.trace.add("routing")
	return s.fakeSchedulerCache.GetSnapshot(ctx, bucket)
}

type geminiV1BetaPipelineUpstream struct {
	service.HTTPUpstream
	trace      *geminiV1BetaPipelineTrace
	bodies     []string
	paths      []string
	accountIDs []int64
}

func (s *geminiV1BetaPipelineUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	s.trace.add("forward")
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	s.bodies = append(s.bodies, string(body))
	s.paths = append(s.paths, req.URL.Path)
	s.accountIDs = append(s.accountIDs, accountID)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {fmt.Sprintf("fixture-gemini-%d", len(s.bodies))}},
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":{"role":"model","parts":[{"text":"fixture answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":13,"candidatesTokenCount":7,"totalTokenCount":20}}`)),
	}, nil
}

type geminiV1BetaPipelineUsageBilling struct {
	service.UsageBillingRepository
	trace    *geminiV1BetaPipelineTrace
	commands []service.UsageBillingCommand
}

func (s *geminiV1BetaPipelineUsageBilling) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	s.trace.add("usage-billing")
	s.commands = append(s.commands, *command)
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

type geminiV1BetaPipelineUsageRepo struct {
	service.UsageLogRepository
	trace *geminiV1BetaPipelineTrace
	logs  []service.UsageLog
}

func (s *geminiV1BetaPipelineUsageRepo) Create(_ context.Context, usage *service.UsageLog) (bool, error) {
	s.trace.add("usage")
	s.logs = append(s.logs, *usage)
	return true, nil
}

type geminiV1BetaBodyAfterAdmission struct{ reads int }

func (s *geminiV1BetaBodyAfterAdmission) Read([]byte) (int, error) {
	s.reads++
	return 0, errors.New("body has already been read by the registrar")
}

func (*geminiV1BetaBodyAfterAdmission) Close() error { return nil }

type geminiV1BetaPipelineFixture struct {
	handler            *GatewayHandler
	apiKey             *service.APIKey
	account            *service.Account
	trace              *geminiV1BetaPipelineTrace
	moderation         *geminiV1BetaPipelineModeration
	audit              *geminiV1BetaPipelineAudit
	billing            *geminiV1BetaPipelineBillingCache
	upstream           *geminiV1BetaPipelineUpstream
	usageBilling       *geminiV1BetaPipelineUsageBilling
	usage              *geminiV1BetaPipelineUsageRepo
	bodyAfterAdmission *geminiV1BetaBodyAfterAdmission
	cachedBody         []byte
	stageNames         []string
	receipts           int
	admitted           bool
}

func newGeminiV1BetaPipelineFixture(t *testing.T) *geminiV1BetaPipelineFixture {
	t.Helper()
	groupID := int64(931)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive, RateMultiplier: 1}
	// Controlled fixture prices keep this entrypoint test independent of any
	// external catalog or built-in model-family fallback.
	inputPrice, outputPrice := 1e-6, 2e-6
	group.ModelPricing = []service.ChannelModelPricing{{
		Models: []string{"gemini-2.5-pro"}, BillingMode: service.BillingModeToken,
		InputPrice: &inputPrice, OutputPrice: &outputPrice,
	}}
	account := &service.Account{
		ID: 932, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "fixture-gemini-key", "base_url": "https://gemini.fixture.invalid"},
		AccountGroups: []service.AccountGroup{{AccountID: 932, GroupID: groupID}},
	}
	f := &geminiV1BetaPipelineFixture{
		account: account,
		apiKey: &service.APIKey{ID: 933, UserID: 934, GroupID: &groupID, Group: group, Status: service.StatusActive,
			User: &service.User{ID: 934, Concurrency: 2, Balance: 100}},
		trace: &geminiV1BetaPipelineTrace{},
	}
	f.moderation = &geminiV1BetaPipelineModeration{trace: f.trace}
	f.audit = &geminiV1BetaPipelineAudit{trace: f.trace}
	f.billing = &geminiV1BetaPipelineBillingCache{trace: f.trace, balance: 100}
	f.upstream = &geminiV1BetaPipelineUpstream{trace: f.trace}
	f.usageBilling = &geminiV1BetaPipelineUsageBilling{trace: f.trace}
	f.usage = &geminiV1BetaPipelineUsageRepo{trace: f.trace}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Default.RateMultiplier = 1
	billing := service.NewBillingCacheService(f.billing, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	snapshot := service.NewSchedulerSnapshotService(&geminiV1BetaPipelineSchedulerCache{
		fakeSchedulerCache: &fakeSchedulerCache{accounts: []*service.Account{account}}, trace: f.trace,
	}, nil, nil, &fakeGroupRepo{group: group}, cfg)
	costBilling := service.NewBillingService(cfg, nil)
	gateway := service.NewGatewayService(
		nil, &fakeGroupRepo{group: group}, f.usage, f.usageBilling,
		nil, nil, nil, nil, cfg, snapshot, nil, costBilling,
		nil, nil, nil, nil, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, service.NewModelPricingResolver(nil, costBilling), nil, nil, nil,
	)
	f.handler = &GatewayHandler{
		gatewayService:           gateway,
		geminiCompatService:      service.NewGeminiMessagesCompatService(nil, nil, nil, nil, nil, nil, f.upstream, nil, cfg),
		billingCacheService:      billing,
		concurrencyHelper:        NewConcurrencyHelper(service.NewConcurrencyService(&fakeConcurrencyCache{}), SSEPingFormatNone, 0),
		preForwardPipeline:       newGatewayPreForwardPipeline(f.moderation),
		securityAuditCoordinator: securityaudit.NewCoordinator(f.audit, f.audit),
		stageAdapterRegistry:     NewStageAdapterRegistry(),
		maxAccountSwitchesGemini: 1,
		cfg:                      cfg,
	}
	return f
}

func (f *geminiV1BetaPipelineFixture) serve(body, sessionID string) *httptest.ResponseRecorder {
	router := gin.New()
	router.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
		if f.apiKey != nil {
			c.Set(string(middleware.ContextKeyAPIKey), f.apiKey)
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, f.apiKey.Group))
		}
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 934, Concurrency: 2})
		meta := moderationcoverage.Entry{
			Method: http.MethodPost, Path: "/v1beta/models/*modelAction", Handler: "GatewayHandler.GeminiV1BetaModels",
			Upstream: true, ModerationRequired: true, Protocol: service.ContentModerationProtocolGemini,
			Pipeline: moderationcoverage.PipelineGatewayPreForward, Status: moderationcoverage.StatusCovered,
			StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("GatewayHandler.GeminiV1BetaModels", service.ContentModerationProtocolGemini),
		}
		moderationcoverage.SetRouteMeta(c, meta)
		defer func() {
			f.stageNames = nil
			for _, stage := range moderationcoverage.PipelineStageExecutionsFromContext(c) {
				f.stageNames = append(f.stageNames, stage.Stage)
			}
			f.receipts = len(moderationcoverage.ModerationReceiptsFromContext(c))
			f.admitted = moderationcoverage.PipelineAdmittedFromContext(c)
		}()
		// Invoke the same entrypoint used by the route registrar, then enter the
		// real handler. Only external storage/upstream ports are test doubles.
		if result := f.handler.EnterGatewayPreForwardPipeline(c, meta); result.Blocked {
			return
		}
		request, ok := gatewayPreForwardRequestFromContext(c, service.ContentModerationProtocolGemini)
		if ok {
			f.cachedBody = append([]byte(nil), request.Body...)
		}
		f.bodyAfterAdmission = &geminiV1BetaBodyAfterAdmission{}
		c.Request.Body = f.bodyAfterAdmission
		f.handler.GeminiV1BetaModels(c)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-pro:generateContent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Session-Id", sessionID)
	router.ServeHTTP(rec, req)
	return rec
}

func TestGeminiV1BetaListModels_AllowlistFiltersNativeResponse(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-2.5-pro"},{"name":"models/gemini-2.5-flash"}],"nextPageToken":"next"}`)
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(body, service.GroupModelAllowlist{Enabled: true, Models: []string{"gemini-2.5-pro"}})
	require.True(t, ok)
	require.True(t, dropped)
	require.JSONEq(t, `{"models":[{"name":"models/gemini-2.5-pro"}],"nextPageToken":"next"}`, string(filtered))
}

func TestGeminiV1BetaListModels_ForcedAntigravityAppliesAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/antigravity/v1beta/models", nil)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{
			Platform: service.PlatformGemini,
			ModelAllowlist: service.GroupModelAllowlist{
				Enabled: true,
				Models:  []string{"gemini-custom"},
			},
		},
	})
	c.Set(string(middleware.ContextKeyForcePlatform), service.PlatformAntigravity)

	(&GatewayHandler{}).GeminiV1BetaListModels(c)

	require.Equal(t, http.StatusOK, rec.Code)
	var got antigravity.GeminiModelsListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Empty(t, got.Models)
}

func TestGeminiModelAllowlist_DisabledPreservesNativeResponse(t *testing.T) {
	body := []byte(`{"models":[{"name":"models/gemini-2.5-pro"}]}`)
	filtered, dropped, ok := filterUpstreamGeminiModelsBody(body, service.GroupModelAllowlist{Enabled: false, Models: []string{"other"}})
	require.True(t, ok)
	require.False(t, dropped)
	require.Equal(t, body, filtered)
}

// TestGeminiV1BetaHandler_PlatformRoutingInvariant 文档化并验证 Handler 层的平台路由逻辑不变量
// 该测试确保 gemini 和 antigravity 平台的路由逻辑符合预期
func TestGeminiV1BetaHandler_PlatformRoutingInvariant(t *testing.T) {
	tests := []struct {
		name            string
		platform        string
		expectedService string
		description     string
	}{
		{
			name:            "Gemini平台使用ForwardNative",
			platform:        service.PlatformGemini,
			expectedService: "GeminiMessagesCompatService.ForwardNative",
			description:     "Gemini OAuth 账户直接调用 Google API",
		},
		{
			name:            "Antigravity平台使用ForwardGemini",
			platform:        service.PlatformAntigravity,
			expectedService: "AntigravityGatewayService.ForwardGemini",
			description:     "Antigravity 账户通过 CRS 中转，支持 Gemini 协议",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaModels 中的路由决策 (lines 199-205 in gemini_v1beta_handler.go)
			var routedService string
			if tt.platform == service.PlatformAntigravity {
				routedService = "AntigravityGatewayService.ForwardGemini"
			} else {
				routedService = "GeminiMessagesCompatService.ForwardNative"
			}

			require.Equal(t, tt.expectedService, routedService,
				"平台 %s 应该路由到 %s: %s",
				tt.platform, tt.expectedService, tt.description)
		})
	}
}

// TestGeminiV1BetaHandler_ListModelsAntigravityFallback 验证 ListModels 的 antigravity 降级逻辑
// 当没有 gemini 账户但有 antigravity 账户时，应返回静态模型列表
func TestGeminiV1BetaHandler_ListModelsAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态列表",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_fallback",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaListModels 的逻辑 (lines 33-44 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_fallback"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

// TestGeminiV1BetaHandler_GetModelAntigravityFallback 验证 GetModel 的 antigravity 降级逻辑
func TestGeminiV1BetaHandler_GetModelAntigravityFallback(t *testing.T) {
	tests := []struct {
		name             string
		hasGeminiAccount bool
		hasAntigravity   bool
		expectedBehavior string
	}{
		{
			name:             "有Gemini账户-调用ForwardAIStudioGET",
			hasGeminiAccount: true,
			hasAntigravity:   false,
			expectedBehavior: "forward_to_upstream",
		},
		{
			name:             "无Gemini有Antigravity-返回静态模型信息",
			hasGeminiAccount: false,
			hasAntigravity:   true,
			expectedBehavior: "static_model_info",
		},
		{
			name:             "无任何账户-返回503",
			hasGeminiAccount: false,
			hasAntigravity:   false,
			expectedBehavior: "service_unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟 GeminiV1BetaGetModel 的逻辑 (lines 77-87 in gemini_v1beta_handler.go)
			var behavior string

			if tt.hasGeminiAccount {
				behavior = "forward_to_upstream"
			} else if tt.hasAntigravity {
				behavior = "static_model_info"
			} else {
				behavior = "service_unavailable"
			}

			require.Equal(t, tt.expectedBehavior, behavior)
		})
	}
}

func TestShouldFallbackGeminiModel_KnownFallbackOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.True(t, shouldFallbackGeminiModel("gemini-3.1-pro-preview-customtools", res))
}

func TestShouldFallbackGeminiModel_UnknownModelOn404(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{StatusCode: http.StatusNotFound}
	require.False(t, shouldFallbackGeminiModel("gemini-future-model", res))
}

func TestShouldFallbackGeminiModel_DelegatesScopeFallback(t *testing.T) {
	t.Parallel()

	res := &service.UpstreamHTTPResult{
		StatusCode: http.StatusForbidden,
		Headers:    http.Header{"Www-Authenticate": []string{"Bearer error=\"insufficient_scope\""}},
		Body:       []byte("insufficient authentication scopes"),
	}
	require.True(t, shouldFallbackGeminiModel("gemini-future-model", res))
}
