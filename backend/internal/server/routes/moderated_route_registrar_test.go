package routes

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
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

func TestOpenAIModeratedRoutesHaveGuardCoverage(t *testing.T) {
	restore := replaceModeratedRouteRegistryForTest(nil)
	defer restore()
	_ = newGatewayRoutesTestRouter()

	openAIProtocols := openAIModeratedRouteProtocolsFromRegistrar(GatewayModeratedRouteCoverageEntries())
	guardProtocols := openAIGuardHelperProtocolsFromHandlerSources(t)

	require.NotEmpty(t, openAIProtocols, "OpenAI moderated route protocols should not be empty")
	require.Empty(t, routeSetDifference(openAIProtocols, guardProtocols),
		"OpenAI moderated route protocols must have a matching checkWithModerationGuard stage in OpenAI handlers")
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

func openAIGuardHelperProtocolsFromHandlerSources(t *testing.T) []string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", ".."))
	handlerDir := filepath.Join(repoRoot, "backend", "internal", "handler")
	files, err := filepath.Glob(filepath.Join(handlerDir, "openai*.go"))
	require.NoError(t, err)
	sort.Strings(files)

	protocolSet := make(map[string]struct{})
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		require.NoError(t, err)
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		require.NoError(t, err)

		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isCheckWithModerationGuardCall(call) {
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

func isCheckWithModerationGuardCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "checkWithModerationGuard"
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
