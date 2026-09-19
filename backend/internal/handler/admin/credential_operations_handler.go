package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CredentialOperationsHandler struct {
	lifecycle service.CredentialInstanceLifecycle
	ops       service.CredentialOperations
	enabled   bool
}

func NewCredentialOperationsHandler(ops service.CredentialOperations, cfg *config.Config, lifecycle service.CredentialInstanceLifecycle) *CredentialOperationsHandler {
	return &CredentialOperationsHandler{ops: ops, enabled: cfg.Gateway.MultiCredentialHTTPEnabled, lifecycle: lifecycle}
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
	switch err.Error() {
	case "CREDENTIAL_OWNERSHIP_MISMATCH", "CREDENTIAL_DUPLICATE", "CREDENTIAL_UNVERIFIED", "CREDENTIAL_IMPORT_EXPIRED", "IDEMPOTENCY_PAYLOAD_MISMATCH", "INSTANCE_EXIT_PENDING", "GROUPED_ROUTE_MIXED_UNSUPPORTED":
		response.ErrorWithDetails(c, 409, "Instance operation requires attention", err.Error(), nil)
		return
	}
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var in service.PrincipalControlUpdate
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid configuration")
		return
	}
	in.Activate = h.enabled && in.AdminState == "ACTIVE"
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
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

func (h *CredentialOperationsHandler) Add(c *gin.Context) {
	actor, principal, version, ok := credentialControlParams(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var in service.CredentialInstanceAddInput
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid instance")
		return
	}
	id, err := h.lifecycle.AddCredentialInstance(c.Request.Context(), actor, principal, version, c.GetHeader("Idempotency-Key"), in)
	if err != nil {
		credentialControlErrorResponse(c, err)
		return
	}
	response.Created(c, gin.H{"instance_id": id, "principal_id": principal})
}
