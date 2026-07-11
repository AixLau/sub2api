package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contentModerationBatchKeySelector struct {
	service *ContentModerationService
	config  *ContentModerationConfig
}

func (s contentModerationBatchKeySelector) NextModerationAPIKey() (string, bool) {
	return s.service.nextUsableAPIKey(s.config)
}

func (s contentModerationBatchKeySelector) ModerationAPIKeySucceeded(key string) {
	s.service.markAPIKeySuccess(key, 0, http.StatusOK)
}

func (s contentModerationBatchKeySelector) ModerationAPIKeyFailed(key string, err error) {
	status := 0
	var providerErr *ModerationProviderError
	if errors.As(err, &providerErr) {
		status = providerErr.HTTPStatus
	}
	s.service.markAPIKeyError(key, err.Error(), 0, status)
}

func (s *ContentModerationService) incrementalModerationAvailable(content ContentModerationInput) bool {
	return s != nil && s.restrictedClientFactory != nil && len(content.Images) == 0 && len(content.Extraction.Sources) > 0
}

func (s *ContentModerationService) runIncrementalModeration(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput) (AggregatedModerationBatch, error) {
	stream, err := CanonicalizeModerationExtraction(content.Extraction)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	if !stream.Complete {
		return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorIncomplete, Err: errors.New("moderation extraction is incomplete")}
	}
	chunks, err := PlanModerationChunks(stream)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	if len(chunks) == 0 {
		return AggregatedModerationBatch{Level: ModerationLevelPass, Evidence: []ModerationBatchEvidence{}}, nil
	}

	feedbackEpoch := uint64(0)
	if s.feedbackEpochRepo != nil {
		feedbackEpoch, err = s.feedbackEpochRepo.GetModerationFeedbackEpoch(ctx)
		if err != nil {
			return AggregatedModerationBatch{}, fmt.Errorf("load moderation feedback epoch: %w", err)
		}
	}
	client, err := s.restrictedClientFactory.Client(cfg.BaseURL, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	var provider ModerationProvider
	if cfg.Provider == "zhipu" {
		provider, err = NewZhipuModerationProvider(cfg.BaseURL, client)
	} else {
		provider, err = NewOpenAIModerationProvider(cfg.BaseURL, cfg.Thresholds, client)
	}
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	_, policyScope, err := CanonicalLegacyModerationPolicyScope(LegacyModerationPolicy{
		Provider: cfg.Provider, BaseURL: cfg.BaseURL, Model: cfg.Model, AuditScope: cfg.AuditScope,
		Thresholds: cfg.Thresholds, Rules: legacyModerationRules(cfg.keywordRules()), EngineMode: cfg.EngineMode,
		ModelFilters: cfg.ModelFilter.Models, GroupFilters: cfg.GroupIDs, FailurePolicy: cfg.FailStrategy.Default,
		AdapterVersion: provider.AdapterVersion(), ExtractorVersion: "moderation-extractor-v1", ChunkerVersion: ModerationChunkerVersion,
		FeedbackEpoch: feedbackEpoch,
	})
	if err != nil {
		return AggregatedModerationBatch{}, err
	}

	cacheEnabled := cfg.PassCacheEnabled && s.passCache != nil && len(s.moderationCacheHMACKey) == sha256.Size && s.moderationCacheKeyVersion > 0
	cacheOpts := ContentModerationPassCacheOptions{Enabled: cacheEnabled, KeyVersion: s.moderationCacheKeyVersion, TTL: time.Duration(cfg.PassCacheTTLSeconds) * time.Second}
	ids := make([]string, len(chunks))
	for index, chunk := range chunks {
		if cacheEnabled {
			_, digest, identityErr := BuildModerationChunkIdentity(s.moderationCacheHMACKey, ModerationIdentityInput{
				KeyVersion: s.moderationCacheKeyVersion, FeedbackEpoch: feedbackEpoch, Provider: cfg.Provider,
				Model: cfg.Model, AuditScope: cfg.AuditScope, PolicyScope: policyScope, ChunkerVersion: ModerationChunkerVersion,
				ContextFrame: chunk.ContextFrame, NormalizedText: chunk.NormalizedText,
			})
			if identityErr != nil {
				return AggregatedModerationBatch{}, identityErr
			}
			ids[index] = hex.EncodeToString(digest)
		} else {
			ids[index] = fmt.Sprintf("fresh:%d", index)
		}
	}

	if cacheEnabled {
		requestKey := moderationRequestCacheKey(ids)
		quarantine, lookupErr := s.passCache.LookupQuarantine(ctx, cacheOpts, []string{requestKey})
		if lookupErr != nil {
			return AggregatedModerationBatch{}, fmt.Errorf("lookup moderation quarantine: %w", lookupErr)
		}
		if _, hit := quarantine[requestKey]; hit {
			return AggregatedModerationBatch{Level: ModerationLevelReview, RiskTypes: []string{"quarantine"}}, nil
		}
	}

	hits := map[string]bool{}
	if cacheEnabled {
		hits, err = s.passCache.LookupPASS(ctx, cacheOpts, ids)
		if err != nil {
			hits = map[string]bool{}
		}
	}
	evidence := make([]ModerationBatchEvidence, 0, len(chunks))
	misses := make([]ModerationBatchChunk, 0, len(chunks))
	for index, chunk := range chunks {
		if hits[ids[index]] {
			evidence = append(evidence, ModerationBatchEvidence{ChunkID: ids[index], Result: ProviderModerationResult{Level: ModerationLevelPass}})
			continue
		}
		misses = append(misses, ModerationBatchChunk{ID: ids[index], Text: chunk.Text})
	}
	if len(misses) > 0 {
		mode := ModerationBatchModeObserve
		if cfg.Mode == ContentModerationModePreBlock {
			mode = ModerationBatchModePreBlock
		}
		executor := ModerationBatchExecutor{
			Provider: provider, Keys: contentModerationBatchKeySelector{service: s, config: cfg}, Model: cfg.Model,
			BatchTimeout: ModerationBatchDefaultTimeout, CallTimeout: min(time.Duration(cfg.TimeoutMS)*time.Millisecond, ModerationBatchDefaultCallTimeout),
			MaxConcurrency: ModerationBatchDefaultConcurrency, MaxCalls: len(misses) * 2,
		}
		evidence = append(evidence, executor.Execute(ctx, misses, mode)...)
	}
	aggregated, err := AggregateModerationBatch(stream.Complete, ids, evidence)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	if cacheEnabled && aggregated.Level == ModerationLevelPass {
		freshKeys := make([]string, 0, len(misses))
		for _, miss := range misses {
			freshKeys = append(freshKeys, miss.ID)
		}
		s.passCache.StorePASS(ctx, cacheOpts, freshKeys)
	}
	_ = input // reserved for request-level comparison metadata in Task 8
	return aggregated, nil
}

func legacyModerationRules(rules []ContentModerationKeywordRule) []LegacyModerationRule {
	out := make([]LegacyModerationRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, LegacyModerationRule{Keyword: rule.Keyword, Category: rule.Category, Severity: rule.Severity, Action: rule.Action, Enabled: rule.Enabled})
	}
	return out
}

func moderationRequestCacheKey(chunkIDs []string) string {
	digest := sha256.Sum256([]byte(strings.Join(chunkIDs, "\n")))
	return hex.EncodeToString(digest[:])
}

func moderationAPIResultFromProvider(result ProviderModerationResult) *moderationAPIResult {
	scores := map[string]float64{}
	for category, score := range result.CategoryScores {
		scores[category] = score
	}
	if result.Level != ModerationLevelPass {
		category := strings.ToLower(string(result.Level))
		if len(result.RiskTypes) > 0 {
			category = result.RiskTypes[0]
		}
		scores[category] = 1
	}
	return &moderationAPIResult{Flagged: result.Level != ModerationLevelPass, CategoryScores: scores}
}

func (s *ContentModerationService) moderationCacheDegradedReason(cfg *ContentModerationConfig) string {
	if cfg == nil || !cfg.PassCacheEnabled {
		return ""
	}
	if s == nil || s.passCache == nil {
		return "cache_unavailable"
	}
	if len(s.moderationCacheHMACKey) != sha256.Size {
		return "hmac_key_unavailable"
	}
	if s.moderationCacheKeyVersion == 0 {
		return "hmac_key_version_invalid"
	}
	return ""
}
