package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
)

type ExecutableStage struct {
	Name string
	Run  func() ExecutableStageResult
}

type ExecutableStageResult struct {
	Stop bool
	Err  error
}

type GatewayPipeline struct {
	Pipeline string
	Source   string
	Stages   []ExecutableStage
}

func ForwardStage(name string, run func() ExecutableStageResult) ExecutableStage {
	return ExecutableStage{
		Name: name,
		Run:  run,
	}
}

func (p GatewayPipeline) Run(c *gin.Context) ExecutableStageResult {
	for _, stage := range p.Stages {
		if stage.Run == nil {
			continue
		}
		result := stage.Run()
		moderationcoverage.MarkPipelineStageExecuted(c, p.Pipeline, stage.Name, p.Source)
		if result.Stop || result.Err != nil {
			return result
		}
	}
	return ExecutableStageResult{}
}

type openAIHTTPExecutableStage struct {
	Stage string
	Run   func() openAIHTTPExecutableStageResult
}

type openAIHTTPExecutableStageResult = ExecutableStageResult

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStages(c *gin.Context, stages []openAIHTTPExecutableStage) openAIHTTPExecutableStageResult {
	return openAIHTTPExecutablePipeline(stages).Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStage(c *gin.Context, stage string, run func() openAIHTTPExecutableStageResult) openAIHTTPExecutableStageResult {
	return openAIHTTPExecutablePipeline([]openAIHTTPExecutableStage{
		{Stage: stage, Run: run},
	}).Run(c)
}

func openAIHTTPExecutablePipeline(stages []openAIHTTPExecutableStage) GatewayPipeline {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages:   openAIHTTPExecutableStages(stages),
	}
}

func openAIHTTPExecutableStages(stages []openAIHTTPExecutableStage) []ExecutableStage {
	executableStages := make([]ExecutableStage, 0, len(stages))
	for _, stage := range stages {
		executableStages = append(executableStages, ExecutableStage{
			Name: stage.Stage,
			Run:  stage.Run,
		})
	}
	return executableStages
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketExecutableStage(c *gin.Context, stage string, run func() ExecutableStageResult) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			{Name: stage, Run: run},
		},
	}.Run(c)
}
