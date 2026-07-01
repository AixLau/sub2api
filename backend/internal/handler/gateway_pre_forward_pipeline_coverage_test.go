package handler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestGatewayPreForwardEntrypointsUseUnifiedPipelineHelper(t *testing.T) {
	tests := map[string][]string{
		"gateway_handler.go":                  {"Messages", "CountTokens"},
		"gateway_handler_chat_completions.go": {"ChatCompletions"},
		"gateway_handler_responses.go":        {"Responses"},
		"gemini_v1beta_handler.go":            {"GeminiV1BetaModels"},
	}

	var violations []string
	for file, functions := range tests {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		expected := make(map[string]struct{}, len(functions))
		for _, name := range functions {
			expected[name] = struct{}{}
		}

		seen := make(map[string]bool, len(functions))
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			if _, ok := expected[fn.Name.Name]; !ok {
				continue
			}
			seen[fn.Name.Name] = true

			hasPipelineCall := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch selector.Sel.Name {
				case "runGatewayPreForwardPipeline":
					hasPipelineCall = true
				case "checkContentModeration":
					pos := fset.Position(selector.Pos())
					violations = append(violations, fmt.Sprintf(
						"%s:%d %s calls checkContentModeration directly",
						file,
						pos.Line,
						fn.Name.Name,
					))
				}
				return true
			})

			if !hasPipelineCall {
				violations = append(violations, fmt.Sprintf(
					"%s %s must call runGatewayPreForwardPipeline",
					file,
					fn.Name.Name,
				))
			}
		}

		for name := range expected {
			if !seen[name] {
				violations = append(violations, fmt.Sprintf("%s missing GatewayHandler.%s", file, name))
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("Anthropic/Gemini pre-forward moderation must use the unified pipeline helper:\n%s", strings.Join(violations, "\n"))
	}
}

func TestGatewayCountTokensUsesForwardStageAdapter(t *testing.T) {
	src, err := os.ReadFile("gateway_handler.go")
	if err != nil {
		t.Fatalf("read gateway_handler.go: %v", err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gateway_handler.go", src, 0)
	if err != nil {
		t.Fatalf("parse gateway_handler.go: %v", err)
	}

	fn := gatewayHandlerFuncDecl(t, parsed, "CountTokens")
	hasForwardStage := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "runGatewayForwardStage" {
			hasForwardStage = true
		}
		return true
	})

	if !hasForwardStage {
		t.Fatalf("GatewayHandler.CountTokens must execute upstream forwarding through runGatewayForwardStage")
	}
}

func TestGatewayMessagesUsesForwardStageAdapterForForwardAttempts(t *testing.T) {
	src, err := os.ReadFile("gateway_handler.go")
	if err != nil {
		t.Fatalf("read gateway_handler.go: %v", err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gateway_handler.go", src, 0)
	if err != nil {
		t.Fatalf("parse gateway_handler.go: %v", err)
	}

	fn := gatewayHandlerFuncDecl(t, parsed, "Messages")
	forwardStageCalls := 0
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "runGatewayForwardStage" {
			forwardStageCalls++
		}
		return true
	})

	if forwardStageCalls < 2 {
		t.Fatalf("GatewayHandler.Messages must execute both upstream forward attempts through runGatewayForwardStage, got %d calls", forwardStageCalls)
	}
}

func gatewayHandlerFuncDecl(t *testing.T, parsed *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	t.Fatalf("missing GatewayHandler.%s", name)
	return nil
}
