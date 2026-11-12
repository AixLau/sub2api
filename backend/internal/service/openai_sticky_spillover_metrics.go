package service

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// OpenAI 调度可观测性：借道/排队事件、排队等待时长，以及按调度路由统计的上游真实
// 缓存用量。所有标签取值都是有界枚举，不含用户、账号或会话标识。
//
// 两类指标不要混淆：
//   - 「备用账号复用」（spill_secondary）是调度层面的账号复用；
//   - 「上游 Prompt Cache 命中」只能由上游返回的 usage 判断：Responses 的
//     input_tokens_details.cached_tokens、Chat Completions 的
//     prompt_tokens_details.cached_tokens（网关内统一为 CacheReadInputTokens）。
//     真实命中率 = cached_tokens / input_tokens，按路由分别统计。

// 调度路由：一次请求最终由哪一类账号服务。
const (
	// OpenAISchedulingRouteStickyPrimary 会话主账号（粘性绑定）。
	OpenAISchedulingRouteStickyPrimary = "sticky_primary"
	// OpenAISchedulingRouteSpillSecondary 主账号已满，借道并复用了备用账号。
	OpenAISchedulingRouteSpillSecondary = "spill_secondary"
	// OpenAISchedulingRouteSpillNew 主账号已满，借道到一个此前未记录的账号。
	OpenAISchedulingRouteSpillNew = "spill_new"
	// OpenAISchedulingRoutePreviousResponse 由 previous_response_id 命中。
	OpenAISchedulingRoutePreviousResponse = "previous_response"
	// OpenAISchedulingRouteLoadBalance 无粘性绑定（新会话或无会话）的负载均衡选号。
	OpenAISchedulingRouteLoadBalance = "load_balance"
)

var openAISchedulingRoutes = []string{
	OpenAISchedulingRouteStickyPrimary,
	OpenAISchedulingRouteSpillSecondary,
	OpenAISchedulingRouteSpillNew,
	OpenAISchedulingRoutePreviousResponse,
	OpenAISchedulingRouteLoadBalance,
}

// 排队等待的结局。
const (
	// OpenAIAccountWaitOutcomeAcquired 在最后排队的账号上拿到槽位。
	OpenAIAccountWaitOutcomeAcquired = "acquired"
	// OpenAIAccountWaitOutcomeAcquiredOtherAccount 排队后经重新选号在另一个账号上拿到槽位。
	OpenAIAccountWaitOutcomeAcquiredOtherAccount = "acquired_other_account"
	// OpenAIAccountWaitOutcomeTimeout 等待预算用尽（返回 429）。
	OpenAIAccountWaitOutcomeTimeout = "timeout"
	// OpenAIAccountWaitOutcomeError 等待被客户端取消或因其他错误中止。
	OpenAIAccountWaitOutcomeError = "error"
)

var openAIAccountWaitOutcomes = []string{
	OpenAIAccountWaitOutcomeAcquired,
	OpenAIAccountWaitOutcomeAcquiredOtherAccount,
	OpenAIAccountWaitOutcomeTimeout,
	OpenAIAccountWaitOutcomeError,
}

type openAISchedulingRouteUsageCounters struct {
	requests     atomic.Int64
	cacheHits    atomic.Int64
	inputTokens  atomic.Int64
	cachedTokens atomic.Int64
}

var (
	openAIStickySpilloverSecondaryReuseTotal  atomic.Int64
	openAIStickySpilloverNewAccountTotal      atomic.Int64
	openAIStickySpilloverSwapTotal            atomic.Int64
	openAIStickySpilloverBindingConflictTotal atomic.Int64
	openAIStickySpilloverPrimaryWaitTotal     atomic.Int64
	openAISchedulingWaitReselectTotal         atomic.Int64
	openAISchedulingWaitQueueFullTotal        atomic.Int64

	openAISchedulingRouteUsage = func() map[string]*openAISchedulingRouteUsageCounters {
		out := make(map[string]*openAISchedulingRouteUsageCounters, len(openAISchedulingRoutes))
		for _, route := range openAISchedulingRoutes {
			out[route] = &openAISchedulingRouteUsageCounters{}
		}
		return out
	}()

	openAISchedulingAccountWaitSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "sub2api_openai_scheduling_account_wait_seconds",
		Help:    "Total time a request spent queueing for an account slot (across wait reselects), by outcome.",
		Buckets: []float64{0.1, 0.5, 1, 3, 5, 10, 20, 30, 60, 120, 300},
	}, []string{"outcome"})

	openAISchedulingMetricsRegistry = newOpenAISchedulingMetricsRegistry()
)

func newOpenAISchedulingMetricsRegistry() *prometheus.Registry {
	registry := prometheus.NewRegistry()
	counter := func(name, help string, labels prometheus.Labels, value *atomic.Int64) {
		registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{
			Name: name, Help: help, ConstLabels: labels,
		}, func() float64 { return float64(value.Load()) }))
	}
	const eventsName = "sub2api_openai_scheduling_events_total"
	const eventsHelp = "OpenAI sticky spillover and account queue events."
	for event, value := range map[string]*atomic.Int64{
		// 主账号排队：全部账号已满，回到主账号的有界等待队列。
		"primary_wait": &openAIStickySpilloverPrimaryWaitTotal,
		// 溢出：主账号已满，请求改由其他账号服务（= 请求级账号切换）。
		"spill_secondary": &openAIStickySpilloverSecondaryReuseTotal,
		"spill_new":       &openAIStickySpilloverNewAccountTotal,
		// 主备互换：会话主绑定实际切换到另一个账号。
		"primary_swap": &openAIStickySpilloverSwapTotal,
		// 并发借道的绑定更新因状态已变而被丢弃。
		"binding_conflict": &openAIStickySpilloverBindingConflictTotal,
		// 排队一个重选间隔后重新选号 / 排队队列已满直接拒绝。
		"wait_reselect":   &openAISchedulingWaitReselectTotal,
		"wait_queue_full": &openAISchedulingWaitQueueFullTotal,
	} {
		counter(eventsName, eventsHelp, prometheus.Labels{"event": event}, value)
	}
	for _, route := range openAISchedulingRoutes {
		usage := openAISchedulingRouteUsage[route]
		labels := prometheus.Labels{"route": route}
		counter("sub2api_openai_scheduling_route_requests_total", "Requests with upstream usage, by scheduling route.", labels, &usage.requests)
		counter("sub2api_openai_scheduling_route_cache_hit_requests_total", "Requests whose upstream usage reported cached_tokens > 0, by scheduling route.", labels, &usage.cacheHits)
		counter("sub2api_openai_scheduling_route_input_tokens_total", "Upstream input tokens (cached included), by scheduling route.", labels, &usage.inputTokens)
		counter("sub2api_openai_scheduling_route_cached_tokens_total", "Upstream cached input tokens (input_tokens_details.cached_tokens / prompt_tokens_details.cached_tokens), by scheduling route.", labels, &usage.cachedTokens)
	}
	registry.MustRegister(openAISchedulingAccountWaitSeconds)
	for _, outcome := range openAIAccountWaitOutcomes {
		openAISchedulingAccountWaitSeconds.WithLabelValues(outcome)
	}
	return registry
}

// OpenAISchedulingMetricsHandler 以 Prometheus 文本格式输出 OpenAI 调度指标。
func OpenAISchedulingMetricsHandler() http.Handler {
	return promhttp.HandlerFor(openAISchedulingMetricsRegistry, promhttp.HandlerOpts{})
}

// OpenAISchedulingRouteUsageSnapshot 某条调度路由上的上游真实缓存用量。
type OpenAISchedulingRouteUsageSnapshot struct {
	// Requests 带上游 usage 的请求数；CacheHitRequests 其中 cached_tokens > 0 的请求数。
	Requests         int64
	CacheHitRequests int64
	// InputTokens 上游输入 token 总量（含缓存部分）；CachedTokens 其中命中缓存的部分。
	InputTokens  int64
	CachedTokens int64
}

// CachedTokenRatio 返回 cached_tokens / input_tokens，即该路由的真实缓存命中率。
func (u OpenAISchedulingRouteUsageSnapshot) CachedTokenRatio() float64 {
	if u.InputTokens <= 0 {
		return 0
	}
	return float64(u.CachedTokens) / float64(u.InputTokens)
}

// OpenAIStickySpilloverMetricsSnapshot 借道相关的进程内累计计数。
type OpenAIStickySpilloverMetricsSnapshot struct {
	// SecondaryReuseTotal 借道并复用了备用账号的次数。这是账号复用，不代表上游
	// Prompt Cache 命中；真实命中率见 RouteUsage。
	SecondaryReuseTotal int64
	// NewAccountTotal 借道到新账号的次数。
	NewAccountTotal int64
	// SwapTotal 主备互换次数（会话主绑定实际切换账号）。
	SwapTotal int64
	// BindingConflictTotal 并发借道时被丢弃的绑定更新次数。
	BindingConflictTotal int64
	// PrimaryWaitTotal 全部账号已满、回到主账号排队的次数。
	PrimaryWaitTotal int64
	// WaitReselectTotal 排队期间重新选号的次数；WaitQueueFullTotal 排队队列已满被拒次数。
	WaitReselectTotal  int64
	WaitQueueFullTotal int64
	// RouteUsage 按调度路由统计的上游真实缓存用量。
	RouteUsage map[string]OpenAISchedulingRouteUsageSnapshot
}

// SpillTotal 返回溢出（借道）总次数。
func (m OpenAIStickySpilloverMetricsSnapshot) SpillTotal() int64 {
	return m.SecondaryReuseTotal + m.NewAccountTotal
}

// SecondaryReuseRate 返回备用账号复用率 = 复用备用账号的借道 / 借道总数。
func (m OpenAIStickySpilloverMetricsSnapshot) SecondaryReuseRate() float64 {
	total := m.SpillTotal()
	if total <= 0 {
		return 0
	}
	return float64(m.SecondaryReuseTotal) / float64(total)
}

// SnapshotOpenAIStickySpilloverMetrics 返回借道计数快照。
func SnapshotOpenAIStickySpilloverMetrics() OpenAIStickySpilloverMetricsSnapshot {
	snapshot := OpenAIStickySpilloverMetricsSnapshot{
		SecondaryReuseTotal:  openAIStickySpilloverSecondaryReuseTotal.Load(),
		NewAccountTotal:      openAIStickySpilloverNewAccountTotal.Load(),
		SwapTotal:            openAIStickySpilloverSwapTotal.Load(),
		BindingConflictTotal: openAIStickySpilloverBindingConflictTotal.Load(),
		PrimaryWaitTotal:     openAIStickySpilloverPrimaryWaitTotal.Load(),
		WaitReselectTotal:    openAISchedulingWaitReselectTotal.Load(),
		WaitQueueFullTotal:   openAISchedulingWaitQueueFullTotal.Load(),
		RouteUsage:           make(map[string]OpenAISchedulingRouteUsageSnapshot, len(openAISchedulingRoutes)),
	}
	for _, route := range openAISchedulingRoutes {
		usage := openAISchedulingRouteUsage[route]
		snapshot.RouteUsage[route] = OpenAISchedulingRouteUsageSnapshot{
			Requests:         usage.requests.Load(),
			CacheHitRequests: usage.cacheHits.Load(),
			InputTokens:      usage.inputTokens.Load(),
			CachedTokens:     usage.cachedTokens.Load(),
		}
	}
	return snapshot
}

func recordOpenAIStickySpilloverEvent(secondaryReused, swapped, conflict bool) {
	if secondaryReused {
		openAIStickySpilloverSecondaryReuseTotal.Add(1)
	} else {
		openAIStickySpilloverNewAccountTotal.Add(1)
	}
	if swapped {
		openAIStickySpilloverSwapTotal.Add(1)
	}
	if conflict {
		openAIStickySpilloverBindingConflictTotal.Add(1)
	}
}

// RecordOpenAIAccountWaitReselect 记录一次「排队一个重选间隔后重新选号」。
func RecordOpenAIAccountWaitReselect() {
	openAISchedulingWaitReselectTotal.Add(1)
}

// RecordOpenAIAccountWaitQueueFull 记录一次「账号排队队列已满，请求被直接拒绝」。
func RecordOpenAIAccountWaitQueueFull() {
	openAISchedulingWaitQueueFullTotal.Add(1)
}

// ObserveOpenAIAccountWait 记录一次请求的排队总时长（跨多轮重选累计）及其结局。
func ObserveOpenAIAccountWait(outcome string, waited time.Duration) {
	if waited <= 0 {
		return
	}
	switch outcome {
	case OpenAIAccountWaitOutcomeAcquired, OpenAIAccountWaitOutcomeAcquiredOtherAccount, OpenAIAccountWaitOutcomeTimeout:
	default:
		outcome = OpenAIAccountWaitOutcomeError
	}
	openAISchedulingAccountWaitSeconds.WithLabelValues(outcome).Observe(waited.Seconds())
}

// OpenAISchedulingRouteOf 归纳一次选号结果的调度路由。
func OpenAISchedulingRouteOf(selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision) string {
	if selection != nil && selection.spillRoute != "" {
		return selection.spillRoute
	}
	switch {
	case decision.StickyPreviousHit:
		return OpenAISchedulingRoutePreviousResponse
	case decision.StickySessionHit:
		return OpenAISchedulingRouteStickyPrimary
	default:
		return OpenAISchedulingRouteLoadBalance
	}
}

// ObserveOpenAISchedulingRouteUsage 按调度路由累计上游返回的真实缓存用量。
// inputTokens 是上游 input_tokens（含缓存部分），cachedTokens 是其中的 cached_tokens。
// 没有上游 usage（inputTokens <= 0）的请求不计入，避免稀释命中率。
func ObserveOpenAISchedulingRouteUsage(route string, inputTokens, cachedTokens int) {
	usage, ok := openAISchedulingRouteUsage[route]
	if !ok || inputTokens <= 0 {
		return
	}
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > inputTokens {
		cachedTokens = inputTokens
	}
	usage.requests.Add(1)
	if cachedTokens > 0 {
		usage.cacheHits.Add(1)
	}
	usage.inputTokens.Add(int64(inputTokens))
	usage.cachedTokens.Add(int64(cachedTokens))
}
