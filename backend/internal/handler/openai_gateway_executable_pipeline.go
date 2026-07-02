package handler

import (
	"context"
	"errors"
	"net/http"
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

type ForwardStageRegistry struct {
	adapters map[forwardStageRegistryKey]ForwardStage
}

type forwardStageRegistryKey struct {
	Stage    string
	Pipeline string
	Name     string
}

type registeredForwardStage struct {
	stage   string
	adapter ForwardStage
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

func NewForwardStageRegistry() *ForwardStageRegistry {
	return &ForwardStageRegistry{adapters: map[forwardStageRegistryKey]ForwardStage{}}
}

func (r *ForwardStageRegistry) Register(descriptor moderationcoverage.RouteAdapterDescriptor, adapter ForwardStage) {
	if r == nil || adapter == nil {
		return
	}
	key := forwardStageRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageForward || key.Pipeline == "" || key.Name == "" {
		return
	}
	if r.adapters == nil {
		r.adapters = map[forwardStageRegistryKey]ForwardStage{}
	}
	r.adapters[key] = adapter
}

func (r *ForwardStageRegistry) Resolve(descriptor moderationcoverage.RouteAdapterDescriptor) (ForwardStage, bool) {
	if r == nil {
		return nil, false
	}
	key := forwardStageRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageForward || key.Pipeline == "" || key.Name == "" {
		return nil, false
	}
	adapter, ok := r.adapters[key]
	if !ok {
		return nil, false
	}
	return registeredForwardStage{stage: key.Stage, adapter: adapter}, true
}

func forwardStageRegistryKeyFromDescriptor(descriptor moderationcoverage.RouteAdapterDescriptor) forwardStageRegistryKey {
	return forwardStageRegistryKey{
		Stage:    moderationcoverage.NormalizeStage(descriptor.Stage),
		Pipeline: moderationcoverage.NormalizePipeline(descriptor.Pipeline),
		Name:     descriptor.Name,
	}
}

func (s registeredForwardStage) StageName() string {
	if s.stage == "" {
		return moderationcoverage.StageForward
	}
	return s.stage
}

func (s registeredForwardStage) RunForward(c *gin.Context) ExecutableStageResult {
	if s.adapter == nil {
		return ExecutableStageResult{}
	}
	return s.adapter.RunForward(c)
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

func newGatewayPipeline(pipeline string, source string, stages []ExecutableStage) GatewayPipeline {
	return GatewayPipeline{
		Pipeline: pipeline,
		Source:   source,
		Stages:   stages,
	}
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

func runGatewayPipelineStage(c *gin.Context, pipeline string, source string, stage ExecutableStage) ExecutableStageResult {
	if stage.Name == "" && stage.Run == nil && stage.RunWithContext == nil {
		return ExecutableStageResult{}
	}
	return newGatewayPipeline(pipeline, source, []ExecutableStage{stage}).Run(c)
}

type openAIHTTPExecutableStage struct {
	Stage string
	Run   func() openAIHTTPExecutableStageResult
}

type openAIHTTPExecutableStageResult = ExecutableStageResult

type openAIHTTPRoutingErrorFormat int

const (
	openAIHTTPRoutingErrorOpenAI openAIHTTPRoutingErrorFormat = iota
	openAIHTTPRoutingErrorAnthropicMessages
	openAIHTTPRoutingErrorEmbeddings
)

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStages(c *gin.Context, stages []openAIHTTPExecutableStage) openAIHTTPExecutableStageResult {
	return openAIHTTPExecutablePipeline(stages).Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPExecutableStage(c *gin.Context, stage string, run func() openAIHTTPExecutableStageResult) openAIHTTPExecutableStageResult {
	return openAIHTTPExecutablePipeline([]openAIHTTPExecutableStage{
		{Stage: stage, Run: run},
	}).Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPBillingStage(c *gin.Context, adapter BillingStage) openAIHTTPExecutableStageResult {
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableBillingStageWithContext(c, adapter),
	)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPRoutingStage(c *gin.Context, adapter RoutingStage) openAIHTTPExecutableStageResult {
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableRoutingStageWithContext(c, adapter),
	)
}

type OpenAIHTTPRoutingStage struct {
	Handler                    *OpenAIGatewayHandler
	RequestContext             context.Context
	ReqLog                     *zap.Logger
	APIKey                     *service.APIKey
	SubjectUserID              int64
	RequestedModel             string
	DisplayModel               string
	SessionHash                *string
	PreviousResponseID         string
	FailedAccountIDs           map[int64]struct{}
	RequiredTransport          service.OpenAIUpstreamTransport
	RequiredCapability         service.OpenAIEndpointCapability
	RequiredImageCapability    service.OpenAIImagesCapability
	RequireCompact             bool
	RequestPlatform            string
	Stream                     bool
	StreamStarted              *bool
	MaxAccountSwitches         int
	SwitchCount                *int
	LastFailoverErr            *service.UpstreamFailoverError
	UseSimpleFailoverExhausted bool
	ErrorFormat                openAIHTTPRoutingErrorFormat
	NoAccountMessage           string
	LogPrefix                  string
	Account                    **service.Account
	AccountReleaseFunc         *func()
	Retry                      *bool
}

func (OpenAIHTTPRoutingStage) StageName() string {
	return moderationcoverage.StageRouting
}

func (s OpenAIHTTPRoutingStage) RunRouting(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.gatewayService == nil || s.APIKey == nil || s.Account == nil {
		return ExecutableStageResult{}
	}
	reqLog := s.ReqLog
	if reqLog == nil {
		reqLog = zap.NewNop()
	}
	logPrefix := s.LogPrefix
	if logPrefix == "" {
		logPrefix = "openai"
	}
	failedAccountIDs := s.FailedAccountIDs
	if failedAccountIDs == nil {
		failedAccountIDs = map[int64]struct{}{}
	}
	ctx := s.RequestContext
	if ctx == nil {
		ctx = c.Request.Context()
	}
	sessionHash := ""
	if s.SessionHash != nil {
		sessionHash = *s.SessionHash
	}
	displayModel := s.DisplayModel
	if displayModel == "" {
		displayModel = s.RequestedModel
	}
	reqLog.Debug(logPrefix+".account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
	selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
		ctx,
		s.APIKey.GroupID,
		s.PreviousResponseID,
		sessionHash,
		s.RequestedModel,
		failedAccountIDs,
		s.RequiredTransport,
		s.RequiredCapability,
		s.RequireCompact,
		s.RequestPlatform,
		s.SubjectUserID,
	)
	if s.RequiredImageCapability != "" {
		selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForImages(
			ctx,
			s.APIKey.GroupID,
			sessionHash,
			s.RequestedModel,
			failedAccountIDs,
			s.RequiredImageCapability,
			s.SubjectUserID,
		)
	}
	if err != nil {
		reqLog.Warn(logPrefix+".account_select_failed", zap.Error(err), zap.Int("excluded_account_count", len(failedAccountIDs)))
		return s.handleOpenAIHTTPRoutingSelectionError(c, err, len(failedAccountIDs) == 0, displayModel)
	}
	if selection == nil || selection.Account == nil {
		s.handleOpenAIHTTPRoutingNoAccount(c, displayModel)
		return ExecutableStageResult{Stop: true}
	}
	if s.PreviousResponseID != "" {
		reqLog.Debug(logPrefix+".account_selected_with_previous_response_id", zap.Int64("account_id", selection.Account.ID))
	}
	if scheduleDecision != (service.OpenAIAccountScheduleDecision{}) {
		reqLog.Debug(logPrefix+".account_schedule_decision",
			zap.String("layer", scheduleDecision.Layer),
			zap.Bool("sticky_previous_hit", scheduleDecision.StickyPreviousHit),
			zap.Bool("sticky_session_hit", scheduleDecision.StickySessionHit),
			zap.Int("candidate_count", scheduleDecision.CandidateCount),
			zap.Int("top_k", scheduleDecision.TopK),
			zap.Int64("latency_ms", scheduleDecision.LatencyMs),
			zap.Float64("load_skew", scheduleDecision.LoadSkew),
		)
	}
	account := selection.Account
	if s.SessionHash != nil {
		*s.SessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		sessionHash = *s.SessionHash
	}
	reqLog.Debug(logPrefix+".account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
	setOpsSelectedAccount(c, account.ID, account.Platform)

	streamStarted := false
	streamStartedPtr := &streamStarted
	if s.StreamStarted != nil {
		streamStartedPtr = s.StreamStarted
	}
	releaseFunc, refreshedAccount, acquired, retryable := h.acquireResponsesAccountSlot(c, s.APIKey.GroupID, sessionHash, selection, s.RequestedModel, false, s.RequiredCapability, s.RequiredImageCapability, s.Stream, streamStartedPtr, reqLog)
	if !acquired {
		if retryable && s.SwitchCount != nil && *s.SwitchCount < s.MaxAccountSwitches {
			failedAccountIDs[account.ID] = struct{}{}
			*s.SwitchCount = *s.SwitchCount + 1
			if s.Retry != nil {
				*s.Retry = true
			}
			reqLog.Info(logPrefix+".concurrency_fallback", zap.Int64("failed_account_id", account.ID), zap.Int("switch_count", *s.SwitchCount))
			return ExecutableStageResult{}
		}
		if retryable {
			s.writeOpenAIHTTPRoutingError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many concurrent requests, please retry later")
		}
		return ExecutableStageResult{Stop: true}
	}
	if s.AccountReleaseFunc != nil {
		*s.AccountReleaseFunc = releaseFunc
	}
	*s.Account = refreshedAccount
	return ExecutableStageResult{}
}

func (s OpenAIHTTPRoutingStage) handleOpenAIHTTPRoutingSelectionError(c *gin.Context, err error, firstAttempt bool, displayModel string) ExecutableStageResult {
	if firstAttempt {
		if errors.Is(err, service.ErrNoAvailableCompactAccounts) {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			s.writeOpenAIHTTPRoutingError(c, http.StatusServiceUnavailable, "compact_not_supported", "No available OpenAI accounts support /responses/compact")
			return ExecutableStageResult{Stop: true, Err: err}
		}
		cls := classifyNoAccountErrorFromGin(c, s.Handler.gatewayService, s.APIKey, displayModel, displayModel, service.PlatformOpenAI)
		if !cls.ModelNotFound {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		}
		message := cls.Message
		if s.NoAccountMessage != "" && !cls.ModelNotFound {
			message = s.NoAccountMessage
		}
		s.writeOpenAIHTTPRoutingError(c, cls.Status, cls.ErrType, message)
		return ExecutableStageResult{Stop: true, Err: err}
	}
	s.writeOpenAIHTTPRoutingFailoverExhausted(c)
	return ExecutableStageResult{Stop: true, Err: err}
}

func (s OpenAIHTTPRoutingStage) handleOpenAIHTTPRoutingNoAccount(c *gin.Context, displayModel string) {
	cls := classifyNoAccountErrorFromGin(c, s.Handler.gatewayService, s.APIKey, displayModel, displayModel, service.PlatformOpenAI)
	if !cls.ModelNotFound {
		markOpsRoutingCapacityLimited(c)
	}
	message := cls.Message
	if s.NoAccountMessage != "" && !cls.ModelNotFound {
		message = s.NoAccountMessage
	}
	s.writeOpenAIHTTPRoutingError(c, cls.Status, cls.ErrType, message)
}

func (s OpenAIHTTPRoutingStage) writeOpenAIHTTPRoutingFailoverExhausted(c *gin.Context) {
	h := s.Handler
	streamStarted := false
	if s.StreamStarted != nil {
		streamStarted = *s.StreamStarted
	}
	switch s.ErrorFormat {
	case openAIHTTPRoutingErrorAnthropicMessages:
		if s.LastFailoverErr != nil {
			h.handleAnthropicFailoverExhausted(c, s.LastFailoverErr, streamStarted)
		} else {
			h.anthropicStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
		}
	case openAIHTTPRoutingErrorEmbeddings:
		if s.LastFailoverErr != nil {
			h.handleFailoverExhausted(c, s.LastFailoverErr, false)
		} else {
			h.errorResponse(c, http.StatusBadGateway, "api_error", "Upstream request failed")
		}
	default:
		if s.LastFailoverErr != nil {
			h.handleFailoverExhausted(c, s.LastFailoverErr, streamStarted)
		} else if s.UseSimpleFailoverExhausted {
			h.handleFailoverExhaustedSimple(c, 502, streamStarted)
		} else {
			h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
		}
	}
}

func (s OpenAIHTTPRoutingStage) writeOpenAIHTTPRoutingError(c *gin.Context, status int, code, message string) {
	h := s.Handler
	streamStarted := false
	if s.StreamStarted != nil {
		streamStarted = *s.StreamStarted
	}
	switch s.ErrorFormat {
	case openAIHTTPRoutingErrorAnthropicMessages:
		h.anthropicStreamingAwareError(c, status, code, message, streamStarted)
	case openAIHTTPRoutingErrorEmbeddings:
		h.errorResponse(c, status, code, message)
	default:
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
	}
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
	adapter = h.openAIHTTPForwardStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableForwardStageWithContext(c, adapter),
	)
}

func (h *OpenAIGatewayHandler) openAIHTTPForwardStageFromRouteDescriptor(c *gin.Context, fallback ForwardStage) ForwardStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return fallback
	}
	descriptors := moderationcoverage.ForwardAdapterDescriptorsForRoute(routeMeta.Handler, routeMeta.Protocol)
	for _, descriptor := range descriptors {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIHTTP {
			continue
		}
		registry := h.forwardStageRegistry
		if registry == nil {
			registry = NewForwardStageRegistry()
			h.forwardStageRegistry = registry
		}
		if adapter, ok := registry.Resolve(descriptor); ok {
			return adapter
		}
		registry.Register(descriptor, fallback)
		if adapter, ok := registry.Resolve(descriptor); ok {
			return adapter
		}
	}
	return fallback
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
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableUsageStageWithContext(c, adapter),
	)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPScheduleResultStage(c *gin.Context, account *service.Account, success bool, firstTokenMs *int) openAIHTTPExecutableStageResult {
	return h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
		Handler:            h,
		Account:            account,
		ScheduleSuccess:    &success,
		ScheduleFirstToken: firstTokenMs,
	})
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPCyberUsageStage(c *gin.Context, input OpenAIHTTPCyberUsageStageInput) openAIHTTPExecutableStageResult {
	return h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
		Handler:            h,
		APIKey:             input.APIKey,
		Account:            input.Account,
		Subscription:       input.Subscription,
		LogModel:           input.Model,
		ForwardErrored:     input.ForwardErrored,
		CyberBlockKey:      input.CyberBlockKey,
		ChannelUsageFields: input.ChannelUsageFields,
		RequestPayloadHash: input.RequestPayloadHash,
	})
}

type OpenAIHTTPCyberUsageStageInput struct {
	APIKey             *service.APIKey
	Account            *service.Account
	Subscription       *service.UserSubscription
	Model              string
	ForwardErrored     bool
	CyberBlockKey      string
	ChannelUsageFields service.ChannelUsageFields
	RequestPayloadHash string
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
	ForwardErrored     bool
	CyberBlockKey      string
	ScheduleSuccess    *bool
	ScheduleFirstToken *int
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
	if s.ScheduleSuccess != nil && s.Account != nil {
		firstTokenMs := s.ScheduleFirstToken
		if firstTokenMs == nil && s.Result != nil {
			firstTokenMs = s.Result.FirstTokenMs
		}
		h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account.ID, *s.ScheduleSuccess, firstTokenMs)
	}
	h.recordCyberPolicyIfMarked(c, s.APIKey, s.Account, s.Subscription, s.LogModel, s.ForwardErrored, s.CyberBlockKey, s.ChannelUsageFields, s.RequestPayloadHash)
	if s.Result == nil {
		return ExecutableStageResult{}
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
	return newGatewayPipeline(
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		openAIHTTPExecutableStages(stages),
	)
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
	return newGatewayPipeline(
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		[]ExecutableStage{
			{Name: stage, Run: run},
		},
	).Run(c)
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketStage(c *gin.Context, adapter any) ExecutableStageResult {
	stage := ExecutableStage{}
	switch typed := adapter.(type) {
	case BillingStage:
		stage = executableBillingStageWithContext(c, typed)
	case RoutingStage:
		stage = executableRoutingStageWithContext(c, typed)
	case ForwardStage:
		stage = executableForwardStageWithContext(c, typed)
	case UsageStage:
		stage = executableUsageStageWithContext(c, typed)
	default:
		return ExecutableStageResult{}
	}
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.SourceOpenAIWebSocketExecutableStage,
		stage,
	)
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketBillingStage(c *gin.Context, adapter BillingStage) ExecutableStageResult {
	return h.runOpenAIWebSocketStage(c, adapter)
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
	return h.runOpenAIWebSocketStage(c, adapter)
}

type OpenAIWebSocketRoutingStage struct {
	Handler               *OpenAIGatewayHandler
	RequestContext        context.Context
	ReqLog                *zap.Logger
	APIKey                *service.APIKey
	SubjectUserID         int64
	RequestedModel        string
	SessionHash           string
	PreviousResponseID    string
	FailedAccountIDs      map[int64]struct{}
	RequestPlatform       string
	ClientConn            *coderws.Conn
	LastFailoverErr       *service.UpstreamFailoverError
	Account               **service.Account
	AccountMaxConcurrency *int
	CurrentAccountRelease *func()
	Token                 *string
	StickyPreviousHit     *bool
	ScheduleLayer         *string
}

func (OpenAIWebSocketRoutingStage) StageName() string {
	return moderationcoverage.StageRouting
}

func (s OpenAIWebSocketRoutingStage) RunRouting(c *gin.Context) ExecutableStageResult {
	h := s.Handler
	if h == nil || h.gatewayService == nil || s.APIKey == nil || s.Account == nil {
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
	failedAccountIDs := s.FailedAccountIDs
	if failedAccountIDs == nil {
		failedAccountIDs = map[int64]struct{}{}
	}
	reqLog.Debug("openai.websocket_account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
	selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
		ctx,
		s.APIKey.GroupID,
		s.PreviousResponseID,
		s.SessionHash,
		s.RequestedModel,
		failedAccountIDs,
		service.OpenAIUpstreamTransportResponsesWebsocketV2,
		service.OpenAIEndpointCapabilityChatCompletions,
		false,
		s.RequestPlatform,
		s.SubjectUserID,
	)
	if err != nil {
		reqLog.Warn("openai.websocket_account_select_failed", zap.Error(err), zap.Int("excluded_account_count", len(failedAccountIDs)))
		s.closeOpenAIWebSocketRoutingNoAccount(err)
		return ExecutableStageResult{Stop: true, Err: err}
	}
	if selection == nil || selection.Account == nil {
		s.closeOpenAIWebSocketRoutingNoAccount(nil)
		return ExecutableStageResult{Stop: true}
	}

	account := selection.Account
	accountMaxConcurrency := account.Concurrency
	if selection.WaitPlan != nil && selection.WaitPlan.MaxConcurrency > 0 {
		accountMaxConcurrency = selection.WaitPlan.MaxConcurrency
	}
	accountReleaseFunc := selection.ReleaseFunc
	if !selection.Acquired {
		if selection.WaitPlan == nil {
			closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
			return ExecutableStageResult{Stop: true}
		}
		fastReleaseFunc, fastAcquired, err := h.concurrencyHelper.TryAcquireAccountSlot(ctx, account.ID, selection.WaitPlan.MaxConcurrency)
		if err != nil {
			reqLog.Warn("openai.websocket_account_slot_acquire_failed", zap.Int64("account_id", account.ID), zap.Error(err))
			closeOpenAIClientWS(s.ClientConn, coderws.StatusInternalError, "failed to acquire account concurrency slot")
			return ExecutableStageResult{Stop: true, Err: err}
		}
		if !fastAcquired {
			closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "account is busy, please retry later")
			return ExecutableStageResult{Stop: true}
		}
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, s.RequestedModel, false, service.OpenAIEndpointCapabilityChatCompletions, "")
		if refreshErr != nil {
			if fastReleaseFunc != nil {
				fastReleaseFunc()
			}
			reqLog.Info("openai.websocket_selected_account_unavailable_before_use", zap.Int64("account_id", account.ID), zap.Error(refreshErr))
			closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "no available account")
			return ExecutableStageResult{Stop: true, Err: refreshErr}
		}
		selection.Account = refreshed
		account = refreshed
		accountReleaseFunc = fastReleaseFunc
	}
	if s.CurrentAccountRelease != nil {
		*s.CurrentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
	}
	if err := h.gatewayService.BindStickySession(ctx, s.APIKey.GroupID, s.SessionHash, account.ID); err != nil {
		reqLog.Warn("openai.websocket_bind_sticky_session_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
	if s.StickyPreviousHit != nil {
		*s.StickyPreviousHit = scheduleDecision.StickyPreviousHit
	}
	if s.ScheduleLayer != nil {
		*s.ScheduleLayer = scheduleDecision.Layer
	}

	token, _, tokenErr := h.gatewayService.GetAccessToken(ctx, account)
	if tokenErr != nil {
		reqLog.Warn("openai.websocket_get_access_token_failed", zap.Int64("account_id", account.ID), zap.Error(tokenErr))
		closeOpenAIClientWS(s.ClientConn, coderws.StatusInternalError, "failed to get access token")
		return ExecutableStageResult{Stop: true, Err: tokenErr}
	}

	reqLog.Debug("openai.websocket_account_selected",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
		zap.String("schedule_layer", scheduleDecision.Layer),
		zap.Int("candidate_count", scheduleDecision.CandidateCount),
	)
	*s.Account = account
	if s.AccountMaxConcurrency != nil {
		*s.AccountMaxConcurrency = accountMaxConcurrency
	}
	if s.Token != nil {
		*s.Token = token
	}
	return ExecutableStageResult{}
}

func (s OpenAIWebSocketRoutingStage) closeOpenAIWebSocketRoutingNoAccount(err error) {
	if s.LastFailoverErr != nil {
		closeOpenAIWSFailoverExhausted(s.ClientConn, s.LastFailoverErr)
		return
	}
	closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "no available account")
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketForwardStage(c *gin.Context, adapter ForwardStage) ExecutableStageResult {
	return h.runOpenAIWebSocketStage(c, adapter)
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
	ScheduleSuccess      *bool
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
		if s.ScheduleSuccess != nil && s.Account != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account.ID, *s.ScheduleSuccess, nil)
		}
		return ExecutableStageResult{}
	}
	if s.Account.Type == service.AccountTypeOAuth {
		h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(ctx, s.Account.ID, s.Result.ResponseHeaders)
	}
	scheduleSuccess := true
	if s.ScheduleSuccess != nil {
		scheduleSuccess = *s.ScheduleSuccess
	}
	h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account.ID, scheduleSuccess, s.Result.FirstTokenMs)
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
	return h.runOpenAIWebSocketStage(c, adapter)
}
