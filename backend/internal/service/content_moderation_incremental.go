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
	started := time.Now()
	cacheState := "cold"
	finalLevel := ModerationLevel("")
	chunkCount := 0
	defer func() {
		if s.metrics != nil {
			s.metrics.observeRequest(cfg.Mode, cacheState, finalLevel, started, chunkCount)
		}
	}()
	stream, err := CanonicalizeModerationExtraction(content.Extraction)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	if !stream.Complete {
		reasons := strings.Join(content.Extraction.TruncateReasons, ",")
		if reasons == "" {
			reasons = "unknown"
		}
		return AggregatedModerationBatch{}, &ModerationBatchError{Code: ModerationBatchErrorIncomplete, Err: fmt.Errorf("moderation extraction is incomplete: %s", reasons)}
	}
	chunks, err := PlanModerationChunks(stream)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	if len(chunks) == 0 {
		finalLevel = ModerationLevelPass
		return AggregatedModerationBatch{Level: ModerationLevelPass, Evidence: []ModerationBatchEvidence{}}, nil
	}
	chunkCount = len(chunks)

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
			if s.metrics != nil {
				s.metrics.cache.WithLabelValues("quarantine_read", "error").Inc()
			}
			return AggregatedModerationBatch{}, fmt.Errorf("lookup moderation quarantine: %w", lookupErr)
		}
		if _, hit := quarantine[requestKey]; hit {
			if s.metrics != nil {
				s.metrics.cache.WithLabelValues("quarantine_read", "hit").Inc()
			}
			finalLevel = ModerationLevelReview
			return AggregatedModerationBatch{Level: ModerationLevelReview, RiskTypes: []string{"quarantine"}}, nil
		}
	}

	hits := map[string]bool{}
	if cacheEnabled {
		hits, err = s.passCache.LookupPASS(ctx, cacheOpts, ids)
		if err != nil {
			if s.metrics != nil {
				s.metrics.cache.WithLabelValues("pass_read", "error").Inc()
			}
			hits = map[string]bool{}
		} else if s.metrics != nil {
			s.metrics.cache.WithLabelValues("pass_read", "success").Inc()
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
	if len(hits) == len(chunks) {
		cacheState = "all_hit"
	} else if len(hits) > 0 {
		cacheState = "incremental"
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
		if s.metrics != nil {
			for _, item := range evidence[len(evidence)-len(misses):] {
				result := "success"
				if item.Err != nil {
					result = "error"
				}
				s.metrics.providerCalls.WithLabelValues(result).Inc()
			}
		}
	}
	aggregated, err := AggregateModerationBatch(stream.Complete, ids, evidence)
	if err != nil {
		return AggregatedModerationBatch{}, err
	}
	finalLevel = aggregated.Level
	if s.metrics != nil && strings.EqualFold(strings.TrimSpace(input.Provider), "openai") {
		s.metrics.forwardedEvidence.WithLabelValues(boundedModerationAggregate(aggregated.Level)).Inc()
	}
	if cacheEnabled && aggregated.Level == ModerationLevelPass {
		freshKeys := make([]string, 0, len(misses))
		for _, miss := range misses {
			freshKeys = append(freshKeys, miss.ID)
		}
		s.passCache.StorePASS(ctx, cacheOpts, freshKeys)
		if s.metrics != nil && len(freshKeys) > 0 {
			s.metrics.cache.WithLabelValues("pass_write", "attempt").Add(float64(len(freshKeys)))
		}
	}
	if cacheState == "all_hit" && cfg.Mode == ContentModerationModeObserve && shouldForceFreshModeration(input.RequestID) {
		freshChunks := make([]ModerationBatchChunk, len(chunks))
		for index, chunk := range chunks {
			freshChunks[index] = ModerationBatchChunk{ID: ids[index], Text: chunk.Text}
		}
		executor := ModerationBatchExecutor{Provider: provider, Keys: contentModerationBatchKeySelector{service: s, config: cfg}, Model: cfg.Model, BatchTimeout: ModerationBatchDefaultTimeout, CallTimeout: min(time.Duration(cfg.TimeoutMS)*time.Millisecond, ModerationBatchDefaultCallTimeout), MaxConcurrency: ModerationBatchDefaultConcurrency, MaxCalls: len(freshChunks) * 2}
		fresh, freshErr := AggregateModerationBatch(true, ids, executor.Execute(ctx, freshChunks, ModerationBatchModeObserve))
		if s.metrics != nil {
			result := "provider_error"
			if freshErr == nil && fresh.Level == aggregated.Level {
				result = "equivalent"
			} else if freshErr == nil {
				result = "mismatch"
			}
			s.metrics.forcedFresh.WithLabelValues(result).Inc()
		}
	}
	if cacheEnabled && cfg.Provider == "zhipu" && aggregated.Level == ModerationLevelPass && strings.TrimSpace(input.RequestID) != "" {
		decisionID := contentModerationDecisionID(input, nil, "")
		if decisionID != "" {
			now := time.Now()
			metadata := ContentModerationComparisonMetadata{
				RequestID: input.RequestID, DecisionID: decisionID, RequestHMAC: moderationRequestCacheKey(ids),
				ChunkKeys: append([]string(nil), ids...), Provider: cfg.Provider, Model: cfg.Model, PolicyScope: policyScope,
				AggregateLevel: string(aggregated.Level), TotalChunks: len(ids), CachedChunks: len(hits), FreshChunks: len(misses),
				CompletePASSEvidence: true, ForwardedUpstream: strings.ToLower(strings.TrimSpace(input.Provider)),
				ForwardedAt: now, CorrelationDeadline: now.Add(10 * time.Minute),
			}
			if err := s.passCache.StoreComparisonMetadata(ctx, input.RequestID, metadata); err != nil {
				return AggregatedModerationBatch{}, fmt.Errorf("store moderation comparison metadata: %w", err)
			}
		}
	}
	return aggregated, nil
}

func shouldForceFreshModeration(requestID string) bool {
	if strings.TrimSpace(requestID) == "" {
		return false
	}
	digest := sha256.Sum256([]byte(requestID))
	return int(digest[0]) < 3
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
