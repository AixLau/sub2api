package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CredentialMigrationHandler struct {
	store service.CredentialMigrationStore
}

func NewCredentialMigrationHandler(store service.CredentialMigrationStore) *CredentialMigrationHandler {
	return &CredentialMigrationHandler{store: store}
}
func (h *CredentialMigrationHandler) Preview(c *gin.Context) {
	var in struct {
		AccountIDs []int64 `json:"account_ids"`
	}
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "Invalid account IDs")
		return
	}
	result, err := h.store.PreviewCredentialMigration(c.Request.Context(), in.AccountIDs)
	if err != nil {
		response.Error(c, 503, "Migration preview unavailable")
		return
	}
	response.Success(c, gin.H{"items": result, "activation_available": false})
}
func (h *CredentialMigrationHandler) Rollback(c *gin.Context) {
	actor, id, version, ok := credentialControlParams(c)
	if !ok {
		return
	}
	if err := h.store.PrepareCredentialRollback(c.Request.Context(), actor, id, version); err != nil {
		credentialControlErrorResponse(c, err)
		return
	}
	response.Accepted(c, gin.H{"state": "PAUSED", "legacy_routing_enabled": false})
}

func (h *CredentialMigrationHandler) Shadow(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid principal ID")
		return
	}
	demand, err := h.store.ShadowCredentialDecision(c.Request.Context(), id)
	if err != nil {
		response.Error(c, 503, "Shadow decision unavailable")
		return
	}
	response.Success(c, gin.H{"mode": "SHADOW", "demand": demand, "execution_permitted": false})
}
