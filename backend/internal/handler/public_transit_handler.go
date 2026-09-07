package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type PublicTransitHandler struct{ svc *service.PublicTransitService }

func NewPublicTransitHandler(s *service.PublicTransitService) *PublicTransitHandler {
	return &PublicTransitHandler{svc: s}
}
func (h *PublicTransitHandler) Discovery(c *gin.Context) {
	v, e := h.svc.Discovery(c.Request.Context(), "")
	if e != nil {
		c.Status(500)
		return
	}
	c.JSON(http.StatusOK, v)
}
func (h *PublicTransitHandler) Snapshot(c *gin.Context) {
	v, e := h.svc.Snapshot(c.Request.Context(), "")
	if e != nil {
		c.Status(500)
		return
	}
	c.Header("Cache-Control", "public, max-age=60")
	c.JSON(http.StatusOK, v)
}
