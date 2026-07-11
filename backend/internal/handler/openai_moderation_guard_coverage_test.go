package handler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOpenAIEntryPointsUseModerationGuardForOpenAIProtocols(t *testing.T) {
	files, err := filepath.Glob("openai*.go")
	if err != nil {
		t.Fatalf("glob OpenAI handler files: %v", err)
	}
	sort.Strings(files)

	var violations []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "checkContentModeration" {
				return true
			}

			protocol := contentModerationProtocolArg(call)
			if strings.HasPrefix(protocol, "ContentModerationProtocolOpenAI") {
				pos := fset.Position(selector.Pos())
				violations = append(violations, fmt.Sprintf(
					"%s:%d calls h.checkContentModeration with service.%s",
					file,
					pos.Line,
					protocol,
				))
			}

			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf(
			"OpenAI protocol moderation must go through moderationGuard; direct calls are only allowed for non-OpenAI protocols such as ContentModerationProtocolAnthropicMessages:\n%s",
			strings.Join(violations, "\n"),
		)
	}
}

func TestOpenAIEntryPointsUseUnifiedModerationGuardHelper(t *testing.T) {
	files, err := filepath.Glob("openai*.go")
	if err != nil {
		t.Fatalf("glob OpenAI handler files: %v", err)
	}
	sort.Strings(files)

	var violations []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}

		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range assign.Rhs {
				selector, ok := rhs.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "moderationGuard" {
					continue
				}
				if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "h" {
					pos := fset.Position(selector.Pos())
					violations = append(violations, fmt.Sprintf(
						"%s:%d reads h.moderationGuard directly; use h.checkWithModerationGuard",
						file,
						pos.Line,
					))
				}
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Fatalf("OpenAI entrypoints must use the unified moderation guard helper:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCountTokensModerationCoverageUsesAuditedPreForwardBodyBeforeFallbackRead(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_count_tokens.go")
	if err != nil {
		t.Fatalf("read CountTokens handler: %v", err)
	}

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "openai_gateway_count_tokens.go", src, 0)
	if err != nil {
		t.Fatalf("parse CountTokens handler: %v", err)
	}

	var cachedBodyCall token.Pos
	var fallbackBodyRead token.Pos
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "CountTokens" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "gatewayPreForwardRequestFromContext":
				cachedBodyCall = ident.Pos()
			case "readLenientJSONRequestBodyWithPrealloc":
				fallbackBodyRead = ident.Pos()
			}
			return true
		})
	}

	if cachedBodyCall == token.NoPos {
		t.Fatal("OpenAIGatewayHandler.CountTokens must consume the audited gateway pre-forward request from context")
	}
	if fallbackBodyRead == token.NoPos {
		t.Fatal("OpenAIGatewayHandler.CountTokens must retain a direct-handler request-body fallback")
	}
	if cachedBodyCall >= fallbackBodyRead {
		t.Fatalf(
			"CountTokens must prefer audited bytes before the direct-handler fallback (cached at %s, fallback at %s)",
			fset.Position(cachedBodyCall),
			fset.Position(fallbackBodyRead),
		)
	}
}

func contentModerationProtocolArg(call *ast.CallExpr) string {
	for _, arg := range call.Args {
		selector, ok := arg.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if strings.HasPrefix(selector.Sel.Name, "ContentModerationProtocol") {
			return selector.Sel.Name
		}
	}
	return ""
}
