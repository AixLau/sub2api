package handler

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayPreForwardEntrypointsUseUnifiedPipelineHelper(t *testing.T) {
	require.Empty(t, gatewayPreForwardDirectModerationViolations(t),
		"Gateway pre-forward moderation must not bypass the unified pipeline helper")
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

func TestGatewayPreForwardHandlersUseNamedBillingStage(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "gateway_handler.go", handler: "Messages"},
		{file: "gateway_handler.go", handler: "CountTokens"},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels"},
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
			var anonymousBillingLines []int
			hasNamedBillingStage := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if compositeTypeName(lit.Type) != "GatewayBillingStage" {
					return true
				}
				hasNamedBillingStage = true
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if ok && key.Name == "Billing" {
						anonymousBillingLines = append(anonymousBillingLines, fset.Position(kv.Pos()).Line)
					}
				}
				return true
			})

			require.True(t, hasNamedBillingStage, "GatewayHandler.%s must pass GatewayBillingStage to runGatewayBillingStage", tt.handler)
			require.Empty(t, anonymousBillingLines, "GatewayHandler.%s must not configure GatewayBillingStage with anonymous Billing funcs at lines %v", tt.handler, anonymousBillingLines)
		})
	}
}

func TestGatewayChatCompletionsPreForwardRunsThroughGatewayPipelineRegistrar(t *testing.T) {
	requireGatewayHandlerPreForwardThroughRegistrarInFile(t, "gateway_handler_chat_completions.go", "ChatCompletions")
}

func TestGatewayResponsesPreForwardRunsThroughGatewayPipelineRegistrar(t *testing.T) {
	requireGatewayHandlerPreForwardThroughRegistrarInFile(t, "gateway_handler_responses.go", "Responses")
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

func gatewayPreForwardDirectModerationViolations(t *testing.T) []string {
	t.Helper()
	tests := map[string][]string{
		"gateway_handler_chat_completions.go": {"ChatCompletions"},
		"gateway_handler_responses.go":        {"Responses"},
		"gateway_handler.go":                  {"Messages", "CountTokens"},
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
					pos := fset.Position(selector.Pos())
					violations = append(violations, fmt.Sprintf(
						"%s:%d %s calls runGatewayPreForwardPipeline directly",
						file,
						pos.Line,
						fn.Name.Name,
					))
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
		}
		for name := range expected {
			if !seen[name] {
				violations = append(violations, fmt.Sprintf("%s missing GatewayHandler.%s", file, name))
			}
		}
	}
	return violations
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

func TestGatewayChatCompletionsAndResponsesUseForwardStageAdapter(t *testing.T) {
	tests := []struct {
		file         string
		handler      string
		stageAdapter string
		forbidden    []string
	}{
		{
			file:         "gateway_handler_chat_completions.go",
			handler:      "ChatCompletions",
			stageAdapter: "GatewayChatCompletionsForwardStage",
			forbidden:    []string{"ForwardAsChatCompletions"},
		},
		{
			file:         "gateway_handler_responses.go",
			handler:      "Responses",
			stageAdapter: "GatewayResponsesForwardStage",
			forbidden:    []string{"ForwardAsResponses"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
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
			forwardStageCalls := 0
			hasNamedForwardStage := false
			var directForwardLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if ok {
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
						if selector.Sel.Name == "runGatewayForwardStage" {
							forwardStageCalls++
						}
						for _, forbidden := range tt.forbidden {
							if selector.Sel.Name == forbidden {
								directForwardLines = append(directForwardLines, fset.Position(call.Pos()).Line)
							}
						}
					}
				}
				lit, ok := node.(*ast.CompositeLit)
				if ok && compositeTypeName(lit.Type) == tt.stageAdapter {
					hasNamedForwardStage = true
				}
				return true
			})

			if forwardStageCalls == 0 {
				t.Fatalf("GatewayHandler.%s must execute upstream forwarding through runGatewayForwardStage", tt.handler)
			}
			if !hasNamedForwardStage {
				t.Fatalf("GatewayHandler.%s must pass %s to runGatewayForwardStage", tt.handler, tt.stageAdapter)
			}
			if len(directForwardLines) > 0 {
				t.Fatalf("GatewayHandler.%s must not call upstream forward services directly at lines %v", tt.handler, directForwardLines)
			}
		})
	}
}

func TestGatewayForwardStageRunnerUsesRouteDescriptorResolver(t *testing.T) {
	src, err := os.ReadFile("gateway_pre_forward_pipeline.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gateway_pre_forward_pipeline.go", src, 0)
	require.NoError(t, err)

	fn := gatewayHandlerFuncDecl(t, parsed, "runGatewayForwardStage")
	callsDescriptorResolver := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "gatewayForwardStageFromRouteDescriptor" {
			callsDescriptorResolver = true
		}
		return true
	})
	require.True(t, callsDescriptorResolver, "runGatewayForwardStage must resolve forward adapter from route descriptor metadata before execution")
}

func TestGatewayMessagesAndGeminiUseUsageStageAdapter(t *testing.T) {
	tests := []struct {
		file     string
		handler  string
		minCalls int
	}{
		{file: "gateway_handler.go", handler: "Messages", minCalls: 2},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels", minCalls: 1},
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions", minCalls: 1},
		{file: "gateway_handler_responses.go", handler: "Responses", minCalls: 1},
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
			var directRecordUsageLines []int
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
				if selector.Sel.Name == "RecordUsage" || selector.Sel.Name == "RecordUsageWithLongContext" {
					directRecordUsageLines = append(directRecordUsageLines, fset.Position(call.Pos()).Line)
				}
				return true
			})

			if usageStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute usage recording through runGatewayUsageStage, got %d calls", tt.handler, usageStageCalls)
			}
			if len(directRecordUsageLines) > 0 {
				t.Fatalf("GatewayHandler.%s must not call usage repository/service directly at lines %v", tt.handler, directRecordUsageLines)
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
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions", minCalls: 1},
		{file: "gateway_handler_responses.go", handler: "Responses", minCalls: 1},
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
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions", minCalls: 1},
		{file: "gateway_handler_responses.go", handler: "Responses", minCalls: 1},
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
			var directBillingLines []int
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
				if selector.Sel.Name == "CheckBillingEligibility" {
					directBillingLines = append(directBillingLines, fset.Position(call.Pos()).Line)
				}
				return true
			})

			if billingStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute billing checks through runGatewayBillingStage, got %d calls", tt.handler, billingStageCalls)
			}
			require.Empty(t, directBillingLines,
				"GatewayHandler.%s must not call billing service directly at lines %v", tt.handler, directBillingLines)
		})
	}
}

func TestGatewayPreForwardHandlersUseNamedBillingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "gateway_handler.go", handler: "Messages"},
		{file: "gateway_handler.go", handler: "CountTokens"},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels"},
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions"},
		{file: "gateway_handler_responses.go", handler: "Responses"},
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
			hasNamedAdapter := false
			var anonymousBillingStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "GatewayBillingStage":
					hasNamedAdapter = true
				case "BillingStageAdapter":
					anonymousBillingStageAdapterLines = append(anonymousBillingStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			if !hasNamedAdapter {
				t.Fatalf("%s.%s must pass GatewayBillingStage to runGatewayBillingStage", tt.file, tt.handler)
			}
			if len(anonymousBillingStageAdapterLines) > 0 {
				t.Fatalf("%s.%s must not wrap billing with anonymous BillingStageAdapter at lines %v", tt.file, tt.handler, anonymousBillingStageAdapterLines)
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
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions", minCalls: 1},
		{file: "gateway_handler_responses.go", handler: "Responses", minCalls: 1},
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
			var directRoutingLines []int
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
				if selector.Sel.Name == "SelectAccountWithLoadAwareness" || selector.Sel.Name == "SelectAccountForModel" {
					directRoutingLines = append(directRoutingLines, fset.Position(call.Pos()).Line)
				}
				return true
			})

			if routingStageCalls < tt.minCalls {
				t.Fatalf("GatewayHandler.%s must execute account selection through runGatewayRoutingStage, got %d calls", tt.handler, routingStageCalls)
			}
			require.Empty(t, directRoutingLines,
				"GatewayHandler.%s must not call routing service directly at lines %v", tt.handler, directRoutingLines)
		})
	}
}

func TestGatewayPreForwardHandlersUseNamedRoutingStage(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "gateway_handler.go", handler: "Messages"},
		{file: "gateway_handler.go", handler: "CountTokens"},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels"},
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions"},
		{file: "gateway_handler_responses.go", handler: "Responses"},
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
			var anonymousRoutingLines []int
			hasNamedRoutingStage := false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if compositeTypeName(lit.Type) != "GatewayRoutingStage" {
					return true
				}
				hasNamedRoutingStage = true
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if ok && key.Name == "Routing" {
						anonymousRoutingLines = append(anonymousRoutingLines, fset.Position(kv.Pos()).Line)
					}
				}
				return true
			})

			require.True(t, hasNamedRoutingStage, "GatewayHandler.%s must pass GatewayRoutingStage to runGatewayRoutingStage", tt.handler)
			require.Empty(t, anonymousRoutingLines, "GatewayHandler.%s must not configure GatewayRoutingStage with anonymous Routing funcs at lines %v", tt.handler, anonymousRoutingLines)
		})
	}
}

func TestGatewayPreForwardHandlersUseNamedRoutingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "gateway_handler.go", handler: "Messages"},
		{file: "gateway_handler.go", handler: "CountTokens"},
		{file: "gemini_v1beta_handler.go", handler: "GeminiV1BetaModels"},
		{file: "gateway_handler_chat_completions.go", handler: "ChatCompletions"},
		{file: "gateway_handler_responses.go", handler: "Responses"},
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
			hasNamedAdapter := false
			var anonymousRoutingStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "GatewayRoutingStage":
					hasNamedAdapter = true
				case "RoutingStageAdapter":
					anonymousRoutingStageAdapterLines = append(anonymousRoutingStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			if !hasNamedAdapter {
				t.Fatalf("%s.%s must pass GatewayRoutingStage to runGatewayRoutingStage", tt.file, tt.handler)
			}
			if len(anonymousRoutingStageAdapterLines) > 0 {
				t.Fatalf("%s.%s must not wrap routing with anonymous RoutingStageAdapter at lines %v", tt.file, tt.handler, anonymousRoutingStageAdapterLines)
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
