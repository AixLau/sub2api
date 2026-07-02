package handler

import (
	"context"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
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

type GatewayPipelineRunner struct{}

type ForwardStage interface {
	StageName() string
	RunForward(*gin.Context) ExecutableStageResult
}

type BillingStage interface {
	StageName() string
	RunBilling(*gin.Context) ExecutableStageResult
}

type RoutingStage interface {
	StageName() string
	RunRouting(*gin.Context) ExecutableStageResult
}

type ForwardStageAdapter struct {
	Name    string
	Forward func(*gin.Context) ExecutableStageResult
}

type BillingStageAdapter struct {
	Name    string
	Billing func(*gin.Context) ExecutableStageResult
}

type RoutingStageAdapter struct {
	Name    string
	Routing func(*gin.Context) ExecutableStageResult
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

func (a BillingStageAdapter) StageName() string {
	if a.Name == "" {
		return moderationcoverage.StageBilling
	}
	return a.Name
}

func (a BillingStageAdapter) RunBilling(c *gin.Context) ExecutableStageResult {
	if a.Billing == nil {
		return ExecutableStageResult{}
	}
	return a.Billing(c)
}

func (a RoutingStageAdapter) StageName() string {
	if a.Name == "" {
		return moderationcoverage.StageRouting
	}
	return a.Name
}

func (a RoutingStageAdapter) RunRouting(c *gin.Context) ExecutableStageResult {
	if a.Routing == nil {
		return ExecutableStageResult{}
	}
	return a.Routing(c)
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

func ExecutableBillingStage(adapter BillingStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(c *gin.Context) ExecutableStageResult {
			return adapter.RunBilling(c)
		},
	}
}

func ExecutableRoutingStage(adapter RoutingStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(c *gin.Context) ExecutableStageResult {
			return adapter.RunRouting(c)
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

func executableBillingStageWithContext(c *gin.Context, adapter BillingStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(*gin.Context) ExecutableStageResult {
			return adapter.RunBilling(c)
		},
	}
}

func executableRoutingStageWithContext(c *gin.Context, adapter RoutingStage) ExecutableStage {
	return ExecutableStage{
		Name: adapter.StageName(),
		RunWithContext: func(*gin.Context) ExecutableStageResult {
			return adapter.RunRouting(c)
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
	return GatewayPipelineRunner{}.Run(c, p)
}

func (GatewayPipelineRunner) Run(c *gin.Context, p GatewayPipeline) ExecutableStageResult {
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
		moderationcoverage.ObservePipelineStageExecutedWithResult(c, moderationcoverage.PipelineGatewayGlobal, stage.Name, p.Source, result.Err != nil)
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

func (h *OpenAIGatewayHandler) runOpenAIHTTPBillingStage(c *gin.Context, adapter BillingStage) openAIHTTPExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			executableBillingStageWithContext(c, adapter),
		},
	}.Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPRoutingStage(c *gin.Context, adapter RoutingStage) openAIHTTPExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIHTTP,
		Source:   moderationcoverage.SourceOpenAIHTTPExecutableStage,
		Stages: []ExecutableStage{
			executableRoutingStageWithContext(c, adapter),
		},
	}.Run(c)
}

type OpenAIHTTPBillingStage struct {
	Handler          *OpenAIGatewayHandler
	ReqLog           *zap.Logger
	APIKey           *service.APIKey
	Subscription     *service.UserSubscription
	StreamStarted    bool
	ErrorResponder   func(*gin.Context, int, string, string)
	ErrorComponent   string
	RequestContext   context.Context
	QuotaPlatformCtx context.Context
}

func (OpenAIHTTPBillingStage) StageName() string {
	return moderationcoverage.StageBilling
}

func (s OpenAIHTTPBillingStage) RunBilling(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.billingCacheService == nil || s.APIKey == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	quotaCtx := s.QuotaPlatformCtx
	if quotaCtx == nil {
		quotaCtx = c.Request.Context()
	}
	reqLog := s.ReqLog
	if reqLog == nil {
		reqLog = zap.NewNop()
	}
	if err := h.billingCacheService.CheckBillingEligibility(ctx, s.APIKey.User, s.APIKey, s.APIKey.Group, s.Subscription, service.QuotaPlatform(quotaCtx, s.APIKey)); err != nil {
		component := s.ErrorComponent
		if component == "" {
			component = "openai.billing_eligibility_check_failed"
		}
		reqLog.Info(component, zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		if s.ErrorResponder != nil {
			s.ErrorResponder(c, status, code, message)
		} else {
			h.handleStreamingAwareError(c, status, code, message, s.StreamStarted)
		}
		return ExecutableStageResult{Stop: true, Err: err}
	}
	return ExecutableStageResult{}
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

type OpenAIHTTPForwardKind string

const (
	OpenAIHTTPForwardResponses       OpenAIHTTPForwardKind = "responses"
	OpenAIHTTPForwardChatCompletions OpenAIHTTPForwardKind = "chat_completions"
	OpenAIHTTPForwardMessages        OpenAIHTTPForwardKind = "messages"
	OpenAIHTTPForwardImages          OpenAIHTTPForwardKind = "images"
	OpenAIHTTPForwardEmbeddings      OpenAIHTTPForwardKind = "embeddings"
)

type OpenAIHTTPForwardStage struct {
	GatewayService          *service.OpenAIGatewayService
	Kind                    OpenAIHTTPForwardKind
	RequestContext          context.Context
	Account                 *service.Account
	Body                    []byte
	PromptCacheKey          string
	DefaultMappedModel      string
	ParsedImagesRequest     *service.OpenAIImagesRequest
	ChannelMappedModel      string
	ReleaseFunc             func()
	WriterSizeBeforeForward *int
	Result                  **service.OpenAIForwardResult
}

func (OpenAIHTTPForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s OpenAIHTTPForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.GatewayService == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	if s.WriterSizeBeforeForward != nil {
		*s.WriterSizeBeforeForward = c.Writer.Size()
	}
	defer func() {
		if s.ReleaseFunc != nil {
			s.ReleaseFunc()
		}
	}()

	var result *service.OpenAIForwardResult
	var err error
	switch s.Kind {
	case OpenAIHTTPForwardChatCompletions:
		result, err = s.GatewayService.ForwardAsChatCompletions(ctx, c, s.Account, s.Body, s.PromptCacheKey, s.DefaultMappedModel)
	case OpenAIHTTPForwardMessages:
		result, err = s.GatewayService.ForwardAsAnthropic(ctx, c, s.Account, s.Body, s.PromptCacheKey, s.DefaultMappedModel)
	case OpenAIHTTPForwardImages:
		result, err = s.GatewayService.ForwardImages(ctx, c, s.Account, s.Body, s.ParsedImagesRequest, s.ChannelMappedModel)
	case OpenAIHTTPForwardEmbeddings:
		result, err = s.GatewayService.ForwardEmbeddings(ctx, c, s.Account, s.Body, s.DefaultMappedModel)
	default:
		result, err = s.GatewayService.Forward(ctx, c, s.Account, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
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

type OpenAIHTTPUsageStage struct {
	Handler            *OpenAIGatewayHandler
	RequestContext     context.Context
	Result             *service.OpenAIForwardResult
	APIKey             *service.APIKey
	Account            *service.Account
	Subscription       *service.UserSubscription
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	ClientIP           string
	RequestPayloadHash string
	ChannelUsageFields service.ChannelUsageFields
	CyberBlocked       bool
	Mandatory          bool
	LogComponent       string
	LogMessage         string
	LogUserID          int64
	LogModel           string
}

func (OpenAIHTTPUsageStage) StageName() string {
	return moderationcoverage.StageUsage
}

func (s OpenAIHTTPUsageStage) RunUsage(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	record := func(taskCtx context.Context) {
		if err := h.gatewayService.RecordUsage(taskCtx, &service.OpenAIRecordUsageInput{
			Result:             s.Result,
			APIKey:             s.APIKey,
			User:               s.APIKey.User,
			Account:            s.Account,
			Subscription:       s.Subscription,
			InboundEndpoint:    s.InboundEndpoint,
			UpstreamEndpoint:   s.UpstreamEndpoint,
			UserAgent:          s.UserAgent,
			IPAddress:          s.ClientIP,
			RequestPayloadHash: s.RequestPayloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: s.ChannelUsageFields,
			CyberBlocked:       s.CyberBlocked,
		}); err != nil {
			logger.L().With(
				zap.String("component", s.LogComponent),
				zap.Int64("user_id", s.LogUserID),
				zap.Int64("api_key_id", s.APIKey.ID),
				zap.Any("group_id", s.APIKey.GroupID),
				zap.String("model", s.LogModel),
				zap.Int64("account_id", s.Account.ID),
			).Error(s.LogMessage, zap.Error(err))
		}
	}
	if s.Mandatory {
		h.submitMandatoryUsageRecordTask(ctx, record)
		return ExecutableStageResult{}
	}
	h.submitOpenAIUsageRecordTask(ctx, s.Result, record)
	return ExecutableStageResult{}
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

func (h *OpenAIGatewayHandler) runOpenAIWebSocketBillingStage(c *gin.Context, adapter BillingStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			executableBillingStageWithContext(c, adapter),
		},
	}.Run(c)
}

type OpenAIWebSocketBillingStage struct {
	Handler          *OpenAIGatewayHandler
	RequestContext   context.Context
	QuotaPlatformCtx context.Context
	ReqLog           *zap.Logger
	APIKey           *service.APIKey
	Subscription     *service.UserSubscription
	ClientConn       *coderws.Conn
}

func (OpenAIWebSocketBillingStage) StageName() string {
	return moderationcoverage.StageBilling
}

func (s OpenAIWebSocketBillingStage) RunBilling(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.billingCacheService == nil || s.APIKey == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	quotaCtx := s.QuotaPlatformCtx
	if quotaCtx == nil {
		quotaCtx = c.Request.Context()
	}
	reqLog := s.ReqLog
	if reqLog == nil {
		reqLog = zap.NewNop()
	}
	if err := h.billingCacheService.CheckBillingEligibility(ctx, s.APIKey.User, s.APIKey, s.APIKey.Group, s.Subscription, service.QuotaPlatform(quotaCtx, s.APIKey)); err != nil {
		reqLog.Info("openai.websocket_billing_eligibility_check_failed", zap.Error(err))
		closeOpenAIClientWS(s.ClientConn, coderws.StatusPolicyViolation, "billing check failed")
		return ExecutableStageResult{Stop: true, Err: err}
	}
	return ExecutableStageResult{}
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketRoutingStage(c *gin.Context, adapter RoutingStage) ExecutableStageResult {
	return GatewayPipeline{
		Pipeline: moderationcoverage.PipelineOpenAIWebSocket,
		Source:   moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		Stages: []ExecutableStage{
			executableRoutingStageWithContext(c, adapter),
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

type OpenAIWebSocketForwardStage struct {
	GatewayService *service.OpenAIGatewayService
	RequestContext context.Context
	ClientConn     *coderws.Conn
	Account        *service.Account
	Token          string
	FirstMessage   []byte
	Hooks          *service.OpenAIWSIngressHooks
	Err            *error
}

func (OpenAIWebSocketForwardStage) StageName() string {
	return moderationcoverage.StageForward
}

func (s OpenAIWebSocketForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.GatewayService == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	err := s.GatewayService.ProxyResponsesWebSocketFromClient(ctx, c, s.ClientConn, s.Account, s.Token, s.FirstMessage, s.Hooks)
	if s.Err != nil {
		*s.Err = err
	}
	return ExecutableStageResult{Err: err}
}

type OpenAIWebSocketUsageStage struct {
	Handler              *OpenAIGatewayHandler
	RequestContext       context.Context
	ReqLog               *zap.Logger
	APIKey               *service.APIKey
	Account              *service.Account
	Subscription         *service.UserSubscription
	Model                string
	TurnErr              error
	Result               *service.OpenAIForwardResult
	CyberBlockKey        string
	ChannelMapping       service.ChannelMappingResult
	RequestPayloadHash   string
	ReleaseTurnSlots     func()
	CyberBlockedThisConn *bool
	UserAgent            string
	ClientIP             string
}

func (OpenAIWebSocketUsageStage) StageName() string {
	return moderationcoverage.StageUsage
}

func (s OpenAIWebSocketUsageStage) RunUsage(c *gin.Context) ExecutableStageResult {
	// Cyber turn state must live exactly one turn; usage recording runs async.
	defer clearCyberPolicyTurnState(c)
	if s.ReleaseTurnSlots != nil {
		s.ReleaseTurnSlots()
	}
	h := s.Handler
	if h == nil {
		return ExecutableStageResult{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	reqLog := s.ReqLog
	if reqLog == nil {
		reqLog = zap.NewNop()
	}

	h.recordCyberPolicyIfMarked(c, s.APIKey, s.Account, s.Subscription, s.Model, s.TurnErr != nil, s.CyberBlockKey, s.ChannelMapping.ToUsageFields(s.Model, ""), s.RequestPayloadHash)
	if service.GetOpsCyberPolicy(c) != nil && s.CyberBlockedThisConn != nil {
		*s.CyberBlockedThisConn = true
	}
	if s.TurnErr != nil {
		if s.Result == nil || s.Result.ImageCount <= 0 {
			return ExecutableStageResult{}
		}
		// Cyber-hit usage is already written by recordCyberPolicyIfMarked(forwardErrored=true).
		if service.GetOpsCyberPolicy(c) != nil {
			return ExecutableStageResult{}
		}
		reqLog.Warn("openai.websocket_partial_error_with_image_result",
			zap.Int64("account_id", s.Account.ID),
			zap.Int("image_count", s.Result.ImageCount),
			zap.Error(s.TurnErr),
		)
	}
	if s.Result == nil {
		return ExecutableStageResult{}
	}
	if s.Account.Type == service.AccountTypeOAuth {
		h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(ctx, s.Account.ID, s.Result.ResponseHeaders)
	}
	h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account.ID, true, s.Result.FirstTokenMs)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, s.Account)
	cyberBlocked := service.GetOpsCyberPolicy(c) != nil
	h.submitOpenAIUsageRecordTask(ctx, s.Result, func(taskCtx context.Context) {
		if err := h.gatewayService.RecordUsage(taskCtx, &service.OpenAIRecordUsageInput{
			Result:             s.Result,
			APIKey:             s.APIKey,
			User:               s.APIKey.User,
			Account:            s.Account,
			Subscription:       s.Subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          s.UserAgent,
			IPAddress:          s.ClientIP,
			RequestPayloadHash: s.RequestPayloadHash,
			APIKeyService:      h.apiKeyService,
			ChannelUsageFields: s.ChannelMapping.ToUsageFields(s.Model, s.Result.UpstreamModel),
			CyberBlocked:       cyberBlocked,
		}); err != nil {
			reqLog.Error("openai.websocket_record_usage_failed",
				zap.Int64("account_id", s.Account.ID),
				zap.String("request_id", s.Result.RequestID),
				zap.Error(err),
			)
		}
	})
	return ExecutableStageResult{}
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
