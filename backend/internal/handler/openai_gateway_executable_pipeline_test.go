package handler

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPExecutableStagesRunInOrderAndRecordExecutions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	var calls []string
	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPExecutableStages(c, []openAIHTTPExecutableStage{
		{Stage: moderationcoverage.StageBilling, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageBilling)
			return openAIHTTPExecutableStageResult{}
		}},
		{Stage: moderationcoverage.StageRouting, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageRouting)
			return openAIHTTPExecutableStageResult{}
		}},
		{Stage: moderationcoverage.StageForward, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageForward)
			return openAIHTTPExecutableStageResult{}
		}},
		{Stage: moderationcoverage.StageUsage, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageUsage)
			return openAIHTTPExecutableStageResult{}
		}},
	})

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []string{
		moderationcoverage.StageBilling,
		moderationcoverage.StageRouting,
		moderationcoverage.StageForward,
		moderationcoverage.StageUsage,
	}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageRouting, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageUsage, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIHTTPExecutableStagesStopBeforeLaterStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var calls []string
	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPExecutableStages(c, []openAIHTTPExecutableStage{
		{Stage: moderationcoverage.StageBilling, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageBilling)
			return openAIHTTPExecutableStageResult{Stop: true}
		}},
		{Stage: moderationcoverage.StageRouting, Run: func() openAIHTTPExecutableStageResult {
			calls = append(calls, moderationcoverage.StageRouting)
			return openAIHTTPExecutableStageResult{}
		}},
	})

	require.True(t, result.Stop)
	require.Equal(t, []string{moderationcoverage.StageBilling}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIHTTPExecutableStagePreservesError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := moderationcoverage.ResetPipelineExecutionObserverForTest()
	defer restoreObserver()
	expectedErr := errors.New("billing failed")

	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPExecutableStage(c, moderationcoverage.StageBilling, func() openAIHTTPExecutableStageResult {
		return openAIHTTPExecutableStageResult{Stop: true, Err: expectedErr}
	})

	require.True(t, result.Stop)
	require.ErrorIs(t, result.Err, expectedErr)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage, Error: true},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
	snapshot := moderationcoverage.PipelineExecutionObserverSnapshot()
	require.Equal(t, int64(2), snapshot.ErrorCount)
	require.Len(t, snapshot.Executions, 2)
	requirePipelineExecutionObservedWithError(t, snapshot.Executions, moderationcoverage.PipelineOpenAIHTTP, moderationcoverage.StageBilling, moderationcoverage.SourceOpenAIHTTPExecutableStage)
	requirePipelineExecutionObservedWithError(t, snapshot.Executions, moderationcoverage.PipelineGatewayGlobal, moderationcoverage.StageBilling, moderationcoverage.SourceOpenAIHTTPExecutableStage)
}

func TestOpenAIHTTPExecutableStageNilContextIsSafe(t *testing.T) {
	calls := 0

	require.NotPanics(t, func() {
		result := (&OpenAIGatewayHandler{}).runOpenAIHTTPExecutableStage(nil, moderationcoverage.StageUsage, func() openAIHTTPExecutableStageResult {
			calls++
			return openAIHTTPExecutableStageResult{}
		})

		require.False(t, result.Stop)
		require.NoError(t, result.Err)
	})
	require.Equal(t, 1, calls)
	require.Empty(t, moderationcoverage.PipelineStageExecutionsFromContext(nil))
}

func TestProtocolStageRunnersDelegateToGlobalStageRunner(t *testing.T) {
	tests := []struct {
		file      string
		functions []string
	}{
		{
			file: "openai_gateway_executable_pipeline.go",
			functions: []string{
				"runOpenAIHTTPBillingStage",
				"runOpenAIHTTPRoutingStage",
				"runOpenAIHTTPForwardStage",
				"runOpenAIHTTPUsageStage",
				"runOpenAIWebSocketStage",
			},
		},
		{
			file: "gateway_pre_forward_pipeline.go",
			functions: []string{
				"runGatewayBillingStage",
				"runGatewayRoutingStage",
				"runGatewayForwardStage",
				"runGatewayUsageStage",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			for _, functionName := range tt.functions {
				fn := handlerFuncDeclByName(t, parsed, functionName)
				var gatewayPipelineLiteralLines []int
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					lit, ok := node.(*ast.CompositeLit)
					if !ok || handlerASTLastIdentName(lit.Type) != "GatewayPipeline" {
						return true
					}
					gatewayPipelineLiteralLines = append(gatewayPipelineLiteralLines, fset.Position(lit.Pos()).Line)
					return true
				})
				require.Empty(t, gatewayPipelineLiteralLines,
					"%s must delegate stage execution to the global GatewayPipeline stage runner instead of constructing GatewayPipeline directly at lines %v",
					functionName,
					gatewayPipelineLiteralLines,
				)
			}
		})
	}
}

func TestGatewayPipelineConstructionIsCentralized(t *testing.T) {
	tests := []struct {
		file             string
		allowedFunctions map[string]struct{}
	}{
		{
			file: "openai_gateway_executable_pipeline.go",
			allowedFunctions: map[string]struct{}{
				"newGatewayPipeline": {},
			},
		},
		{
			file:             "gateway_pre_forward_pipeline.go",
			allowedFunctions: map[string]struct{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src, err := os.ReadFile(tt.file)
			require.NoError(t, err)
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, tt.file, src, 0)
			require.NoError(t, err)

			var directConstructionLines []int
			for _, decl := range parsed.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				if _, allowed := tt.allowedFunctions[fn.Name.Name]; allowed {
					continue
				}
				ast.Inspect(fn.Body, func(node ast.Node) bool {
					lit, ok := node.(*ast.CompositeLit)
					if !ok || handlerASTLastIdentName(lit.Type) != "GatewayPipeline" {
						return true
					}
					directConstructionLines = append(directConstructionLines, fset.Position(lit.Pos()).Line)
					return true
				})
			}

			require.Empty(t, directConstructionLines,
				"%s must construct GatewayPipeline only through newGatewayPipeline, found direct literals at lines %v",
				tt.file,
				directConstructionLines,
			)
		})
	}
}

func TestGatewayPipelineRunsGenericExecutableStagesWithMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var calls []string
	pipeline := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			{Name: moderationcoverage.StageBilling, Run: func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageBilling)
				return ExecutableStageResult{}
			}},
			{Name: moderationcoverage.StageRouting, Run: func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageRouting)
				return ExecutableStageResult{}
			}},
			ForwardStageFunc(moderationcoverage.StageForward, func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageForward)
				return ExecutableStageResult{}
			}),
			{Name: moderationcoverage.StageUsage, Run: func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageUsage)
				return ExecutableStageResult{}
			}},
		},
	}

	result := pipeline.Run(c)

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []string{
		moderationcoverage.StageBilling,
		moderationcoverage.StageRouting,
		moderationcoverage.StageForward,
		moderationcoverage.StageUsage,
	}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageRouting, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageUsage, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIWebSocketStageRunnerRunsAllAdapterTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var calls []string
	handler := &OpenAIGatewayHandler{}

	result := handler.runOpenAIWebSocketStage(c, BillingStageAdapter{
		Name: moderationcoverage.StageBilling,
		Billing: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageBilling)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, RoutingStageAdapter{
		Name: moderationcoverage.StageRouting,
		Routing: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageRouting)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, ForwardStageAdapter{
		Name: moderationcoverage.StageForward,
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageForward)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, UsageStageAdapter{
		Name: moderationcoverage.StageUsage,
		Usage: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageUsage)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	require.Equal(t, []string{
		moderationcoverage.StageBilling,
		moderationcoverage.StageRouting,
		moderationcoverage.StageForward,
		moderationcoverage.StageUsage,
	}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageRouting, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageUsage, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayPipelineRunnerRunsPipelineAndRecordsGlobalExecution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := moderationcoverage.ResetPipelineExecutionObserverForTest()
	defer restoreObserver()

	calls := 0
	pipeline := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			{Name: moderationcoverage.StageForward, Run: func() ExecutableStageResult {
				calls++
				return ExecutableStageResult{}
			}},
		},
	}

	result := GatewayPipelineRunner{}.Run(c, pipeline)

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, 1, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))

	snapshot := moderationcoverage.PipelineExecutionObserverSnapshot()
	require.Len(t, snapshot.Executions, 2)
	requirePipelineExecutionObserved(t, snapshot.Executions, moderationcoverage.PipelineGatewayGlobal, moderationcoverage.StageForward, moderationcoverage.SourceOpenAIHTTPExecutableStage)
}

func TestGatewayPipelineRunsForwardStageAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	calls := 0

	pipeline := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			ExecutableForwardStage(ForwardStageAdapter{
				Name: moderationcoverage.StageForward,
				Forward: func(ctx *gin.Context) ExecutableStageResult {
					require.Same(t, c, ctx)
					calls++
					return ExecutableStageResult{}
				},
			}),
		},
	}

	result := pipeline.Run(c)

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, 1, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayPipelineRunsUsageStageAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	calls := 0

	pipeline := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			ExecutableUsageStage(UsageStageAdapter{
				Usage: func(ctx *gin.Context) ExecutableStageResult {
					require.Same(t, c, ctx)
					calls++
					return ExecutableStageResult{}
				},
			}),
		},
	}

	result := pipeline.Run(c)

	require.NoError(t, result.Err)
	require.False(t, result.Stop)
	require.Equal(t, 1, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageUsage, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestGatewayPipelineStopBlocksLaterGenericExecutableStages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	var calls []string
	pipeline := GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			{Name: moderationcoverage.StageBilling, Run: func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageBilling)
				return ExecutableStageResult{Stop: true}
			}},
			{Name: moderationcoverage.StageRouting, Run: func() ExecutableStageResult {
				calls = append(calls, moderationcoverage.StageRouting)
				return ExecutableStageResult{}
			}},
		},
	}

	result := pipeline.Run(c)

	require.True(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []string{moderationcoverage.StageBilling}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIHTTPExecutableStageAdapterUsesGatewayPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	openAIStage := openAIHTTPExecutableStage{
		Stage: moderationcoverage.StageForward,
		Run: func() openAIHTTPExecutableStageResult {
			return openAIHTTPExecutableStageResult{}
		},
	}

	pipeline := openAIHTTPExecutablePipeline([]openAIHTTPExecutableStage{openAIStage})
	require.Equal(t, moderationcoverage.PipelineOpenAIHTTP, pipeline.Pipeline)
	require.Equal(t, moderationcoverage.SourceOpenAIHTTPExecutableStage, pipeline.Source)
	require.Len(t, pipeline.Stages, 1)
	require.Equal(t, moderationcoverage.StageForward, pipeline.Stages[0].Name)

	result := pipeline.Run(c)

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{Pipeline: moderationcoverage.PipelineOpenAIHTTP, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIHTTPExecutableStage},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func requirePipelineExecutionObserved(t *testing.T, executions []moderationcoverage.PipelineStageExecutionObservation, pipeline, stage, source string) {
	t.Helper()
	for _, execution := range executions {
		if execution.Pipeline == pipeline && execution.Stage == stage && execution.Source == source && execution.Count == 1 {
			return
		}
	}
	t.Fatalf("missing pipeline execution observation pipeline=%s stage=%s source=%s in %#v", pipeline, stage, source, executions)
}

func requirePipelineExecutionObservedWithError(t *testing.T, executions []moderationcoverage.PipelineStageExecutionObservation, pipeline, stage, source string) {
	t.Helper()
	for _, execution := range executions {
		if execution.Pipeline == pipeline && execution.Stage == stage && execution.Source == source && execution.Count == 1 && execution.ErrorCount == 1 {
			return
		}
	}
	t.Fatalf("missing failed pipeline execution observation pipeline=%s stage=%s source=%s in %#v", pipeline, stage, source, executions)
}

func handlerFuncDeclByName(t *testing.T, file *ast.File, functionName string) *ast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name != nil && fn.Name.Name == functionName {
			return fn
		}
	}
	t.Fatalf("function %s not found", functionName)
	return nil
}

func handlerASTLastIdentName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.StarExpr:
		return handlerASTLastIdentName(typed.X)
	case *ast.IndexExpr:
		return handlerASTLastIdentName(typed.X)
	case *ast.IndexListExpr:
		return handlerASTLastIdentName(typed.X)
	default:
		return ""
	}
}
