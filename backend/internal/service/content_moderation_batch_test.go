package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type moderationBatchProviderStub struct {
	call func(context.Context, string, string, string) (ProviderModerationResult, error)
}

func (p moderationBatchProviderStub) Name() string           { return "stub" }
func (p moderationBatchProviderStub) AdapterVersion() string { return "stub-v1" }
func (p moderationBatchProviderStub) ModerateText(ctx context.Context, model, key, text string) (ProviderModerationResult, error) {
	return p.call(ctx, model, key, text)
}

type moderationBatchKeysStub struct {
	mu     sync.Mutex
	keys   []string
	cursor int
	failed int
}

func (s *moderationBatchKeysStub) NextModerationAPIKey() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.keys) == 0 {
		return "", false
	}
	key := s.keys[s.cursor%len(s.keys)]
	s.cursor++
	return key, true
}
func (*moderationBatchKeysStub) ModerationAPIKeySucceeded(string) {}
func (s *moderationBatchKeysStub) ModerationAPIKeyFailed(string, error) {
	s.mu.Lock()
	s.failed++
	s.mu.Unlock()
}

func TestModerationBatchExecutorBoundsConcurrencyAndAssociatesIDs(t *testing.T) {
	var active, peak atomic.Int64
	provider := moderationBatchProviderStub{call: func(_ context.Context, _, _, text string) (ProviderModerationResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if current <= old || peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		return ProviderModerationResult{Level: ModerationLevelPass, RiskTypes: []string{text}}, nil
	}}
	chunks := make([]ModerationBatchChunk, 12)
	for i := range chunks {
		chunks[i] = ModerationBatchChunk{ID: string(rune('a' + i)), Text: string(rune('A' + i))}
	}
	got := (ModerationBatchExecutor{Provider: provider, Keys: &moderationBatchKeysStub{keys: []string{"key"}}, Model: "model"}).Execute(context.Background(), chunks, ModerationBatchModeObserve)
	require.LessOrEqual(t, peak.Load(), int64(4))
	require.Len(t, got, len(chunks))
	for index := range chunks {
		require.Equal(t, chunks[index].ID, got[index].ChunkID)
		require.Equal(t, []string{chunks[index].Text}, got[index].Result.RiskTypes)
	}
}

func TestModerationBatchExecutorRetriesOnceAndHonorsCallBudget(t *testing.T) {
	var calls atomic.Int64
	provider := moderationBatchProviderStub{call: func(context.Context, string, string, string) (ProviderModerationResult, error) {
		calls.Add(1)
		return ProviderModerationResult{}, newModerationProviderError("stub", ModerationProviderErrorRateLimit, 429, errors.New("limited"))
	}}
	got := (ModerationBatchExecutor{Provider: provider, Keys: &moderationBatchKeysStub{keys: []string{"a", "b"}}, MaxConcurrency: 1, MaxCalls: 2}).Execute(context.Background(), []ModerationBatchChunk{{ID: "a"}, {ID: "b"}}, ModerationBatchModeObserve)
	require.Equal(t, int64(2), calls.Load())
	require.True(t, IsModerationBatchError(got[0].Err, ModerationBatchErrorRateLimit))
	require.True(t, IsModerationBatchError(got[1].Err, ModerationBatchErrorCallBudgetExceeded))
}

func TestModerationBatchExecutorCallTimeout(t *testing.T) {
	provider := moderationBatchProviderStub{call: func(ctx context.Context, _, _, _ string) (ProviderModerationResult, error) {
		<-ctx.Done()
		return ProviderModerationResult{}, ctx.Err()
	}}
	got := (ModerationBatchExecutor{Provider: provider, Keys: &moderationBatchKeysStub{keys: []string{"key"}}, CallTimeout: time.Millisecond}).Execute(context.Background(), []ModerationBatchChunk{{ID: "a"}}, ModerationBatchModeObserve)
	require.True(t, IsModerationBatchError(got[0].Err, ModerationBatchErrorTimeout))
}

func TestModerationBatchExplicitHitSurvivesCancellation(t *testing.T) {
	provider := moderationBatchProviderStub{call: func(ctx context.Context, _, _, text string) (ProviderModerationResult, error) {
		if text == "reject" {
			return ProviderModerationResult{Level: ModerationLevelReject, RiskTypes: []string{"risk"}}, nil
		}
		<-ctx.Done()
		return ProviderModerationResult{}, ctx.Err()
	}}
	chunks := []ModerationBatchChunk{{ID: "hit", Text: "reject"}, {ID: "slow", Text: "slow"}}
	evidence := (ModerationBatchExecutor{Provider: provider, Keys: &moderationBatchKeysStub{keys: []string{"key"}}, MaxConcurrency: 2}).Execute(context.Background(), chunks, ModerationBatchModePreBlock)
	got, err := AggregateModerationBatch(true, []string{"hit", "slow"}, evidence)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelReject, got.Level)
}

func TestAggregateModerationBatchStrictEvidence(t *testing.T) {
	pass := func(id string) ModerationBatchEvidence {
		return ModerationBatchEvidence{ChunkID: id, Result: ProviderModerationResult{Level: ModerationLevelPass}}
	}
	got, err := AggregateModerationBatch(true, []string{"a", "b"}, []ModerationBatchEvidence{pass("a"), pass("b")})
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, got.Level)

	_, err = AggregateModerationBatch(false, []string{"a"}, []ModerationBatchEvidence{pass("a")})
	require.True(t, IsModerationBatchError(err, ModerationBatchErrorIncomplete))
	_, err = AggregateModerationBatch(true, []string{"a"}, nil)
	require.True(t, IsModerationBatchError(err, ModerationBatchErrorMissingResult))
	_, err = AggregateModerationBatch(true, []string{"a"}, []ModerationBatchEvidence{pass("b")})
	require.True(t, IsModerationBatchError(err, ModerationBatchErrorUnexpectedID))
	_, err = AggregateModerationBatch(true, []string{"a"}, []ModerationBatchEvidence{pass("a"), pass("a")})
	require.True(t, IsModerationBatchError(err, ModerationBatchErrorDuplicateID))
}
