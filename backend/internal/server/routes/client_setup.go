package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterClientSetupRoutes(
	v1 *gin.RouterGroup,
	h *handler.ClientSetupHandler,
	jwtAuth middleware.JWTAuthMiddleware,
) {
	if h == nil {
		return
	}

	clientSetup := v1.Group("/client-setup")
	{
		clientSetup.POST("/sessions", h.CreateSession)
		clientSetup.GET("/sessions/:setup_id", h.GetSession)
		clientSetup.POST("/exchange", h.Exchange)

		authenticated := clientSetup.Group("")
		authenticated.Use(gin.HandlerFunc(jwtAuth))
		{
			authenticated.POST("/sessions/:setup_id/approve", h.ApproveSession)
		}
	}
}
