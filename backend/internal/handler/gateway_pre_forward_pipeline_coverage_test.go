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
		"gateway_handler_chat_completions.go": {"ChatCompletions"},
		"gateway_handler_responses.go":        {"Responses"},
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

func TestAnthropicMessagesPreForwardRunsThroughGatewayPipelineRegistrar(t *testing.T) {
	requireGatewayHandlerPreForwardThroughRegistrar(t, "Messages")
}

func TestGatewayCountTokensPreForwardRunsThroughGatewayPipelineRegistrar(t *testing.T) {
	requireGatewayHandlerPreForwardThroughRegistrar(t, "CountTokens")
}

func TestGatewayGeminiV1BetaModelsPreForwardRunsThroughGatewayPipelineRegistrar(t *testing.T) {
	requireGatewayHandlerPreForwardThroughRegistrarInFile(t, "gemini_v1beta_handler.go", "GeminiV1BetaModels")
}

func requireGatewayHandlerPreForwardThroughRegistrar(t *testing.T, handlerName string) {
	requireGatewayHandlerPreForwardThroughRegistrarInFile(t, "gateway_handler.go", handlerName)
}

func requireGatewayHandlerPreForwardThroughRegistrarInFile(t *testing.T, fileName, handlerName string) {
	t.Helper()
	src, err := os.ReadFile(fileName)
	if err != nil {
		t.Fatalf("read %s: %v", fileName, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, fileName, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", fileName, err)
	}

	fn := gatewayHandlerFuncDecl(t, parsed, handlerName)
	var directPipelineCalls []int
	hasCachedPreForwardRequest := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fun.Sel.Name == "runGatewayPreForwardPipeline" {
				directPipelineCalls = append(directPipelineCalls, fset.Position(fun.Pos()).Line)
			}
		case *ast.Ident:
			if fun.Name == "gatewayPreForwardRequestFromContext" {
				hasCachedPreForwardRequest = true
			}
		}
		return true
	})

	if len(directPipelineCalls) > 0 {
		t.Fatalf("GatewayHandler.%s must receive pre-forward admission from GatewayPipelineRegistrar instead of calling runGatewayPreForwardPipeline directly at lines %v", handlerName, directPipelineCalls)
	}
	if !hasCachedPreForwardRequest {
		t.Fatalf("GatewayHandler.%s must consume the GatewayPipelineRegistrar pre-forward request cache", handlerName)
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

func TestGatewayCountTokensUsesNamedForwardStageAdapter(t *testing.T) {
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
	hasNamedAdapter := false
	hasAnonymousForwardStageAdapter := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "GatewayCountTokensForwardStage":
			hasNamedAdapter = true
		case "ForwardStageAdapter":
			hasAnonymousForwardStageAdapter = true
		}
		return true
	})

	if !hasNamedAdapter {
		t.Fatalf("GatewayHandler.CountTokens must pass GatewayCountTokensForwardStage to runGatewayForwardStage")
	}
	if hasAnonymousForwardStageAdapter {
		t.Fatalf("GatewayHandler.CountTokens must not wrap forwarding with anonymous ForwardStageAdapter")
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

func TestGatewayMessagesUsesNamedForwardStageAdapters(t *testing.T) {
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
	namedAdapters := map[string]bool{
		"GatewayMessagesGeminiForwardStage": false,
		"GatewayMessagesForwardStage":       false,
	}
	var anonymousForwardStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch name := compositeTypeName(lit.Type); name {
		case "GatewayMessagesGeminiForwardStage", "GatewayMessagesForwardStage":
			namedAdapters[name] = true
		case "ForwardStageAdapter":
			anonymousForwardStageAdapterLines = append(anonymousForwardStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	for name, seen := range namedAdapters {
		if !seen {
			t.Fatalf("GatewayHandler.Messages must pass %s to runGatewayForwardStage", name)
		}
	}
	if len(anonymousForwardStageAdapterLines) > 0 {
		t.Fatalf("GatewayHandler.Messages must not wrap forwarding with anonymous ForwardStageAdapter at lines %v", anonymousForwardStageAdapterLines)
	}
}

func TestGatewayGeminiV1BetaModelsUsesForwardStageAdapter(t *testing.T) {
	src, err := os.ReadFile("gemini_v1beta_handler.go")
	if err != nil {
		t.Fatalf("read gemini_v1beta_handler.go: %v", err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gemini_v1beta_handler.go", src, 0)
	if err != nil {
		t.Fatalf("parse gemini_v1beta_handler.go: %v", err)
	}

	fn := gatewayHandlerFuncDecl(t, parsed, "GeminiV1BetaModels")
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
		t.Fatalf("GatewayHandler.GeminiV1BetaModels must execute upstream forwarding through runGatewayForwardStage")
	}
}

func TestGatewayGeminiV1BetaModelsUsesNamedForwardStageAdapter(t *testing.T) {
	src, err := os.ReadFile("gemini_v1beta_handler.go")
	if err != nil {
		t.Fatalf("read gemini_v1beta_handler.go: %v", err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gemini_v1beta_handler.go", src, 0)
	if err != nil {
		t.Fatalf("parse gemini_v1beta_handler.go: %v", err)
	}

	fn := gatewayHandlerFuncDecl(t, parsed, "GeminiV1BetaModels")
	hasNamedAdapter := false
	var anonymousForwardStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "GatewayGeminiV1BetaForwardStage":
			hasNamedAdapter = true
		case "ForwardStageAdapter":
			anonymousForwardStageAdapterLines = append(anonymousForwardStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	if !hasNamedAdapter {
		t.Fatalf("GatewayHandler.GeminiV1BetaModels must pass GatewayGeminiV1BetaForwardStage to runGatewayForwardStage")
	}
	if len(anonymousForwardStageAdapterLines) > 0 {
		t.Fatalf("GatewayHandler.GeminiV1BetaModels must not wrap forwarding with anonymous ForwardStageAdapter at lines %v", anonymousForwardStageAdapterLines)
	}
}

func TestGatewayMessagesAndGeminiUseUsageStageAdapter(t *testing.T) {
	tests := []struct {
		file     string
		handler  string
		minCalls int
	}{
		{file: "gateway_handler.go", handler: "Messages", minCalls: 2},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels", minCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}

			fn := gatewayHandlerFuncDecl(t, parsed, tt.handler)
			usageStageCalls := 0
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "runGatewayUsageStage" {
					usageStageCalls++
				}
				return true
			})

			if usageStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute usage recording through runGatewayUsageStage, got %d calls", tt.handler, usageStageCalls)
			}
		})
	}
}

func TestGatewayMessagesAndGeminiUseNamedUsageStageAdapter(t *testing.T) {
	tests := []struct {
		file     string
		handler  string
		minCalls int
	}{
		{file: "gateway_handler.go", handler: "Messages", minCalls: 2},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels", minCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}

			fn := gatewayHandlerFuncDecl(t, parsed, tt.handler)
			namedUsageStageCalls := 0
			var anonymousUsageStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "GatewayUsageStage":
					namedUsageStageCalls++
				case "UsageStageAdapter":
					anonymousUsageStageAdapterLines = append(anonymousUsageStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			if namedUsageStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must pass GatewayUsageStage to runGatewayUsageStage, got %d calls", tt.handler, namedUsageStageCalls)
			}
			if len(anonymousUsageStageAdapterLines) > 0 {
				t.Fatalf("GatewayHandler.%s must not wrap usage with anonymous UsageStageAdapter at lines %v", tt.handler, anonymousUsageStageAdapterLines)
			}
		})
	}
}

func TestGatewayPreForwardHandlersUseBillingStage(t *testing.T) {
	tests := []struct {
		file     string
		handler  string
		minCalls int
	}{
		{file: "gateway_handler.go", handler: "Messages", minCalls: 2},
		{file: "gateway_handler.go", handler: "CountTokens", minCalls: 1},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels", minCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}

			fn := gatewayHandlerFuncDecl(t, parsed, tt.handler)
			billingStageCalls := 0
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "runGatewayBillingStage" {
					billingStageCalls++
				}
				return true
			})

			if billingStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute billing checks through runGatewayBillingStage, got %d calls", tt.handler, billingStageCalls)
			}
		})
	}
}

func TestGatewayPreForwardHandlersUseRoutingStage(t *testing.T) {
	tests := []struct {
		file     string
		handler  string
		minCalls int
	}{
		{file: "gateway_handler.go", handler: "Messages", minCalls: 2},
		{file: "gateway_handler.go", handler: "CountTokens", minCalls: 1},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels", minCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", tt.file, err)
			}

			fn := gatewayHandlerFuncDecl(t, parsed, tt.handler)
			routingStageCalls := 0
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "runGatewayRoutingStage" {
					routingStageCalls++
				}
				return true
			})

			if routingStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute account selection through runGatewayRoutingStage, got %d calls", tt.handler, routingStageCalls)
			}
		})
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

func compositeTypeName(expr ast.Expr) string {
	switch typ := expr.(type) {
	case *ast.Ident:
		return typ.Name
	case *ast.SelectorExpr:
		return typ.Sel.Name
	default:
		return ""
	}
}
