package service

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalizeSemanticReviewTaxonomy bounds model-provided labels to a small,
// stable vocabulary suitable for aggregation. Explanatory model output should
// remain in evidence/reason text rather than becoming a grouping key.
func normalizeSemanticReviewTaxonomy(result ContentModerationSemanticReviewResult) ContentModerationSemanticReviewResult {
	if len(result.ReasonDetails) == 0 {
		result.ReasonDetails = normalizeSemanticReviewReasonDetails(result.ReasonCodes)
	} else {
		result.ReasonDetails = normalizeSemanticReviewReasonDetails(result.ReasonDetails)
	}
	result.Categories = normalizeSemanticReviewTaxonomyValues(result.Categories, canonicalSemanticReviewCategory)
	result.ReasonCodes = normalizeSemanticReviewTaxonomyValues(result.ReasonCodes, canonicalSemanticReviewReasonCode)
	return result
}

func normalizeSemanticReviewReasonDetails(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, min(len(values), 8))
	for _, value := range values {
		value = trimRunes(redactContentModerationSecrets(strings.TrimSpace(value)), 64)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func normalizeSemanticReviewTaxonomyValues(values []string, normalize func(string) string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalize(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func canonicalSemanticReviewCategory(value string) string {
	key := canonicalSemanticReviewLabel(value)
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "benign") || strings.HasPrefix(key, "safe_") {
		return "benign_context"
	}
	if strings.Contains(key, "prompt_injection") ||
		strings.Contains(key, "policy_override") ||
		strings.Contains(key, "policy_evasion") ||
		strings.Contains(key, "policy_injection") {
		return "jailbreak"
	}
	switch key {
	case "benign", "benign_context", "safe", "educational", "authorized_testing", "authorized_test", "financial_research", "public_market_data", "public_data":
		return "benign_context"
	case "cyber", "cybersecurity", "hacking", "security":
		return "cyber"
	case "jailbreak", "safety_bypass", "policy_bypass", "prompt_injection":
		return "jailbreak"
	case "credential_theft", "credential_attack", "credential_harvesting", "password_theft", "secret_theft":
		return "credential_theft"
	case "unauthorized_access", "unauthorised_access", "access_control_bypass", "account_takeover", "intrusion":
		return "unauthorized_access"
	case "malware", "ransomware", "computer_virus", "trojan":
		return "malware"
	case "biosecurity", "virology", "biological_risk", "biological_virus", "pathogen", "bioweapon":
		return "biosecurity"
	case "weapons", "weapon", "weaponization":
		return "weapons"
	case "exploit_delivery", "exploitation", "vulnerability_exploitation", "exploit":
		return "exploit_delivery"
	case "evasion", "detection_evasion", "stealth":
		return "evasion"
	case "destructive_intrusion", "destruction", "data_destruction":
		return "destructive_intrusion"
	case "reverse_engineering", "reverse_engineer":
		return "reverse_engineering"
	case "license_cracking", "piracy", "license_bypass":
		return "license_cracking"
	case "privacy", "privacy_invasion", "doxxing", "personal_data":
		return "privacy"
	case "fraud", "deception", "deception_fraud", "competitive_abuse", "scam":
		return "fraud"
	case "market_manipulation", "market_abuse", "insider_trading":
		return "market_manipulation"
	case "sexual_exploitation", "sexual_content":
		return "sexual_exploitation"
	case "child_safety", "child_exploitation", "csam":
		return "child_safety"
	case "self_harm", "suicide":
		return "self_harm"
	case "violence", "physical_harm", "violent":
		return "violence"
	case "hate", "hateful":
		return "hate"
	case "other", "other_risk", "unknown":
		return "other"
	default:
		return "other"
	}
}

func canonicalSemanticReviewReasonCode(value string) string {
	key := canonicalSemanticReviewLabel(value)
	if key == "" {
		return ""
	}
	// Preserve application-generated codes through taxonomy normalization.
	switch key {
	case "final_inconclusive", "platform_license_circumvention", "platform_virology":
		return key
	case "semantic_policy_public_harmless", "semantic_policy_reject", "semantic_policy_reject_inconsistent", "semantic_policy_allow_inconsistent", "semantic_policy_harmless_review", "semantic_policy_unsubstantiated_fraud", "semantic_policy_context_only", "unstructured_model_output", "invalid_model_output_json":
		return key
	case "no_authorization", "unauthorized_access", "unauthorised_access", "without_permission":
		return "unauthorized_access"
	case "credential_theft", "credential_attack", "credential_harvesting":
		return "credential_theft"
	case "malware", "ransomware":
		return "malware"
	case "exploit", "exploit_delivery", "vulnerability_exploitation":
		return "exploit_delivery"
	case "evasion", "detection_evasion":
		return "evasion"
	case "deception", "deception_fraud", "fraud", "competitive_abuse":
		return "fraud"
	case "market_manipulation", "market_abuse":
		return "market_manipulation"
	case "privacy", "privacy_invasion", "doxxing":
		return "privacy"
	case "physical_harm", "violence", "violent":
		return "violence"
	case "sexual_exploitation":
		return "sexual_exploitation"
	case "self_harm", "suicide":
		return "self_harm"
	case "jailbreak", "safety_bypass", "prompt_injection":
		return "jailbreak"
	case "hate", "hateful":
		return "hate"
	case "benign_context", "benign", "safe", "public_market_data", "authorized_testing", "no_harmful_content", "no_actionable_request", "no_user_request", "no_user_request_provided", "harmless_context":
		return "benign_context"
	case "ambiguous_context", "ambiguous", "unclear", "insufficient_context":
		return "ambiguous_context"
	default:
		return "model_reason_other"
	}
}

func canonicalSemanticReviewLabel(value string) string {
	value = strings.ToLower(norm.NFKC.String(strings.TrimSpace(value)))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}
