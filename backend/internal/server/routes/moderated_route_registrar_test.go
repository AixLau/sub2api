package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type moderatedRouteCoverageManifest struct {
	SchemaVersion int                                   `json:"schema_version"`
	Entries       []moderatedRouteCoverageManifestEntry `json:"entries"`
}

type moderatedRouteCoverageManifestEntry struct {
	Method             string `json:"method"`
	Path               string `json:"path"`
	Upstream           bool   `json:"upstream"`
	ModerationRequired bool   `json:"moderation_required"`
	Protocol           string `json:"protocol"`
	Status             string `json:"status"`
	Handler            string `json:"handler"`
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
