//go:build unit

package handler

// 槽位终检与生图跳门回归（handler 半程）：
//   - 槽位获取成功后的利润终检：越线账号释放槽位并要求调用方排除重选，
//     不写响应、不绑定粘连；
//   - openAIResponsesRequiredCapability 的请求能力映射覆盖生图与原生远程压缩。

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type profitCountingConcurrencyCache struct {
	fakeConcurrencyCache
	accountReleases atomic.Int64
}

func (c *profitCountingConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error {
	c.accountReleases.Add(1)
	return nil
}

func profitSlotTestAccount(id int64, rate float64) *service.Account {
	now := time.Now()
	return &service.Account{
		ID:             id,
		Platform:       service.PlatformOpenAI,
		Type:           service.AccountTypeAPIKey,
		Status:         service.StatusActive,
		Schedulable:    true,
		Concurrency:    2,
		RateMultiplier: &rate,
		Extra: map[string]any{
			"upstream_billing_probe": map[string]any{
				"status":      service.UpstreamBillingProbeStatusOK,
				"received_at": now.Add(-time.Minute),
				"fresh_until": now.Add(30 * time.Minute),
				"data": map[string]any{
					"billing_scope":            "token",
					"resolved_rate_multiplier": rate,
					"peak_rate_enabled":        false,
				},
			},
		},
	}
}

func profitSlotTestContext(t *testing.T, gw *service.OpenAIGatewayService, groupID int64, suppress bool) context.Context {
	t.Helper()
	group := &service.Group{
		ID:                   groupID,
		Platform:             service.PlatformOpenAI,
		Status:               service.StatusActive,
		Hydrated:             true,
		RateMultiplier:       1.0,
		SubscriptionType:     service.SubscriptionTypeStandard,
		ProfitControlEnabled: true,
		ProfitMinMargin:      0.5,
	}
	base := context.WithValue(context.Background(), ctxkey.Group, group)
	if suppress {
		base = service.WithOpenAIProfitControlSuppressed(base)
	}
	ctx, pricingAt := gw.WithOpenAIRequestPricingContext(base, &groupID)
	require.False(t, pricingAt.IsZero())
	return ctx
}

func TestAcquireResponsesAccountSlotProfitRecheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(50)

	newFixture := func(t *testing.T, accountID int64, selectedRate, latestRate float64, suppress bool) (*OpenAIGatewayHandler, *gin.Context, *httptest.ResponseRecorder, *profitCountingConcurrencyCache, *service.AccountSelectionResult) {
		t.Helper()
		selected := profitSlotTestAccount(accountID, selectedRate)
		repo := &openAIImagesFailoverAccountRepo{accounts: []service.Account{*selected}}
		cfg := &config.Config{RunMode: config.RunModeSimple}
		cache := &profitCountingConcurrencyCache{}
		concurrencyService := service.NewConcurrencyService(cache)
		gw := service.NewOpenAIGatewayService(
			repo, nil, nil, nil, nil, nil, nil, cfg, nil, concurrencyService, nil,
			nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		)
		ctx := profitSlotTestContext(t, gw, groupID, suppress)
		selection, err := gw.SelectAccountWithLoadAwareness(ctx, &groupID, "", "gpt-5.1", nil)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, !suppress, selection.ProfitGateActive())
		repo.accounts[0] = *profitSlotTestAccount(accountID, latestRate)

		h := &OpenAIGatewayHandler{
			gatewayService:    gw,
			concurrencyHelper: NewConcurrencyHelper(concurrencyService, SSEPingFormatClaude, 0),
		}
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
		return h, c, w, cache, selection
	}

	t.Run("veto releases slot and requests reschedule without writing response", func(t *testing.T) {
		h, c, w, cache, selection := newFixture(t, 1, 0.3, 0.8, false)
		streamStarted := false

		release, result := h.acquireResponsesAccountSlot(c, &groupID, "", selection, false, &streamStarted, zap.NewNop())
		require.Equal(t, openAISlotAcquireProfitVetoed, result, "status=%d body=%s releases=%d", w.Code, w.Body.String(), cache.accountReleases.Load())
		require.Nil(t, release)
		require.Zero(t, w.Body.Len(), "利润终检否决不得写出任何响应")
		require.Equal(t, int64(1), cache.accountReleases.Load(), "否决后必须立即释放已获取的槽位")
	})

	t.Run("qualifying account acquires normally", func(t *testing.T) {
		h, c, _, _, selection := newFixture(t, 2, 0.3, 0.3, false)
		streamStarted := false

		release, result := h.acquireResponsesAccountSlot(c, &groupID, "", selection, false, &streamStarted, zap.NewNop())
		require.Equal(t, openAISlotAcquireOK, result)
		require.NotNil(t, release)
		release()
	})

	t.Run("image intent suppression keeps official behavior", func(t *testing.T) {
		h, c, _, _, selection := newFixture(t, 3, 0.3, 0.8, true)
		streamStarted := false

		release, result := h.acquireResponsesAccountSlot(c, &groupID, "", selection, false, &streamStarted, zap.NewNop())
		require.Equal(t, openAISlotAcquireOK, result, "生图意图跳门：过贵账号照常获取（图片边界不装门）")
		require.NotNil(t, release)
		release()
	})
}

func TestOpenAIResponsesRequiredCapabilityForRequest(t *testing.T) {
	require.Equal(t, service.OpenAIEndpointCapabilityResponses, openAIResponsesRequiredCapability(true, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityChatCompletions, openAIResponsesRequiredCapability(false, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityChatCompletions, openAIResponsesRequiredCapability(true, service.PlatformGrok))
	require.Equal(t, service.OpenAIEndpointCapabilityResponses, openAIResponsesRequiredCapabilityForRequest(false, true, service.PlatformOpenAI))
	require.Equal(t, service.OpenAIEndpointCapabilityChatCompletions, openAIResponsesRequiredCapabilityForRequest(false, true, service.PlatformGrok))
}
