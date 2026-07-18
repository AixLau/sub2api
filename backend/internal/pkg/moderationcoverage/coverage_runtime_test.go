package moderationcoverage

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRuntimeRouteMetaHelpersRoundTripNormalizedMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	SetRouteMeta(c, Entry{
		Method:             " post ",
		Path:               " /v1/responses ",
		Handler:            " OpenAIGatewayHandler.Responses ",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           " openai_responses ",
		Pipeline:           " OPENAI_HTTP ",
		Status:             " COVERED ",
	})

	meta, ok := RouteMetaFromContext(c)
	require.True(t, ok)
	require.Equal(t, "POST", meta.Method)
	require.Equal(t, "/v1/responses", meta.Path)
	require.Equal(t, "OpenAIGatewayHandler.Responses", meta.Handler)
	require.Equal(t, "openai_responses", meta.Protocol)
	require.Equal(t, PipelineOpenAIHTTP, meta.Pipeline)
	require.Equal(t, StatusCovered, meta.Status)
}

func TestPipelineAdmissionHelpersSetBooleanFlagAndMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	MarkPipelineAdmitted(c, " OPENAI_HTTP ", " PRE_FORWARD ", " source ")

	require.True(t, PipelineAdmittedFromContext(c))
	admission, ok := PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.True(t, admission.Admitted)
	require.Equal(t, PipelineOpenAIHTTP, admission.Pipeline)
	require.Equal(t, StagePreForward, admission.Stage)
	require.Equal(t, "source", admission.Source)
	require.True(t, admission.ModerationCompleted)
	require.Equal(t, "legacy_attested", admission.ReceiptOutcome)
	require.Empty(t, PipelineStageExecutionsFromContext(c))
}

func TestPipelineAdmissionAfterModerationRejectsDeferredReceipt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	MarkModerationReceipt(c, ModerationExecutionReceipt{
		RequestID:      "req-1",
		Protocol:       "openai_responses",
		LocalScanDone:  false,
		Outcome:        "deferred_selected_account",
		ForwardAllowed: true,
	})
	MarkPipelineAdmittedAfterModeration(c, PipelineOpenAIHTTP, StagePreForward, "selected-account-preguard")

	require.False(t, PipelineAdmittedFromContext(c))
	admission, ok := PipelineAdmissionFromContext(c)
	require.True(t, ok)
	require.False(t, admission.Admitted)
	require.False(t, admission.ModerationCompleted)
	require.Equal(t, "deferred_selected_account", admission.ReceiptOutcome)
}

func TestModerationReceiptHistoryKeepsIndependentWebSocketFrames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	for _, outcome := range []string{"no_hit", "allow"} {
		MarkModerationReceipt(c, ModerationExecutionReceipt{
			Protocol:       "openai_responses",
			LocalScanDone:  true,
			Outcome:        outcome,
			ForwardAllowed: true,
		})
	}

	history := ModerationReceiptsFromContext(c)
	require.Len(t, history, 2)
	require.Equal(t, "no_hit", history[0].Outcome)
	require.Equal(t, "allow", history[1].Outcome)
	require.True(t, ModerationReceiptAllowsForward(c))
}

func TestPipelineEntrypointHelpersSetAndMatchPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	MarkPipelineEntrypointEntered(c, " OPENAI_WEBSOCKET ", " websocket entrypoint ")

	entrypoint, ok := PipelineEntrypointEnteredFromContext(c, PipelineOpenAIWebSocket)
	require.True(t, ok)
	require.True(t, entrypoint.Entered)
	require.Equal(t, PipelineOpenAIWebSocket, entrypoint.Pipeline)
	require.Equal(t, "websocket entrypoint", entrypoint.Source)
	_, ok = PipelineEntrypointEnteredFromContext(c, PipelineOpenAIHTTP)
	require.False(t, ok)
	require.False(t, PipelineAdmittedFromContext(c))
	require.Empty(t, PipelineStageExecutionsFromContext(c))
}

func TestPipelineStageExecutionHelpersCollectNormalizedDedupedExecutions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := ResetPipelineExecutionObserverForTest()
	defer restoreObserver()

	require.NotPanics(t, func() {
		MarkPipelineStageExecuted(nil, PipelineOpenAIHTTP, StageRouting, "ignored")
	})
	require.Empty(t, PipelineStageExecutionsFromContext(nil))

	MarkPipelineStageExecuted(c, " OPENAI_HTTP ", " ROUTING ", " SourceB ")
	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageRouting, "SourceB")
	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageRouting, " SourceA ")
	MarkPipelineStageExecuted(c, PipelineGatewayPreForward, StageBilling, "BillingSource")
	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageUsage, "UsageSource")

	require.False(t, PipelineAdmittedFromContext(c))
	_, ok := PipelineAdmissionFromContext(c)
	require.False(t, ok)
	require.Equal(t, []PipelineStageExecution{
		{Pipeline: PipelineGatewayPreForward, Stage: StageBilling, Source: "BillingSource"},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageRouting, Source: "SourceA"},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageRouting, Source: "SourceB"},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageUsage, Source: "UsageSource"},
	}, PipelineStageExecutionsFromContext(c))
}

func TestPipelineExecutionObserverCountsStageExecutionsAndCanResetForTest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := ResetPipelineExecutionObserverForTest()
	defer restoreObserver()

	MarkPipelineStageExecuted(c, " OPENAI_HTTP ", " ROUTING ", " SourceB ")
	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageRouting, "SourceB")
	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageRouting, " SourceA ")
	MarkPipelineStageExecuted(c, PipelineGatewayPreForward, StageBilling, "BillingSource")
	MarkPipelineStageExecuted(c, "", StageBilling, "missing-pipeline")

	snapshot := PipelineExecutionObserverSnapshot()
	require.NotNil(t, snapshot.LastObservedAt)
	require.Equal(t, int64(4), snapshot.TotalCount)
	require.Equal(t, []PipelineStageExecutionObservation{
		{Pipeline: PipelineGatewayPreForward, Stage: StageBilling, Source: "BillingSource", Count: 1, RecentCount: 1, LastObservedAt: snapshot.Executions[0].LastObservedAt},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageRouting, Source: "SourceA", Count: 1, RecentCount: 1, LastObservedAt: snapshot.Executions[1].LastObservedAt},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageRouting, Source: "SourceB", Count: 2, RecentCount: 2, LastObservedAt: snapshot.Executions[2].LastObservedAt},
	}, snapshot.Executions)

	restoreSeeded := ReplacePipelineExecutionObserverForTest([]PipelineStageExecutionObservation{
		{Pipeline: " OPENAI_HTTP ", Stage: " USAGE ", Source: " UsageSource ", Count: 7},
	})
	seeded := PipelineExecutionObserverSnapshot()
	require.Equal(t, int64(7), seeded.TotalCount)
	require.Len(t, seeded.Executions, 1)
	require.Equal(t, PipelineStageExecutionObservation{
		Pipeline:       PipelineOpenAIHTTP,
		Stage:          StageUsage,
		Source:         "UsageSource",
		Count:          7,
		RecentCount:    7,
		LastObservedAt: seeded.Executions[0].LastObservedAt,
	}, seeded.Executions[0])

	restoreSeeded()
	require.Equal(t, snapshot.TotalCount, PipelineExecutionObserverSnapshot().TotalCount)

	resetRestore := ResetPipelineExecutionObserverForTest()
	require.Empty(t, PipelineExecutionObserverSnapshot().Executions)
	resetRestore()
	require.Equal(t, snapshot.TotalCount, PipelineExecutionObserverSnapshot().TotalCount)
}

func TestPipelineExecutionObserverSplitsExecutionsByRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responsesCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	chatCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := ResetPipelineExecutionObserverForTest()
	defer restoreObserver()

	SetRouteMeta(responsesCtx, Entry{
		Method:   "POST",
		Path:     "/v1/responses",
		Handler:  "OpenAIGatewayHandler.Responses",
		Protocol: "openai_responses",
		Pipeline: PipelineOpenAIHTTP,
	})
	SetRouteMeta(chatCtx, Entry{
		Method:   "POST",
		Path:     "/v1/chat/completions",
		Handler:  "OpenAIGatewayHandler.ChatCompletions",
		Protocol: "openai_chat_completions",
		Pipeline: PipelineOpenAIHTTP,
	})

	MarkPipelineStageExecuted(responsesCtx, PipelineOpenAIHTTP, StageRouting, SourceOpenAIHTTPExecutableStage)
	MarkPipelineStageExecuted(chatCtx, PipelineOpenAIHTTP, StageRouting, SourceOpenAIHTTPExecutableStage)

	snapshot := PipelineExecutionObserverSnapshot()
	require.Equal(t, int64(2), snapshot.TotalCount)
	require.Len(t, snapshot.Executions, 2)
	require.Equal(t, "/v1/chat/completions", snapshot.Executions[0].Path)
	require.Equal(t, "/v1/responses", snapshot.Executions[1].Path)
}

func TestPipelineExecutionObserverExposesRecentWindowCounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	restoreObserver := ResetPipelineExecutionObserverForTest()
	defer restoreObserver()

	MarkPipelineStageExecuted(c, PipelineOpenAIHTTP, StageRouting, SourceOpenAIHTTPExecutableStage)
	MarkPipelineStageExecutedWithResult(c, PipelineOpenAIHTTP, StageForward, SourceOpenAIHTTPExecutableStage, true)

	snapshot := PipelineExecutionObserverSnapshot()
	require.Positive(t, snapshot.RecentWindowSeconds)
	require.Equal(t, int64(2), snapshot.RecentWindowCount)
	require.Equal(t, int64(1), snapshot.RecentWindowErrorCount)
	require.Equal(t, int64(1), snapshot.ErrorCount)
	require.Len(t, snapshot.Executions, 2)
	require.Equal(t, int64(1), snapshot.Executions[1].ErrorCount)
}

func TestPipelineExecutionObserverExcludesOldExecutionsFromRecentWindow(t *testing.T) {
	oldObservedAt := time.Now().UTC().Add(-10 * time.Minute)
	recentObservedAt := time.Now().UTC()
	restoreObserver := ReplacePipelineExecutionObserverForTest([]PipelineStageExecutionObservation{
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageRouting,
			Source:         SourceOpenAIHTTPExecutableStage,
			Count:          4,
			ErrorCount:     2,
			LastObservedAt: &oldObservedAt,
		},
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageForward,
			Source:         SourceOpenAIHTTPExecutableStage,
			Count:          3,
			ErrorCount:     1,
			LastObservedAt: &recentObservedAt,
		},
	})
	defer restoreObserver()

	snapshot := PipelineExecutionObserverSnapshot()
	require.Equal(t, int64(7), snapshot.TotalCount)
	require.Equal(t, int64(3), snapshot.ErrorCount)
	require.Equal(t, int64(3), snapshot.RecentWindowCount)
	require.Equal(t, int64(1), snapshot.RecentWindowErrorCount)
}

func TestPipelineExecutionObserverExposesRecentCountsPerExecution(t *testing.T) {
	oldObservedAt := time.Now().UTC().Add(-10 * time.Minute)
	recentObservedAt := time.Now().UTC()
	restoreObserver := ReplacePipelineExecutionObserverForTest([]PipelineStageExecutionObservation{
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageForward,
			Source:         SourceOpenAIHTTPExecutableStage,
			Method:         http.MethodPost,
			Path:           "/v1/responses",
			Handler:        "OpenAIGatewayHandler.Responses",
			Protocol:       "openai_responses",
			Count:          4,
			ErrorCount:     2,
			LastObservedAt: &oldObservedAt,
		},
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageForward,
			Source:         SourceOpenAIHTTPExecutableStage,
			Method:         http.MethodPost,
			Path:           "/v1/responses",
			Handler:        "OpenAIGatewayHandler.Responses",
			Protocol:       "openai_responses",
			Count:          3,
			ErrorCount:     1,
			LastObservedAt: &recentObservedAt,
		},
	})
	defer restoreObserver()

	snapshot := PipelineExecutionObserverSnapshot()
	require.Len(t, snapshot.Executions, 1)
	require.Equal(t, int64(7), snapshot.Executions[0].Count)
	require.Equal(t, int64(3), snapshot.Executions[0].ErrorCount)
	require.Equal(t, int64(3), snapshot.Executions[0].RecentCount)
	require.Equal(t, int64(1), snapshot.Executions[0].RecentErrorCount)
}

func TestPipelineExecutionObserverExposesRouteSummaries(t *testing.T) {
	oldObservedAt := time.Now().UTC().Add(-10 * time.Minute)
	recentObservedAt := time.Now().UTC()
	restoreObserver := ReplacePipelineExecutionObserverForTest([]PipelineStageExecutionObservation{
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageForward,
			Source:         SourceOpenAIHTTPExecutableStage,
			Method:         http.MethodPost,
			Path:           "/v1/responses",
			Handler:        "OpenAIGatewayHandler.Responses",
			Protocol:       "openai_responses",
			Count:          4,
			ErrorCount:     2,
			LastObservedAt: &oldObservedAt,
		},
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageUsage,
			Source:         SourceOpenAIHTTPExecutableStage,
			Method:         http.MethodPost,
			Path:           "/v1/responses",
			Handler:        "OpenAIGatewayHandler.Responses",
			Protocol:       "openai_responses",
			Count:          3,
			ErrorCount:     1,
			LastObservedAt: &recentObservedAt,
		},
		{
			Pipeline:       PipelineGatewayPreForward,
			Stage:          StageRouting,
			Source:         SourceGatewayRoutingStage,
			Method:         http.MethodPost,
			Path:           "/v1/messages",
			Handler:        "GatewayHandler.Messages",
			Protocol:       "anthropic_messages",
			Count:          2,
			LastObservedAt: &recentObservedAt,
		},
	})
	defer restoreObserver()

	snapshot := PipelineExecutionObserverSnapshot()
	require.Len(t, snapshot.Routes, 2)
	require.Equal(t, PipelineStageRouteExecutionObservation{
		Pipeline:       PipelineGatewayPreForward,
		Method:         http.MethodPost,
		Path:           "/v1/messages",
		Handler:        "GatewayHandler.Messages",
		Protocol:       "anthropic_messages",
		Count:          2,
		RecentCount:    2,
		LastObservedAt: snapshot.Routes[0].LastObservedAt,
		Stages:         []PipelineStageExecutionObservation{snapshot.Routes[0].Stages[0]},
	}, snapshot.Routes[0])
	require.Equal(t, PipelineGatewayPreForward, snapshot.Routes[0].Stages[0].Pipeline)
	require.Equal(t, StageRouting, snapshot.Routes[0].Stages[0].Stage)
	require.Equal(t, int64(2), snapshot.Routes[0].Stages[0].Count)

	require.Equal(t, PipelineOpenAIHTTP, snapshot.Routes[1].Pipeline)
	require.Equal(t, http.MethodPost, snapshot.Routes[1].Method)
	require.Equal(t, "/v1/responses", snapshot.Routes[1].Path)
	require.Equal(t, "OpenAIGatewayHandler.Responses", snapshot.Routes[1].Handler)
	require.Equal(t, "openai_responses", snapshot.Routes[1].Protocol)
	require.Equal(t, int64(7), snapshot.Routes[1].Count)
	require.Equal(t, int64(3), snapshot.Routes[1].ErrorCount)
	require.Equal(t, int64(3), snapshot.Routes[1].RecentCount)
	require.Equal(t, int64(1), snapshot.Routes[1].RecentErrorCount)
	require.Len(t, snapshot.Routes[1].Stages, 2)
	require.Equal(t, StageForward, snapshot.Routes[1].Stages[0].Stage)
	require.Equal(t, StageUsage, snapshot.Routes[1].Stages[1].Stage)
}

func TestPipelineExecutionObserverReportsExpectedStageObservationCoverage(t *testing.T) {
	restoreRegistry := ReplaceRegistryForTest([]Entry{
		{
			Method:             http.MethodPost,
			Path:               "/v1/responses",
			Handler:            "OpenAIGatewayHandler.Responses",
			Upstream:           true,
			ModerationRequired: true,
			Protocol:           "openai_responses",
			Pipeline:           PipelineOpenAIHTTP,
			Status:             StatusCovered,
			StageCoverage: []PipelineStageCoverage{
				CoveredPipelineStage(StageRouting),
				CoveredPipelineStage(StageForward),
			},
		},
	})
	defer restoreRegistry()
	observedAt := time.Now().UTC()
	restoreObserver := ReplacePipelineExecutionObserverForTest([]PipelineStageExecutionObservation{
		{
			Pipeline:       PipelineOpenAIHTTP,
			Stage:          StageRouting,
			Source:         SourceOpenAIHTTPExecutableStage,
			Method:         http.MethodPost,
			Path:           "/v1/responses",
			Handler:        "OpenAIGatewayHandler.Responses",
			Protocol:       "openai_responses",
			Count:          1,
			LastObservedAt: &observedAt,
		},
	})
	defer restoreObserver()

	snapshot := PipelineExecutionObserverSnapshot()

	require.Equal(t, PipelineExecutionStageObservationCoverage{
		Status:           "mismatch",
		ExpectedStages:   2,
		ObservedStages:   1,
		UnobservedStages: []string{"POST /v1/responses OpenAIGatewayHandler.Responses forward"},
	}, snapshot.StageObservationCoverage)
}

func TestPipelineStageExecutionsFromContextNormalizesStoredValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(PipelineStageExecutionsContextKey, []PipelineStageExecution{
		{Pipeline: " OPENAI_HTTP ", Stage: " USAGE ", Source: " Source "},
		{Pipeline: PipelineOpenAIHTTP, Stage: StageUsage, Source: "Source"},
	})

	require.Equal(t, []PipelineStageExecution{
		{Pipeline: PipelineOpenAIHTTP, Stage: StageUsage, Source: "Source"},
	}, PipelineStageExecutionsFromContext(c))
}
