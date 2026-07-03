package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesWebSocketUsesExecutableGatewayStages(t *testing.T) {
	calls := openAIWebSocketExecutableStageCalls(t, "openai_gateway_handler.go", "ResponsesWebSocket")

	for _, stage := range []string{
		"moderationcoverage.StageBilling",
		"moderationcoverage.StageRouting",
		"moderationcoverage.StageForward",
		"moderationcoverage.StageUsage",
	} {
		require.Contains(t, calls, stage, "ResponsesWebSocket must execute %s through runOpenAIWebSocketExecutableStage", stage)
	}
}

func TestOpenAIResponsesWebSocketCoverageIncludesExecutableStages(t *testing.T) {
	require.Equal(t, []moderationcoverage.PipelineStageCoverage{
		{Stage: moderationcoverage.StageModeration, Required: true, Covered: true},
		{Stage: moderationcoverage.StageCyber, Required: true, Covered: true},
		{Stage: moderationcoverage.StageImage, Required: true, Covered: true},
		{Stage: moderationcoverage.StageBilling, Required: true, Covered: true},
		{Stage: moderationcoverage.StageRouting, Required: true, Covered: true},
		{Stage: moderationcoverage.StageForward, Required: true, Covered: true},
		{Stage: moderationcoverage.StageUsage, Required: true, Covered: true},
	}, moderationcoverage.OpenAIWebSocketPipelineStagesForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"))
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
