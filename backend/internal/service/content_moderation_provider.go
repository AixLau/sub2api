package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
)

type ModerationLevel string

const (
	ModerationLevelPass   ModerationLevel = "PASS"
	ModerationLevelReview ModerationLevel = "REVIEW"
	ModerationLevelReject ModerationLevel = "REJECT"
)

type ProviderModerationResult struct {
	Level          ModerationLevel
	RiskTypes      []string
	CategoryScores map[string]float64
}

type ModerationProvider interface {
	Name() string
	AdapterVersion() string
	ModerateText(ctx context.Context, model, apiKey, text string) (ProviderModerationResult, error)
}

type ModerationProviderErrorKind string

const (
	ModerationProviderErrorAuth      ModerationProviderErrorKind = "auth"
	ModerationProviderErrorRateLimit ModerationProviderErrorKind = "rate_limit"
	ModerationProviderErrorTimeout   ModerationProviderErrorKind = "timeout"
	ModerationProviderErrorHTTP      ModerationProviderErrorKind = "http"
	ModerationProviderErrorSchema    ModerationProviderErrorKind = "schema"
	ModerationProviderErrorTransport ModerationProviderErrorKind = "transport"
)

type ModerationProviderError struct {
	Kind       ModerationProviderErrorKind
	Provider   string
	HTTPStatus int
	Err        error
}

func (e *ModerationProviderError) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("%s moderation %s error (status %d): %v", e.Provider, e.Kind, e.HTTPStatus, e.Err)
	}
	return fmt.Sprintf("%s moderation %s error: %v", e.Provider, e.Kind, e.Err)
}
func (e *ModerationProviderError) Unwrap() error { return e.Err }

func IsModerationProviderError(err error, kind ModerationProviderErrorKind) bool {
	var target *ModerationProviderError
	return errors.As(err, &target) && target.Kind == kind
}

type moderationProviderAdapter struct {
	name, version, endpoint string
	client                  *http.Client
	thresholds              map[string]float64
}

func NewOpenAIModerationProvider(baseURL string, thresholds map[string]float64, client *http.Client) (ModerationProvider, error) {
	endpoint, err := moderationProviderEndpoint(baseURL, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("moderation HTTP client is required")
	}
	known := ContentModerationDefaultThresholds()
	for category, threshold := range thresholds {
		if _, ok := known[category]; !ok || threshold < 0 || threshold > 1 {
			return nil, fmt.Errorf("invalid OpenAI moderation threshold %q", category)
		}
		known[category] = threshold
	}
	return &moderationProviderAdapter{name: "openai", version: "openai-v1", endpoint: endpoint, client: client, thresholds: known}, nil
}

func NewZhipuModerationProvider(baseURL string, client *http.Client) (ModerationProvider, error) {
	endpoint, err := moderationProviderEndpoint(baseURL, "/api/paas/v4/moderations")
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("moderation HTTP client is required")
	}
	return &moderationProviderAdapter{name: "zhipu", version: "zhipu-v1", endpoint: endpoint, client: client}, nil
}

func moderationProviderEndpoint(baseURL, fixedPath string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", errors.New("invalid moderation provider base URL")
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = fixedPath, "", "", ""
	return u.String(), nil
}

func (p *moderationProviderAdapter) Name() string           { return p.name }
func (p *moderationProviderAdapter) AdapterVersion() string { return p.version }

func (p *moderationProviderAdapter) ModerateText(ctx context.Context, model, apiKey, text string) (ProviderModerationResult, error) {
	raw, err := json.Marshal(struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{model, text})
	if err != nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(raw))
	if err != nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorTransport, 0, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		kind := ModerationProviderErrorTransport
		if errors.Is(err, context.DeadlineExceeded) {
			kind = ModerationProviderErrorTimeout
		}
		return ProviderModerationResult{}, p.providerError(kind, 0, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := ModerationProviderErrorHTTP
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			kind = ModerationProviderErrorAuth
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			kind = ModerationProviderErrorRateLimit
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return ProviderModerationResult{}, p.providerError(kind, resp.StatusCode, errors.New("provider rejected request"))
	}
	if p.name == "openai" {
		return p.decodeOpenAI(resp.Body)
	}
	return p.decodeZhipu(resp.Body)
}

func (p *moderationProviderAdapter) providerError(kind ModerationProviderErrorKind, status int, err error) error {
	return newModerationProviderError(p.name, kind, status, err)
}

func newModerationProviderError(provider string, kind ModerationProviderErrorKind, status int, err error) error {
	return &ModerationProviderError{Kind: kind, Provider: provider, HTTPStatus: status, Err: err}
}

func (p *moderationProviderAdapter) decodeOpenAI(body io.Reader) (ProviderModerationResult, error) {
	type result struct {
		Flagged        *bool               `json:"flagged"`
		CategoryScores *map[string]float64 `json:"category_scores"`
	}
	var response struct {
		Results []result `json:"results"`
	}
	if err := decodeModerationJSON(body, &response); err != nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, err)
	}
	if len(response.Results) != 1 || response.Results[0].Flagged == nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, errors.New("OpenAI moderation requires exactly one result with flagged state"))
	}
	r := response.Results[0]
	if *r.Flagged {
		scores := map[string]float64{}
		if r.CategoryScores != nil {
			scores = *r.CategoryScores
		}
		return ProviderModerationResult{Level: ModerationLevelReject, CategoryScores: scores, RiskTypes: []string{}}, nil
	}
	if r.CategoryScores == nil || len(*r.CategoryScores) == 0 {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, errors.New("OpenAI unflagged moderation requires scores"))
	}
	level := ModerationLevelPass
	for category, score := range *r.CategoryScores {
		threshold, ok := p.thresholds[category]
		if !ok || score < 0 || score > 1 {
			return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, fmt.Errorf("unknown or invalid category %q", category))
		}
		if score >= threshold {
			level = ModerationLevelReject
		}
	}
	return ProviderModerationResult{Level: level, CategoryScores: *r.CategoryScores, RiskTypes: []string{}}, nil
}

func (p *moderationProviderAdapter) decodeZhipu(body io.Reader) (ProviderModerationResult, error) {
	type result struct {
		ContentType string             `json:"content_type"`
		RiskLevel   *string            `json:"risk_level"`
		RiskType    *[]json.RawMessage `json:"risk_type"`
		RiskTypes   *[]json.RawMessage `json:"risk_types"`
	}
	var response struct {
		ID         string   `json:"id"`
		Created    int64    `json:"created"`
		RequestID  string   `json:"request_id"`
		ResultList []result `json:"result_list"`
		Results    []result `json:"results"`
		Usage      struct {
			ModerationText struct {
				CallCount float64 `json:"call_count"`
			} `json:"moderation_text"`
		} `json:"usage"`
	}
	if err := decodeModerationJSON(body, &response); err != nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, err)
	}
	results := response.ResultList
	if len(results) == 0 {
		results = response.Results
	}
	if len(results) != 1 {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, errors.New("Zhipu moderation requires exactly one result"))
	}
	r := results[0]
	riskTypes := r.RiskType
	if riskTypes == nil {
		riskTypes = r.RiskTypes
	}
	if r.RiskLevel == nil || riskTypes == nil {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, errors.New("missing Zhipu moderation fields"))
	}
	level := ModerationLevel(*r.RiskLevel)
	if level != ModerationLevelPass && level != ModerationLevelReview && level != ModerationLevelReject {
		return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, errors.New("unknown Zhipu risk level"))
	}
	values := make([]string, 0, len(*riskTypes))
	for _, raw := range *riskTypes {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return ProviderModerationResult{}, p.providerError(ModerationProviderErrorSchema, 0, err)
		}
		values = append(values, value)
	}
	risks, err := normalizeZhipuRiskTypes(values)
	if err != nil {
		return ProviderModerationResult{}, err
	}
	return ProviderModerationResult{Level: level, RiskTypes: risks, CategoryScores: map[string]float64{}}, nil
}

func normalizeZhipuRiskTypes(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
			return nil, newModerationProviderError("zhipu", ModerationProviderErrorSchema, 0, errors.New("invalid Zhipu risk type"))
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) > 32 {
			return nil, newModerationProviderError("zhipu", ModerationProviderErrorSchema, 0, errors.New("too many Zhipu risk types"))
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i], out[j]) < 0 })
	return out, nil
}

func decodeModerationJSON(body io.Reader, out any) error {
	raw, err := io.ReadAll(io.LimitReader(body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return errors.New("moderation response exceeds size limit")
	}
	if !utf8.Valid(raw) {
		return errors.New("moderation response is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	return requireJSONEOF(dec)
}

func requireJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
