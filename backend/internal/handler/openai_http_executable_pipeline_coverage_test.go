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

func TestOpenAIHTTPHandlersUseExecutableGatewayStages(t *testing.T) {
	tests := []struct {
		file    string
		handler string
		stages  []string
	}{
		{
			file:    "openai_chat_completions.go",
			handler: "ChatCompletions",
			stages:  []string{"moderationcoverage.StageBilling", "moderationcoverage.StageRouting", "moderationcoverage.StageForward", "moderationcoverage.StageUsage"},
		},
		{
			file:    "openai_gateway_handler.go",
			handler: "Responses",
			stages:  []string{"moderationcoverage.StageBilling", "moderationcoverage.StageRouting", "moderationcoverage.StageForward", "moderationcoverage.StageUsage"},
		},
		{
			file:    "openai_images.go",
			handler: "Images",
			stages:  []string{"moderationcoverage.StageBilling", "moderationcoverage.StageRouting", "moderationcoverage.StageForward", "moderationcoverage.StageUsage"},
		},
		{
			file:    "openai_embeddings.go",
			handler: "Embeddings",
			stages:  []string{"moderationcoverage.StageBilling", "moderationcoverage.StageRouting", "moderationcoverage.StageForward", "moderationcoverage.StageUsage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			calls := openAIHTTPExecutableStageCalls(t, tt.file, tt.handler)
			for _, stage := range tt.stages {
				require.Contains(t, calls, stage, "%s.%s must execute %s through runOpenAIHTTPExecutableStage", tt.file, tt.handler, stage)
			}
		})
	}
}

func openAIHTTPExecutableStageCalls(t *testing.T, fileName string, handlerName string) map[string]int {
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
			if !ok || selector.Sel.Name != "runOpenAIHTTPExecutableStage" {
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
