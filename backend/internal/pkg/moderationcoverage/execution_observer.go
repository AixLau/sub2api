package moderationcoverage

import (
	"sort"
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

type PipelineExecutionSnapshot struct {
	TotalCount             int64                               `json:"total_count"`
	ErrorCount             int64                               `json:"error_count"`
	RecentWindowSeconds    int64                               `json:"recent_window_seconds"`
	RecentWindowCount      int64                               `json:"recent_window_count"`
	RecentWindowErrorCount int64                               `json:"recent_window_error_count"`
	LastObservedAt         *time.Time                          `json:"last_observed_at,omitempty"`
	Executions             []PipelineStageExecutionObservation `json:"executions"`
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
		TotalCount:             pipelineExecutionObserver.totalCount,
		ErrorCount:             pipelineExecutionObserver.errorCount,
		RecentWindowSeconds:    int64(pipelineExecutionRecentWindow.Seconds()),
		RecentWindowCount:      recentCount,
		RecentWindowErrorCount: recentErrorCount,
		LastObservedAt:         lastSeen,
		Executions:             executions,
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
