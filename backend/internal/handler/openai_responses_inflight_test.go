//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponses_RetainsInflightReservationThroughRouting(t *testing.T) {
	cache := newHandlerInflightCache(10)
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := newOpenAIHandlerForPreviousResponseIDValidation(t, nil)
	h.billingCacheService = billing
	h.gatewayService = service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h.stageAdapterRegistry = NewStageAdapterRegistry()
	h.stageAdapterRegistry.RegisterBilling(moderationcoverage.RouteAdapterDescriptor{
		Stage: moderationcoverage.StageBilling, Pipeline: moderationcoverage.PipelineOpenAIHTTP, Name: "OpenAIHTTPBillingStage",
	}, BillingStageAdapter{Name: "OpenAIHTTPBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
		return ExecutableStageResult{}
	}})
	var task service.UsageRecordTask
	var abandon func()
	ran := false
	h.stageAdapterRegistry.RegisterRouting(moderationcoverage.RouteAdapterDescriptor{
		Stage: moderationcoverage.StageRouting, Pipeline: moderationcoverage.PipelineOpenAIHTTP, Name: "OpenAIHTTPRoutingStage",
	}, RoutingStageAdapter{Name: "OpenAIHTTPRoutingStage", Routing: func(c *gin.Context) ExecutableStageResult {
		require.NotNil(t, service.InflightReservationFromContext(c.Request.Context()))
		require.Equal(t, 1, cache.count())
		task, abandon = wrapUsageRecordTaskContext(c.Request.Context(), func(context.Context) { ran = true })
		c.Status(http.StatusNoContent)
		return ExecutableStageResult{Stop: true}
	}})
	c, recorder := newOpenAIResponsesFailoverTestContext(t, nil)
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	apiKey.Group.RateMultiplier = 1
	apiKey.User.Balance = 10

	h.Responses(c)

	require.Equal(t, http.StatusNoContent, c.Writer.Status(), recorder.Body.String())
	require.NotNil(t, task, "the request must reach routing with its reservation")
	defer abandon()
	require.Equal(t, 1, cache.count(), "handler return must not release pending usage reservation")
	task(context.Background())
	require.True(t, ran)
	require.Zero(t, cache.count(), "usage completion releases the reservation")
}
