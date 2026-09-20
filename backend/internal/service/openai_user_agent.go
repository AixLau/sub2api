package service

import (
	"context"
	"net/http"
	"strings"
)

// resolveOpenAICodexCanonicalUserAgent resolves the single global OpenAI identity.
// Account and inbound client identities never override the system setting.
func resolveOpenAICodexCanonicalUserAgent(ctx context.Context, settingService *SettingService) string {
	var canonicalUA string
	if settingService != nil {
		canonicalUA = settingService.GetOpenAICodexCanonicalUserAgent(ctx)
	} else {
		canonicalUA = codexCanonicalUserAgent()
	}
	return resolveCodexOutboundIdentityWithCanonicalUA("", canonicalUA).userAgent
}

// applyOpenAIUpstreamIdentity runs after account header overrides and before body
// identity projection. Other providers using the OpenAI protocol keep their identity.
func applyOpenAIUpstreamIdentity(ctx context.Context, account *Account, settings *SettingService, h http.Header) {
	if account == nil || !account.IsOpenAI() || h == nil {
		return
	}
	identity := resolveCodexOutboundIdentityWithCanonicalUA("", resolveOpenAICodexCanonicalUserAgent(ctx, settings))
	for name := range h {
		if strings.EqualFold(name, "User-Agent") {
			delete(h, name)
		}
	}
	h.Set("User-Agent", identity.userAgent)
	if h.Get("originator") != "" {
		h.Set("originator", identity.originator)
		h.Set("version", identity.version)
	}
}
