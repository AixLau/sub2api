package routes

import (
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const moderatedRouteStatusCovered = "covered"

type routeRegistrar interface {
	GET(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
	POST(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
}

// ModeratedRouteMeta describes an upstream route that must remain covered by
// the gateway content moderation guard before upstream forwarding.
type ModeratedRouteMeta struct {
	Method             string
	Path               string
	Handler            string
	Upstream           bool
	ModerationRequired bool
	Protocol           string
	Status             string
	ReviewReason       string
}

type ModeratedRouteRegistrar struct {
	routes routeRegistrar
}

var moderatedRouteRegistry = struct {
	sync.Mutex
	entries []ModeratedRouteMeta
}{}

func NewModeratedRouteRegistrar(routes routeRegistrar) *ModeratedRouteRegistrar {
	return &ModeratedRouteRegistrar{routes: routes}
}

func (r *ModeratedRouteRegistrar) GET(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
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
	moderatedRouteRegistry.Lock()
	defer moderatedRouteRegistry.Unlock()

	entriesByKey := make(map[string]ModeratedRouteMeta, len(moderatedRouteRegistry.entries))
	for _, entry := range moderatedRouteRegistry.entries {
		normalized := normalizeModeratedRouteMeta(entry)
		key := moderatedRouteMetaKey(normalized)
		if key == "" {
			continue
		}
		entriesByKey[key] = normalized
	}

	keys := make([]string, 0, len(entriesByKey))
	for key := range entriesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]ModeratedRouteMeta, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, entriesByKey[key])
	}
	return entries
}

func resetModeratedRouteRegistryForTest() {
	moderatedRouteRegistry.Lock()
	defer moderatedRouteRegistry.Unlock()
	moderatedRouteRegistry.entries = nil
}

func registerModeratedRoute(meta ModeratedRouteMeta) {
	normalized := normalizeModeratedRouteMeta(meta)
	moderatedRouteRegistry.Lock()
	defer moderatedRouteRegistry.Unlock()
	moderatedRouteRegistry.entries = append(moderatedRouteRegistry.entries, normalized)
}

func normalizeModeratedRouteMeta(meta ModeratedRouteMeta) ModeratedRouteMeta {
	meta.Method = strings.ToUpper(strings.TrimSpace(meta.Method))
	meta.Path = strings.TrimSpace(meta.Path)
	meta.Handler = strings.TrimSpace(meta.Handler)
	meta.Protocol = strings.TrimSpace(meta.Protocol)
	meta.Status = strings.ToLower(strings.TrimSpace(meta.Status))
	meta.ReviewReason = strings.TrimSpace(meta.ReviewReason)
	return meta
}

func moderatedRouteMetaKey(meta ModeratedRouteMeta) string {
	if strings.TrimSpace(meta.Method) == "" || strings.TrimSpace(meta.Path) == "" {
		return ""
	}
	return meta.Method + " " + meta.Path + " " + meta.Protocol
}

func coveredModeratedRoute(path, handlerName, protocol, reviewReason string) ModeratedRouteMeta {
	return ModeratedRouteMeta{
		Path:               path,
		Handler:            handlerName,
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           protocol,
		Status:             moderatedRouteStatusCovered,
		ReviewReason:       reviewReason,
	}
}
