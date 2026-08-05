package handler

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// MerchantSSOHandler handles the authenticated user-side merchant entry point.
type MerchantSSOHandler struct {
	merchantService *service.MerchantSSOService
}

func NewMerchantSSOHandler(merchantService *service.MerchantSSOService) *MerchantSSOHandler {
	return &MerchantSSOHandler{merchantService: merchantService}
}

func (h *MerchantSSOHandler) ListIntegrations(c *gin.Context) {
	items, err := h.merchantService.ListPublicIntegrations(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *MerchantSSOHandler) Launch(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	integrationID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || integrationID <= 0 {
		response.BadRequest(c, "invalid integration id")
		return
	}
	result, err := h.merchantService.Launch(c.Request.Context(), integrationID, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *MerchantSSOHandler) ListBindings(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	items, err := h.merchantService.ListBindingsByUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *MerchantSSOHandler) ListRechargeRecords(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	bindingID, err := strconv.ParseInt(c.Param("binding_id"), 10, 64)
	if err != nil || bindingID <= 0 {
		response.BadRequest(c, "invalid binding id")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.merchantService.ListRechargeRecords(c.Request.Context(), subject.UserID, bindingID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *MerchantSSOHandler) SyncRechargeRecords(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	bindingID, err := strconv.ParseInt(c.Param("binding_id"), 10, 64)
	if err != nil || bindingID <= 0 {
		response.BadRequest(c, "invalid binding id")
		return
	}
	var input struct {
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if err := c.ShouldBindJSON(&input); err != nil && c.Request.ContentLength != 0 {
		response.BadRequest(c, err.Error())
		return
	}
	result, err := h.merchantService.SyncRechargeRecords(c.Request.Context(), subject.UserID, bindingID, service.MerchantRechargeQuery{
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
