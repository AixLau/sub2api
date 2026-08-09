package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
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
	"github.com/Wei-Shaw/sub2api/internal/service"
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
		"gateway upstream routes must be registered through GatewayPipelineRegistrar or explicit NoAudit methods, found raw registrations at %s",
		strings.Join(rawRegistrations, ", "))
}

func TestGatewaySourceDoesNotUseLegacyModeratedRegistrarForUpstreamRoutes(t *testing.T) {
	legacyRegistrars := legacyModeratedRouteRegistrarNamesFromSource(t, gatewaySourceFile(t))

	require.Empty(t, legacyRegistrars,
		"production gateway route registration must use GatewayPipelineRegistrar as the only upstream registration API, found legacy registrar variables %s",
		strings.Join(legacyRegistrars, ", "))
}

func TestGatewayRouteRegistrationUsesSingleGlobalPipelineEntrypoint(t *testing.T) {
	entrypointKeys := gatewayPipelineEntrypointKeysFromSource(t, gatewaySourceFile(t))

	require.Equal(t, []string{"moderationcoverage.PipelineGatewayGlobal"}, entrypointKeys,
		"production gateway route registration must bind one gateway_global entrypoint; global entrypoint dispatches to protocol adapters")
}

func TestGatewayRouteRegistrationDelegatesGlobalEntrypointToDispatcher(t *testing.T) {
	directCalls := gatewayRouteRegistrationInlinePipelineDispatchesFromSource(t, gatewaySourceFile(t))

	require.Empty(t, directCalls,
		"production gateway route registration must delegate gateway_global dispatch to GatewayPipelineEntrypointDispatcher instead of inline protocol dispatch, found %s",
		strings.Join(directCalls, ", "))
}

func TestGatewayRouteRegistrationDelegatesBranchEntrypointsToRegistrar(t *testing.T) {
	directCalls := gatewayRouteRegistrationDirectBranchEntrypointsFromSource(t, gatewaySourceFile(t))

	require.Empty(t, directCalls,
		"production gateway route registration must enter auto-route branch pipelines through enterModeratedRouteBranchPipeline helper, found direct EnterBranchPipeline calls at %s",
		strings.Join(directCalls, ", "))
}

func TestModeratedRouteRegistrarInjectsRuntimeRouteMetaBeforeHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		method   string
		meta     ModeratedRouteMeta
		register func(*ModeratedRouteRegistrar, string, ModeratedRouteMeta, ...gin.HandlerFunc) gin.IRoutes
	}{
		{
			name:   "GET",
			method: http.MethodGet,
			meta: ModeratedRouteMeta{
				Path:               " /runtime-meta ",
				Handler:            " OpenAIGatewayHandler.ResponsesWebSocket ",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           " openai_responses ",
				Status:             moderationcoverage.StatusCovered,
			},
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.GET(path, meta, handlers...)
			},
		},
		{
			name:   "POST",
			method: http.MethodPost,
			meta: ModeratedRouteMeta{
				Path:               " /runtime-meta ",
				Handler:            " OpenAIGatewayHandler.Responses ",
				Upstream:           true,
				ModerationRequired: true,
				Protocol:           " openai_responses ",
				Status:             moderationcoverage.StatusCovered,
			},
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.POST(path, meta, handlers...)
			},
		},
		{
			name:   "GETNoAudit",
			method: http.MethodGet,
			meta: intentionalNoAuditRoute(
				" /runtime-meta ",
				" NoAuditHandler ",
				"test route",
			),
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.GETNoAudit(path, meta, handlers...)
			},
		},
		{
			name:   "DELETENoAudit",
			method: http.MethodDelete,
			meta: intentionalNoAuditRoute(
				" /runtime-meta ",
				" NoAuditDeleteHandler ",
				"test delete route",
			),
			register: func(registrar *ModeratedRouteRegistrar, path string, meta ModeratedRouteMeta, handlers ...gin.HandlerFunc) gin.IRoutes {
				return registrar.DELETENoAudit(path, meta, handlers...)
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
			tt.register(registrar, "/runtime-meta", tt.meta, func(c *gin.Context) {
				runtimeMeta, ok = moderationcoverage.RouteMetaFromContext(c)
				if runtimeMeta.Pipeline != "" {
					moderationcoverage.MarkPipelineAdmitted(c, runtimeMeta.Pipeline, moderationcoverage.StagePreForward, "test pipeline")
				}
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(tt.method, "/runtime-meta", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusNoContent, rec.Code)
			require.True(t, ok)
			require.Equal(t, tt.method, runtimeMeta.Method)
			require.Equal(t, "/runtime-meta", runtimeMeta.Path)
			if tt.name == "GETNoAudit" || tt.name == "DELETENoAudit" {
				if tt.name == "GETNoAudit" {
					require.Equal(t, "NoAuditHandler", runtimeMeta.Handler)
				} else {
					require.Equal(t, "NoAuditDeleteHandler", runtimeMeta.Handler)
				}
				require.Empty(t, runtimeMeta.Protocol)
				require.Empty(t, runtimeMeta.Pipeline)
			} else if tt.method == http.MethodGet {
				require.Equal(t, "OpenAIGatewayHandler.ResponsesWebSocket", runtimeMeta.Handler)
				require.Equal(t, "openai_responses", runtimeMeta.Protocol)
				require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, runtimeMeta.Pipeline)
				require.Equal(t, moderationcoverage.StageAdapterDescriptorsForRoute(runtimeMeta.Handler, runtimeMeta.Protocol), runtimeMeta.StageAdapterDescriptors)
			} else {
				require.Equal(t, "OpenAIGatewayHandler.Responses", runtimeMeta.Handler)
				require.Equal(t, "openai_responses", runtimeMeta.Protocol)
				require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, runtimeMeta.Pipeline)
				require.Equal(t, moderationcoverage.StageAdapterDescriptorsForRoute(runtimeMeta.Handler, runtimeMeta.Protocol), runtimeMeta.StageAdapterDescriptors)
				requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageModeration)
				requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageCyber)
				requireStageRequiredAndCovered(t, runtimeMeta, moderationcoverage.StageImage)
			}
		})
	}
}

func TestModeratedRouteRegistrarRequiresPipelineAdmissionBeforeSuccessfulResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	var admittedBeforeHandler bool

	registrar.POST("/pipeline-admission-required", coveredOpenAIHTTPRoute(
		"/pipeline-admission-required",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		admittedBeforeHandler = moderationcoverage.PipelineAdmittedFromContext(c)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-admission-required", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.False(t, admittedBeforeHandler)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "pipeline_admission_missing")
}

func TestGatewayPipelineRegistrarRunsPipelineEntrypointBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	var entrypointCalled bool
	var handlerCalled bool
	var metaAtEntrypoint ModeratedRouteMeta
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineOpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			entrypointCalled = true
			metaAtEntrypoint = meta
			moderationcoverage.MarkPipelineAdmitted(
				c,
				meta.Pipeline,
				moderationcoverage.StagePreForward,
				"test gateway pipeline entrypoint",
			)
			return GatewayPipelineEntryResult{}
		}),
	})

	registrar.POST("/pipeline-entrypoint", coveredOpenAIHTTPRoute(
		"/pipeline-entrypoint",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		handlerCalled = true
		require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-entrypoint", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, entrypointCalled)
	require.True(t, handlerCalled)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, metaAtEntrypoint.Pipeline)
	require.Equal(t, "/pipeline-entrypoint", metaAtEntrypoint.Path)
	require.Equal(t, "openai_responses", metaAtEntrypoint.Protocol)
}

func TestGatewayPipelineRegistrarReleasesResourcesFromEntrypointContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	moderationService := service.NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineOpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			protectedCtx, err := moderationService.AcquireRequestResources(c.Request.Context(), c.Request.ContentLength, c.GetHeader("Content-Encoding"))
			require.NoError(t, err)
			c.Request = c.Request.WithContext(protectedCtx)
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test resource admission")
			return GatewayPipelineEntryResult{}
		}),
	})

	registrar.POST("/resource-release", coveredOpenAIHTTPRoute(
		"/resource-release",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		require.Greater(t, moderationService.ResourceProtectionStatus().ActiveBytes, int64(0))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/resource-release", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	status := moderationService.ResourceProtectionStatus()
	require.Zero(t, status.ActiveBytes)
	require.Zero(t, status.ActiveReservations)
}

func TestGatewayPipelineRegistrarRequiresEntrypointForPipelineRouteAtRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{})

	require.PanicsWithValue(t,
		"gateway pipeline route POST /pipeline-entrypoint-missing OpenAIGatewayHandler.Responses requires entrypoint for pipeline openai_http",
		func() {
			registrar.POST("/pipeline-entrypoint-missing", coveredOpenAIHTTPRoute(
				"/pipeline-entrypoint-missing",
				"OpenAIGatewayHandler.Responses",
				"openai_responses",
				"test route",
			), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})
		},
	)
}

func TestGatewayPipelineRegistrarAcceptsGlobalEntrypointForPipelineRouteAtRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineGatewayGlobal: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test global gateway pipeline entrypoint")
			return GatewayPipelineEntryResult{}
		}),
	})

	require.NotPanics(t, func() {
		registrar.POST("/pipeline-global-entrypoint", coveredOpenAIHTTPRoute(
			"/pipeline-global-entrypoint",
			"OpenAIGatewayHandler.Responses",
			"openai_responses",
			"test route",
		), func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
	})
}

func TestGatewayPipelineRegistrarRunsGlobalEntrypointBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	var entrypointCalled bool
	var handlerCalled bool
	var metaAtEntrypoint ModeratedRouteMeta
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineGatewayGlobal: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			entrypointCalled = true
			metaAtEntrypoint = meta
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test global gateway pipeline entrypoint")
			return GatewayPipelineEntryResult{}
		}),
	})

	registrar.POST("/pipeline-global-entrypoint", coveredOpenAIHTTPRoute(
		"/pipeline-global-entrypoint",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		handlerCalled = true
		require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-global-entrypoint", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, entrypointCalled)
	require.True(t, handlerCalled)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, metaAtEntrypoint.Pipeline)
	require.Equal(t, "/pipeline-global-entrypoint", metaAtEntrypoint.Path)
	require.Equal(t, "openai_responses", metaAtEntrypoint.Protocol)
}

func TestGatewayPipelineRegistrarRunsOpenAIWebSocketEntrypointBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	var entrypointCalled bool
	var handlerCalled bool
	var metaAtEntrypoint ModeratedRouteMeta
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineOpenAIWebSocket: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			entrypointCalled = true
			metaAtEntrypoint = meta
			moderationcoverage.MarkPipelineStageExecuted(
				c,
				meta.Pipeline,
				moderationcoverage.StagePreForward,
				"test websocket gateway pipeline entrypoint",
			)
			return GatewayPipelineEntryResult{}
		}),
	})

	registrar.GET("/pipeline-ws-entrypoint", coveredOpenAIWebSocketRoute(
		"/pipeline-ws-entrypoint",
		"OpenAIGatewayHandler.ResponsesWebSocket",
		"openai_responses",
		"test websocket route",
	), func(c *gin.Context) {
		handlerCalled = true
		executions := moderationcoverage.PipelineStageExecutionsFromContext(c)
		require.Len(t, executions, 1)
		require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, executions[0].Pipeline)
		require.Equal(t, moderationcoverage.StagePreForward, executions[0].Stage)
		c.Status(http.StatusUpgradeRequired)
	})

	req := httptest.NewRequest(http.MethodGet, "/pipeline-ws-entrypoint", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUpgradeRequired, rec.Code)
	require.True(t, entrypointCalled)
	require.True(t, handlerCalled)
	require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, metaAtEntrypoint.Pipeline)
	require.Equal(t, "/pipeline-ws-entrypoint", metaAtEntrypoint.Path)
	require.Equal(t, "openai_responses", metaAtEntrypoint.Protocol)
}

func TestGatewayPipelineEntrypointDispatcherDispatchesOpenAIHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var called bool
	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		IsOpenAIPlatform: func(*gin.Context) bool { return true },
		OpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			called = true
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test openai http dispatcher")
			return GatewayPipelineEntryResult{Stop: true}
		}),
	})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Handler:  "OpenAIGatewayHandler.Responses",
	})

	require.True(t, result.Stop)
	require.True(t, called)
	require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
}

func TestGatewayPipelineEntrypointDispatcherSkipsOpenAIHTTPForNonOpenAIPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var called bool
	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		GroupPlatform:    func(*gin.Context) string { return service.PlatformAnthropic },
		IsOpenAIPlatform: func(*gin.Context) bool { return false },
		OpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			called = true
			return GatewayPipelineEntryResult{Stop: true}
		}),
	})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Handler:  "OpenAIGatewayHandler.Responses",
	})

	require.False(t, result.Stop)
	require.False(t, called)
}

func TestGatewayPipelineEntrypointDispatcherMarksOpenAIWebSocketEntrypoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Protocol: service.ContentModerationProtocolOpenAIResponses,
		Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
	})

	require.False(t, result.Stop)
	_, ok := moderationcoverage.PipelineEntrypointEnteredFromContext(c, moderationcoverage.PipelineOpenAIWebSocket)
	require.True(t, ok)
	_, ok = moderationcoverage.PipelineAdmissionFromContext(c)
	require.False(t, ok, "the WebSocket handshake is only an entrypoint; each response.create frame must earn its own admission")
}

func TestGatewayPipelineEntrypointDispatcherDispatchesGatewayPreForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var called bool
	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		GroupPlatform: func(*gin.Context) string { return service.PlatformAnthropic },
		GatewayPreForward: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			called = true
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test gateway pre-forward dispatcher")
			return GatewayPipelineEntryResult{Stop: true}
		}),
	})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Protocol: service.ContentModerationProtocolAnthropicMessages,
		Handler:  "GatewayHandler.Messages",
	})

	require.True(t, result.Stop)
	require.True(t, called)
	require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
}

func TestGatewayPipelineEntrypointDispatcherSkipsGatewayPreForwardForOpenAIPlatformWithoutForce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var called bool
	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		GroupPlatform: func(*gin.Context) string { return service.PlatformOpenAI },
		GatewayPreForward: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			called = true
			return GatewayPipelineEntryResult{Stop: true}
		}),
	})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: moderationcoverage.PipelineGatewayPreForward,
		Protocol: service.ContentModerationProtocolAnthropicMessages,
		Handler:  "GatewayHandler.Messages",
	})

	require.False(t, result.Stop)
	require.False(t, called)
}

func TestModeratedRouteRegistrarAcceptsBlockedBranchWithoutPipelineAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	registrar.POST("/pipeline-blocked", coveredModeratedRoute(
		"/pipeline-blocked",
		"GatewayHandler.CountTokens",
		service.ContentModerationProtocolAnthropicMessages,
		"test blocked branch",
	), func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "unsupported platform"})
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-blocked", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "unsupported platform")
	require.NotContains(t, rec.Body.String(), "pipeline_admission_missing")
}

func TestGatewayPipelineEntrypointDispatcherIgnoresUnknownPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	dispatcher := NewGatewayPipelineEntrypointDispatcher(GatewayPipelineEntrypointDispatcherConfig{
		OpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			t.Fatalf("unknown pipeline must not dispatch to OpenAI HTTP")
			return GatewayPipelineEntryResult{}
		}),
	})

	result := dispatcher.EnterGatewayPipeline(c, ModeratedRouteMeta{
		Pipeline: "future_pipeline",
		Protocol: service.ContentModerationProtocolOpenAIResponses,
	})

	require.False(t, result.Stop)
}

func TestGatewayPipelineRegistrarStopsBeforeHandlerWhenEntrypointStops(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	var handlerCalled bool
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineOpenAIHTTP: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			c.JSON(http.StatusForbidden, gin.H{"error": "blocked_by_pipeline_entrypoint"})
			return GatewayPipelineEntryResult{Stop: true}
		}),
	})

	registrar.POST("/pipeline-entrypoint-block", coveredOpenAIHTTPRoute(
		"/pipeline-entrypoint-block",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-entrypoint-block", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "blocked_by_pipeline_entrypoint")
	require.False(t, handlerCalled)
}

func TestModeratedRouteRegistrarPreservesPipelineAdmissionFromHandlerPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	var admittedBeforeHandler bool
	var admissionAfterPipeline moderationcoverage.PipelineAdmission
	var admittedAfterPipeline bool

	registrar.POST("/pipeline-admitted", coveredOpenAIHTTPRoute(
		"/pipeline-admitted",
		"OpenAIGatewayHandler.Responses",
		"openai_responses",
		"test route",
	), func(c *gin.Context) {
		admittedBeforeHandler = moderationcoverage.PipelineAdmittedFromContext(c)
		moderationcoverage.MarkPipelineAdmitted(
			c,
			moderationcoverage.PipelineOpenAIHTTP,
			moderationcoverage.StagePreForward,
			moderationcoverage.SourceOpenAIHTTPPreForward,
		)
		admissionAfterPipeline, admittedAfterPipeline = moderationcoverage.PipelineAdmissionFromContext(c)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/pipeline-admitted", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.False(t, admittedBeforeHandler)
	require.True(t, admittedAfterPipeline)
	require.True(t, admissionAfterPipeline.Admitted)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, admissionAfterPipeline.Pipeline)
	require.Equal(t, moderationcoverage.StagePreForward, admissionAfterPipeline.Stage)
	require.Equal(t, moderationcoverage.SourceOpenAIHTTPPreForward, admissionAfterPipeline.Source)
}

func TestModeratedRouteRegistrarEnforcesRuntimeBranchRouteMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	registrar := NewModeratedRouteRegistrar(router)
	openAIMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/v1/messages",
		"OpenAIGatewayHandler.Messages",
		"openai_messages",
		"test openai messages branch",
	))

	registrar.POST("/messages", coveredModeratedRoute(
		"/v1/messages",
		"GatewayHandler.Messages",
		"anthropic_messages",
		"test default messages branch",
	), func(c *gin.Context) {
		setModeratedRouteBranchMeta(c, openAIMeta)
		moderationcoverage.MarkPipelineAdmitted(
			c,
			moderationcoverage.PipelineOpenAIHTTP,
			moderationcoverage.StagePreForward,
			moderationcoverage.SourceOpenAIHTTPPreForward,
		)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/messages", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.NotContains(t, rec.Body.String(), "pipeline_admission_missing")
}

func TestModeratedRouteRegistrarEnterBranchPipelineSetsMetaAndRunsEntrypoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()

	router := gin.New()
	var metaAtEntrypoint ModeratedRouteMeta
	registrar := NewGatewayPipelineRegistrar(router, GatewayPipelineEntrypoints{
		moderationcoverage.PipelineGatewayGlobal: GatewayPipelineEntrypointFunc(func(c *gin.Context, meta ModeratedRouteMeta) GatewayPipelineEntryResult {
			metaAtEntrypoint = meta
			moderationcoverage.MarkPipelineAdmitted(c, meta.Pipeline, moderationcoverage.StagePreForward, "test branch entrypoint")
			return GatewayPipelineEntryResult{}
		}),
	})
	defaultMeta := coveredModeratedRoute(
		"/v1/messages",
		"GatewayHandler.Messages",
		"anthropic_messages",
		"test default messages branch",
	)
	branchMeta := registerModeratedRouteBranch(http.MethodPost, coveredOpenAIHTTPRoute(
		"/v1/messages",
		"OpenAIGatewayHandler.Messages",
		"openai_messages",
		"test openai messages branch",
	))

	registrar.POST("/messages", defaultMeta, func(c *gin.Context) {
		result := registrar.EnterBranchPipeline(c, branchMeta)
		require.False(t, result.Stop)
		runtimeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
		require.True(t, ok)
		require.Equal(t, branchMeta.Handler, runtimeMeta.Handler)
		require.True(t, moderationcoverage.PipelineAdmittedFromContext(c))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/messages", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, branchMeta.Handler, metaAtEntrypoint.Handler)
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, metaAtEntrypoint.Pipeline)
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
	require.Equal(t, moderationcoverage.StageAdapterDescriptorsForRoute(httpRoute.Handler, httpRoute.Protocol), httpRoute.StageAdapterDescriptors)

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
	require.Equal(t, moderationcoverage.StageAdapterDescriptorsForRoute(webSocketRoute.Handler, webSocketRoute.Protocol), webSocketRoute.StageAdapterDescriptors)
}

func TestOpenAIPipelineRouteHelpersAttachRealtimeWebSocketMetadata(t *testing.T) {
	route := coveredOpenAIWebSocketRoute(
		"/v1/realtime",
		"OpenAIGatewayHandler.RealtimeWebSocket",
		"openai_realtime",
		"test realtime route",
	)

	require.Equal(t, moderationcoverage.PipelineOpenAIWebSocket, route.Pipeline)
	requireStageRequiredAndCovered(t, route, moderationcoverage.StageModeration)
	requireStageRequiredAndCovered(t, route, moderationcoverage.StagePreForward)
	requireStageRequiredAndCovered(t, route, moderationcoverage.StageForward)
	require.Equal(t, moderationcoverage.StageAdapterDescriptorsForRoute(route.Handler, route.Protocol), route.StageAdapterDescriptors)
}

func TestGatewayPreForwardRouteHelpersAttachPipelineMetadata(t *testing.T) {
	for _, tt := range []struct {
		name     string
		path     string
		handler  string
		protocol string
	}{
		{
			name:     "anthropic messages",
			path:     "/v1/messages",
			handler:  "GatewayHandler.Messages",
			protocol: "anthropic_messages",
		},
		{
			name:     "anthropic count tokens",
			path:     "/v1/messages/count_tokens",
			handler:  "GatewayHandler.CountTokens",
			protocol: "anthropic_messages",
		},
		{
			name:     "root anthropic count tokens",
			path:     "/messages/count_tokens",
			handler:  "GatewayHandler.CountTokens",
			protocol: "anthropic_messages",
		},
		{
			name:     "gemini model actions",
			path:     "/v1beta/models/*modelAction",
			handler:  "GatewayHandler.GeminiV1BetaModels",
			protocol: "gemini",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			route := coveredModeratedRoute(tt.path, tt.handler, tt.protocol, "test route")

			require.Equal(t, moderationcoverage.PipelineGatewayPreForward, route.Pipeline)
			require.Equal(t, moderationcoverage.GatewayPreForwardPipelineStagesForRoute(route.Handler, route.Protocol), route.StageCoverage)
			requireStageRequiredAndCovered(t, route, moderationcoverage.StageModeration)
			requireStageRequiredAndCovered(t, route, moderationcoverage.StagePreForward)
		})
	}
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

	require.Equal(t, []string{"openai_chat_completions", "openai_messages", "openai_responses"}, openAIHTTPProtocols,
		"CyberStage coverage is required for OpenAI HTTP Chat, Messages, and Responses moderated route protocols")
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

	for _, handlerName := range []string{"OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.AlphaSearch", "OpenAIGatewayHandler.Images", "OpenAIGatewayHandler.GrokVideoGeneration", "OpenAIGatewayHandler.GrokVideoEdit", "OpenAIGatewayHandler.GrokVideoExtension", "OpenAIGatewayHandler.Embeddings", "OpenAIGatewayHandler.Messages"} {
		coverage, ok := stageCoverage[handlerName]
		require.True(t, ok, "OpenAI HTTP handler %s should be present in source coverage scan", handlerName)
		require.True(t, coverage.HasHTTPPreForwardPipeline,
			"%s must run the OpenAI HTTP pre-forward pipeline before routing/billing/forwarding, either from the handler or GatewayPipelineRegistrar entrypoint", handlerName)
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
		"POST /alpha/search",
		"POST /backend-api/codex/alpha/search",
		"POST /backend-api/codex/responses",
		"POST /backend-api/codex/responses/*subpath",
		"POST /chat/completions",
		"POST /embeddings",
		"POST /images/edits",
		"POST /images/edits/async",
		"POST /images/generations",
		"POST /images/generations/async",
		"POST /responses",
		"POST /responses/*subpath",
		"POST /tts",
		"POST /v1/alpha/search",
		"POST /v1/chat/completions",
		"POST /v1/embeddings",
		"POST /v1/images/edits",
		"POST /v1/images/edits/async",
		"POST /v1/images/generations",
		"POST /v1/images/generations/async",
		"POST /v1/messages",
		"POST /v1/responses",
		"POST /v1/responses/*subpath",
		"POST /v1/tts",
		"POST /v1/videos",
		"POST /v1/videos/edits",
		"POST /v1/videos/extensions",
		"POST /v1/videos/generations",
		"POST /v1/web_search",
		"POST /videos",
		"POST /videos/edits",
		"POST /videos/extensions",
		"POST /videos/generations",
		"POST /web_search",
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
		case "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.AlphaSearch":
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Images":
			requireStageNotRequired(t, entry, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.GrokVideoGeneration", "OpenAIGatewayHandler.GrokVideoEdit", "OpenAIGatewayHandler.GrokVideoExtension":
			requireStageNotRequired(t, entry, moderationcoverage.StageCyber)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Embeddings", "OpenAIGatewayHandler.GrokVoice", "GatewayHandler.WebSearch":
			requireStageNotRequired(t, entry, moderationcoverage.StageCyber)
			requireStageNotRequired(t, entry, moderationcoverage.StageImage)
		case "OpenAIGatewayHandler.Messages":
			require.Equal(t, "openai_messages", entry.Protocol)
			requireStageRequiredAndCovered(t, entry, moderationcoverage.StageCyber)
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

func TestGatewayPreForwardPipelineCoverageMetadataMatchesHandlerSourceCoverage(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	stageCoverage := gatewayPreForwardStageCoverageFromHandlerSources(t)
	entries := gatewayPreForwardPipelineEntriesFromRegistrar(GatewayModeratedRouteCoverageEntries())

	for _, entry := range entries {
		coverage, ok := stageCoverage[entry.Handler]
		require.True(t, ok, "handler %s should be present in Gateway pre-forward source coverage scan", entry.Handler)
		require.Equal(t, coverage.HasModerationStage, stageRequiredAndCovered(entry, moderationcoverage.StageModeration),
			"%s %s moderation metadata drifted from handler source coverage", entry.Method, entry.Path)
		require.Equal(t, coverage.HasPreForwardStage, stageRequiredAndCovered(entry, moderationcoverage.StagePreForward),
			"%s %s pre-forward metadata drifted from handler source coverage", entry.Method, entry.Path)
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
		registrarByRoute[moderatedRouteKey(entry.Method, entry.Path, entry.Protocol+" "+entry.Handler)] = entry
	}

	for _, entry := range manifest.Entries {
		routeKey := moderatedRouteKey(entry.Method, entry.Path, entry.Protocol+" "+entry.Handler)
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
			expectedPipeline = moderationcoverage.PipelineGatewayPreForward
			expectedStages = moderationcoverage.GatewayPreForwardPipelineStagesForRoute(entry.Handler, entry.Protocol)
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
	case "/v1/images/batches/:id/cancel", "/v1/stt", "/v1/custom-voices":
		return false
	}
	switch path {
	case "/messages/count_tokens",
		"/responses", "/responses/*subpath", "/alpha/search",
		"/chat/completions",
		"/embeddings",
		"/images/generations", "/images/edits",
		"/images/generations/async", "/images/edits/async",
		"/videos", "/videos/generations", "/videos/edits", "/videos/extensions",
		"/tts", "/web_search":
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
		case "openai_chat_completions", "openai_messages", "openai_responses":
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

type gatewayPreForwardHandlerStageCoverage struct {
	HasModerationStage bool
	HasPreForwardStage bool
	HasBillingStage    bool
	HasRoutingStage    bool
	HasForwardStage    bool
	HasUsageStage      bool
}

func gatewayPreForwardStageCoverageFromHandlerSources(t *testing.T) map[string]gatewayPreForwardHandlerStageCoverage {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	handlerDir := filepath.Join(repoRoot, "backend", "internal", "handler")
	files := []string{
		filepath.Join(handlerDir, "gateway_handler.go"),
		filepath.Join(handlerDir, "gateway_handler_chat_completions.go"),
		filepath.Join(handlerDir, "gateway_handler_responses.go"),
		filepath.Join(handlerDir, "gemini_v1beta_handler.go"),
	}
	coverageByHandler := make(map[string]gatewayPreForwardHandlerStageCoverage)

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
			handlerName, ok := gatewayPreForwardHandlerStageCoverageName(fn)
			if !ok {
				continue
			}
			coverage := gatewayPreForwardHandlerStageCoverage{}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch {
				case isGatewayPreForwardRequestFromContextCall(call):
					coverage.HasModerationStage = true
					coverage.HasPreForwardStage = true
				case isGatewayBillingStageCall(call):
					coverage.HasBillingStage = true
				case isGatewayRoutingStageCall(call):
					coverage.HasRoutingStage = true
				case isGatewayForwardStageCall(call):
					coverage.HasForwardStage = true
				case isGatewayUsageStageCall(call):
					coverage.HasUsageStage = true
				}
				return true
			})
			coverageByHandler[handlerName] = coverage
		}
	}
	return coverageByHandler
}

func openAIHTTPStageCoverageFromHandlerSources(t *testing.T) map[string]openAIHTTPHandlerStageCoverage {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	handlerDir := filepath.Join(repoRoot, "backend", "internal", "handler")
	files := []string{
		filepath.Join(handlerDir, "gateway_web_search.go"),
		filepath.Join(handlerDir, "grok_audio.go"),
		filepath.Join(handlerDir, "openai_alpha_search.go"),
		filepath.Join(handlerDir, "openai_chat_completions.go"),
		filepath.Join(handlerDir, "openai_embeddings.go"),
		filepath.Join(handlerDir, "openai_gateway_handler.go"),
		filepath.Join(handlerDir, "openai_images.go"),
		filepath.Join(handlerDir, "grok_media.go"),
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
					case isOpenAIHTTPBillingStageCall(n):
						coverage.HasBillingStage = true
					case isOpenAIHTTPRoutingStageCall(n):
						coverage.HasRoutingStage = true
					case isOpenAIHTTPForwardStageCall(n):
						coverage.HasForwardStage = true
					case isOpenAIHTTPUsageStageCall(n):
						coverage.HasUsageStage = true
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
	mergeOpenAIHTTPGatewayEntrypointStageCoverage(t, coverageByHandler)

	return coverageByHandler
}

func mergeOpenAIHTTPGatewayEntrypointStageCoverage(t *testing.T, coverageByHandler map[string]openAIHTTPHandlerStageCoverage) {
	t.Helper()

	protocols := openAIHTTPGatewayEntrypointProtocolsFromHandlerSources(t)
	for _, protocol := range protocols {
		switch protocol {
		case "openai_chat_completions":
			coverage := coverageByHandler["OpenAIGatewayHandler.ChatCompletions"]
			coverage.Protocol = protocol
			coverage.HasHTTPPreForwardPipeline = true
			coverage.HasModerationStage = true
			coverage.HasCyberStage = true
			coverage.ModerationLocations = append(coverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverage.CyberLocations = append(coverage.CyberLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.ChatCompletions"] = coverage
		case "openai_messages":
			coverage := coverageByHandler["OpenAIGatewayHandler.Messages"]
			coverage.Protocol = protocol
			coverage.HasHTTPPreForwardPipeline = true
			coverage.HasModerationStage = true
			coverage.HasCyberStage = true
			coverage.ModerationLocations = append(coverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverage.CyberLocations = append(coverage.CyberLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.Messages"] = coverage
		case "openai_responses":
			coverage := coverageByHandler["OpenAIGatewayHandler.Responses"]
			coverage.Protocol = protocol
			coverage.HasHTTPPreForwardPipeline = true
			coverage.HasModerationStage = true
			coverage.HasCyberStage = true
			coverage.HasImageStage = true
			coverage.ModerationLocations = append(coverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverage.CyberLocations = append(coverage.CyberLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.Responses"] = coverage
			alphaCoverage := coverageByHandler["OpenAIGatewayHandler.AlphaSearch"]
			alphaCoverage.Protocol = protocol
			alphaCoverage.HasHTTPPreForwardPipeline = true
			alphaCoverage.HasModerationStage = true
			alphaCoverage.HasCyberStage = true
			alphaCoverage.HasImageStage = true
			alphaCoverage.ModerationLocations = append(alphaCoverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			alphaCoverage.CyberLocations = append(alphaCoverage.CyberLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.AlphaSearch"] = alphaCoverage
			for _, handlerName := range []string{"OpenAIGatewayHandler.GrokVoice", "GatewayHandler.WebSearch"} {
				aliasCoverage := coverageByHandler[handlerName]
				aliasCoverage.Protocol = protocol
				aliasCoverage.HasHTTPPreForwardPipeline = true
				aliasCoverage.HasModerationStage = true
				aliasCoverage.HasBillingStage = true
				aliasCoverage.HasRoutingStage = true
				aliasCoverage.HasForwardStage = true
				aliasCoverage.HasUsageStage = true
				aliasCoverage.ModerationLocations = append(aliasCoverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
				coverageByHandler[handlerName] = aliasCoverage
			}
		case "openai_images":
			coverage := coverageByHandler["OpenAIGatewayHandler.Images"]
			coverage.Protocol = protocol
			coverage.HasHTTPPreForwardPipeline = true
			coverage.HasModerationStage = true
			coverage.HasImageStage = true
			coverage.ModerationLocations = append(coverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.Images"] = coverage
			for _, handlerName := range []string{"OpenAIGatewayHandler.GrokVideoGeneration", "OpenAIGatewayHandler.GrokVideoEdit", "OpenAIGatewayHandler.GrokVideoExtension"} {
				videoCoverage := coverageByHandler[handlerName]
				videoCoverage.Protocol = protocol
				videoCoverage.HasHTTPPreForwardPipeline = true
				videoCoverage.HasModerationStage = true
				videoCoverage.HasImageStage = true
				videoCoverage.HasBillingStage = true
				videoCoverage.HasRoutingStage = true
				videoCoverage.HasForwardStage = true
				videoCoverage.HasUsageStage = true
				videoCoverage.ModerationLocations = append(videoCoverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
				coverageByHandler[handlerName] = videoCoverage
			}
		case "openai_embeddings":
			coverage := coverageByHandler["OpenAIGatewayHandler.Embeddings"]
			coverage.Protocol = protocol
			coverage.HasHTTPPreForwardPipeline = true
			coverage.HasModerationStage = true
			coverage.ModerationLocations = append(coverage.ModerationLocations, "backend/internal/handler/content_moderation_guard.go:EnterOpenAIHTTPGatewayPipeline")
			coverageByHandler["OpenAIGatewayHandler.Embeddings"] = coverage
		}
	}
}

func gatewayPreForwardHandlerStageCoverageName(fn *ast.FuncDecl) (string, bool) {
	switch fn.Name.Name {
	case "Messages", "CountTokens", "GeminiV1BetaModels", "ChatCompletions", "Responses":
	default:
		return "", false
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	receiver := strings.TrimPrefix(astExprLastIdentName(fn.Recv.List[0].Type), "*")
	if receiver != "GatewayHandler" {
		return "", false
	}
	return "GatewayHandler." + fn.Name.Name, true
}

func openAIHTTPHandlerStageCoverageName(fn *ast.FuncDecl) (string, bool) {
	switch fn.Name.Name {
	case "ChatCompletions", "Responses", "AlphaSearch", "Images", "GrokVideoGeneration", "GrokVideoEdit", "GrokVideoExtension", "GrokVoice", "Embeddings", "Messages", "WebSearch":
	default:
		return "", false
	}
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	receiver := astExprLastIdentName(fn.Recv.List[0].Type)
	if receiver != "OpenAIGatewayHandler" && !(receiver == "GatewayHandler" && fn.Name.Name == "WebSearch") {
		return "", false
	}
	return receiver + "." + fn.Name.Name, true
}

func isOpenAIHTTPModeratedHandler(handler string) bool {
	switch strings.TrimSpace(handler) {
	case "OpenAIGatewayHandler.ChatCompletions", "OpenAIGatewayHandler.Messages", "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.AlphaSearch", "OpenAIGatewayHandler.Images", "OpenAIGatewayHandler.GrokVideoGeneration", "OpenAIGatewayHandler.GrokVideoEdit", "OpenAIGatewayHandler.GrokVideoExtension", "OpenAIGatewayHandler.GrokVoice", "OpenAIGatewayHandler.Embeddings", "GatewayHandler.WebSearch":
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
	case "openai_chat_completions", "openai_messages", "openai_responses", "openai_images", "openai_embeddings":
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

	moderatedRegistrars := collectGatewayPipelineRegistrarNames(parsed)
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

func collectGatewayPipelineRegistrarNames(file *ast.File) map[string]struct{} {
	names := make(map[string]struct{})
	ast.Inspect(file, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok || callName(call) != "NewGatewayPipelineRegistrar" || i >= len(assign.Lhs) {
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

func legacyModeratedRouteRegistrarNamesFromSource(t *testing.T, file string) []string {
	t.Helper()

	src, err := os.ReadFile(file)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	registrars := make([]string, 0)
	ast.Inspect(parsed, func(node ast.Node) bool {
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
				pos := fset.Position(call.Pos())
				registrars = append(registrars, fmt.Sprintf("%s %s",
					sourceLocation(repoRootFromTestFile(t), file, pos.Line), ident.Name))
			}
		}
		return true
	})
	sort.Strings(registrars)
	return registrars
}

func gatewayPipelineEntrypointKeysFromSource(t *testing.T, file string) []string {
	t.Helper()

	src, err := os.ReadFile(file)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	keys := make(map[string]struct{})
	ast.Inspect(parsed, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || astExprLastIdentName(lit.Type) != "GatewayPipelineEntrypoints" {
			return true
		}
		for _, element := range lit.Elts {
			kv, ok := element.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			keys[exprString(kv.Key)] = struct{}{}
		}
		return true
	})
	return sortedRouteSet(keys)
}

func gatewayRouteRegistrationInlinePipelineDispatchesFromSource(t *testing.T, file string) []string {
	t.Helper()

	src, err := os.ReadFile(file)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	violations := make([]string, 0)
	registerFn := functionDeclByName(parsed, "RegisterGatewayRoutes")
	require.NotNil(t, registerFn)
	ast.Inspect(registerFn.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SwitchStmt:
			if exprString(typed.Tag) == "meta.Pipeline" {
				pos := fset.Position(typed.Pos())
				violations = append(violations, fmt.Sprintf("%s inline meta.Pipeline switch",
					sourceLocation(repoRootFromTestFile(t), file, pos.Line)))
			}
		case *ast.CallExpr:
			call := exprString(typed.Fun)
			switch {
			case strings.Contains(call, "EnterOpenAIHTTPGatewayPipeline"),
				strings.Contains(call, "EnterOpenAIWebSocketGatewayPipeline"),
				strings.Contains(call, "EnterGatewayPreForwardPipeline"):
				pos := fset.Position(typed.Pos())
				violations = append(violations, fmt.Sprintf("%s direct %s call",
					sourceLocation(repoRootFromTestFile(t), file, pos.Line), call))
			}
		}
		return true
	})
	sort.Strings(violations)
	return violations
}

func gatewayRouteRegistrationDirectBranchEntrypointsFromSource(t *testing.T, file string) []string {
	t.Helper()

	src, err := os.ReadFile(file)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	violations := make([]string, 0)
	registerFn := functionDeclByName(parsed, "RegisterGatewayRoutes")
	require.NotNil(t, registerFn)
	ast.Inspect(registerFn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "EnterBranchPipeline" {
			return true
		}
		pos := fset.Position(call.Pos())
		violations = append(violations, sourceLocation(repoRootFromTestFile(t), file, pos.Line))
		return true
	})
	sort.Strings(violations)
	return violations
}

func functionDeclByName(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func exprString(expr ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
		return ""
	}
	return buf.String()
}

func isModeratedRouteRegistrarConstructor(name string) bool {
	switch name {
	case "NewModeratedRouteRegistrar", "NewGatewayPipelineRegistrar":
		return true
	default:
		return false
	}
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
	for _, protocol := range openAIHTTPGatewayEntrypointProtocolsFromHandlerSources(t) {
		protocolSet[protocol] = struct{}{}
	}
	return sortedRouteSet(protocolSet)
}

func openAIHTTPGatewayEntrypointProtocolsFromHandlerSources(t *testing.T) []string {
	t.Helper()

	repoRoot := repoRootFromTestFile(t)
	file := filepath.Join(repoRoot, "backend", "internal", "handler", "content_moderation_guard.go")
	src, err := os.ReadFile(file)
	require.NoError(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	require.NoError(t, err)

	protocolSet := make(map[string]struct{})
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "EnterOpenAIHTTPGatewayPipeline" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			caseClause, ok := node.(*ast.CaseClause)
			if ok {
				for _, expr := range caseClause.List {
					protocol := serviceProtocolConstantValue(astExprLastIdentName(expr))
					if isOpenAIHTTPModerationProtocol(protocol) {
						protocolSet[protocol] = struct{}{}
					}
				}
				return true
			}
			call, ok := node.(*ast.CallExpr)
			if !ok || !isOpenAIHTTPPreForwardPipelineCall(call) {
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

func isOpenAIHTTPBillingStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPBillingStage"
}

func isOpenAIHTTPRoutingStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPRoutingStage"
}

func isOpenAIHTTPForwardStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPForwardStage"
}

func isOpenAIHTTPUsageStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runOpenAIHTTPUsageStage"
}

func isGatewayPreForwardRequestFromContextCall(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "gatewayPreForwardRequestFromContext"
}

func isGatewayBillingStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runGatewayBillingStage"
}

func isGatewayRoutingStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runGatewayRoutingStage"
}

func isGatewayForwardStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runGatewayForwardStage"
}

func isGatewayUsageStageCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "runGatewayUsageStage"
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
	case "ContentModerationProtocolOpenAIMessages":
		return "openai_messages"
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

func gatewayPreForwardPipelineEntriesFromRegistrar(entries []ModeratedRouteMeta) []ModeratedRouteMeta {
	out := make([]ModeratedRouteMeta, 0)
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if entry.Pipeline != moderationcoverage.PipelineGatewayPreForward {
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
