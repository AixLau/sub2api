package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/gin-gonic/gin"
)

type ExecutableStage struct {
	Name           string
	Run            func() ExecutableStageResult
	RunWithContext func(*gin.Context) ExecutableStageResult
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

type ForwardStage interface {
	StageName() string
	RunForward(*gin.Context) ExecutableStageResult
}

type ForwardStageAdapter struct {
	Name    string
	Forward func(*gin.Context) ExecutableStageResult
}

type UsageStage interface {
	StageName() string
	RunUsage(*gin.Context) ExecutableStageResult
}

type UsageStageAdapter struct {
	Name  string
	Usage func(*gin.Context) ExecutableStageResult
}

func (a ForwardStageAdapter) StageName() string {
	if a.Name == "" {
		return moderationcoverage.StageForward
	}
	return a.Name
}

func (a ForwardStageAdapter) RunForward(c *gin.Context) ExecutableStageResult {
	if a.Forward == nil {
		return ExecutableStageResult{}
	}
	return a.Forward(c)
}

func (a UsageStageAdapter) StageName() string {
	if a.Name == "" {
		return moderationcoverage.StageUsage
	}
	return a.Name
}

func (a UsageStageAdapter) RunUsage(c *gin.Context) ExecutableStageResult {
	if a.Usage == nil {
		return ExecutableStageResult{}
	}
	return a.Usage(c)
}

func ForwardStageFunc(name string, run func() ExecutableStageResult) ExecutableStage {
	return ExecutableForwardStage(ForwardStageAdapter{
		Name: name,
		Forward: func(*gin.Context) ExecutableStageResult {
			return run()
		},
	})
}

func ExecutableForwardStage(adapter ForwardStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(c *gin.Context) ExecutableStageResult {
			return adapter.RunForward(c)
		},
	}
}

func ExecutableUsageStage(adapter UsageStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(c *gin.Context) ExecutableStageResult {
			return adapter.RunUsage(c)
		},
	}
}

func executableForwardStageWithContext(c *gin.Context, adapter ForwardStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(*gin.Context) ExecutableStageResult {
			return adapter.RunForward(c)
		},
	}
}

func executableUsageStageWithContext(c *gin.Context, adapter UsageStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(*gin.Context) ExecutableStageResult {
			return adapter.RunUsage(c)
		},
	}
}

func (p GatewayPipeline) Run(c *gin.Context) ExecutableStageResult {
	for _, stage := range p.Stages {
		if stage.Run == nil && stage.RunWithContext == nil {
			continue
		}
		result := ExecutableStageResult{}
		if stage.RunWithContext != nil {
			result = stage.RunWithContext(c)
		} else {
			result = stage.Run()
		}
		moderationcoverage.MarkPipelineStageExecutedWithResult(c, p.Pipeline, stage.Name, p.Source, result.Err != nil)
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

func (h *OpenAIGatewayHandler) runOpenAIHTTPForwardStage(c *gin.Context, adapter ForwardStage) openAIHTTPExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			executableForwardStageWithContext(c, adapter),
		},
	}.Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPUsageStage(c *gin.Context, adapter UsageStage) openAIHTTPExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			executableUsageStageWithContext(c, adapter),
		},
	}.Run(c)
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

func (h *OpenAIGatewayHandler) runOpenAIWebSocketForwardStage(c *gin.Context, adapter ForwardStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			executableForwardStageWithContext(c, adapter),
		},
	}.Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketUsageStage(c *gin.Context, adapter UsageStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			executableUsageStageWithContext(c, adapter),
		},
	}.Run(c)
}
