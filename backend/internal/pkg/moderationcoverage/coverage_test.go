package moderationcoverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPPipelineStagesForRouteIsSharedFactSource(t *testing.T) {
	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageCyber, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.ChatCompletions", "openai_chat_completions"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageCyber, Required: true, Covered: true},
		{Stage: StageImage, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Responses", "openai_responses"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageImage, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Images", "openai_images"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Embeddings", "openai_embeddings"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageCyber, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Messages", "openai_messages"))

	require.Nil(t, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"))
	require.Nil(t, OpenAIHTTPPipelineStagesForRoute("GatewayHandler.Messages", "anthropic_messages"))
}

func TestAnnotatePipelineCoverageUsesSharedOpenAIHTTPFacts(t *testing.T) {
	entry := AnnotatePipelineCoverage(Entry{
		Method:             "POST",
		Path:               "/v1/responses",
		Handler:            "OpenAIGatewayHandler.Responses",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           "openai_responses",
		Status:             StatusCovered,
	})

	require.Equal(t, PipelineOpenAIHTTP, entry.Pipeline)
	require.Equal(t, OpenAIHTTPPipelineStagesForRoute(entry.Handler, entry.Protocol), entry.StageCoverage)
}

func TestGatewayPreForwardPipelineStagesForRouteIsSharedFactSource(t *testing.T) {
	withForward := []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StagePreForward, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
	}
	withForwardAndUsage := []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StagePreForward, Required: true, Covered: true},
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}

	require.Equal(t, withForwardAndUsage, GatewayPreForwardPipelineStagesForRoute("GatewayHandler.Messages", "anthropic_messages"))
	require.Equal(t, withForward, GatewayPreForwardPipelineStagesForRoute("GatewayHandler.CountTokens", "anthropic_messages"))
	require.Equal(t, withForwardAndUsage, GatewayPreForwardPipelineStagesForRoute("GatewayHandler.GeminiV1BetaModels", "gemini"))
	require.Equal(t, withForwardAndUsage, GatewayPreForwardPipelineStagesForRoute("GatewayHandler.ChatCompletions", "openai_chat_completions"))
	require.Equal(t, withForwardAndUsage, GatewayPreForwardPipelineStagesForRoute("GatewayHandler.Responses", "openai_responses"))
	require.Nil(t, GatewayPreForwardPipelineStagesForRoute("OpenAIGatewayHandler.Responses", "openai_responses"))
}

func TestAnnotatePipelineCoverageUsesSharedGatewayPreForwardFacts(t *testing.T) {
	entry := AnnotatePipelineCoverage(Entry{
		Method:             "POST",
		Path:               "/v1/messages",
		Handler:            "GatewayHandler.Messages",
		Upstream:           true,
		ModerationRequired: true,
		Protocol:           "anthropic_messages",
		Status:             StatusCovered,
	})

	require.Equal(t, PipelineGatewayPreForward, entry.Pipeline)
	require.Equal(t, GatewayPreForwardPipelineStagesForRoute(entry.Handler, entry.Protocol), entry.StageCoverage)
}

func TestForwardAdaptersForRouteIsSharedFactSource(t *testing.T) {
	require.Equal(t, []string{"OpenAIHTTPForwardStage"}, ForwardAdaptersForRoute("OpenAIGatewayHandler.Responses", "openai_responses"))
	require.Equal(t, []string{"OpenAIHTTPForwardStage"}, ForwardAdaptersForRoute("OpenAIGatewayHandler.Embeddings", "openai_embeddings"))
	require.Equal(t, []string{"OpenAIWebSocketForwardStage"}, ForwardAdaptersForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"))
	require.Equal(t, []string{"GatewayMessagesGeminiForwardStage", "GatewayMessagesForwardStage"}, ForwardAdaptersForRoute("GatewayHandler.Messages", "anthropic_messages"))
	require.Equal(t, []string{"GatewayCountTokensForwardStage"}, ForwardAdaptersForRoute("GatewayHandler.CountTokens", "anthropic_messages"))
	require.Equal(t, []string{"GatewayGeminiV1BetaForwardStage"}, ForwardAdaptersForRoute("GatewayHandler.GeminiV1BetaModels", "gemini"))
	require.Equal(t, []string{"GatewayChatCompletionsForwardStage"}, ForwardAdaptersForRoute("GatewayHandler.ChatCompletions", "openai_chat_completions"))
	require.Equal(t, []string{"GatewayResponsesForwardStage"}, ForwardAdaptersForRoute("GatewayHandler.Responses", "openai_responses"))

	require.Nil(t, ForwardAdaptersForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_realtime"))
	require.Nil(t, ForwardAdaptersForRoute("UnknownHandler", "openai_responses"))
}

func TestForwardAdapterDescriptorsForRouteCarriesExecutableMetadata(t *testing.T) {
	require.Equal(t, []RouteAdapterDescriptor{{
		Stage:    StageForward,
		Pipeline: PipelineOpenAIHTTP,
		Name:     "OpenAIHTTPForwardStage",
	}}, ForwardAdapterDescriptorsForRoute("OpenAIGatewayHandler.Responses", "openai_responses"))

	require.Equal(t, []RouteAdapterDescriptor{{
		Stage:    StageForward,
		Pipeline: PipelineOpenAIWebSocket,
		Name:     "OpenAIWebSocketForwardStage",
	}}, ForwardAdapterDescriptorsForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"))

	require.Equal(t, []RouteAdapterDescriptor{
		{Stage: StageForward, Pipeline: PipelineGatewayPreForward, Name: "GatewayMessagesGeminiForwardStage"},
		{Stage: StageForward, Pipeline: PipelineGatewayPreForward, Name: "GatewayMessagesForwardStage"},
	}, ForwardAdapterDescriptorsForRoute("GatewayHandler.Messages", "anthropic_messages"))

	require.Nil(t, ForwardAdapterDescriptorsForRoute("UnknownHandler", "openai_responses"))
}

func TestNormalizeStageCoverageSortsExecutableGatewayStages(t *testing.T) {
	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageBilling, Required: true, Covered: true},
		{Stage: StageRouting, Required: true, Covered: true},
		{Stage: StageForward, Required: true, Covered: true},
		{Stage: StageUsage, Required: true, Covered: true},
	}, NormalizeStageCoverage([]PipelineStageCoverage{
		{Stage: " USAGE ", Required: true, Covered: true},
		{Stage: " FORWARD ", Required: true, Covered: true},
		{Stage: " BILLING ", Required: true, Covered: true},
		{Stage: " ROUTING ", Required: true, Covered: true},
	}))
}
