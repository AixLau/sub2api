package moderationcoverage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHTTPPipelineStagesForRouteIsSharedFactSource(t *testing.T) {
	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageCyber, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.ChatCompletions", "openai_chat_completions"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageCyber, Required: true, Covered: true},
		{Stage: StageImage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Responses", "openai_responses"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
		{Stage: StageImage, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Images", "openai_images"))

	require.Equal(t, []PipelineStageCoverage{
		{Stage: StageModeration, Required: true, Covered: true},
	}, OpenAIHTTPPipelineStagesForRoute("OpenAIGatewayHandler.Embeddings", "openai_embeddings"))

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
