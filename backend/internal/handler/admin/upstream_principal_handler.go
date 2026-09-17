package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UpstreamPrincipalHandler struct {
	reader  service.UpstreamPrincipalReader
	enabled bool
}

func NewUpstreamPrincipalHandler(reader service.UpstreamPrincipalReader, cfg *config.Config) *UpstreamPrincipalHandler {
	return &UpstreamPrincipalHandler{reader: reader, enabled: cfg != nil && cfg.Gateway.MultiCredentialHTTPEnabled}
}

func (h *UpstreamPrincipalHandler) List(c *gin.Context) {
	after, err := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	if err != nil || after < 0 {
		response.ErrorWithDetails(c, http.StatusBadRequest, "Invalid cursor", "INVALID_CURSOR", nil)
		return
	}
	items, err := h.reader.ListPrincipals(c.Request.Context(), service.DeploymentPrincipalScope, after, 100)
	if err != nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Principal store unavailable", "ADMISSION_STORE_UNAVAILABLE", nil)
		return
	}
	for i := range items {
		items[i].ComputeCapacityView(h.enabled)
	}
	response.Success(c, gin.H{"items": items, "http_enabled": h.enabled})
}

func (h *UpstreamPrincipalHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorWithDetails(c, http.StatusBadRequest, "Invalid principal ID", "INVALID_ID", nil)
		return
	}
	p, err := h.reader.GetPrincipal(c.Request.Context(), service.DeploymentPrincipalScope, id)
	if err != nil {
		response.ErrorWithDetails(c, http.StatusServiceUnavailable, "Principal store unavailable", "ADMISSION_STORE_UNAVAILABLE", nil)
		return
	}
	if p == nil {
		response.ErrorWithDetails(c, http.StatusNotFound, "Principal not found", "NOT_FOUND", nil)
		return
	}
	p.ComputeCapacityView(h.enabled)
	response.Success(c, p)
}
