package routes

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type moderatedRouteCoverageManifest struct {
	SchemaVersion int                                   `json:"schema_version"`
	Entries       []moderatedRouteCoverageManifestEntry `json:"entries"`
}

type moderatedRouteCoverageManifestEntry struct {
	Route              moderatedRouteCoverageManifestRoute        `json:"route"`
	Method             string                                     `json:"method"`
	Path               string                                     `json:"path"`
	Upstream           bool                                       `json:"upstream"`
	ModerationRequired bool                                       `json:"moderation_required"`
	Protocol           string                                     `json:"protocol"`
	Pipeline           string                                     `json:"pipeline"`
	StageCoverage      []moderationcoverage.PipelineStageCoverage `json:"stage_coverage"`
	Status             string                                     `json:"status"`
	Handler            string                                     `json:"handler"`
}

type moderatedRouteCoverageManifestRoute struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	Handler  string `json:"handler"`
	Protocol string `json:"protocol"`
}

func TestModeratedRouteRegistrarMatchesManifestAndRegisteredGatewayRoutes(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	router := newGatewayRoutesTestRouter()

	actualRoutes := moderatedRoutesRequiringCoverageFromRouter(router)
	manifestRoutes := moderatedRoutePathsFromManifest(t)
	registrarRoutes := moderatedRoutePathsFromRegistrar(GatewayModeratedRouteCoverageEntries())
	manifestRouteProtocols := moderatedRouteProtocolKeysFromManifest(t)
	registrarRouteProtocols := moderatedRouteProtocolKeysFromRegistrar(GatewayModeratedRouteCoverageEntries())
	manifestRouteHandlers := moderatedRouteHandlerKeysFromManifest(t)
	registrarRouteHandlers := moderatedRouteHandlerKeysFromRegistrar(GatewayModeratedRouteCoverageEntries())

	require.NotEmpty(t, actualRoutes, "gateway routes requiring moderation coverage should not be empty")
	require.NotEmpty(t, manifestRoutes, "coverage manifest routes should not be empty")
	require.NotEmpty(t, registrarRoutes, "moderated route registrar entries should not be empty")
	require.NotEmpty(t, manifestRouteProtocols, "coverage manifest route protocols should not be empty")
	require.NotEmpty(t, registrarRouteProtocols, "moderated route registrar protocols should not be empty")
	require.NotEmpty(t, manifestRouteHandlers, "coverage manifest route handlers should not be empty")
	require.NotEmpty(t, registrarRouteHandlers, "moderated route registrar handlers should not be empty")

	require.Empty(t, routeSetDifference(actualRoutes, registrarRoutes),
		"all registered gateway routes that can reach upstream content must be registered through the moderated route registrar")
	require.Empty(t, routeSetDifference(registrarRoutes, actualRoutes),
		"moderated route registrar entries must correspond to real Gin gateway routes")
	require.Empty(t, routeSetDifference(manifestRouteProtocols, registrarRouteProtocols),
		"covered manifest routes must be registered through the moderated route registrar")
	require.Empty(t, routeSetDifference(registrarRouteProtocols, manifestRouteProtocols),
		"moderated route registrar entries must be declared in the coverage manifest")
	require.Empty(t, routeSetDifference(manifestRouteHandlers, registrarRouteHandlers),
		"covered manifest route handlers must match the moderated route registrar")
	require.Empty(t, routeSetDifference(registrarRouteHandlers, manifestRouteHandlers),
		"moderated route registrar handlers must match the coverage manifest")
}

func TestGatewaySourceDoesNotRegisterUpstreamRoutesOutsideModeratedRegistrar(t *testing.T) {
	rawRegistrations := rawGatewayUpstreamRouteRegistrationsFromSource(t, gatewaySourceFile(t))

	require.Empty(t, rawRegistrations,
		"gateway upstream routes must be registered through ModeratedRouteRegistrar or explicit NoAudit methods, found raw registrations at %s",
		strings.Join(rawRegistrations, ", "))
}

func TestModeratedRouteRegistrarInjectsRuntimeRouteMetaBeforeHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		method   string
		register func(*ModeratedRouteRegistrar, string, ModeratedRouteMeta, ...gin.HandlerFunc) gin.IRoutes
	}{
		{
			name:   "GET",
			method: http.MethodGet,
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.GET(path, meta, handlers...)
			},
		},
		{
			name:   "POST",
			method: http.MethodPost,
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.POST(path, meta, handlers...)
			},
		},
		{
			name:   "GETNoAudit",
			method: http.MethodGet,
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.GETNoAudit(path, meta, handlers...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := replaceModeratedRouteRegistryForTest(nil)
			defer restore()

			router := gin.New()
			registrar := NewModeratedRouteRegistrar(router)
			var runtimeMeta moderationcoverage.Entry
			var ok bool
			tt.register(registrar, "/runtime-meta", ModeratedRouteMeta{
				Path:               " /runtime-meta ",
				Handler:            " OpenAIGatewayHandler.Responses ",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           " openai_responses ",
				Status:             moderationcoverage.StatusCovered,
			}, func(c *gin.Context) {
				runtimeMeta, ok = moderationcoverage.RouteMetaFromContext(c)
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(tt.method, "/runtime-meta", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusNoContent, rec.Code)
			require.True(t, ok)
			require.Equal(t, tt.method, runtimeMeta.Method)
			require.Equal(t, "/runtime-meta", runtimeMeta.Path)
			require.Equal(t, "OpenAIGatewayHandler.Responses", runtimeMeta.Handler)
			require.Equal(t, "openai_responses", runtimeMeta.Protocol)
			require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, runtimeMeta.Pipeline)
			requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageModeration)
			requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageImage)
		})
	}
}

func TestModeratedRouteRegistrarBindsPipelineAdmissionBeforeHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	var admission moderationcoverage.PipelineAdmission
	var admitted bool

	registrar.POST("/pipeline-admission", coveredOpenAIHTTPRoute(
		"/pipeline-admission",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		admission, admitted = moderationcoverage.PipelineAdmissionFromContext(c)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-admission", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, admitted)
	require.True(t, admission.Admitted)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, admission.Pipeline)
	require.Equal(t, moderationcoverage.StagePreForward, admission.Stage)
	require.Equal(t, moderationcoverage.SourceModeratedRouteRegistrar, admission.Source)
}

func TestModeratedRouteRegistrarDoesNotBindPipelineAdmissionForNoAuditRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	var admitted bool

	registrar.GETNoAudit("/health", intentionalNoAuditRoute(
		"/health",
		"HealthHandler",
		"test route",
	), func(c *gin.Context) {
		admitted = moderationcoverage.PipelineAdmittedFromContext(c)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.False(t, admitted)
}

func TestOpenAIPipelineRouteHelpersAttachPipelineMetadata(t *testing.T) {
	httpRoute := coveredOpenAIHTTPRoute(
		"/v1/responses",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, httpRoute.Pipeline)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageModeration)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageCyber)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageImage)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageBilling)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageRouting)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageForward)
	requireStageRequiredAndCovered(t, httpRoute, moderationcoverage.StageUsage)

	webSocketRoute := coveredOpenAIWebSocketRoute(
		"/v1/responses",
		"OpenAIGatewayHandler.ResponsesWebSocket",
		"openai_responses",
		"test route",
	)
	require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, webSocketRoute.Pipeline)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageModeration)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageCyber)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageImage)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StagePreForward)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageBilling)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageRouting)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageForward)
	requireStageRequiredAndCovered(t, webSocketRoute, moderationcoverage.StageUsage)
}

func TestGatewayRoutesHaveExplicitModerationClassification(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	router := newGatewayRoutesTestRouter()

	actualRoutes := allGatewayRoutesFromRouter(router)
	classifiedRoutes := classifiedRoutesFromRegistrar(GatewayModeratedRouteCoverageEntries())

	require.NotEmpty(t, actualRoutes, "registered gateway routes should not be empty")
	require.NotEmpty(t, classifiedRoutes, "route moderation classification entries should not be empty")
	require.Empty(t, routeSetDifference(actualRoutes, classifiedRoutes),
		"every registered gateway route must explicitly declare whether moderation is required")
	require.Empty(t, routeSetDifference(classifiedRoutes, actualRoutes),
		"route moderation classification entries must correspond to real Gin gateway routes")
}

func TestOpenAIModeratedRoutesHaveGatewayPipelineModerationCoverage(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	openAIProtocols := openAIModeratedRouteProtocolsFromRegistrar(GatewayModeratedRouteCoverageEntries())
	guardProtocols := openAIGuardHelperProtocolsFromHandlerSources(t)
	pipelineCoverage := openAIGatewayPipelineModerationCoverageFromHandlerSources(t)

	require.NotEmpty(t, openAIProtocols, "OpenAI moderated route protocols should not be empty")
	require.Empty(t, routeSetDifference(openAIProtocols, guardProtocols),
		"OpenAI moderated route protocols must still pass through the OpenAI pre-forward moderation stage")
	require.True(t, pipelineCoverage.DelegatesToOpenAIGatewayPipeline,
		"OpenAI moderation_required registrar protocols must be covered by the GatewayPipeline moderation stage; checkWithModerationGuard does not call OpenAIGatewayPipeline.CheckModeration")
	require.True(t, pipelineCoverage.ForwardsModerationProtocol,
		"checkWithModerationGuard calls OpenAIGatewayPipeline.CheckModeration at %s, but must forward moderationGuardInput.Protocol or the whole moderationGuardInput so registrar protocol coverage reaches the pipeline stage",
		strings.Join(pipelineCoverage.Locations, ", "))

	pipelineProtocols := guardProtocols
	require.Empty(t, routeSetDifference(openAIProtocols, pipelineProtocols),
		"OpenAI moderation_required registrar protocols must have GatewayPipeline moderation stage coverage")
}

func TestOpenAIHTTPModeratedRoutesHaveCyberStageCoverage(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	openAIHTTPProtocols := openAIHTTPModeratedRouteProtocolsFromRegistrar(GatewayModeratedRouteCoverageEntries())
	stageCoverage := openAIHTTPStageCoverageFromHandlerSources(t)
	directRejectLocations := openAIHTTPDirectCyberRejectLocations(stageCoverage)
	cyberStageProtocols := openAIHTTPCyberStageProtocols(stageCoverage)

	require.Equal(t, []string{"openai_chat_completions", "openai_responses"}, openAIHTTPProtocols,
		"CyberStage coverage is required only for OpenAI HTTP Chat and Responses moderated route protocols")
	require.Empty(t, directRejectLocations,
		"OpenAI HTTP Chat/Responses must route cyber session checks through OpenAIGatewayPipeline.CheckCyberSession/CyberStage, not direct rejectIfCyberSessionBlocked calls at %s",
		strings.Join(directRejectLocations, ", "))
	require.Empty(t, routeSetDifference(openAIHTTPProtocols, cyberStageProtocols),
		"OpenAI HTTP moderation_required registrar protocols must have CyberStage coverage through CheckCyberSession")
	require.Empty(t, routeSetDifference(cyberStageProtocols, openAIHTTPProtocols),
		"OpenAI HTTP CyberStage coverage should be scoped to moderated Chat/Responses protocols")
}

func TestOpenAIHTTPModerationStageRunsBeforeCyberStage(t *testing.T) {
	stageCoverage := openAIHTTPStageCoverageFromHandlerSources(t)

	for _, handlerName := range []string{"OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Responses"} {
		coverage, ok := stageCoverage[handlerName]
		require.True(t, ok, "OpenAI HTTP handler %s should be present in source coverage scan", handlerName)
		require.True(t, coverage.HasModerationStage,
			"%s must call checkWithModerationGuard before CyberStage", handlerName)
		require.True(t, coverage.HasCyberStage,
			"%s must call OpenAIGatewayPipeline.CheckCyberSession/CyberStage after moderation; old direct rejectIfCyberSessionBlocked calls are not stage coverage", handlerName)
		require.LessOrEqual(t, coverage.FirstModerationPos, coverage.FirstCyberPos,
			"%s must run moderation before CyberStage (moderation at %s, cyber at %s)",
			handlerName, strings.Join(coverage.ModerationLocations, ", "), strings.Join(coverage.CyberLocations, ", "))
	}
}

func TestOpenAIHTTPHandlersUseUnifiedPreForwardPipeline(t *testing.T) {
	stageCoverage := openAIHTTPStageCoverageFromHandlerSources(t)

	for _, handlerName := range []string{"OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.Images", "OpenAIGatewayHandler.Embeddings", "OpenAIGatewayHandler.Messages"} {
		coverage, ok := stageCoverage[handlerName]
		require.True(t, ok, "OpenAI HTTP handler %s should be present in source coverage scan", handlerName)
		require.True(t, coverage.HasHTTPPreForwardPipeline,
			"%s must call runOpenAIHTTPPreForwardPipeline before routing/billing/forwarding", handlerName)
		require.NotContains(t, coverage.DirectStageLocations, "checkWithModerationGuard",
			"%s must not call checkWithModerationGuard directly after adopting the unified HTTP pre-forward pipeline", handlerName)
		require.NotContains(t, coverage.DirectStageLocations, "checkCyberSessionWithPipeline",
			"%s must not call checkCyberSessionWithPipeline directly after adopting the unified HTTP pre-forward pipeline", handlerName)
		require.False(t, coverage.HasDirectCyberReject,
			"%s must run cyber checks through runOpenAIHTTPPreForwardPipeline, not rejectIfCyberSessionBlocked directly", handlerName)
	}

	responsesCoverage := stageCoverage["OpenAIGatewayHandler.Responses"]
	require.False(t, responsesCoverage.HasDirectResponsesImageStage,
		"OpenAIGatewayHandler.Responses must run image permission/slot through runOpenAIHTTPPreForwardPipeline, not inline IsImageGenerationIntent/acquireImageGenerationSlot calls")
}

func TestOpenAIHTTPModeratedRouteRegistrarExposesPipelineStages(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	entries := openAIHTTPPipelineEntriesFromRegistrar(GatewayModeratedRouteCoverageEntries())

	require.Equal(t, []string{
		"POST /backend-api/codex/responses",
		"POST /backend-api/codex/responses/*subpath",
		"POST /chat/completions",
		"POST /embeddings",
		"POST /images/edits",
		"POST /images/generations",
		"POST /responses",
		"POST /responses/*subpath",
		"POST /v1/chat/completions",
		"POST /v1/embeddings",
		"POST /v1/images/edits",
		"POST /v1/images/generations",
		"POST /v1/responses",
		"POST /v1/responses/*subpath",
	}, moderatedRoutePathKeysFromEntries(entries))

	for _, entry := range entries {
		require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, entry.Pipeline, "path=%s handler=%s", entry.Path, entry.Handler)
		requireStageRequiredAndCovered(t, entry, moderationcoverage.StageModeration)
		requireStageRequiredAndCovered(t, entry, moderationcoverage.StageBilling)
		requireStageRequiredAndCovered(t, entry, moderationcoverage.StageRouting)
		requireStageRequiredAndCovered(t, entry, moderationcoverage.StageForward)
		requireStageRequiredAndCovered(t, entry, moderationcoverage.StageUsage)
		require.False(t, isOpenAIResponsesWebSocketHandler(entry.Handler),
			"OpenAI HTTP pipeline metadata must not include Responses WebSocket routes")

		switch entry.Handler {
		case "OpenAIGatewayHandler.ChatCompletions":
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageCyber)
			requireStageNotRequired(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Responses":
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Images":
			requireStageNotRequired(t, entry, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Embeddings":
			requireStageNotRequired(t, entry, moderationcoverage.StageCyber)
			requireStageNotRequired(t, entry, moderationcoverage.StageImage)
		default:
			t.Fatalf("unexpected OpenAI HTTP pipeline handler %q for %s", entry.Handler, entry.Path)
		}
	}
}

func TestOpenAIHTTPPipelineCoverageMetadataMatchesHandlerSourceCoverage(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	stageCoverage := openAIHTTPStageCoverageFromHandlerSources(t)
	entries := openAIHTTPPipelineEntriesFromRegistrar(GatewayModeratedRouteCoverageEntries())

	for _, entry := range entries {
		coverage, ok := stageCoverage[entry.Handler]
		require.True(t, ok, "handler %s should be present in source coverage scan", entry.Handler)
		require.Equal(t, coverage.HasModerationStage, stageRequiredAndCovered(entry, moderationcoverage.StageModeration),
			"%s %s moderation metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasCyberStage, stageRequiredAndCovered(entry, moderationcoverage.StageCyber),
			"%s %s cyber metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasImageStage, stageRequiredAndCovered(entry, moderationcoverage.StageImage),
			"%s %s image metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasBillingStage, stageRequiredAndCovered(entry, moderationcoverage.StageBilling),
			"%s %s billing metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasRoutingStage, stageRequiredAndCovered(entry, moderationcoverage.StageRouting),
			"%s %s routing metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasForwardStage, stageRequiredAndCovered(entry, moderationcoverage.StageForward),
			"%s %s forward metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasUsageStage, stageRequiredAndCovered(entry, moderationcoverage.StageUsage),
			"%s %s usage metadata drifted from handler source coverage", entry.Method, entry.Path)
	}
}

func TestGatewayModerationCoverageManifestPipelineStagesMatchRegistrarFacts(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	manifest := loadModeratedRouteCoverageManifest(t)
	registrarByRoute := make(map[string]ModeratedRouteMeta)
	for _, entry := range GatewayModeratedRouteCoverageEntries() {
		entry = moderationcoverage.NormalizeEntry(entry)
		registrarByRoute[moderatedRouteKey(entry.Method, entry.Path, entry.Protocol)] = entry
	}

	for _, entry := range manifest.Entries {
		routeKey := moderatedRouteKey(entry.Method, entry.Path, entry.Protocol)
		require.Equal(t, entry.Method, entry.Route.Method, "manifest route.method must mirror top-level method for %s", routeKey)
		require.Equal(t, entry.Path, entry.Route.Path, "manifest route.path must mirror top-level path for %s", routeKey)
		require.Equal(t, entry.Handler, entry.Route.Handler, "manifest route.handler must mirror top-level handler for %s", routeKey)
		require.Equal(t, entry.Protocol, entry.Route.Protocol, "manifest route.protocol must mirror top-level protocol for %s", routeKey)

		expectedPipeline := moderationcoverage.PipelineOpenAIHTTP
		expectedStages := moderationcoverage.OpenAIHTTPPipelineStagesForRoute(entry.Handler, entry.Protocol)
		if len(expectedStages) == 0 {
			expectedPipeline = moderationcoverage.PipelineOpenAIWebSocket
			expectedStages = moderationcoverage.OpenAIWebSocketPipelineStagesForRoute(entry.Handler, entry.Protocol)
		}
		if len(expectedStages) == 0 {
			require.Empty(t, entry.Pipeline, "non-pipeline manifest route must not declare pipeline metadata: %s", routeKey)
			require.Empty(t, entry.StageCoverage, "non-pipeline manifest route must not declare stage coverage: %s", routeKey)
			continue
		}

		require.Equal(t, expectedPipeline, entry.Pipeline, "manifest pipeline drifted for %s", routeKey)
		require.Equal(t, expectedStages, moderationcoverage.NormalizeStageCoverage(entry.StageCoverage), "manifest stage coverage drifted for %s", routeKey)

		registrarEntry, ok := registrarByRoute[routeKey]
		require.True(t, ok, "registrar missing manifest route %s", routeKey)
		require.Equal(t, entry.Pipeline, registrarEntry.Pipeline, "registrar pipeline drifted from manifest for %s", routeKey)
		require.Equal(t, entry.StageCoverage, registrarEntry.StageCoverage, "registrar stage coverage drifted from manifest for %s", routeKey)
	}
}

func allGatewayRoutesFromRouter(router *gin.Engine) []string {
	routeSet := make(map[string]struct{})
	for _, route := range router.Routes() {
		routeSet[moderatedRouteKey(route.Method, route.Path, "")] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func classifiedRoutesFromRegistrar(entries []ModeratedRouteMeta) []string {
	routeSet := make(map[string]struct{})
	for _, entry := range entries {
		if strings.TrimSpace(entry.Status) == "" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, "")] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRoutesRequiringCoverageFromRouter(router *gin.Engine) []string {
	routeSet := make(map[string]struct{})
	for _, route := range router.Routes() {
		if routeRequiresModerationCoverage(route.Method, route.Path) {
			routeSet[moderatedRouteKey(route.Method, route.Path, "")] = struct{}{}
		}
	}
	return sortedRouteSet(routeSet)
}

func routeRequiresModerationCoverage(method, path string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost:
		return postRouteCanReachUpstreamContent(path)
	case http.MethodGet:
		switch strings.TrimSpace(path) {
		case "/v1/responses", "/responses", "/backend-api/codex/responses":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func postRouteCanReachUpstreamContent(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch path {
	case "/responses", "/responses/*subpath",
		"/chat/completions",
		"/embeddings",
		"/images/generations", "/images/edits":
		return true
	}
	return strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1beta/") ||
		strings.HasPrefix(path, "/antigravity/v1/") ||
		strings.HasPrefix(path, "/antigravity/v1beta/") ||
		strings.HasPrefix(path, "/backend-api/codex/")
}

func moderatedRoutePathsFromRegistrar(entries []ModeratedRouteMeta) []string {
	routeSet := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, "")] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRoutePathsFromManifest(t *testing.T) []string {
	t.Helper()

	manifest := loadModeratedRouteCoverageManifest(t)
	require.Equal(t, 1, manifest.SchemaVersion)

	routeSet := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, "")] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRouteProtocolKeysFromRegistrar(entries []ModeratedRouteMeta) []string {
	routeSet := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, entry.Protocol)] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRouteProtocolKeysFromManifest(t *testing.T) []string {
	t.Helper()

	manifest := loadModeratedRouteCoverageManifest(t)
	require.Equal(t, 1, manifest.SchemaVersion)

	routeSet := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, entry.Protocol)] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRouteHandlerKeysFromRegistrar(entries []ModeratedRouteMeta) []string {
	routeSet := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, entry.Handler)] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func moderatedRouteHandlerKeysFromManifest(t *testing.T) []string {
	t.Helper()

	manifest := loadModeratedRouteCoverageManifest(t)
	require.Equal(t, 1, manifest.SchemaVersion)

	routeSet := make(map[string]struct{})
	for _, entry := range manifest.Entries {
		if !entry.Upstream || !entry.ModerationRequired || strings.TrimSpace(entry.Status) != "covered" {
			continue
		}
		routeSet[moderatedRouteKey(entry.Method, entry.Path, entry.Handler)] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func loadModeratedRouteCoverageManifest(t *testing.T) moderatedRouteCoverageManifest {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	path := filepath.Join(repoRoot, "docs", "risk-control", "content-moderation-gateway-coverage.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var manifest moderatedRouteCoverageManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	return manifest
}

func moderatedRouteKey(method, path, protocol string) string {
	key := strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
	if protocol == "" {
		return key
	}
	return key + " " + strings.TrimSpace(protocol)
}

func routeSetDifference(left, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, route := range right {
		rightSet[route] = struct{}{}
	}
	missing := make([]string, 0)
	for _, route := range left {
		if _, ok := rightSet[route]; !ok {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	return missing
}

func sortedRouteSet(routeSet map[string]struct{}) []string {
	routes := make([]string, 0, len(routeSet))
	for route := range routeSet {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}

func openAIModeratedRouteProtocolsFromRegistrar(entries []ModeratedRouteMeta) []string {
	protocolSet := make(map[string]struct{})
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if !entry.Upstream || !entry.ModerationRequired || entry.Status != moderationcoverage.StatusCovered {
			continue
		}
		if strings.HasPrefix(entry.Protocol, "openai_") {
			protocolSet[entry.Protocol] = struct{}{}
		}
	}
	return sortedRouteSet(protocolSet)
}

func openAIHTTPModeratedRouteProtocolsFromRegistrar(entries []ModeratedRouteMeta) []string {
	protocolSet := make(map[string]struct{})
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if !entry.Upstream || !entry.ModerationRequired || entry.Status != moderationcoverage.StatusCovered {
			continue
		}
		if !isOpenAIHTTPModeratedHandler(entry.Handler) {
			continue
		}
		switch entry.Protocol {
		case "openai_chat_completions", "openai_responses":
			protocolSet[entry.Protocol] = struct{}{}
		}
	}
	return sortedRouteSet(protocolSet)
}

type openAIHTTPHandlerStageCoverage struct {
	Protocol                     string
	HasModerationStage           bool
	HasCyberStage                bool
	HasImageStage                bool
	HasBillingStage              bool
	HasRoutingStage              bool
	HasForwardStage              bool
	HasUsageStage                bool
	HasHTTPPreForwardPipeline    bool
	HasDirectResponsesImageStage bool
	HasDirectCyberReject         bool
	FirstModerationPos           token.Pos
	FirstCyberPos                token.Pos
	ModerationLocations          []string
	CyberLocations               []string
	DirectStageLocations         []string
	DirectCyberRejectLocations   []string
}

func openAIHTTPStageCoverageFromHandlerSources(t *testing.T) map[string]openAIHTTPHandlerStageCoverage {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	handlerDir := filepath.Join(repoRoot, "backend", "internal", "handler")
	files := []string{
		filepath.Join(handlerDir, "openai_chat_completions.go"),
		filepath.Join(handlerDir, "openai_embeddings.go"),
		filepath.Join(handlerDir, "openai_gateway_handler.go"),
		filepath.Join(handlerDir, "openai_images.go"),
	}
	pipelineFields := openAIGatewayPipelineFieldsFromHandlerSources(t, files)
	coverageByHandler := make(map[string]openAIHTTPHandlerStageCoverage)

	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		require.NoError(t, err)

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			handlerName, ok := openAIHTTPHandlerStageCoverageName(fn)
			if !ok {
				continue
			}
			coverage := openAIHTTPHandlerStageCoverage{}
			pipelineAliases := collectOpenAIGatewayPipelineAliases(fn.Body, pipelineFields)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.FuncLit:
					return false
				case *ast.CallExpr:
					location := sourceLocation(repoRoot, file, fset.Position(n.Pos()).Line)
					switch {
					case isOpenAIHTTPPreForwardPipelineCall(n):
						coverage.HasHTTPPreForwardPipeline = true
						coverage.HasModerationStage = true
						hasCyberStage := !openAIHTTPPreForwardPipelineSkipsCyberStage(n)
						coverage.HasCyberStage = hasCyberStage
						if openAIHTTPPreForwardPipelineEnablesImageStage(n) {
							coverage.HasImageStage = true
						}
						if coverage.FirstModerationPos == token.NoPos || n.Pos() < coverage.FirstModerationPos {
							coverage.FirstModerationPos = n.Pos()
						}
						if hasCyberStage && (coverage.FirstCyberPos == token.NoPos || n.Pos() < coverage.FirstCyberPos) {
							coverage.FirstCyberPos = n.Pos()
						}
						coverage.ModerationLocations = append(coverage.ModerationLocations, location)
						if hasCyberStage {
							coverage.CyberLocations = append(coverage.CyberLocations, location)
						}
						if protocol := serviceProtocolConstantValue(contentModerationProtocolArg(n)); isOpenAIHTTPModerationProtocol(protocol) {
							coverage.Protocol = protocol
						}
					case isCheckWithModerationGuardCall(n):
						coverage.HasModerationStage = true
						if coverage.FirstModerationPos == token.NoPos || n.Pos() < coverage.FirstModerationPos {
							coverage.FirstModerationPos = n.Pos()
						}
						coverage.ModerationLocations = append(coverage.ModerationLocations, location)
						coverage.DirectStageLocations = append(coverage.DirectStageLocations, "checkWithModerationGuard")
						if protocol := serviceProtocolConstantValue(contentModerationProtocolArg(n)); isOpenAIHTTPModerationProtocol(protocol) {
							coverage.Protocol = protocol
						}
					case isCheckCyberSessionCall(n, pipelineFields, pipelineAliases):
						coverage.HasCyberStage = true
						if coverage.FirstCyberPos == token.NoPos || n.Pos() < coverage.FirstCyberPos {
							coverage.FirstCyberPos = n.Pos()
						}
						coverage.CyberLocations = append(coverage.CyberLocations, location)
						coverage.DirectStageLocations = append(coverage.DirectStageLocations, "checkCyberSessionWithPipeline")
						if protocol := serviceProtocolConstantValue(contentModerationProtocolArg(n)); isOpenAIHTTPModerationProtocol(protocol) {
							coverage.Protocol = protocol
						}
					case isDirectCyberSessionRejectCall(n):
						coverage.HasDirectCyberReject = true
						coverage.DirectCyberRejectLocations = append(coverage.DirectCyberRejectLocations, location)
					case isDirectResponsesImageStageCall(n):
						coverage.HasDirectResponsesImageStage = true
						coverage.HasImageStage = true
					case isOpenAIHTTPForwardStageCall(n):
						coverage.HasForwardStage = true
					case isOpenAIHTTPExecutableStageCall(n):
						switch openAIHTTPExecutableStageArg(n) {
						case moderationcoverage.StageBilling:
							coverage.HasBillingStage = true
						case moderationcoverage.StageRouting:
							coverage.HasRoutingStage = true
						case moderationcoverage.StageForward:
							coverage.HasForwardStage = true
						case moderationcoverage.StageUsage:
							coverage.HasUsageStage = true
						}
					}
				}
				return true
			})
			coverageByHandler[handlerName] = coverage
		}
	}

	return coverageByHandler
}

func openAIHTTPHandlerStageCoverageName(fn *ast.FuncDecl) (string, bool) {
	switch fn.Name.Name {
	case "ChatCompletions", "Responses", "Images", "Embeddings", "Messages":
	default:
		return "", false
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	if astExprLastIdentName(fn.Recv.List[0].Type) != "OpenAIGatewayHandler" {
		return "", false
	}
	return "OpenAIGatewayHandler." + fn.Name.Name, true
}

func isOpenAIHTTPModeratedHandler(handler string) bool {
	switch strings.TrimSpace(handler) {
	case "OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.Images", "OpenAIGatewayHandler.Embeddings":
		return true
	default:
		return false
	}
}

func isOpenAIResponsesWebSocketHandler(handler string) bool {
	return strings.TrimSpace(handler) == "OpenAIGatewayHandler.ResponsesWebSocket"
}

func openAIHTTPDirectCyberRejectLocations(coverageByHandler map[string]openAIHTTPHandlerStageCoverage) []string {
	locations := make([]string, 0)
	for _, handlerName := range []string{"OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Responses"} {
		coverage := coverageByHandler[handlerName]
		locations = append(locations, coverage.DirectCyberRejectLocations...)
	}
	sort.Strings(locations)
	return locations
}

func openAIHTTPCyberStageProtocols(coverageByHandler map[string]openAIHTTPHandlerStageCoverage) []string {
	protocolSet := make(map[string]struct{})
	for _, coverage := range coverageByHandler {
		if !coverage.HasCyberStage || !isOpenAIHTTPModerationProtocol(coverage.Protocol) {
			continue
		}
		protocolSet[coverage.Protocol] = struct{}{}
	}
	return sortedRouteSet(protocolSet)
}

func isOpenAIHTTPModerationProtocol(protocol string) bool {
	switch protocol {
	case "openai_chat_completions", "openai_responses", "openai_images", "openai_embeddings":
		return true
	default:
		return false
	}
}

func isCheckCyberSessionCall(call *ast.CallExpr, pipelineFields, pipelineAliases map[string]struct{}) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "CheckCyberSession":
		return isOpenAIGatewayPipelineReceiver(selector.X, pipelineFields, pipelineAliases)
	case "checkCyberSessionWithPipeline":
		receiver, ok := selector.X.(*ast.Ident)
		return ok && receiver.Name == "h"
	}
	return false
}

func isDirectCyberSessionRejectCall(call *ast.CallExpr) bool {
	return callName(call) == "rejectIfCyberSessionBlocked"
}

func callName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func sourceLocation(repoRoot, file string, line int) string {
	if rel, err := filepath.Rel(repoRoot, file); err == nil {
		return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), line)
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(file), line)
}

func rawGatewayUpstreamRouteRegistrationsFromSource(t *testing.T, file string) []string {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	src, err := os.ReadFile(file)
	require.NoError(t, err)

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	moderatedRegistrars := collectModeratedRouteRegistrarNames(parsed)
	rawRegistrations := make([]string, 0)
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !isGatewayRouteRegistrationMethod(selector.Sel.Name) {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		if _, ok := moderatedRegistrars[receiver.Name]; ok {
			return true
		}
		routePath := firstStringArg(call)
		if !routeRequiresModerationCoverage(selector.Sel.Name, routePath) {
			return true
		}

		pos := fset.Position(call.Pos())
		rawRegistrations = append(rawRegistrations, fmt.Sprintf("%s %s %s",
			sourceLocation(repoRoot, file, pos.Line),
			strings.ToUpper(selector.Sel.Name),
			routePath,
		))
		return true
	})
	sort.Strings(rawRegistrations)
	return rawRegistrations
}

func collectModeratedRouteRegistrarNames(file *ast.File) map[string]struct{} {
	names := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok || callName(call) != "NewModeratedRouteRegistrar" || i >= len(assign.Lhs) {
				continue
			}
			if ident, ok := assign.Lhs[i].(*ast.Ident); ok {
				names[ident.Name] = struct{}{}
			}
		}
		return true
	})
	return names
}

func isGatewayRouteRegistrationMethod(name string) bool {
	switch name {
	case http.MethodGet, http.MethodPost:
		return true
	default:
		return false
	}
}

func firstStringArg(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	literal, ok := call.Args[0].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return ""
	}
	return value
}

type openAIGatewayPipelineModerationCoverage struct {
	DelegatesToOpenAIGatewayPipeline bool
	ForwardsModerationProtocol       bool
	Locations                        []string
}

func openAIGuardHelperProtocolsFromHandlerSources(t *testing.T) []string {
	t.Helper()

	files := handlerSourceFiles(t, "openai*.go")

	protocolSet := make(map[string]struct{})
	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		require.NoError(t, err)

		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || (!isCheckWithModerationGuardCall(call) && !isOpenAIHTTPPreForwardPipelineCall(call)) {
				return true
			}
			protocol := contentModerationProtocolArg(call)
			if strings.HasPrefix(protocol, "ContentModerationProtocolOpenAI") {
				protocolSet[serviceProtocolConstantValue(protocol)] = struct{}{}
			}
			return true
		})
	}
	return sortedRouteSet(protocolSet)
}

func openAIGatewayPipelineModerationCoverageFromHandlerSources(t *testing.T) openAIGatewayPipelineModerationCoverage {
	t.Helper()

	files := handlerSourceFiles(t, "*.go")
	pipelineFields := openAIGatewayPipelineFieldsFromHandlerSources(t, files)
	repoRoot := repoRootFromTestFile(t)
	coverage := openAIGatewayPipelineModerationCoverage{}

	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		require.NoError(t, err)

		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "checkWithModerationGuard" || fn.Body == nil {
				continue
			}

			pipelineAliases := collectOpenAIGatewayPipelineAliases(fn.Body, pipelineFields)
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "CheckModeration" {
					return true
				}
				if !isOpenAIGatewayPipelineReceiver(selector.X, pipelineFields, pipelineAliases) {
					return true
				}

				coverage.DelegatesToOpenAIGatewayPipeline = true
				pos := fset.Position(selector.Sel.Pos())
				if rel, err := filepath.Rel(repoRoot, file); err == nil {
					coverage.Locations = append(coverage.Locations, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line))
				} else {
					coverage.Locations = append(coverage.Locations, fmt.Sprintf("%s:%d", filepath.ToSlash(file), pos.Line))
				}
				if callForwardsModerationInputProtocol(call) {
					coverage.ForwardsModerationProtocol = true
				}
				return true
			})
		}
	}

	sort.Strings(coverage.Locations)
	return coverage
}

func handlerSourceFiles(t *testing.T, pattern string) []string {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	handlerDir := filepath.Join(repoRoot, "backend", "internal", "handler")
	files, err := filepath.Glob(filepath.Join(handlerDir, pattern))
	require.NoError(t, err)
	sort.Strings(files)

	sourceFiles := make([]string, 0, len(files))
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			sourceFiles = append(sourceFiles, file)
		}
	}
	return sourceFiles
}

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
}

func gatewaySourceFile(t *testing.T) string {
	t.Helper()

	return filepath.Join(repoRootFromTestFile(t), "backend", "internal", "server", "routes", "gateway.go")
}

func openAIGatewayPipelineFieldsFromHandlerSources(t *testing.T, files []string) map[string]struct{} {
	t.Helper()

	fields := make(map[string]struct{})
	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		require.NoError(t, err)

		ast.Inspect(parsed, func(node ast.Node) bool {
			typeSpec, ok := node.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "OpenAIGatewayHandler" {
				return true
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {
				if !astExprMentionsOpenAIGatewayPipeline(field.Type) {
					continue
				}
				if len(field.Names) == 0 {
					if name := astExprLastIdentName(field.Type); name != "" {
						fields[name] = struct{}{}
					}
					continue
				}
				for _, name := range field.Names {
					fields[name.Name] = struct{}{}
				}
			}
			return true
		})
	}
	return fields
}

func collectOpenAIGatewayPipelineAliases(body *ast.BlockStmt, pipelineFields map[string]struct{}) map[string]struct{} {
	aliases := make(map[string]struct{})

	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				if !isOpenAIGatewayPipelineExpr(rhs, pipelineFields, aliases) {
					continue
				}
				if i >= len(n.Lhs) {
					continue
				}
				if ident, ok := n.Lhs[i].(*ast.Ident); ok {
					aliases[ident.Name] = struct{}{}
				}
			}
		case *ast.ValueSpec:
			for i, value := range n.Values {
				if !isOpenAIGatewayPipelineExpr(value, pipelineFields, aliases) {
					continue
				}
				if i >= len(n.Names) {
					continue
				}
				aliases[n.Names[i].Name] = struct{}{}
			}
		}
		return true
	})

	return aliases
}

func isOpenAIGatewayPipelineReceiver(expr ast.Expr, pipelineFields, pipelineAliases map[string]struct{}) bool {
	return isOpenAIGatewayPipelineExpr(expr, pipelineFields, pipelineAliases)
}

func isOpenAIGatewayPipelineExpr(expr ast.Expr, pipelineFields, pipelineAliases map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		if _, ok := pipelineAliases[e.Name]; ok {
			return true
		}
		return astExprNameMentionsOpenAIGatewayPipeline(e.Name)
	case *ast.SelectorExpr:
		if ident, ok := e.X.(*ast.Ident); ok && ident.Name == "h" {
			if _, ok := pipelineFields[e.Sel.Name]; ok {
				return true
			}
		}
		return astExprMentionsOpenAIGatewayPipeline(e)
	case *ast.CallExpr:
		return astExprMentionsOpenAIGatewayPipeline(e.Fun)
	case *ast.CompositeLit:
		return astExprMentionsOpenAIGatewayPipeline(e.Type)
	case *ast.ParenExpr:
		return isOpenAIGatewayPipelineExpr(e.X, pipelineFields, pipelineAliases)
	case *ast.StarExpr:
		return isOpenAIGatewayPipelineExpr(e.X, pipelineFields, pipelineAliases)
	case *ast.UnaryExpr:
		return isOpenAIGatewayPipelineExpr(e.X, pipelineFields, pipelineAliases)
	default:
		return astExprMentionsOpenAIGatewayPipeline(expr)
	}
}

func astExprMentionsOpenAIGatewayPipeline(expr ast.Expr) bool {
	return astExprNameMentionsOpenAIGatewayPipeline(astExprName(expr))
}

func astExprNameMentionsOpenAIGatewayPipeline(name string) bool {
	return strings.Contains(name, "OpenAIGatewayPipeline") || strings.Contains(name, "GatewayPipeline")
}

func astExprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		prefix := astExprName(e.X)
		if prefix == "" {
			return e.Sel.Name
		}
		return prefix + "." + e.Sel.Name
	case *ast.StarExpr:
		return astExprName(e.X)
	case *ast.ParenExpr:
		return astExprName(e.X)
	case *ast.CallExpr:
		return astExprName(e.Fun)
	case *ast.CompositeLit:
		return astExprName(e.Type)
	case *ast.IndexExpr:
		return astExprName(e.X)
	case *ast.IndexListExpr:
		return astExprName(e.X)
	default:
		return ""
	}
}

func astExprLastIdentName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	case *ast.StarExpr:
		return astExprLastIdentName(e.X)
	case *ast.ParenExpr:
		return astExprLastIdentName(e.X)
	default:
		return ""
	}
}

func callForwardsModerationInputProtocol(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if exprForwardsModerationInputProtocol(arg) {
			return true
		}
	}
	return false
}

func exprForwardsModerationInputProtocol(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "input"
	case *ast.SelectorExpr:
		if e.Sel.Name == "Protocol" {
			if ident, ok := e.X.(*ast.Ident); ok && ident.Name == "input" {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			switch value := elt.(type) {
			case *ast.KeyValueExpr:
				if key, ok := value.Key.(*ast.Ident); ok && key.Name == "Protocol" && exprForwardsModerationInputProtocol(value.Value) {
					return true
				}
			case ast.Expr:
				if exprForwardsModerationInputProtocol(value) {
					return true
				}
			}
		}
	case *ast.CallExpr:
		return callForwardsModerationInputProtocol(e)
	case *ast.ParenExpr:
		return exprForwardsModerationInputProtocol(e.X)
	case *ast.UnaryExpr:
		return exprForwardsModerationInputProtocol(e.X)
	}
	return false
}

func isCheckWithModerationGuardCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "checkWithModerationGuard"
}

func isOpenAIHTTPPreForwardPipelineCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPPreForwardPipeline"
}

func isOpenAIHTTPExecutableStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPExecutableStage"
}

func isOpenAIHTTPForwardStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPForwardStage"
}

func openAIHTTPExecutableStageArg(call *ast.CallExpr) string {
	if len(call.Args) < 2 {
		return ""
	}
	return stageConstantValue(call.Args[1])
}

func stageConstantValue(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		switch e.Sel.Name {
		case "StageBilling":
			return moderationcoverage.StageBilling
		case "StageRouting":
			return moderationcoverage.StageRouting
		case "StageForward":
			return moderationcoverage.StageForward
		case "StageUsage":
			return moderationcoverage.StageUsage
		case "StageModeration":
			return moderationcoverage.StageModeration
		case "StageCyber":
			return moderationcoverage.StageCyber
		case "StageImage":
			return moderationcoverage.StageImage
		}
	case *ast.Ident:
		return e.Name
	}
	return ""
}

func openAIHTTPPreForwardPipelineEnablesImageStage(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		literal, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, elt := range literal.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "EnableImageStage" {
				continue
			}
			value, ok := kv.Value.(*ast.Ident)
			return ok && value.Name == "true"
		}
	}
	return false
}

func openAIHTTPPreForwardPipelineSkipsCyberStage(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		literal, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, elt := range literal.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "SkipCyberStage" {
				continue
			}
			value, ok := kv.Value.(*ast.Ident)
			return ok && value.Name == "true"
		}
	}
	return false
}

func isDirectResponsesImageStageCall(call *ast.CallExpr) bool {
	switch callName(call) {
	case "IsImageGenerationIntent", "GroupAllowsImageGeneration", "acquireImageGenerationSlot":
		return true
	default:
		return false
	}
}

func contentModerationProtocolArg(call *ast.CallExpr) string {
	for _, arg := range call.Args {
		selector, ok := arg.(*ast.SelectorExpr)
		if ok && strings.HasPrefix(selector.Sel.Name, "ContentModerationProtocol") {
			return selector.Sel.Name
		}

		literal, ok := arg.(*ast.CompositeLit)
		if !ok {
			continue
		}
		for _, elt := range literal.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Protocol" {
				continue
			}
			selector, ok := kv.Value.(*ast.SelectorExpr)
			if ok && strings.HasPrefix(selector.Sel.Name, "ContentModerationProtocol") {
				return selector.Sel.Name
			}
		}
	}
	return ""
}

func serviceProtocolConstantValue(name string) string {
	switch name {
	case "ContentModerationProtocolOpenAIChat":
		return "openai_chat_completions"
	case "ContentModerationProtocolOpenAIResponses":
		return "openai_responses"
	case "ContentModerationProtocolOpenAIImages":
		return "openai_images"
	case "ContentModerationProtocolOpenAIEmbeddings":
		return "openai_embeddings"
	default:
		return name
	}
}

func openAIHTTPPipelineEntriesFromRegistrar(entries []ModeratedRouteMeta) []ModeratedRouteMeta {
	out := make([]ModeratedRouteMeta, 0)
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if entry.Pipeline != moderationcoverage.PipelineOpenAIHTTP {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return moderatedRouteKey(out[i].Method, out[i].Path, "") < moderatedRouteKey(out[j].Method, out[j].Path, "")
	})
	return out
}

func moderatedRoutePathKeysFromEntries(entries []ModeratedRouteMeta) []string {
	routeSet := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		routeSet[moderatedRouteKey(entry.Method, entry.Path, "")] = struct{}{}
	}
	return sortedRouteSet(routeSet)
}

func requireStageRequiredAndCovered(t *testing.T, entry ModeratedRouteMeta, stage string) {
	t.Helper()
	require.True(t, stageRequiredAndCovered(entry, stage),
		"%s %s must require and cover %s stage, got %#v", entry.Method, entry.Path, stage, entry.StageCoverage)
}

func requireStageNotRequired(t *testing.T, entry ModeratedRouteMeta, stage string) {
	t.Helper()
	require.False(t, stageRequired(entry, stage),
		"%s %s must not require %s stage, got %#v", entry.Method, entry.Path, stage, entry.StageCoverage)
}

func stageRequiredAndCovered(entry ModeratedRouteMeta, stage string) bool {
	for _, item := range entry.StageCoverage {
		if item.Stage == stage {
			return item.Required && item.Covered
		}
	}
	return false
}

func stageRequired(entry ModeratedRouteMeta, stage string) bool {
	for _, item := range entry.StageCoverage {
		if item.Stage == stage {
			return item.Required
		}
	}
	return false
}
