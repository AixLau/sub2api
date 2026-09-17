package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"strings"
)

type CredentialOperationsHandler struct {
	ops     service.CredentialOperations
	enabled bool
}

func NewCredentialOperationsHandler(ops service.CredentialOperations, cfg *config.Config) *CredentialOperationsHandler {
	return &CredentialOperationsHandler{ops: ops, enabled: cfg.Gateway.MultiCredentialHTTPEnabled}
}
func credentialControlParams(c *gin.Context) (int64, int64, int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authorization required")
		return 0, 0, 0, false
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid ID")
		return 0, 0, 0, false
	}
	header := c.GetHeader("If-Match")
	if !strings.HasPrefix(header, "\"v") || !strings.HasSuffix(header, "\"") {
		response.ErrorWithDetails(c, 428, "If-Match required", "CONFIG_VERSION_REQUIRED", nil)
		return 0, 0, 0, false
	}
	version, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(header, "\"v"), "\""), 10, 64)
	if err != nil || version <= 0 {
		response.BadRequest(c, "Invalid If-Match")
		return 0, 0, 0, false
	}
	return subject.UserID, id, version, true
}
func credentialControlErrorResponse(c *gin.Context, err error) {
	if err.Error() == "CONFIG_VERSION_CONFLICT" {
		response.ErrorWithDetails(c, 412, "Configuration changed", "CONFIG_VERSION_CONFLICT", nil)
		return
	}
	response.ErrorWithDetails(c, 409, "Control operation could not be applied", "CONTROL_OPERATION_REJECTED", nil)
}
func (h *CredentialOperationsHandler) Principal(c *gin.Context) {
	actor, id, version, ok := credentialControlParams(c)
	if !ok {
		return
	}
	var in service.PrincipalControlUpdate
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid configuration")
		return
	}
	v, err := h.ops.UpdatePrincipal(c.Request.Context(), actor, id, version, in)
	if err != nil {
		credentialControlErrorResponse(c, err)
		return
	}
	v.ComputeCapacityView(h.enabled)
	c.Header("ETag", `"v`+strconv.FormatInt(v.ConfigVersion, 10)+`"`)
	if v.Overhang > 0 {
		response.Accepted(c, v)
	} else {
		response.Success(c, v)
	}
}
func (h *CredentialOperationsHandler) Instance(c *gin.Context) {
	actor, id, version, ok := credentialControlParams(c)
	if !ok {
		return
	}
	var in service.InstanceControlUpdate
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid configuration")
		return
	}
	v, err := h.ops.UpdateInstance(c.Request.Context(), actor, id, version, in)
	if err != nil {
		credentialControlErrorResponse(c, err)
		return
	}
	v.ComputeCapacityView(h.enabled)
	response.Accepted(c, v)
}
func (h *CredentialOperationsHandler) Runtime(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid ID")
		return
	}
	v, err := h.ops.CredentialRuntime(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, "Runtime unavailable")
		return
	}
	response.Success(c, v)
}
func (h *CredentialOperationsHandler) Resolve(c *gin.Context) {
	actor, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authorization required")
		return
	}
	var in service.CredentialResolveInput
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid resolution")
		return
	}
	if err := h.ops.ResolveCredentialLease(c.Request.Context(), actor.UserID, c.Param("id"), in); err != nil {
		credentialControlErrorResponse(c, err)
		return
	}
	response.Success(c, gin.H{"state": "RELEASED", "accepted_unknown_risk": in.AcceptUnknownRisk})
}
