package admin

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

// GetRPM reads only the requested page's counters; it never runs account scoring.
func (h *AccountHandler) GetRPM(c *gin.Context) {
	var req struct {
		AccountIDs []int64 `json:"account_ids" binding:"required,min=1,max=1000,dive,gt=0"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "account_ids must contain 1 to 1000 positive IDs")
		return
	}
	ids := make([]int64, 0, len(req.AccountIDs))
	seen := make(map[int64]bool, len(req.AccountIDs))
	for _, id := range req.AccountIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if h.rpmCache == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account RPM is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	counts, err := h.rpmCache.GetRecentRPMBatch(ctx, ids)
	if err != nil {
		_ = c.Error(err)
		response.Error(c, http.StatusServiceUnavailable, "Account RPM is unavailable")
		return
	}
	// A successful metrics poll is a read, not an account mutation.
	middleware.SkipAudit(c)
	response.Success(c, gin.H{"rpm": counts})
}
