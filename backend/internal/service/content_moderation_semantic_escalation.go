package service

import (
	"context"
	"errors"
	"strings"
)

const semanticReviewEscalationReasonInitialReview = "initial_review"

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
	input.ReasoningEffort = cfg.EscalationReasoningEffort
	if strings.TrimSpace(input.UsageRecordID) != "" {
		input.UsageRecordID += "-escalation"
	}
	result, err := s.semanticReviewRouter.Review(ctx, escalationCfg, input)
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

func semanticReviewEscalationFailClosed(cfg *ContentModerationConfig, attempted bool) bool {
	return attempted && cfg != nil && cfg.Mode == ContentModerationModePreBlock &&
		cfg.SemanticReview.EscalationFailClosed
}

func applySemanticReviewEscalationEvidencePolicy(
	result ContentModerationSemanticReviewResult,
	evidenceComplete bool,
) (ContentModerationSemanticReviewResult, bool) {
	result = normalizeSemanticReviewResult(result)
	if evidenceComplete || result.Verdict == "review" {
		return result, false
	}
	if result.Verdict == "reject" {
		result.ModelSeverity = result.Severity
	}
	result.Verdict = "review"
	result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "semantic_policy_incomplete_escalation_evidence")
	return result, true
}
