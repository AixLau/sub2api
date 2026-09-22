package service

import (
	"context"
	"net/http"
	"strings"
)

// OpenAIGatewayService implements the unscoped source directory for the OpenAI
// OAuth outbound transport capability. PluginManager wraps it with the binding's
// account allowlist before exposing it to a plugin, so this service itself can
// enumerate active OpenAI OAuth accounts without widening a plugin's scope.

// ListPluginAccounts returns the ids of active OpenAI OAuth-like accounts.
func (s *OpenAIGatewayService) ListPluginAccounts(ctx context.Context, platform, accountType string) ([]int64, error) {
	if s == nil || s.accountRepo == nil {
		return nil, nil
	}
	if p := strings.TrimSpace(platform); p != "" && p != PlatformOpenAI {
		return nil, nil
	}
	if at := strings.TrimSpace(accountType); at != "" && at != AccountTypeOAuth {
		// Setup-token style accounts are OAuth-like but not AccountTypeOAuth; the
		// current binding only routes AccountTypeOAuth, so honour that filter.
		return nil, nil
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		account := accounts[i]
		if account.Status == StatusActive && account.IsOpenAIOAuthLike() && !account.IsShadow() && account.Type == AccountTypeOAuth {
			ids = append(ids, account.ID)
		}
	}
	return ids, nil
}

// ResolvePluginOutboundIdentity resolves the access token plus the outbound
// identity headers and proxy the host would attach to a live request for the
// account. It returns (nil, nil) for out-of-scope accounts or when no token can
// be resolved.
func (s *OpenAIGatewayService) ResolvePluginOutboundIdentity(ctx context.Context, accountID int64) (*PluginOutboundIdentity, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, nil
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || account.Type != AccountTypeOAuth || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return nil, nil
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	headers := http.Header{}
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, headers, account); err != nil {
		return nil, err
	}
	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)
	applyOpenAIUpstreamIdentity(ctx, account, s.settingService, headers)
	return &PluginOutboundIdentity{
		AccountID:   account.ID,
		Platform:    account.Platform,
		AccountType: account.Type,
		ProxyURL:    resolveAccountProxyURL(account),
		Token:       token,
		Headers:     headers,
	}, nil
}
