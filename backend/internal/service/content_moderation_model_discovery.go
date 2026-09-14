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
	"os"
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
			started := time.Now()
			slog.Info("content_moderation.semantic_review_configured_start",
				"model", model, "attempt", attempt, "attempt_count", attemptCount,
				"endpoint", "/v1/"+normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint),
				"reasoning_effort", cfg.ReasoningEffort, "max_output_tokens", cfg.MaxOutputTokens,
				"input_runes", len([]rune(input.Text)), "timeout_ms", timeoutMS)
			result, err := callConfiguredSemanticModel(reviewCtx, cfg, model, input, timeoutMS, userAgent, r.internalToken)
			if err == nil {
				result.Model = model
				result.UpstreamEndpoint = "/v1/" + normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint)
				result.AttemptCount = attemptCount
				if modelIndex > 0 {
					result.FallbackFrom = cfg.PrimaryModel
					result.FallbackReason = primaryFailure
				}
				r.recordConfiguredUsage(ctx, input, result, int(time.Since(started).Milliseconds()))
				slog.Info("content_moderation.semantic_review_configured_success", "model", model, "fallback", modelIndex > 0, "attempt", attempt,
					"total_ms", time.Since(started).Milliseconds())
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

func (r *openAIContentModerationSemanticReviewRouter) recordConfiguredUsage(ctx context.Context, input ContentModerationSemanticReviewInput, result ContentModerationSemanticReviewResult, durationMS int) {
	if r == nil || r.usageRecorder == nil {
		return
	}
	inbound := "/internal/content-moderation/semantic-review"
	upstream := "/v1/" + normalizeContentModerationSemanticReviewEndpoint(result.UpstreamEndpoint)
	if result.UpstreamEndpoint == "" {
		upstream = "/v1/responses"
	}
	if err := r.usageRecorder.Record(ctx, PlatformUsageRecord{
		Source:           UsageSourceContentModeration,
		RequestID:        semanticReviewUsageRecordID(input),
		Model:            result.Model,
		RequestedModel:   result.Model,
		UpstreamModel:    result.UpstreamModel,
		GroupID:          cloneInt64Ptr(input.GroupID),
		Usage:            result.Usage,
		RequestType:      RequestTypeSync,
		DurationMS:       &durationMS,
		FirstTokenMS:     result.FirstTokenMS,
		UserAgent:        platformUsageStringPtr(result.UserAgent),
		InboundEndpoint:  &inbound,
		UpstreamEndpoint: &upstream,
	}); err != nil {
		slog.Warn("content_moderation.semantic_review_usage_record_failed", "model", result.Model, "error", sanitizeSemanticReviewError(err.Error()))
	}
}

func callConfiguredSemanticModel(ctx context.Context, cfg ContentModerationSemanticReviewConfig, model string, input ContentModerationSemanticReviewInput, timeoutMS int, userAgent, internalToken string) (ContentModerationSemanticReviewResult, error) {
	started := time.Now()
	base := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if parsed, parseErr := url.Parse(base); parseErr == nil && strings.Trim(parsed.Path, "/") == "" {
		base += "/v1"
	}
	endpoint := "/chat/completions"
	protocol := normalizeContentModerationSemanticReviewEndpoint(cfg.APIEndpoint)
	if protocol == "responses" {
		endpoint = "/responses"
	} else if protocol == "messages" {
		endpoint = "/messages"
	}
	u, err := url.Parse(base + endpoint)
	if err != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("接口地址错误")
	}
	compactInstructions := semanticReviewInstructionsForKind(input.ReviewKind, input.FinalReview) + "\nReturn exactly one minified JSON object with only the required fields. Do not include explanations, markdown, whitespace padding, or extra fields."
	body := map[string]any{"model": model, "temperature": 0, "max_tokens": cfg.MaxOutputTokens, "response_format": map[string]any{"type": "json_object"}, "messages": []map[string]string{{"role": "system", "content": compactInstructions}, {"role": "user", "content": input.Text}}}
	if protocol == "responses" {
		body = map[string]any{
			"model":             model,
			"instructions":      compactInstructions,
			"input":             input.Text,
			"max_output_tokens": cfg.MaxOutputTokens,
			"reasoning":         map[string]any{"effort": cfg.ReasoningEffort},
			"store":             false,
		}
	} else if protocol == "messages" {
		body = map[string]any{
			"model":      model,
			"system":     compactInstructions,
			"messages":   []map[string]any{{"role": "user", "content": input.Text}},
			"max_tokens": cfg.MaxOutputTokens,
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
	if protocol == "messages" {
		req.Header.Set("x-api-key", cfg.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
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
	headersAt := time.Now()
	slog.Info("content_moderation.semantic_review_configured_headers",
		"model", model, "status", resp.StatusCode,
		"time_to_headers_ms", headersAt.Sub(started).Milliseconds())
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("读取模型响应失败，请检查网络连接或请求超时设置")
	}
	bodyReadAt := time.Now()
	logConfiguredSemanticResponseDebug(model, resp.StatusCode, data)
	slog.Info("content_moderation.semantic_review_configured_body_read",
		"model", model, "response_bytes", len(data),
		"response_read_ms", bodyReadAt.Sub(headersAt).Milliseconds(),
		"http_total_ms", bodyReadAt.Sub(started).Milliseconds())
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
	if err := configuredSemanticResponseCompletionError(data, cfg.MaxOutputTokens); err != nil {
		return ContentModerationSemanticReviewResult{}, err
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage OpenAIUsage `json:"usage"`
	}
	content := ""
	var usage OpenAIUsage
	reasoningSummary := ""
	if protocol == "responses" {
		parsed, parseErr := parseSemanticReviewResponse(data, resp.Header.Get("Content-Type"))
		if parseErr == nil {
			content = parsed.Text
			usage = parsed.Usage
			reasoningSummary = parsed.ReasoningSummary
		} else {
			// A few OpenAI-compatible providers ignore the selected Responses
			// endpoint and return a Chat Completions envelope. Accept that shape
			// here while keeping the same strict JSON result validation below.
			var compatible struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
				Usage OpenAIUsage `json:"usage"`
			}
			if json.Unmarshal(data, &compatible) == nil && len(compatible.Choices) > 0 {
				content = compatible.Choices[0].Message.Content
				usage = compatible.Usage
			}
		}
	} else if protocol == "messages" {
		var envelope struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Usage OpenAIUsage `json:"usage"`
		}
		if err := json.Unmarshal(data, &envelope); err == nil {
			for _, block := range envelope.Content {
				if block.Type == "text" {
					content = block.Text
					break
				}
			}
			usage = envelope.Usage
		}
	} else {
		if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Choices) > 0 {
			content = envelope.Choices[0].Message.Content
			usage = envelope.Usage
		}
	}
	if strings.TrimSpace(content) == "" {
		return ContentModerationSemanticReviewResult{}, errors.New("模型未返回最终审核文本，请检查输出上限、思考强度及接口端点")
	}
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	result, err := parseConfiguredSemanticReviewContent(content)
	if err != nil {
		return ContentModerationSemanticReviewResult{}, errors.New("模型返回内容无法解析")
	}
	parsedAt := time.Now()
	slog.Info("content_moderation.semantic_review_configured_parsed",
		"model", model, "parse_ms", parsedAt.Sub(bodyReadAt).Milliseconds(),
		"total_ms", parsedAt.Sub(started).Milliseconds())
	result.Usage = usage
	result.ReasoningSummary = reasoningSummary
	return result, nil
}

func configuredSemanticResponseCompletionError(data []byte, maxOutputTokens int) error {
	var envelope struct {
		Status            string `json:"status"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return nil // Non-JSON envelopes are handled by the stream parser.
	}
	outputLimited := envelope.IncompleteDetails.Reason == "max_output_tokens"
	for _, choice := range envelope.Choices {
		outputLimited = outputLimited || choice.FinishReason == "length"
	}
	if outputLimited {
		return fmt.Errorf("模型输出达到上限（%d tokens），未完成审核结果；请降低思考强度或提高输出上限", maxOutputTokens)
	}
	switch envelope.Status {
	case "incomplete", "failed", "cancelled", "canceled", "in_progress", "queued":
		return errors.New("模型响应未完成，未获得最终审核结果")
	}
	return nil
}

func logConfiguredSemanticResponseDebug(model string, status int, body []byte) {
	if strings.TrimSpace(os.Getenv("CONTENT_MODERATION_DEBUG_RESPONSE")) != "1" {
		return
	}
	slog.Warn("content_moderation.semantic_review_configured_response_debug",
		"model", model, "status", status, "response_bytes", len(body), "response", string(body))
}

// parseConfiguredSemanticReviewContent accepts the compact review object as
// well as the common Responses-compatible aliases emitted by providers that
// do not enforce a response schema (decision/category/reason_code).
func parseConfiguredSemanticReviewContent(content string) (ContentModerationSemanticReviewResult, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		// Some compatible endpoints prepend a short explanation despite the
		// output contract. Recover only a complete JSON object and continue to
		// reject the response if no object can be decoded.
		start, end := strings.IndexByte(content, '{'), strings.LastIndexByte(content, '}')
		if start < 0 || end <= start || json.Unmarshal([]byte(content[start:end+1]), &raw) != nil {
			return ContentModerationSemanticReviewResult{}, err
		}
	}
	if _, ok := raw["verdict"]; !ok {
		for _, alias := range []string{"decision", "judgment", "judgement", "classification", "action"} {
			if value, exists := raw[alias]; exists {
				raw["verdict"] = value
				break
			}
		}
	}
	if _, ok := raw["verdict"]; !ok {
		var rejected, allowed bool
		if value, exists := raw["reject"]; exists {
			_ = json.Unmarshal(value, &rejected)
		}
		if value, exists := raw["allow"]; exists {
			_ = json.Unmarshal(value, &allowed)
		}
		switch {
		case rejected:
			raw["verdict"] = json.RawMessage(`"reject"`)
		case allowed:
			raw["verdict"] = json.RawMessage(`"allow"`)
		}
	}
	if _, ok := raw["categories"]; !ok {
		if category, ok := raw["category"]; ok {
			var value string
			if json.Unmarshal(category, &value) == nil && strings.TrimSpace(value) != "" {
				raw["categories"], _ = json.Marshal([]string{value})
			}
		}
	}
	if _, ok := raw["harm_mechanism"]; !ok {
		if mechanism, exists := raw["mechanism"]; exists {
			raw["harm_mechanism"] = mechanism
		}
	}
	if _, ok := raw["reason_codes"]; !ok {
		reasonCode, ok := raw["reason_code"]
		if !ok {
			reasonCode, ok = raw["reason"]
		}
		if ok {
			var value string
			if json.Unmarshal(reasonCode, &value) == nil && strings.TrimSpace(value) != "" {
				raw["reason_codes"], _ = json.Marshal([]string{value})
			}
		}
	}
	normalized, err := json.Marshal(raw)
	if err != nil {
		return ContentModerationSemanticReviewResult{}, err
	}
	var result ContentModerationSemanticReviewResult
	if err := json.Unmarshal(normalized, &result); err != nil || strings.TrimSpace(result.Verdict) == "" {
		if err == nil {
			err = errors.New("missing verdict")
		}
		return ContentModerationSemanticReviewResult{}, err
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
