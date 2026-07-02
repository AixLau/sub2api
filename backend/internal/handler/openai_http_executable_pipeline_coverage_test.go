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
			stages:  []string{},
		},
		{
			file:    "openai_gateway_handler.go",
			handler: "Responses",
			stages:  []string{},
		},
		{
			file:    "openai_images.go",
			handler: "Images",
			stages:  []string{},
		},
		{
			file:    "openai_embeddings.go",
			handler: "Embeddings",
			stages:  []string{},
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

func TestOpenAIHTTPHandlersUseRoutingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			require.Positive(t, openAIHTTPRoutingStageAdapterCalls(t, tt.file, tt.handler),
				"%s.%s must execute routing through runOpenAIHTTPRoutingStage", tt.file, tt.handler)
			calls := openAIHTTPExecutableStageCalls(t, tt.file, tt.handler)
			require.NotContains(t, calls, "moderationcoverage.StageRouting",
				"%s.%s must not wrap routing with runOpenAIHTTPExecutableStage", tt.file, tt.handler)
		})
	}
}

func TestOpenAIHTTPHandlersUseBillingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			require.Positive(t, openAIHTTPBillingStageAdapterCalls(t, tt.file, tt.handler),
				"%s.%s must execute billing through runOpenAIHTTPBillingStage", tt.file, tt.handler)
			calls := openAIHTTPExecutableStageCalls(t, tt.file, tt.handler)
			require.NotContains(t, calls, "moderationcoverage.StageBilling",
				"%s.%s must not wrap billing with runOpenAIHTTPExecutableStage", tt.file, tt.handler)
		})
	}
}

func TestOpenAIHTTPHandlersUseNamedRoutingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			hasNamedAdapter := false
			var anonymousRoutingStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "OpenAIHTTPRoutingStage":
					hasNamedAdapter = true
				case "RoutingStageAdapter":
					anonymousRoutingStageAdapterLines = append(anonymousRoutingStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			require.True(t, hasNamedAdapter, "%s.%s must pass OpenAIHTTPRoutingStage to runOpenAIHTTPRoutingStage", tt.file, tt.handler)
			require.Empty(t, anonymousRoutingStageAdapterLines, "%s.%s must not wrap routing with anonymous RoutingStageAdapter at lines %v", tt.file, tt.handler, anonymousRoutingStageAdapterLines)
		})
	}
}

func TestOpenAIHTTPHandlersDoNotPassRoutingClosureToRoutingStage(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			var routingClosureLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok || compositeTypeName(lit.Type) != "OpenAIHTTPRoutingStage" {
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

			require.Empty(t, routingClosureLines, "%s.%s must not pass Routing closures to OpenAIHTTPRoutingStage at lines %v", tt.file, tt.handler, routingClosureLines)
		})
	}
}

func TestOpenAIHTTPHandlersUseNamedBillingStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			hasNamedAdapter := false
			var anonymousBillingStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "OpenAIHTTPBillingStage":
					hasNamedAdapter = true
				case "BillingStageAdapter":
					anonymousBillingStageAdapterLines = append(anonymousBillingStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			require.True(t, hasNamedAdapter, "%s.%s must pass OpenAIHTTPBillingStage to runOpenAIHTTPBillingStage", tt.file, tt.handler)
			require.Empty(t, anonymousBillingStageAdapterLines, "%s.%s must not wrap billing with anonymous BillingStageAdapter at lines %v", tt.file, tt.handler, anonymousBillingStageAdapterLines)
		})
	}
}

func TestOpenAIHTTPHandlersUseUsageStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			require.Positive(t, openAIHTTPUsageStageAdapterCalls(t, tt.file, tt.handler),
				"%s.%s must execute usage through runOpenAIHTTPUsageStage", tt.file, tt.handler)
			calls := openAIHTTPExecutableStageCalls(t, tt.file, tt.handler)
			require.NotContains(t, calls, "moderationcoverage.StageUsage",
				"%s.%s must not wrap usage with runOpenAIHTTPExecutableStage", tt.file, tt.handler)
		})
	}
}

func TestOpenAIHTTPHandlersUseNamedUsageStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			hasNamedAdapter := false
			var anonymousUsageStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "OpenAIHTTPUsageStage":
					hasNamedAdapter = true
				case "UsageStageAdapter":
					anonymousUsageStageAdapterLines = append(anonymousUsageStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			require.True(t, hasNamedAdapter, "%s.%s must pass OpenAIHTTPUsageStage to runOpenAIHTTPUsageStage", tt.file, tt.handler)
			require.Empty(t, anonymousUsageStageAdapterLines, "%s.%s must not wrap usage with anonymous UsageStageAdapter at lines %v", tt.file, tt.handler, anonymousUsageStageAdapterLines)
		})
	}
}

func TestOpenAIHTTPHandlersScheduleResultsStayInsideUsageStage(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
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
				"%s.%s must report schedule results through OpenAIHTTPUsageStage, direct calls at lines %v", tt.file, tt.handler, directScheduleResultLines)
		})
	}
}

func TestOpenAIHTTPHandlersCyberPolicyRecordsStayInsideUsageStage(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			var directCyberLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "recordCyberPolicyIfMarked" {
					directCyberLines = append(directCyberLines, fset.Position(call.Pos()).Line)
				}
				return true
			})

			require.Empty(t, directCyberLines,
				"%s.%s must record cyber policy through OpenAIHTTPUsageStage, direct calls at lines %v", tt.file, tt.handler, directCyberLines)
		})
	}
}

func TestOpenAIHTTPHandlersUseForwardStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			require.Positive(t, openAIHTTPForwardStageAdapterCalls(t, tt.file, tt.handler),
				"%s.%s must execute forward through runOpenAIHTTPForwardStage", tt.file, tt.handler)
			calls := openAIHTTPExecutableStageCalls(t, tt.file, tt.handler)
			require.NotContains(t, calls, "moderationcoverage.StageForward",
				"%s.%s must not wrap forward with runOpenAIHTTPExecutableStage", tt.file, tt.handler)
		})
	}
}

func TestOpenAIHTTPHandlersUseNamedForwardStageAdapter(t *testing.T) {
	tests := []struct {
		file    string
		handler string
	}{
		{file: "openai_chat_completions.go", handler: "ChatCompletions"},
		{file: "openai_gateway_handler.go", handler: "Responses"},
		{file: "openai_gateway_handler.go", handler: "Messages"},
		{file: "openai_images.go", handler: "Images"},
		{file: "openai_embeddings.go", handler: "Embeddings"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			fn := openAIHTTPHandlerFuncDecl(t, file, tt.handler)
			hasNamedAdapter := false
			var anonymousForwardStageAdapterLines []int
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				switch compositeTypeName(lit.Type) {
				case "OpenAIHTTPForwardStage":
					hasNamedAdapter = true
				case "ForwardStageAdapter":
					anonymousForwardStageAdapterLines = append(anonymousForwardStageAdapterLines, fset.Position(lit.Pos()).Line)
				}
				return true
			})

			require.True(t, hasNamedAdapter, "%s.%s must pass OpenAIHTTPForwardStage to runOpenAIHTTPForwardStage", tt.file, tt.handler)
			require.Empty(t, anonymousForwardStageAdapterLines, "%s.%s must not wrap forwarding with anonymous ForwardStageAdapter at lines %v", tt.file, tt.handler, anonymousForwardStageAdapterLines)
		})
	}
}

func TestOpenAIHTTPForwardStageRunnerUsesRouteDescriptorResolver(t *testing.T) {
	src, err := os.ReadFile("openai_gateway_executable_pipeline.go")
	require.NoError(t, err)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "openai_gateway_executable_pipeline.go", src, 0)
	require.NoError(t, err)

	fn := openAIHTTPHandlerFuncDecl(t, file, "runOpenAIHTTPForwardStage")
	callsDescriptorResolver := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "openAIHTTPForwardStageFromRouteDescriptor" {
			callsDescriptorResolver = true
		}
		return true
	})
	require.True(t, callsDescriptorResolver, "runOpenAIHTTPForwardStage must resolve forward adapter from route descriptor metadata before execution")
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

func openAIHTTPForwardStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
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
			if ok && selector.Sel.Name == "runOpenAIHTTPForwardStage" {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}

func openAIHTTPBillingStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
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
			if ok && selector.Sel.Name == "runOpenAIHTTPBillingStage" {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}

func openAIHTTPRoutingStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
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
			if ok && selector.Sel.Name == "runOpenAIHTTPRoutingStage" {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}

func openAIHTTPHandlerFuncDecl(t *testing.T, file *ast.File, handlerName string) *ast.FuncDecl {
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

func openAIHTTPUsageStageAdapterCalls(t *testing.T, fileName string, handlerName string) int {
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
			if ok && selector.Sel.Name == "runOpenAIHTTPUsageStage" {
				calls++
			}
			return true
		})
		return calls
	}

	t.Fatalf("%s does not define handler %s", fileName, handlerName)
	return 0
}
