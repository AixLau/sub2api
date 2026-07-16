package service

import (
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
)

const openAIMaxOutputTokensSupportKey = "openai_responses_max_output_tokens_supported"

func shouldStripOpenAIAPIKeyMaxOutputTokens(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return false
	}
	if account.Extra != nil {
		if supported, ok := account.Extra[openAIMaxOutputTokensSupportKey].(bool); ok && supported {
			return false
		}
		if supported, ok := account.Extra[openai_compat.ExtraKeyResponsesSupported].(bool); ok && supported {
			return false
		}
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" || isOfficialOpenAIBaseURL(baseURL) {
		return false
	}
	return true
}

func isOfficialOpenAIBaseURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), "api.openai.com")
}
