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
	for _, stage := range []string{
		"moderationcoverage.StageBilling",
		"moderationcoverage.StageRouting",
	} {
		require.Contains(t, calls, stage, "ResponsesWebSocket must execute %s through runOpenAIWebSocketExecutableStage", stage)
	}
	require.Positive(t, openAIWebSocketForwardStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute forward through runOpenAIWebSocketForwardStage")
	require.NotContains(t, calls, "moderationcoverage.StageForward",
		"ResponsesWebSocket must not wrap forward with runOpenAIWebSocketExecutableStage")
	require.Positive(t, openAIWebSocketUsageStageAdapterCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket"),
		"ResponsesWebSocket must execute usage through runOpenAIWebSocketUsageStage")
	require.NotContains(t, calls, "moderationcoverage.StageUsage",
		"ResponsesWebSocket must not use an empty runOpenAIWebSocketExecutableStage wrapper for usage")
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
			if ok && selector.Sel.Name == "runOpenAIWebSocketForwardStage" {
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

func openAIWebSocketUsageStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
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
			if ok && selector.Sel.Name == "runOpenAIWebSocketUsageStage" {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}
