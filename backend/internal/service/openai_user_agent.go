package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// resolveOpenAICodexUpstreamUserAgent returns the User-Agent used for Codex
// upstream requests. Account-level configuration has the highest priority,
// followed by the global system setting and the built-in default.
func resolveOpenAICodexUpstreamUserAgent(ctx context.Context, account *Account, settingService *SettingService) string {
	if account != nil {
		if customUA := strings.TrimSpace(account.GetOpenAIUserAgent()); customUA != "" {
			return customUA
		}
	}
	if settingService != nil {
		if systemUA := strings.TrimSpace(settingService.GetOpenAICodexUserAgent(ctx)); systemUA != "" {
			return systemUA
		}
	}
	return DefaultOpenAICodexUserAgent
}

// resolveOpenAICodexCanonicalUserAgent returns the fully normalized system
// identity, including the currently effective client version.
func resolveOpenAICodexCanonicalUserAgent(ctx context.Context, settingService *SettingService) string {
	if settingService != nil {
		if canonicalUA := strings.TrimSpace(settingService.GetOpenAICodexCanonicalUserAgent(ctx)); canonicalUA != "" {
			return canonicalUA
		}
	}
	return codexCLIUserAgent
}

// Messages bridge requests preserve a valid official client identity when no
// account or system override is configured. Explicit local configuration keeps
// priority over the inbound client identity.
func resolveOpenAIMessagesBridgeUserAgent(ctx context.Context, account *Account, settingService *SettingService, clientUserAgent string) string {
	if account != nil {
		if customUA := strings.TrimSpace(account.GetOpenAIUserAgent()); customUA != "" {
			return customUA
		}
	}
	if settingService != nil {
		if systemUA := strings.TrimSpace(settingService.GetOpenAICodexUserAgent(ctx)); systemUA != "" {
			return systemUA
		}
	}
	if _, pairedUA, ok := openai.PairCodexClientIdentity(clientUserAgent); ok {
		return pairedUA
	}
	return DefaultOpenAICodexUserAgent
}
