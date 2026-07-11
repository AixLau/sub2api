package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestContentModerationMetricsHandlerExportsPrometheusText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetModerationMetrics(service.NewContentModerationMetrics())
	handler := NewContentModerationHandler(svc)
	router := gin.New()
	router.GET("/admin/risk-control/metrics", handler.GetMetrics)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/risk-control/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/plain")
	require.Contains(t, recorder.Body.String(), "sub2api_moderation_")
}
