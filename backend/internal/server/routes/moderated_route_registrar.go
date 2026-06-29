package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
)

type routeRegistrar interface {
	GET(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
	POST(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
}

// ModeratedRouteMeta describes an upstream route that must remain covered by
// the gateway content moderation guard before upstream forwarding.
type ModeratedRouteMeta = moderationcoverage.Entry

type ModeratedRouteRegistrar struct {
	routes routeRegistrar
}

func NewModeratedRouteRegistrar(routes routeRegistrar) *ModeratedRouteRegistrar {
	return &ModeratedRouteRegistrar{routes: routes}
}

func (r *ModeratedRouteRegistrar) GET(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	registerModeratedRoute(meta)
	return r.routes.GET(relativePath, handlers...)
}

func (r *ModeratedRouteRegistrar) GETNoAudit(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	registerModeratedRoute(meta)
	return r.routes.GET(relativePath, handlers...)
}

func (r *ModeratedRouteRegistrar) POST(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "POST"
	registerModeratedRoute(meta)
	return r.routes.POST(relativePath, handlers...)
}

func GatewayModeratedRouteCoverageEntries() []ModeratedRouteMeta {
	return moderationcoverage.Entries()
}

func replaceModeratedRouteRegistryForTest(entries []ModeratedRouteMeta) func() {
	return moderationcoverage.ReplaceRegistryForTest(entries)
}

func registerModeratedRoute(meta ModeratedRouteMeta) {
	moderationcoverage.Register(meta)
}

func coveredModeratedRoute(path, handlerName, protocol, reviewReason string) ModeratedRouteMeta {
	return ModeratedRouteMeta{
		Path:               path,
		Handler:            handlerName,
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           protocol,
		Status:             moderationcoverage.StatusCovered,
		ReviewReason:       reviewReason,
	}
}

func intentionalNoAuditRoute(path, handlerName, reviewReason string) ModeratedRouteMeta {
	return ModeratedRouteMeta{
		Path:               path,
		Handler:            handlerName,
		Upstream:           false,
		ModerationRequired: false,
		Status:             moderationcoverage.StatusIntentionalNoAudit,
		ReviewReason:       reviewReason,
	}
}
