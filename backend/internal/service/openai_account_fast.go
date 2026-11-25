package service

import (
	"fmt"
	"maps"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	openaiAccountForceFastKey  = "force_openai_fast"
	openaiAccountFreeFastKey   = "free_openai_fast"
	openaiBusinessFastUntilKey = "openai_business_fast_until"
)

var (
	openaiBusinessFastEnd        = time.Date(2027, time.January, 1, 8, 0, 0, 0, time.UTC) // End of December 31 Pacific time.
	openaiBusinessFastUntilValue = openaiBusinessFastEnd.Format(time.RFC3339)
)

func isOpenAIBusinessPlan(plan string) bool {
	// Codex uses "business" for Enterprise. ChatGPT Business uses these SKUs.
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "team", "self_serve_business_usage_based", "self_serve_business_prolite":
		return true
	default:
		return false
	}
}

// ApplyOpenAIBusinessFastImportDefaults applies the promotion to newly imported accounts.
// Explicit account overrides take precedence over the temporary defaults.
func (input *CreateAccountInput) ApplyOpenAIBusinessFastImportDefaults(at time.Time) {
	plan, _ := input.Credentials["plan_type"].(string)
	if input.Platform != PlatformOpenAI || input.Type != AccountTypeOAuth ||
		!isOpenAIBusinessPlan(plan) || !at.Before(openaiBusinessFastEnd) {
		return
	}
	input.Extra = maps.Clone(input.Extra)
	if input.Extra == nil {
		input.Extra = make(map[string]any, 1)
	}
	input.Extra[openaiBusinessFastUntilKey] = openaiBusinessFastUntilValue
}

func (a *Account) openAIFastEnabled(key string, at time.Time) bool {
	if a == nil || !a.IsOpenAI() {
		return false
	}
	if enabled, ok := a.Extra[key].(bool); ok {
		return enabled
	}
	if a.Extra[openaiBusinessFastUntilKey] != openaiBusinessFastUntilValue ||
		!a.IsOpenAIOAuth() || !isOpenAIBusinessPlan(a.GetCredential("plan_type")) {
		return false
	}
	if at.IsZero() {
		at = time.Now()
	}
	return at.Before(openaiBusinessFastEnd)
}

func validateOpenAIFastExtra(extra map[string]any) error {
	for _, key := range []string{openaiAccountForceFastKey, openaiAccountFreeFastKey} {
		if raw, exists := extra[key]; exists {
			if _, ok := raw.(bool); !ok {
				return infraerrors.BadRequest("OPENAI_FAST_INVALID", fmt.Sprintf("%s must be a boolean", key))
			}
		}
	}
	if until, exists := extra[openaiBusinessFastUntilKey]; exists && until != openaiBusinessFastUntilValue {
		return infraerrors.BadRequest("OPENAI_FAST_INVALID", "invalid OpenAI Business Fast promotion deadline")
	}
	return nil
}
