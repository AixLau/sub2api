package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
