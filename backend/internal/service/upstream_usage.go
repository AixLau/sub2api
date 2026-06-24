package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const upstreamUsageBodyLimit int64 = 2 << 20

// QueryOpenAIAPIKeyUpstreamUsage queries a sub2api-compatible upstream usage endpoint.
func (s *AccountTestService) QueryOpenAIAPIKeyUpstreamUsage(ctx context.Context, account *Account) (map[string]any, error) {
	if s == nil {
		return nil, newUpstreamModelSyncConfigError("Account test service is not configured", nil)
	}
	if account == nil {
		return nil, newUpstreamModelSyncConfigError("Account is required", nil)
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError(
			fmt.Sprintf("Unsupported account type for upstream usage query: %s/%s", account.Platform, account.Type), nil,
		)
	}
	if s.httpUpstream == nil {
		return nil, newUpstreamModelSyncConfigError("Upstream HTTP client is not configured", nil)
	}

	apiKey := strings.TrimSpace(account.GetOpenAIApiKey())
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No OpenAI API key is available", nil)
	}
	baseURL := account.GetOpenAIBaseURL()
	if strings.TrimSpace(baseURL) == "" {
		return nil, newUpstreamModelSyncConfigError("OpenAI API-key base URL is required for upstream usage query", nil)
	}
	normalizedBaseURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI base URL", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildOpenAIUsageURL(normalizedBaseURL), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid OpenAI upstream usage URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.doUpstreamModelsRequest(req, upstreamModelsProxyURL(account), account)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to request upstream usage", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamUsageBodyLimit+1))
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to read upstream usage", err)
	}
	if int64(len(body)) > upstreamUsageBodyLimit {
		return nil, newUpstreamModelSyncUpstreamError("Upstream usage response is too large", fmt.Errorf("response exceeds %d bytes", upstreamUsageBodyLimit))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newUpstreamModelSyncUpstreamError(
			fmt.Sprintf("Upstream usage request failed with HTTP %d", resp.StatusCode),
			fmt.Errorf("upstream usage returned HTTP %d", resp.StatusCode),
		)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Upstream usage response was not valid JSON", err)
	}
	return result, nil
}

func buildOpenAIUsageURL(base string) string {
	normalized := strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.HasSuffix(normalized, "/v1/usage") {
		return normalized
	}
	if strings.HasSuffix(normalized, "/v1") {
		return normalized + "/usage"
	}
	return normalized + "/v1/usage"
}
