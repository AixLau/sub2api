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
	SyntheticAll       bool
	ContextOnly        bool
	// ProviderFallback marks a candidate that exists only because the primary
	// reviewer was unreachable. It carries no matched keyword: the marker is
	// internal routing state, not user evidence, so it must not occupy the
	// keyword slot where the admin UI and log search expect a matched string.
	ProviderFallback bool
}

type contentModerationRequiredSemanticReviewContextKey struct{}

func applySemanticReviewLogAttribution(log *ContentModerationLog, result ContentModerationSemanticReviewResult, latency int, fallbackModel string) {
	if log == nil {
		return
	}
	log.DecisionSource = contentModerationDecisionSourceSemantic
	log.ModerationProvider = "platform_openai"
	if result.AccountID <= 0 {
		log.ModerationProvider = "custom_api"
	}
	log.ModerationModel = strings.TrimSpace(result.Model)
	if log.ModerationModel == "" {
		log.ModerationModel = strings.TrimSpace(fallbackModel)
	}
	log.UpstreamLatencyMS = &latency
}

func contentModerationSemanticGateCandidateForKeyword(cfg *ContentModerationConfig, content ContentModerationInput, rule ContentModerationKeywordRule, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || cfg.EngineMode == ContentModerationEngineModeRulesOnly || !cfg.SemanticReview.Enabled || router == nil || strings.TrimSpace(content.Text) == "" {
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
	reviewText, evidenceComplete := buildContentModerationSemanticReviewEvidence(cfg.SemanticReview, content, rule.Keyword)
	if strings.TrimSpace(reviewText) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	return contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{
			Text:             reviewText,
			EvidenceComplete: evidenceComplete,
		},
		Keyword:     strings.TrimSpace(rule.Keyword),
		Category:    category,
		Severity:    strings.TrimSpace(rule.Severity),
		ContextOnly: semanticReviewEvidenceContextOnly(cfg.SemanticReview, content, rule.Keyword),
	}, true
}

func contentModerationSemanticGateCandidateForAll(cfg *ContentModerationConfig, content ContentModerationInput, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || cfg.EngineMode == ContentModerationEngineModeRulesOnly || !cfg.SemanticReview.Enabled || router == nil || strings.TrimSpace(content.Text) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	if normalizeContentModerationSemanticReviewTrigger(cfg.SemanticReview.Trigger) != ContentModerationSemanticReviewTriggerAll {
		return contentModerationSemanticGateCandidate{}, false
	}
	reviewText, evidenceComplete := buildContentModerationSemanticReviewEvidence(cfg.SemanticReview, content, "")
	if strings.TrimSpace(reviewText) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	return contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{
			Text:             reviewText,
			EvidenceComplete: evidenceComplete,
		},
		Keyword:      "semantic_review",
		Category:     "semantic_review",
		Severity:     ContentModerationKeywordSeverityHigh,
		SyntheticAll: true,
		ContextOnly:  semanticReviewEvidenceContextOnly(cfg.SemanticReview, content, ""),
	}, true
}

func contentModerationSemanticGateCandidateForPromptFilter(cfg *ContentModerationConfig, content ContentModerationInput, hit contentModerationPromptFilterHit, router ContentModerationSemanticReviewRouter) (contentModerationSemanticGateCandidate, bool) {
	if cfg == nil || cfg.EngineMode == ContentModerationEngineModeRulesOnly || !cfg.SemanticReview.Enabled || router == nil || len(hit.Verdict.Matches) == 0 {
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
	nonTerminalContext := !contentModerationPromptFilterSourceCanHardBlock(hit.Source)
	semanticCfg := cfg.SemanticReview
	reviewKeyword := keyword
	if nonTerminalContext {
		// Review only the latest direct user turn when the local hit came from
		// assistant, developer, tool, or ambient context. The context hit may
		// trigger review, but its text cannot establish the user's intent.
		semanticCfg.Trigger = ContentModerationSemanticReviewTriggerAll
		reviewKeyword = ""
	}
	reviewText, evidenceComplete := buildContentModerationSemanticReviewEvidence(semanticCfg, reviewContent, reviewKeyword)
	if strings.TrimSpace(reviewText) == "" {
		return contentModerationSemanticGateCandidate{}, false
	}
	return contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{
			Text:             reviewText,
			EvidenceComplete: evidenceComplete,
		},
		Keyword:            keyword,
		Category:           category,
		Severity:           promptFilterSeverity(hit.Verdict),
		MatchedSource:      strings.TrimSpace(hit.Source.Source),
		MatchedSourceRole:  strings.TrimSpace(hit.Source.Role),
		NonTerminalContext: nonTerminalContext,
		ContextOnly:        nonTerminalContext || semanticReviewEvidenceContextOnly(cfg.SemanticReview, reviewContent, keyword),
	}, true
}

func (s *ContentModerationService) semanticReviewGate(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, candidate contentModerationSemanticGateCandidate) (*ContentModerationDecision, bool) {
	if s == nil || cfg == nil || s.semanticReviewRouter == nil {
		if required, _ := ctx.Value(contentModerationRequiredSemanticReviewContextKey{}).(bool); required {
			return semanticReviewUnavailableDecision(cfg != nil && cfg.Mode == ContentModerationModePreBlock), true
		}
		return nil, false
	}
	required, _ := ctx.Value(contentModerationRequiredSemanticReviewContextKey{}).(bool)
	evidenceComplete := candidate.Input.EvidenceComplete
	candidate.Input = contentModerationSemanticReviewInputForCheck(
		input,
		candidate.Input.Text,
		semanticReviewDecisionID(input, hashText),
	)
	candidate.Input.EvidenceComplete = evidenceComplete
	started := time.Now()
	result, err := s.semanticReviewRouter.Review(ctx, cfg.SemanticReview, candidate.Input)
	if err != nil {
		latency := int(time.Since(started).Milliseconds())
		s.persistSemanticReviewErrorLog(
			ctx,
			input,
			cfg,
			content,
			hashText,
			cfg.SemanticReview.PrimaryModel,
			"semantic_review_gate_failed",
			&latency,
			err,
		)
		slog.Warn("content_moderation.semantic_review_gate_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"candidate_keyword", candidate.Keyword,
			"candidate_category", candidate.Category,
			"error", err)
		if required {
			return semanticReviewUnavailableDecision(cfg.Mode == ContentModerationModePreBlock), true
		}
		// Semantic review is an optional pre-check for legacy callers.
		return nil, false
	}
	if state, ok := ctx.Value(contentModerationSemanticReviewStateContextKey{}).(*contentModerationSemanticReviewState); ok && state != nil {
		state.Completed = true
	}
	// Capture the reviewer's own verdict before any policy pass can rewrite or
	// replace it. semantic_review_verdict records the post-policy verdict, so
	// without this the audit record cannot say what the model actually decided.
	rawVerdict := result.Verdict
	result, policyOverride := applySemanticReviewGatePolicies(
		result,
		candidate.Input.EvidenceComplete,
		candidate.ContextOnly,
		candidate.Input.ReviewKind,
	)
	escalationInput := candidate.Input
	if result.Verdict == "review" && !candidate.ContextOnly && contentModerationSemanticEscalationEnabled(cfg.SemanticReview) {
		escalationInput = contentModerationSemanticGateEscalationInput(input, cfg, content, candidate, candidate.Input)
		var escalated ContentModerationSemanticReviewResult
		var escalationErr error
		escalated, _, escalationErr = s.escalateSemanticReview(ctx, cfg.SemanticReview, escalationInput, result)
		if escalationErr != nil {
			escalationLatency := int(time.Since(started).Milliseconds())
			s.persistSemanticReviewErrorLog(ctx, input, cfg, content, hashText, cfg.SemanticReview.EscalationModel, "final_semantic_review_failed", &escalationLatency, escalationErr)
			return semanticReviewUnavailableDecision(cfg.Mode == ContentModerationModePreBlock), true
		}
		result = escalated
	}
	result = semanticReviewContextOnlyDecision(result, candidate.ContextOnly)
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
	metadata := contentModerationSemanticGateMetadata(cfg, content, input.Protocol, candidate, result, rawVerdict, policyOverride)
	categoryScores := map[string]float64{"semantic_review": score}
	latency := int(time.Since(started).Milliseconds())
	if result.Verdict != "allow" && result.Verdict != "reject" {
		if contentModerationSemanticGateCandidateReviewKind(candidate) == contentModerationReviewKindPromptInjection {
			// The prompt-injection reviewer has its own opt-in fail-closed contract
			// (prompt_injection_reviewer_enabled + prompt_injection_fail_closed),
			// which deliberately reports a non-terminal PI outcome as an unavailable
			// reviewer with a 503. Leave that contract untouched.
			finalLatency := int(time.Since(started).Milliseconds())
			s.persistSemanticReviewErrorLog(ctx, input, cfg, content, hashText, cfg.SemanticReview.EscalationModel,
				"final_semantic_review_failed", &finalLatency, errors.New("final semantic reviewer is unavailable"))
			return semanticReviewUnavailableDecision(cfg.Mode == ContentModerationModePreBlock), true
		}
		// A non-terminal verdict means the reviewer could not resolve an
		// outcome-changing safety fact. That is a legitimate outcome of the initial
		// screen, not a reviewer outage: with escalation disabled the verdict
		// necessarily stays review and no final reviewer was ever called, so
		// reporting "the final reviewer is unavailable" described an event that did
		// not happen and discarded the candidate from human triage.
		//
		// The enforcement decision is deliberately unchanged: a pre-block
		// deployment still fails closed, because 503-on-unresolved-review is the
		// existing safety posture. Only the record and the label now state what
		// actually happened.
		blocked := cfg.Mode == ContentModerationModePreBlock
		log := s.buildLog(input, cfg, ContentModerationActionSemanticReviewReview, true, category, score, categoryScores, content.ExcerptText(), &latency, nil, metadata)
		applySemanticReviewLogAttribution(log, result, latency, cfg.SemanticReview.PrimaryModel)
		applySemanticReviewSubmittedLog(log, cfg, result)
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = ContentModerationActionSemanticReviewReview
		log.EffectiveKeywordAction = ContentModerationActionSemanticReviewReview
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = ContentModerationActionSemanticReviewReview
		log.ReviewStatus = ContentModerationReviewStatusPending
		log.UserViolationEligible = false
		log.Enforcement = contentModerationEnforcementFor(blocked)
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
		if blocked {
			return semanticReviewUnresolvedReviewBlock(), true
		}
		return &ContentModerationDecision{
			Allowed:                true,
			Flagged:                true,
			HighestCategory:        category,
			HighestScore:           score,
			CategoryScores:         categoryScores,
			Action:                 ContentModerationActionSemanticReviewReview,
			MatchedKeyword:         candidate.Keyword,
			KeywordCategory:        candidate.Category,
			KeywordSeverity:        candidate.Severity,
			KeywordAction:          ContentModerationActionSemanticReviewReview,
			EffectiveKeywordAction: ContentModerationActionSemanticReviewReview,
			RiskContextType:        ContentModerationRiskContextActualRequest,
			RiskContextReason:      ContentModerationActionSemanticReviewReview,
		}, true
	}
	if result.Verdict == "allow" && (required || (candidate.ContextOnly && policyOverride)) {
		log := s.buildLog(input, cfg, ContentModerationActionSemanticReviewAllow, false, category, score, categoryScores,
			content.ExcerptText(), &latency, nil, metadata)
		applySemanticReviewLogAttribution(log, result, latency, cfg.SemanticReview.PrimaryModel)
		applySemanticReviewSubmittedLog(log, cfg, result)
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = ContentModerationActionSemanticReviewAllow
		log.EffectiveKeywordAction = ContentModerationActionSemanticReviewAllow
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = ContentModerationActionSemanticReviewAllow
		log.UserViolationEligible = false
		log.Enforcement = ContentModerationEnforcementAllowed
		s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
	}

	switch result.Verdict {
	case "reject":
		s.recordPreBlockSyncMetric(0, ContentModerationActionSemanticReviewReject)
		log := s.buildLog(input, cfg, ContentModerationActionSemanticReviewReject, true, category, score, categoryScores, content.KeywordHitExcerpt(candidate.Keyword), &latency, nil, metadata)
		applySemanticReviewLogAttribution(log, result, latency, cfg.SemanticReview.PrimaryModel)
		applySemanticReviewSubmittedLog(log, cfg, result)
		log.MatchedKeyword = candidate.Keyword
		log.KeywordCategory = candidate.Category
		log.KeywordSeverity = candidate.Severity
		log.KeywordAction = ContentModerationActionSemanticReviewReject
		log.EffectiveKeywordAction = ContentModerationActionSemanticReviewReject
		log.RiskContextType = ContentModerationRiskContextActualRequest
		log.RiskContextReason = "semantic_review_reject"
		log.UserViolationEligible = !candidate.ContextOnly && escalationInput.EvidenceComplete && !semanticReviewFinalInconclusive(result)
		// A reject only blocks in pre_block mode. In observe mode the request is
		// still forwarded, so the record states that instead of leaving the admin UI
		// to infer "blocked" from the action.
		log.Enforcement = contentModerationEnforcementFor(cfg.Mode == ContentModerationModePreBlock)
		if cfg.Mode == ContentModerationModePreBlock {
			s.enqueueRecord(ctx, input, cfg, log, hashText, log.UserViolationEligible, log.UserViolationEligible)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
		}
		return &ContentModerationDecision{
			Allowed:                cfg.Mode != ContentModerationModePreBlock,
			Blocked:                cfg.Mode == ContentModerationModePreBlock,
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
	}
	if required {
		return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionSemanticReviewAllow}, true
	}
	return nil, false
}

func applySemanticReviewGatePolicies(
	result ContentModerationSemanticReviewResult,
	evidenceComplete bool,
	contextOnly bool,
	reviewKind string,
) (ContentModerationSemanticReviewResult, bool) {
	if normalizeSemanticReviewVerdict(result.Verdict) == "review" {
		return normalizeSemanticReviewResult(result), false
	}
	var policyOverride bool
	if reviewKind == contentModerationReviewKindPromptInjection {
		result, policyOverride = applyPromptInjectionReviewPolicy(result, evidenceComplete)
	} else {
		result, policyOverride = applySemanticReviewPolicyWithPromotion(result, evidenceComplete)
	}
	result, attributionOverride := applySemanticReviewAttributionPolicy(result, contextOnly)
	policyOverride = policyOverride || attributionOverride
	if evidenceComplete {
		var highRiskReviewOverride bool
		result, highRiskReviewOverride = applySemanticReviewHighRiskReviewPolicy(result, contextOnly)
		policyOverride = policyOverride || highRiskReviewOverride
	}
	return result, policyOverride
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
	reviewText, evidenceComplete := buildContentModerationSemanticReviewEvidence(semanticCfg, content, focusKeyword)
	candidate := contentModerationSemanticGateCandidate{
		Input: ContentModerationSemanticReviewInput{
			Text:             reviewText,
			EvidenceComplete: evidenceComplete,
		},
		// The primary reviewer being unreachable is technical state, not content
		// evidence. Leaving the keyword slot empty keeps the diagnostic marker out
		// of matched_keyword, where it used to be indistinguishable from a real
		// keyword hit and made provider failures look like content violations.
		// The provenance is carried by ProviderFallback, RiskContextReason
		// ("semantic_review_provider_fallback"), and the provider error log.
		ProviderFallback: true,
		ContextOnly:      semanticReviewEvidenceContextOnly(semanticCfg, content, focusKeyword),
	}
	if strings.TrimSpace(candidate.Input.Text) == "" {
		return nil, false
	}
	candidate.Input = contentModerationSemanticReviewInputForCheck(
		input,
		candidate.Input.Text,
		semanticReviewDecisionID(input, hashText),
	)
	candidate.Input.EvidenceComplete = evidenceComplete
	providerErrorText := ""
	if providerErr != nil {
		providerErrorText = providerErr.Error()
	}
	started := time.Now()
	result, err := s.semanticReviewRouter.Review(ctx, semanticCfg, candidate.Input)
	if err != nil {
		latency := int(time.Since(started).Milliseconds())
		s.persistSemanticReviewErrorLog(
			ctx,
			input,
			cfg,
			content,
			hashText,
			semanticCfg.PrimaryModel,
			"semantic_review_provider_fallback_failed",
			&latency,
			err,
		)
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
	// Capture the reviewer's own verdict before any policy pass can rewrite it.
	rawVerdict := result.Verdict
	result, policyOverride := applySemanticReviewGatePolicies(
		result,
		candidate.Input.EvidenceComplete,
		candidate.ContextOnly,
		contentModerationReviewKindGeneral,
	)
	escalationInput := candidate.Input
	if result.Verdict == "review" && !candidate.ContextOnly && contentModerationSemanticEscalationEnabled(cfg.SemanticReview) {
		escalationInput = contentModerationSemanticGateEscalationInput(input, cfg, content, candidate, candidate.Input)
		var escalated ContentModerationSemanticReviewResult
		var escalationErr error
		escalated, _, escalationErr = s.escalateSemanticReview(ctx, cfg.SemanticReview, escalationInput, result)
		if escalationErr != nil {
			s.persistSemanticReviewErrorLog(ctx, input, cfg, content, hashText, cfg.SemanticReview.EscalationModel, "final_semantic_review_failed", nil, escalationErr)
			return semanticReviewUnavailableDecision(allowBlock && cfg.Mode == ContentModerationModePreBlock), true
		}
		result = escalated
	}
	result = semanticReviewContextOnlyDecision(result, candidate.ContextOnly)
	if result.Verdict != "allow" && result.Verdict != "reject" {
		s.persistSemanticReviewErrorLog(ctx, input, cfg, content, hashText, cfg.SemanticReview.EscalationModel,
			"final_semantic_review_failed", nil, errors.New("final semantic reviewer is unavailable"))
		return semanticReviewUnavailableDecision(allowBlock && cfg.Mode == ContentModerationModePreBlock), true
	}
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
	metadata := contentModerationSemanticGateMetadata(cfg, content, input.Protocol, candidate, result, rawVerdict, policyOverride)
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
		applySemanticReviewSubmittedLog(log, cfg, result)
	}

	switch result.Verdict {
	case "reject":
		blocked := allowBlock && cfg.Mode == ContentModerationModePreBlock
		action := ContentModerationActionSemanticReviewReject
		log := s.buildLog(input, cfg, action, true, category, score, categoryScores, content.ExcerptText(), &latency, nil, metadata)
		applySemanticReviewLogAttribution(log, result, latency, cfg.SemanticReview.PrimaryModel)
		setLogMetadata(log, action)
		log.UserViolationEligible = !candidate.ContextOnly && escalationInput.EvidenceComplete && !semanticReviewFinalInconclusive(result)
		log.Enforcement = contentModerationEnforcementFor(blocked)
		enforcementEligible := blocked && log.UserViolationEligible
		if blocked {
			s.recordPreBlockSyncMetric(latency, action)
			s.enqueueRecord(ctx, input, cfg, log, hashText, enforcementEligible, enforcementEligible)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
		}
		return buildDecision(action, true, blocked), true
	default:
		if allowBlock && cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionSemanticReviewAllow)
		}
		return buildDecision(ContentModerationActionSemanticReviewAllow, false, false), true
	}
}

func (s *ContentModerationService) persistSemanticReviewErrorLog(
	ctx context.Context,
	input ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	content ContentModerationInput,
	hashText string,
	model string,
	reason string,
	latencyMS *int,
	err error,
) {
	if s == nil || cfg == nil || err == nil {
		return
	}
	sanitizedErr := sanitizeSemanticReviewError(err.Error())
	if sanitizedErr == "" {
		sanitizedErr = "semantic review failed"
	}
	log := s.buildContentModerationErrorLog(input, cfg, content, latencyMS, nil, errors.New(sanitizedErr))
	log.DecisionSource = contentModerationDecisionSourceSemantic
	log.ModerationProvider = "platform_openai"
	log.ModerationModel = strings.TrimSpace(model)
	if log.ModerationModel == "" {
		log.ModerationModel = ContentModerationSemanticReviewPrimaryModel
	}
	log.RiskContextType = "semantic_review"
	log.RiskContextReason = strings.TrimSpace(reason)
	s.persistContentModerationLog(ctx, cfg, log, hashText, false, false)
}

func contentModerationSemanticGateMetadata(cfg *ContentModerationConfig, content ContentModerationInput, protocol string, candidate contentModerationSemanticGateCandidate, result ContentModerationSemanticReviewResult, rawVerdict string, policyOverride bool) contentModerationMetadata {
	metadata := map[string]any{}
	matchedSource := strings.TrimSpace(candidate.MatchedSource)
	if matchedSource == "" {
		matchedSource = contentModerationMatchedSource(protocol, candidate.Keyword, content)
	}
	base := contentModerationHitLogMetadata(cfg, content, matchedSource)
	if strings.TrimSpace(string(base)) != "" {
		_ = json.Unmarshal([]byte(base), &metadata)
	}
	metadata["semantic_review_model"] = result.Model
	if result.AttemptCount > 0 {
		metadata["semantic_review_attempt_count"] = result.AttemptCount
	}
	if result.FallbackFrom != "" {
		metadata["semantic_review_fallback_from"] = result.FallbackFrom
		metadata["semantic_review_fallback_reason"] = result.FallbackReason
	}
	metadata["semantic_review_verdict"] = result.Verdict
	// The verdict the reviewer produced before the policy passes below rewrite it.
	// policy_override only says that something changed; this says what the model
	// actually decided, which is what makes an override auditable.
	if strings.TrimSpace(rawVerdict) != "" {
		metadata["semantic_review_raw_verdict"] = rawVerdict
	}
	metadata["semantic_review_intent"] = result.Intent
	metadata["semantic_review_target"] = result.Target
	metadata["semantic_review_authorization"] = result.Authorization
	metadata["semantic_review_information_access"] = result.InformationAccess
	metadata["semantic_review_harm_mechanism"] = result.HarmMechanism
	metadata["semantic_review_harm_evidence"] = result.HarmEvidence
	metadata["semantic_review_deception_type"] = result.DeceptionType
	metadata["semantic_review_categories"] = result.Categories
	metadata["semantic_review_confidence"] = result.Confidence
	metadata["semantic_review_severity"] = result.Severity
	if result.ModelSeverity != "" {
		metadata["semantic_review_model_severity"] = result.ModelSeverity
	}
	metadata["semantic_review_operationality"] = result.Operationality
	metadata["semantic_review_executability"] = result.Executability
	metadata["semantic_review_reason_codes"] = result.ReasonCodes
	metadata["semantic_review_reason_details"] = result.ReasonDetails
	if result.ReasoningSummary != "" {
		metadata["semantic_review_reasoning_summary"] = result.ReasoningSummary
	}
	metadata["semantic_review_policy_override"] = policyOverride
	addSemanticReviewEscalationMetadata(metadata, result)
	if keyword := strings.TrimSpace(candidate.Keyword); keyword != "" {
		metadata["semantic_review_candidate"] = keyword
	}
	if candidate.ProviderFallback {
		metadata["semantic_review_candidate_provider_fallback"] = true
	}
	if candidate.SyntheticAll {
		metadata["semantic_review_candidate_synthetic_all"] = true
	}
	if candidate.ContextOnly {
		metadata["semantic_review_candidate_context_only"] = true
	}
	if strings.TrimSpace(candidate.MatchedSource) != "" {
		metadata["semantic_review_candidate_source"] = candidate.MatchedSource
		metadata["semantic_review_candidate_source_role"] = candidate.MatchedSourceRole
		metadata["semantic_review_candidate_non_terminal_context"] = candidate.NonTerminalContext
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return base
	}
	return contentModerationMetadata(raw)
}

func semanticReviewRouterUnavailableError() error {
	return errors.New("semantic review router is unavailable")
}
