package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
)

type openAIHTTPExecutableStage struct {
	Stage string
	Run   func() openAIHTTPExecutableStageResult
}

type openAIHTTPExecutableStageResult struct {
	Stop bool
	Err  error
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStages(c *gin.Context, stages []openAIHTTPExecutableStage) openAIHTTPExecutableStageResult {
	for _, stage := range stages {
		result := h.runOpenAIHTTPExecutableStage(c, stage.Stage, stage.Run)
		if result.Stop || result.Err != nil {
			return result
		}
	}
	return openAIHTTPExecutableStageResult{}
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStage(c *gin.Context, stage string, run func() openAIHTTPExecutableStageResult) openAIHTTPExecutableStageResult {
	if run == nil {
		return openAIHTTPExecutableStageResult{}
	}
	result := run()
	moderationcoverage.MarkPipelineStageExecuted(
		c,
		moderationcoverage.PipelineOpenAIHTTP,
		stage,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
	)
	return result
}
