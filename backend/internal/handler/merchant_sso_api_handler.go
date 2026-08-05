package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// MerchantSSOAPIHandler is the inbound, server-to-server merchant contract.
type MerchantSSOAPIHandler struct {
	service *service.MerchantSSOAPIService
}

func NewMerchantSSOAPIHandler(svc *service.MerchantSSOAPIService) *MerchantSSOAPIHandler {
	return &MerchantSSOAPIHandler{service: svc}
}

func (h *MerchantSSOAPIHandler) Authenticate(c *gin.Context) {
	key := strings.TrimSpace(c.GetHeader("X-API-Key"))
	if key == "" {
		auth := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(auth, "Bearer ") {
			key = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	if err := h.service.AuthenticateAPIKey(key); err != nil {
		response.ErrorFrom(c, err)
		c.Abort()
		return
	}
	c.Next()
}

func (h *MerchantSSOAPIHandler) RegisterLogin(c *gin.Context)   { h.run(c, h.service.RegisterLogin) }
func (h *MerchantSSOAPIHandler) Login(c *gin.Context)           { h.run(c, h.service.Login) }
func (h *MerchantSSOAPIHandler) RechargeRecords(c *gin.Context) { h.run(c, h.service.RechargeRecords) }

func (h *MerchantSSOAPIHandler) run(c *gin.Context, call func(context.Context, service.MerchantSSOAPIRequest) (*service.MerchantSSOAPIResult, error)) {
	var req service.MerchantSSOAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := call(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": "0", "message": "ok", "data": result})
}
