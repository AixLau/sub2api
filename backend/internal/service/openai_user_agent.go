package service

import (
	"context"
	"strings"
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
