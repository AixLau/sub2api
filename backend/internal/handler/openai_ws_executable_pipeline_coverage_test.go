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
