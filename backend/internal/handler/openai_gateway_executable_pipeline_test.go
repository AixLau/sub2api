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

func TestExecutableForwardStageRejectsDeferredModerationReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodPost,
		Path:               "/v1/responses",
		Handler:            "OpenAIGatewayHandler.Responses",
		Upstream:           true,
		ModerationRequired: true,
		Pipeline:           moderationcoverage.PipelineOpenAIHTTP,
	})
	moderationcoverage.MarkModerationReceipt(c, moderationcoverage.ModerationExecutionReceipt{
		LocalScanDone:  true,
		Outcome:        "deferred",
		ForwardAllowed: false,
	})

	forwarded := false
	stage := ExecutableForwardStage(ForwardStageAdapter{Forward: func(*gin.Context) ExecutableStageResult {
		forwarded = true
		return ExecutableStageResult{}
	}})
	result := stage.RunWithContext(c)

	require.False(t, forwarded)
	require.True(t, result.Stop)
	require.ErrorIs(t, result.Err, errModerationReceiptNotForwardable)
}

func markForwardableModerationReceipt(c *gin.Context, protocol string) {
	moderationcoverage.MarkModerationReceipt(c, moderationcoverage.ModerationExecutionReceipt{
		Protocol:       protocol,
		LocalScanDone:  true,
		Outcome:        "no_hit",
		ForwardAllowed: true,
	})
}

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
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:             http.MethodGet,
		Path:               "/v1/responses",
		Handler:            "OpenAIGatewayHandler.ResponsesWebSocket",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           "openai_responses",
		Pipeline:           moderationcoverage.PipelineOpenAIWebSocket,
		Status:             moderationcoverage.StatusCovered,
		StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute(
			"OpenAIGatewayHandler.ResponsesWebSocket",
			"openai_responses",
		),
	})
	markForwardableModerationReceipt(c, "openai_responses")

	var calls []string
	handler := &OpenAIGatewayHandler{}

	result := handler.runOpenAIWebSocketStage(c, BillingStageAdapter{
		Name: "OpenAIWebSocketBillingStage",
		Billing: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageBilling)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, RoutingStageAdapter{
		Name: "OpenAIWebSocketRoutingStage",
		Routing: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageRouting)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, ForwardStageAdapter{
		Name: "OpenAIWebSocketForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, moderationcoverage.StageForward)
			return ExecutableStageResult{}
		},
	})
	require.False(t, result.Stop)
	require.NoError(t, result.Err)

	result = handler.runOpenAIWebSocketStage(c, UsageStageAdapter{
		Name: "OpenAIWebSocketUsageStage",
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
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageBilling, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage, Method: http.MethodGet, Path: "/v1/responses", Handler: "OpenAIGatewayHandler.ResponsesWebSocket", Protocol: "openai_responses"},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageRouting, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage, Method: http.MethodGet, Path: "/v1/responses", Handler: "OpenAIGatewayHandler.ResponsesWebSocket", Protocol: "openai_responses"},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageForward, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage, Method: http.MethodGet, Path: "/v1/responses", Handler: "OpenAIGatewayHandler.ResponsesWebSocket", Protocol: "openai_responses"},
		{Pipeline: moderationcoverage.PipelineOpenAIWebSocket, Stage: moderationcoverage.StageUsage, Source: moderationcoverage.SourceOpenAIWebSocketExecutableStage, Method: http.MethodGet, Path: "/v1/responses", Handler: "OpenAIGatewayHandler.ResponsesWebSocket", Protocol: "openai_responses"},
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

func TestForwardStageRegistryResolvesRouteAdapterDescriptor(t *testing.T) {
	registry := NewForwardStageRegistry()
	called := false
	registry.Register(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	}, ForwardStageAdapter{
		Name: "OpenAIHTTPForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			called = true
			return ExecutableStageResult{}
		},
	})

	adapter, ok := registry.Resolve(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	})
	require.True(t, ok)
	require.Equal(t, moderationcoverage.StageForward, adapter.StageName())

	result := adapter.RunForward(nil)

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.True(t, called)

	_, ok = registry.Resolve(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Name:     "OpenAIHTTPForwardStage",
	})
	require.False(t, ok)

	_, ok = registry.Resolve(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageUsage,
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	})
	require.False(t, ok)
}

func TestOpenAIHTTPForwardStageUsesRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:   http.MethodPost,
		Path:     "/v1/responses",
		Handler:  "OpenAIGatewayHandler.Responses",
		Protocol: "openai_responses",
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
	})
	calls := []string{}
	registered := ForwardStageAdapter{
		Name: "OpenAIHTTPForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "registered")
			return ExecutableStageResult{}
		},
	}
	direct := ForwardStageAdapter{
		Name: "OpenAIHTTPForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "direct")
			return ExecutableStageResult{}
		},
	}
	handler := &OpenAIGatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	handler.forwardStageRegistry.Register(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	}, registered)

	result := handler.runOpenAIHTTPForwardStage(c, direct)

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []string{"registered"}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			Stage:    moderationcoverage.StageForward,
			Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
			Method:   http.MethodPost,
			Path:     "/v1/responses",
			Handler:  "OpenAIGatewayHandler.Responses",
			Protocol: "openai_responses",
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIHTTPForwardStageReleasesSlotWhenDescriptorUsesRegisteredAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:   http.MethodPost,
		Path:     "/v1/images/generations",
		Handler:  "OpenAIGatewayHandler.Images",
		Protocol: "openai_images",
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
	})
	registry := NewStageAdapterRegistry()
	forwardCalls := 0
	registry.RegisterForward(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	}, ForwardStageAdapter{
		Name: "OpenAIHTTPForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			forwardCalls++
			return ExecutableStageResult{}
		},
	})
	handler := &OpenAIGatewayHandler{stageAdapterRegistry: registry}
	releaseCalls := 0

	result := handler.runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
		ReleaseFunc: func() {
			releaseCalls++
		},
	})

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, 1, forwardCalls)
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAIHTTPForwardStageReleasesSlotOnceForFallbackAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:   http.MethodPost,
		Path:     "/v1/images/generations",
		Handler:  "OpenAIGatewayHandler.Images",
		Protocol: "openai_images",
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
	})
	releaseCalls := 0

	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPForwardStage(c, OpenAIHTTPForwardStage{
		ReleaseFunc: func() {
			releaseCalls++
		},
	})

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, 1, releaseCalls)
}

func TestOpenAIHTTPForwardStageRequiresRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	calls := 0

	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPForwardStage(c, ForwardStageAdapter{
		Name: "OpenAIHTTPForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline route metadata is required before forward")
	require.Equal(t, 0, calls)
}

func TestOpenAIHTTPForwardStageRequiresDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:                  http.MethodPost,
		Path:                    "/v1/responses",
		Handler:                 "OpenAIGatewayHandler.Responses",
		Protocol:                "openai_responses",
		Pipeline:                moderationcoverage.PipelineOpenAIHTTP,
		StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("OpenAIGatewayHandler.Responses", "openai_responses"),
	})
	calls := 0

	result := (&OpenAIGatewayHandler{}).runOpenAIHTTPForwardStage(c, ForwardStageAdapter{
		Name: "UnregisteredForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline forward stage adapter is not bound by route descriptor")
	require.Equal(t, 0, calls)
}

func TestOpenAIHTTPBillingRoutingUsageStagesRequireRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
		run     func(*OpenAIGatewayHandler, *gin.Context, any) openAIHTTPExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "OpenAIHTTPBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPBillingStage(c, adapter.(BillingStage))
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "OpenAIHTTPRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPRoutingStage(c, adapter.(RoutingStage))
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "OpenAIHTTPUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPUsageStage(c, adapter.(UsageStage))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			calls := 0

			result := tt.run(&OpenAIGatewayHandler{}, c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline route metadata is required before "+tt.stage)
			require.Equal(t, 0, calls)
		})
	}
}

func TestOpenAIHTTPBillingRoutingUsageStagesRequireDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
		run     func(*OpenAIGatewayHandler, *gin.Context, any) openAIHTTPExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "UnregisteredBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPBillingStage(c, adapter.(BillingStage))
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "UnregisteredRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPRoutingStage(c, adapter.(RoutingStage))
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "UnregisteredUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context, adapter any) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPUsageStage(c, adapter.(UsageStage))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:                  http.MethodPost,
				Path:                    "/v1/responses",
				Handler:                 "OpenAIGatewayHandler.Responses",
				Protocol:                "openai_responses",
				Pipeline:                moderationcoverage.PipelineOpenAIHTTP,
				StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("OpenAIGatewayHandler.Responses", "openai_responses"),
			})
			calls := 0

			result := tt.run(&OpenAIGatewayHandler{}, c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline "+tt.stage+" stage adapter is not bound by route descriptor")
			require.Equal(t, 0, calls)
		})
	}
}

func TestOpenAIHTTPBillingRoutingUsageStagesUseRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		stage     string
		register  func(*StageAdapterRegistry, string, *int)
		run       func(*OpenAIGatewayHandler, *gin.Context) openAIHTTPExecutableStageResult
		wantExec  moderationcoverage.PipelineStageExecution
		wantCalls int
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterBilling(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageBilling, Pipeline: pipeline, Name: "OpenAIHTTPBillingStage"}, BillingStageAdapter{
					Name: "OpenAIHTTPBillingStage",
					Billing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPBillingStage(c, BillingStageAdapter{Name: "OpenAIHTTPBillingStage"})
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterRouting(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageRouting, Pipeline: pipeline, Name: "OpenAIHTTPRoutingStage"}, RoutingStageAdapter{
					Name: "OpenAIHTTPRoutingStage",
					Routing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPRoutingStage(c, RoutingStageAdapter{Name: "OpenAIHTTPRoutingStage"})
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterUsage(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageUsage, Pipeline: pipeline, Name: "OpenAIHTTPUsageStage"}, UsageStageAdapter{
					Name: "OpenAIHTTPUsageStage",
					Usage: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) openAIHTTPExecutableStageResult {
				return h.runOpenAIHTTPUsageStage(c, UsageStageAdapter{Name: "OpenAIHTTPUsageStage"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:   http.MethodPost,
				Path:     "/v1/responses",
				Handler:  "OpenAIGatewayHandler.Responses",
				Protocol: "openai_responses",
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
			})
			calls := 0
			registry := NewStageAdapterRegistry()
			tt.register(registry, moderationcoverage.PipelineOpenAIHTTP, &calls)
			handler := &OpenAIGatewayHandler{stageAdapterRegistry: registry}

			result := tt.run(handler, c)

			require.False(t, result.Stop)
			require.NoError(t, result.Err)
			require.Equal(t, 1, calls)
			require.Equal(t, []moderationcoverage.PipelineStageExecution{{
				Pipeline: moderationcoverage.PipelineOpenAIHTTP,
				Stage:    tt.stage,
				Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
				Method:   http.MethodPost,
				Path:     "/v1/responses",
				Handler:  "OpenAIGatewayHandler.Responses",
				Protocol: "openai_responses",
			}}, moderationcoverage.PipelineStageExecutionsFromContext(c))
		})
	}
}

func TestOpenAIHTTPForwardStageDoesNotCacheRequestFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIGatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	calls := []string{}
	for _, name := range []string{"first", "second"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
			Method:   http.MethodPost,
			Path:     "/v1/responses",
			Handler:  "OpenAIGatewayHandler.Responses",
			Protocol: "openai_responses",
			Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		})
		result := handler.runOpenAIHTTPForwardStage(c, ForwardStageAdapter{
			Name: "OpenAIHTTPForwardStage",
			Forward: func(*gin.Context) ExecutableStageResult {
				calls = append(calls, name)
				return ExecutableStageResult{}
			},
		})
		require.False(t, result.Stop)
		require.NoError(t, result.Err)
	}

	require.Equal(t, []string{"first", "second"}, calls)
}

func TestOpenAIWebSocketForwardStageUsesRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:   http.MethodGet,
		Path:     "/v1/responses",
		Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
		Protocol: "openai_responses",
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
	})
	calls := []string{}
	registered := ForwardStageAdapter{
		Name: "OpenAIWebSocketForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "registered")
			return ExecutableStageResult{}
		},
	}
	direct := ForwardStageAdapter{
		Name: "OpenAIWebSocketForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls = append(calls, "direct")
			return ExecutableStageResult{}
		},
	}
	handler := &OpenAIGatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	handler.forwardStageRegistry.Register(moderationcoverage.RouteAdapterDescriptor{
		Stage:    moderationcoverage.StageForward,
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Name:     "OpenAIWebSocketForwardStage",
	}, registered)

	result := handler.runOpenAIWebSocketStage(c, direct)

	require.False(t, result.Stop)
	require.NoError(t, result.Err)
	require.Equal(t, []string{"registered"}, calls)
	require.Equal(t, []moderationcoverage.PipelineStageExecution{
		{
			Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
			Stage:    moderationcoverage.StageForward,
			Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
			Method:   http.MethodGet,
			Path:     "/v1/responses",
			Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
			Protocol: "openai_responses",
		},
	}, moderationcoverage.PipelineStageExecutionsFromContext(c))
}

func TestOpenAIWebSocketForwardStageRequiresRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	calls := 0

	result := (&OpenAIGatewayHandler{}).runOpenAIWebSocketStage(c, ForwardStageAdapter{
		Name: "OpenAIWebSocketForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline route metadata is required before forward")
	require.Equal(t, 0, calls)
}

func TestOpenAIWebSocketForwardStageRequiresDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
		Method:                  http.MethodGet,
		Path:                    "/v1/responses",
		Handler:                 "OpenAIGatewayHandler.ResponsesWebSocket",
		Protocol:                "openai_responses",
		Pipeline:                moderationcoverage.PipelineOpenAIWebSocket,
		StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"),
	})
	calls := 0

	result := (&OpenAIGatewayHandler{}).runOpenAIWebSocketStage(c, ForwardStageAdapter{
		Name: "UnregisteredForwardStage",
		Forward: func(*gin.Context) ExecutableStageResult {
			calls++
			return ExecutableStageResult{}
		},
	})

	require.True(t, result.Stop)
	require.ErrorContains(t, result.Err, "pipeline forward stage adapter is not bound by route descriptor")
	require.Equal(t, 0, calls)
}

func TestOpenAIWebSocketBillingRoutingUsageStagesRequireRegistrarRouteMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "OpenAIWebSocketBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "OpenAIWebSocketRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "OpenAIWebSocketUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			calls := 0

			result := (&OpenAIGatewayHandler{}).runOpenAIWebSocketStage(c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline route metadata is required before "+tt.stage)
			require.Equal(t, 0, calls)
		})
	}
}

func TestOpenAIWebSocketBillingRoutingUsageStagesRequireDescriptorBoundAdapter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name    string
		stage   string
		adapter func(*int) any
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			adapter: func(calls *int) any {
				return BillingStageAdapter{Name: "UnregisteredBillingStage", Billing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			adapter: func(calls *int) any {
				return RoutingStageAdapter{Name: "UnregisteredRoutingStage", Routing: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			adapter: func(calls *int) any {
				return UsageStageAdapter{Name: "UnregisteredUsageStage", Usage: func(*gin.Context) ExecutableStageResult {
					*calls++
					return ExecutableStageResult{}
				}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:                  http.MethodGet,
				Path:                    "/v1/responses",
				Handler:                 "OpenAIGatewayHandler.ResponsesWebSocket",
				Protocol:                "openai_responses",
				Pipeline:                moderationcoverage.PipelineOpenAIWebSocket,
				StageAdapterDescriptors: moderationcoverage.StageAdapterDescriptorsForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"),
			})
			calls := 0

			result := (&OpenAIGatewayHandler{}).runOpenAIWebSocketStage(c, tt.adapter(&calls))

			require.True(t, result.Stop)
			require.ErrorContains(t, result.Err, "pipeline "+tt.stage+" stage adapter is not bound by route descriptor")
			require.Equal(t, 0, calls)
		})
	}
}

func TestOpenAIWebSocketBillingRoutingUsageStagesUseRouteDescriptorRegistry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		stage    string
		register func(*StageAdapterRegistry, string, *int)
		run      func(*OpenAIGatewayHandler, *gin.Context) ExecutableStageResult
	}{
		{
			name:  "billing",
			stage: moderationcoverage.StageBilling,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterBilling(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageBilling, Pipeline: pipeline, Name: "OpenAIWebSocketBillingStage"}, BillingStageAdapter{
					Name: "OpenAIWebSocketBillingStage",
					Billing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runOpenAIWebSocketStage(c, BillingStageAdapter{Name: "OpenAIWebSocketBillingStage"})
			},
		},
		{
			name:  "routing",
			stage: moderationcoverage.StageRouting,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterRouting(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageRouting, Pipeline: pipeline, Name: "OpenAIWebSocketRoutingStage"}, RoutingStageAdapter{
					Name: "OpenAIWebSocketRoutingStage",
					Routing: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runOpenAIWebSocketStage(c, RoutingStageAdapter{Name: "OpenAIWebSocketRoutingStage"})
			},
		},
		{
			name:  "usage",
			stage: moderationcoverage.StageUsage,
			register: func(registry *StageAdapterRegistry, pipeline string, calls *int) {
				registry.RegisterUsage(moderationcoverage.RouteAdapterDescriptor{Stage: moderationcoverage.StageUsage, Pipeline: pipeline, Name: "OpenAIWebSocketUsageStage"}, UsageStageAdapter{
					Name: "OpenAIWebSocketUsageStage",
					Usage: func(*gin.Context) ExecutableStageResult {
						*calls++
						return ExecutableStageResult{}
					},
				})
			},
			run: func(h *OpenAIGatewayHandler, c *gin.Context) ExecutableStageResult {
				return h.runOpenAIWebSocketStage(c, UsageStageAdapter{Name: "OpenAIWebSocketUsageStage"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
				Method:   http.MethodGet,
				Path:     "/v1/responses",
				Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
				Protocol: "openai_responses",
				Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
			})
			calls := 0
			registry := NewStageAdapterRegistry()
			tt.register(registry, moderationcoverage.PipelineOpenAIWebSocket, &calls)
			handler := &OpenAIGatewayHandler{stageAdapterRegistry: registry}

			result := tt.run(handler, c)

			require.False(t, result.Stop)
			require.NoError(t, result.Err)
			require.Equal(t, 1, calls)
			require.Equal(t, []moderationcoverage.PipelineStageExecution{{
				Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
				Stage:    tt.stage,
				Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
				Method:   http.MethodGet,
				Path:     "/v1/responses",
				Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
				Protocol: "openai_responses",
			}}, moderationcoverage.PipelineStageExecutionsFromContext(c))
		})
	}
}

func TestOpenAIWebSocketForwardStageDoesNotCacheRequestFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIGatewayHandler{
		forwardStageRegistry: NewForwardStageRegistry(),
	}
	calls := []string{}
	for _, name := range []string{"first", "second"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		moderationcoverage.SetRouteMeta(c, moderationcoverage.Entry{
			Method:   http.MethodGet,
			Path:     "/v1/responses",
			Handler:  "OpenAIGatewayHandler.ResponsesWebSocket",
			Protocol: "openai_responses",
			Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		})
		result := handler.runOpenAIWebSocketStage(c, ForwardStageAdapter{
			Name: "OpenAIWebSocketForwardStage",
			Forward: func(*gin.Context) ExecutableStageResult {
				calls = append(calls, name)
				return ExecutableStageResult{}
			},
		})
		require.False(t, result.Stop)
		require.NoError(t, result.Err)
	}

	require.Equal(t, []string{"first", "second"}, calls)
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
