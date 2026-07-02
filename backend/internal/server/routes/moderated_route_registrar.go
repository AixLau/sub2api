package routes

import (
	"net/http"

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
	routes      routeRegistrar
	entrypoints GatewayPipelineEntrypoints
}

func NewModeratedRouteRegistrar(routes routeRegistrar) *ModeratedRouteRegistrar {
	return NewGatewayPipelineRegistrar(routes, nil)
}

func NewGatewayPipelineRegistrar(routes routeRegistrar, entrypoints GatewayPipelineEntrypoints) *ModeratedRouteRegistrar {
	return &ModeratedRouteRegistrar{
		routes:      routes,
		entrypoints: normalizeGatewayPipelineEntrypoints(entrypoints),
	}
}

func (r *ModeratedRouteRegistrar) GET(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	meta = registerModeratedRoute(meta)
	return r.routes.GET(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) GETNoAudit(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "GET"
	meta = registerModeratedRoute(meta)
	return r.routes.GET(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
}

func (r *ModeratedRouteRegistrar) POST(relativePath string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
	meta.Method = "POST"
	meta = registerModeratedRoute(meta)
	return r.routes.POST(relativePath, r.prependModeratedRouteMetaHandler(meta, handlers)...)
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

func registerModeratedRouteBranch(method string, meta ModeratedRouteMeta) ModeratedRouteMeta {
	meta.Method = method
	return registerModeratedRoute(meta)
}

func setModeratedRouteBranchMeta(c *gin.Context, meta ModeratedRouteMeta) {
	moderationcoverage.SetRouteMeta(c, meta)
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
	prepended := make([]gin.HandlerFunc, 0, len(handlers)+1)
	prepended = append(prepended, func(c *gin.Context) {
		moderationcoverage.SetRouteMeta(c, meta)
		if r != nil && r.runGatewayPipelineEntrypoint(c, meta).Stop {
			return
		}
		c.Next()
		enforceModeratedRoutePipelineAdmission(c, meta)
	})
	prepended = append(prepended, handlers...)
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
	entrypoint := r.entrypoints[meta.Pipeline]
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
	if ok && admission.Admitted && admission.Pipeline == meta.Pipeline {
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
	meta.Pipeline = moderationcoverage.PipelineOpenAIWebSocket
	meta.StageCoverage = []moderationcoverage.PipelineStageCoverage{
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageModeration),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageCyber),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageImage),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StagePreForward),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageBilling),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageRouting),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageForward),
		moderationcoverage.CoveredPipelineStage(moderationcoverage.StageUsage),
	}
	return moderationcoverage.NormalizeEntry(meta)
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
