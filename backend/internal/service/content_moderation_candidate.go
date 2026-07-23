package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/promptfilter"
)

const (
	contentModerationDecisionSourceOrdinaryAPI    = "ordinary_api"
	contentModerationDecisionSourceSemantic       = "semantic_review"
	contentModerationDecisionSourceInfrastructure = "infrastructure"
	contentModerationDecisionSourceExtraction     = "local_extraction"

	contentModerationSourceOriginUserTurn        = "user_turn"
	contentModerationSourceOriginPlatformWrapper = "platform_wrapper"

	contentModerationCandidateKindKeyword         = "keyword"
	contentModerationCandidateKindPromptFilter    = "prompt_filter"
	contentModerationCandidateKindLocalClassifier = "local_classifier"

	contentModerationCandidateRouteOrdinary = "ordinary"
	contentModerationCandidateRouteSemantic = "semantic"

	contentModerationCandidateFailureCacheTTL      = 15 * time.Second
	contentModerationDecisionCacheOperationTimeout = 2 * time.Second
	contentModerationCandidatePreferredRunes       = 1_200
	contentModerationCandidateEvidenceRevision     = "candidate-evidence-v2"
	contentModerationReviewKindGeneral             = "general"
	contentModerationReviewKindPromptInjection     = "prompt_injection"
)

type contentModerationCandidateSelection struct {
	Source                 ContentModerationInputSource
	Origin                 string
	Kind                   string
	Rule                   ContentModerationKeywordRule
	Fragment               string
	ReviewText             string
	ReviewKind             string
	EvidenceComplete       bool
	EvidenceRunes          int
	EvidenceRevision       string
	EvidenceDigest         string
	EvidenceWindowed       bool
	EvidenceWindows        int
	EvidenceMatchesTotal   int
	EvidenceMatchesCovered int
	Route                  string
	MatchStartByte         int
	MatchEndByte           int
	PromptHit              *contentModerationPromptFilterHit
}

func (selection contentModerationCandidateSelection) input() ContentModerationInput {
	source := selection.Source
	source.Text = selection.Fragment
	return ContentModerationInput{
		Text:    selection.Fragment,
		Sources: []ContentModerationInputSource{source},
	}
}

func (selection contentModerationCandidateSelection) hash() string {
	return selection.input().Hash()
}

func (selection contentModerationCandidateSelection) metadata() contentModerationSelectionMetadata {
	return contentModerationSelectionMetadata{
		SchemaVersion:          1,
		CandidateKind:          selection.Kind,
		CandidateKeyword:       selection.Rule.Keyword,
		CandidateCategory:      selection.Rule.Category,
		CandidateSeverity:      selection.Rule.Severity,
		Route:                  selection.Route,
		SourceOrigin:           selection.Origin,
		SelectedSource:         selection.Source.Source,
		SelectedSourceRole:     selection.Source.Role,
		SelectedFragmentRunes:  len([]rune(selection.Fragment)),
		ReviewKind:             selection.ReviewKind,
		EvidenceComplete:       selection.EvidenceComplete,
		EvidenceRunes:          selection.EvidenceRunes,
		EvidenceRevision:       selection.EvidenceRevision,
		EvidenceDigest:         selection.EvidenceDigest,
		EvidenceWindowed:       selection.EvidenceWindowed,
		EvidenceWindows:        selection.EvidenceWindows,
		EvidenceMatchesTotal:   selection.EvidenceMatchesTotal,
		EvidenceMatchesCovered: selection.EvidenceMatchesCovered,
	}
}

// contentModerationCandidateSelectionForInput deliberately chooses exactly one
// matched user source. Context wrappers, system instructions, tool output, and
// history cannot be concatenated into the provider prompt. We walk backwards
// so the most recent risky user turn wins, while still catching a risky turn
// in request history when the final turn is benign.
func contentModerationCandidateSelectionForInput(cfg *ContentModerationConfig, content ContentModerationInput) (contentModerationCandidateSelection, bool) {
	for index := len(content.Sources) - 1; index >= 0; index-- {
		source := content.Sources[index]
		if cfg != nil && (cfg.SemanticReview.PromptInjectionReviewerEnabled || cfg.SemanticReview.PromptInjectionFailClosed) {
			source = contentModerationCandidateFullReviewSource(content, source)
		}
		if !contentModerationSourceIsActionableUserTurn(source) {
			continue
		}
		if selection, found := contentModerationCandidateSelectionForSource(cfg, source); found {
			return selection, true
		}
	}

	return contentModerationCandidateSelection{}, false
}

func contentModerationCandidateFullReviewSource(content ContentModerationInput, source ContentModerationInputSource) ContentModerationInputSource {
	for index := len(content.Extraction.Sources) - 1; index >= 0; index-- {
		extracted := content.Extraction.Sources[index]
		if strings.TrimSpace(extracted.Source) != strings.TrimSpace(source.Source) ||
			strings.ToLower(strings.TrimSpace(extracted.Role)) != strings.ToLower(strings.TrimSpace(source.Role)) {
			continue
		}
		source.Text = normalizeContentModerationText(extracted.Text)
		source.Truncated = extracted.Truncated || !content.Extraction.Complete
		source.TruncateReasons = normalizeContentModerationTruncateReasons(append(
			append([]string(nil), extracted.TruncateReasons...),
			content.Extraction.TruncateReasons...,
		))
		return source
	}
	return source
}

func contentModerationCandidateSelectionFromRule(cfg *ContentModerationConfig, source ContentModerationInputSource, origin string, rule ContentModerationKeywordRule, kind string) contentModerationCandidateSelection {
	startByte, endByte := -1, -1
	if start, end, found := findDisplayKeywordSpanWithBoundary(source.Text, rule.Keyword); found {
		startByte, endByte = start, end
	}
	return contentModerationCandidateSelectionFromRuleAt(cfg, source, origin, rule, kind, startByte, endByte)
}

func contentModerationCandidateSelectionFromRuleAt(cfg *ContentModerationConfig, source ContentModerationInputSource, origin string, rule ContentModerationKeywordRule, kind string, startByte, endByte int) contentModerationCandidateSelection {
	rule = normalizeContentModerationKeywordRules([]ContentModerationKeywordRule{rule})[0]
	maxRunes := maxContentModerationCandidateRunes
	if cfg != nil && cfg.CandidateFragmentRunes > 0 {
		maxRunes = cfg.CandidateFragmentRunes
	}
	maxRunes = contentModerationCandidateAdaptiveRunes(source.Text, maxRunes)
	fragment := contentModerationCandidateFragment(source.Text, rule.Keyword, maxRunes)
	if startByte >= 0 && endByte > startByte {
		fragment = contentModerationCandidateFragmentAroundByteSpan(source.Text, startByte, endByte, maxRunes)
	}
	reviewKind := contentModerationReviewKindGeneral
	if contentModerationCandidateIsPromptInjection(kind, rule) {
		reviewKind = contentModerationReviewKindPromptInjection
	}
	return contentModerationCandidateSelection{
		Source:           source,
		Origin:           origin,
		Kind:             kind,
		Rule:             rule,
		Fragment:         fragment,
		ReviewText:       source.Text,
		ReviewKind:       reviewKind,
		EvidenceComplete: !source.Truncated && len(source.TruncateReasons) == 0,
		EvidenceRunes:    len([]rune(source.Text)),
		EvidenceRevision: contentModerationCandidateEvidenceRevision,
		Route:            contentModerationCandidateRouteFor(cfg, rule.Category),
		MatchStartByte:   startByte,
		MatchEndByte:     endByte,
	}
}

func contentModerationCandidateIsPromptInjection(kind string, rule ContentModerationKeywordRule) bool {
	if normalizeContentModerationKeywordCategory(rule.Category) == ContentModerationKeywordCategoryJailbreak {
		return true
	}
	if kind != contentModerationCandidateKindPromptFilter {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(rule.Keyword))
	for _, marker := range []string{"jailbreak", "prompt_injection", "system_prompt", "prompt_obfuscation", "agent_tool_permission"} {
		if strings.Contains(name, marker) {
			return true
		}
	}
	return false
}

func contentModerationCandidateAdaptiveRunes(text string, configuredMax int) int {
	if configuredMax <= 0 || configuredMax > maxContentModerationCandidateRunes {
		configuredMax = maxContentModerationCandidateRunes
	}
	if configuredMax <= contentModerationCandidatePreferredRunes {
		return configuredMax
	}
	comparable := normalizeKeywordComparable(text)
	for _, marker := range []string{
		"authorized", "authorization", "permission", "self-owned", "self owned",
		"ctf", "capture the flag", "sandbox", "lab", "third party", "third-party",
		"授权", "未经授权", "已获许可", "自有", "自己的系统", "靶场", "第三方", "测试环境",
	} {
		if strings.Contains(comparable, normalizeKeywordComparable(marker)) {
			return configuredMax
		}
	}
	return contentModerationCandidatePreferredRunes
}

func contentModerationCandidateSelectionForSource(cfg *ContentModerationConfig, source ContentModerationInputSource) (contentModerationCandidateSelection, bool) {
	if cfg == nil || !contentModerationSourceIsActionableUserTurn(source) {
		return contentModerationCandidateSelection{}, false
	}
	candidates := make([]contentModerationCandidateSelection, 0, 4)
	for _, rule := range normalizeContentModerationKeywordRules(cfg.keywordRules()) {
		if !rule.Enabled {
			continue
		}
		if _, hit := matchContentModerationKeyword(source.Text, []ContentModerationKeywordRule{rule}); !hit {
			continue
		}
		startByte, endByte := -1, -1
		if start, end, found := findDisplayKeywordSpanWithBoundary(source.Text, rule.Keyword); found {
			startByte, endByte = start, end
		}
		candidates = append(candidates, contentModerationCandidateSelectionFromRuleAt(
			cfg,
			source,
			contentModerationSourceOriginUserTurn,
			rule,
			contentModerationCandidateKindKeyword,
			startByte,
			endByte,
		))
	}
	if rule, hit := matchContextualBuiltInRiskRule(source.Text); hit {
		candidates = append(candidates, contentModerationCandidateSelectionFromRule(cfg, source, contentModerationSourceOriginUserTurn, rule, contentModerationCandidateKindKeyword))
	}

	verdict := promptfilter.Inspect(source.Text, cfg.promptFilterConfig())
	if match, hit := contentModerationCandidatePromptFilterMatch(verdict.Matches); hit {
		category := normalizeContentModerationKeywordCategory(match.Category)
		if strings.TrimSpace(match.Category) == "" {
			category = ContentModerationKeywordCategoryCyber
		}
		rule := ContentModerationKeywordRule{
			Keyword:  strings.TrimSpace(match.Name),
			Category: category,
			Severity: promptFilterSeverity(verdict),
			Action:   ContentModerationKeywordActionBlock,
			Enabled:  true,
		}
		selection := contentModerationCandidateSelectionFromRuleAt(cfg, source, contentModerationSourceOriginUserTurn, rule, contentModerationCandidateKindPromptFilter, match.StartByte, match.EndByte)
		selection.PromptHit = &contentModerationPromptFilterHit{Source: source, Verdict: verdict}
		candidates = append(candidates, selection)
	}

	if cfg.LocalClassifier.Enabled {
		if candidate, hit := contentModerationLocalClassifierCandidateForText(source.Text); hit {
			rule := ContentModerationKeywordRule{
				Keyword:  candidate.Keyword,
				Category: candidate.Category,
				Severity: candidate.Severity,
				Action:   ContentModerationKeywordActionBlock,
				Enabled:  true,
			}
			candidates = append(candidates, contentModerationCandidateSelectionFromRule(cfg, source, contentModerationSourceOriginUserTurn, rule, contentModerationCandidateKindLocalClassifier))
		}
	}
	if len(candidates) == 0 {
		return contentModerationCandidateSelection{}, false
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if contentModerationCandidateSelectionPreferred(candidate, selected) {
			selected = candidate
		}
	}
	if selected.ReviewKind == contentModerationReviewKindPromptInjection &&
		(cfg.SemanticReview.PromptInjectionReviewerEnabled || cfg.SemanticReview.PromptInjectionFailClosed) {
		selected = contentModerationCandidateBuildPromptInjectionEvidence(cfg, selected)
	}
	return selected, true
}

func contentModerationCandidateBuildPromptInjectionEvidence(cfg *ContentModerationConfig, selection contentModerationCandidateSelection) contentModerationCandidateSelection {
	matches := []promptfilter.Match(nil)
	scanComplete := true
	if selection.PromptHit != nil {
		matches = selection.PromptHit.Verdict.Matches
		scanComplete = selection.PromptHit.Verdict.ScanComplete
	}
	maxRunes := maxModerationInputRunes
	if cfg != nil && cfg.SemanticReview.PromptInjectionMaxInputRunes > 0 {
		maxRunes = cfg.SemanticReview.PromptInjectionMaxInputRunes
	}
	evidence, err := buildContentModerationPromptInjectionEvidence(contentModerationPromptInjectionEvidenceInput{
		SourceText:     selection.Source.Text,
		Matches:        matches,
		MaxRunes:       maxRunes,
		SourceComplete: selection.EvidenceComplete && scanComplete,
	})
	if err != nil {
		selection.ReviewText = redactContentModerationSecrets(selection.Fragment)
		selection.EvidenceComplete = false
		selection.EvidenceRunes = len([]rune(selection.ReviewText))
		selection.EvidenceRevision = contentModerationPromptInjectionEvidenceRevision
		return selection
	}
	selection.ReviewText = evidence.Text
	selection.EvidenceComplete = evidence.Complete
	selection.EvidenceRunes = evidence.Runes
	selection.EvidenceRevision = evidence.Revision
	selection.EvidenceDigest = evidence.Digest
	selection.EvidenceWindowed = evidence.Windowed
	selection.EvidenceWindows = evidence.WindowCount
	selection.EvidenceMatchesTotal = evidence.MatchesTotal
	selection.EvidenceMatchesCovered = evidence.MatchesCovered
	return selection
}

func contentModerationCandidatePromptFilterMatch(matches []promptfilter.Match) (promptfilter.Match, bool) {
	if len(matches) == 0 {
		return promptfilter.Match{}, false
	}
	selected := matches[0]
	for _, candidate := range matches[1:] {
		if contentModerationCandidatePromptFilterMatchPreferred(candidate, selected) {
			selected = candidate
		}
	}
	return selected, true
}

func contentModerationCandidatePromptFilterMatchPreferred(candidate, current promptfilter.Match) bool {
	if candidate.Operational != current.Operational {
		return candidate.Operational
	}
	if candidate.Strict != current.Strict {
		return candidate.Strict
	}
	if candidate.Weight != current.Weight {
		return candidate.Weight > current.Weight
	}
	candidateHasSpan := candidate.StartByte >= 0 && candidate.EndByte > candidate.StartByte
	currentHasSpan := current.StartByte >= 0 && current.EndByte > current.StartByte
	if candidateHasSpan != currentHasSpan {
		return candidateHasSpan
	}
	if candidate.StartByte != current.StartByte {
		return candidate.StartByte > current.StartByte
	}
	return candidate.Name < current.Name
}

func contentModerationCandidateSelectionPreferred(candidate, current contentModerationCandidateSelection) bool {
	// A semantic-only candidate must never be hidden by an ordinary moderation
	// keyword in the same user turn. The ordinary APIs intentionally do not
	// classify jailbreak, authorization, or cyber-operational intent.
	if candidate.Route != current.Route {
		return candidate.Route == contentModerationCandidateRouteSemantic
	}
	candidateSeverity := contentModerationCandidateSeverityRank(candidate.Rule.Severity)
	currentSeverity := contentModerationCandidateSeverityRank(current.Rule.Severity)
	if candidateSeverity != currentSeverity {
		return candidateSeverity > currentSeverity
	}
	candidateHasSpan := candidate.MatchStartByte >= 0 && candidate.MatchEndByte > candidate.MatchStartByte
	currentHasSpan := current.MatchStartByte >= 0 && current.MatchEndByte > current.MatchStartByte
	if candidateHasSpan != currentHasSpan {
		return candidateHasSpan
	}
	if candidate.MatchStartByte != current.MatchStartByte {
		return candidate.MatchStartByte > current.MatchStartByte
	}
	candidateKind := contentModerationCandidateKindRank(candidate.Kind)
	currentKind := contentModerationCandidateKindRank(current.Kind)
	if candidateKind != currentKind {
		return candidateKind > currentKind
	}
	return len([]rune(candidate.Rule.Keyword)) > len([]rune(current.Rule.Keyword))
}

func contentModerationCandidateKindRank(kind string) int {
	switch kind {
	case contentModerationCandidateKindPromptFilter:
		return 3
	case contentModerationCandidateKindKeyword:
		return 2
	case contentModerationCandidateKindLocalClassifier:
		return 1
	default:
		return 0
	}
}

func contentModerationCandidateSeverityRank(severity string) int {
	switch normalizeContentModerationKeywordSeverity(severity) {
	case ContentModerationKeywordSeverityCritical:
		return 4
	case ContentModerationKeywordSeverityHigh:
		return 3
	case ContentModerationKeywordSeverityMedium:
		return 2
	default:
		return 1
	}
}

func contentModerationSourceIsActionableUserTurn(source ContentModerationInputSource) bool {
	if !strings.EqualFold(strings.TrimSpace(source.Role), "user") {
		return false
	}
	// Source provenance comes from the protocol extractor. Do not treat marker
	// text such as "AGENTS.md" or "environment_context" as trusted context:
	// a caller can place those strings in a real user turn to bypass review.
	return strings.TrimSpace(source.Text) != ""
}

func contentModerationCandidateFragment(text, keyword string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || maxRunes > maxContentModerationCandidateRunes {
		maxRunes = maxContentModerationCandidateRunes
	}
	if len([]rune(text)) <= maxRunes {
		return text
	}
	if start, end, ok := findDisplayKeywordSpanWithBoundary(text, strings.TrimSpace(keyword)); ok {
		return contentModerationCandidateFragmentAroundByteSpan(text, start, end, maxRunes)
	}
	return trimRunes(text, maxRunes)
}

func contentModerationCandidateFragmentAroundByteSpan(text string, startByte, endByte, maxRunes int) string {
	if maxRunes <= 0 || maxRunes > maxContentModerationCandidateRunes {
		maxRunes = maxContentModerationCandidateRunes
	}
	return trimRunes(contentModerationExcerptAroundByteSpan(text, startByte, endByte, maxRunes), maxRunes)
}

func contentModerationCandidateRouteFor(cfg *ContentModerationConfig, category string) string {
	category = normalizeContentModerationKeywordCategory(category)
	if contentModerationCategoryRequiresSemanticReview(category) {
		return contentModerationCandidateRouteSemantic
	}

	provider := ""
	if cfg != nil {
		provider = strings.ToLower(strings.TrimSpace(cfg.Provider))
	}
	if contentModerationOrdinaryProviderSupportsCategory(provider, category) {
		return contentModerationCandidateRouteOrdinary
	}
	return contentModerationCandidateRouteSemantic
}

func contentModerationCategoryRequiresSemanticReview(category string) bool {
	switch category {
	case ContentModerationKeywordCategoryJailbreak,
		ContentModerationKeywordCategoryCyber,
		ContentModerationKeywordCategoryAccountAbuse,
		ContentModerationKeywordCategoryPrivacy,
		ContentModerationKeywordCategoryHighImpactDecision,
		ContentModerationKeywordCategoryRegulatedAdvice,
		ContentModerationKeywordCategoryCopyright,
		ContentModerationKeywordCategoryBiometric,
		ContentModerationKeywordCategoryCustom:
		return true
	default:
		return false
	}
}

func contentModerationOrdinaryProviderSupportsCategory(provider, category string) bool {
	switch provider {
	case "zhipu":
		switch category {
		case ContentModerationKeywordCategoryPolitical,
			ContentModerationKeywordCategoryFraud,
			ContentModerationKeywordCategoryMinorSafety,
			ContentModerationKeywordCategorySelfHarm,
			ContentModerationKeywordCategoryViolence,
			ContentModerationKeywordCategoryWeapons,
			ContentModerationKeywordCategoryOther:
			return true
		}
	case "openai":
		switch category {
		case ContentModerationKeywordCategoryMinorSafety,
			ContentModerationKeywordCategorySelfHarm,
			ContentModerationKeywordCategoryViolence,
			ContentModerationKeywordCategoryWeapons,
			ContentModerationKeywordCategoryOther:
			return true
		}
	}
	return false
}

func (s *ContentModerationService) candidateDecisionCacheKey(cfg *ContentModerationConfig, input ContentModerationCheckInput, selection contentModerationCandidateSelection) string {
	policyRevision := contentModerationPolicyRevision(true, cfg)
	namespace := "candidate-decision-v3"
	evidenceIdentity := selection.Fragment
	instructionsRevision := ""
	if selection.Route == contentModerationCandidateRouteSemantic {
		instructionsRevision = semanticReviewInstructionsRevision
	}
	if cfg != nil && cfg.SemanticReview.PromptInjectionReviewerEnabled && selection.ReviewKind == contentModerationReviewKindPromptInjection {
		namespace = "candidate-decision-v4"
		evidenceIdentity = selection.Source.Text
		instructionsRevision = promptInjectionReviewerInstructionsRevision
	}
	parts := []string{
		namespace,
		policyRevision,
		fmtInt64(input.UserID),
		fmtInt64(input.APIKeyID),
		fmtInt64(contentModerationLogGroupID(input.GroupID)),
		strings.TrimSpace(input.Endpoint),
		strings.TrimSpace(input.Protocol),
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Model),
		selection.Source.Source,
		selection.Source.Role,
		selection.Kind,
		selection.Rule.Keyword,
		selection.Rule.Category,
		selection.Rule.Severity,
		selection.Route,
		evidenceIdentity,
	}
	if instructionsRevision != "" {
		parts = append(parts, instructionsRevision)
	}
	if namespace == "candidate-decision-v4" {
		parts = append(parts,
			selection.ReviewKind,
			promptInjectionReviewerSchemaRevision,
			selection.EvidenceRevision,
			selection.EvidenceDigest,
			strconv.FormatBool(selection.EvidenceComplete),
			strconv.Itoa(selection.EvidenceRunes),
		)
	}
	// An incomplete extraction has no provider payload to identify it. Include
	// the bounded source text and its reasons so two different malformed user
	// turns cannot collapse into the same audit row merely because their
	// selected fragments happen to match.
	if contentModerationCandidateExtractionIncomplete(selection) {
		parts = append(parts,
			selection.Source.Text,
			strconv.FormatBool(selection.Source.Truncated),
			strings.Join(normalizeContentModerationTruncateReasons(selection.Source.TruncateReasons), ","),
		)
	}
	payload := strings.Join(parts, "\n")
	if key := s.candidateDecisionHMACKey(); len(key) == sha256.Size {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(payload))
		return hex.EncodeToString(mac.Sum(nil))
	}
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func (s *ContentModerationService) candidatePayloadHMAC(payload string) string {
	payload = strings.TrimSpace(payload)
	if key := s.candidateDecisionHMACKey(); len(key) == sha256.Size {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte("candidate-payload-v1\n" + payload))
		return hex.EncodeToString(mac.Sum(nil))
	}
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

func fmtInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

func cloneContentModerationDecision(in ContentModerationDecision) ContentModerationDecision {
	out := in
	out.CategoryScores = cloneFloatMap(in.CategoryScores)
	return out
}

type contentModerationCandidateOutcome struct {
	Decision   *ContentModerationDecision
	DecisionID string
	Cacheable  bool
	CacheHit   bool
	CacheTTL   time.Duration
}

func (s *ContentModerationService) executeCandidateDecision(
	ctx context.Context,
	cfg *ContentModerationConfig,
	input ContentModerationCheckInput,
	selection contentModerationCandidateSelection,
	run func(context.Context) contentModerationCandidateOutcome,
) contentModerationCandidateOutcome {
	key := s.candidateDecisionCacheKey(cfg, input, selection)
	if cached, ok := s.lookupCandidateDecisionCache(ctx, cfg, key); ok {
		decision := cloneContentModerationDecision(cached.Decision)
		return s.finishCandidateDecisionOutcome(ctx, contentModerationCandidateOutcome{Decision: &decision, DecisionID: cached.DecisionID, Cacheable: true, CacheHit: true}, false)
	}

	coordinator := s.candidateDecisionFlights
	outcome, joined, completed := coordinator.Do(ctx, key, func() contentModerationCandidateOutcome {
		if cached, ok := s.lookupCandidateDecisionCache(ctx, cfg, key); ok {
			return contentModerationCandidateOutcome{
				Decision:   ptrContentModerationDecision(cloneContentModerationDecision(cached.Decision)),
				DecisionID: cached.DecisionID,
				Cacheable:  true,
				CacheHit:   true,
			}
		}

		reviewCtx, cancel := s.candidateReviewContext(ctx, cfg)
		defer cancel()

		owner := contentModerationRandomToken()
		if s.distributedDecisionCacheEnabled(cfg) {
			lockTTL := s.candidateDecisionLockTTL(cfg)
			acquired, err := s.decisionCache.TryAcquire(reviewCtx, key, owner, lockTTL)
			if err != nil {
				// Redis is an optimization. Local coalescing remains active when it
				// is unavailable, so a cache outage cannot suppress moderation.
				slog.Warn("content_moderation.candidate_decision_lock_failed", "error", err)
			} else if !acquired {
				if cached, ok := s.waitForCandidateDecisionCache(reviewCtx, cfg, key); ok {
					return contentModerationCandidateOutcome{
						Decision:   ptrContentModerationDecision(cloneContentModerationDecision(cached.Decision)),
						DecisionID: cached.DecisionID,
						Cacheable:  true,
						CacheHit:   true,
					}
				}
				return s.cacheCandidateDecisionOutcome(cfg, key, s.candidateUnavailableOutcome(
					reviewCtx,
					input,
					cfg,
					selection,
					contentModerationDecisionSourceInfrastructure,
					"decision_cache",
					"",
					"decision_cache_lock_timeout",
				))
			} else {
				release := s.startCandidateDecisionLease(key, owner, lockTTL)
				defer release()
			}
		}

		outcome := run(reviewCtx)
		if outcome.Decision == nil {
			outcome = s.candidateUnavailableOutcome(
				reviewCtx,
				input,
				cfg,
				selection,
				contentModerationDecisionSourceOrdinaryAPI,
				"decision_pipeline",
				"",
				"empty_candidate_decision",
			)
		}
		return s.cacheCandidateDecisionOutcome(cfg, key, outcome)
	})
	if !completed {
		return contentModerationCandidateOutcome{Decision: contentModerationFailureDecision(cfg)}
	}
	return s.finishCandidateDecisionOutcome(ctx, outcome, joined)
}

func (s *ContentModerationService) finishCandidateDecisionOutcome(ctx context.Context, outcome contentModerationCandidateOutcome, joined bool) contentModerationCandidateOutcome {
	if joined || outcome.CacheHit {
		s.recordCandidateDuplicateRetry(ctx, outcome.DecisionID)
	}
	if outcome.Decision != nil {
		decision := cloneContentModerationDecision(*outcome.Decision)
		decision.candidateDecisionID = outcome.DecisionID
		outcome.Decision = &decision
	}
	return outcome
}

func ptrContentModerationDecision(value ContentModerationDecision) *ContentModerationDecision {
	return &value
}

func (s *ContentModerationService) decisionCacheEnabled(cfg *ContentModerationConfig) bool {
	return s != nil && cfg != nil && cfg.DecisionCacheEnabled && s.candidateDecisionMemory != nil && len(s.candidateDecisionHMACKey()) == sha256.Size
}

func (s *ContentModerationService) distributedDecisionCacheEnabled(cfg *ContentModerationConfig) bool {
	return s.decisionCacheEnabled(cfg) && s.decisionCache != nil
}

func (s *ContentModerationService) candidateDecisionTTL(cfg *ContentModerationConfig) time.Duration {
	seconds := defaultContentModerationDecisionCacheTTLSeconds
	if cfg != nil && cfg.DecisionCacheTTLSeconds > 0 {
		seconds = cfg.DecisionCacheTTLSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (s *ContentModerationService) candidateDecisionLockTTL(cfg *ContentModerationConfig) time.Duration {
	return s.candidateReviewTimeout(cfg) + 5*time.Second
}

func (s *ContentModerationService) candidateReviewContext(parent context.Context, cfg *ContentModerationConfig) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), s.candidateReviewTimeout(cfg))
}

func (s *ContentModerationService) candidateReviewTimeout(cfg *ContentModerationConfig) time.Duration {
	timeout := 30 * time.Second
	if cfg != nil && cfg.RequestAuditTimeoutMS > 0 {
		timeout = time.Duration(cfg.RequestAuditTimeoutMS) * time.Millisecond
	}
	if timeout < time.Second {
		return time.Second
	}
	if timeout > 5*time.Minute {
		return 5 * time.Minute
	}
	return timeout
}

func (s *ContentModerationService) cacheCandidateDecisionOutcome(cfg *ContentModerationConfig, key string, outcome contentModerationCandidateOutcome) contentModerationCandidateOutcome {
	if outcome.Decision == nil || !outcome.Cacheable || !s.decisionCacheEnabled(cfg) {
		return outcome
	}
	ttl := outcome.CacheTTL
	if ttl <= 0 {
		ttl = s.candidateDecisionTTL(cfg)
	}
	entry := ContentModerationCachedDecision{
		Decision:   cloneContentModerationDecision(*outcome.Decision),
		DecisionID: outcome.DecisionID,
	}
	s.storeCandidateDecisionCache(key, entry, ttl)
	return outcome
}

func (s *ContentModerationService) startCandidateDecisionLease(key, owner string, ttl time.Duration) func() {
	if s == nil || s.decisionCache == nil || ttl <= 0 {
		return func() {}
	}
	refreshEvery := ttl / 3
	if refreshEvery < time.Second {
		refreshEvery = time.Second
	}
	if refreshEvery > 10*time.Second {
		refreshEvery = 10 * time.Second
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(refreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				leaseCtx, cancel := context.WithTimeout(context.Background(), contentModerationDecisionCacheOperationTimeout)
				renewed, err := s.decisionCache.Renew(leaseCtx, key, owner, ttl)
				cancel()
				if err != nil {
					slog.Warn("content_moderation.candidate_decision_lock_renew_failed", "error", err)
					continue
				}
				if !renewed {
					slog.Warn("content_moderation.candidate_decision_lock_lost")
					return
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		releaseCtx, cancel := context.WithTimeout(context.Background(), contentModerationDecisionCacheOperationTimeout)
		defer cancel()
		if err := s.decisionCache.Release(releaseCtx, key, owner); err != nil {
			slog.Warn("content_moderation.candidate_decision_lock_release_failed", "error", err)
		}
	}
}

func (s *ContentModerationService) lookupCandidateDecisionCache(ctx context.Context, cfg *ContentModerationConfig, key string) (*ContentModerationCachedDecision, bool) {
	if !s.decisionCacheEnabled(cfg) {
		return nil, false
	}
	if entry, ok := s.candidateDecisionMemory.Get(key); ok {
		return entry, true
	}
	if s.decisionCache == nil {
		return nil, false
	}
	entry, err := s.decisionCache.Get(ctx, key)
	if err != nil {
		slog.Warn("content_moderation.candidate_decision_cache_read_failed", "error", err)
		return nil, false
	}
	if entry == nil {
		return nil, false
	}
	s.candidateDecisionMemory.Store(key, *entry, s.candidateDecisionTTL(cfg))
	return entry, true
}

func (s *ContentModerationService) storeCandidateDecisionCache(key string, entry ContentModerationCachedDecision, ttl time.Duration) {
	if s == nil {
		return
	}
	if s.candidateDecisionMemory != nil {
		s.candidateDecisionMemory.Store(key, entry, ttl)
	}
	if s.decisionCache == nil {
		return
	}
	cacheCtx, cancel := context.WithTimeout(context.Background(), contentModerationDecisionCacheOperationTimeout)
	defer cancel()
	if err := s.decisionCache.Store(cacheCtx, key, entry, ttl); err != nil {
		slog.Warn("content_moderation.candidate_decision_cache_store_failed", "error", err)
	}
}

func (s *ContentModerationService) waitForCandidateDecisionCache(ctx context.Context, cfg *ContentModerationConfig, key string) (*ContentModerationCachedDecision, bool) {
	deadline := time.Now().Add(s.candidateDecisionLockTTL(cfg))
	for time.Now().Before(deadline) {
		if entry, ok := s.lookupCandidateDecisionCache(ctx, cfg, key); ok {
			return entry, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(25 * time.Millisecond):
		}
	}
	return nil, false
}

func (s *ContentModerationService) recordCandidateDuplicateRetry(ctx context.Context, decisionID string) {
	if s == nil || strings.TrimSpace(decisionID) == "" {
		return
	}
	if s.enqueueDuplicateRetry(decisionID) {
		return
	}
	s.runtimeMu.Lock()
	running := s.runtimeStarted && !s.runtimeClosed
	s.runtimeMu.Unlock()
	if running {
		slog.Warn("content_moderation.duplicate_retry_queue_full", "decision_id", decisionID)
		return
	}
	s.recordCandidateDuplicateRetrySync(ctx, decisionID)
}

func (s *ContentModerationService) recordCandidateDuplicateRetrySync(ctx context.Context, decisionID string) {
	if s == nil || strings.TrimSpace(decisionID) == "" {
		return
	}
	repo, ok := s.repo.(ContentModerationRetryDedupeRepository)
	if !ok {
		return
	}
	recordCtx, cancel := contentModerationDetachedContext(ctx, contentModerationPersistenceTimeout)
	defer cancel()
	if err := repo.IncrementDuplicateRetryCount(recordCtx, decisionID); err != nil {
		slog.Warn("content_moderation.duplicate_retry_increment_failed", "decision_id", decisionID, "error", err)
	}
}

func (s *ContentModerationService) checkCandidateOnly(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	selection, found := contentModerationCandidateSelectionForInput(cfg, content)
	if !found {
		s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
		return allow
	}
	if contentModerationCandidateExtractionIncomplete(selection) {
		outcome := s.executeCandidateDecision(ctx, cfg, input, selection, func(reviewCtx context.Context) contentModerationCandidateOutcome {
			return s.candidateExtractionFailureOutcome(reviewCtx, input, cfg, content, &selection)
		})
		if outcome.Decision == nil {
			return contentModerationFailureDecision(cfg)
		}
		decision := cloneContentModerationDecision(*outcome.Decision)
		decision.candidateDecisionID = outcome.DecisionID
		return &decision
	}
	if strings.TrimSpace(selection.Fragment) == "" {
		return allow
	}

	outcome := s.executeCandidateDecision(ctx, cfg, input, selection, func(reviewCtx context.Context) contentModerationCandidateOutcome {
		return s.runCandidateSelection(reviewCtx, input, cfg, selection)
	})
	if outcome.Decision == nil {
		return contentModerationFailureDecision(cfg)
	}
	decision := cloneContentModerationDecision(*outcome.Decision)
	decision.candidateDecisionID = outcome.DecisionID
	return &decision
}

// checkPromptInjectionBaseline runs before ordinary group/model/account scope.
// It only evaluates prompt-injection candidates from actionable user sources;
// ordinary moderation remains governed by the configured scope.
func (s *ContentModerationService) checkPromptInjectionBaseline(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig) (*ContentModerationDecision, bool) {
	if s == nil || cfg == nil || cfg.Mode == ContentModerationModeOff ||
		(!cfg.SemanticReview.PromptInjectionReviewerEnabled && !cfg.SemanticReview.PromptInjectionFailClosed) {
		return nil, false
	}
	baselineCfg := cloneContentModerationConfig(cfg)
	baselineCfg.AuditScope = ContentModerationAuditScopeUserOnly
	baselineCfg.PromptFilterMode = promptfilter.ModeObserve
	content := ExtractContentModerationInput(input.Protocol, input.Body, ContentModerationAuditScopeUserOnly)
	content.Normalize()
	selection, found := contentModerationCandidateSelectionForInput(baselineCfg, content)
	if !found || selection.ReviewKind != contentModerationReviewKindPromptInjection {
		return nil, false
	}
	outcome := s.executeCandidateDecision(ctx, baselineCfg, input, selection, func(reviewCtx context.Context) contentModerationCandidateOutcome {
		if contentModerationCandidateExtractionIncomplete(selection) {
			return s.candidateExtractionFailureOutcome(reviewCtx, input, baselineCfg, content, &selection)
		}
		return s.runCandidateSelection(reviewCtx, input, baselineCfg, selection)
	})
	if outcome.Decision == nil {
		return contentModerationFailureDecision(baselineCfg), true
	}
	decision := cloneContentModerationDecision(*outcome.Decision)
	decision.candidateDecisionID = outcome.DecisionID
	return &decision, true
}

// checkCandidateOnlyAccountAttempt intentionally avoids the legacy async
// semantic enqueue path. Account selection can retry the same gateway request
// several times; candidate mode must reuse the bounded synchronous decision,
// not schedule another review over the full request context.
func (s *ContentModerationService) checkCandidateOnlyAccountAttempt(
	ctx context.Context,
	input ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	riskEnabled bool,
	content ContentModerationInput,
	inputHash string,
	policyRevision string,
) (*ContentModerationGateResult, error) {
	snapshotCtx := context.WithValue(ctx, contentModerationPolicySnapshotContextKey{}, contentModerationPolicySnapshot{
		riskEnabled: riskEnabled,
		config:      cloneContentModerationConfig(cfg),
	})
	decision, err := s.Check(snapshotCtx, input)
	if err != nil {
		return nil, err
	}
	disposition := ContentModerationDispositionAllowed
	reusable := true
	switch {
	case decision != nil && decision.Action == ContentModerationActionError:
		disposition = ContentModerationDispositionProviderErrorOpen
		reusable = false
	case decision != nil && decision.Blocked:
		disposition = ContentModerationDispositionBlocked
		reusable = false
	case !riskEnabled || !cfg.Enabled || cfg.Mode == ContentModerationModeOff || content.IsEmpty():
		disposition = ContentModerationDispositionDeterministicAllow
	}
	result := &ContentModerationGateResult{
		Disposition:    disposition,
		Decision:       decision,
		InputHash:      inputHash,
		PolicyRevision: policyRevision,
	}
	if reusable {
		candidateDecisionID := ""
		if decision != nil {
			candidateDecisionID = decision.candidateDecisionID
		}
		result.NextState = &ContentModerationAttemptState{
			Disposition:         disposition,
			Decision:            decision,
			InputHash:           inputHash,
			PolicyRevision:      policyRevision,
			Reusable:            true,
			candidateDecisionID: candidateDecisionID,
			policySnapshot: &contentModerationPolicySnapshot{
				riskEnabled: riskEnabled,
				config:      cloneContentModerationConfig(cfg),
			},
		}
	}
	return result, nil
}

func (s *ContentModerationService) runCandidateSelection(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, selection contentModerationCandidateSelection) contentModerationCandidateOutcome {
	if selection.Kind == contentModerationCandidateKindLocalClassifier {
		resolved, decision := s.resolveLocalClassifierCandidate(ctx, input, cfg, selection)
		if decision != nil {
			if cfg.Mode == ContentModerationModePreBlock {
				s.recordPreBlockSyncMetric(0, decision.Action)
			}
			metadata := resolved.metadata().mapValue()
			metadata["local_classifier_verdict"] = decision.Action
			log := s.buildCandidateLog(
				input,
				cfg,
				resolved,
				contentModerationCandidateKindLocalClassifier,
				decision.Action,
				decision.Flagged,
				"local_classifier",
				0,
				nil,
				nil,
				marshalContentModerationMetadata(metadata),
			)
			log.ModerationProvider = "local_classifier"
			decisionID := s.persistCandidateAudit(ctx, input, cfg, resolved, log, false)
			return contentModerationCandidateOutcome{Decision: decision, DecisionID: decisionID, Cacheable: decision.Action != ContentModerationActionError}
		}
		selection = resolved
	}
	if selection.Route == contentModerationCandidateRouteSemantic {
		return s.runCandidateSemanticReview(ctx, input, cfg, selection, "")
	}
	return s.runCandidateOrdinaryReview(ctx, input, cfg, selection)
}

func (s *ContentModerationService) resolveLocalClassifierCandidate(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, selection contentModerationCandidateSelection) (contentModerationCandidateSelection, *ContentModerationDecision) {
	base, ok := contentModerationLocalClassifierCandidateForText(selection.Source.Text)
	if !ok {
		return selection, &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	}
	response, err := s.callLocalClassifier(ctx, cfg, input, selection.input(), base)
	if err != nil {
		selection.Route = contentModerationCandidateRouteSemantic
		return selection, nil
	}
	rule, action := contentModerationRuleFromLocalClassifierResponse(cfg, base, response)
	if action == "" {
		return selection, &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	}
	selection.Rule = rule
	selection.Route = contentModerationCandidateRouteFor(cfg, rule.Category)
	return selection, nil
}

func (s *ContentModerationService) runCandidateOrdinaryReview(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, selection contentModerationCandidateSelection) contentModerationCandidateOutcome {
	if len(cfg.apiKeys()) == 0 {
		return s.runCandidateSemanticReview(ctx, input, cfg, selection, "ordinary_moderation_api_key_unavailable")
	}
	started := time.Now()
	result, err := s.callModerationContent(ctx, cfg, selection.input(), true)
	latency := int(time.Since(started).Milliseconds())
	if err != nil {
		return s.runCandidateSemanticReview(ctx, input, cfg, selection, sanitizeSemanticReviewError(err.Error()))
	}
	if result != nil && result.ProviderLevel == ModerationLevelReview {
		return s.runCandidateSemanticReview(ctx, input, cfg, selection, "ordinary_moderation_review")
	}
	if result == nil {
		return s.candidateUnavailableOutcome(
			ctx,
			input,
			cfg,
			selection,
			contentModerationDecisionSourceOrdinaryAPI,
			cfg.Provider,
			cfg.Model,
			"ordinary_moderation_empty_result",
		)
	}

	flaggedByScore, highestCategory, highestScore := evaluateModerationScores(result.CategoryScores, cfg.Thresholds)
	flagged := result.Flagged || flaggedByScore
	if result.ProviderLevel == ModerationLevelReject {
		flagged = true
	}
	if !flagged && contentModerationCandidateOrdinaryAllowRequiresSemantic(selection) {
		return s.runCandidateSemanticReview(ctx, input, cfg, selection, "ordinary_moderation_high_risk_allow")
	}
	action := ContentModerationActionAllow
	blocked := false
	if flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	if cfg.Mode == ContentModerationModePreBlock {
		s.recordPreBlockSyncMetric(latency, action)
	}

	decision := &ContentModerationDecision{
		Allowed:                !blocked,
		Blocked:                blocked,
		Flagged:                flagged,
		Message:                "",
		StatusCode:             0,
		HighestCategory:        highestCategory,
		HighestScore:           highestScore,
		CategoryScores:         cloneFloatMap(result.CategoryScores),
		Action:                 action,
		MatchedKeyword:         selection.Rule.Keyword,
		KeywordCategory:        selection.Rule.Category,
		KeywordSeverity:        selection.Rule.Severity,
		KeywordAction:          normalizeContentModerationKeywordAction(selection.Rule.Action),
		EffectiveKeywordAction: normalizeContentModerationKeywordAction(selection.Rule.Action),
		RiskContextType:        ContentModerationRiskContextActualRequest,
		RiskContextReason:      "candidate_ordinary_moderation",
	}
	if blocked {
		decision.Message = cfg.BlockMessage
		decision.StatusCode = cfg.BlockStatus
	}
	log := s.buildCandidateLog(input, cfg, selection, contentModerationDecisionSourceOrdinaryAPI, action, flagged, highestCategory, highestScore, result.CategoryScores, &latency, "")
	decisionID := s.persistCandidateAudit(ctx, input, cfg, selection, log, blocked)
	return contentModerationCandidateOutcome{Decision: decision, DecisionID: decisionID, Cacheable: true}
}

func (s *ContentModerationService) runCandidateSemanticReview(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, selection contentModerationCandidateSelection, ordinaryReason string) contentModerationCandidateOutcome {
	if promptInjectionFailClosedActive(cfg, selection) && !cfg.SemanticReview.PromptInjectionReviewerEnabled {
		return s.candidateUnavailableOutcome(
			ctx, input, cfg, selection, contentModerationDecisionSourceSemantic,
			"platform_openai", ContentModerationSemanticReviewPrimaryModel,
			"prompt_injection_reviewer_disabled",
		)
	}
	if s == nil || s.semanticReviewRouter == nil {
		return s.candidateUnavailableOutcome(
			ctx,
			input,
			cfg,
			selection,
			contentModerationDecisionSourceSemantic,
			"platform_openai",
			ContentModerationSemanticReviewPrimaryModel,
			"semantic_review_router_unavailable",
		)
	}
	semanticCfg := contentModerationSemanticReviewConfigForProviderFallback(cfg)
	semanticReviewText := contentModerationCandidateSemanticInput(selection)
	if semanticCfg.PromptInjectionReviewerEnabled && selection.ReviewKind == contentModerationReviewKindPromptInjection {
		semanticReviewText = selection.ReviewText
	}
	semanticInput := contentModerationSemanticReviewInputForCheck(
		input,
		semanticReviewText,
		semanticReviewDecisionID(input, s.candidateDecisionCacheKey(cfg, input, selection)),
	)
	// Phase 0 keeps the legacy candidate payload and 2K behavior explicit while
	// allowing the transport to honor larger effective limits for future
	// prompt-injection evidence. candidate_fragment_runes remains display-only.
	semanticInput.MaxInputRunes = maxContentModerationCandidateRunes
	semanticInput.ReviewKind = contentModerationReviewKindGeneral
	semanticInput.EvidenceComplete = true
	semanticInput.EvidenceRevision = "legacy-candidate-evidence-v1"
	if semanticCfg.PromptInjectionReviewerEnabled && selection.ReviewKind == contentModerationReviewKindPromptInjection {
		semanticInput.ReviewKind = selection.ReviewKind
		semanticInput.MaxInputRunes = semanticCfg.PromptInjectionMaxInputRunes
		semanticInput.EvidenceComplete = selection.EvidenceComplete
		semanticInput.EvidenceRevision = selection.EvidenceRevision
	}
	started := time.Now()
	result, err := s.semanticReviewRouter.Review(ctx, semanticCfg, semanticInput)
	latency := int(time.Since(started).Milliseconds())
	if err != nil {
		if s.metrics != nil {
			s.metrics.observeSemanticReview(semanticCfg.PrimaryModel, "error", started, OpenAIUsage{})
		}
		slog.Warn("content_moderation.candidate_semantic_review_failed", "candidate_category", selection.Rule.Category, "error", err)
		return s.candidateUnavailableOutcome(
			ctx,
			input,
			cfg,
			selection,
			contentModerationDecisionSourceSemantic,
			"platform_openai",
			semanticCfg.PrimaryModel,
			sanitizeSemanticReviewError(err.Error()),
		)
	}
	if s.metrics != nil {
		s.metrics.observeSemanticReview(result.Model, result.Verdict, started, result.Usage)
	}
	var policyOverride bool
	if semanticInput.ReviewKind == contentModerationReviewKindPromptInjection {
		result, policyOverride = applyPromptInjectionReviewPolicy(result, semanticInput.EvidenceComplete)
	} else {
		result, policyOverride = applySemanticReviewPolicy(result)
	}
	result, candidatePolicyOverride := applyCandidateSemanticReviewPolicy(result, selection, semanticInput.EvidenceComplete)
	policyOverride = policyOverride || candidatePolicyOverride
	if semanticInput.ReviewKind == contentModerationReviewKindPromptInjection && s.metrics != nil {
		s.metrics.observePromptInjectionReview(result.Verdict, semanticInput.EvidenceComplete, len([]rune(semanticInput.Text)))
	}
	category, score, scores := semanticReviewCategorySummary(result)
	buildDecision := func(action string, flagged, blocked bool) *ContentModerationDecision {
		decision := &ContentModerationDecision{
			Allowed:                !blocked,
			Blocked:                blocked,
			Flagged:                flagged,
			HighestCategory:        category,
			HighestScore:           score,
			CategoryScores:         scores,
			Action:                 action,
			MatchedKeyword:         selection.Rule.Keyword,
			KeywordCategory:        selection.Rule.Category,
			KeywordSeverity:        selection.Rule.Severity,
			KeywordAction:          normalizeContentModerationKeywordAction(selection.Rule.Action),
			EffectiveKeywordAction: normalizeContentModerationKeywordAction(selection.Rule.Action),
			RiskContextType:        ContentModerationRiskContextActualRequest,
			RiskContextReason:      "candidate_semantic_review",
		}
		if blocked {
			decision.Message = cfg.BlockMessage
			decision.StatusCode = cfg.BlockStatus
		}
		return decision
	}
	metadata := contentModerationCandidateSemanticMetadata(selection, result, policyOverride, ordinaryReason)
	switch result.Verdict {
	case "reject":
		blocked := cfg.Mode == ContentModerationModePreBlock
		action := ContentModerationActionSemanticReviewReject
		if blocked {
			s.recordPreBlockSyncMetric(latency, action)
		}
		log := s.buildCandidateLog(input, cfg, selection, contentModerationDecisionSourceSemantic, action, true, category, score, scores, &latency, metadata)
		log.ModerationProvider = "platform_openai"
		log.ModerationModel = result.Model
		decisionID := s.persistCandidateAudit(ctx, input, cfg, selection, log, blocked)
		return contentModerationCandidateOutcome{Decision: buildDecision(action, true, blocked), DecisionID: decisionID, Cacheable: true}
	case "review":
		action := ContentModerationActionSemanticReviewReview
		promptInjectionFailClosed := promptInjectionFailClosedActive(cfg, selection)
		highRiskDeferred := highRiskCandidateReviewDeferredActive(cfg, selection, result)
		failClosed := promptInjectionFailClosed || highRiskDeferred
		if failClosed {
			action = ContentModerationActionSemanticReviewDeferred
			if !semanticInput.EvidenceComplete {
				action = ContentModerationActionSemanticReviewIncomplete
			}
		}
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(latency, action)
		}
		log := s.buildCandidateLog(input, cfg, selection, contentModerationDecisionSourceSemantic, action, true, category, score, scores, &latency, metadata)
		log.ModerationProvider = "platform_openai"
		log.ModerationModel = result.Model
		log.ReviewStatus = ContentModerationReviewStatusPending
		if failClosed {
			// A fail-closed transport decision protects the request boundary, but is
			// not a confirmed user violation and must not feed enforcement counters.
			log.UserViolationEligible = false
		}
		decisionID := s.persistCandidateAudit(ctx, input, cfg, selection, log, false)
		if failClosed {
			decision := buildDecision(action, true, true)
			decision.StatusCode = http.StatusServiceUnavailable
			decision.Message = promptInjectionDeferredMessage(action)
			if promptInjectionFailClosed && s.metrics != nil {
				reason := "review"
				if action == ContentModerationActionSemanticReviewIncomplete {
					reason = "incomplete"
				}
				s.metrics.observePromptInjectionFailClosed(reason)
			}
			return contentModerationCandidateOutcome{Decision: decision, DecisionID: decisionID, Cacheable: true, CacheTTL: s.candidateFailureCacheTTL(cfg)}
		}
		return contentModerationCandidateOutcome{Decision: buildDecision(action, true, false), DecisionID: decisionID, Cacheable: true}
	default:
		log := s.buildCandidateLog(input, cfg, selection, contentModerationDecisionSourceSemantic, ContentModerationActionSemanticReviewAllow, false, category, score, scores, &latency, metadata)
		log.ModerationProvider = "platform_openai"
		log.ModerationModel = result.Model
		decisionID := s.persistCandidateAudit(ctx, input, cfg, selection, log, false)
		if cfg.Mode == ContentModerationModePreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionSemanticReviewAllow)
		}
		return contentModerationCandidateOutcome{Decision: buildDecision(ContentModerationActionSemanticReviewAllow, false, false), DecisionID: decisionID, Cacheable: true}
	}
}

const candidateSemanticReviewAutoRejectConfidence = 0.85

// applyCandidateSemanticReviewPolicy uses the local candidate as corroborating
// evidence. A high-confidence review from a general reviewer is still
// conservative for high-risk candidates, while explicit benign/defensive
// classifications remain reviewable instead of being rejected blindly.
func applyCandidateSemanticReviewPolicy(
	result ContentModerationSemanticReviewResult,
	selection contentModerationCandidateSelection,
	evidenceComplete bool,
) (ContentModerationSemanticReviewResult, bool) {
	result = normalizeSemanticReviewResult(result)
	if result.Verdict != "review" || !evidenceComplete || result.Confidence < candidateSemanticReviewAutoRejectConfidence {
		return result, false
	}
	if result.Intent == "benign" || result.Intent == "defensive" {
		return result, false
	}
	// Candidate severity is corroborating evidence, not a substitute for the
	// semantic dimensions required to reject. In particular, ambiguity or an
	// absent harm mechanism must remain reviewable.
	if !semanticReviewPolicyRejectEligible(result) {
		return result, false
	}
	category := normalizeContentModerationKeywordCategory(selection.Rule.Category)
	if !contentModerationCandidateIsHighRisk(selection, category) {
		return result, false
	}
	result.Verdict = "reject"
	result.Severity = ContentModerationKeywordSeverityHigh
	result.ReasonCodes = appendSemanticReviewReasonCode(result.ReasonCodes, "semantic_policy_high_risk_candidate")
	return result, true
}

func highRiskCandidateReviewDeferredActive(
	cfg *ContentModerationConfig,
	selection contentModerationCandidateSelection,
	result ContentModerationSemanticReviewResult,
) bool {
	if cfg == nil || cfg.Mode != ContentModerationModePreBlock {
		return false
	}
	category := normalizeContentModerationKeywordCategory(selection.Rule.Category)
	return contentModerationCandidateIsHighRisk(selection, category) ||
		semanticReviewResultIsHighRisk(result)
}

func contentModerationCandidateIsHighRisk(selection contentModerationCandidateSelection, normalizedCategory string) bool {
	severity := normalizeContentModerationKeywordSeverity(selection.Rule.Severity)
	return severity == ContentModerationKeywordSeverityHigh ||
		severity == ContentModerationKeywordSeverityCritical ||
		contentModerationDeferredHighRiskCategory(normalizedCategory)
}

func contentModerationDeferredHighRiskCategory(category string) bool {
	switch normalizeContentModerationKeywordCategory(category) {
	case ContentModerationKeywordCategoryJailbreak,
		ContentModerationKeywordCategoryCyber,
		ContentModerationKeywordCategoryMinorSafety,
		ContentModerationKeywordCategorySelfHarm,
		ContentModerationKeywordCategoryViolence,
		ContentModerationKeywordCategoryWeapons,
		ContentModerationKeywordCategoryPrivacy,
		ContentModerationKeywordCategoryFraud,
		ContentModerationKeywordCategoryAccountAbuse:
		return true
	default:
		return false
	}
}

func promptInjectionFailClosedActive(cfg *ContentModerationConfig, selection contentModerationCandidateSelection) bool {
	return cfg != nil && cfg.Mode == ContentModerationModePreBlock &&
		cfg.SemanticReview.PromptInjectionFailClosed &&
		selection.ReviewKind == contentModerationReviewKindPromptInjection
}

func promptInjectionDeferredMessage(action string) string {
	switch action {
	case ContentModerationActionSemanticReviewIncomplete:
		return "内容审核证据不完整，请缩短请求后重试"
	case ContentModerationActionSemanticReviewUnavailable:
		return "内容审核暂时不可用，请稍后重试"
	default:
		return "内容审核需要进一步复核，请稍后重试"
	}
}

func contentModerationCandidateSemanticInput(selection contentModerationCandidateSelection) string {
	return selection.Fragment
}

func contentModerationCandidateSemanticMetadata(selection contentModerationCandidateSelection, result ContentModerationSemanticReviewResult, policyOverride bool, ordinaryReason string) contentModerationMetadata {
	metadata := selection.metadata().mapValue()
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
	metadata["semantic_review_reason_details"] = result.ReasonDetails
	metadata["semantic_review_active_override"] = result.ActiveOverride
	metadata["semantic_review_presentation"] = result.Presentation
	metadata["semantic_review_targets"] = result.Targets
	metadata["semantic_review_instructions_revision"] = semanticReviewInstructionsRevision
	if selection.ReviewKind == contentModerationReviewKindPromptInjection {
		metadata["semantic_review_instructions_revision"] = promptInjectionReviewerInstructionsRevision
		metadata["semantic_review_schema_revision"] = promptInjectionReviewerSchemaRevision
	}
	metadata["semantic_review_policy_override"] = policyOverride
	if ordinaryReason != "" {
		metadata["ordinary_moderation_reason"] = ordinaryReason
	}
	return marshalContentModerationMetadata(metadata)
}

func (s *ContentModerationService) buildCandidateLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, selection contentModerationCandidateSelection, decisionSource, action string, flagged bool, category string, score float64, scores map[string]float64, latency *int, metadata contentModerationMetadata) *ContentModerationLog {
	log := s.buildLog(input, cfg, action, flagged, category, score, scores, selection.Fragment, latency, nil, metadata)
	log.DecisionSource = decisionSource
	log.ModerationProvider = cfg.Provider
	log.ModerationModel = cfg.Model
	log.SourceOrigin = selection.Origin
	log.SelectedSource = selection.Source.Source
	log.SelectedSourceRole = selection.Source.Role
	log.SelectedFragmentRunes = len([]rune(selection.Fragment))
	log.MatchedKeyword = selection.Rule.Keyword
	log.KeywordCategory = selection.Rule.Category
	log.KeywordSeverity = selection.Rule.Severity
	log.KeywordAction = normalizeContentModerationKeywordAction(selection.Rule.Action)
	log.EffectiveKeywordAction = normalizeContentModerationKeywordAction(selection.Rule.Action)
	log.RiskContextType = ContentModerationRiskContextActualRequest
	log.RiskContextReason = "candidate_selected_user_fragment"
	log.UserViolationEligible = selection.Origin == contentModerationSourceOriginUserTurn
	return log
}

// persistCandidateAudit is the only persistence path for candidate decisions.
// Candidate allow/review/reject results are all retained so an operator can
// compare the exact reviewer payload with the resulting verdict. Side effects
// are intentionally limited to an actual synchronous block.
func (s *ContentModerationService) persistCandidateAudit(
	ctx context.Context,
	input ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	selection contentModerationCandidateSelection,
	log *ContentModerationLog,
	blocked bool,
) string {
	if s == nil || log == nil {
		return ""
	}
	if blocked {
		s.enqueueRecord(ctx, input, cfg, log, selection.hash(), true, true)
	} else {
		s.persistContentModerationLog(ctx, cfg, log, selection.hash(), false, false)
	}
	payloadForHMAC := selection.Fragment
	if selection.ReviewKind == contentModerationReviewKindPromptInjection && strings.TrimSpace(selection.Source.Text) != "" {
		payloadForHMAC = selection.Source.Text
	}
	s.storeCandidateEvidence(ctx, log, selection, s.candidatePayloadHMAC(payloadForHMAC))
	return log.DecisionID
}

func (s *ContentModerationService) candidateUnavailableOutcome(
	ctx context.Context,
	input ContentModerationCheckInput,
	cfg *ContentModerationConfig,
	selection contentModerationCandidateSelection,
	decisionSource string,
	provider string,
	model string,
	reason string,
) contentModerationCandidateOutcome {
	reason = sanitizeSemanticReviewError(reason)
	if reason == "" {
		reason = "candidate_reviewer_unavailable"
	}
	metadata := selection.metadata().mapValue()
	metadata["availability_failure"] = reason
	metadata["reviewer_provider"] = provider
	metadata["reviewer_model"] = model
	log := s.buildCandidateLog(
		input,
		cfg,
		selection,
		decisionSource,
		ContentModerationActionError,
		false,
		"",
		0,
		nil,
		nil,
		marshalContentModerationMetadata(metadata),
	)
	failClosed := promptInjectionFailClosedActive(cfg, selection)
	if failClosed {
		log.Action = ContentModerationActionSemanticReviewUnavailable
		log.Flagged = true
		log.ReviewStatus = ContentModerationReviewStatusPending
		log.UserViolationEligible = false
	}
	log.ModerationProvider = strings.TrimSpace(provider)
	log.ModerationModel = strings.TrimSpace(model)
	log.Error = reason
	log.RiskContextReason = "candidate_reviewer_unavailable"
	decisionID := s.persistCandidateAudit(ctx, input, cfg, selection, log, false)
	if cfg != nil && cfg.Mode == ContentModerationModePreBlock {
		action := ContentModerationActionError
		if failClosed {
			action = ContentModerationActionSemanticReviewUnavailable
		}
		s.recordPreBlockSyncMetric(0, action)
	}
	if failClosed {
		if s.metrics != nil {
			s.metrics.observePromptInjectionReview("error", selection.EvidenceComplete, selection.EvidenceRunes)
			reasonLabel := "unavailable"
			if reason == "prompt_injection_reviewer_disabled" {
				reasonLabel = "disabled"
			}
			s.metrics.observePromptInjectionFailClosed(reasonLabel)
		}
		return contentModerationCandidateOutcome{
			Decision: &ContentModerationDecision{
				Allowed: false, Blocked: true, Flagged: true,
				Message:        promptInjectionDeferredMessage(ContentModerationActionSemanticReviewUnavailable),
				StatusCode:     http.StatusServiceUnavailable,
				Action:         ContentModerationActionSemanticReviewUnavailable,
				MatchedKeyword: selection.Rule.Keyword, KeywordCategory: selection.Rule.Category,
				KeywordSeverity: selection.Rule.Severity, RiskContextType: ContentModerationRiskContextActualRequest,
				RiskContextReason: "candidate_reviewer_unavailable",
			},
			DecisionID: decisionID, Cacheable: true, CacheTTL: s.candidateFailureCacheTTL(cfg),
		}
	}
	if selection.ReviewKind == contentModerationReviewKindPromptInjection && s.metrics != nil {
		s.metrics.observePromptInjectionReview("error", selection.EvidenceComplete, selection.EvidenceRunes)
	}
	return contentModerationCandidateOutcome{
		Decision:   contentModerationFailureDecision(cfg),
		DecisionID: decisionID,
		Cacheable:  true,
		CacheTTL:   s.candidateFailureCacheTTL(cfg),
	}
}

func contentModerationCandidateOrdinaryAllowRequiresSemantic(selection contentModerationCandidateSelection) bool {
	if normalizeContentModerationKeywordSeverity(selection.Rule.Severity) == ContentModerationKeywordSeverityCritical {
		return true
	}
	return selection.Kind == contentModerationCandidateKindPromptFilter &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(selection.Rule.Keyword)), "candidate_")
}

func (s *ContentModerationService) candidateFailureCacheTTL(cfg *ContentModerationConfig) time.Duration {
	ttl := s.candidateDecisionTTL(cfg)
	if ttl > contentModerationCandidateFailureCacheTTL {
		return contentModerationCandidateFailureCacheTTL
	}
	return ttl
}

func contentModerationCandidateExtractionIncomplete(selection contentModerationCandidateSelection) bool {
	reasons := normalizeContentModerationTruncateReasons(selection.Source.TruncateReasons)
	for _, reason := range reasons {
		if reason != "source_max_runes" {
			return true
		}
	}
	// A source_max_runes marker describes the bounded candidate view. The raw
	// extraction remains available to existing moderation paths, so Phase 0
	// records incomplete V2 evidence without changing the legacy decision.
	return selection.Source.Truncated && len(reasons) == 0
}

func (s *ContentModerationService) candidateExtractionFailureOutcome(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, selection *contentModerationCandidateSelection) contentModerationCandidateOutcome {
	reasons := append([]string(nil), content.TruncateReasons...)
	selectedSource := ""
	selectedRole := ""
	selectedFragment := ""
	if selection != nil {
		selectedSource = selection.Source.Source
		selectedRole = selection.Source.Role
		selectedFragment = selection.Fragment
		reasons = append([]string(nil), selection.Source.TruncateReasons...)
	}
	reasons = normalizeContentModerationTruncateReasons(reasons)
	metadata := map[string]any{
		"extraction_complete": false,
		"truncate_reasons":    reasons,
		"selected_source":     selectedSource,
		"selected_role":       selectedRole,
	}
	log := s.buildLog(
		input,
		cfg,
		ContentModerationActionError,
		false,
		"",
		0,
		nil,
		selectedFragment,
		nil,
		nil,
		marshalContentModerationMetadata(metadata),
	)
	log.DecisionSource = contentModerationDecisionSourceExtraction
	log.ModerationProvider = "local_extractor"
	log.Error = "candidate_extraction_incomplete"
	log.TruncateReasons = reasons
	log.SelectedSource = selectedSource
	log.SelectedSourceRole = selectedRole
	log.SelectedFragmentRunes = len([]rune(selectedFragment))
	log.RiskContextType = ContentModerationRiskContextUnknown
	log.RiskContextReason = "candidate_extraction_incomplete"
	failClosed := selection != nil && promptInjectionFailClosedActive(cfg, *selection)
	if failClosed {
		log.Action = ContentModerationActionSemanticReviewIncomplete
		log.Flagged = true
		log.ReviewStatus = ContentModerationReviewStatusPending
		log.UserViolationEligible = false
	}
	s.persistContentModerationLog(ctx, cfg, log, "", false, false)
	if selection != nil {
		payload := selection.Fragment
		if strings.TrimSpace(selection.Source.Text) != "" {
			payload = selection.Source.Text
		}
		s.storeCandidateEvidence(ctx, log, *selection, s.candidatePayloadHMAC(payload))
	}
	if cfg != nil && cfg.Mode == ContentModerationModePreBlock {
		action := ContentModerationActionError
		if failClosed {
			action = ContentModerationActionSemanticReviewIncomplete
		}
		s.recordPreBlockSyncMetric(0, action)
	}
	if failClosed {
		if s.metrics != nil {
			s.metrics.observePromptInjectionReview("error", false, selection.EvidenceRunes)
			s.metrics.observePromptInjectionFailClosed("incomplete")
		}
		return contentModerationCandidateOutcome{
			Decision: &ContentModerationDecision{
				Allowed: false, Blocked: true, Flagged: true,
				Message:        promptInjectionDeferredMessage(ContentModerationActionSemanticReviewIncomplete),
				StatusCode:     http.StatusServiceUnavailable,
				Action:         ContentModerationActionSemanticReviewIncomplete,
				MatchedKeyword: selection.Rule.Keyword, KeywordCategory: selection.Rule.Category,
				KeywordSeverity:   selection.Rule.Severity,
				RiskContextType:   ContentModerationRiskContextUnknown,
				RiskContextReason: "candidate_extraction_incomplete",
			},
			DecisionID: log.DecisionID, Cacheable: true, CacheTTL: s.candidateFailureCacheTTL(cfg),
		}
	}
	return contentModerationCandidateOutcome{
		Decision:   contentModerationFailureDecision(cfg),
		DecisionID: log.DecisionID,
		Cacheable:  true,
		CacheTTL:   s.candidateDecisionTTL(cfg),
	}
}
