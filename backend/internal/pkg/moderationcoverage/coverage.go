package moderationcoverage

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const (
	StatusCovered            = "covered"
	StatusIntentionalNoAudit = "intentional_no_audit"

	PipelineOpenAIHTTP               = "openai_http"
	PipelineOpenAIHTTPVersion        = "openai-http-executable-v1"
	PipelineOpenAIWebSocket          = "openai_websocket"
	PipelineOpenAIWebSocketVersion   = "openai-websocket-executable-v1"
	PipelineGatewayPreForward        = "gateway_pre_forward"
	PipelineGatewayPreForwardVersion = "gateway-pre-forward-v1"
	PipelineGatewayGlobal            = "gateway_global"
	PipelineGatewayGlobalVersion     = "gateway-global-v1"

	StageModeration = "moderation"
	StageCyber      = "cyber"
	StageImage      = "image"
	StagePreForward = "pre_forward"
	StageBilling    = "billing"
	StageRouting    = "routing"
	StageForward    = "forward"
	StageUsage      = "usage"

	RuntimeRouteMetaContextKey        = "moderationcoverage.route_meta"
	PipelineEntrypointContextKey      = "moderationcoverage.pipeline_entrypoint"
	PipelineAdmittedContextKey        = "pipeline_admitted"
	PipelineAdmissionContextKey       = "moderationcoverage.pipeline_admission"
	PipelineStageExecutionsContextKey = "moderationcoverage.pipeline_stage_executions"
	ModerationReceiptContextKey       = "moderationcoverage.execution_receipt"
	ModerationReceiptsContextKey      = "moderationcoverage.execution_receipts"

	SourceOpenAIHTTPPreForward           = "OpenAIGatewayPipeline.RunHTTPPreForward"
	SourceOpenAIHTTPExecutableStage      = "OpenAIGatewayPipeline.RunHTTPExecutableStage"
	SourceOpenAIWebSocketInitialFrame    = "OpenAIGatewayPipeline.RunWebSocketInitialFrame"
	SourceOpenAIWebSocketFollowupFrame   = "OpenAIGatewayPipeline.RunWebSocketFollowupFrame"
	SourceOpenAIWebSocketExecutableStage = "OpenAIGatewayPipeline.RunWebSocketExecutableStage"
	SourceGatewayPreForward              = "GatewayPreForwardPipeline.Run"
	SourceGatewayBillingStage            = "GatewayPipeline.RunBillingStage"
	SourceGatewayRoutingStage            = "GatewayPipeline.RunRoutingStage"
	SourceGatewayForwardStage            = "GatewayPipeline.RunForwardStage"
	SourceGatewayUsageStage              = "GatewayPipeline.RunUsageStage"
)

type PipelineStageCoverage struct {
	Stage    string
	Required bool
	Covered  bool
}

type RouteAdapterDescriptor struct {
	Stage    string `json:"stage"`
	Pipeline string `json:"pipeline"`
	Name     string `json:"name"`
}

type Entry struct {
	Method                  string
	Path                    string
	Handler                 string
	Upstream                bool
	ModerationRequired      bool
	Protocol                string
	Pipeline                string
	StageCoverage           []PipelineStageCoverage
	StageAdapterDescriptors []RouteAdapterDescriptor
	Status                  string
	ReviewReason            string
}

type PipelineAdmission struct {
	Admitted            bool
	Pipeline            string
	Stage               string
	Source              string
	ModerationCompleted bool
	ReceiptOutcome      string
}

// ModerationExecutionReceipt is the content-free proof that a request or
// WebSocket frame completed its moderation gate before it was forwarded.
// It intentionally contains no user-controlled source text or identifiers.
type ModerationExecutionReceipt struct {
	RequestID      string
	Protocol       string
	PolicyRevision string
	LocalScanDone  bool
	SemanticCalled bool
	Outcome        string
	ForwardAllowed bool
}

type PipelineEntrypoint struct {
	Entered  bool
	Pipeline string
	Source   string
}

type PipelineStageExecution struct {
	Pipeline string
	Stage    string
	Source   string
	Method   string
	Path     string
	Handler  string
	Protocol string
	Error    bool
}

type Status struct {
	ManifestVersion string   `json:"manifest_version"`
	ManifestHash    string   `json:"manifest_hash"`
	Status          string   `json:"status"`
	RequiredRoutes  int      `json:"required_routes"`
	CoveredRoutes   int      `json:"covered_routes"`
	UncoveredRoutes []string `json:"uncovered_routes"`
}

var registry = struct {
	sync.Mutex
	entries []Entry
}{}

func Register(entry Entry) {
	normalized := NormalizeEntry(entry)
	registry.Lock()
	defer registry.Unlock()
	registry.entries = append(registry.entries, normalized)
}

func Entries() []Entry {
	registry.Lock()
	defer registry.Unlock()
	return entriesSnapshotLocked()
}

func SetRouteMeta(c *gin.Context, entry Entry) {
	if c == nil {
		return
	}
	c.Set(RuntimeRouteMetaContextKey, NormalizeEntry(entry))
}

func RouteMetaFromContext(c *gin.Context) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	value, ok := c.Get(RuntimeRouteMetaContextKey)
	if !ok {
		return Entry{}, false
	}
	switch meta := value.(type) {
	case Entry:
		return NormalizeEntry(meta), true
	case *Entry:
		if meta == nil {
			return Entry{}, false
		}
		return NormalizeEntry(*meta), true
	default:
		return Entry{}, false
	}
}

func MarkPipelineAdmitted(c *gin.Context, pipeline, stage, source string) {
	if c == nil {
		return
	}
	admission := PipelineAdmission{
		Admitted:            true,
		Pipeline:            NormalizePipeline(pipeline),
		Stage:               NormalizeStage(stage),
		Source:              strings.TrimSpace(source),
		ModerationCompleted: true,
		ReceiptOutcome:      "legacy_attested",
	}
	if receipt, ok := ModerationReceiptFromContext(c); ok {
		admission.ModerationCompleted = receipt.LocalScanDone
		admission.ReceiptOutcome = receipt.Outcome
		admission.Admitted = receipt.LocalScanDone && receipt.ForwardAllowed
	}
	if !admission.Admitted {
		c.Set(PipelineAdmittedContextKey, false)
		c.Set(PipelineAdmissionContextKey, admission)
		return
	}
	c.Set(PipelineAdmittedContextKey, true)
	c.Set(PipelineAdmissionContextKey, admission)
}

// MarkPipelineAdmittedAfterModeration is the production admission path. In
// contrast to the legacy metadata helper above, it cannot admit a request
// without a completed, forwardable receipt.
func MarkPipelineAdmittedAfterModeration(c *gin.Context, pipeline, stage, source string) {
	if c == nil {
		return
	}
	receipt, ok := ModerationReceiptFromContext(c)
	admission := PipelineAdmission{
		Pipeline: NormalizePipeline(pipeline),
		Stage:    NormalizeStage(stage),
		Source:   strings.TrimSpace(source),
	}
	if ok {
		admission.ModerationCompleted = receipt.LocalScanDone
		admission.ReceiptOutcome = receipt.Outcome
		admission.Admitted = receipt.LocalScanDone && receipt.ForwardAllowed
	}
	c.Set(PipelineAdmittedContextKey, admission.Admitted)
	c.Set(PipelineAdmissionContextKey, admission)
}

// MarkModerationReceipt replaces the current-frame receipt and appends an
// immutable copy to the request history. WebSocket callers use the history to
// prove that every response.create frame was independently checked.
func MarkModerationReceipt(c *gin.Context, receipt ModerationExecutionReceipt) {
	if c == nil {
		return
	}
	receipt = normalizeModerationReceipt(receipt)
	c.Set(ModerationReceiptContextKey, receipt)
	history := ModerationReceiptsFromContext(c)
	history = append(history, receipt)
	c.Set(ModerationReceiptsContextKey, history)
}

func ModerationReceiptFromContext(c *gin.Context) (ModerationExecutionReceipt, bool) {
	if c == nil {
		return ModerationExecutionReceipt{}, false
	}
	value, ok := c.Get(ModerationReceiptContextKey)
	if !ok {
		return ModerationExecutionReceipt{}, false
	}
	switch receipt := value.(type) {
	case ModerationExecutionReceipt:
		return normalizeModerationReceipt(receipt), true
	case *ModerationExecutionReceipt:
		if receipt == nil {
			return ModerationExecutionReceipt{}, false
		}
		return normalizeModerationReceipt(*receipt), true
	default:
		return ModerationExecutionReceipt{}, false
	}
}

func ModerationReceiptsFromContext(c *gin.Context) []ModerationExecutionReceipt {
	if c == nil {
		return nil
	}
	value, ok := c.Get(ModerationReceiptsContextKey)
	if !ok {
		return nil
	}
	history, ok := value.([]ModerationExecutionReceipt)
	if !ok {
		return nil
	}
	result := make([]ModerationExecutionReceipt, len(history))
	for i := range history {
		result[i] = normalizeModerationReceipt(history[i])
	}
	return result
}

func ModerationReceiptAllowsForward(c *gin.Context) bool {
	receipt, ok := ModerationReceiptFromContext(c)
	return ok && receipt.LocalScanDone && receipt.ForwardAllowed
}

func PipelineAdmittedFromContext(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, ok := c.Get(PipelineAdmittedContextKey)
	if !ok {
		return false
	}
	admitted, ok := value.(bool)
	return ok && admitted
}

func PipelineAdmissionFromContext(c *gin.Context) (PipelineAdmission, bool) {
	if c == nil {
		return PipelineAdmission{}, false
	}
	value, ok := c.Get(PipelineAdmissionContextKey)
	if !ok {
		return PipelineAdmission{}, false
	}
	switch admission := value.(type) {
	case PipelineAdmission:
		return normalizePipelineAdmission(admission), true
	case *PipelineAdmission:
		if admission == nil {
			return PipelineAdmission{}, false
		}
		return normalizePipelineAdmission(*admission), true
	default:
		return PipelineAdmission{}, false
	}
}

func MarkPipelineEntrypointEntered(c *gin.Context, pipeline, source string) {
	if c == nil {
		return
	}
	entrypoint := PipelineEntrypoint{
		Entered:  true,
		Pipeline: NormalizePipeline(pipeline),
		Source:   strings.TrimSpace(source),
	}
	if entrypoint.Pipeline == "" {
		return
	}
	c.Set(PipelineEntrypointContextKey, entrypoint)
}

func PipelineEntrypointEnteredFromContext(c *gin.Context, pipeline string) (PipelineEntrypoint, bool) {
	if c == nil {
		return PipelineEntrypoint{}, false
	}
	value, ok := c.Get(PipelineEntrypointContextKey)
	if !ok {
		return PipelineEntrypoint{}, false
	}
	var entrypoint PipelineEntrypoint
	switch v := value.(type) {
	case PipelineEntrypoint:
		entrypoint = v
	case *PipelineEntrypoint:
		if v == nil {
			return PipelineEntrypoint{}, false
		}
		entrypoint = *v
	default:
		return PipelineEntrypoint{}, false
	}
	entrypoint.Pipeline = NormalizePipeline(entrypoint.Pipeline)
	entrypoint.Source = strings.TrimSpace(entrypoint.Source)
	entrypoint.Entered = entrypoint.Entered && entrypoint.Pipeline != ""
	if !entrypoint.Entered || entrypoint.Pipeline != NormalizePipeline(pipeline) {
		return PipelineEntrypoint{}, false
	}
	return entrypoint, true
}

func MarkPipelineStageExecuted(c *gin.Context, pipeline, stage, source string) {
	MarkPipelineStageExecutedWithResult(c, pipeline, stage, source, false)
}

func MarkPipelineStageExecutedWithResult(c *gin.Context, pipeline, stage, source string, failed bool) {
	if c == nil {
		return
	}
	routeMeta, _ := RouteMetaFromContext(c)
	execution := normalizePipelineStageExecution(PipelineStageExecution{
		Pipeline: pipeline,
		Stage:    stage,
		Source:   source,
		Method:   routeMeta.Method,
		Path:     routeMeta.Path,
		Handler:  routeMeta.Handler,
		Protocol: routeMeta.Protocol,
		Error:    failed,
	})
	if execution.Pipeline == "" || execution.Stage == "" || execution.Source == "" {
		return
	}
	recordPipelineStageExecution(execution)
	executions := append(PipelineStageExecutionsFromContext(c), execution)
	c.Set(PipelineStageExecutionsContextKey, normalizePipelineStageExecutions(executions))
}

func ObservePipelineStageExecutedWithResult(c *gin.Context, pipeline, stage, source string, failed bool) {
	routeMeta, _ := RouteMetaFromContext(c)
	execution := normalizePipelineStageExecution(PipelineStageExecution{
		Pipeline: pipeline,
		Stage:    stage,
		Source:   source,
		Method:   routeMeta.Method,
		Path:     routeMeta.Path,
		Handler:  routeMeta.Handler,
		Protocol: routeMeta.Protocol,
		Error:    failed,
	})
	if execution.Pipeline == "" || execution.Stage == "" || execution.Source == "" {
		return
	}
	recordPipelineStageExecution(execution)
}

func PipelineStageExecutionsFromContext(c *gin.Context) []PipelineStageExecution {
	if c == nil {
		return nil
	}
	value, ok := c.Get(PipelineStageExecutionsContextKey)
	if !ok {
		return nil
	}
	switch executions := value.(type) {
	case []PipelineStageExecution:
		return normalizePipelineStageExecutions(executions)
	case []*PipelineStageExecution:
		values := make([]PipelineStageExecution, 0, len(executions))
		for _, execution := range executions {
			if execution == nil {
				continue
			}
			values = append(values, *execution)
		}
		return normalizePipelineStageExecutions(values)
	case PipelineStageExecution:
		return normalizePipelineStageExecutions([]PipelineStageExecution{executions})
	case *PipelineStageExecution:
		if executions == nil {
			return nil
		}
		return normalizePipelineStageExecutions([]PipelineStageExecution{*executions})
	default:
		return nil
	}
}

func ReplaceRegistryForTest(entries []Entry) func() {
	registry.Lock()
	previous := entriesSnapshotLocked()
	registry.entries = normalizeEntries(entries)
	registry.Unlock()

	return func() {
		registry.Lock()
		defer registry.Unlock()
		registry.entries = previous
	}
}

func CoverageStatus(manifestVersion string) Status {
	return CoverageStatusFromEntries(manifestVersion, Entries())
}

func CoverageStatusFromEntries(manifestVersion string, entries []Entry) Status {
	required := 0
	covered := 0
	uncoveredRoutes := make([]string, 0)
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired {
			continue
		}
		normalizedMethod := NormalizeMethod(entry.Method)
		normalizedPath := NormalizePath(entry.Path)
		normalizedStatus := NormalizeStatus(entry.Status)
		required++
		if normalizedStatus == StatusCovered {
			covered++
			continue
		}
		route := strings.TrimSpace(normalizedMethod + " " + normalizedPath)
		if route == "" {
			route = "unknown"
		}
		uncoveredRoutes = append(uncoveredRoutes, route)
	}

	status := "covered"
	if required == 0 {
		status = "unknown"
	} else if covered != required || len(uncoveredRoutes) > 0 {
		status = "mismatch"
	}
	return Status{
		ManifestVersion: manifestVersion,
		ManifestHash:    HashFromEntries(entries),
		Status:          status,
		RequiredRoutes:  required,
		CoveredRoutes:   covered,
		UncoveredRoutes: uncoveredRoutes,
	}
}

func HashFromEntries(entries []Entry) string {
	routes := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Upstream || !entry.ModerationRequired {
			continue
		}
		routes = append(routes, NormalizeStatus(entry.Status)+" "+NormalizeMethod(entry.Method)+" "+NormalizePath(entry.Path))
	}
	sort.Strings(routes)
	sum := sha256.Sum256([]byte(strings.Join(routes, "\n")))
	return hex.EncodeToString(sum[:])
}

func NormalizeEntry(entry Entry) Entry {
	entry.Method = NormalizeMethod(entry.Method)
	entry.Path = NormalizePath(entry.Path)
	entry.Handler = strings.TrimSpace(entry.Handler)
	entry.Protocol = strings.TrimSpace(entry.Protocol)
	entry.Pipeline = NormalizePipeline(entry.Pipeline)
	entry.StageCoverage = NormalizeStageCoverage(entry.StageCoverage)
	entry.StageAdapterDescriptors = NormalizeRouteAdapterDescriptors(entry.StageAdapterDescriptors)
	entry.Status = NormalizeStatus(entry.Status)
	entry.ReviewReason = strings.TrimSpace(entry.ReviewReason)
	return entry
}

func AnnotatePipelineCoverage(entry Entry) Entry {
	entry = NormalizeEntry(entry)
	if stages := OpenAIWebSocketPipelineStagesForRoute(entry.Handler, entry.Protocol); len(stages) > 0 {
		entry.Pipeline = PipelineOpenAIWebSocket
		entry.StageCoverage = stages
		entry.StageAdapterDescriptors = StageAdapterDescriptorsForRoute(entry.Handler, entry.Protocol)
		return NormalizeEntry(entry)
	}
	stages := OpenAIHTTPPipelineStagesForRoute(entry.Handler, entry.Protocol)
	if len(stages) == 0 {
		if stages = GatewayPreForwardPipelineStagesForRoute(entry.Handler, entry.Protocol); len(stages) == 0 {
			return entry
		}
		entry.Pipeline = PipelineGatewayPreForward
		entry.StageCoverage = stages
		entry.StageAdapterDescriptors = StageAdapterDescriptorsForRoute(entry.Handler, entry.Protocol)
		return NormalizeEntry(entry)
	}
	entry.Pipeline = PipelineOpenAIHTTP
	entry.StageCoverage = stages
	entry.StageAdapterDescriptors = StageAdapterDescriptorsForRoute(entry.Handler, entry.Protocol)
	return NormalizeEntry(entry)
}

func OpenAIHTTPPipelineStagesForRoute(handlerName, protocol string) []PipelineStageCoverage {
	if !IsOpenAIHTTPPipelineProtocol(protocol) {
		return nil
	}

	stages := []PipelineStageCoverage{
		CoveredPipelineStage(StageModeration),
	}
	switch strings.TrimSpace(handlerName) {
	case "OpenAIGatewayHandler.ChatCompletions":
		stages = append(stages, CoveredPipelineStage(StageCyber))
	case "OpenAIGatewayHandler.Messages":
		stages = append(stages, CoveredPipelineStage(StageCyber))
	case "OpenAIGatewayHandler.Responses", "OpenAIGatewayHandler.AlphaSearch":
		stages = append(stages,
			CoveredPipelineStage(StageCyber),
			CoveredPipelineStage(StageImage),
		)
	case "OpenAIGatewayHandler.Images":
		stages = append(stages, CoveredPipelineStage(StageImage))
	case "OpenAIGatewayHandler.GrokVideoGeneration", "OpenAIGatewayHandler.GrokVideoEdit", "OpenAIGatewayHandler.GrokVideoExtension":
		stages = append(stages, CoveredPipelineStage(StageImage))
	case "OpenAIGatewayHandler.Embeddings", "OpenAIGatewayHandler.GrokVoice", "GatewayHandler.WebSearch", "GatewayHandler.XSearch":
	default:
		return nil
	}
	stages = append(stages,
		CoveredPipelineStage(StageBilling),
		CoveredPipelineStage(StageRouting),
		CoveredPipelineStage(StageForward),
		CoveredPipelineStage(StageUsage),
	)
	return NormalizeStageCoverage(stages)
}

func OpenAIWebSocketPipelineStagesForRoute(handlerName, protocol string) []PipelineStageCoverage {
	switch strings.TrimSpace(protocol) {
	case "openai_responses", "openai_realtime":
	default:
		return nil
	}
	switch strings.TrimSpace(handlerName) {
	case "OpenAIGatewayHandler.ResponsesWebSocket",
		"OpenAIGatewayHandler.RealtimeWebSocket",
		"OpenAIGatewayHandler.Realtime":
	default:
		return nil
	}
	return NormalizeStageCoverage([]PipelineStageCoverage{
		CoveredPipelineStage(StageModeration),
		CoveredPipelineStage(StageCyber),
		CoveredPipelineStage(StageImage),
		CoveredPipelineStage(StagePreForward),
		CoveredPipelineStage(StageBilling),
		CoveredPipelineStage(StageRouting),
		CoveredPipelineStage(StageForward),
		CoveredPipelineStage(StageUsage),
	})
}

func GatewayPreForwardPipelineStagesForRoute(handlerName, protocol string) []PipelineStageCoverage {
	if !IsGatewayPreForwardPipelineProtocol(protocol) {
		return nil
	}
	var stages []PipelineStageCoverage
	switch strings.TrimSpace(handlerName) {
	case "GatewayHandler.Messages", "GatewayHandler.GeminiV1BetaModels", "GatewayHandler.ChatCompletions", "GatewayHandler.Responses":
		stages = []PipelineStageCoverage{
			CoveredPipelineStage(StageModeration),
			CoveredPipelineStage(StagePreForward),
			CoveredPipelineStage(StageBilling),
			CoveredPipelineStage(StageRouting),
			CoveredPipelineStage(StageForward),
			CoveredPipelineStage(StageUsage),
		}
	case "GatewayHandler.CountTokens":
		stages = []PipelineStageCoverage{
			CoveredPipelineStage(StageModeration),
			CoveredPipelineStage(StagePreForward),
			CoveredPipelineStage(StageBilling),
			CoveredPipelineStage(StageRouting),
			CoveredPipelineStage(StageForward),
		}
	default:
		return nil
	}
	return NormalizeStageCoverage(stages)
}

func ForwardAdaptersForRoute(handlerName, protocol string) []string {
	descriptors := ForwardAdapterDescriptorsForRoute(handlerName, protocol)
	if len(descriptors) == 0 {
		return nil
	}
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.Name == "" {
			continue
		}
		names = append(names, descriptor.Name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func ForwardAdapterDescriptorsForRoute(handlerName, protocol string) []RouteAdapterDescriptor {
	descriptors := StageAdapterDescriptorsForRoute(handlerName, protocol)
	if len(descriptors) == 0 {
		return nil
	}
	out := make([]RouteAdapterDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if NormalizeStage(descriptor.Stage) != StageForward {
			continue
		}
		out = append(out, descriptor)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func StageAdapterDescriptorsForRoute(handlerName, protocol string) []RouteAdapterDescriptor {
	handlerName = strings.TrimSpace(handlerName)
	protocol = strings.TrimSpace(protocol)
	descriptor := func(stage, pipeline, name string) RouteAdapterDescriptor {
		return RouteAdapterDescriptor{
			Stage:    NormalizeStage(stage),
			Pipeline: NormalizePipeline(pipeline),
			Name:     strings.TrimSpace(name),
		}
	}
	openAIHTTP := func() []RouteAdapterDescriptor {
		return []RouteAdapterDescriptor{
			descriptor(StageBilling, PipelineOpenAIHTTP, "OpenAIHTTPBillingStage"),
			descriptor(StageRouting, PipelineOpenAIHTTP, "OpenAIHTTPRoutingStage"),
			descriptor(StageForward, PipelineOpenAIHTTP, "OpenAIHTTPForwardStage"),
			descriptor(StageUsage, PipelineOpenAIHTTP, "OpenAIHTTPUsageStage"),
		}
	}
	openAIWebSocket := func() []RouteAdapterDescriptor {
		return []RouteAdapterDescriptor{
			descriptor(StageBilling, PipelineOpenAIWebSocket, "OpenAIWebSocketBillingStage"),
			descriptor(StageRouting, PipelineOpenAIWebSocket, "OpenAIWebSocketRoutingStage"),
			descriptor(StageForward, PipelineOpenAIWebSocket, "OpenAIWebSocketForwardStage"),
			descriptor(StageUsage, PipelineOpenAIWebSocket, "OpenAIWebSocketUsageStage"),
		}
	}
	gatewayPreForward := func(forwardNames ...string) []RouteAdapterDescriptor {
		descriptors := []RouteAdapterDescriptor{
			descriptor(StageBilling, PipelineGatewayPreForward, "GatewayBillingStage"),
			descriptor(StageRouting, PipelineGatewayPreForward, "GatewayRoutingStage"),
		}
		for _, name := range forwardNames {
			descriptors = append(descriptors, descriptor(StageForward, PipelineGatewayPreForward, name))
		}
		if len(GatewayPreForwardPipelineStagesForRoute(handlerName, protocol)) > 0 {
			for _, stage := range GatewayPreForwardPipelineStagesForRoute(handlerName, protocol) {
				if NormalizeStage(stage.Stage) == StageUsage {
					descriptors = append(descriptors, descriptor(StageUsage, PipelineGatewayPreForward, "GatewayUsageStage"))
					break
				}
			}
		}
		return descriptors
	}
	switch handlerName {
	case "OpenAIGatewayHandler.ChatCompletions",
		"OpenAIGatewayHandler.Messages",
		"OpenAIGatewayHandler.Responses",
		"OpenAIGatewayHandler.AlphaSearch",
		"OpenAIGatewayHandler.Images",
		"OpenAIGatewayHandler.GrokVideoGeneration",
		"OpenAIGatewayHandler.GrokVideoEdit",
		"OpenAIGatewayHandler.GrokVideoExtension",
		"OpenAIGatewayHandler.Embeddings":
		if len(OpenAIHTTPPipelineStagesForRoute(handlerName, protocol)) > 0 {
			return openAIHTTP()
		}
	case "GatewayHandler.WebSearch", "GatewayHandler.XSearch":
		if len(OpenAIHTTPPipelineStagesForRoute(handlerName, protocol)) > 0 {
			return openAIHTTP()
		}
	case "OpenAIGatewayHandler.ResponsesWebSocket":
		if len(OpenAIWebSocketPipelineStagesForRoute(handlerName, protocol)) > 0 {
			return openAIWebSocket()
		}
	case "OpenAIGatewayHandler.RealtimeWebSocket", "OpenAIGatewayHandler.Realtime":
		if len(OpenAIWebSocketPipelineStagesForRoute(handlerName, protocol)) > 0 {
			return openAIWebSocket()
		}
	case "GatewayHandler.Messages":
		return gatewayPreForward("GatewayMessagesGeminiForwardStage", "GatewayMessagesForwardStage")
	case "GatewayHandler.CountTokens":
		return gatewayPreForward("GatewayCountTokensForwardStage")
	case "GatewayHandler.GeminiV1BetaModels":
		return gatewayPreForward("GatewayGeminiV1BetaForwardStage")
	case "GatewayHandler.ChatCompletions":
		return gatewayPreForward("GatewayChatCompletionsForwardStage")
	case "GatewayHandler.Responses":
		return gatewayPreForward("GatewayResponsesForwardStage")
	}
	return nil
}

func CoveredPipelineStage(stage string) PipelineStageCoverage {
	return PipelineStageCoverage{
		Stage:    NormalizeStage(stage),
		Required: true,
		Covered:  true,
	}
}

func NormalizeRouteAdapterDescriptors(descriptors []RouteAdapterDescriptor) []RouteAdapterDescriptor {
	if len(descriptors) == 0 {
		return nil
	}
	seen := make(map[string]RouteAdapterDescriptor, len(descriptors))
	for _, descriptor := range descriptors {
		normalized := RouteAdapterDescriptor{
			Stage:    NormalizeStage(descriptor.Stage),
			Pipeline: NormalizePipeline(descriptor.Pipeline),
			Name:     strings.TrimSpace(descriptor.Name),
		}
		if normalized.Stage == "" || normalized.Pipeline == "" || normalized.Name == "" {
			continue
		}
		key := normalized.Stage + "\x00" + normalized.Pipeline + "\x00" + normalized.Name
		seen[key] = normalized
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := seen[keys[i]]
		right := seen[keys[j]]
		if left.Stage != right.Stage {
			return PipelineStageSortKey(left.Stage) < PipelineStageSortKey(right.Stage)
		}
		if left.Pipeline != right.Pipeline {
			return left.Pipeline < right.Pipeline
		}
		return left.Name < right.Name
	})
	out := make([]RouteAdapterDescriptor, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func IsOpenAIHTTPPipelineProtocol(protocol string) bool {
	switch strings.TrimSpace(protocol) {
	case "openai_chat_completions", "openai_messages", "openai_responses", "openai_images", "openai_embeddings":
		return true
	default:
		return false
	}
}

func IsGatewayPreForwardPipelineProtocol(protocol string) bool {
	switch strings.TrimSpace(protocol) {
	case "anthropic_messages", "gemini", "openai_chat_completions", "openai_responses":
		return true
	default:
		return false
	}
}

func NormalizeMethod(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func NormalizePath(value string) string {
	return strings.TrimSpace(value)
}

func NormalizeStatus(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizePipeline(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeStage(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeSource(value string) string {
	return strings.TrimSpace(value)
}

func NormalizeStageCoverage(stages []PipelineStageCoverage) []PipelineStageCoverage {
	stagesByName := make(map[string]PipelineStageCoverage, len(stages))
	for _, stage := range stages {
		stage.Stage = NormalizeStage(stage.Stage)
		if stage.Stage == "" {
			continue
		}
		stagesByName[stage.Stage] = stage
	}

	names := make([]string, 0, len(stagesByName))
	for name := range stagesByName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return PipelineStageSortKey(names[i]) < PipelineStageSortKey(names[j])
	})

	normalized := make([]PipelineStageCoverage, 0, len(names))
	for _, name := range names {
		normalized = append(normalized, stagesByName[name])
	}
	return normalized
}

func normalizePipelineAdmission(admission PipelineAdmission) PipelineAdmission {
	admission.Pipeline = NormalizePipeline(admission.Pipeline)
	admission.Stage = NormalizeStage(admission.Stage)
	admission.Source = NormalizeSource(admission.Source)
	admission.ReceiptOutcome = strings.TrimSpace(strings.ToLower(admission.ReceiptOutcome))
	return admission
}

func normalizeModerationReceipt(receipt ModerationExecutionReceipt) ModerationExecutionReceipt {
	receipt.RequestID = strings.TrimSpace(receipt.RequestID)
	receipt.Protocol = strings.TrimSpace(strings.ToLower(receipt.Protocol))
	receipt.PolicyRevision = strings.TrimSpace(receipt.PolicyRevision)
	receipt.Outcome = strings.TrimSpace(strings.ToLower(receipt.Outcome))
	if !receipt.LocalScanDone {
		receipt.ForwardAllowed = false
	}
	return receipt
}

func normalizePipelineStageExecution(execution PipelineStageExecution) PipelineStageExecution {
	execution.Pipeline = NormalizePipeline(execution.Pipeline)
	execution.Stage = NormalizeStage(execution.Stage)
	execution.Source = NormalizeSource(execution.Source)
	execution.Method = NormalizeMethod(execution.Method)
	execution.Path = NormalizePath(execution.Path)
	execution.Handler = strings.TrimSpace(execution.Handler)
	execution.Protocol = strings.TrimSpace(execution.Protocol)
	return execution
}

func normalizePipelineStageExecutions(executions []PipelineStageExecution) []PipelineStageExecution {
	executionsByKey := make(map[string]PipelineStageExecution, len(executions))
	for _, execution := range executions {
		normalized := normalizePipelineStageExecution(execution)
		if normalized.Pipeline == "" || normalized.Stage == "" || normalized.Source == "" {
			continue
		}
		executionsByKey[pipelineStageExecutionKey(normalized)] = normalized
	}

	normalized := make([]PipelineStageExecution, 0, len(executionsByKey))
	for _, execution := range executionsByKey {
		normalized = append(normalized, execution)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return pipelineStageExecutionLess(normalized[i], normalized[j])
	})
	return normalized
}

func pipelineStageExecutionKey(execution PipelineStageExecution) string {
	return strings.Join([]string{
		execution.Pipeline,
		execution.Stage,
		execution.Source,
		execution.Method,
		execution.Path,
		execution.Handler,
		execution.Protocol,
	}, "\x00")
}

func pipelineStageExecutionLess(left, right PipelineStageExecution) bool {
	if left.Pipeline != right.Pipeline {
		return left.Pipeline < right.Pipeline
	}
	leftStage := PipelineStageSortKey(left.Stage)
	rightStage := PipelineStageSortKey(right.Stage)
	if leftStage != rightStage {
		return leftStage < rightStage
	}
	if left.Source != right.Source {
		return left.Source < right.Source
	}
	if left.Method != right.Method {
		return left.Method < right.Method
	}
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	if left.Handler != right.Handler {
		return left.Handler < right.Handler
	}
	return left.Protocol < right.Protocol
}

func PipelineStageSortKey(stage string) string {
	switch NormalizeStage(stage) {
	case StageModeration:
		return "00:" + StageModeration
	case StageCyber:
		return "01:" + StageCyber
	case StageImage:
		return "02:" + StageImage
	case StagePreForward:
		return "03:" + StagePreForward
	case StageBilling:
		return "04:" + StageBilling
	case StageRouting:
		return "05:" + StageRouting
	case StageForward:
		return "06:" + StageForward
	case StageUsage:
		return "07:" + StageUsage
	default:
		return "99:" + NormalizeStage(stage)
	}
}

func normalizeEntries(entries []Entry) []Entry {
	normalized := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		normalized = append(normalized, NormalizeEntry(entry))
	}
	return normalized
}

func entriesSnapshotLocked() []Entry {
	entriesByKey := make(map[string]Entry, len(registry.entries))
	for _, entry := range registry.entries {
		normalized := NormalizeEntry(entry)
		key := entryKey(normalized)
		if key == "" {
			continue
		}
		entriesByKey[key] = normalized
	}

	keys := make([]string, 0, len(entriesByKey))
	for key := range entriesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	entries := make([]Entry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, entriesByKey[key])
	}
	return entries
}

func entryKey(entry Entry) string {
	if strings.TrimSpace(entry.Method) == "" || strings.TrimSpace(entry.Path) == "" {
		return ""
	}
	return entry.Method + " " + entry.Path + " " + entry.Protocol + " " + entry.Handler
}
