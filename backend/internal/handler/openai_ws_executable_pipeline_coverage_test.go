package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesWebSocketUsesExecutableGatewayStages(t *testing.T) {
	calls := openAIWebSocketExecutableStageCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket")
	require.Positive(t, openAIWebSocketBillingStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute billing through runOpenAIWebSocketStage")
	require.NotContains(t, calls, "moderationcoverage.StageBilling",
		"ResponsesWebSocket must not wrap billing with runOpenAIWebSocketExecutableStage")
	require.Positive(t, openAIWebSocketRoutingStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute routing through runOpenAIWebSocketStage")
	require.NotContains(t, calls, "moderationcoverage.StageRouting",
		"ResponsesWebSocket must not wrap routing with runOpenAIWebSocketExecutableStage")
	require.Positive(t, openAIWebSocketForwardStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute forward through runOpenAIWebSocketStage")
	require.NotContains(t, calls, "moderationcoverage.StageForward",
		"ResponsesWebSocket must not wrap forward with runOpenAIWebSocketExecutableStage")
	require.Positive(t, openAIWebSocketUsageStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute usage through runOpenAIWebSocketStage")
	require.NotContains(t, calls, "moderationcoverage.StageUsage",
		"ResponsesWebSocket must not use an empty runOpenAIWebSocketExecutableStage wrapper for usage")
}

func TestOpenAIResponsesWebSocketUsesNamedFramePipelineAdapter(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	var directFramePipelineLines []int
	hasNamedInitialAdapter := false
	hasNamedFollowupAdapter := false
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
		case "runOpenAIWebSocketInitialFramePipeline", "runOpenAIWebSocketFollowupFramePipeline":
			directFramePipelineLines = append(directFramePipelineLines, fset.Position(call.Pos()).Line)
		case "runOpenAIWebSocketFramePipeline":
			if len(call.Args) < 2 {
				return true
			}
			adapterName := strings.TrimSpace(nodeString(t, fset, call.Args[1]))
			if lit, ok := call.Args[1].(*ast.CompositeLit); ok {
				adapterName = compositeTypeName(lit.Type)
			}
			switch adapterName {
			case "OpenAIWebSocketInitialFramePipelineAdapter":
				hasNamedInitialAdapter = true
			case "OpenAIWebSocketFollowupFramePipelineAdapter":
				hasNamedFollowupAdapter = true
			}
		}
		return true
	})

	require.Empty(t, directFramePipelineLines, "ResponsesWebSocket must not call low-level WebSocket frame pipeline helpers directly at lines %v", directFramePipelineLines)
	require.True(t, hasNamedInitialAdapter, "ResponsesWebSocket must run initial frame checks through OpenAIWebSocketInitialFramePipelineAdapter")
	require.True(t, hasNamedFollowupAdapter, "ResponsesWebSocket must run follow-up frame checks through OpenAIWebSocketFollowupFramePipelineAdapter")
}

func TestOpenAIResponsesWebSocketUsesUnifiedStageRunner(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	var directStageRunnerLines []int
	unifiedStageRunnerCalls := 0
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
		case "runOpenAIWebSocketBillingStage",
			"runOpenAIWebSocketRoutingStage",
			"runOpenAIWebSocketForwardStage",
			"runOpenAIWebSocketUsageStage":
			directStageRunnerLines = append(directStageRunnerLines, fset.Position(call.Pos()).Line)
		case "runOpenAIWebSocketStage":
			unifiedStageRunnerCalls++
		}
		return true
	})

	require.Empty(t, directStageRunnerLines, "ResponsesWebSocket must not call low-level WebSocket stage runners directly at lines %v", directStageRunnerLines)
	require.Positive(t, unifiedStageRunnerCalls, "ResponsesWebSocket must run WebSocket executable stages through runOpenAIWebSocketStage")
}

func TestOpenAIResponsesWebSocketUsesNamedBillingStageAdapter(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	hasNamedAdapter := false
	var anonymousBillingStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "OpenAIWebSocketBillingStage":
			hasNamedAdapter = true
		case "BillingStageAdapter":
			anonymousBillingStageAdapterLines = append(anonymousBillingStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	require.True(t, hasNamedAdapter, "ResponsesWebSocket must pass OpenAIWebSocketBillingStage to runOpenAIWebSocketBillingStage")
	require.Empty(t, anonymousBillingStageAdapterLines, "ResponsesWebSocket must not wrap billing with anonymous BillingStageAdapter at lines %v", anonymousBillingStageAdapterLines)
}

func TestOpenAIResponsesWebSocketUsesNamedRoutingStageAdapter(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	hasNamedAdapter := false
	var anonymousRoutingStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "OpenAIWebSocketRoutingStage":
			hasNamedAdapter = true
		case "RoutingStageAdapter":
			anonymousRoutingStageAdapterLines = append(anonymousRoutingStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	require.True(t, hasNamedAdapter, "ResponsesWebSocket must pass OpenAIWebSocketRoutingStage to runOpenAIWebSocketRoutingStage")
	require.Empty(t, anonymousRoutingStageAdapterLines, "ResponsesWebSocket must not wrap routing with anonymous RoutingStageAdapter at lines %v", anonymousRoutingStageAdapterLines)
}

func TestOpenAIResponsesWebSocketDoesNotPassRoutingClosureToRoutingStage(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	var routingClosureLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok || compositeTypeName(lit.Type) != "OpenAIWebSocketRoutingStage" {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if ok && key.Name == "Routing" {
				routingClosureLines = append(routingClosureLines, fset.Position(kv.Pos()).Line)
			}
		}
		return true
	})

	require.Empty(t, routingClosureLines, "ResponsesWebSocket must not pass Routing closures to OpenAIWebSocketRoutingStage at lines %v", routingClosureLines)
}

func TestOpenAIResponsesWebSocketUsesNamedForwardStageAdapter(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	hasNamedAdapter := false
	var anonymousForwardStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "OpenAIWebSocketForwardStage":
			hasNamedAdapter = true
		case "ForwardStageAdapter":
			anonymousForwardStageAdapterLines = append(anonymousForwardStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	require.True(t, hasNamedAdapter, "ResponsesWebSocket must pass OpenAIWebSocketForwardStage to runOpenAIWebSocketForwardStage")
	require.Empty(t, anonymousForwardStageAdapterLines, "ResponsesWebSocket must not wrap forwarding with anonymous ForwardStageAdapter at lines %v", anonymousForwardStageAdapterLines)
}

func TestOpenAIResponsesWebSocketUsesNamedUsageStageAdapter(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	hasNamedAdapter := false
	var anonymousUsageStageAdapterLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch compositeTypeName(lit.Type) {
		case "OpenAIWebSocketUsageStage":
			hasNamedAdapter = true
		case "UsageStageAdapter":
			anonymousUsageStageAdapterLines = append(anonymousUsageStageAdapterLines, fset.Position(lit.Pos()).Line)
		}
		return true
	})

	require.True(t, hasNamedAdapter, "ResponsesWebSocket must pass OpenAIWebSocketUsageStage to runOpenAIWebSocketUsageStage")
	require.Empty(t, anonymousUsageStageAdapterLines, "ResponsesWebSocket must not wrap usage with anonymous UsageStageAdapter at lines %v", anonymousUsageStageAdapterLines)
}

func TestOpenAIResponsesWebSocketScheduleResultsStayInsideUsageStage(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_handler.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_handler.go", src, 0)
	require.NoError(t, err)

	fn := openAIWebSocketHandlerFuncDecl(t, file, "ResponsesWebSocket")
	var directScheduleResultLines []int
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "ReportOpenAIAccountScheduleResult" {
			directScheduleResultLines = append(directScheduleResultLines, fset.Position(call.Pos()).Line)
		}
		return true
	})

	require.Empty(t, directScheduleResultLines,
		"ResponsesWebSocket must report schedule results through OpenAIWebSocketUsageStage, direct calls at lines %v", directScheduleResultLines)
}

func openAIWebSocketExecutableStageCalls(t *testing.T, fileName string, handlerName string) map[string]int {
	t.Helper()

	src, err := os.ReadFile(fileName)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, src, 0)
	require.NoError(t, err)

	calls := make(map[string]int)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != handlerName {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "runOpenAIWebSocketExecutableStage" {
				return true
			}
			if len(call.Args) < 2 {
				return true
			}
			calls[strings.TrimSpace(nodeString(t, fset, call.Args[1]))]++
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return nil
}

func openAIWebSocketForwardStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
	return openAIWebSocketUnifiedStageAdapterCalls(t, fileName, handlerName, "OpenAIWebSocketForwardStage")
}

func openAIWebSocketBillingStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
	return openAIWebSocketUnifiedStageAdapterCalls(t, fileName, handlerName, "OpenAIWebSocketBillingStage")
}

func openAIWebSocketRoutingStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
	return openAIWebSocketUnifiedStageAdapterCalls(t, fileName, handlerName, "OpenAIWebSocketRoutingStage")
}

func openAIWebSocketUsageStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
	return openAIWebSocketUnifiedStageAdapterCalls(t, fileName, handlerName, "OpenAIWebSocketUsageStage")
}

func openAIWebSocketUnifiedStageAdapterCalls(t *testing.T, fileName string, handlerName string, adapterType string) int {
	t.Helper()

	src, err := os.ReadFile(fileName)
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, fileName, src, 0)
	require.NoError(t, err)

	calls := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != handlerName {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "runOpenAIWebSocketStage" || len(call.Args) < 2 {
				return true
			}
			if lit, ok := call.Args[1].(*ast.CompositeLit); ok && compositeTypeName(lit.Type) == adapterType {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}

func openAIWebSocketHandlerFuncDecl(t *testing.T, file *ast.File, handlerName string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == handlerName {
			return fn
		}
	}
	t.Fatalf("handler %s not found", handlerName)
	return nil
}
