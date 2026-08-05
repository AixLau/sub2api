package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterMerchantSSOAPIRoutes(v1 *gin.RouterGroup, h *handler.MerchantSSOAPIHandler) {
	route := v1.Group("/merchant-sso")
	route.Use(h.Authenticate)
	route.POST("/register-login", h.RegisterLogin)
	route.POST("/login", h.Login)
	route.POST("/recharge-records", h.RechargeRecords)
}
