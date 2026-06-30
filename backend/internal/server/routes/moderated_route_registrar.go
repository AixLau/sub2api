package routes

import (
	"strings"

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
	meta := ModeratedRouteMeta{
		Path:               path,
		Handler:            handlerName,
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           protocol,
		Status:             moderationcoverage.StatusCovered,
		ReviewReason:       reviewReason,
	}
	annotateOpenAIHTTPPipelineCoverage(&meta)
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

func annotateOpenAIHTTPPipelineCoverage(meta *ModeratedRouteMeta) {
	if meta == nil {
		return
	}
	stages := openAIHTTPPipelineStagesForRoute(meta.Handler, meta.Protocol)
	if len(stages) == 0 {
		return
	}
	meta.Pipeline = moderationcoverage.PipelineOpenAIHTTP
	meta.StageCoverage = stages
}

func openAIHTTPPipelineStagesForRoute(handlerName, protocol string) []moderationcoverage.PipelineStageCoverage {
	if !isOpenAIHTTPPipelineProtocol(protocol) {
		return nil
	}

	stages := []moderationcoverage.PipelineStageCoverage{
		coveredPipelineStage(moderationcoverage.StageModeration),
	}
	switch strings.TrimSpace(handlerName) {
	case "OpenAIGatewayHandler.ChatCompletions":
		stages = append(stages, coveredPipelineStage(moderationcoverage.StageCyber))
	case "OpenAIGatewayHandler.Responses":
		stages = append(stages,
			coveredPipelineStage(moderationcoverage.StageCyber),
			coveredPipelineStage(moderationcoverage.StageImage),
		)
	case "OpenAIGatewayHandler.Images":
		stages = append(stages, coveredPipelineStage(moderationcoverage.StageImage))
	case "OpenAIGatewayHandler.Embeddings":
	default:
		return nil
	}
	return stages
}

func coveredPipelineStage(stage string) moderationcoverage.PipelineStageCoverage {
	return moderationcoverage.PipelineStageCoverage{
		Stage:    stage,
		Required: true,
		Covered:  true,
	}
}

func isOpenAIHTTPPipelineProtocol(protocol string) bool {
	switch strings.TrimSpace(protocol) {
	case "openai_chat_completions", "openai_responses", "openai_images", "openai_embeddings":
		return true
	default:
		return false
	}
}
