package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type incrementalClientFactoryStub struct{ client *http.Client }

func (f incrementalClientFactoryStub) Client(string, time.Duration) (*http.Client, error) {
	return f.client, nil
}

type incrementalEpochStub struct{ value uint64 }

func (e incrementalEpochStub) GetModerationFeedbackEpoch(context.Context) (uint64, error) {
	return e.value, nil
}
func (e incrementalEpochStub) IncrementModerationFeedbackEpoch(context.Context) (uint64, error) {
	return e.value + 1, nil
}

type incrementalPassCacheStub struct {
	mu        sync.Mutex
	pass      map[string]bool
	lookupErr error
	lookups   int
	stores    int
}

func (c *incrementalPassCacheStub) LookupPASS(_ context.Context, _ ContentModerationPassCacheOptions, keys []string) (map[string]bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lookups++
	if c.lookupErr != nil {
		return map[string]bool{"partial-must-not-be-used": true}, c.lookupErr
	}
	out := map[string]bool{}
	for _, key := range keys {
		if c.pass[key] {
			out[key] = true
		}
	}
	return out, nil
}
func (c *incrementalPassCacheStub) StorePASS(_ context.Context, _ ContentModerationPassCacheOptions, keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stores++
	for _, key := range keys {
		c.pass[key] = true
	}
}
func (*incrementalPassCacheStub) DeletePASS(context.Context, ContentModerationPassCacheOptions, []string) error {
	return nil
}
func (*incrementalPassCacheStub) LookupQuarantine(context.Context, ContentModerationPassCacheOptions, []string) (map[string]ContentModerationQuarantineEntry, error) {
	return map[string]ContentModerationQuarantineEntry{}, nil
}
func (*incrementalPassCacheStub) StoreQuarantine(context.Context, ContentModerationPassCacheOptions, map[string]ContentModerationQuarantineEntry) error {
	return nil
}
func (*incrementalPassCacheStub) DeleteQuarantine(context.Context, ContentModerationPassCacheOptions, []string) error {
	return nil
}
func (*incrementalPassCacheStub) GetComparisonMetadata(context.Context, string) (*ContentModerationComparisonMetadata, error) {
	return nil, nil
}
func (*incrementalPassCacheStub) StoreComparisonMetadata(context.Context, string, ContentModerationComparisonMetadata) error {
	return nil
}
func (*incrementalPassCacheStub) DeleteComparisonMetadata(context.Context, string) error { return nil }

func TestContentModerationIncrementalRepeatUsesPASSCache(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"risk_level":"PASS","risk_types":[]}]}`)), Request: req}, nil
	})}
	cache := &incrementalPassCacheStub{pass: map[string]bool{}}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(cache, incrementalEpochStub{value: 3}, incrementalClientFactoryStub{client: client}, []byte(strings.Repeat("k", 32)), 1)
	cfg := defaultContentModerationConfig()
	cfg.Provider = "zhipu"
	cfg.BaseURL = "https://open.bigmodel.cn/api"
	cfg.Model = "moderation"
	cfg.APIKeys = []string{"key"}
	cfg.PassCacheEnabled = true
	text := strings.Repeat("界", 2000)
	content := ContentModerationInput{Extraction: ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Source: "messages[0]", Role: "user", Text: text}}}}

	first, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, first.Level)
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, 1, cache.stores)

	second, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, second.Level)
	require.Equal(t, int64(2), calls.Load(), "all-hit repeat must not call the provider")
	require.Equal(t, 2, cache.lookups)
}

func TestContentModerationIncrementalCacheReadErrorAuditsEveryChunk(t *testing.T) {
	var calls atomic.Int64
	client := &http.Client{Transport: moderationRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"results":[{"risk_level":"PASS","risk_types":[]}]}`)), Request: req}, nil
	})}
	cache := &incrementalPassCacheStub{pass: map[string]bool{}, lookupErr: errors.New("redis unavailable")}
	svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil)
	svc.SetIncrementalModerationDependencies(cache, incrementalEpochStub{}, incrementalClientFactoryStub{client: client}, []byte(strings.Repeat("k", 32)), 1)
	cfg := defaultContentModerationConfig()
	cfg.Provider, cfg.BaseURL, cfg.Model = "zhipu", "https://open.bigmodel.cn/api", "moderation"
	cfg.APIKeys, cfg.PassCacheEnabled = []string{"key"}, true
	content := ContentModerationInput{Extraction: ModerationExtraction{Complete: true, Sources: []ModerationTextSource{{Source: "message", Role: "user", Text: strings.Repeat("界", 2000)}}}}

	got, err := svc.runIncrementalModeration(context.Background(), ContentModerationCheckInput{}, cfg, content)
	require.NoError(t, err)
	require.Equal(t, ModerationLevelPass, got.Level)
	require.Equal(t, int64(2), calls.Load())
}
