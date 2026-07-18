package routes

import (
	"fmt"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type routeRegistrar interface {
	GET(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
	POST(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
	DELETE(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes
}

// ModeratedRouteMeta describes an upstream route that must remain covered by
// the gateway content moderation guard before upstream forwarding.
type ModeratedRouteMeta = moderationcoverage.Entry

type GatewayPipelineEntryResult struct {
	Stop bool
}

type GatewayPipelineEntrypoint interface {
	EnterGatewayPipeline(*gin.Context, ModeratedRouteMeta) GatewayPipelineEntryResult
}

type GatewayPipelineEntrypointFunc func(*gin.Context, ModeratedRouteMeta) GatewayPipelineEntryResult

func (f GatewayPipelineEntrypointFunc) EnterGatewayPipeline(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if f == nil {
		return GatewayPipelineEntryResult{}
	}
	return f(c, meta)
}

type GatewayPipelineEntrypoints map[string]GatewayPipelineEntrypoint

type ModeratedRouteRegistrar struct {
	routes                    routeRegistrar
	entrypoints               GatewayPipelineEntrypoints
	requirePipelineEntrypoint bool
}

func NewModeratedRouteRegistrar(routes routeRegistrar) *ModeratedRouteRegistrar {
	return &ModeratedRouteRegistrar{
		routes:      routes,
		entrypoints: nil,
	}
}

func NewGatewayPipelineRegistrar(routes routeRegistrar, entrypoints GatewayPipelineEntrypoints) *ModeratedRouteRegistrar {
	return &ModeratedRouteRegistrar{
		routes:                    routes,
		entrypoints:               normalizeGatewayPipelineEntrypoints(entrypoints),
		requirePipelineEntrypoint: true,
	}
}

func (r *ModeratedRouteRegistrar) GET(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	meta = r.registerRoute(meta)
	return r.routes.GET(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) GETNoAudit(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	meta = r.registerRoute(meta)
	return r.routes.GET(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) POST(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "POST"
	meta = r.registerRoute(meta)
	return r.routes.POST(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) DELETE(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "DELETE"
	meta = r.registerRoute(meta)
	return r.routes.DELETE(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) DELETENoAudit(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "DELETE"
	meta = r.registerRoute(meta)
	return r.routes.DELETE(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func GatewayModeratedRouteCoverageEntries() []ModeratedRouteMeta {
	return moderationcoverage.Entries()
}

func replaceModeratedRouteRegistryForTest(entries []ModeratedRouteMeta) func() {
	return moderationcoverage.ReplaceRegistryForTest(entries)
}

func registerModeratedRoute(meta ModeratedRouteMeta) ModeratedRouteMeta {
	meta = moderationcoverage.AnnotatePipelineCoverage(meta)
	moderationcoverage.Register(meta)
	return meta
}

func (r *ModeratedRouteRegistrar) registerRoute(meta ModeratedRouteMeta) ModeratedRouteMeta {
	meta = moderationcoverage.AnnotatePipelineCoverage(meta)
	r.requireEntrypointForPipelineRoute(meta)
	moderationcoverage.Register(meta)
	return meta
}

func (r *ModeratedRouteRegistrar) requireEntrypointForPipelineRoute(meta ModeratedRouteMeta) {
	if r == nil || !r.requirePipelineEntrypoint {
		return
	}
	meta = moderationcoverage.NormalizeEntry(meta)
	if !meta.Upstream || !meta.ModerationRequired || meta.Pipeline == "" {
		return
	}
	if r.entrypointForPipeline(meta.Pipeline) != nil {
		return
	}
	panic(fmt.Sprintf(
		"gateway pipeline route %s %s %s requires entrypoint for pipeline %s",
		meta.Method,
		meta.Path,
		meta.Handler,
		meta.Pipeline,
	))
}

func (r *ModeratedRouteRegistrar) entrypointForPipeline(pipeline string) GatewayPipelineEntrypoint {
	if r == nil || len(r.entrypoints) == 0 {
		return nil
	}
	pipeline = moderationcoverage.NormalizePipeline(pipeline)
	if entrypoint := r.entrypoints[pipeline]; entrypoint != nil {
		return entrypoint
	}
	return r.entrypoints[moderationcoverage.PipelineGatewayGlobal]
}

func registerModeratedRouteBranch(method string, meta ModeratedRouteMeta) ModeratedRouteMeta {
	meta.Method = method
	return registerModeratedRoute(meta)
}

func setModeratedRouteBranchMeta(c *gin.Context, meta ModeratedRouteMeta) {
	moderationcoverage.SetRouteMeta(c, meta)
}

func enterModeratedRouteBranchPipeline(c *gin.Context, registrar *ModeratedRouteRegistrar, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if registrar == nil {
		setModeratedRouteBranchMeta(c, meta)
		return GatewayPipelineEntryResult{}
	}
	return registrar.EnterBranchPipeline(c, meta)
}

func (r *ModeratedRouteRegistrar) EnterBranchPipeline(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	setModeratedRouteBranchMeta(c, meta)
	if r == nil {
		return GatewayPipelineEntryResult{}
	}
	return r.runGatewayPipelineEntrypoint(c, meta)
}

func normalizeGatewayPipelineEntrypoints(entrypoints GatewayPipelineEntrypoints) GatewayPipelineEntrypoints {
	if len(entrypoints) == 0 {
		return nil
	}
	normalized := make(GatewayPipelineEntrypoints, len(entrypoints))
	for pipeline, entrypoint := range entrypoints {
		pipeline = moderationcoverage.NormalizePipeline(pipeline)
		if pipeline == "" || entrypoint == nil {
			continue
		}
		normalized[pipeline] = entrypoint
	}
	return normalized
}

func (r *ModeratedRouteRegistrar) prependModeratedRouteMetaHandler(meta ModeratedRouteMeta, handlers []gin.HandlerFunc) []gin.HandlerFunc {
	routeMetaHandler := func(c *gin.Context) {
		moderationcoverage.SetRouteMeta(c, meta)
		c.Next()
		enforceModeratedRoutePipelineAdmission(c, meta)
	}
	if len(handlers) == 0 {
		return []gin.HandlerFunc{routeMetaHandler}
	}
	prepended := make([]gin.HandlerFunc, 0, len(handlers)+2)
	prepended = append(prepended, routeMetaHandler)
	if r == nil || len(r.entrypoints) == 0 {
		prepended = append(prepended, handlers...)
		return prepended
	}
	prepended = append(prepended, handlers[:len(handlers)-1]...)
	prepended = append(prepended, func(c *gin.Context) {
		// Resolve the request context when the deferred function runs. The
		// pipeline entrypoint replaces c.Request with a context containing the
		// resource reservation; evaluating c.Request.Context() while registering
		// the defer captures the old context and leaks that reservation.
		defer func() {
			service.ReleaseRequestResources(c.Request.Context())
		}()
		if r.runGatewayPipelineEntrypoint(c, meta).Stop {
			return
		}
		c.Next()
		c.Abort()
	})
	prepended = append(prepended, handlers[len(handlers)-1])
	return prepended
}

func (r *ModeratedRouteRegistrar) runGatewayPipelineEntrypoint(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
	if r == nil || len(r.entrypoints) == 0 {
		return GatewayPipelineEntryResult{}
	}
	meta = moderationcoverage.NormalizeEntry(meta)
	if !meta.Upstream || !meta.ModerationRequired || meta.Pipeline == "" {
		return GatewayPipelineEntryResult{}
	}
	entrypoint := r.entrypointForPipeline(meta.Pipeline)
	if entrypoint == nil {
		return GatewayPipelineEntryResult{}
	}
	result := entrypoint.EnterGatewayPipeline(c, meta)
	if result.Stop {
		c.Abort()
	}
	return result
}

func enforceModeratedRoutePipelineAdmission(c *gin.Context, meta ModeratedRouteMeta) {
	if runtimeMeta, ok := moderationcoverage.RouteMetaFromContext(c); ok {
		meta = runtimeMeta
	}
	meta = moderationcoverage.NormalizeEntry(meta)
	if !meta.Upstream || !meta.ModerationRequired || meta.Pipeline == "" {
		return
	}
	if c.Writer.Status() >= http.StatusBadRequest {
		return
	}
	admission, ok := moderationcoverage.PipelineAdmissionFromContext(c)
	if ok && admission.Admitted && admission.ModerationCompleted && admission.Pipeline == meta.Pipeline {
		return
	}
	if !c.Writer.Written() {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"error": "pipeline_admission_missing",
		})
	}
}

func coveredModeratedRoute(path, handlerName, protocol, reviewReason string) ModeratedRouteMeta {
	meta := ModeratedRouteMeta{
		Path:               path,
		Handler:            handlerName,
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           protocol,
		Status:             moderationcoverage.StatusCovered,
		ReviewReason:       reviewReason,
	}
	return moderationcoverage.AnnotatePipelineCoverage(meta)
}

func coveredOpenAIHTTPRoute(path, handlerName, protocol, reviewReason string) ModeratedRouteMeta {
	meta := coveredModeratedRoute(path, handlerName, protocol, reviewReason)
	return mustHavePipelineCoverage(meta, moderationcoverage.PipelineOpenAIHTTP)
}

func coveredOpenAIWebSocketRoute(path, handlerName, protocol, reviewReason string) ModeratedRouteMeta {
	meta := coveredModeratedRoute(path, handlerName, protocol, reviewReason)
	return mustHavePipelineCoverage(meta, moderationcoverage.PipelineOpenAIWebSocket)
}

func mustHavePipelineCoverage(meta ModeratedRouteMeta, pipeline string) ModeratedRouteMeta {
	meta = moderationcoverage.NormalizeEntry(meta)
	if meta.Pipeline != moderationcoverage.NormalizePipeline(pipeline) || len(meta.StageCoverage) == 0 {
		panic("moderated route metadata missing required pipeline coverage")
	}
	return meta
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
