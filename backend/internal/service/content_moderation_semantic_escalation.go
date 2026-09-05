package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const semanticReviewEscalationReasonInitialReview = "initial_review"

func applyFinalSemanticReviewPolicy(result ContentModerationSemanticReviewResult) (ContentModerationSemanticReviewResult, error) {
	result = normalizeSemanticReviewResult(result)
	result.FinalReview = true
	if result.Verdict != "allow" && result.Verdict != "reject" {
		return result, errors.New("final semantic review returned a non-terminal verdict")
	}
	if semanticReviewPolicyRejectEligible(result) {
		result.Verdict = "reject"
		result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "semantic_policy_reject")
	}
	if result.HarmEvidence == "explicit" {
		for _, category := range result.Categories {
			switch category {
			case "license_cracking":
				result.Verdict = "reject"
				result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "platform_license_circumvention")
			case "biosecurity":
				result.Verdict = "reject"
				result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "platform_virology")
			}
		}
	}
	return result, nil
}

func semanticReviewUnavailableDecision(block bool) *ContentModerationDecision {
	if !block {
		return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionError}
	}
	return &ContentModerationDecision{
		Blocked: true, Action: ContentModerationActionSemanticReviewUnavailable,
		StatusCode: http.StatusServiceUnavailable, Message: ContentModerationTemporaryClientMessage,
	}
}

func semanticReviewContextOnlyDecision(result ContentModerationSemanticReviewResult, contextOnly bool) ContentModerationSemanticReviewResult {
	if contextOnly {
		result.Verdict = "allow"
		result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "semantic_policy_context_only")
	}
	return result
}

func semanticReviewFinalInconclusive(result ContentModerationSemanticReviewResult) bool {
	for _, reason := range result.ReasonCodes {
		if reason == "final_inconclusive" {
			return true
		}
	}
	return false
}

type contentModerationSemanticEscalationTrace struct {
	Attempted         bool
	InitialModel      string
	InitialVerdict    string
	InitialSeverity   string
	InitialConfidence float64
	Model             string
	Reason            string
	EvidenceComplete  bool
	Error             string
}

func contentModerationSemanticEscalationEnabled(cfg ContentModerationSemanticReviewConfig) bool {
	return cfg.EscalationEnabled && strings.TrimSpace(cfg.EscalationModel) != ""
}

func (s *ContentModerationService) escalateSemanticReview(
	ctx context.Context,
	cfg ContentModerationSemanticReviewConfig,
	input ContentModerationSemanticReviewInput,
	initial ContentModerationSemanticReviewResult,
) (ContentModerationSemanticReviewResult, bool, error) {
	if s == nil || s.semanticReviewRouter == nil || !contentModerationSemanticEscalationEnabled(cfg) ||
		normalizeSemanticReviewVerdict(initial.Verdict) != "review" {
		return initial, false, nil
	}

	trace := &contentModerationSemanticEscalationTrace{
		Attempted:         true,
		InitialModel:      strings.TrimSpace(initial.Model),
		InitialVerdict:    normalizeSemanticReviewVerdict(initial.Verdict),
		InitialSeverity:   strings.TrimSpace(initial.Severity),
		InitialConfidence: initial.Confidence,
		Model:             strings.TrimSpace(cfg.EscalationModel),
		Reason:            semanticReviewEscalationReasonInitialReview,
		EvidenceComplete:  input.EvidenceComplete,
	}
	if strings.TrimSpace(input.Text) == "" {
		err := errors.New("semantic review escalation input is empty")
		trace.Error = sanitizeSemanticReviewError(err.Error())
		initial.escalation = trace
		return initial, true, err
	}

	escalationCfg := cfg
	escalationCfg.PrimaryModel = trace.Model
	escalationCfg.FallbackModels = []string{}
	escalationCfg.TimeoutMS = cfg.EscalationTimeoutMS
	escalationCfg.PrimaryTimeoutMS = cfg.EscalationTimeoutMS
	escalationCfg.FallbackTimeoutMS = cfg.EscalationTimeoutMS
	escalationCfg.MaxAttemptsPerModel = 1
	escalationCfg.MaxInputRunes = cfg.EscalationMaxInputRunes
	escalationCfg.ReasoningEffort = cfg.EscalationReasoningEffort
	escalationCfg.disableDiscoveredFallback = true

	input.MaxInputRunes = cfg.EscalationMaxInputRunes
	input.FinalReview = true
	input.ReviewKind = contentModerationReviewKindGeneral
	input.ReasoningEffort = cfg.EscalationReasoningEffort
	if strings.TrimSpace(input.UsageRecordID) != "" {
		input.UsageRecordID += "-escalation"
	}
	result, err := s.semanticReviewRouter.Review(ctx, escalationCfg, input)
	if err == nil {
		result, err = applyFinalSemanticReviewPolicy(result)
	}
	if err != nil {
		trace.Error = sanitizeSemanticReviewError(err.Error())
		initial.escalation = trace
		return initial, true, err
	}
	result.escalation = trace
	return result, true, nil
}

func contentModerationSemanticGateEscalationInput(
	checkInput ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	content ContentModerationInput,
	candidate contentModerationSemanticGateCandidate,
	base ContentModerationSemanticReviewInput,
) ContentModerationSemanticReviewInput {
	if cfg == nil {
		return base
	}
	reviewCfg := cfg.SemanticReview
	reviewCfg.MaxInputRunes = reviewCfg.EscalationMaxInputRunes
	text, complete := buildContentModerationSemanticReviewEvidence(reviewCfg, content, candidate.Keyword)
	base.Text = text
	base.EvidenceComplete = complete
	base.EvidenceRevision = "semantic-escalation-evidence-v1"
	base.MaxInputRunes = reviewCfg.EscalationMaxInputRunes
	base.GroupID = cloneInt64Ptr(checkInput.GroupID)
	if contentModerationSemanticGateCandidateReviewKind(candidate) == contentModerationReviewKindPromptInjection {
		base.ReviewKind = contentModerationReviewKindPromptInjection
	}
	return base
}

func contentModerationSemanticGateCandidateReviewKind(candidate contentModerationSemanticGateCandidate) string {
	category := normalizeContentModerationKeywordCategory(candidate.Category)
	keyword := strings.ToLower(strings.TrimSpace(candidate.Keyword))
	if category == ContentModerationKeywordCategoryJailbreak {
		return contentModerationReviewKindPromptInjection
	}
	for _, marker := range []string{"jailbreak", "prompt_injection", "system_prompt", "prompt_obfuscation", "agent_tool_permission"} {
		if strings.Contains(keyword, marker) {
			return contentModerationReviewKindPromptInjection
		}
	}
	return contentModerationReviewKindGeneral
}

func addSemanticReviewEscalationMetadata(metadata map[string]any, result ContentModerationSemanticReviewResult) {
	if metadata == nil || result.escalation == nil || !result.escalation.Attempted {
		return
	}
	trace := result.escalation
	metadata["semantic_review_escalation_attempted"] = true
	metadata["semantic_review_escalation_reason"] = trace.Reason
	metadata["semantic_review_escalation_model"] = trace.Model
	metadata["semantic_review_escalation_evidence_complete"] = trace.EvidenceComplete
	metadata["semantic_review_initial_model"] = trace.InitialModel
	metadata["semantic_review_initial_verdict"] = trace.InitialVerdict
	metadata["semantic_review_initial_severity"] = trace.InitialSeverity
	metadata["semantic_review_initial_confidence"] = trace.InitialConfidence
	if trace.Error != "" {
		metadata["semantic_review_escalation_error"] = trace.Error
	}
}
