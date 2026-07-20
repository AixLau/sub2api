package handler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type gatewayModerationCoverageManifest struct {
	SchemaVersion int                              `json:"schema_version"`
	Entries       []gatewayModerationCoverageEntry `json:"entries"`
}

type gatewayModerationCoverageEntry struct {
	Method             string `json:"method"`
	Path               string `json:"path"`
	Handler            string `json:"handler"`
	Upstream           bool   `json:"upstream"`
	ModerationRequired bool   `json:"moderation_required"`
	Protocol           string `json:"protocol"`
	Status             string `json:"status"`
	ReviewReason       string `json:"review_reason"`
}

func TestGatewayModerationCoverageManifestDefinesCriticalUpstreamEntrypoints(t *testing.T) {
	manifest := loadGatewayModerationCoverageManifest(t)
	if manifest.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", manifest.SchemaVersion)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("coverage manifest has no entries")
	}

	byRouteProtocolHandler := make(map[string]gatewayModerationCoverageEntry, len(manifest.Entries))
	for i, entry := range manifest.Entries {
		if strings.TrimSpace(entry.Method) == "" {
			t.Fatalf("entries[%d] missing method", i)
		}
		if strings.TrimSpace(entry.Path) == "" {
			t.Fatalf("entries[%d] missing path", i)
		}
		if strings.TrimSpace(entry.Handler) == "" {
			t.Fatalf("entries[%d] missing handler", i)
		}
		if entry.Upstream && entry.ModerationRequired && strings.TrimSpace(entry.Protocol) == "" {
			t.Fatalf("entries[%d] path %q requires moderation but has no protocol", i, entry.Path)
		}
		if !validGatewayModerationCoverageStatus(entry.Status) {
			t.Fatalf("entries[%d] path %q has invalid status %q", i, entry.Path, entry.Status)
		}
		if entry.Status == "intentional_no_audit" && strings.TrimSpace(entry.ReviewReason) == "" {
			t.Fatalf("entries[%d] path %q is intentional_no_audit but missing review_reason", i, entry.Path)
		}
		routeProtocolHandlerKey := entry.Method + " " + entry.Path + " " + entry.Protocol + " " + entry.Handler
		if previous, ok := byRouteProtocolHandler[routeProtocolHandlerKey]; ok {
			t.Fatalf("duplicate route branch %s for handlers %q and %q", routeProtocolHandlerKey, previous.Handler, entry.Handler)
		}
		byRouteProtocolHandler[routeProtocolHandlerKey] = entry
	}

	requiredCovered := map[string]string{
		"POST /v1/messages":                            "anthropic_messages",
		"POST /v1/messages openai_messages":            "openai_messages",
		"POST /antigravity/v1/messages":                "anthropic_messages",
		"POST /v1/messages/count_tokens":               "anthropic_messages",
		"POST /messages/count_tokens":                  "anthropic_messages",
		"POST /antigravity/v1/messages/count_tokens":   "anthropic_messages",
		"POST /v1/chat/completions":                    "openai_chat_completions",
		"POST /chat/completions":                       "openai_chat_completions",
		"POST /v1/responses":                           "openai_responses",
		"POST /v1/responses/*subpath":                  "openai_responses",
		"POST /responses":                              "openai_responses",
		"POST /responses/*subpath":                     "openai_responses",
		"GET /v1/responses":                            "openai_responses",
		"GET /responses":                               "openai_responses",
		"POST /backend-api/codex/responses":            "openai_responses",
		"POST /backend-api/codex/responses/*subpath":   "openai_responses",
		"GET /backend-api/codex/responses":             "openai_responses",
		"POST /v1/embeddings":                          "openai_embeddings",
		"POST /embeddings":                             "openai_embeddings",
		"POST /v1/images/generations":                  "openai_images",
		"POST /v1/images/edits":                        "openai_images",
		"POST /images/generations":                     "openai_images",
		"POST /images/edits":                           "openai_images",
		"POST /v1beta/models/*modelAction":             "gemini",
		"POST /antigravity/v1beta/models/*modelAction": "gemini",
	}

	for route, protocol := range requiredCovered {
		lookupKey := route
		if !strings.HasSuffix(route, " "+protocol) {
			lookupKey = route + " " + protocol
		}
		var entry gatewayModerationCoverageEntry
		ok := false
		for _, candidate := range byRouteProtocolHandler {
			if candidate.Method+" "+candidate.Path+" "+candidate.Protocol == lookupKey {
				entry = candidate
				ok = true
				break
			}
		}
		if !ok {
			t.Fatalf("required gateway route %q missing from moderation coverage manifest", route)
		}
		if !entry.Upstream || !entry.ModerationRequired || entry.Status != "covered" {
			t.Fatalf("route %q coverage invalid: upstream=%v moderation_required=%v status=%q", route, entry.Upstream, entry.ModerationRequired, entry.Status)
		}
		if entry.Protocol != protocol {
			t.Fatalf("route %q protocol = %q, want %q", route, entry.Protocol, protocol)
		}
	}
}

func loadGatewayModerationCoverageManifest(t *testing.T) gatewayModerationCoverageManifest {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	path := filepath.Join(repoRoot, "docs", "risk-control", "content-moderation-gateway-coverage.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var manifest gatewayModerationCoverageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return manifest
}

func validGatewayModerationCoverageStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "covered", "planned", "intentional_no_audit":
		return true
	default:
		return false
	}
}
