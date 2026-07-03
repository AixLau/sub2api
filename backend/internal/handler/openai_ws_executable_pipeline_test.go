package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponsesWebSocketCoverageIncludesExecutableStageMetadata(t *testing.T) {
	require.Equal(t, []moderationcoverage.PipelineStageCoverage{
		{Stage: moderationcoverage.StageModeration, Required: true, Covered: true},
		{Stage: moderationcoverage.StageCyber, Required: true, Covered: true},
		{Stage: moderationcoverage.StageImage, Required: true, Covered: true},
		{Stage: moderationcoverage.StagePreForward, Required: true, Covered: true},
		{Stage: moderationcoverage.StageBilling, Required: true, Covered: true},
		{Stage: moderationcoverage.StageRouting, Required: true, Covered: true},
		{Stage: moderationcoverage.StageForward, Required: true, Covered: true},
		{Stage: moderationcoverage.StageUsage, Required: true, Covered: true},
	}, moderationcoverage.OpenAIWebSocketPipelineStagesForRoute("OpenAIGatewayHandler.ResponsesWebSocket", "openai_responses"))
}
