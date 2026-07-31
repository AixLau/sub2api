package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// RewardCampaignFeatureGuard hides the new campaign surface while an operator
// rolls it out. Legacy reward adapters deliberately remain outside this guard.
func RewardCampaignFeatureGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || !settingService.IsRewardCampaignsEnabled(c.Request.Context()) {
			response.NotFound(c, "Reward campaigns are not enabled")
			c.Abort()
			return
		}
		c.Next()
	}
}
