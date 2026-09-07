package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type PublicTransitHandler struct {
	svc      *service.PublicTransitService
	v2       *ChannelMonitorV2Handler
	settings *service.SettingService
}

func NewPublicTransitHandler(s *service.PublicTransitService, v2 *ChannelMonitorV2Handler, settings *service.SettingService) *PublicTransitHandler {
	return &PublicTransitHandler{svc: s, v2: v2, settings: settings}
}
func (h *PublicTransitHandler) enabled(c *gin.Context) bool {
	return h.settings == nil || h.settings.PublicTransitEnabled(c.Request.Context())
}
func (h *PublicTransitHandler) Discovery(c *gin.Context) {
	if !h.enabled(c) {
		c.Status(http.StatusNotFound)
		return
	}
	v, e := h.svc.Discovery(c.Request.Context(), "")
	if e != nil {
		c.Status(500)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (h *PublicTransitHandler) Snapshot(c *gin.Context) {
	if !h.enabled(c) {
		c.Status(http.StatusNotFound)
		return
	}
	if h.v2 != nil {
		c.Header("Cache-Control", "public, max-age=60")
		h.v2.PublicSnapshot(c)
		return
	}
	v, e := h.svc.Snapshot(c.Request.Context(), "")
	if e != nil {
		c.Status(500)
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	c.JSON(http.StatusOK, v)
}
