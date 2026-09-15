package service

import (
	"context"
)

// reviewRulesOnly is the observation-mode implementation for the canonical
// rules engine. It records the rule result without entering any provider API.
func (s *ContentModerationService) reviewRulesOnly(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) *ContentModerationDecision {
	if s == nil || cfg == nil {
		return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	}
	if rule, hit := matchContentModerationLocalRuleInputSet(content, cfg.keywordRuleSet()); hit {
		log := s.buildLog(input, cfg, ContentModerationActionKeywordReview, true, rule.Category, 1, map[string]float64{"keyword": 1}, content.KeywordHitExcerpt(rule.Keyword), nil, nil, "")
		applyContentModerationKeywordMetadata(log, contentModerationKeywordDecision{rule: rule, context: classifyContentModerationKeywordContext(content.Text, rule), action: ContentModerationActionKeywordReview, flagged: true, effectiveAction: ContentModerationKeywordActionObserve})
		log.DecisionSource = "rule"
		log.QueueDelayMS = queueDelayPointer(ctx)
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
		return &ContentModerationDecision{Allowed: true, Flagged: true, Action: ContentModerationActionKeywordReview, MatchedKeyword: rule.Keyword, KeywordCategory: rule.Category, KeywordSeverity: rule.Severity}
	}
	if cfg.RecordNonHits {
		log := s.buildLog(input, cfg, ContentModerationActionAllow, false, "", 0, nil, content.ExcerptText(), nil, nil, "")
		log.DecisionSource = "rule"
		log.QueueDelayMS = queueDelayPointer(ctx)
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
	}
	s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
	return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
}

func queueDelayPointer(ctx context.Context) *int {
	if value, ok := ctx.Value(contentModerationQueueDelayContextKey{}).(int); ok {
		return &value
	}
	return nil
}
