package moderationcoverage

import (
	"sort"
	"sync"
	"time"
)

type PipelineStageExecutionObservation struct {
	Pipeline       string     `json:"pipeline"`
	Stage          string     `json:"stage"`
	Source         string     `json:"source"`
	Count          int64      `json:"count"`
	LastObservedAt *time.Time `json:"last_observed_at,omitempty"`
}

type PipelineExecutionSnapshot struct {
	TotalCount     int64                               `json:"total_count"`
	LastObservedAt *time.Time                          `json:"last_observed_at,omitempty"`
	Executions     []PipelineStageExecutionObservation `json:"executions"`
}

var pipelineExecutionObserver = struct {
	sync.Mutex
	observations map[string]PipelineStageExecutionObservation
	totalCount   int64
	lastSeen     *time.Time
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
	observation.Count++
	observation.LastObservedAt = &now
	pipelineExecutionObserver.observations[key] = observation
	pipelineExecutionObserver.totalCount++
	pipelineExecutionObserver.lastSeen = &now
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
	pipelineExecutionObserver.lastSeen = nil
	for _, observation := range observations {
		normalized := normalizePipelineStageExecution(PipelineStageExecution{
			Pipeline: observation.Pipeline,
			Stage:    observation.Stage,
			Source:   observation.Source,
		})
		if normalized.Pipeline == "" || normalized.Stage == "" || normalized.Source == "" || observation.Count <= 0 {
			continue
		}
		lastObservedAt := observation.LastObservedAt
		if lastObservedAt == nil {
			now := time.Now().UTC()
			lastObservedAt = &now
		}
		seeded := PipelineStageExecutionObservation{
			Pipeline:       normalized.Pipeline,
			Stage:          normalized.Stage,
			Source:         normalized.Source,
			Count:          observation.Count,
			LastObservedAt: lastObservedAt,
		}
		pipelineExecutionObserver.observations[pipelineStageExecutionKey(normalized)] = seeded
		pipelineExecutionObserver.totalCount += observation.Count
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
	executions := make([]PipelineStageExecutionObservation, 0, len(pipelineExecutionObserver.observations))
	for _, observation := range pipelineExecutionObserver.observations {
		executions = append(executions, clonePipelineStageExecutionObservation(observation))
	}
	sort.Slice(executions, func(i, j int) bool {
		if executions[i].Pipeline != executions[j].Pipeline {
			return executions[i].Pipeline < executions[j].Pipeline
		}
		leftStage := PipelineStageSortKey(executions[i].Stage)
		rightStage := PipelineStageSortKey(executions[j].Stage)
		if leftStage != rightStage {
			return leftStage < rightStage
		}
		return executions[i].Source < executions[j].Source
	})
	var lastSeen *time.Time
	if pipelineExecutionObserver.lastSeen != nil {
		t := *pipelineExecutionObserver.lastSeen
		lastSeen = &t
	}
	return PipelineExecutionSnapshot{
		TotalCount:     pipelineExecutionObserver.totalCount,
		LastObservedAt: lastSeen,
		Executions:     executions,
	}
}

func restorePipelineExecutionObserverLocked(snapshot PipelineExecutionSnapshot) {
	pipelineExecutionObserver.observations = make(map[string]PipelineStageExecutionObservation, len(snapshot.Executions))
	pipelineExecutionObserver.totalCount = snapshot.TotalCount
	pipelineExecutionObserver.lastSeen = nil
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
		})
		pipelineExecutionObserver.observations[key] = normalized
	}
}

func clonePipelineStageExecutionObservation(observation PipelineStageExecutionObservation) PipelineStageExecutionObservation {
	if observation.LastObservedAt != nil {
		t := *observation.LastObservedAt
		observation.LastObservedAt = &t
	}
	return observation
}
