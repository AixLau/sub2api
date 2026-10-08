package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountRPMRouteRequiresAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Account: &adminhandler.AccountHandler{}}}
	auth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			servermiddleware.AbortWithError(c, 401, "UNAUTHORIZED", "Authorization required")
			return
		}
		servermiddleware.AbortWithError(c, 403, "FORBIDDEN", "Admin access required")
	})
	audit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, auth, audit, stepUp, nil, nil)
	for _, tc := range []struct {
		auth   string
		status int
	}{{"", 401}, {"Bearer user-token", 403}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/rpm", nil)
		req.Header.Set("Authorization", tc.auth)
		router.ServeHTTP(rec, req)
		require.Equal(t, tc.status, rec.Code)
	}
}
