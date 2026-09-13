package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

var errContentModerationModelsEmpty = errors.New("未获取到任何可用模型")

func fetchContentModerationModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API Key 无效或未填写")
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("接口地址错误")
	}
	if strings.Trim(u.Path, "/") == "" {
		u.Path = "/v1"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("接口地址错误: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	setConfiguredCodexIdentityHeaders(req, DefaultOpenAICodexUserAgent)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网络请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("API Key 无效")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("API Key 权限不足")
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("服务商接口异常（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("获取模型列表失败（HTTP %d）", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("服务商返回格式无效")
	}
	seen := map[string]struct{}{}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				models = append(models, id)
			}
		}
	}
	if len(models) == 0 {
		return nil, errContentModerationModelsEmpty
	}
	return models, nil
}

func (r *openAIContentModerationSemanticReviewRouter) reviewWithConfiguredAPI(ctx context.Context, cfg ContentModerationSemanticReviewConfig, input ContentModerationSemanticReviewInput) (ContentModerationSemanticReviewResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	reviewCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	models := append([]string{cfg.PrimaryModel}, cfg.FallbackModels...)
	seen := map[string]bool{}
	var last error
	primaryFailure := ""
	attemptCount := 0
	maxAttempts := cfg.MaxAttemptsPerModel
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	userAgent := DefaultOpenAICodexUserAgent
	if r != nil && r.settingService != nil {
		if configured := strings.TrimSpace(r.settingService.GetOpenAICodexUserAgent(reviewCtx)); configured != "" {
			userAgent = configured
		}
	}
	for modelIndex, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[strings.ToLower(model)] {
			continue
		}
		seen[strings.ToLower(model)] = true
		timeoutMS := cfg.PrimaryTimeoutMS
		if modelIndex > 0 {
			timeoutMS = cfg.FallbackTimeoutMS
		}
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if err := reviewCtx.Err(); err != nil {
				last = err
				break
			}
			attemptCount++
			result, err := callConfiguredSemanticModel(reviewCtx, cfg, model, input, timeoutMS, userAgent, r.internalToken)
			if err == nil {
				result.Model = model
				result.AttemptCount = attemptCount
				if modelIndex > 0 {
					result.FallbackFrom = cfg.PrimaryModel
					result.FallbackReason = primaryFailure
				}
				slog.Info("content_moderation.semantic_review_configured_success", "model", model, "fallback", modelIndex > 0, "attempt", attempt)
				return normalizeSemanticReviewResult(result), nil
			}
			last = err
			if modelIndex == 0 && primaryFailure == "" {
				primaryFailure = sanitizeSemanticReviewError(err.Error())
			}
			slog.Warn("content_moderation.semantic_review_configured_attempt_failed", "model", model, "attempt", attempt, "error", sanitizeSemanticReviewError(err.Error()))
			if attempt < maxAttempts {
				backoff := time.Duration(attempt) * 100 * time.Millisecond
				timer := time.NewTimer(backoff)
				select {
				case <-reviewCtx.Done():
					timer.Stop()
					break
				case <-timer.C:
				}
				if reviewCtx.Err() != nil {
					break
				}
			}
		}
	}
	if last == nil {
		last = errors.New("未配置主内容审计模型")
	}
	return ContentModerationSemanticReviewResult{}, &ContentModerationSemanticReviewUnavailableError{Err: last}
}

func callConfiguredSemanticModel(ctx context.Context, cfg ContentModerationSemanticReviewConfig, model string, input ContentModerationSemanticReviewInput, timeoutMS int, userAgent, internalToken string) (ContentModerationSemanticReviewResult, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if parsed, parseErr := url.Parse(base); parseErr == nil && strings.Trim(parsed.Path, "/") == "" {
		base += "/v1"
	}
	endpoint := "/chat/completions"
	if normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint) == "responses" {
		endpoint = "/responses"
	}
	u, err := url.Parse(base + endpoint)
	if err != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("接口地址错误")
	}
	body := map[string]any{"model": model, "temperature": 0, "max_tokens": cfg.MaxOutputTokens, "messages": []map[string]string{{"role": "system", "content": semanticReviewInstructionsForKind(input.ReviewKind, input.FinalReview)}, {"role": "user", "content": input.Text}}}
	if normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint) == "responses" {
		body = map[string]any{
			"model":             model,
			"instructions":      semanticReviewInstructionsForKind(input.ReviewKind, input.FinalReview),
			"input":             input.Text,
			"max_output_tokens": cfg.MaxOutputTokens,
			"reasoning":         map[string]any{"effort": cfg.ReasoningEffort},
			"text":              map[string]any{"format": semanticReviewJSONSchemaForKind(input.ReviewKind, input.FinalReview)},
			"store":             false,
		}
	}
	raw, _ := json.Marshal(body)
	if timeoutMS <= 0 {
		timeoutMS = cfg.TimeoutMS
	}
	client := &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("接口地址错误")
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	setConfiguredCodexIdentityHeaders(req, userAgent)
	if strings.TrimSpace(internalToken) != "" {
		req.Header.Set(contentModerationInternalRequestHeader, internalToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ContentModerationSemanticReviewResult{}, errors.New("主模型请求超时")
		}
		return ContentModerationSemanticReviewResult{}, errors.New("网络请求失败")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode == 401 {
		return ContentModerationSemanticReviewResult{}, errors.New("API Key 无效")
	}
	if resp.StatusCode == 403 {
		return ContentModerationSemanticReviewResult{}, errors.New("API Key 权限不足")
	}
	if resp.StatusCode == 429 {
		return ContentModerationSemanticReviewResult{}, errors.New("主模型触发限流")
	}
	if resp.StatusCode >= 500 {
		return ContentModerationSemanticReviewResult{}, fmt.Errorf("模型服务暂时不可用（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ContentModerationSemanticReviewResult{}, fmt.Errorf("模型调用失败（HTTP %d）", resp.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	content := ""
	if normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint) == "responses" {
		parsed, parseErr := parseSemanticReviewResponse(data, resp.Header.Get("Content-Type"))
		if parseErr == nil {
			content = parsed.Text
		}
	} else {
		if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Choices) > 0 {
			content = envelope.Choices[0].Message.Content
		}
	}
	if strings.TrimSpace(content) == "" {
		return ContentModerationSemanticReviewResult{}, errors.New("服务商返回格式无效")
	}
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var result ContentModerationSemanticReviewResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("模型返回内容无法解析")
	}
	return result, nil
}

// setConfiguredCodexIdentityHeaders makes custom moderation requests look like
// the supported Codex app-server client to upstream gateways that enforce
// codex_cli_only. The x-codex-* headers satisfy the engine fingerprint gate;
// originator/version satisfy the app-server identity gate.
func setConfiguredCodexIdentityHeaders(req *http.Request, userAgent string) {
	if req == nil {
		return
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = DefaultOpenAICodexUserAgent
	}
	originator, pairedUA, ok := openai.PairCodexClientIdentity(userAgent)
	if !ok {
		originator, pairedUA = openai.CodexDefaultOriginator, DefaultOpenAICodexUserAgent
	}
	req.Header.Set("User-Agent", pairedUA)
	req.Header.Set("originator", originator)
	req.Header.Set("version", CodexCanonicalClientVersion())
	req.Header.Set("x-codex-installation-id", uuid.NewString())
	req.Header.Set("x-codex-window-id", uuid.NewString())
}
