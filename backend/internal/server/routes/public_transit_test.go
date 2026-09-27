package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicTransitRouteSettings struct {
	service.SettingRepository
	value string
}

func (s publicTransitRouteSettings) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyPublicTransitEnabled {
		panic("unexpected setting")
	}
	return s.value, nil
}

func TestPublicTransitV2RouteUsesSnapshotHandlerAndFeatureGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name    string
		enabled string
		status  int
	}{
		{"enabled validates range", "true", http.StatusBadRequest},
		{"disabled hides endpoint", "false", http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := service.NewPublicTransitService(service.NewChannelMonitorV2Service(nil), service.NewGroupService(nil, nil))
			settings := service.NewSettingService(publicTransitRouteSettings{value: test.enabled}, &config.Config{})
			router := gin.New()
			RegisterPublicTransitRoutes(router, router.Group("/api/v1"), &handler.Handlers{
				PublicTransit: handler.NewPublicTransitHandler(svc, settings),
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/public/transit/v2/snapshot?range=invalid", nil))
			require.Equal(t, test.status, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			if test.status == http.StatusBadRequest {
				require.Contains(t, recorder.Body.String(), "invalid public transit range")
			}
		})
	}
}
