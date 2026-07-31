package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRewardCampaignFeatureGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name       string
		value      string
		wantStatus int
	}{
		{name: "missing defaults off", wantStatus: http.StatusNotFound},
		{name: "disabled", value: "false", wantStatus: http.StatusNotFound},
		{name: "enabled", value: "true", wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			if tc.value != "" {
				values[service.SettingKeyRewardCampaignsEnabled] = tc.value
			}
			svc := service.NewSettingService(&complianceGuardRepoStub{values: values}, &config.Config{})
			router := gin.New()
			router.Use(RewardCampaignFeatureGuard(svc))
			router.GET("/rewards", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rewards", nil))
			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}
