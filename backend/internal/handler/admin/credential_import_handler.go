package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CredentialImportHandler struct {
	refresh *service.CredentialRefreshCoordinator
	imports *service.CredentialImportService
	creator service.CredentialPrincipalCreator
}

func NewCredentialImportHandler(imports *service.CredentialImportService, creator service.CredentialPrincipalCreator, refresh *service.CredentialRefreshCoordinator) *CredentialImportHandler {
	return &CredentialImportHandler{imports: imports, creator: creator, refresh: refresh}
}
func (h *CredentialImportHandler) Import(c *gin.Context) {
	owner, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || owner.UserID <= 0 {
		response.Unauthorized(c, "Authorization required")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 70<<10)
	var input struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid credential import")
		return
	}
	view, err := h.imports.Import(c.Request.Context(), owner.UserID, c.GetHeader("Idempotency-Key"), service.CredentialSecret{AccessToken: input.AccessToken, RefreshToken: input.RefreshToken, ClientID: input.ClientID})
	if err != nil {
		credentialImportError(c, err)
		return
	}
	response.Accepted(c, view)
}
func (h *CredentialImportHandler) Get(c *gin.Context) {
	owner, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || owner.UserID <= 0 {
		response.Unauthorized(c, "Authorization required")
		return
	}
	view, err := h.imports.Get(c.Request.Context(), owner.UserID, c.Param("id"))
	if err != nil {
		credentialImportError(c, err)
		return
	}
	response.Success(c, view)
}
func (h *CredentialImportHandler) CreatePrincipal(c *gin.Context) {
	owner, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || owner.UserID <= 0 {
		response.Unauthorized(c, "Authorization required")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var input service.CreateCredentialPrincipalInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid principal configuration")
		return
	}
	id, err := h.creator.CreateCredentialPrincipal(c.Request.Context(), owner.UserID, c.GetHeader("Idempotency-Key"), input)
	if err != nil {
		credentialImportError(c, err)
		return
	}
	response.Created(c, gin.H{"id": id, "admission_state": "PAUSED", "routing_mode": "OFF"})
}
func credentialImportError(c *gin.Context, err error) {
	status, reason := 503, "CREDENTIAL_STORE_UNAVAILABLE"
	switch {
	case errors.Is(err, service.ErrCredentialDuplicate):
		status, reason = 409, "CREDENTIAL_DUPLICATE"
	case errors.Is(err, service.ErrCredentialConflict):
		status, reason = 409, "IDEMPOTENCY_PAYLOAD_MISMATCH"
	case errors.Is(err, service.ErrCredentialNotFound):
		status, reason = 404, "CREDENTIAL_NOT_FOUND"
	case errors.Is(err, service.ErrCredentialImportExpired):
		status, reason = 409, "CREDENTIAL_IMPORT_EXPIRED"
	case errors.Is(err, service.ErrCredentialUnverified):
		status, reason = 409, "CREDENTIAL_UNVERIFIED"
	case errors.Is(err, service.ErrCredentialVaultUnavailable):
		reason = "CREDENTIAL_VAULT_UNAVAILABLE"
	}
	response.ErrorWithDetails(c, status, "Credential operation unavailable", reason, nil)
}

func (h *CredentialImportHandler) Refresh(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid instance ID")
		return
	}
	if err = h.refresh.Refresh(c.Request.Context(), id); err != nil {
		credentialImportError(c, err)
		return
	}
	response.Success(c, gin.H{"state": "REFRESHED"})
}
