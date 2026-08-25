package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
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

var errModerationReceiptNotForwardable = errors.New("moderation receipt is missing or not forwardable")

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

type StageAdapterRegistry struct {
	forwardAdapters map[stageAdapterRegistryKey]ForwardStage
	billingAdapters map[stageAdapterRegistryKey]BillingStage
	routingAdapters map[stageAdapterRegistryKey]RoutingStage
	usageAdapters   map[stageAdapterRegistryKey]UsageStage
}

type ForwardStageRegistry = StageAdapterRegistry

type stageAdapterRegistryKey struct {
	Stage    string
	Pipeline string
	Name     string
}

type registeredForwardStage struct {
	stage   string
	adapter ForwardStage
}

type registeredBillingStage struct {
	stage   string
	adapter BillingStage
}

type registeredRoutingStage struct {
	stage   string
	adapter RoutingStage
}

type registeredUsageStage struct {
	stage   string
	adapter UsageStage
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

func NewStageAdapterRegistry() *StageAdapterRegistry {
	return &StageAdapterRegistry{
		forwardAdapters: map[stageAdapterRegistryKey]ForwardStage{},
		billingAdapters: map[stageAdapterRegistryKey]BillingStage{},
		routingAdapters: map[stageAdapterRegistryKey]RoutingStage{},
		usageAdapters:   map[stageAdapterRegistryKey]UsageStage{},
	}
}

func NewForwardStageRegistry() *ForwardStageRegistry {
	return NewStageAdapterRegistry()
}

func (r *StageAdapterRegistry) Register(descriptor moderationcoverage.RouteAdapterDescriptor, adapter ForwardStage) {
	r.RegisterForward(descriptor, adapter)
}

func (r *StageAdapterRegistry) RegisterForward(descriptor moderationcoverage.RouteAdapterDescriptor, adapter ForwardStage) {
	if r == nil || adapter == nil {
		return
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageForward || key.Pipeline == "" || key.Name == "" {
		return
	}
	if r.forwardAdapters == nil {
		r.forwardAdapters = map[stageAdapterRegistryKey]ForwardStage{}
	}
	r.forwardAdapters[key] = adapter
}

func (r *StageAdapterRegistry) RegisterBilling(descriptor moderationcoverage.RouteAdapterDescriptor, adapter BillingStage) {
	if r == nil || adapter == nil {
		return
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageBilling || key.Pipeline == "" || key.Name == "" {
		return
	}
	if r.billingAdapters == nil {
		r.billingAdapters = map[stageAdapterRegistryKey]BillingStage{}
	}
	r.billingAdapters[key] = adapter
}

func (r *StageAdapterRegistry) RegisterRouting(descriptor moderationcoverage.RouteAdapterDescriptor, adapter RoutingStage) {
	if r == nil || adapter == nil {
		return
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageRouting || key.Pipeline == "" || key.Name == "" {
		return
	}
	if r.routingAdapters == nil {
		r.routingAdapters = map[stageAdapterRegistryKey]RoutingStage{}
	}
	r.routingAdapters[key] = adapter
}

func (r *StageAdapterRegistry) RegisterUsage(descriptor moderationcoverage.RouteAdapterDescriptor, adapter UsageStage) {
	if r == nil || adapter == nil {
		return
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageUsage || key.Pipeline == "" || key.Name == "" {
		return
	}
	if r.usageAdapters == nil {
		r.usageAdapters = map[stageAdapterRegistryKey]UsageStage{}
	}
	r.usageAdapters[key] = adapter
}

func (r *StageAdapterRegistry) Resolve(descriptor moderationcoverage.RouteAdapterDescriptor) (ForwardStage, bool) {
	return r.ResolveForward(descriptor)
}

func (r *StageAdapterRegistry) ResolveForward(descriptor moderationcoverage.RouteAdapterDescriptor) (ForwardStage, bool) {
	if r == nil {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageForward || key.Pipeline == "" || key.Name == "" {
		return nil, false
	}
	adapter, ok := r.forwardAdapters[key]
	if !ok {
		return nil, false
	}
	return registeredForwardStage{stage: key.Stage, adapter: adapter}, true
}

func (r *StageAdapterRegistry) ResolveBilling(descriptor moderationcoverage.RouteAdapterDescriptor) (BillingStage, bool) {
	if r == nil {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageBilling || key.Pipeline == "" || key.Name == "" {
		return nil, false
	}
	adapter, ok := r.billingAdapters[key]
	if !ok {
		return nil, false
	}
	return registeredBillingStage{stage: key.Stage, adapter: adapter}, true
}

func (r *StageAdapterRegistry) ResolveRouting(descriptor moderationcoverage.RouteAdapterDescriptor) (RoutingStage, bool) {
	if r == nil {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageRouting || key.Pipeline == "" || key.Name == "" {
		return nil, false
	}
	adapter, ok := r.routingAdapters[key]
	if !ok {
		return nil, false
	}
	return registeredRoutingStage{stage: key.Stage, adapter: adapter}, true
}

func (r *StageAdapterRegistry) ResolveUsage(descriptor moderationcoverage.RouteAdapterDescriptor) (UsageStage, bool) {
	if r == nil {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	if key.Stage != moderationcoverage.StageUsage || key.Pipeline == "" || key.Name == "" {
		return nil, false
	}
	adapter, ok := r.usageAdapters[key]
	if !ok {
		return nil, false
	}
	return registeredUsageStage{stage: key.Stage, adapter: adapter}, true
}

func stageAdapterRegistryKeyFromDescriptor(descriptor moderationcoverage.RouteAdapterDescriptor) stageAdapterRegistryKey {
	return stageAdapterRegistryKey{
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

func blockedForwardStage(pipeline, message string) ForwardStage {
	pipeline = moderationcoverage.NormalizePipeline(pipeline)
	if message == "" {
		message = "pipeline forward stage is not registered"
	}
	return ForwardStageAdapter{
		Name: moderationcoverage.StageForward,
		Forward: func(*gin.Context) ExecutableStageResult {
			return ExecutableStageResult{
				Stop: true,
				Err:  fmt.Errorf("%s: %s", pipeline, message),
			}
		},
	}
}

func blockedBillingStage(pipeline, message string) BillingStage {
	pipeline = moderationcoverage.NormalizePipeline(pipeline)
	if message == "" {
		message = "pipeline billing stage is not registered"
	}
	return BillingStageAdapter{
		Name: moderationcoverage.StageBilling,
		Billing: func(*gin.Context) ExecutableStageResult {
			return ExecutableStageResult{
				Stop: true,
				Err:  fmt.Errorf("%s: %s", pipeline, message),
			}
		},
	}
}

func blockedRoutingStage(pipeline, message string) RoutingStage {
	pipeline = moderationcoverage.NormalizePipeline(pipeline)
	if message == "" {
		message = "pipeline routing stage is not registered"
	}
	return RoutingStageAdapter{
		Name: moderationcoverage.StageRouting,
		Routing: func(*gin.Context) ExecutableStageResult {
			return ExecutableStageResult{
				Stop: true,
				Err:  fmt.Errorf("%s: %s", pipeline, message),
			}
		},
	}
}

func blockedUsageStage(pipeline, message string) UsageStage {
	pipeline = moderationcoverage.NormalizePipeline(pipeline)
	if message == "" {
		message = "pipeline usage stage is not registered"
	}
	return UsageStageAdapter{
		Name: moderationcoverage.StageUsage,
		Usage: func(*gin.Context) ExecutableStageResult {
			return ExecutableStageResult{
				Stop: true,
				Err:  fmt.Errorf("%s: %s", pipeline, message),
			}
		},
	}
}

func stageAdapterDescriptorsForRuntimeRoute(routeMeta moderationcoverage.Entry) []moderationcoverage.RouteAdapterDescriptor {
	descriptors := moderationcoverage.NormalizeRouteAdapterDescriptors(routeMeta.StageAdapterDescriptors)
	if len(descriptors) > 0 {
		return descriptors
	}
	return moderationcoverage.StageAdapterDescriptorsForRoute(routeMeta.Handler, routeMeta.Protocol)
}

func stageAdapterImplementationName(adapter any) string {
	if adapter == nil {
		return ""
	}
	if named, ok := adapter.(interface{ StageName() string }); ok {
		name := strings.TrimSpace(named.StageName())
		switch name {
		case "", moderationcoverage.StageBilling, moderationcoverage.StageRouting, moderationcoverage.StageForward, moderationcoverage.StageUsage:
		default:
			return name
		}
	}
	t := reflect.TypeOf(adapter)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

func bindForwardStageAdapterForDescriptor(registry *StageAdapterRegistry, descriptor moderationcoverage.RouteAdapterDescriptor, fallback ForwardStage) (ForwardStage, bool) {
	if registry == nil {
		return nil, false
	}
	if adapter, ok := registry.ResolveForward(descriptor); ok {
		return adapter, true
	}
	if strings.TrimSpace(descriptor.Name) == "" || stageAdapterImplementationName(fallback) != strings.TrimSpace(descriptor.Name) {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	return registeredForwardStage{stage: key.Stage, adapter: fallback}, true
}

func bindBillingStageAdapterForDescriptor(registry *StageAdapterRegistry, descriptor moderationcoverage.RouteAdapterDescriptor, fallback BillingStage) (BillingStage, bool) {
	if registry == nil {
		return nil, false
	}
	if adapter, ok := registry.ResolveBilling(descriptor); ok {
		return adapter, true
	}
	if strings.TrimSpace(descriptor.Name) == "" || stageAdapterImplementationName(fallback) != strings.TrimSpace(descriptor.Name) {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	return registeredBillingStage{stage: key.Stage, adapter: fallback}, true
}

func bindRoutingStageAdapterForDescriptor(registry *StageAdapterRegistry, descriptor moderationcoverage.RouteAdapterDescriptor, fallback RoutingStage) (RoutingStage, bool) {
	if registry == nil {
		return nil, false
	}
	if adapter, ok := registry.ResolveRouting(descriptor); ok {
		return adapter, true
	}
	if strings.TrimSpace(descriptor.Name) == "" || stageAdapterImplementationName(fallback) != strings.TrimSpace(descriptor.Name) {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	return registeredRoutingStage{stage: key.Stage, adapter: fallback}, true
}

func bindUsageStageAdapterForDescriptor(registry *StageAdapterRegistry, descriptor moderationcoverage.RouteAdapterDescriptor, fallback UsageStage) (UsageStage, bool) {
	if registry == nil {
		return nil, false
	}
	if adapter, ok := registry.ResolveUsage(descriptor); ok {
		return adapter, true
	}
	if strings.TrimSpace(descriptor.Name) == "" || stageAdapterImplementationName(fallback) != strings.TrimSpace(descriptor.Name) {
		return nil, false
	}
	key := stageAdapterRegistryKeyFromDescriptor(descriptor)
	return registeredUsageStage{stage: key.Stage, adapter: fallback}, true
}

func (s registeredBillingStage) StageName() string {
	if s.stage == "" {
		return moderationcoverage.StageBilling
	}
	return s.stage
}

func (s registeredBillingStage) RunBilling(c *gin.Context) ExecutableStageResult {
	if s.adapter == nil {
		return ExecutableStageResult{}
	}
	return s.adapter.RunBilling(c)
}

func (s registeredRoutingStage) StageName() string {
	if s.stage == "" {
		return moderationcoverage.StageRouting
	}
	return s.stage
}

func (s registeredRoutingStage) RunRouting(c *gin.Context) ExecutableStageResult {
	if s.adapter == nil {
		return ExecutableStageResult{}
	}
	return s.adapter.RunRouting(c)
}

func (s registeredUsageStage) StageName() string {
	if s.stage == "" {
		return moderationcoverage.StageUsage
	}
	return s.stage
}

func (s registeredUsageStage) RunUsage(c *gin.Context) ExecutableStageResult {
	if s.adapter == nil {
		return ExecutableStageResult{}
	}
	return s.adapter.RunUsage(c)
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
			if moderationReceiptRequiredAndMissing(c) {
				recordContentModerationForwardConflict(c)
				return ExecutableStageResult{Stop: true, Err: errModerationReceiptNotForwardable}
			}
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
			if moderationReceiptRequiredAndMissing(c) {
				recordContentModerationForwardConflict(c)
				return ExecutableStageResult{Stop: true, Err: errModerationReceiptNotForwardable}
			}
			return adapter.RunForward(c)
		},
	}
}

func moderationReceiptRequiredAndMissing(c *gin.Context) bool {
	meta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok || !meta.Upstream || !meta.ModerationRequired {
		return false
	}
	return !moderationcoverage.ModerationReceiptAllowsForward(c)
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
	adapter = h.openAIHTTPBillingStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableBillingStageWithContext(c, adapter),
	)
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPRoutingStage(c *gin.Context, adapter RoutingStage) openAIHTTPExecutableStageResult {
	adapter = h.openAIHTTPRoutingStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableRoutingStageWithContext(c, adapter),
	)
}

func (h *OpenAIGatewayHandler) openAIHTTPStageAdapterRegistry() *StageAdapterRegistry {
	if h == nil {
		return nil
	}
	registry := h.stageAdapterRegistry
	if registry == nil {
		registry = h.forwardStageRegistry
	}
	if registry == nil {
		registry = NewStageAdapterRegistry()
		h.stageAdapterRegistry = registry
	}
	return registry
}

func (h *OpenAIGatewayHandler) openAIHTTPBillingStageFromRouteDescriptor(c *gin.Context, fallback BillingStage) BillingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedBillingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline route metadata is required before billing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIHTTP ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageBilling {
			continue
		}
		found = true
		if adapter, ok := bindBillingStageAdapterForDescriptor(h.openAIHTTPStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedBillingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline billing stage descriptor is required before billing")
	}
	return blockedBillingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline billing stage adapter is not bound by route descriptor")
}

func (h *OpenAIGatewayHandler) openAIHTTPRoutingStageFromRouteDescriptor(c *gin.Context, fallback RoutingStage) RoutingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedRoutingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline route metadata is required before routing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIHTTP ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageRouting {
			continue
		}
		found = true
		if adapter, ok := bindRoutingStageAdapterForDescriptor(h.openAIHTTPStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedRoutingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline routing stage descriptor is required before routing")
	}
	return blockedRoutingStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline routing stage adapter is not bound by route descriptor")
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
	PreviousResponseCanMove    bool
	UseUpstreamTokenCost       bool
	RequestPlatform            string
	Stream                     bool
	StreamStarted              *bool
	MaxAccountSwitches         int
	SwitchCount                *int
	LastFailoverErr            *service.UpstreamFailoverError
	ProfitVetoCount            *int
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
	var selection *service.AccountSelectionResult
	var scheduleDecision service.OpenAIAccountScheduleDecision
	var err error
	if s.RequiredImageCapability != "" && s.RequiredCapability == "" {
		selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForImages(
			ctx,
			s.APIKey.GroupID,
			sessionHash,
			s.RequestedModel,
			failedAccountIDs,
			s.RequiredImageCapability,
			s.SubjectUserID,
		)
	} else {
		selection, scheduleDecision, err = h.gatewayService.SelectAccountWithSchedulerForCapabilityAndImage(
			ctx,
			s.APIKey.GroupID,
			s.PreviousResponseID,
			sessionHash,
			s.RequestedModel,
			failedAccountIDs,
			s.RequiredTransport,
			s.RequiredCapability,
			s.RequiredImageCapability,
			s.RequireCompact,
			s.PreviousResponseCanMove,
			s.UseUpstreamTokenCost,
			s.RequestPlatform,
			s.SubjectUserID,
		)
	}
	if err != nil {
		fields := append(openAIAccountScheduleDecisionLogFields(scheduleDecision),
			zap.Error(openAICompatibleSelectionErrorForLog(err, s.RequestPlatform)),
			zap.Int("excluded_account_count", len(failedAccountIDs)),
		)
		reqLog.Warn(logPrefix+".account_select_failed", fields...)
		return s.handleOpenAIHTTPRoutingSelectionError(c, err, len(failedAccountIDs) == 0, s.RequestedModel, displayModel)
	}
	if selection == nil || selection.Account == nil {
		s.handleOpenAIHTTPRoutingNoAccount(c, s.RequestedModel, displayModel)
		return ExecutableStageResult{Stop: true}
	}
	if s.PreviousResponseID != "" {
		reqLog.Debug(logPrefix+".account_selected_with_previous_response_id", zap.Int64("account_id", selection.Account.ID))
	}
	if scheduleDecision != (service.OpenAIAccountScheduleDecision{}) {
		reqLog.Debug(logPrefix+".account_schedule_decision", openAIAccountScheduleDecisionLogFields(scheduleDecision)...)
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
	releaseFunc, refreshedAccount, acquired, retryReason := h.acquireResponsesAccountSlotForRequest(c, s.APIKey.GroupID, sessionHash, selection, s.RequestedModel, false, s.RequiredCapability, s.RequiredImageCapability, s.Stream, streamStartedPtr, reqLog)
	if !acquired {
		if retryReason == openAISlotRetryProfitVeto && refreshedAccount != nil {
			vetoCount := 0
			if s.ProfitVetoCount != nil {
				vetoCount = *s.ProfitVetoCount
			}
			if !recordOpenAIProfitVeto(failedAccountIDs, refreshedAccount.ID, &vetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, *streamStartedPtr, reqLog, vetoCount)
				return ExecutableStageResult{Stop: true}
			}
			if s.ProfitVetoCount != nil {
				*s.ProfitVetoCount = vetoCount
			}
			if s.Retry != nil {
				*s.Retry = true
			}
			return ExecutableStageResult{}
		}
		if retryReason == openAISlotRetryAccountUnavailable && refreshedAccount != nil {
			failedAccountIDs[refreshedAccount.ID] = struct{}{}
			if s.Retry != nil {
				*s.Retry = true
			}
			return ExecutableStageResult{}
		}
		if retryReason == openAISlotRetryCapacity {
			s.writeOpenAIHTTPRoutingError(c, http.StatusTooManyRequests, "rate_limit_error", "Too many concurrent requests, please retry later")
		}
		return ExecutableStageResult{Stop: true}
	}
	if value, ok := c.Get(openAIHTTPPreForwardRequestContextKey); ok {
		if request, requestOK := value.(openAIHTTPPreForwardRequest); requestOK {
			subject, _ := middleware2.GetAuthSubjectFromContext(c)
			gate := runSelectedAccountContentModeration(c, reqLog, h.contentModerationService, s.APIKey, subject, request.Protocol, request.Model, request.contentModerationBody(), refreshedAccount)
			if gate != nil && gate.Decision != nil && gate.Decision.Blocked {
				if releaseFunc != nil {
					releaseFunc()
				}
				format := openAIHTTPModerationErrorOpenAI
				if request.Protocol == service.ContentModerationProtocolOpenAIMessages {
					format = openAIHTTPModerationErrorAnthropic
				}
				h.writeOpenAIHTTPModerationError(c, format, gate.Decision)
				return ExecutableStageResult{Stop: true}
			}
		}
	}
	if s.AccountReleaseFunc != nil {
		*s.AccountReleaseFunc = releaseFunc
	}
	*s.Account = refreshedAccount
	return ExecutableStageResult{}
}

func openAIAccountScheduleDecisionLogFields(decision service.OpenAIAccountScheduleDecision) []zap.Field {
	return []zap.Field{
		zap.String("layer", decision.Layer),
		zap.Bool("sticky_previous_hit", decision.StickyPreviousHit),
		zap.Bool("sticky_session_hit", decision.StickySessionHit),
		zap.String("model", decision.RequestedModel),
		zap.String("platform", decision.Platform),
		zap.String("required_transport", string(decision.RequiredTransport)),
		zap.String("required_capability", string(decision.RequiredCapability)),
		zap.String("required_image_capability", string(decision.RequiredImageCapability)),
		zap.Bool("require_compact", decision.RequireCompact),
		zap.String("snapshot_version", decision.SnapshotVersion),
		zap.Int("snapshot_candidate_count", decision.SnapshotCandidateCount),
		zap.Int("db_candidate_count", decision.DBCandidateCount),
		zap.Int("candidate_count", decision.CandidateCount),
		zap.Int("top_k", decision.TopK),
		zap.Int64("latency_ms", decision.LatencyMs),
		zap.Float64("load_skew", decision.LoadSkew),
		zap.Int64("selected_account_id", decision.SelectedAccountID),
		zap.String("selected_account_type", decision.SelectedAccountType),
		zap.Int("filtered_by_model_count", decision.FilteredByModelCount),
		zap.Int("filtered_by_schedulable_count", decision.FilteredBySchedulableCount),
		zap.Int("filtered_by_runtime_status_count", decision.FilteredByRuntimeStatusCount),
		zap.Int("filtered_by_user_cooldown_count", decision.FilteredByUserCooldownCount),
		zap.Int("filtered_by_transport_count", decision.FilteredByTransportCount),
		zap.Int("filtered_by_capability_count", decision.FilteredByCapabilityCount),
		zap.Int("filtered_by_concurrency_count", decision.FilteredByConcurrencyCount),
	}
}

func (s OpenAIHTTPRoutingStage) handleOpenAIHTTPRoutingSelectionError(c *gin.Context, err error, firstAttempt bool, requestedModel, displayModel string) ExecutableStageResult {
	if firstAttempt {
		if errors.Is(err, service.ErrNoAllowedCodexAccounts) {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			s.writeOpenAIHTTPRoutingError(c, http.StatusForbidden, "forbidden_error", service.CodexOfficialClientsOnlyMessage)
			return ExecutableStageResult{Stop: true, Err: err}
		}
		if errors.Is(err, service.ErrNoAvailableCompactAccounts) {
			markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
			s.writeOpenAIHTTPRoutingError(c, http.StatusServiceUnavailable, "compact_not_supported", "No available OpenAI accounts support /responses/compact")
			return ExecutableStageResult{Stop: true, Err: err}
		}
		cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, s.Handler.gatewayService, s.APIKey, requestedModel, displayModel)
		cls = classifySelectionFailureError(err, cls)
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

func (s OpenAIHTTPRoutingStage) handleOpenAIHTTPRoutingNoAccount(c *gin.Context, requestedModel, displayModel string) {
	cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, s.Handler.gatewayService, s.APIKey, requestedModel, displayModel)
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
		status, code, message, retryAfter := billingErrorDetailsForContext(c, err)
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
	adapter, releaseFunc := detachOpenAIHTTPForwardRelease(adapter)
	adapter = h.openAIHTTPForwardStageFromRouteDescriptor(c, adapter)
	if releaseFunc != nil {
		adapter = forwardStageWithRelease{stage: adapter, release: releaseFunc}
	}
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableForwardStageWithContext(c, adapter),
	)
}

func detachOpenAIHTTPForwardRelease(adapter ForwardStage) (ForwardStage, func()) {
	switch stage := adapter.(type) {
	case OpenAIHTTPForwardStage:
		release := stage.ReleaseFunc
		stage.ReleaseFunc = nil
		return stage, release
	case *OpenAIHTTPForwardStage:
		if stage == nil {
			return adapter, nil
		}
		copyStage := *stage
		release := copyStage.ReleaseFunc
		copyStage.ReleaseFunc = nil
		return copyStage, release
	default:
		return adapter, nil
	}
}

type forwardStageWithRelease struct {
	stage   ForwardStage
	release func()
}

func (s forwardStageWithRelease) StageName() string {
	if s.stage == nil {
		return moderationcoverage.StageForward
	}
	return s.stage.StageName()
}

func (s forwardStageWithRelease) RunForward(c *gin.Context) ExecutableStageResult {
	defer func() {
		if s.release != nil {
			s.release()
		}
	}()
	if s.stage == nil {
		return ExecutableStageResult{}
	}
	return s.stage.RunForward(c)
}

func (h *OpenAIGatewayHandler) openAIHTTPForwardStageFromRouteDescriptor(c *gin.Context, fallback ForwardStage) ForwardStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedForwardStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline route metadata is required before forward")
	}
	descriptors := stageAdapterDescriptorsForRuntimeRoute(routeMeta)
	found := false
	for _, descriptor := range descriptors {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIHTTP ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageForward {
			continue
		}
		found = true
		if adapter, ok := bindForwardStageAdapterForDescriptor(h.openAIHTTPStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedForwardStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline forward stage descriptor is required before forward")
	}
	return blockedForwardStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline forward stage adapter is not bound by route descriptor")
}

type OpenAIHTTPForwardKind string

const (
	OpenAIHTTPForwardResponses       OpenAIHTTPForwardKind = "responses"
	OpenAIHTTPForwardChatCompletions OpenAIHTTPForwardKind = "chat_completions"
	OpenAIHTTPForwardMessages        OpenAIHTTPForwardKind = "messages"
	OpenAIHTTPForwardImages          OpenAIHTTPForwardKind = "images"
	OpenAIHTTPForwardEmbeddings      OpenAIHTTPForwardKind = "embeddings"
	OpenAIHTTPForwardAlphaSearch     OpenAIHTTPForwardKind = "alpha_search"
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
		if s.Kind == OpenAIHTTPForwardImages {
			*s.WriterSizeBeforeForward = service.OpenAIImagesJSONKeepaliveAdjustedWrittenSize(c)
		} else {
			*s.WriterSizeBeforeForward = service.OpenAICompactKeepaliveAdjustedWrittenSize(c)
		}
	}
	defer func() {
		if s.ReleaseFunc != nil {
			s.ReleaseFunc()
		}
	}()

	var result *service.OpenAIForwardResult
	var err error
	bindRequestedReasoningEffort(c, s.Body, "")
	switch s.Kind {
	case OpenAIHTTPForwardChatCompletions:
		result, err = s.GatewayService.ForwardAsChatCompletions(ctx, c, s.Account, s.Body, s.PromptCacheKey, s.DefaultMappedModel)
	case OpenAIHTTPForwardMessages:
		result, err = s.GatewayService.ForwardAsAnthropic(ctx, c, s.Account, s.Body, s.PromptCacheKey, s.DefaultMappedModel)
	case OpenAIHTTPForwardImages:
		result, err = s.GatewayService.ForwardImages(ctx, c, s.Account, s.Body, s.ParsedImagesRequest, s.ChannelMappedModel)
	case OpenAIHTTPForwardEmbeddings:
		result, err = s.GatewayService.ForwardEmbeddings(ctx, c, s.Account, s.Body, s.DefaultMappedModel)
	case OpenAIHTTPForwardAlphaSearch:
		result, err = s.GatewayService.ForwardAlphaSearch(ctx, c, s.Account, s.Body)
	default:
		result, err = s.GatewayService.Forward(ctx, c, s.Account, s.Body)
	}
	if s.Result != nil {
		*s.Result = result
	}
	return ExecutableStageResult{Err: err}
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPUsageStage(c *gin.Context, adapter UsageStage) openAIHTTPExecutableStageResult {
	adapter = h.openAIHTTPUsageStageFromRouteDescriptor(c, adapter)
	return runGatewayPipelineStage(c,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.SourceOpenAIHTTPExecutableStage,
		executableUsageStageWithContext(c, adapter),
	)
}

// runOpenAIHTTPFailedUsageStage records only failed forwarding attempts that
// carry explicit, positive upstream usage. It deliberately uses the mandatory
// submit path because a retry may start immediately after this call.
func (h *OpenAIGatewayHandler) runOpenAIHTTPFailedUsageStage(c *gin.Context, stage OpenAIHTTPUsageStage) bool {
	if stage.Result == nil || !stage.Result.HasBillableUsage() || service.GetOpsCyberPolicy(c) != nil {
		return false
	}
	stage.Source = service.UsageSourceFailedUpstream
	stage.ForwardErrored = true
	stage.Mandatory = true
	h.runOpenAIHTTPUsageStage(c, stage)
	return true
}

func (h *OpenAIGatewayHandler) openAIHTTPUsageStageFromRouteDescriptor(c *gin.Context, fallback UsageStage) UsageStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedUsageStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline route metadata is required before usage")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIHTTP ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageUsage {
			continue
		}
		found = true
		if adapter, ok := bindUsageStageAdapterForDescriptor(h.openAIHTTPStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedUsageStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline usage stage descriptor is required before usage")
	}
	return blockedUsageStage(moderationcoverage.PipelineOpenAIHTTP, "pipeline usage stage adapter is not bound by route descriptor")
}

func (h *OpenAIGatewayHandler) runOpenAIHTTPScheduleResultStage(
	c *gin.Context,
	account *service.Account,
	forwardModel string,
	requireCompact bool,
	result *service.OpenAIForwardResult,
	success bool,
	firstTokenMs *int,
	observedErr ...error,
) openAIHTTPExecutableStageResult {
	var scheduleErr error
	if len(observedErr) > 0 {
		scheduleErr = observedErr[0]
	}
	return h.runOpenAIHTTPUsageStage(c, OpenAIHTTPUsageStage{
		Handler:                h,
		Result:                 result,
		Account:                account,
		ScheduleModel:          forwardModel,
		ScheduleRequireCompact: requireCompact,
		ScheduleSuccess:        &success,
		ScheduleFirstToken:     firstTokenMs,
		ScheduleObservedErr:    scheduleErr,
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
		RequestBody:        input.RequestBody,
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
	RequestBody        []byte
}

type OpenAIHTTPUsageStage struct {
	Handler                *OpenAIGatewayHandler
	RequestContext         context.Context
	Result                 *service.OpenAIForwardResult
	Source                 service.UsageSource
	APIKey                 *service.APIKey
	Account                *service.Account
	Subscription           *service.UserSubscription
	InboundEndpoint        string
	UpstreamEndpoint       string
	UserAgent              string
	ClientIP               string
	SessionID              string
	RequestPayloadHash     string
	RequestBody            []byte
	QuotaPlatform          string
	ChannelUsageFields     service.ChannelUsageFields
	CyberBlocked           bool
	ForwardErrored         bool
	CyberBlockKey          string
	ScheduleSuccess        *bool
	ScheduleModel          string
	ScheduleRequireCompact bool
	ScheduleFirstToken     *int
	ScheduleObservedErr    error
	Mandatory              bool
	LogComponent           string
	LogMessage             string
	LogUserID              int64
	LogModel               string
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
	stampOpenAIRequestedReasoningEffort(s.Result, c)
	phaseLatency := service.UsagePhaseLatencySnapshot(c)
	if s.ScheduleSuccess != nil && s.Account != nil {
		scheduleSuccess := *s.ScheduleSuccess
		if scheduleSuccess && s.Result != nil {
			scheduleSuccess = s.Result.SucceededForScheduling()
		}
		firstTokenMs := s.ScheduleFirstToken
		if firstTokenMs == nil && s.Result != nil {
			firstTokenMs = s.Result.FirstTokenMs
		}
		model := strings.TrimSpace(s.ScheduleModel)
		if model == "" {
			model = s.LogModel
		}
		model = openAIAccountScheduleModel(c, s.Account, model, s.ScheduleRequireCompact, s.Result)
		h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account, model, scheduleSuccess, firstTokenMs, s.ScheduleObservedErr)
	}
	failedUpstreamUsage := s.Source.Normalize() == service.UsageSourceFailedUpstream
	if failedUpstreamUsage {
		// Cyber failures have their own usage recorder. Recording both paths would
		// charge the same upstream failure twice with different token breakdowns.
		if service.GetOpsCyberPolicy(c) != nil || s.Result == nil || !s.Result.HasBillableUsage() {
			return ExecutableStageResult{}
		}
		s.Mandatory = true
	} else {
		h.recordCyberPolicyIfMarked(c, s.APIKey, s.Account, s.Subscription, s.LogModel, s.ForwardErrored, s.CyberBlockKey, s.ChannelUsageFields, s.RequestPayloadHash, s.RequestBody)
	}
	if s.Result == nil {
		return ExecutableStageResult{}
	}
	quotaPlatform := s.QuotaPlatform
	if quotaPlatform == "" {
		quotaPlatform = service.QuotaPlatform(c.Request.Context(), s.APIKey)
	}
	record := func(taskCtx context.Context) {
		if err := h.gatewayService.RecordUsage(taskCtx, &service.OpenAIRecordUsageInput{
			Result:             s.Result,
			Source:             s.Source,
			APIKey:             s.APIKey,
			User:               s.APIKey.User,
			Account:            s.Account,
			Subscription:       s.Subscription,
			InboundEndpoint:    s.InboundEndpoint,
			UpstreamEndpoint:   s.UpstreamEndpoint,
			UserAgent:          s.UserAgent,
			IPAddress:          s.ClientIP,
			SessionID:          s.SessionID,
			RequestPayloadHash: s.RequestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			PricingAt:          service.OpenAIPricingAtFromContext(ctx),
			PhaseLatency:       phaseLatency,
			ChannelUsageFields: s.ChannelUsageFields,
			CyberBlocked:       s.CyberBlocked,
			NativeCompactionV2: service.IsOpenAINativeCompactionV2(c),
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
		typed = h.openAIWebSocketBillingStageFromRouteDescriptor(c, typed)
		stage = executableBillingStageWithContext(c, typed)
	case RoutingStage:
		typed = h.openAIWebSocketRoutingStageFromRouteDescriptor(c, typed)
		stage = executableRoutingStageWithContext(c, typed)
	case ForwardStage:
		typed = h.openAIWebSocketForwardStageFromRouteDescriptor(c, typed)
		stage = executableForwardStageWithContext(c, typed)
	case UsageStage:
		typed = h.openAIWebSocketUsageStageFromRouteDescriptor(c, typed)
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

func (h *OpenAIGatewayHandler) openAIWebSocketStageAdapterRegistry() *StageAdapterRegistry {
	if h == nil {
		return nil
	}
	registry := h.stageAdapterRegistry
	if registry == nil {
		registry = h.forwardStageRegistry
	}
	if registry == nil {
		registry = NewStageAdapterRegistry()
		h.stageAdapterRegistry = registry
	}
	return registry
}

func (h *OpenAIGatewayHandler) openAIWebSocketBillingStageFromRouteDescriptor(c *gin.Context, fallback BillingStage) BillingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedBillingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline route metadata is required before billing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIWebSocket ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageBilling {
			continue
		}
		found = true
		if adapter, ok := bindBillingStageAdapterForDescriptor(h.openAIWebSocketStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedBillingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline billing stage descriptor is required before billing")
	}
	return blockedBillingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline billing stage adapter is not bound by route descriptor")
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

func (h *OpenAIGatewayHandler) openAIWebSocketRoutingStageFromRouteDescriptor(c *gin.Context, fallback RoutingStage) RoutingStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedRoutingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline route metadata is required before routing")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIWebSocket ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageRouting {
			continue
		}
		found = true
		if adapter, ok := bindRoutingStageAdapterForDescriptor(h.openAIWebSocketStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedRoutingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline routing stage descriptor is required before routing")
	}
	return blockedRoutingStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline routing stage adapter is not bound by route descriptor")
}

type OpenAIWebSocketRoutingStage struct {
	Handler                 *OpenAIGatewayHandler
	RequestContext          context.Context
	ReqLog                  *zap.Logger
	APIKey                  *service.APIKey
	SubjectUserID           int64
	RequestedModel          string
	SessionHash             string
	PreviousResponseID      string
	FailedAccountIDs        map[int64]struct{}
	RequiredTransport       service.OpenAIUpstreamTransport
	RequiredCapability      service.OpenAIEndpointCapability
	UseUpstreamTokenCost    bool
	PreviousResponseCanMove bool
	RequestPlatform         string
	ClientConn              *coderws.Conn
	LastFailoverErr         *service.UpstreamFailoverError
	HandleFailover          func(*service.Account, *service.UpstreamFailoverError) bool
	ProfitVetoCount         *int
	Retry                   *bool
	AdmittedContext         *context.Context
	Account                 **service.Account
	AccountMaxConcurrency   *int
	CurrentAccountRelease   *func()
	Token                   *string
	StickyPreviousHit       *bool
	ScheduleLayer           *string
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
	requiredTransport := s.RequiredTransport
	if requiredTransport == "" {
		requiredTransport = service.OpenAIUpstreamTransportResponsesWebsocketV2
	}
	requiredCapability := s.RequiredCapability
	if requiredCapability == "" {
		requiredCapability = service.OpenAIEndpointCapabilityChatCompletions
	}
	reqLog.Debug("openai.websocket_account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
	selection, scheduleDecision, err := h.gatewayService.SelectAccountWithSchedulerForCapability(
		ctx,
		s.APIKey.GroupID,
		s.PreviousResponseID,
		s.SessionHash,
		s.RequestedModel,
		failedAccountIDs,
		requiredTransport,
		requiredCapability,
		false,
		s.PreviousResponseCanMove,
		s.UseUpstreamTokenCost,
		s.RequestPlatform,
		s.SubjectUserID,
	)
	if err != nil {
		reqLog.Warn("openai.websocket_account_select_failed", zap.Error(openAICompatibleSelectionErrorForLog(err, s.RequestPlatform)), zap.Int("excluded_account_count", len(failedAccountIDs)))
		s.closeOpenAIWebSocketRoutingNoAccount(c, err)
		return ExecutableStageResult{Stop: true, Err: err}
	}
	if selection == nil || selection.Account == nil {
		s.closeOpenAIWebSocketRoutingNoAccount(c, nil)
		return ExecutableStageResult{Stop: true}
	}

	account := selection.Account
	accountMaxConcurrency := account.Concurrency
	if selection.WaitPlan != nil && selection.WaitPlan.MaxConcurrency > 0 {
		accountMaxConcurrency = selection.WaitPlan.MaxConcurrency
	}
	admissionCtx := service.ContextWithSelectionProfitGate(ctx, selection)
	accountReleaseFunc := selection.ReleaseFunc
	if selection.Acquired {
		latest, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(admissionCtx, account)
		if vetoed {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			reqLog.Debug("openai.websocket_account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			return s.retryAfterProfitVeto(account)
		}
		account = latest
		selection.Account = latest
	}
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
		refreshed, refreshErr := h.gatewayService.RefreshSelectedAccountBeforeUse(ctx, account, s.RequestedModel, false, requiredCapability, "")
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
		latest, vetoed, reason := h.gatewayService.ProfitControlVetoLatest(admissionCtx, account)
		if vetoed {
			if fastReleaseFunc != nil {
				fastReleaseFunc()
			}
			reqLog.Debug("openai.websocket_account_slot_profit_vetoed", zap.Int64("account_id", account.ID), zap.String("reason", reason))
			return s.retryAfterProfitVeto(account)
		}
		account = latest
		selection.Account = latest
	}
	ctx = admissionCtx
	if s.AdmittedContext != nil {
		*s.AdmittedContext = ctx
	}
	if s.CurrentAccountRelease != nil {
		*s.CurrentAccountRelease = wrapReleaseOnDone(ctx, accountReleaseFunc)
	}
	if err := h.gatewayService.BindStickySessionAfterProfitAdmission(ctx, s.APIKey.GroupID, s.SessionHash, account.ID); err != nil {
		reqLog.Warn("openai.websocket_bind_sticky_session_after_profit_admission_failed", zap.Int64("account_id", account.ID), zap.Error(err))
	}
	if s.StickyPreviousHit != nil {
		*s.StickyPreviousHit = scheduleDecision.StickyPreviousHit
	}
	if s.ScheduleLayer != nil {
		*s.ScheduleLayer = scheduleDecision.Layer
	}

	token, _, tokenErr := h.gatewayService.GetRequestCredential(ctx, c, account)
	if tokenErr != nil {
		reqLog.Warn("openai.websocket_get_access_token_failed", zap.Int64("account_id", account.ID), zap.Error(tokenErr))
		var failoverErr *service.UpstreamFailoverError
		if errors.As(tokenErr, &failoverErr) && s.HandleFailover != nil {
			if s.HandleFailover(account, failoverErr) {
				if s.Retry != nil {
					*s.Retry = true
				}
				return ExecutableStageResult{}
			}
			return ExecutableStageResult{Stop: true, Err: tokenErr}
		}
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

func (s OpenAIWebSocketRoutingStage) retryAfterProfitVeto(account *service.Account) ExecutableStageResult {
	count := 0
	if s.ProfitVetoCount != nil {
		count = *s.ProfitVetoCount
	}
	if !recordOpenAIProfitVeto(s.FailedAccountIDs, account.ID, &count) {
		closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "no available account")
		return ExecutableStageResult{Stop: true}
	}
	if s.ProfitVetoCount != nil {
		*s.ProfitVetoCount = count
	}
	if s.Retry != nil {
		*s.Retry = true
	}
	return ExecutableStageResult{}
}

func (s OpenAIWebSocketRoutingStage) closeOpenAIWebSocketRoutingNoAccount(c *gin.Context, err error) {
	if errors.Is(err, service.ErrNoAllowedCodexAccounts) {
		closeOpenAIClientWS(s.ClientConn, coderws.StatusPolicyViolation, service.CodexOfficialClientsOnlyMessage)
		return
	}
	if s.LastFailoverErr != nil {
		closeOpenAIWSFailoverExhausted(c, s.ClientConn, s.LastFailoverErr)
		return
	}
	closeOpenAIClientWS(s.ClientConn, coderws.StatusTryAgainLater, "no available account")
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketForwardStage(c *gin.Context, adapter ForwardStage) ExecutableStageResult {
	return h.runOpenAIWebSocketStage(c, adapter)
}

func (h *OpenAIGatewayHandler) openAIWebSocketForwardStageFromRouteDescriptor(c *gin.Context, fallback ForwardStage) ForwardStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedForwardStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline route metadata is required before forward")
	}
	descriptors := stageAdapterDescriptorsForRuntimeRoute(routeMeta)
	found := false
	for _, descriptor := range descriptors {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIWebSocket ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageForward {
			continue
		}
		found = true
		if adapter, ok := bindForwardStageAdapterForDescriptor(h.openAIWebSocketStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedForwardStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline forward stage descriptor is required before forward")
	}
	return blockedForwardStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline forward stage adapter is not bound by route descriptor")
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
	UpstreamModel        string
	TurnErr              error
	Result               *service.OpenAIForwardResult
	CyberBlockKey        string
	CyberBlockBody       []byte
	ChannelMapping       service.ChannelMappingResult
	RequestPayloadHash   string
	QuotaPlatform        string
	ReleaseTurnSlots     func()
	CyberBlockedThisConn *bool
	ScheduleSuccess      *bool
	UserAgent            string
	ClientIP             string
	SessionID            string
	PricingAt            time.Time
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
	phaseLatency := service.UsagePhaseLatencySnapshot(c)
	reqLog := s.ReqLog
	if reqLog == nil {
		reqLog = zap.NewNop()
	}

	upstreamModel := strings.TrimSpace(s.UpstreamModel)
	if upstreamModel == "" && s.Result != nil {
		upstreamModel = strings.TrimSpace(s.Result.UpstreamModel)
	}
	h.recordCyberPolicyIfMarked(c, s.APIKey, s.Account, s.Subscription, s.Model, s.TurnErr != nil, s.CyberBlockKey, s.ChannelMapping.ToUsageFields(s.Model, upstreamModel), s.RequestPayloadHash, s.CyberBlockBody)
	if service.GetOpsCyberPolicy(c) != nil && s.CyberBlockedThisConn != nil {
		*s.CyberBlockedThisConn = true
	}
	if s.TurnErr != nil {
		if s.Result == nil || !s.Result.HasBillableUsage() {
			return ExecutableStageResult{}
		}
		// Cyber-hit usage is already written by recordCyberPolicyIfMarked(forwardErrored=true).
		if service.GetOpsCyberPolicy(c) != nil {
			return ExecutableStageResult{}
		}
		reqLog.Warn("openai.websocket_failed_with_billable_usage",
			zap.Int64("account_id", s.Account.ID),
			zap.Int("input_tokens", s.Result.Usage.InputTokens),
			zap.Int("output_tokens", s.Result.Usage.OutputTokens),
			zap.Int("image_count", s.Result.ImageCount),
			zap.Error(s.TurnErr),
		)
	}
	if s.Result == nil {
		if s.ScheduleSuccess != nil && s.Account != nil {
			h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account, openAIAccountScheduleModel(c, s.Account, s.Model, false, nil), *s.ScheduleSuccess, nil, s.TurnErr)
		}
		return ExecutableStageResult{}
	}
	if s.Account.Type == service.AccountTypeOAuth && !s.Account.IsShadow() {
		h.gatewayService.UpdateCodexUsageSnapshotFromHeaders(ctx, s.Account.ID, s.Result.ResponseHeaders)
	}
	scheduleSuccess := true
	if s.ScheduleSuccess != nil {
		scheduleSuccess = *s.ScheduleSuccess
	}
	if scheduleSuccess {
		scheduleSuccess = s.Result.SucceededForScheduling()
	}
	scheduleModel := upstreamModel
	if scheduleModel == "" {
		scheduleModel = s.Account.GetMappedModel(s.Model)
	}
	h.gatewayService.ReportOpenAIAccountScheduleResult(s.Account, scheduleModel, scheduleSuccess, s.Result.FirstTokenMs, s.TurnErr)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, s.Account, s.Result)
	cyberBlocked := service.GetOpsCyberPolicy(c) != nil
	quotaPlatform := s.QuotaPlatform
	if quotaPlatform == "" {
		quotaPlatform = service.QuotaPlatform(c.Request.Context(), s.APIKey)
	}
	record := func(taskCtx context.Context) {
		source := service.UsageSourceGateway
		if s.TurnErr != nil {
			source = service.UsageSourceFailedUpstream
		}
		if err := h.gatewayService.RecordUsage(taskCtx, &service.OpenAIRecordUsageInput{
			Result:             s.Result,
			Source:             source,
			APIKey:             s.APIKey,
			User:               s.APIKey.User,
			Account:            s.Account,
			Subscription:       s.Subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          s.UserAgent,
			IPAddress:          s.ClientIP,
			SessionID:          s.SessionID,
			RequestPayloadHash: s.RequestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
			PricingAt:          s.PricingAt,
			PhaseLatency:       phaseLatency,
			ChannelUsageFields: s.ChannelMapping.ToUsageFields(s.Model, upstreamModel),
			CyberBlocked:       cyberBlocked,
			NativeCompactionV2: service.IsOpenAINativeCompactionV2(c),
		}); err != nil {
			reqLog.Error("openai.websocket_record_usage_failed",
				zap.Int64("account_id", s.Account.ID),
				zap.String("request_id", s.Result.RequestID),
				zap.Error(err),
			)
		}
	}
	if s.TurnErr != nil {
		h.submitMandatoryUsageRecordTask(ctx, record)
	} else {
		h.submitOpenAIUsageRecordTask(ctx, s.Result, record)
	}
	return ExecutableStageResult{}
}

func (h *OpenAIGatewayHandler) runOpenAIWebSocketUsageStage(c *gin.Context, adapter UsageStage) ExecutableStageResult {
	return h.runOpenAIWebSocketStage(c, adapter)
}

func (h *OpenAIGatewayHandler) openAIWebSocketUsageStageFromRouteDescriptor(c *gin.Context, fallback UsageStage) UsageStage {
	routeMeta, ok := moderationcoverage.RouteMetaFromContext(c)
	if !ok {
		return blockedUsageStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline route metadata is required before usage")
	}
	found := false
	for _, descriptor := range stageAdapterDescriptorsForRuntimeRoute(routeMeta) {
		if moderationcoverage.NormalizePipeline(descriptor.Pipeline) != moderationcoverage.PipelineOpenAIWebSocket ||
			moderationcoverage.NormalizeStage(descriptor.Stage) != moderationcoverage.StageUsage {
			continue
		}
		found = true
		if adapter, ok := bindUsageStageAdapterForDescriptor(h.openAIWebSocketStageAdapterRegistry(), descriptor, fallback); ok {
			return adapter
		}
	}
	if !found {
		return blockedUsageStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline usage stage descriptor is required before usage")
	}
	return blockedUsageStage(moderationcoverage.PipelineOpenAIWebSocket, "pipeline usage stage adapter is not bound by route descriptor")
}
