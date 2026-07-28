package main

import (
	"strings"
	"testing"
)

func TestExtractGoSymbolsAndContracts(t *testing.T) {
	source := []byte(`package sample

import (
	"os"
	"github.com/gin-gonic/gin"
)

type Request struct {
	RequestID string ` + "`json:\"request_id\" mapstructure:\"request_id\"`" + `
}

func (h *Handler) Register(group *gin.RouterGroup) {
	group.POST("/v1/responses", h.Responses)
	_ = os.Getenv("UPSTREAM_TOKEN")
	h.Forward()
}
`)
	index := extractSource("sample.go", "blob", source)

	if !containsSymbol(index.Symbols, "Handler.Register") {
		t.Fatalf("missing method symbol: %#v", index.Symbols)
	}
	assertContains(t, index.Contracts["json"], "request_id")
	assertContains(t, index.Contracts["mapstructure"], "request_id")
	assertContains(t, index.Contracts["route"], "POST /v1/responses")
	assertContains(t, index.Contracts["env"], "UPSTREAM_TOKEN")
}

func TestExtractVueContracts(t *testing.T) {
	source := []byte(`<script setup lang="ts">
const props = defineProps<{ requestId: string; enabled?: boolean }>()
const emit = defineEmits<{ (event: 'saved'): void }>()
const label = t('admin.usage.excludeUsers')
api.get('/api/v1/usage')
const base = import.meta.env.VITE_API_BASE_URL
</script>`)
	index := extractSource("UsageView.vue", "blob", source)

	if !containsSymbol(index.Symbols, "UsageView") {
		t.Fatalf("missing component symbol: %#v", index.Symbols)
	}
	assertContains(t, index.Contracts["prop"], "requestId")
	assertContains(t, index.Contracts["i18n"], "admin.usage.excludeUsers")
	assertContains(t, index.Contracts["api"], "GET /api/v1/usage")
	assertContains(t, index.Contracts["env"], "VITE_API_BASE_URL")
}

func TestCompareIndexesReportsDeclarationAndContractChanges(t *testing.T) {
	oldIndex := extractSource("sample.go", "old", []byte(`package sample
func Keep() string { return "old_key" }
func Removed() {}
`))
	newIndex := extractSource("sample.go", "new", []byte(`package sample
func Keep() string { return "new_key" }
func Added() {}
`))
	changed, added, deleted, contracts := compareIndexes(oldIndex, newIndex)

	assertContains(t, changed, "Keep")
	assertContains(t, added, "Added")
	assertContains(t, deleted, "Removed")
	if len(contracts["field"]) == 0 {
		t.Fatalf("expected field contract delta: %#v", contracts)
	}
}

func TestMatchPathPatternSupportsRecursivePatterns(t *testing.T) {
	for _, test := range []struct {
		pattern string
		path    string
		want    bool
	}{
		{"backend/internal/service/content_moderation*.go", "backend/internal/service/content_moderation_guard.go", true},
		{"frontend/src/**/*.vue", "frontend/src/views/admin/SettingsView.vue", true},
		{"backend/ent/**", "backend/ent/schema/user.go", true},
		{"deploy/**", "frontend/src/App.vue", false},
	} {
		if got := matchPathPattern(test.pattern, test.path); got != test.want {
			t.Errorf("matchPathPattern(%q, %q)=%t want %t", test.pattern, test.path, got, test.want)
		}
	}
}

func containsSymbol(symbols []Symbol, expected string) bool {
	for _, symbol := range symbols {
		if symbol.Name == expected {
			return true
		}
	}
	return false
}

func assertContains(t *testing.T, values []string, expected string) {
	t.Helper()
	for _, value := range values {
		if value == expected {
			return
		}
	}
	t.Fatalf("%q not found in %s", expected, strings.Join(values, ", "))
}
