package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ModerationBatchDefaultTimeout     = 8 * time.Second
	ModerationBatchDefaultCallTimeout = 3 * time.Second
	ModerationBatchDefaultConcurrency = 4
)

type ModerationBatchMode string

const (
	ModerationBatchModePreBlock ModerationBatchMode = "pre_block"
	ModerationBatchModeObserve  ModerationBatchMode = "observe"
)

type ModerationBatchErrorCode string

const (
	ModerationBatchErrorRateLimit          ModerationBatchErrorCode = "rate_limit"
	ModerationBatchErrorAuthExhausted      ModerationBatchErrorCode = "auth_exhausted"
	ModerationBatchErrorProvider           ModerationBatchErrorCode = "provider_error"
	ModerationBatchErrorSchema             ModerationBatchErrorCode = "schema_error"
	ModerationBatchErrorTimeout            ModerationBatchErrorCode = "timeout"
	ModerationBatchErrorCanceled           ModerationBatchErrorCode = "canceled"
	ModerationBatchErrorMissingResult      ModerationBatchErrorCode = "missing_result"
	ModerationBatchErrorDuplicateID        ModerationBatchErrorCode = "duplicate_id"
	ModerationBatchErrorUnexpectedID       ModerationBatchErrorCode = "unexpected_id"
	ModerationBatchErrorIncomplete         ModerationBatchErrorCode = "incomplete_extraction"
	ModerationBatchErrorCallBudgetExceeded ModerationBatchErrorCode = "call_budget_exceeded"
)

type ModerationBatchError struct {
	Code    ModerationBatchErrorCode
	ChunkID string
	Err     error
}

func (e *ModerationBatchError) Error() string {
	if e.ChunkID == "" {
		return fmt.Sprintf("moderation batch %s: %v", e.Code, e.Err)
	}
	return fmt.Sprintf("moderation batch %s for chunk %s: %v", e.Code, e.ChunkID, e.Err)
}

func (e *ModerationBatchError) Unwrap() error { return e.Err }

func IsModerationBatchError(err error, code ModerationBatchErrorCode) bool {
	var target *ModerationBatchError
	return errors.As(err, &target) && target.Code == code
}

type ModerationBatchChunk struct {
	ID   string
	Text string
}

type ModerationBatchEvidence struct {
	ChunkID string
	Result  ProviderModerationResult
	Err     error
}

type ModerationAPIKeySelector interface {
	NextModerationAPIKey() (string, bool)
	ModerationAPIKeySucceeded(key string)
	ModerationAPIKeyFailed(key string, err error)
}

type ModerationBatchExecutor struct {
	Provider       ModerationProvider
	Keys           ModerationAPIKeySelector
	Model          string
	BatchTimeout   time.Duration
	CallTimeout    time.Duration
	MaxConcurrency int
	MaxCalls       int
}

func (e ModerationBatchExecutor) Execute(ctx context.Context, chunks []ModerationBatchChunk, mode ModerationBatchMode) []ModerationBatchEvidence {
	if len(chunks) == 0 {
		return []ModerationBatchEvidence{}
	}
	batchTimeout := e.BatchTimeout
	if batchTimeout <= 0 {
		batchTimeout = ModerationBatchDefaultTimeout
	}
	callTimeout := e.CallTimeout
	if callTimeout <= 0 {
		callTimeout = ModerationBatchDefaultCallTimeout
	}
	workers := e.MaxConcurrency
	if workers <= 0 || workers > ModerationBatchDefaultConcurrency {
		workers = ModerationBatchDefaultConcurrency
	}
	maxCalls := e.MaxCalls
	if maxCalls <= 0 {
		maxCalls = len(chunks) * 2
	}

	batchCtx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()
	resultCtx, cancelForHit := context.WithCancelCause(batchCtx)
	defer cancelForHit(nil)

	type indexedChunk struct {
		index int
		chunk ModerationBatchChunk
	}
	jobs := make(chan indexedChunk)
	results := make([]ModerationBatchEvidence, len(chunks))
	var calls atomic.Int64
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for job := range jobs {
			evidence := ModerationBatchEvidence{ChunkID: job.chunk.ID}
			for attempt := 0; attempt < 2; attempt++ {
				if err := resultCtx.Err(); err != nil {
					evidence.Err = moderationBatchContextError(job.chunk.ID, resultCtx)
					break
				}
				if calls.Add(1) > int64(maxCalls) {
					evidence.Err = &ModerationBatchError{Code: ModerationBatchErrorCallBudgetExceeded, ChunkID: job.chunk.ID, Err: errors.New("request call budget exhausted")}
					break
				}
				key, ok := e.Keys.NextModerationAPIKey()
				if !ok {
					evidence.Err = &ModerationBatchError{Code: ModerationBatchErrorAuthExhausted, ChunkID: job.chunk.ID, Err: errors.New("no usable moderation API key")}
					break
				}
				callCtx, callCancel := context.WithTimeout(resultCtx, callTimeout)
				providerResult, err := e.Provider.ModerateText(callCtx, e.Model, key, job.chunk.Text)
				callCancel()
				if err == nil {
					e.Keys.ModerationAPIKeySucceeded(key)
					evidence.Result = providerResult
					if providerResult.Level == ModerationLevelReject || (providerResult.Level == ModerationLevelReview && mode == ModerationBatchModePreBlock) {
						cancelForHit(errors.New("explicit moderation hit"))
					}
					break
				}
				e.Keys.ModerationAPIKeyFailed(key, err)
				evidence.Err = moderationBatchProviderError(job.chunk.ID, err)
				if !moderationBatchRetryable(err) {
					break
				}
			}
			results[job.index] = evidence
		}
	}

	for i := 0; i < min(workers, len(chunks)); i++ {
		wg.Add(1)
		go worker()
	}
	for index, chunk := range chunks {
		select {
		case jobs <- indexedChunk{index: index, chunk: chunk}:
		case <-resultCtx.Done():
			for remaining := index; remaining < len(chunks); remaining++ {
				results[remaining] = ModerationBatchEvidence{ChunkID: chunks[remaining].ID, Err: moderationBatchContextError(chunks[remaining].ID, resultCtx)}
			}
			close(jobs)
			wg.Wait()
			return results
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

func moderationBatchRetryable(err error) bool {
	return IsModerationProviderError(err, ModerationProviderErrorAuth) ||
		IsModerationProviderError(err, ModerationProviderErrorRateLimit) ||
		IsModerationProviderError(err, ModerationProviderErrorHTTP) ||
		IsModerationProviderError(err, ModerationProviderErrorTransport)
}

func moderationBatchProviderError(chunkID string, err error) error {
	code := ModerationBatchErrorProvider
	switch {
	case IsModerationProviderError(err, ModerationProviderErrorRateLimit):
		code = ModerationBatchErrorRateLimit
	case IsModerationProviderError(err, ModerationProviderErrorAuth):
		code = ModerationBatchErrorAuthExhausted
	case IsModerationProviderError(err, ModerationProviderErrorSchema):
		code = ModerationBatchErrorSchema
	case IsModerationProviderError(err, ModerationProviderErrorTimeout), errors.Is(err, context.DeadlineExceeded):
		code = ModerationBatchErrorTimeout
	case errors.Is(err, context.Canceled):
		code = ModerationBatchErrorCanceled
	}
	return &ModerationBatchError{Code: code, ChunkID: chunkID, Err: err}
}

func moderationBatchContextError(chunkID string, ctx context.Context) error {
	code := ModerationBatchErrorCanceled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		code = ModerationBatchErrorTimeout
	}
	return &ModerationBatchError{Code: code, ChunkID: chunkID, Err: ctx.Err()}
}

type AggregatedModerationBatch struct {
	Level     ModerationLevel
	RiskTypes []string
	Evidence  []ModerationBatchEvidence
}

func AggregateModerationBatch(complete bool, requiredIDs []string, evidence []ModerationBatchEvidence) (AggregatedModerationBatch, error) {
	if !complete {
		return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorIncomplete, Err: errors.New("moderation extraction is incomplete")}
	}
	required := make(map[string]struct{}, len(requiredIDs))
	for _, id := range requiredIDs {
		if _, exists := required[id]; exists {
			return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorDuplicateID, ChunkID: id, Err: errors.New("duplicate required chunk ID")}
		}
		required[id] = struct{}{}
	}
	byID := make(map[string]ModerationBatchEvidence, len(evidence))
	best := ModerationLevelPass
	var explicitHit *ModerationBatchEvidence
	for _, item := range evidence {
		if _, ok := required[item.ChunkID]; !ok {
			return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorUnexpectedID, ChunkID: item.ChunkID, Err: errors.New("unexpected chunk evidence")}
		}
		if _, exists := byID[item.ChunkID]; exists {
			return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorDuplicateID, ChunkID: item.ChunkID, Err: errors.New("duplicate chunk evidence")}
		}
		byID[item.ChunkID] = item
		if item.Err == nil && (item.Result.Level == ModerationLevelReject || item.Result.Level == ModerationLevelReview) {
			candidate := item
			if explicitHit == nil || candidate.Result.Level == ModerationLevelReject {
				explicitHit = &candidate
			}
			if candidate.Result.Level == ModerationLevelReject {
				best = ModerationLevelReject
			} else if best == ModerationLevelPass {
				best = ModerationLevelReview
			}
		}
	}
	if explicitHit != nil {
		return AggregatedModerationBatch{Level: best, RiskTypes: append([]string(nil), explicitHit.Result.RiskTypes...), Evidence: append([]ModerationBatchEvidence(nil), evidence...)}, nil
	}
	for _, id := range requiredIDs {
		item, ok := byID[id]
		if !ok {
			return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorMissingResult, ChunkID: id, Err: errors.New("missing chunk evidence")}
		}
		if item.Err != nil {
			return AggregatedModerationBatch{}, item.Err
		}
		if item.Result.Level != ModerationLevelPass {
			return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorMissingResult, ChunkID: id, Err: errors.New("invalid provider result level")}
		}
	}
	return AggregatedModerationBatch{Level: ModerationLevelPass, Evidence: append([]ModerationBatchEvidence(nil), evidence...)}, nil
}
