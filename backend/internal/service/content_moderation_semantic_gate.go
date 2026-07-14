package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"
)

type contentModerationSemanticGateCandidate struct {
	Input              ContentModerationSemanticReviewInput
	Keyword            string
	Category           string
	Severity           string
	MatchedSource      string
	MatchedSourceRole  string
	NonTerminalContext bool
}

func contentModerationSemanticGateCandidateForKeyword(cfg *ContentModerationConfig, content ContentModerationInput, rule ContentModerationKeywordRule, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || !cfg.SemanticReview.Enabled || router == nil || strings.TrimSpace(content.Text) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	category := strings.ToLower(strings.TrimSpace(rule.Category))
	if normalizeContentModerationSemanticReviewTrigger(cfg.SemanticReview.Trigger) != ContentModerationSemanticReviewTriggerAll &&
		category != ContentModerationKeywordCategoryCyber && category != ContentModerationKeywordCategoryJailbreak &&
		!strings.Contains(category, "cyber") && !strings.Contains(category, "jailbreak") &&
		!strings.EqualFold(strings.TrimSpace(rule.Action), ContentModerationKeywordActionBlock) &&
		!strings.EqualFold(strings.TrimSpace(rule.Severity), ContentModerationKeywordSeverityHigh) &&
		!strings.EqualFold(strings.TrimSpace(rule.Severity), ContentModerationKeywordSeverityCritical) {
		return contentModerationSemanticGateCandidate{}, false
	}
	return contentModerationSemanticGateCandidate{
		Input:    ContentModerationSemanticReviewInput{Text: buildContentModerationSemanticReviewInput(cfg.SemanticReview, content, rule.Keyword)},
		Keyword:  strings.TrimSpace(rule.Keyword),
		Category: category,
		Severity: strings.TrimSpace(rule.Severity),
	}, true
}

func contentModerationSemanticGateCandidateForAll(cfg *ContentModerationConfig, content ContentModerationInput, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || !cfg.SemanticReview.Enabled || router == nil || strings.TrimSpace(content.Text) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	if normalizeContentModerationSemanticReviewTrigger(cfg.SemanticReview.Trigger) != ContentModerationSemanticReviewTriggerAll {
		return contentModerationSemanticGateCandidate{}, false
	}
	return contentModerationSemanticGateCandidate{
		Input:    ContentModerationSemanticReviewInput{Text: buildContentModerationSemanticReviewInput(cfg.SemanticReview, content, "")},
		Keyword:  "semantic_review",
		Category: "semantic_review",
		Severity: ContentModerationKeywordSeverityHigh,
	}, true
}

func contentModerationSemanticGateCandidateForPromptFilter(cfg *ContentModerationConfig, content ContentModerationInput, hit contentModerationPromptFilterHit, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || !cfg.SemanticReview.Enabled || router == nil || len(hit.Verdict.Matches) == 0 {
		return contentModerationSemanticGateCandidate{}, false
	}
	if normalizeContentModerationSemanticReviewTrigger(cfg.SemanticReview.Trigger) == ContentModerationSemanticReviewTriggerAll {
		return contentModerationSemanticGateCandidate{}, false
	}
	reviewContent := contentModerationPromptFilterSemanticReviewContent(content, hit)
	if strings.TrimSpace(reviewContent.Text) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	keyword := hit.Verdict.Matches[0].Name
	category := strings.TrimSpace(hit.Verdict.Matches[0].Category)
	if category == "" {
		category = "cyber"
	}
	return contentModerationSemanticGateCandidate{
		Input:              ContentModerationSemanticReviewInput{Text: buildContentModerationSemanticReviewInput(cfg.SemanticReview, reviewContent, keyword)},
		Keyword:            keyword,
		Category:           category,
		Severity:           promptFilterSeverity(hit.Verdict),
		MatchedSource:      strings.TrimSpace(hit.Source.Source),
		MatchedSourceRole:  strings.TrimSpace(hit.Source.Role),
		NonTerminalContext: !contentModerationPromptFilterSourceCanHardBlock(hit.Source),
	}, true
}

func (s *ContentModerationService) semanticReviewGate(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, candidate contentModerationSemanticGateCandidate) (*ContentModerationDecision, bool) {
	if s == nil || cfg == nil || s.semanticReviewRouter == nil {
		return nil, false
	}
	candidate.Input = contentModerationSemanticReviewInputForCheck(
		input,
		candidate.Input.Text,
		semanticReviewDecisionID(input, hashText),
	)
	result, err := s.semanticReviewRouter.Review(ctx, cfg.SemanticReview, candidate.Input)
	if err != nil {
		slog.Warn("content_moderation.semantic_review_gate_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"candidate_keyword", candidate.Keyword,
			"candidate_category", candidate.Category,
			"error", err)
		// Semantic review is an optional pre-check. If it is unavailable,
		// continue to the required ordinary moderation API in hybrid mode.
		return nil, false
	}
	if state, ok := ctx.Value(contentModerationSemanticReviewStateContextKey{}).(*contentModerationSemanticReviewState); ok && state != nil {
		state.Completed = true
	}
	result, policyOverride := applySemanticReviewPolicy(result)
	category := "semantic_review"
	if len(result.Categories) > 0 && strings.TrimSpace(result.Categories[0]) != "" {
		category = result.Categories[0]
	}
	score := result.Confidence
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	metadata := contentModerationSemanticGateMetadata(cfg, content, input.Protocol, candidate, result, policyOverride)
	categoryScores := map[string]float64{"semantic_review": score}

	switch result.Verdict {
	case "reject":
		s.recordPreBlockSyncMetric(0, ContentModerationActionSemanticReviewReject)
		log := s.buildLog(input, cfg, ContentModerationActionSemanticReviewReject, true, category, score, categoryScores, content.KeywordHitExcerpt(candidate.Keyword), nil, nil, metadata)
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = ContentModerationActionSemanticReviewReject
		log.EffectiveKeywordAction = ContentModerationActionSemanticReviewReject
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = "semantic_review_reject"
		s.enqueueRecord(ctx, input, cfg, log, hashText, true, true)
		return &ContentModerationDecision{
			Allowed:                false,
			Blocked:                true,
			Flagged:                true,
			Message:                cfg.BlockMessage,
			StatusCode:             cfg.BlockStatus,
			HighestCategory:        category,
			HighestScore:           score,
			CategoryScores:         categoryScores,
			Action:                 ContentModerationActionSemanticReviewReject,
			MatchedKeyword:         candidate.Keyword,
			KeywordCategory:        candidate.Category,
			KeywordSeverity:        candidate.Severity,
			KeywordAction:          ContentModerationActionSemanticReviewReject,
			EffectiveKeywordAction: ContentModerationActionSemanticReviewReject,
			RiskContextType:        ContentModerationRiskContextActualRequest,
			RiskContextReason:      "semantic_review_reject",
		}, true
	case "review":
		log := s.buildLog(input, cfg, ContentModerationActionSemanticReviewReview, true, category, score, categoryScores, content.KeywordHitExcerpt(candidate.Keyword), nil, nil, metadata)
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = ContentModerationActionSemanticReviewReview
		log.EffectiveKeywordAction = ContentModerationActionSemanticReviewReview
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = "semantic_review_review"
		log.ReviewStatus = ContentModerationReviewStatusPending
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
	}
	return nil, false
}

func (s *ContentModerationService) semanticReviewProviderFallback(
	ctx context.Context,
	input ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	content ContentModerationInput,
	hashText string,
	focusKeyword string,
	providerErr error,
	allowBlock bool,
) (*ContentModerationDecision, bool) {
	if s == nil || cfg == nil || s.semanticReviewRouter == nil || content.IsEmpty() || strings.TrimSpace(content.Text) == "" {
		return nil, false
	}
	semanticCfg := contentModerationSemanticReviewConfigForProviderFallback(cfg)
	candidate := contentModerationSemanticGateCandidate{
		Input:    ContentModerationSemanticReviewInput{Text: buildContentModerationSemanticReviewInput(semanticCfg, content, focusKeyword)},
		Keyword:  "provider_unavailable",
		Category: "semantic_fallback",
		Severity: ContentModerationKeywordSeverityHigh,
	}
	if strings.TrimSpace(candidate.Input.Text) == "" {
		return nil, false
	}
	candidate.Input = contentModerationSemanticReviewInputForCheck(
		input,
		candidate.Input.Text,
		semanticReviewDecisionID(input, hashText),
	)
	providerErrorText := ""
	if providerErr != nil {
		providerErrorText = providerErr.Error()
	}
	started := time.Now()
	result, err := s.semanticReviewRouter.Review(ctx, semanticCfg, candidate.Input)
	if err != nil {
		slog.Warn("content_moderation.semantic_review_provider_fallback_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"provider_error", sanitizeSemanticReviewError(providerErrorText),
			"error", sanitizeSemanticReviewError(err.Error()))
		return nil, false
	}
	if state, ok := ctx.Value(contentModerationSemanticReviewStateContextKey{}).(*contentModerationSemanticReviewState); ok && state != nil {
		state.Completed = true
	}
	result, policyOverride := applySemanticReviewPolicy(result)
	category := "semantic_review"
	if len(result.Categories) > 0 && strings.TrimSpace(result.Categories[0]) != "" {
		category = result.Categories[0]
	}
	score := result.Confidence
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	categoryScores := map[string]float64{"semantic_review": score}
	metadata := contentModerationSemanticGateMetadata(cfg, content, input.Protocol, candidate, result, policyOverride)
	latency := int(time.Since(started).Milliseconds())
	buildDecision := func(action string, flagged, blocked bool) *ContentModerationDecision {
		return &ContentModerationDecision{
			Allowed: !blocked,
			Blocked: blocked,
			Flagged: flagged,
			Message: func() string {
				if blocked {
					return cfg.BlockMessage
				}
				return ""
			}(),
			StatusCode: func() int {
				if blocked {
					return cfg.BlockStatus
				}
				return 0
			}(),
			HighestCategory:        category,
			HighestScore:           score,
			CategoryScores:         categoryScores,
			Action:                 action,
			MatchedKeyword:         candidate.Keyword,
			KeywordCategory:        candidate.Category,
			KeywordSeverity:        candidate.Severity,
			KeywordAction:          action,
			EffectiveKeywordAction: action,
			RiskContextType:        ContentModerationRiskContextActualRequest,
			RiskContextReason:      "semantic_review_provider_fallback",
		}
	}
	setLogMetadata := func(log *ContentModerationLog, action string) {
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = action
		log.EffectiveKeywordAction = action
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = "semantic_review_provider_fallback"
	}

	switch result.Verdict {
	case "reject":
		blocked := allowBlock && cfg.Mode == ContentModerationModePreBlock
		action := ContentModerationActionSemanticReviewReject
		log := s.buildLog(input, cfg, action, true, category, score, categoryScores, content.ExcerptText(), &latency, nil, metadata)
		setLogMetadata(log, action)
		if blocked {
			s.recordPreBlockSyncMetric(latency, action)
			s.enqueueRecord(ctx, input, cfg, log, hashText, true, true)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, true, true)
		}
		return buildDecision(action, true, blocked), true
	case "review":
		action := ContentModerationActionSemanticReviewReview
		log := s.buildLog(input, cfg, action, true, category, score, categoryScores, content.ExcerptText(), &latency, nil, metadata)
		setLogMetadata(log, action)
		log.ReviewStatus = ContentModerationReviewStatusPending
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
		if allowBlock && cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(latency, action)
		}
		return buildDecision(action, true, false), true
	default:
		if allowBlock && cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionSemanticReviewAllow)
		}
		return buildDecision(ContentModerationActionSemanticReviewAllow, false, false), true
	}
}

func contentModerationSemanticGateMetadata(cfg *ContentModerationConfig, content ContentModerationInput, protocol string, candidate contentModerationSemanticGateCandidate, result ContentModerationSemanticReviewResult, policyOverride bool) string {
	metadata := map[string]any{}
	matchedSource := strings.TrimSpace(candidate.MatchedSource)
	if matchedSource == "" {
		matchedSource = contentModerationMatchedSource(protocol, candidate.Keyword, content)
	}
	base := contentModerationHitLogMetadata(cfg, content, matchedSource)
	if strings.TrimSpace(base) != "" {
		_ = json.Unmarshal([]byte(base), &metadata)
	}
	metadata["semantic_review_model"] = result.Model
	metadata["semantic_review_verdict"] = result.Verdict
	metadata["semantic_review_intent"] = result.Intent
	metadata["semantic_review_target"] = result.Target
	metadata["semantic_review_authorization"] = result.Authorization
	metadata["semantic_review_information_access"] = result.InformationAccess
	metadata["semantic_review_harm_mechanism"] = result.HarmMechanism
	metadata["semantic_review_categories"] = result.Categories
	metadata["semantic_review_confidence"] = result.Confidence
	metadata["semantic_review_severity"] = result.Severity
	metadata["semantic_review_operationality"] = result.Operationality
	metadata["semantic_review_executability"] = result.Executability
	metadata["semantic_review_reason_codes"] = result.ReasonCodes
	metadata["semantic_review_policy_override"] = policyOverride
	metadata["semantic_review_candidate"] = candidate.Keyword
	if strings.TrimSpace(candidate.MatchedSource) != "" {
		metadata["semantic_review_candidate_source"] = candidate.MatchedSource
		metadata["semantic_review_candidate_source_role"] = candidate.MatchedSourceRole
		metadata["semantic_review_candidate_non_terminal_context"] = candidate.NonTerminalContext
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return base
	}
	return string(raw)
}

func semanticReviewRouterUnavailableError() error {
	return errors.New("semantic review router is unavailable")
}
