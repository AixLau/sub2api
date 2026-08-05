package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// MerchantSSOHandler exposes merchant integration configuration and operational
// tools to administrators.
type MerchantSSOHandler struct {
	merchantService *service.MerchantSSOService
}

func NewMerchantSSOHandler(merchantService *service.MerchantSSOService) *MerchantSSOHandler {
	return &MerchantSSOHandler{merchantService: merchantService}
}

func (h *MerchantSSOHandler) ListIntegrations(c *gin.Context) {
	includeDisabled, _ := strconv.ParseBool(c.DefaultQuery("include_disabled", "true"))
	items, err := h.merchantService.ListIntegrations(c.Request.Context(), includeDisabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *MerchantSSOHandler) CreateIntegration(c *gin.Context) {
	var input service.MerchantIntegrationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	item, err := h.merchantService.CreateIntegration(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, item)
}

func (h *MerchantSSOHandler) GetIntegration(c *gin.Context) {
	id, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	item, err := h.merchantService.GetIntegration(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *MerchantSSOHandler) UpdateIntegration(c *gin.Context) {
	id, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	var input service.MerchantIntegrationInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	item, err := h.merchantService.UpdateIntegration(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *MerchantSSOHandler) SetIntegrationEnabled(c *gin.Context) {
	id, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	enabled, valid := merchantEnabledBody(c)
	if !valid {
		return
	}
	item, err := h.merchantService.SetIntegrationEnabled(c.Request.Context(), id, enabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *MerchantSSOHandler) DeleteIntegration(c *gin.Context) {
	id, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	if err := h.merchantService.DeleteIntegration(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true, "id": id})
}

func (h *MerchantSSOHandler) CreateEndpoint(c *gin.Context) {
	integrationID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	var input service.MerchantAPIEndpointInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	item, err := h.merchantService.CreateEndpoint(c.Request.Context(), integrationID, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, item)
}

func (h *MerchantSSOHandler) UpdateEndpoint(c *gin.Context) {
	integrationID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	endpointID, ok := merchantPathID(c, "endpoint_id")
	if !ok {
		return
	}
	var input service.MerchantAPIEndpointInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	item, err := h.merchantService.UpdateEndpoint(c.Request.Context(), integrationID, endpointID, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *MerchantSSOHandler) SetEndpointEnabled(c *gin.Context) {
	integrationID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	endpointID, ok := merchantPathID(c, "endpoint_id")
	if !ok {
		return
	}
	enabled, valid := merchantEnabledBody(c)
	if !valid {
		return
	}
	item, err := h.merchantService.SetEndpointEnabled(c.Request.Context(), integrationID, endpointID, enabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *MerchantSSOHandler) DeleteEndpoint(c *gin.Context) {
	integrationID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	endpointID, ok := merchantPathID(c, "endpoint_id")
	if !ok {
		return
	}
	if err := h.merchantService.DeleteEndpoint(c.Request.Context(), integrationID, endpointID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true, "id": endpointID})
}

func (h *MerchantSSOHandler) TestEndpoint(c *gin.Context) {
	integrationID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	endpointID, ok := merchantPathID(c, "endpoint_id")
	if !ok {
		return
	}
	var input struct {
		UserID    int64  `json:"user_id"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&input); err != nil && !strings.Contains(err.Error(), "EOF") {
			response.BadRequest(c, err.Error())
			return
		}
	}
	result, err := h.merchantService.TestEndpoint(c.Request.Context(), integrationID, endpointID, input.UserID, service.MerchantRechargeQuery{
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *MerchantSSOHandler) ListUserBindings(c *gin.Context) {
	userID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	items, err := h.merchantService.ListBindingsByUser(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *MerchantSSOHandler) ListUserRechargeRecords(c *gin.Context) {
	userID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	bindingID, ok := merchantPathID(c, "binding_id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.merchantService.ListRechargeRecords(c.Request.Context(), userID, bindingID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *MerchantSSOHandler) SyncUserRechargeRecords(c *gin.Context) {
	userID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	bindingID, ok := merchantPathID(c, "binding_id")
	if !ok {
		return
	}
	var input struct {
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if c.Request.Body != nil {
		if err := c.ShouldBindJSON(&input); err != nil && !strings.Contains(err.Error(), "EOF") {
			response.BadRequest(c, err.Error())
			return
		}
	}
	result, err := h.merchantService.SyncRechargeRecords(c.Request.Context(), userID, bindingID, service.MerchantRechargeQuery{
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *MerchantSSOHandler) SyncUserBinding(c *gin.Context) {
	h.runBindingAction(c, func(userID, bindingID int64) (any, error) {
		return h.merchantService.SyncBinding(c.Request.Context(), userID, bindingID)
	})
}
func (h *MerchantSSOHandler) BindUserBinding(c *gin.Context) {
	h.runBindingAction(c, func(userID, bindingID int64) (any, error) {
		return h.merchantService.BindBinding(c.Request.Context(), userID, bindingID)
	})
}
func (h *MerchantSSOHandler) StatusUserBinding(c *gin.Context) {
	h.runBindingAction(c, func(userID, bindingID int64) (any, error) {
		return h.merchantService.StatusBinding(c.Request.Context(), userID, bindingID)
	})
}

func (h *MerchantSSOHandler) runBindingAction(c *gin.Context, action func(int64, int64) (any, error)) {
	userID, ok := merchantPathID(c, "id")
	if !ok {
		return
	}
	bindingID, ok := merchantPathID(c, "binding_id")
	if !ok {
		return
	}
	result, err := action(userID, bindingID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func merchantPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

func merchantEnabledBody(c *gin.Context) (bool, bool) {
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		response.BadRequest(c, "enabled is required")
		return false, false
	}
	return *input.Enabled, true
}
