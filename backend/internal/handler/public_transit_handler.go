package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PublicTransitHandler struct {
	svc      publicTransitReader
	settings *service.SettingService
}

type publicTransitReader interface {
	Discovery(context.Context, string) (*service.PublicTransitDiscovery, error)
	Snapshot(context.Context, string) (*service.PublicTransitSnapshot, error)
}

func NewPublicTransitHandler(s *service.PublicTransitService, settings *service.SettingService) *PublicTransitHandler {
	return &PublicTransitHandler{svc: s, settings: settings}
}
func (h *PublicTransitHandler) enabled(c *gin.Context) bool {
	return h.settings == nil || h.settings.PublicTransitEnabled(c.Request.Context())
}
func (h *PublicTransitHandler) Discovery(c *gin.Context) {
	if !h.enabled(c) {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusNotFound)
		return
	}
	v, err := h.svc.Discovery(c.Request.Context(), "")
	if err != nil {
		c.Header("Cache-Control", "no-store")
		response.InternalError(c, "public transit discovery unavailable")
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	c.JSON(http.StatusOK, v)
}
func (h *PublicTransitHandler) Snapshot(c *gin.Context) {
	if !h.enabled(c) {
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusNotFound)
		return
	}
	v, err := h.svc.Snapshot(c.Request.Context(), c.Query("range"))
	if err != nil {
		c.Header("Cache-Control", "no-store")
		switch {
		case errors.Is(err, service.ErrChannelMonitorDisabled):
			c.Status(http.StatusNotFound)
		case errors.Is(err, service.ErrChannelMonitorV2InvalidRange):
			response.BadRequest(c, "invalid public transit range")
		default:
			response.InternalError(c, "public transit snapshot unavailable")
		}
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	c.JSON(http.StatusOK, v)
}
