package moderationcoverage

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type PipelineStageExecutionObservation struct {
	Pipeline         string     `json:"pipeline"`
	Stage            string     `json:"stage"`
	Source           string     `json:"source"`
	Method           string     `json:"method,omitempty"`
	Path             string     `json:"path,omitempty"`
	Handler          string     `json:"handler,omitempty"`
	Protocol         string     `json:"protocol,omitempty"`
	Count            int64      `json:"count"`
	ErrorCount       int64      `json:"error_count"`
	RecentCount      int64      `json:"recent_count"`
	RecentErrorCount int64      `json:"recent_error_count"`
	LastObservedAt   *time.Time `json:"last_observed_at,omitempty"`
}

type PipelineStageRouteExecutionObservation struct {
	Pipeline         string                              `json:"pipeline"`
	Method           string                              `json:"method,omitempty"`
	Path             string                              `json:"path,omitempty"`
	Handler          string                              `json:"handler,omitempty"`
	Protocol         string                              `json:"protocol,omitempty"`
	Count            int64                               `json:"count"`
	ErrorCount       int64                               `json:"error_count"`
	RecentCount      int64                               `json:"recent_count"`
	RecentErrorCount int64                               `json:"recent_error_count"`
	LastObservedAt   *time.Time                          `json:"last_observed_at,omitempty"`
	Stages           []PipelineStageExecutionObservation `json:"stages"`
}

type PipelineExecutionSnapshot struct {
	TotalCount               int64                                     `json:"total_count"`
	ErrorCount               int64                                     `json:"error_count"`
	RecentWindowSeconds      int64                                     `json:"recent_window_seconds"`
	RecentWindowCount        int64                                     `json:"recent_window_count"`
	RecentWindowErrorCount   int64                                     `json:"recent_window_error_count"`
	LastObservedAt           *time.Time                                `json:"last_observed_at,omitempty"`
	Executions               []PipelineStageExecutionObservation       `json:"executions"`
	Routes                   []PipelineStageRouteExecutionObservation  `json:"routes"`
	StageObservationCoverage PipelineExecutionStageObservationCoverage `json:"stage_observation_coverage"`
}

type PipelineExecutionStageObservationCoverage struct {
	Status           string   `json:"status"`
	ExpectedStages   int      `json:"expected_stages"`
	ObservedStages   int      `json:"observed_stages"`
	UnobservedStages []string `json:"unobserved_stages"`
}

const (
	pipelineExecutionRecentWindow = 5 * time.Minute
	pipelineExecutionMaxEvents    = 4096
)

type pipelineStageExecutionEvent struct {
	ObservedAt time.Time
	Count      int64
	ErrorCount int64
	Execution  PipelineStageExecution
}

var pipelineExecutionObserver = struct {
	sync.Mutex
	observations map[string]PipelineStageExecutionObservation
	totalCount   int64
	errorCount   int64
	lastSeen     *time.Time
	events       []pipelineStageExecutionEvent
}{
	observations: make(map[string]PipelineStageExecutionObservation),
}

func recordPipelineStageExecution(execution PipelineStageExecution) {
	execution = normalizePipelineStageExecution(execution)
	if execution.Pipeline == "" || execution.Stage == "" || execution.Source == "" {
		return
	}
	now := time.Now().UTC()
	key := pipelineStageExecutionKey(execution)

	pipelineExecutionObserver.Lock()
	defer pipelineExecutionObserver.Unlock()

	observation := pipelineExecutionObserver.observations[key]
	observation.Pipeline = execution.Pipeline
	observation.Stage = execution.Stage
	observation.Source = execution.Source
	observation.Method = execution.Method
	observation.Path = execution.Path
	observation.Handler = execution.Handler
	observation.Protocol = execution.Protocol
	observation.Count++
	if execution.Error {
		observation.ErrorCount++
	}
	observation.LastObservedAt = &now
	pipelineExecutionObserver.observations[key] = observation
	pipelineExecutionObserver.totalCount++
	if execution.Error {
		pipelineExecutionObserver.errorCount++
	}
	pipelineExecutionObserver.lastSeen = &now
	appendPipelineStageExecutionEventLocked(pipelineStageExecutionEvent{
		ObservedAt: now,
		Count:      1,
		ErrorCount: boolToInt64(execution.Error),
		Execution:  execution,
	})
}

func PipelineExecutionObserverSnapshot() PipelineExecutionSnapshot {
	pipelineExecutionObserver.Lock()
	defer pipelineExecutionObserver.Unlock()
	return pipelineExecutionObserverSnapshotLocked()
}

func ReplacePipelineExecutionObserverForTest(observations []PipelineStageExecutionObservation) func() {
	pipelineExecutionObserver.Lock()
	previous := pipelineExecutionObserverSnapshotLocked()
	pipelineExecutionObserver.observations = make(map[string]PipelineStageExecutionObservation, len(observations))
	pipelineExecutionObserver.totalCount = 0
	pipelineExecutionObserver.errorCount = 0
	pipelineExecutionObserver.lastSeen = nil
	pipelineExecutionObserver.events = nil
	for _, observation := range observations {
		normalized := normalizePipelineStageExecution(PipelineStageExecution{
			Pipeline: observation.Pipeline,
			Stage:    observation.Stage,
			Source:   observation.Source,
			Method:   observation.Method,
			Path:     observation.Path,
			Handler:  observation.Handler,
			Protocol: observation.Protocol,
		})
		if normalized.Pipeline == "" || normalized.Stage == "" || normalized.Source == "" || observation.Count <= 0 {
			continue
		}
		if observation.ErrorCount < 0 {
			observation.ErrorCount = 0
		}
		if observation.ErrorCount > observation.Count {
			observation.ErrorCount = observation.Count
		}
		lastObservedAt := observation.LastObservedAt
		if lastObservedAt == nil {
			now := time.Now().UTC()
			lastObservedAt = &now
		}
		key := pipelineStageExecutionKey(normalized)
		seeded := PipelineStageExecutionObservation{
			Pipeline:       normalized.Pipeline,
			Stage:          normalized.Stage,
			Source:         normalized.Source,
			Method:         normalized.Method,
			Path:           normalized.Path,
			Handler:        normalized.Handler,
			Protocol:       normalized.Protocol,
			Count:          observation.Count,
			ErrorCount:     observation.ErrorCount,
			LastObservedAt: lastObservedAt,
		}
		if existing, ok := pipelineExecutionObserver.observations[key]; ok {
			seeded.Count += existing.Count
			seeded.ErrorCount += existing.ErrorCount
			if existing.LastObservedAt != nil && existing.LastObservedAt.After(*seeded.LastObservedAt) {
				t := *existing.LastObservedAt
				seeded.LastObservedAt = &t
			}
		}
		pipelineExecutionObserver.observations[key] = seeded
		pipelineExecutionObserver.totalCount += observation.Count
		pipelineExecutionObserver.errorCount += observation.ErrorCount
		appendPipelineStageExecutionEventLocked(pipelineStageExecutionEvent{
			ObservedAt: *lastObservedAt,
			Count:      observation.Count,
			ErrorCount: observation.ErrorCount,
			Execution:  normalized,
		})
		if pipelineExecutionObserver.lastSeen == nil || lastObservedAt.After(*pipelineExecutionObserver.lastSeen) {
			t := *lastObservedAt
			pipelineExecutionObserver.lastSeen = &t
		}
	}
	pipelineExecutionObserver.Unlock()

	return func() {
		pipelineExecutionObserver.Lock()
		defer pipelineExecutionObserver.Unlock()
		restorePipelineExecutionObserverLocked(previous)
	}
}

func ResetPipelineExecutionObserverForTest() func() {
	return ReplacePipelineExecutionObserverForTest(nil)
}

func pipelineExecutionObserverSnapshotLocked() PipelineExecutionSnapshot {
	now := time.Now().UTC()
	recentByKey := pipelineExecutionRecentCountsByKeyLocked(now)
	executions := make([]PipelineStageExecutionObservation, 0, len(pipelineExecutionObserver.observations))
	for _, observation := range pipelineExecutionObserver.observations {
		clone := clonePipelineStageExecutionObservation(observation)
		key := pipelineStageExecutionKey(PipelineStageExecution{
			Pipeline: clone.Pipeline,
			Stage:    clone.Stage,
			Source:   clone.Source,
			Method:   clone.Method,
			Path:     clone.Path,
			Handler:  clone.Handler,
			Protocol: clone.Protocol,
		})
		if recent, ok := recentByKey[key]; ok {
			clone.RecentCount = recent.count
			clone.RecentErrorCount = recent.errorCount
		} else {
			clone.RecentCount = 0
			clone.RecentErrorCount = 0
		}
		executions = append(executions, clone)
	}
	sort.Slice(executions, func(i, j int) bool {
		return pipelineStageExecutionObservationLess(executions[i], executions[j])
	})
	var lastSeen *time.Time
	if pipelineExecutionObserver.lastSeen != nil {
		t := *pipelineExecutionObserver.lastSeen
		lastSeen = &t
	}
	recentCount, recentErrorCount := pipelineExecutionRecentCountsFromRecentByKey(recentByKey)
	return PipelineExecutionSnapshot{
		TotalCount:               pipelineExecutionObserver.totalCount,
		ErrorCount:               pipelineExecutionObserver.errorCount,
		RecentWindowSeconds:      int64(pipelineExecutionRecentWindow.Seconds()),
		RecentWindowCount:        recentCount,
		RecentWindowErrorCount:   recentErrorCount,
		LastObservedAt:           lastSeen,
		Executions:               executions,
		Routes:                   pipelineExecutionRouteObservationsFromExecutions(executions),
		StageObservationCoverage: pipelineExecutionStageObservationCoverageFromExecutions(executions),
	}
}

func restorePipelineExecutionObserverLocked(snapshot PipelineExecutionSnapshot) {
	pipelineExecutionObserver.observations = make(map[string]PipelineStageExecutionObservation, len(snapshot.Executions))
	pipelineExecutionObserver.totalCount = snapshot.TotalCount
	pipelineExecutionObserver.errorCount = snapshot.ErrorCount
	pipelineExecutionObserver.lastSeen = nil
	pipelineExecutionObserver.events = nil
	if snapshot.LastObservedAt != nil {
		t := *snapshot.LastObservedAt
		pipelineExecutionObserver.lastSeen = &t
	}
	for _, observation := range snapshot.Executions {
		normalized := clonePipelineStageExecutionObservation(observation)
		key := pipelineStageExecutionKey(PipelineStageExecution{
			Pipeline: normalized.Pipeline,
			Stage:    normalized.Stage,
			Source:   normalized.Source,
			Method:   normalized.Method,
			Path:     normalized.Path,
			Handler:  normalized.Handler,
			Protocol: normalized.Protocol,
		})
		pipelineExecutionObserver.observations[key] = normalized
		if normalized.LastObservedAt != nil {
			appendPipelineStageExecutionEventLocked(pipelineStageExecutionEvent{
				ObservedAt: *normalized.LastObservedAt,
				Count:      normalized.Count,
				ErrorCount: normalized.ErrorCount,
				Execution: PipelineStageExecution{
					Pipeline: normalized.Pipeline,
					Stage:    normalized.Stage,
					Source:   normalized.Source,
					Method:   normalized.Method,
					Path:     normalized.Path,
					Handler:  normalized.Handler,
					Protocol: normalized.Protocol,
				},
			})
		}
	}
}

type pipelineExecutionRecentCounts struct {
	count      int64
	errorCount int64
}

func clonePipelineStageExecutionObservation(observation PipelineStageExecutionObservation) PipelineStageExecutionObservation {
	if observation.LastObservedAt != nil {
		t := *observation.LastObservedAt
		observation.LastObservedAt = &t
	}
	return observation
}

func appendPipelineStageExecutionEventLocked(event pipelineStageExecutionEvent) {
	pipelineExecutionObserver.events = append(pipelineExecutionObserver.events, event)
	if overflow := len(pipelineExecutionObserver.events) - pipelineExecutionMaxEvents; overflow > 0 {
		pipelineExecutionObserver.events = pipelineExecutionObserver.events[overflow:]
	}
}

func pipelineExecutionRecentCountsLocked(now time.Time) (int64, int64) {
	return pipelineExecutionRecentCountsFromRecentByKey(pipelineExecutionRecentCountsByKeyLocked(now))
}

func pipelineExecutionRecentCountsByKeyLocked(now time.Time) map[string]pipelineExecutionRecentCounts {
	cutoff := now.Add(-pipelineExecutionRecentWindow)
	recentByKey := make(map[string]pipelineExecutionRecentCounts)
	for _, event := range pipelineExecutionObserver.events {
		if event.ObservedAt.Before(cutoff) {
			continue
		}
		key := pipelineStageExecutionKey(event.Execution)
		if key == "" {
			continue
		}
		recent := recentByKey[key]
		recent.count += event.Count
		recent.errorCount += event.ErrorCount
		recentByKey[key] = recent
	}
	return recentByKey
}

func pipelineExecutionRecentCountsFromRecentByKey(recentByKey map[string]pipelineExecutionRecentCounts) (int64, int64) {
	var count int64
	var errorCount int64
	for _, recent := range recentByKey {
		count += recent.count
		errorCount += recent.errorCount
	}
	return count, errorCount
}

func pipelineExecutionRouteObservationsFromExecutions(executions []PipelineStageExecutionObservation) []PipelineStageRouteExecutionObservation {
	byRoute := make(map[string]*PipelineStageRouteExecutionObservation)
	for _, execution := range executions {
		execution = clonePipelineStageExecutionObservation(execution)
		key := pipelineStageExecutionRouteKey(execution)
		if key == "" {
			continue
		}
		route := byRoute[key]
		if route == nil {
			route = &PipelineStageRouteExecutionObservation{
				Pipeline: execution.Pipeline,
				Method:   execution.Method,
				Path:     execution.Path,
				Handler:  execution.Handler,
				Protocol: execution.Protocol,
				Stages:   make([]PipelineStageExecutionObservation, 0),
			}
			byRoute[key] = route
		}
		route.Count += execution.Count
		route.ErrorCount += execution.ErrorCount
		route.RecentCount += execution.RecentCount
		route.RecentErrorCount += execution.RecentErrorCount
		if execution.LastObservedAt != nil && (route.LastObservedAt == nil || execution.LastObservedAt.After(*route.LastObservedAt)) {
			t := *execution.LastObservedAt
			route.LastObservedAt = &t
		}
		route.Stages = append(route.Stages, execution)
	}

	routes := make([]PipelineStageRouteExecutionObservation, 0, len(byRoute))
	for _, route := range byRoute {
		sort.Slice(route.Stages, func(i, j int) bool {
			return pipelineStageExecutionObservationLess(route.Stages[i], route.Stages[j])
		})
		routes = append(routes, *route)
	}
	sort.Slice(routes, func(i, j int) bool {
		return pipelineRouteExecutionObservationLess(routes[i], routes[j])
	})
	return routes
}

func pipelineExecutionStageObservationCoverageFromExecutions(executions []PipelineStageExecutionObservation) PipelineExecutionStageObservationCoverage {
	observed := make(map[string]struct{}, len(executions))
	for _, execution := range executions {
		execution = clonePipelineStageExecutionObservation(execution)
		key := pipelineStageExecutionObservationCoverageKey(execution.Pipeline, execution.Method, execution.Path, execution.Handler, execution.Protocol, execution.Stage)
		if key == "" {
			continue
		}
		observed[key] = struct{}{}
	}

	expected := make(map[string]string)
	registry.Lock()
	entries := entriesSnapshotLocked()
	registry.Unlock()
	for _, entry := range entries {
		entry = NormalizeEntry(entry)
		if !entry.Upstream || !entry.ModerationRequired || entry.Pipeline == "" || entry.Status != StatusCovered {
			continue
		}
		for _, stage := range entry.StageCoverage {
			stage.Stage = NormalizeStage(stage.Stage)
			if !stage.Required || !stage.Covered || stage.Stage == "" {
				continue
			}
			key := pipelineStageExecutionObservationCoverageKey(entry.Pipeline, entry.Method, entry.Path, entry.Handler, entry.Protocol, stage.Stage)
			if key == "" {
				continue
			}
			expected[key] = strings.TrimSpace(strings.Join([]string{
				entry.Method,
				entry.Path,
				entry.Handler,
				stage.Stage,
			}, " "))
		}
	}

	unobserved := make([]string, 0)
	observedExpected := 0
	for key, label := range expected {
		if _, ok := observed[key]; ok {
			observedExpected++
			continue
		}
		unobserved = append(unobserved, label)
	}
	sort.Strings(unobserved)
	status := "not_applicable"
	if len(expected) > 0 {
		status = "covered"
	}
	if len(unobserved) > 0 {
		status = "mismatch"
	}
	return PipelineExecutionStageObservationCoverage{
		Status:           status,
		ExpectedStages:   len(expected),
		ObservedStages:   observedExpected,
		UnobservedStages: unobserved,
	}
}

func pipelineStageExecutionObservationCoverageKey(pipeline, method, path, handler, protocol, stage string) string {
	pipeline = NormalizePipeline(pipeline)
	method = NormalizeMethod(method)
	path = NormalizePath(path)
	handler = strings.TrimSpace(handler)
	protocol = strings.TrimSpace(protocol)
	stage = NormalizeStage(stage)
	if pipeline == "" || method == "" || path == "" || handler == "" || protocol == "" || stage == "" {
		return ""
	}
	return strings.Join([]string{pipeline, method, path, handler, protocol, stage}, "\x00")
}

func pipelineStageExecutionRouteKey(execution PipelineStageExecutionObservation) string {
	return strings.Join([]string{
		NormalizePipeline(execution.Pipeline),
		NormalizeMethod(execution.Method),
		NormalizePath(execution.Path),
		strings.TrimSpace(execution.Handler),
		strings.TrimSpace(execution.Protocol),
	}, "\x00")
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func pipelineStageExecutionObservationLess(left, right PipelineStageExecutionObservation) bool {
	return pipelineStageExecutionLess(
		PipelineStageExecution{
			Pipeline: left.Pipeline,
			Stage:    left.Stage,
			Source:   left.Source,
			Method:   left.Method,
			Path:     left.Path,
			Handler:  left.Handler,
			Protocol: left.Protocol,
		},
		PipelineStageExecution{
			Pipeline: right.Pipeline,
			Stage:    right.Stage,
			Source:   right.Source,
			Method:   right.Method,
			Path:     right.Path,
			Handler:  right.Handler,
			Protocol: right.Protocol,
		},
	)
}

func pipelineRouteExecutionObservationLess(left, right PipelineStageRouteExecutionObservation) bool {
	return pipelineStageExecutionLess(
		PipelineStageExecution{
			Pipeline: left.Pipeline,
			Method:   left.Method,
			Path:     left.Path,
			Handler:  left.Handler,
			Protocol: left.Protocol,
		},
		PipelineStageExecution{
			Pipeline: right.Pipeline,
			Method:   right.Method,
			Path:     right.Path,
			Handler:  right.Handler,
			Protocol: right.Protocol,
		},
	)
}
