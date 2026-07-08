package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/moderationcoverage"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"golang.org/x/text/unicode/norm"
)

const (
	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"

	contentModerationAPIKeysModeAppend  = "append"
	contentModerationAPIKeysModeReplace = "replace"

	ContentModerationActionAllow                     = "allow"
	ContentModerationActionBlock                     = "block"
	ContentModerationActionHashBlock                 = "hash_block"
	ContentModerationActionKeywordBlock              = "keyword_block"
	ContentModerationActionKeywordReview             = "keyword_review"
	ContentModerationActionError                     = "error"
	ContentModerationActionCyberPolicy               = "cyber_policy" // cyber_policy 硬阻断的风控日志 action（封号计数排除按此值过滤）
	ContentModerationActionCyberPolicySessionBlocked = "cyber_policy_session_blocked"

	contentModerationKeywordCategory = "keyword"

	ContentModerationKeywordModeKeywordOnly   = "keyword_only"
	ContentModerationKeywordModeKeywordAndAPI = "keyword_and_api"
	ContentModerationKeywordModeAPIOnly       = "api_only"

	ContentModerationEngineModeRuleOnly = "rule_only"
	ContentModerationEngineModeAPIOnly  = "api_only"
	ContentModerationEngineModeHybrid   = "hybrid"

	ContentModerationFailStrategyOpen   = "open"
	ContentModerationFailStrategyClosed = "closed"

	ContentModerationKeywordCategoryCustom             = "custom"
	ContentModerationKeywordCategoryJailbreak          = "jailbreak"
	ContentModerationKeywordCategoryCyber              = "cyber"
	ContentModerationKeywordCategoryMinorSafety        = "minor_safety"
	ContentModerationKeywordCategorySelfHarm           = "self_harm"
	ContentModerationKeywordCategoryViolence           = "violence"
	ContentModerationKeywordCategoryWeapons            = "weapons"
	ContentModerationKeywordCategoryPrivacy            = "privacy"
	ContentModerationKeywordCategoryFraud              = "fraud"
	ContentModerationKeywordCategoryAccountAbuse       = "account_abuse"
	ContentModerationKeywordCategoryPolitical          = "political"
	ContentModerationKeywordCategoryHighImpactDecision = "high_impact_decision"
	ContentModerationKeywordCategoryRegulatedAdvice    = "regulated_advice"
	ContentModerationKeywordCategoryCopyright          = "copyright"
	ContentModerationKeywordCategoryBiometric          = "biometric"
	ContentModerationKeywordCategoryOther              = "other"

	ContentModerationKeywordSeverityLow      = "low"
	ContentModerationKeywordSeverityMedium   = "medium"
	ContentModerationKeywordSeverityHigh     = "high"
	ContentModerationKeywordSeverityCritical = "critical"

	ContentModerationKeywordActionBlock   = "block"
	ContentModerationKeywordActionObserve = "observe"
	ContentModerationKeywordActionWarn    = "warn"

	ContentModerationRiskContextActualRequest  = "actual_request"
	ContentModerationRiskContextMetaDiscussion = "meta_discussion"
	ContentModerationRiskContextCodexInternal  = "codex_internal"
	ContentModerationRiskContextEducational    = "educational"
	ContentModerationRiskContextUnknown        = "unknown"

	ContentModerationReviewStatusPending            = "pending"
	ContentModerationReviewStatusFalsePositive      = "false_positive"
	ContentModerationReviewStatusConfirmedViolation = "confirmed_violation"

	ContentModerationModelFilterAll     = "all"
	ContentModerationModelFilterInclude = "include"
	ContentModerationModelFilterExclude = "exclude"

	ContentModerationProtocolAnthropicMessages = "anthropic_messages"
	ContentModerationProtocolOpenAIResponses   = "openai_responses"
	ContentModerationProtocolOpenAIChat        = "openai_chat_completions"
	ContentModerationProtocolOpenAIMessages    = "openai_messages"
	ContentModerationProtocolGemini            = "gemini"
	ContentModerationProtocolOpenAIImages      = "openai_images"
	ContentModerationProtocolOpenAIEmbeddings  = "openai_embeddings"

	ContentModerationAuditScopeUserOnly    = "user_only"
	ContentModerationAuditScopeUserAndTool = "user_and_tool"
	ContentModerationAuditScopeAllContext  = "all_context"

	defaultContentModerationBaseURL     = "https://api.openai.com"
	defaultContentModerationModel       = "omni-moderation-latest"
	defaultContentModerationTimeoutMS   = 3000
	maxContentModerationTimeoutMS       = 30000
	maxModerationInputRunes             = 12000
	maxModerationExcerptRunes           = 240
	maxContentModerationRawRequestBytes = 64 * 1024 * 1024

	defaultContentModerationWorkerCount                    = 4
	maxContentModerationWorkerCount                        = 32
	defaultContentModerationQueueSize                      = 32768
	maxContentModerationQueueSize                          = 100000
	defaultContentModerationBanThreshold                   = 10
	defaultContentModerationViolationWindowHours           = 720
	defaultContentModerationBlockHTTPStatus                = http.StatusForbidden
	defaultContentModerationBlockMessage                   = "内容审计命中风险规则，请调整输入后重试"
	defaultContentModerationRetryCount                     = 2
	maxContentModerationRetryCount                         = 5
	defaultContentModerationLocalClassifierTimeoutMS       = 80
	maxContentModerationLocalClassifierTimeoutMS           = 1000
	defaultContentModerationLocalClassifierMaxConcurrency  = 1
	maxContentModerationLocalClassifierMaxConcurrency      = 4
	defaultContentModerationLocalClassifierBlockThreshold  = 0.85
	defaultContentModerationLocalClassifierReviewThreshold = 0.65
	minContentModerationLocalClassifierScore               = 60
	defaultContentModerationHitRetentionDays               = 180
	defaultContentModerationNonHitRetentionDays            = 3
	maxContentModerationRetentionDays                      = 3650
	maxContentModerationNonHitRetentionDays                = 3
	contentModerationKeyRateLimitFreezeDuration            = time.Minute
	contentModerationKeyAuthFreezeDuration                 = 10 * time.Minute
	contentModerationKeyHTTPErrorFreezeDuration            = 10 * time.Second
	maxContentModerationInputImages                        = 1
	maxContentModerationTestImages                         = maxContentModerationInputImages
	maxContentModerationTestImageBytes                     = 8 * 1024 * 1024
	maxContentModerationTestImageDataURLBytes              = 12 * 1024 * 1024
	maxContentModerationBlockedKeywords                    = 10000
	maxContentModerationBlockedKeywordRunes                = 200
	maxContentModerationModelFilterModels                  = 1000
	maxContentModerationModelFilterRunes                   = 200

	contentModerationCleanupInterval = 24 * time.Hour
	contentModerationCleanupTimeout  = 30 * time.Minute
	contentModerationCleanupDelay    = 5 * time.Minute

	contentModerationPolicySchemaVersion           = "2026-06-29.1"
	contentModerationExtractorVersion              = "v4"
	contentModerationMinimumSecurityBaselineCommit = "9216c848"
	contentModerationRouteManifestVersion          = "2026-06-29.11"
	contentModerationPipelineCoverageVersion       = "gateway-pipeline-coverage-v1"
	minContentModerationBuildCommitPrefixLen       = 7
)

type contentModerationRouteCoverageEntry = moderationcoverage.Entry

var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

type ContentModerationConfig struct {
	Enabled              bool                                   `json:"enabled"`
	Mode                 string                                 `json:"mode"`
	BaseURL              string                                 `json:"base_url"`
	Model                string                                 `json:"model"`
	APIKey               string                                 `json:"api_key,omitempty"`
	APIKeys              []string                               `json:"api_keys,omitempty"`
	TimeoutMS            int                                    `json:"timeout_ms"`
	SampleRate           int                                    `json:"sample_rate"`
	AllGroups            bool                                   `json:"all_groups"`
	GroupIDs             []int64                                `json:"group_ids"`
	RecordNonHits        bool                                   `json:"record_non_hits"`
	AuditScope           string                                 `json:"audit_scope,omitempty"`
	StoreInputExcerpt    bool                                   `json:"store_input_excerpt"`
	SearchInputExcerpt   bool                                   `json:"search_input_excerpt"`
	Thresholds           map[string]float64                     `json:"thresholds"`
	WorkerCount          int                                    `json:"worker_count"`
	QueueSize            int                                    `json:"queue_size"`
	BlockStatus          int                                    `json:"block_status"`
	BlockMessage         string                                 `json:"block_message"`
	EmailOnHit           bool                                   `json:"email_on_hit"`
	AutoBanEnabled       bool                                   `json:"auto_ban_enabled"`
	BanThreshold         int                                    `json:"ban_threshold"`
	ViolationWindowHours int                                    `json:"violation_window_hours"`
	RetryCount           int                                    `json:"retry_count"`
	HitRetentionDays     int                                    `json:"hit_retention_days"`
	NonHitRetentionDays  int                                    `json:"non_hit_retention_days"`
	PreHashCheckEnabled  bool                                   `json:"pre_hash_check_enabled"`
	BlockedKeywords      []string                               `json:"blocked_keywords"`
	KeywordRules         []ContentModerationKeywordRule         `json:"keyword_rules,omitempty"`
	KeywordBlockingMode  string                                 `json:"keyword_blocking_mode"`
	EngineMode           string                                 `json:"engine_mode,omitempty"`
	LocalClassifier      ContentModerationLocalClassifierConfig `json:"local_classifier,omitempty"`
	ModelFilter          ContentModerationModelFilter           `json:"model_filter"`
	FailStrategy         ContentModerationFailStrategy          `json:"fail_strategy"`
	// CyberPolicyExcludeFromBanCount 为 true 时，cyber_policy 命中不参与自动封号计数：
	// 当次不判定封号，且历史 cyber 行在 CountFlaggedByUserSince 中被排除。
	// 默认 false（计入，与历史行为一致；旧配置 JSON 无此字段时反序列化为 false）。
	CyberPolicyExcludeFromBanCount bool `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationConfigView struct {
	Enabled                        bool                                   `json:"enabled"`
	Mode                           string                                 `json:"mode"`
	BaseURL                        string                                 `json:"base_url"`
	Model                          string                                 `json:"model"`
	APIKeyConfigured               bool                                   `json:"api_key_configured"`
	APIKeyMasked                   string                                 `json:"api_key_masked"`
	APIKeyCount                    int                                    `json:"api_key_count"`
	APIKeyMasks                    []string                               `json:"api_key_masks"`
	APIKeyStatuses                 []ContentModerationAPIKeyStatus        `json:"api_key_statuses"`
	TimeoutMS                      int                                    `json:"timeout_ms"`
	SampleRate                     int                                    `json:"sample_rate"`
	AllGroups                      bool                                   `json:"all_groups"`
	GroupIDs                       []int64                                `json:"group_ids"`
	RecordNonHits                  bool                                   `json:"record_non_hits"`
	AuditScope                     string                                 `json:"audit_scope"`
	StoreInputExcerpt              bool                                   `json:"store_input_excerpt"`
	SearchInputExcerpt             bool                                   `json:"search_input_excerpt"`
	Thresholds                     map[string]float64                     `json:"thresholds"`
	WorkerCount                    int                                    `json:"worker_count"`
	QueueSize                      int                                    `json:"queue_size"`
	BlockStatus                    int                                    `json:"block_status"`
	BlockMessage                   string                                 `json:"block_message"`
	EmailOnHit                     bool                                   `json:"email_on_hit"`
	AutoBanEnabled                 bool                                   `json:"auto_ban_enabled"`
	BanThreshold                   int                                    `json:"ban_threshold"`
	ViolationWindowHours           int                                    `json:"violation_window_hours"`
	RetryCount                     int                                    `json:"retry_count"`
	HitRetentionDays               int                                    `json:"hit_retention_days"`
	NonHitRetentionDays            int                                    `json:"non_hit_retention_days"`
	PreHashCheckEnabled            bool                                   `json:"pre_hash_check_enabled"`
	BlockedKeywords                []string                               `json:"blocked_keywords"`
	KeywordRules                   []ContentModerationKeywordRule         `json:"keyword_rules"`
	KeywordBlockingMode            string                                 `json:"keyword_blocking_mode"`
	EngineMode                     string                                 `json:"engine_mode"`
	LocalClassifier                ContentModerationLocalClassifierConfig `json:"local_classifier"`
	ModelFilter                    ContentModerationModelFilter           `json:"model_filter"`
	FailStrategy                   ContentModerationFailStrategy          `json:"fail_strategy"`
	CyberPolicyExcludeFromBanCount bool                                   `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationKeywordRule struct {
	Keyword  string `json:"keyword"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Action   string `json:"action"`
	Enabled  bool   `json:"enabled"`
}

type ContentModerationLocalClassifierConfig struct {
	Enabled         bool    `json:"enabled"`
	URL             string  `json:"url,omitempty"`
	TimeoutMS       int     `json:"timeout_ms"`
	MaxConcurrency  int     `json:"max_concurrency"`
	BlockThreshold  float64 `json:"block_threshold"`
	ReviewThreshold float64 `json:"review_threshold"`
}

type ContentModerationFailStrategy struct {
	Default         string  `json:"default"`
	TrustedGroupIDs []int64 `json:"trusted_group_ids"`
	PublicGroupIDs  []int64 `json:"public_group_ids"`
}

type ContentModerationAPIKeyStatus struct {
	Index          int        `json:"index"`
	KeyHash        string     `json:"key_hash"`
	Masked         string     `json:"masked"`
	Status         string     `json:"status"`
	FailureCount   int        `json:"failure_count"`
	SuccessCount   int64      `json:"success_count"`
	LastError      string     `json:"last_error"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	FrozenUntil    *time.Time `json:"frozen_until,omitempty"`
	LastLatencyMS  int        `json:"last_latency_ms"`
	LastHTTPStatus int        `json:"last_http_status"`
	LastTested     bool       `json:"last_tested"`
	Configured     bool       `json:"configured"`
}

type ContentModerationAPIKeyLoad struct {
	Index          int    `json:"index"`
	KeyHash        string `json:"key_hash"`
	Masked         string `json:"masked"`
	Status         string `json:"status"`
	Active         int64  `json:"active"`
	Total          int64  `json:"total"`
	Success        int64  `json:"success"`
	Errors         int64  `json:"errors"`
	AvgLatencyMS   int64  `json:"avg_latency_ms"`
	LastLatencyMS  int    `json:"last_latency_ms"`
	LastHTTPStatus int    `json:"last_http_status"`
}

type TestContentModerationAPIKeysInput struct {
	APIKeys   []string `json:"api_keys"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type TestContentModerationAPIKeysResult struct {
	Items       []ContentModerationAPIKeyStatus   `json:"items"`
	AuditResult *ContentModerationTestAuditResult `json:"audit_result,omitempty"`
	ImageCount  int                               `json:"image_count"`
}

type ContentModerationTestAuditResult struct {
	Flagged         bool               `json:"flagged"`
	HighestCategory string             `json:"highest_category"`
	HighestScore    float64            `json:"highest_score"`
	CompositeScore  float64            `json:"composite_score"`
	CategoryScores  map[string]float64 `json:"category_scores"`
	Thresholds      map[string]float64 `json:"thresholds"`
}

type UpdateContentModerationConfigInput struct {
	Enabled                        *bool                                   `json:"enabled"`
	Mode                           *string                                 `json:"mode"`
	BaseURL                        *string                                 `json:"base_url"`
	Model                          *string                                 `json:"model"`
	APIKey                         *string                                 `json:"api_key"`
	APIKeys                        *[]string                               `json:"api_keys"`
	APIKeysMode                    string                                  `json:"api_keys_mode"`
	DeleteAPIKeyHashes             *[]string                               `json:"delete_api_key_hashes"`
	ClearAPIKey                    bool                                    `json:"clear_api_key"`
	TimeoutMS                      *int                                    `json:"timeout_ms"`
	SampleRate                     *int                                    `json:"sample_rate"`
	AllGroups                      *bool                                   `json:"all_groups"`
	GroupIDs                       *[]int64                                `json:"group_ids"`
	RecordNonHits                  *bool                                   `json:"record_non_hits"`
	AuditScope                     *string                                 `json:"audit_scope"`
	StoreInputExcerpt              *bool                                   `json:"store_input_excerpt"`
	SearchInputExcerpt             *bool                                   `json:"search_input_excerpt"`
	Thresholds                     *map[string]float64                     `json:"thresholds"`
	WorkerCount                    *int                                    `json:"worker_count"`
	QueueSize                      *int                                    `json:"queue_size"`
	BlockStatus                    *int                                    `json:"block_status"`
	BlockMessage                   *string                                 `json:"block_message"`
	EmailOnHit                     *bool                                   `json:"email_on_hit"`
	AutoBanEnabled                 *bool                                   `json:"auto_ban_enabled"`
	BanThreshold                   *int                                    `json:"ban_threshold"`
	ViolationWindowHours           *int                                    `json:"violation_window_hours"`
	RetryCount                     *int                                    `json:"retry_count"`
	HitRetentionDays               *int                                    `json:"hit_retention_days"`
	NonHitRetentionDays            *int                                    `json:"non_hit_retention_days"`
	PreHashCheckEnabled            *bool                                   `json:"pre_hash_check_enabled"`
	BlockedKeywords                *[]string                               `json:"blocked_keywords"`
	KeywordRules                   *[]ContentModerationKeywordRule         `json:"keyword_rules"`
	KeywordBlockingMode            *string                                 `json:"keyword_blocking_mode"`
	EngineMode                     *string                                 `json:"engine_mode"`
	LocalClassifier                *ContentModerationLocalClassifierConfig `json:"local_classifier"`
	ModelFilter                    *ContentModerationModelFilter           `json:"model_filter"`
	FailStrategy                   *ContentModerationFailStrategy          `json:"fail_strategy"`
	CyberPolicyExcludeFromBanCount *bool                                   `json:"cyber_policy_exclude_from_ban_count"`
}

type ContentModerationKeywordTestResult struct {
	Matched           bool   `json:"matched"`
	MatchedKeyword    string `json:"matched_keyword"`
	KeywordCategory   string `json:"keyword_category"`
	KeywordSeverity   string `json:"keyword_severity"`
	Action            string `json:"keyword_action"`
	EffectiveAction   string `json:"effective_keyword_action"`
	RiskContextType   string `json:"risk_context_type"`
	RiskContextReason string `json:"risk_context_reason"`
	NormalizedExcerpt string `json:"normalized_excerpt"`
}

type ContentModerationModelFilter struct {
	Type   string   `json:"type"`
	Models []string `json:"models"`
}

type ContentModerationCheckInput struct {
	RequestID  string
	UserID     int64
	UserEmail  string
	APIKeyID   int64
	APIKeyName string
	GroupID    *int64
	GroupName  string
	Endpoint   string
	Provider   string
	Model      string
	Protocol   string
	Body       []byte
}

type ContentModerationInput struct {
	Text            string
	Images          []string
	Sources         []ContentModerationInputSource
	Truncated       bool
	TruncateReasons []string
}

type ContentModerationInputSource struct {
	Source string
	Text   string
}

func (in *ContentModerationInput) Normalize() {
	if in == nil {
		return
	}
	in.Text = trimRunes(normalizeContentModerationText(in.Text), maxModerationInputRunes)
	in.Images = normalizeModerationImages(in.Images)
	in.Sources = normalizeContentModerationInputSources(in.Sources)
	in.TruncateReasons = normalizeContentModerationTruncateReasons(in.TruncateReasons)
	in.Truncated = in.Truncated || len(in.TruncateReasons) > 0
}

func (in ContentModerationInput) IsEmpty() bool {
	return strings.TrimSpace(in.Text) == "" && len(in.Images) == 0
}

func (in ContentModerationInput) hasOversizedEncodedPayloadSkipped() bool {
	for _, reason := range in.TruncateReasons {
		switch strings.TrimSpace(reason) {
		case "oversized_base64_skipped", "oversized_base64_decoded_skipped":
			return true
		}
	}
	return false
}

func (in ContentModerationInput) ModerationInput() any {
	images := limitContentModerationImages(in.Images)
	if len(images) == 0 {
		return in.Text
	}
	parts := make([]moderationAPIInputPart, 0, len(images)+1)
	if strings.TrimSpace(in.Text) != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: in.Text})
	}
	for _, image := range images {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts
}

func (in ContentModerationInput) ExcerptText() string {
	return in.Text
}

func (in ContentModerationInput) KeywordHitExcerpt(keyword string) string {
	if strings.TrimSpace(keyword) == "" {
		return in.ExcerptText()
	}
	for _, source := range in.Sources {
		if excerpt, ok := contentModerationKeywordHitExcerptFromText(source.Text, keyword); ok {
			return excerpt
		}
	}
	if excerpt, ok := contentModerationKeywordHitExcerptFromText(in.Text, keyword); ok {
		return excerpt
	}
	return in.ExcerptText()
}

func (in ContentModerationInput) Hash() string {
	h := sha256.New()
	_, _ = h.Write([]byte("text:"))
	_, _ = h.Write([]byte(in.Text))
	for _, image := range in.Images {
		imageHash := sha256.Sum256([]byte(image))
		_, _ = h.Write([]byte("\nimage:"))
		_, _ = h.Write([]byte(hex.EncodeToString(imageHash[:])))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type ContentModerationDecision struct {
	Allowed                bool               `json:"allowed"`
	Blocked                bool               `json:"blocked"`
	Flagged                bool               `json:"flagged"`
	Message                string             `json:"message"`
	StatusCode             int                `json:"status_code"`
	InputHash              string             `json:"input_hash,omitempty"`
	HighestCategory        string             `json:"highest_category"`
	HighestScore           float64            `json:"highest_score"`
	CategoryScores         map[string]float64 `json:"category_scores"`
	Action                 string             `json:"action"`
	MatchedKeyword         string             `json:"matched_keyword,omitempty"`
	KeywordCategory        string             `json:"keyword_category,omitempty"`
	KeywordSeverity        string             `json:"keyword_severity,omitempty"`
	KeywordAction          string             `json:"keyword_action,omitempty"`
	EffectiveKeywordAction string             `json:"effective_keyword_action,omitempty"`
	RiskContextType        string             `json:"risk_context_type,omitempty"`
	RiskContextReason      string             `json:"risk_context_reason,omitempty"`
}

type ContentModerationLog struct {
	ID                     int64              `json:"id"`
	DecisionID             string             `json:"decision_id,omitempty"`
	RequestID              string             `json:"request_id"`
	UserID                 *int64             `json:"user_id,omitempty"`
	UserEmail              string             `json:"user_email"`
	APIKeyID               *int64             `json:"api_key_id,omitempty"`
	APIKeyName             string             `json:"api_key_name"`
	GroupID                *int64             `json:"group_id,omitempty"`
	GroupName              string             `json:"group_name"`
	Endpoint               string             `json:"endpoint"`
	Provider               string             `json:"provider"`
	Model                  string             `json:"model"`
	Mode                   string             `json:"mode"`
	Action                 string             `json:"action"`
	Flagged                bool               `json:"flagged"`
	HighestCategory        string             `json:"highest_category"`
	HighestScore           float64            `json:"highest_score"`
	CategoryScores         map[string]float64 `json:"category_scores"`
	ThresholdSnapshot      map[string]float64 `json:"threshold_snapshot"`
	InputExcerpt           string             `json:"input_excerpt"`
	UpstreamLatencyMS      *int               `json:"upstream_latency_ms,omitempty"`
	Error                  string             `json:"error"`
	MatchedKeyword         string             `json:"matched_keyword"`
	KeywordCategory        string             `json:"keyword_category"`
	KeywordSeverity        string             `json:"keyword_severity"`
	KeywordAction          string             `json:"keyword_action"`
	EffectiveKeywordAction string             `json:"effective_keyword_action"`
	RiskContextType        string             `json:"risk_context_type"`
	RiskContextReason      string             `json:"risk_context_reason"`
	ReviewStatus           string             `json:"review_status"`
	ReviewNote             string             `json:"review_note"`
	ReviewedBy             *int64             `json:"reviewed_by,omitempty"`
	ReviewedAt             *time.Time         `json:"reviewed_at,omitempty"`
	ViolationCount         int                `json:"violation_count"`
	AutoBanned             bool               `json:"auto_banned"`
	EmailSent              bool               `json:"email_sent"`
	UserStatus             string             `json:"user_status"`
	QueueDelayMS           *int               `json:"queue_delay_ms,omitempty"`
	RawRequestAvailable    bool               `json:"raw_request_available"`
	RawRequestBytes        int                `json:"raw_request_bytes"`
	RawRequestTruncated    bool               `json:"raw_request_truncated"`
	CreatedAt              time.Time          `json:"created_at"`
	persisted              bool
}

type ContentModerationRawRequestSnapshot struct {
	ID            int64     `json:"id"`
	LogID         int64     `json:"log_id"`
	RequestID     string    `json:"request_id"`
	BodyEncrypted string    `json:"-"`
	BodyBytes     int       `json:"body_bytes"`
	Truncated     bool      `json:"truncated"`
	CreatedAt     time.Time `json:"created_at"`
}

type ContentModerationRawRequestView struct {
	LogID     int64     `json:"log_id"`
	RequestID string    `json:"request_id"`
	Body      string    `json:"body"`
	BodyBytes int       `json:"body_bytes"`
	Truncated bool      `json:"truncated"`
	CreatedAt time.Time `json:"created_at"`
}

type ContentModerationLogFilter struct {
	Pagination         pagination.PaginationParams
	Result             string
	ReviewStatus       string
	GroupID            *int64
	Endpoint           string
	Search             string
	SearchInputExcerpt bool
	From               *time.Time
	To                 *time.Time
}

type ContentModerationCleanupResult struct {
	DeletedHit    int64     `json:"deleted_hit"`
	DeletedNonHit int64     `json:"deleted_non_hit"`
	FinishedAt    time.Time `json:"finished_at"`
}

type ContentModerationBuildStatus struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	BuildType string `json:"build_type"`
}

type ContentModerationSecurityBaselineStatus struct {
	PolicySchemaVersion           string `json:"policy_schema_version"`
	ModerationExtractorVersion    string `json:"moderation_extractor_version"`
	MinimumSecurityBaselineCommit string `json:"minimum_security_baseline_commit"`
	BaselineSatisfied             bool   `json:"baseline_satisfied"`
	BaselineSatisfactionMethod    string `json:"baseline_satisfaction_method"`
}

type ContentModerationEffectiveProtectionStatus struct {
	EffectiveBlocking          bool     `json:"effective_blocking"`
	RiskControlEnabled         bool     `json:"risk_control_enabled"`
	ModerationEnabled          bool     `json:"moderation_enabled"`
	Mode                       string   `json:"mode"`
	AuditScope                 string   `json:"audit_scope"`
	PublicFailStrategy         string   `json:"public_fail_strategy"`
	GroupCoverage              string   `json:"group_coverage"`
	ModelCoverage              string   `json:"model_coverage"`
	EngineMode                 string   `json:"engine_mode"`
	ExternalAPIConfigured      bool     `json:"external_api_configured"`
	ExternalAPIHealthy         bool     `json:"external_api_healthy"`
	ExternalAPIUsableKeyCount  int      `json:"external_api_usable_key_count"`
	ExternalAPILastError       string   `json:"external_api_last_error"`
	HighRiskRulesBlocking      bool     `json:"high_risk_rules_blocking"`
	DeterministicPolicyPresent bool     `json:"deterministic_policy_present"`
	HighRiskRulesPresent       bool     `json:"high_risk_rules_present"`
	UnsafeReasons              []string `json:"unsafe_reasons"`
}

type ContentModerationRouteCoverageStatus = moderationcoverage.Status

type ContentModerationPipelineCoverageStatus struct {
	ManifestVersion   string                                                   `json:"manifest_version"`
	Version           string                                                   `json:"version"`
	ManifestHash      string                                                   `json:"manifest_hash"`
	Status            string                                                   `json:"status"`
	Global            ContentModerationGlobalPipelineCoverageStatus            `json:"global"`
	OpenAIHTTP        ContentModerationOpenAIHTTPPipelineCoverageStatus        `json:"openai_http"`
	OpenAIWebSocket   ContentModerationOpenAIWebSocketPipelineCoverageStatus   `json:"openai_websocket"`
	GatewayPreForward ContentModerationGatewayPreForwardPipelineCoverageStatus `json:"gateway_pre_forward"`
}

type ContentModerationGlobalPipelineCoverageStatus = ContentModerationPipelineGroupCoverageStatus

type ContentModerationOpenAIHTTPPipelineCoverageStatus = ContentModerationPipelineGroupCoverageStatus

type ContentModerationOpenAIWebSocketPipelineCoverageStatus struct {
	Version         string                                         `json:"version"`
	Pipeline        string                                         `json:"pipeline"`
	Status          string                                         `json:"status"`
	RequiredRoutes  int                                            `json:"required_routes"`
	CoveredRoutes   int                                            `json:"covered_routes"`
	UncoveredRoutes []string                                       `json:"uncovered_routes"`
	StageCoverage   []ContentModerationPipelineStageCoverageStatus `json:"stage_coverage"`
	Routes          []ContentModerationPipelineRouteCoverageStatus `json:"routes"`
	Responses       ContentModerationPipelineGroupCoverageStatus   `json:"responses"`
	Realtime        ContentModerationPipelineGroupCoverageStatus   `json:"realtime"`
}

type ContentModerationGatewayPreForwardPipelineCoverageStatus = ContentModerationPipelineGroupCoverageStatus

type ContentModerationPipelineGroupCoverageStatus struct {
	Version         string                                         `json:"version"`
	Pipeline        string                                         `json:"pipeline"`
	Status          string                                         `json:"status"`
	RequiredRoutes  int                                            `json:"required_routes"`
	CoveredRoutes   int                                            `json:"covered_routes"`
	UncoveredRoutes []string                                       `json:"uncovered_routes"`
	StageCoverage   []ContentModerationPipelineStageCoverageStatus `json:"stage_coverage"`
	Routes          []ContentModerationPipelineRouteCoverageStatus `json:"routes"`
}

type ContentModerationPipelineStageCoverageStatus struct {
	Stage           string   `json:"stage"`
	RequiredRoutes  int      `json:"required_routes"`
	CoveredRoutes   int      `json:"covered_routes"`
	UncoveredRoutes []string `json:"uncovered_routes"`
}

type ContentModerationPipelineRouteCoverageStatus struct {
	Method                    string                                              `json:"method"`
	Path                      string                                              `json:"path"`
	Handler                   string                                              `json:"handler"`
	Protocol                  string                                              `json:"protocol"`
	Pipeline                  string                                              `json:"pipeline"`
	Covered                   bool                                                `json:"covered"`
	ForwardAdapters           []string                                            `json:"forward_adapters,omitempty"`
	ForwardAdapterDescriptors []moderationcoverage.RouteAdapterDescriptor         `json:"forward_adapter_descriptors,omitempty"`
	StageAdapterDescriptors   []moderationcoverage.RouteAdapterDescriptor         `json:"stage_adapter_descriptors,omitempty"`
	UncoveredStages           []string                                            `json:"uncovered_stages,omitempty"`
	Stages                    []ContentModerationPipelineRouteStageCoverageStatus `json:"stages"`
}

type ContentModerationPipelineRouteStageCoverageStatus struct {
	Stage    string `json:"stage"`
	Required bool   `json:"required"`
	Covered  bool   `json:"covered"`
}

type ContentModerationPipelineExecutionStatus = moderationcoverage.PipelineExecutionSnapshot

type ContentModerationPipelineExecutionObservationStatus = moderationcoverage.PipelineStageExecutionObservation

type ContentModerationRuntimeStatus struct {
	Build                        ContentModerationBuildStatus               `json:"build"`
	SecurityBaseline             ContentModerationSecurityBaselineStatus    `json:"security_baseline"`
	EffectiveProtection          ContentModerationEffectiveProtectionStatus `json:"effective_protection"`
	RouteCoverage                ContentModerationRouteCoverageStatus       `json:"route_coverage"`
	PipelineCoverage             ContentModerationPipelineCoverageStatus    `json:"pipeline_coverage"`
	PipelineExecution            ContentModerationPipelineExecutionStatus   `json:"pipeline_execution"`
	Enabled                      bool                                       `json:"enabled"`
	RiskControlEnabled           bool                                       `json:"risk_control_enabled"`
	Mode                         string                                     `json:"mode"`
	WorkerCount                  int                                        `json:"worker_count"`
	MaxWorkers                   int                                        `json:"max_workers"`
	ActiveWorkers                int                                        `json:"active_workers"`
	IdleWorkers                  int                                        `json:"idle_workers"`
	QueueSize                    int                                        `json:"queue_size"`
	QueueLength                  int                                        `json:"queue_length"`
	QueueUsagePercent            float64                                    `json:"queue_usage_percent"`
	Enqueued                     int64                                      `json:"enqueued"`
	Dropped                      int64                                      `json:"dropped"`
	Processed                    int64                                      `json:"processed"`
	Errors                       int64                                      `json:"errors"`
	PreBlockActive               int                                        `json:"pre_block_active"`
	PreBlockChecked              int64                                      `json:"pre_block_checked"`
	PreBlockAllowed              int64                                      `json:"pre_block_allowed"`
	PreBlockBlocked              int64                                      `json:"pre_block_blocked"`
	PreBlockErrors               int64                                      `json:"pre_block_errors"`
	PreBlockAvgLatencyMS         int64                                      `json:"pre_block_avg_latency_ms"`
	PreBlockAPIKeyActive         int64                                      `json:"pre_block_api_key_active"`
	PreBlockAPIKeyAvailableCount int64                                      `json:"pre_block_api_key_available_count"`
	PreBlockAPIKeyTotalCalls     int64                                      `json:"pre_block_api_key_total_calls"`
	PreBlockAPIKeyLoads          []ContentModerationAPIKeyLoad              `json:"pre_block_api_key_loads"`
	APIKeyStatuses               []ContentModerationAPIKeyStatus            `json:"api_key_statuses"`
	FlaggedHashCount             int64                                      `json:"flagged_hash_count"`
	LastCleanupAt                *time.Time                                 `json:"last_cleanup_at,omitempty"`
	LastCleanupDeletedHit        int64                                      `json:"last_cleanup_deleted_hit"`
	LastCleanupDeletedNonHit     int64                                      `json:"last_cleanup_deleted_non_hit"`
	Outbox                       ContentModerationOutboxStatus              `json:"outbox"`
}

type ContentModerationUnbanUserResult struct {
	UserID int64  `json:"user_id"`
	Status string `json:"status"`
}

type ContentModerationLogReviewInput struct {
	Status     string `json:"status"`
	Note       string `json:"note"`
	ReviewedBy int64  `json:"reviewed_by"`
}

type ContentModerationDeleteHashResult struct {
	InputHash string `json:"input_hash"`
	Deleted   bool   `json:"deleted"`
}

type ContentModerationClearHashesResult struct {
	Deleted int64 `json:"deleted"`
}

type ContentModerationRepository interface {
	CreateLog(ctx context.Context, log *ContentModerationLog) error
	ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error)
	// CountFlaggedByUserSince 统计窗口内计入封号的违规次数（排除 hash_block；
	// excludeCyberPolicy 为 true 时额外排除 cyber_policy 行）。
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error)
	CleanupExpiredLogs(ctx context.Context, hitBefore time.Time, nonHitBefore time.Time) (*ContentModerationCleanupResult, error)
	// UpdateLogEmailSent 回写邮件发送结果（F7：CreateLog 先行后补 EmailSent）。
	UpdateLogEmailSent(ctx context.Context, id int64, sent bool) error
	UpdateLogViolationCountByDecisionID(ctx context.Context, decisionID string, count int) error
	UpdateLogAccountActionByDecisionID(ctx context.Context, decisionID string, violationCount int, autoBanned bool) error
	UpdateLogEmailSentByDecisionID(ctx context.Context, decisionID string, sent bool) error
	ReviewLog(ctx context.Context, id int64, input ContentModerationLogReviewInput) (*ContentModerationLog, error)
}

type ContentModerationRawRequestSnapshotStore interface {
	CreateRawRequestSnapshot(ctx context.Context, snapshot *ContentModerationRawRequestSnapshot) error
	GetRawRequestSnapshotByLogID(ctx context.Context, logID int64) (*ContentModerationRawRequestSnapshot, error)
}

type ContentModerationHashCache interface {
	RecordFlaggedInputHash(ctx context.Context, inputHash string) error
	HasFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	DeleteFlaggedInputHash(ctx context.Context, inputHash string) (bool, error)
	ClearFlaggedInputHashes(ctx context.Context) (int64, error)
	CountFlaggedInputHashes(ctx context.Context) (int64, error)
}

type ContentModerationService struct {
	settingRepo              SettingRepository
	repo                     ContentModerationRepository
	rawRequestSnapshotStore  ContentModerationRawRequestSnapshotStore
	rawRequestEncryptor      SecretEncryptor
	hashCache                ContentModerationHashCache
	groupRepo                GroupRepository
	userRepo                 UserRepository
	authCacheInvalidator     APIKeyAuthCacheInvalidator
	emailService             *EmailService
	outboxRepo               ContentModerationOutboxRepository
	buildInfo                BuildInfo
	baselineStatusMu         sync.Mutex
	baselineStatusValid      bool
	baselineStatus           ContentModerationSecurityBaselineStatus
	httpClient               *http.Client
	asyncQueue               chan contentModerationTask
	workerCount              int
	apiKeyCursor             atomic.Uint64
	asyncActive              atomic.Int64
	asyncEnqueued            atomic.Int64
	asyncDropped             atomic.Int64
	asyncProcessed           atomic.Int64
	asyncErrors              atomic.Int64
	preBlockActive           atomic.Int64
	preBlockChecked          atomic.Int64
	preBlockAllowed          atomic.Int64
	preBlockBlocked          atomic.Int64
	preBlockErrors           atomic.Int64
	preBlockLatencyTotalMS   atomic.Int64
	localClassifierActive    atomic.Int64
	lastCleanupUnix          atomic.Int64
	lastCleanupDeletedHit    atomic.Int64
	lastCleanupDeletedNonHit atomic.Int64
	lastOutboxCleanupDeleted atomic.Int64
	keyHealthMu              sync.Mutex
	keyHealth                map[string]*contentModerationKeyHealth
}

type contentModerationTask struct {
	input            ContentModerationCheckInput
	content          ContentModerationInput
	inputHash        string
	log              *ContentModerationLog
	config           *ContentModerationConfig
	recordHash       bool
	applySideEffects bool
	enqueuedAt       time.Time
}

type contentModerationKeyHealth struct {
	Hash           string
	Masked         string
	FailureCount   int
	SuccessCount   int64
	LastError      string
	LastCheckedAt  time.Time
	FrozenUntil    time.Time
	LastLatencyMS  int
	LastHTTPStatus int
	LastTested     bool
	SyncActive     int64
	SyncTotal      int64
	SyncSuccess    int64
	SyncErrors     int64
	SyncLatencyMS  int64
}

func NewContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
) *ContentModerationService {
	svc := &ContentModerationService{
		settingRepo:          settingRepo,
		repo:                 repo,
		hashCache:            hashCache,
		groupRepo:            groupRepo,
		userRepo:             userRepo,
		authCacheInvalidator: authCacheInvalidator,
		emailService:         emailService,
		httpClient:           &http.Client{},
		workerCount:          maxContentModerationWorkerCount,
		asyncQueue:           make(chan contentModerationTask, maxContentModerationQueueSize),
		keyHealth:            make(map[string]*contentModerationKeyHealth),
	}
	if settingRepo != nil && repo != nil {
		for i := 0; i < svc.workerCount; i++ {
			go svc.worker(i)
		}
		go svc.cleanupWorker()
	}
	return svc
}

func (s *ContentModerationService) SetBuildInfo(buildInfo BuildInfo) {
	if s == nil {
		return
	}
	s.buildInfo = buildInfo
	s.baselineStatusMu.Lock()
	s.baselineStatusValid = false
	s.baselineStatus = ContentModerationSecurityBaselineStatus{}
	s.baselineStatusMu.Unlock()
}

func (s *ContentModerationService) SetRawRequestSnapshotStore(store ContentModerationRawRequestSnapshotStore, encryptor SecretEncryptor) {
	if s == nil {
		return
	}
	s.rawRequestSnapshotStore = store
	s.rawRequestEncryptor = encryptor
}

func (s *ContentModerationService) GetConfig(ctx context.Context) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.configView(cfg), nil
}

func (s *ContentModerationService) UpdateConfig(ctx context.Context, input UpdateContentModerationConfigInput) (*ContentModerationConfigView, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if input.Enabled != nil {
		cfg.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		cfg.Mode = strings.TrimSpace(*input.Mode)
	}
	if input.BaseURL != nil {
		cfg.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		cfg.Model = strings.TrimSpace(*input.Model)
	}
	if input.TimeoutMS != nil {
		cfg.TimeoutMS = *input.TimeoutMS
	}
	if input.SampleRate != nil {
		cfg.SampleRate = *input.SampleRate
	}
	if input.WorkerCount != nil {
		cfg.WorkerCount = *input.WorkerCount
	}
	if input.QueueSize != nil {
		cfg.QueueSize = *input.QueueSize
	}
	if input.BlockStatus != nil {
		cfg.BlockStatus = *input.BlockStatus
	}
	if input.BlockMessage != nil {
		cfg.BlockMessage = strings.TrimSpace(*input.BlockMessage)
	}
	if input.EmailOnHit != nil {
		cfg.EmailOnHit = *input.EmailOnHit
	}
	if input.AutoBanEnabled != nil {
		cfg.AutoBanEnabled = *input.AutoBanEnabled
	}
	if input.BanThreshold != nil {
		cfg.BanThreshold = *input.BanThreshold
	}
	if input.ViolationWindowHours != nil {
		cfg.ViolationWindowHours = *input.ViolationWindowHours
	}
	if input.RetryCount != nil {
		cfg.RetryCount = *input.RetryCount
	}
	if input.HitRetentionDays != nil {
		cfg.HitRetentionDays = *input.HitRetentionDays
	}
	if input.NonHitRetentionDays != nil {
		cfg.NonHitRetentionDays = *input.NonHitRetentionDays
	}
	if input.PreHashCheckEnabled != nil {
		cfg.PreHashCheckEnabled = *input.PreHashCheckEnabled
	}
	if input.BlockedKeywords != nil {
		cfg.BlockedKeywords = normalizeBlockedKeywords(*input.BlockedKeywords)
	}
	if input.KeywordRules != nil {
		cfg.KeywordRules = normalizeContentModerationKeywordRules(*input.KeywordRules)
	}
	if input.KeywordBlockingMode != nil {
		cfg.KeywordBlockingMode = strings.TrimSpace(*input.KeywordBlockingMode)
		if input.EngineMode == nil {
			cfg.EngineMode = ""
		}
	}
	if input.EngineMode != nil {
		cfg.EngineMode = strings.TrimSpace(*input.EngineMode)
		if input.KeywordBlockingMode == nil {
			cfg.KeywordBlockingMode = ""
		}
	}
	if input.LocalClassifier != nil {
		cfg.LocalClassifier = *input.LocalClassifier
	}
	if input.ModelFilter != nil {
		cfg.ModelFilter = *input.ModelFilter
	}
	if input.FailStrategy != nil {
		cfg.FailStrategy = *input.FailStrategy
	}
	if input.AllGroups != nil {
		cfg.AllGroups = *input.AllGroups
	}
	if input.GroupIDs != nil {
		cfg.GroupIDs = normalizeInt64IDs(*input.GroupIDs)
	}
	if input.RecordNonHits != nil {
		cfg.RecordNonHits = *input.RecordNonHits
	}
	if input.AuditScope != nil {
		cfg.AuditScope = strings.TrimSpace(*input.AuditScope)
	}
	if input.StoreInputExcerpt != nil {
		cfg.StoreInputExcerpt = *input.StoreInputExcerpt
	}
	if input.SearchInputExcerpt != nil {
		cfg.SearchInputExcerpt = *input.SearchInputExcerpt
	}
	if input.CyberPolicyExcludeFromBanCount != nil {
		cfg.CyberPolicyExcludeFromBanCount = *input.CyberPolicyExcludeFromBanCount
	}
	if input.Thresholds != nil {
		cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), *input.Thresholds)
	}
	if input.ClearAPIKey {
		cfg.APIKey = ""
		cfg.APIKeys = []string{}
	} else {
		apiKeysMode := normalizeContentModerationAPIKeysMode(input.APIKeysMode)
		if input.DeleteAPIKeyHashes != nil && apiKeysMode != contentModerationAPIKeysModeReplace {
			cfg.APIKeys = deleteModerationAPIKeysByHash(cfg.apiKeys(), *input.DeleteAPIKeyHashes)
			cfg.APIKey = ""
		}
		if input.APIKeys != nil {
			if apiKeysMode == contentModerationAPIKeysModeReplace {
				cfg.APIKeys = normalizeModerationAPIKeys(*input.APIKeys)
			} else {
				cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.apiKeys(), *input.APIKeys...))
			}
			cfg.APIKey = ""
		}
		if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
			cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, *input.APIKey))
			cfg.APIKey = ""
		}
	}
	if err := s.validateConfig(ctx, cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal content moderation config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyContentModerationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save content moderation config: %w", err)
	}
	return s.configView(cfg), nil
}

func (s *ContentModerationService) TestAPIKeys(ctx context.Context, input TestContentModerationAPIKeysInput) (*TestContentModerationAPIKeysResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	keys := normalizeModerationAPIKeys(input.APIKeys)
	configured := false
	if len(keys) == 0 {
		keys = cfg.apiKeys()
		configured = true
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		cfg.BaseURL = input.BaseURL
	}
	if strings.TrimSpace(input.Model) != "" {
		cfg.Model = input.Model
	}
	if input.TimeoutMS > 0 {
		cfg.TimeoutMS = input.TimeoutMS
	}
	cfg.normalize()
	testInput, imageCount, err := buildModerationTestInput(input.Prompt, input.Images)
	if err != nil {
		return nil, err
	}
	auditOnly := contentModerationTestHasAuditInput(input.Prompt, input.Images)
	if configured && auditOnly {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			return &TestContentModerationAPIKeysResult{
				Items:      s.apiKeyStatuses(keys),
				ImageCount: imageCount,
			}, nil
		}
		keys = []string{key}
	}
	if len(keys) == 0 {
		return &TestContentModerationAPIKeysResult{Items: []ContentModerationAPIKeyStatus{}, ImageCount: imageCount}, nil
	}
	items := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	var auditResult *ContentModerationTestAuditResult
	for idx, key := range keys {
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, testInput, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		keyHash := moderationAPIKeyHash(key)
		if err != nil {
			s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		} else {
			s.markAPIKeySuccess(key, latency, httpStatus)
			if auditResult == nil {
				auditResult = buildContentModerationTestAuditResult(result, cfg.Thresholds)
			}
		}
		status := s.apiKeyStatusForHash(idx, keyHash, maskSecretTail(key), configured)
		status.LastTested = true
		items = append(items, status)
	}
	return &TestContentModerationAPIKeysResult{Items: items, AuditResult: auditResult, ImageCount: imageCount}, nil
}

func (s *ContentModerationService) TestKeywords(ctx context.Context, prompt string) (*ContentModerationKeywordTestResult, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	normalized := normalizeKeywordComparable(prompt)
	result := &ContentModerationKeywordTestResult{
		Matched:           false,
		NormalizedExcerpt: trimRunes(normalized, maxModerationExcerptRunes),
	}
	if match, hit := matchContentModerationLocalRule(prompt, cfg.keywordRules()); hit {
		decision := decideContentModerationKeyword(prompt, match)
		result.Matched = true
		result.MatchedKeyword = match.Keyword
		result.KeywordCategory = match.Category
		result.KeywordSeverity = match.Severity
		result.Action = match.Action
		result.EffectiveAction = decision.effectiveAction
		result.RiskContextType = decision.context.Type
		result.RiskContextReason = decision.context.Reason
		result.NormalizedExcerpt = trimRunes(highlightKeywordComparable(normalized, match.Keyword), maxModerationExcerptRunes)
	}
	return result, nil
}

func (s *ContentModerationService) Check(ctx context.Context, input ContentModerationCheckInput) (*ContentModerationDecision, error) {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	if s == nil || s.settingRepo == nil || s.repo == nil {
		slog.Warn("content_moderation.unavailable_fail_closed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return contentModerationFailureDecision(defaultContentModerationConfig()), nil
	}
	if !s.isRiskControlEnabled(ctx) {
		slog.Info("content_moderation.skip_feature_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.skip_config_load_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"error", err)
		return contentModerationFailureDecision(defaultContentModerationConfig()), nil
	}
	inGroupScope := cfg.includesGroup(input.GroupID)
	inModelScope := cfg.includesModel(input.Model)
	slog.Info("content_moderation.config_loaded",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"provider", input.Provider,
		"protocol", input.Protocol,
		"model", input.Model,
		"enabled", cfg.Enabled,
		"mode", cfg.Mode,
		"all_groups", cfg.AllGroups,
		"configured_group_ids", cfg.GroupIDs,
		"in_group_scope", inGroupScope,
		"model_filter_type", cfg.ModelFilter.Type,
		"configured_models", cfg.ModelFilter.Models,
		"in_model_scope", inModelScope,
		"sample_rate", cfg.SampleRate,
		"api_key_count", len(cfg.apiKeys()),
		"engine_mode", cfg.EngineMode,
		"keyword_blocking_mode", cfg.KeywordBlockingMode,
		"pre_hash_check_enabled", cfg.PreHashCheckEnabled,
		"record_non_hits", cfg.RecordNonHits)
	if !cfg.Enabled {
		slog.Info("content_moderation.skip_config_disabled",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeOff {
		slog.Info("content_moderation.skip_mode_off",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if !inGroupScope {
		slog.Info("content_moderation.skip_group_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"all_groups", cfg.AllGroups,
			"configured_group_ids", cfg.GroupIDs)
		return allow, nil
	}
	if !inModelScope {
		slog.Info("content_moderation.skip_model_out_of_scope",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"group_name", input.GroupName,
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"model", input.Model,
			"model_filter_type", cfg.ModelFilter.Type,
			"configured_models", cfg.ModelFilter.Models)
		return allow, nil
	}
	content := ExtractContentModerationInput(input.Protocol, input.Body, cfg.AuditScope)
	if content.IsEmpty() {
		slog.Info("content_moderation.skip_empty_input",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"body_bytes", len(input.Body))
		if cfg.Mode == ContentModerationModePreBlock && cfg.shouldFailClosed(input) && isUnexpectedEmptyModerationInput(input.Protocol, input.Body) {
			s.recordPreBlockSyncMetric(0, ContentModerationActionError)
			slog.Warn("content_moderation.empty_extraction_fail_closed",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"body_bytes", len(input.Body))
			return contentModerationFailureDecision(cfg), nil
		}
		return allow, nil
	}
	content.Normalize()
	slog.Info("content_moderation.input_extracted",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"text_runes", len([]rune(content.Text)),
		"image_count", len(content.Images))
	if cfg.Mode == ContentModerationModePreBlock && cfg.shouldFailClosed(input) && content.hasOversizedEncodedPayloadSkipped() {
		s.recordPreBlockSyncMetric(0, ContentModerationActionError)
		slog.Warn("content_moderation.oversized_encoded_payload_fail_closed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"truncate_reasons", content.TruncateReasons)
		return contentModerationFailureDecision(cfg), nil
	}
	hashText := content.Hash()
	if cfg.PreHashCheckEnabled && s.hashCache != nil {
		matched, err := s.hashCache.HasFlaggedInputHash(ctx, hashText)
		if err != nil {
			slog.Warn("content_moderation.hash_check_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
		if matched {
			if cfg.Mode == ContentModerationModePreBlock {
				s.recordPreBlockSyncMetric(0, ContentModerationActionHashBlock)
			}
			slog.Info("content_moderation.hash_block",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"input_hash", hashText)
			message := cfg.BlockMessage
			if message != "" {
				message = fmt.Sprintf("%s（hash: %s）", message, hashText)
			}
			scores := map[string]float64{"hash": 1.0}
			logMetadata := contentModerationHitLogMetadata(cfg, content, contentModerationPrimarySource(input.Protocol, content))
			log := s.buildLog(input, cfg, ContentModerationActionHashBlock, true, "hash", 1.0, scores, content.ExcerptText(), nil, nil, logMetadata)
			s.enqueueRecord(ctx, input, cfg, log, hashText, false, false)
			return &ContentModerationDecision{
				Allowed:    false,
				Blocked:    true,
				Flagged:    true,
				Message:    message,
				StatusCode: cfg.BlockStatus,
				InputHash:  hashText,
				Action:     ContentModerationActionHashBlock,
			}, nil
		}
	}
	if cfg.Mode == ContentModerationModePreBlock {
		if cfg.shouldRunLocalRules() {
			if keywordMatch, hit := matchContentModerationLocalRule(content.Text, cfg.keywordRules()); hit {
				return s.keywordDecision(ctx, input, cfg, content, hashText, keywordMatch), nil
			}
		} else if keywordMatch, hit := matchContextualBuiltInRiskRule(content.Text); hit {
			return s.keywordDecision(ctx, input, cfg, content, hashText, keywordMatch), nil
		}
		if classifierDecision, decided := s.localClassifierDecision(ctx, input, cfg, content, hashText); decided {
			return classifierDecision, nil
		}
		if !cfg.externalModerationRequired() {
			s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
			slog.Info("content_moderation.skip_external_moderation_rule_only",
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"group_id", contentModerationLogGroupID(input.GroupID),
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"engine_mode", cfg.EngineMode,
				"keyword_blocking_mode", cfg.KeywordBlockingMode)
			return allow, nil
		}
	}
	if len(cfg.apiKeys()) == 0 {
		slog.Info("content_moderation.skip_no_audit_api_keys",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol)
		return allow, nil
	}
	if cfg.Mode == ContentModerationModeObserve {
		slog.Info("content_moderation.enqueue_observe",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"queue_len", len(s.asyncQueue))
		s.enqueueAsync(input, cfg, content, hashText)
		return allow, nil
	}

	return s.checkSync(ctx, input, cfg, content, hashText, nil, true), nil
}

func (s *ContentModerationService) checkSync(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, queueDelay *int, allowBlock bool) *ContentModerationDecision {
	allow := &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	trackPreBlock := queueDelay == nil && allowBlock && cfg != nil && cfg.Mode == ContentModerationModePreBlock
	if trackPreBlock {
		s.preBlockActive.Add(1)
		defer s.preBlockActive.Add(-1)
	}
	start := time.Now()
	result, err := s.callModeration(ctx, cfg, content.ModerationInput(), trackPreBlock)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		if trackPreBlock {
			s.recordPreBlockSyncMetric(latency, ContentModerationActionError)
		}
		slog.Warn("content_moderation.audit_api_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"mode", cfg.Mode,
			"allow_block", allowBlock,
			"queue_delay_ms", queueDelay,
			"latency_ms", latency,
			"error", err)
		if queueDelay != nil {
			s.asyncErrors.Add(1)
		}
		if cfg.RecordNonHits {
			log := s.buildLog(input, cfg, ContentModerationActionError, false, "", 0, nil, content.ExcerptText(), &latency, queueDelay, err.Error())
			_ = s.repo.CreateLog(ctx, log)
		}
		if allowBlock && cfg.Mode == ContentModerationModePreBlock && cfg.shouldFailClosed(input) {
			return contentModerationFailureDecision(cfg)
		}
		return allow
	}

	flagged, highestCategory, highestScore := evaluateModerationScores(result.CategoryScores, cfg.Thresholds)
	action := ContentModerationActionAllow
	blocked := false
	if allowBlock && flagged && cfg.Mode == ContentModerationModePreBlock {
		action = ContentModerationActionBlock
		blocked = true
	}
	if trackPreBlock {
		s.recordPreBlockSyncMetric(latency, action)
	}
	slog.Info("content_moderation.audit_result",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"group_name", input.GroupName,
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"mode", cfg.Mode,
		"allow_block", allowBlock,
		"flagged", flagged,
		"blocked", blocked,
		"action", action,
		"highest_category", highestCategory,
		"highest_score", highestScore,
		"latency_ms", latency,
		"queue_delay_ms", queueDelay)
	shouldRecordNonHit := cfg.RecordNonHits && (flagged || cfg.shouldRecordNonHit(hashText))
	if flagged || shouldRecordNonHit {
		logMetadata := ""
		if flagged {
			logMetadata = contentModerationHitLogMetadata(cfg, content, contentModerationPrimarySource(input.Protocol, content))
		}
		log := s.buildLog(input, cfg, action, flagged, highestCategory, highestScore, result.CategoryScores, content.ExcerptText(), &latency, queueDelay, logMetadata)
		if queueDelay == nil && cfg.Mode == ContentModerationModePreBlock {
			s.enqueueRecord(ctx, input, cfg, log, hashText, flagged, flagged)
		} else {
			s.persistContentModerationLog(ctx, cfg, log, hashText, flagged, flagged)
		}
	}
	if blocked {
		return &ContentModerationDecision{
			Allowed:         false,
			Blocked:         true,
			Flagged:         true,
			Message:         cfg.BlockMessage,
			StatusCode:      cfg.BlockStatus,
			HighestCategory: highestCategory,
			HighestScore:    highestScore,
			CategoryScores:  result.CategoryScores,
			Action:          action,
		}
	}
	return &ContentModerationDecision{
		Allowed:         true,
		Flagged:         flagged,
		Message:         "",
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CategoryScores:  result.CategoryScores,
		Action:          action,
	}
}

func (s *ContentModerationService) recordPreBlockSyncMetric(latencyMS int, action string) {
	if s == nil {
		return
	}
	s.preBlockChecked.Add(1)
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.preBlockLatencyTotalMS.Add(int64(latencyMS))
	switch action {
	case ContentModerationActionBlock, ContentModerationActionHashBlock, ContentModerationActionKeywordBlock:
		s.preBlockBlocked.Add(1)
	case ContentModerationActionError:
		s.preBlockErrors.Add(1)
	default:
		s.preBlockAllowed.Add(1)
	}
}

func (s *ContentModerationService) keywordDecision(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, keywordMatch ContentModerationKeywordRule) *ContentModerationDecision {
	scores := map[string]float64{contentModerationKeywordCategory: 1.0}
	keywordDecision := decideContentModerationKeyword(content.Text, keywordMatch)
	if keywordDecision.blocked {
		s.recordPreBlockSyncMetric(0, ContentModerationActionKeywordBlock)
	} else {
		s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
	}
	slog.Info("content_moderation.keyword_hit",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"keyword_blocking_mode", cfg.KeywordBlockingMode,
		"keyword", keywordMatch.Keyword,
		"keyword_category", keywordMatch.Category,
		"keyword_severity", keywordMatch.Severity,
		"keyword_action", keywordMatch.Action,
		"effective_keyword_action", keywordDecision.effectiveAction,
		"risk_context_type", keywordDecision.context.Type,
		"risk_context_reason", keywordDecision.context.Reason,
		"blocked", keywordDecision.blocked)
	logMetadata := contentModerationHitLogMetadata(cfg, content, contentModerationMatchedSource(input.Protocol, keywordMatch.Keyword, content))
	log := s.buildLog(input, cfg, keywordDecision.action, keywordDecision.flagged, contentModerationKeywordCategory, 1.0, scores, content.KeywordHitExcerpt(keywordMatch.Keyword), nil, nil, logMetadata)
	applyContentModerationKeywordMetadata(log, keywordDecision)
	s.enqueueRecord(ctx, input, cfg, log, hashText, false, keywordDecision.blocked)
	return contentModerationDecisionFromKeyword(cfg, keywordDecision, scores)
}

func (s *ContentModerationService) keywordReviewDecision(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, keywordMatch ContentModerationKeywordRule, reason string) *ContentModerationDecision {
	scores := map[string]float64{contentModerationKeywordCategory: 1.0}
	keywordMatch = normalizeContentModerationKeywordRules([]ContentModerationKeywordRule{keywordMatch})[0]
	keywordDecision := contentModerationKeywordDecision{
		rule: keywordMatch,
		context: contentModerationRiskContext{
			Type:   ContentModerationRiskContextActualRequest,
			Reason: reason,
		},
		action:          ContentModerationActionKeywordReview,
		flagged:         false,
		blocked:         false,
		effectiveAction: ContentModerationKeywordActionObserve,
	}
	s.recordPreBlockSyncMetric(0, ContentModerationActionAllow)
	slog.Info("content_moderation.keyword_review",
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"group_id", contentModerationLogGroupID(input.GroupID),
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"keyword_blocking_mode", cfg.KeywordBlockingMode,
		"keyword", keywordMatch.Keyword,
		"keyword_category", keywordMatch.Category,
		"keyword_severity", keywordMatch.Severity,
		"keyword_action", keywordMatch.Action,
		"effective_keyword_action", keywordDecision.effectiveAction,
		"risk_context_type", keywordDecision.context.Type,
		"risk_context_reason", keywordDecision.context.Reason)
	logMetadata := contentModerationHitLogMetadata(cfg, content, contentModerationMatchedSource(input.Protocol, keywordMatch.Keyword, content))
	log := s.buildLog(input, cfg, keywordDecision.action, keywordDecision.flagged, contentModerationKeywordCategory, 1.0, scores, content.KeywordHitExcerpt(keywordMatch.Keyword), nil, nil, logMetadata)
	applyContentModerationKeywordMetadata(log, keywordDecision)
	s.enqueueRecord(ctx, input, cfg, log, hashText, false, false)
	return contentModerationDecisionFromKeyword(cfg, keywordDecision, scores)
}

type contentModerationLocalClassifierCandidate struct {
	Keyword  string
	Category string
	Severity string
	Score    int
}

type contentModerationLocalClassifierRequest struct {
	Text              string `json:"text"`
	CandidateKeyword  string `json:"candidate_keyword,omitempty"`
	CandidateCategory string `json:"candidate_category,omitempty"`
	CandidateSeverity string `json:"candidate_severity,omitempty"`
	CandidateScore    int    `json:"candidate_score,omitempty"`
	Endpoint          string `json:"endpoint,omitempty"`
	Provider          string `json:"provider,omitempty"`
	Model             string `json:"model,omitempty"`
	Protocol          string `json:"protocol,omitempty"`
}

type contentModerationLocalClassifierResponse struct {
	Label          string  `json:"label"`
	Category       string  `json:"category"`
	Confidence     float64 `json:"confidence"`
	Action         string  `json:"action"`
	Reason         string  `json:"reason"`
	MatchedKeyword string  `json:"matched_keyword"`
}

func (s *ContentModerationService) localClassifierDecision(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) (*ContentModerationDecision, bool) {
	if cfg == nil || !cfg.LocalClassifier.Enabled {
		return nil, false
	}
	candidate, ok := contentModerationLocalClassifierCandidateForText(content.Text)
	if !ok {
		return nil, false
	}
	response, err := s.callLocalClassifier(ctx, cfg, input, content, candidate)
	if err != nil {
		slog.Warn("content_moderation.local_classifier_failed",
			"user_id", input.UserID,
			"api_key_id", input.APIKeyID,
			"group_id", contentModerationLogGroupID(input.GroupID),
			"endpoint", input.Endpoint,
			"protocol", input.Protocol,
			"candidate_keyword", candidate.Keyword,
			"candidate_category", candidate.Category,
			"candidate_score", candidate.Score,
			"error", err)
		return nil, false
	}
	rule, action := contentModerationRuleFromLocalClassifierResponse(cfg, candidate, response)
	switch action {
	case ContentModerationKeywordActionBlock:
		return s.keywordDecision(ctx, input, cfg, content, hashText, rule), true
	case ContentModerationActionKeywordReview:
		reason := strings.TrimSpace(response.Reason)
		if reason == "" {
			reason = "local_classifier_medium_confidence"
		}
		return s.keywordReviewDecision(ctx, input, cfg, content, hashText, rule, reason), true
	default:
		return nil, false
	}
}

func (s *ContentModerationService) callLocalClassifier(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationCheckInput, content ContentModerationInput, candidate contentModerationLocalClassifierCandidate) (*contentModerationLocalClassifierResponse, error) {
	if cfg == nil {
		return nil, errors.New("missing content moderation config")
	}
	localCfg := normalizeContentModerationLocalClassifierConfig(cfg.LocalClassifier)
	if !s.tryBeginLocalClassifierCall(localCfg.MaxConcurrency) {
		return nil, errors.New("local classifier concurrency limit reached")
	}
	defer s.finishLocalClassifierCall()

	payload := contentModerationLocalClassifierRequest{
		Text:              content.Text,
		CandidateKeyword:  candidate.Keyword,
		CandidateCategory: candidate.Category,
		CandidateSeverity: candidate.Severity,
		CandidateScore:    candidate.Score,
		Endpoint:          input.Endpoint,
		Provider:          input.Provider,
		Model:             input.Model,
		Protocol:          input.Protocol,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(localCfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, localCfg.URL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("local classifier status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result contentModerationLocalClassifierResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *ContentModerationService) tryBeginLocalClassifierCall(maxConcurrency int) bool {
	if s == nil {
		return false
	}
	if maxConcurrency <= 0 {
		maxConcurrency = defaultContentModerationLocalClassifierMaxConcurrency
	}
	for {
		active := s.localClassifierActive.Load()
		if active >= int64(maxConcurrency) {
			return false
		}
		if s.localClassifierActive.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (s *ContentModerationService) finishLocalClassifierCall() {
	if s == nil {
		return
	}
	s.localClassifierActive.Add(-1)
}

func contentModerationRuleFromLocalClassifierResponse(cfg *ContentModerationConfig, candidate contentModerationLocalClassifierCandidate, response *contentModerationLocalClassifierResponse) (ContentModerationKeywordRule, string) {
	if cfg == nil || response == nil {
		return ContentModerationKeywordRule{}, ""
	}
	localCfg := normalizeContentModerationLocalClassifierConfig(cfg.LocalClassifier)
	confidence := response.Confidence
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	responseAction := strings.ToLower(strings.TrimSpace(response.Action))
	if responseAction == ContentModerationActionAllow || responseAction == "allow" {
		return ContentModerationKeywordRule{}, ""
	}
	if confidence < localCfg.ReviewThreshold {
		return ContentModerationKeywordRule{}, ""
	}

	keyword := strings.TrimSpace(response.MatchedKeyword)
	if keyword == "" {
		keyword = candidate.Keyword
	}
	if keyword == "" {
		keyword = strings.TrimSpace(response.Label)
	}
	if keyword == "" {
		keyword = "local_classifier"
	}
	category := normalizeLocalClassifierKeywordCategory(response.Category, response.Label, candidate.Category)
	severity := candidate.Severity
	if severity == "" {
		severity = ContentModerationKeywordSeverityHigh
	}
	action := ContentModerationActionKeywordReview
	if confidence >= localCfg.BlockThreshold && responseAction != ContentModerationKeywordActionObserve && responseAction != ContentModerationActionKeywordReview {
		action = ContentModerationKeywordActionBlock
	}
	return ContentModerationKeywordRule{
		Keyword:  keyword,
		Category: category,
		Severity: severity,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}, action
}

func normalizeLocalClassifierKeywordCategory(category string, label string, fallback string) string {
	category = normalizeContentModerationKeywordCategory(category)
	if category != ContentModerationKeywordCategoryOther || strings.TrimSpace(fallback) == "" {
		return category
	}
	normalizedLabel := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(normalizedLabel, "politic"):
		return ContentModerationKeywordCategoryPolitical
	case strings.Contains(normalizedLabel, "cyber"):
		return ContentModerationKeywordCategoryCyber
	case strings.Contains(normalizedLabel, "sexual"):
		return ContentModerationKeywordCategoryOther
	default:
		return normalizeContentModerationKeywordCategory(fallback)
	}
}

func (s *ContentModerationService) enqueueAsync(input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) {
	if s == nil || s.asyncQueue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint, "queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	task := contentModerationTask{
		input:      input,
		content:    content,
		inputHash:  hashText,
		enqueuedAt: time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.async_queue_full", "user_id", input.UserID, "endpoint", input.Endpoint)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) enqueueRecord(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, log *ContentModerationLog, inputHash string, recordHash bool, applySideEffects bool) {
	if s == nil || log == nil {
		return
	}
	s.persistBlockedLogForVisibility(ctx, log)
	if s.enqueueModerationOutboxRecord(input, cfg, log, inputHash, recordHash, applySideEffects) {
		s.asyncEnqueued.Add(1)
		return
	}
	if s.asyncQueue == nil {
		return
	}
	queueSize := defaultContentModerationQueueSize
	if cfg != nil && cfg.QueueSize > 0 {
		queueSize = cfg.QueueSize
	}
	if len(s.asyncQueue) >= queueSize {
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action,
			"queue_size", queueSize)
		s.asyncDropped.Add(1)
		return
	}
	task := contentModerationTask{
		input:            input,
		inputHash:        inputHash,
		log:              log,
		config:           cloneContentModerationConfig(cfg),
		recordHash:       recordHash,
		applySideEffects: applySideEffects,
		enqueuedAt:       time.Now(),
	}
	select {
	case s.asyncQueue <- task:
		s.asyncEnqueued.Add(1)
	default:
		slog.Warn("content_moderation.record_queue_full",
			"user_id", input.UserID,
			"endpoint", input.Endpoint,
			"action", log.Action)
		s.asyncDropped.Add(1)
	}
}

func (s *ContentModerationService) persistBlockedLogForVisibility(ctx context.Context, log *ContentModerationLog) {
	if s == nil || s.repo == nil || log == nil || !contentModerationActionIsBlocking(log.Action) {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.repo.CreateLog(ctx, log); err != nil {
		slog.Warn("content_moderation.create_block_log_failed",
			"user_id", contentModerationEmailUserID(log),
			"endpoint", log.Endpoint,
			"action", log.Action,
			"decision_id", log.DecisionID,
			"error", err)
		return
	}
	log.persisted = true
}

func contentModerationActionIsBlocking(action string) bool {
	switch strings.TrimSpace(action) {
	case ContentModerationActionBlock, ContentModerationActionHashBlock, ContentModerationActionKeywordBlock:
		return true
	default:
		return false
	}
}

func (s *ContentModerationService) worker(id int) {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), maxContentModerationTimeoutMS*time.Millisecond+10*time.Second)
		cfg, err := s.loadConfig(ctx)
		if err != nil || id >= cfg.WorkerCount {
			cancel()
			time.Sleep(time.Second)
			continue
		}
		task, ok := s.dequeueAsyncTask(ctx, time.Second)
		if !ok {
			cancel()
			continue
		}
		func() {
			defer cancel()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("content_moderation.worker_panic", "worker_id", id, "recover", r)
				}
			}()
			if task.log != nil {
				s.asyncActive.Add(1)
				defer s.asyncActive.Add(-1)
				queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
				task.log.QueueDelayMS = &queueDelay
				taskCfg := task.config
				if taskCfg == nil {
					taskCfg = cfg
				}
				s.persistContentModerationLog(ctx, taskCfg, task.log, task.inputHash, task.recordHash, task.applySideEffects)
				s.asyncProcessed.Add(1)
				return
			}
			if !cfg.Enabled || cfg.Mode == ContentModerationModeOff || len(cfg.apiKeys()) == 0 {
				return
			}
			if !cfg.includesGroup(task.input.GroupID) {
				return
			}
			if !cfg.includesModel(task.input.Model) {
				return
			}
			s.asyncActive.Add(1)
			defer s.asyncActive.Add(-1)
			queueDelay := int(time.Since(task.enqueuedAt).Milliseconds())
			_ = s.checkSync(ctx, task.input, cfg, task.content, task.inputHash, &queueDelay, false)
			s.asyncProcessed.Add(1)
		}()
	}
}

func (s *ContentModerationService) dequeueAsyncTask(ctx context.Context, idleWait time.Duration) (contentModerationTask, bool) {
	var zero contentModerationTask
	if s == nil || s.asyncQueue == nil {
		return zero, false
	}
	if idleWait <= 0 {
		idleWait = time.Second
	}
	timer := time.NewTimer(idleWait)
	defer timer.Stop()
	select {
	case task, ok := <-s.asyncQueue:
		return task, ok
	case <-ctx.Done():
		return zero, false
	case <-timer.C:
		return zero, false
	}
}

func (s *ContentModerationService) ListLogs(ctx context.Context, filter ContentModerationLogFilter) ([]ContentModerationLog, *pagination.PaginationResult, error) {
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	if s != nil && s.settingRepo != nil {
		if cfg, err := s.loadConfig(ctx); err == nil && cfg != nil {
			filter.SearchInputExcerpt = cfg.SearchInputExcerpt
		}
	}
	return s.repo.ListLogs(ctx, filter)
}

func (s *ContentModerationService) UnbanUser(ctx context.Context, userID int64) (*ContentModerationUnbanUserResult, error) {
	if s == nil || s.userRepo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_USER_REPOSITORY_UNAVAILABLE", "用户仓储不可用")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, infraerrors.NotFound("USER_NOT_FOUND", "用户不存在")
		}
		return nil, fmt.Errorf("get content moderation unban user: %w", err)
	}
	if user.Status != StatusActive {
		user.Status = StatusActive
		if err := s.userRepo.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("update content moderation unban user: %w", err)
		}
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	return &ContentModerationUnbanUserResult{
		UserID: userID,
		Status: StatusActive,
	}, nil
}

func (s *ContentModerationService) ReviewLog(ctx context.Context, id int64, input ContentModerationLogReviewInput) (*ContentModerationLog, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	if id <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_LOG_ID", "审核记录 ID 无效")
	}
	input.Status = normalizeContentModerationReviewStatus(input.Status)
	input.Note = trimRunes(strings.TrimSpace(input.Note), 1000)
	return s.repo.ReviewLog(ctx, id, input)
}

func (s *ContentModerationService) GetRawRequestSnapshot(ctx context.Context, logID int64) (*ContentModerationRawRequestView, error) {
	if logID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_LOG_ID", "审核记录 ID 无效")
	}
	if s == nil || s.rawRequestSnapshotStore == nil {
		return nil, infraerrors.NotFound("CONTENT_MODERATION_RAW_REQUEST_NOT_FOUND", "原始请求快照不存在")
	}
	if s.rawRequestEncryptor == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_RAW_REQUEST_ENCRYPTOR_UNAVAILABLE", "原始请求解密器不可用")
	}
	snapshot, err := s.rawRequestSnapshotStore.GetRawRequestSnapshotByLogID(ctx, logID)
	if err != nil {
		return nil, err
	}
	body, err := s.rawRequestEncryptor.Decrypt(snapshot.BodyEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt content moderation raw request snapshot: %w", err)
	}
	return &ContentModerationRawRequestView{
		LogID:     snapshot.LogID,
		RequestID: snapshot.RequestID,
		Body:      body,
		BodyBytes: snapshot.BodyBytes,
		Truncated: snapshot.Truncated,
		CreatedAt: snapshot.CreatedAt,
	}, nil
}

func (s *ContentModerationService) DeleteFlaggedInputHash(ctx context.Context, inputHash string) (*ContentModerationDeleteHashResult, error) {
	inputHash = normalizeContentModerationHash(inputHash)
	if inputHash == "" {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_HASH", "风险输入哈希无效")
	}
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.DeleteFlaggedInputHash(ctx, inputHash)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation flagged hash: %w", err)
	}
	return &ContentModerationDeleteHashResult{
		InputHash: inputHash,
		Deleted:   deleted,
	}, nil
}

func (s *ContentModerationService) ClearFlaggedInputHashes(ctx context.Context) (*ContentModerationClearHashesResult, error) {
	if s == nil || s.hashCache == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_HASH_CACHE_UNAVAILABLE", "内容审计哈希缓存不可用")
	}
	deleted, err := s.hashCache.ClearFlaggedInputHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation flagged hashes: %w", err)
	}
	return &ContentModerationClearHashesResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) GetStatus(ctx context.Context) (*ContentModerationRuntimeStatus, error) {
	if s == nil {
		return &ContentModerationRuntimeStatus{}, nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	riskEnabled := s.isRiskControlEnabled(ctx)
	active := int(s.asyncActive.Load())
	if active < 0 {
		active = 0
	}
	if active > cfg.WorkerCount {
		active = cfg.WorkerCount
	}
	preBlockActive := int(s.preBlockActive.Load())
	if preBlockActive < 0 {
		preBlockActive = 0
	}
	preBlockChecked := s.preBlockChecked.Load()
	preBlockAvgLatency := int64(0)
	if preBlockChecked > 0 {
		preBlockAvgLatency = s.preBlockLatencyTotalMS.Load() / preBlockChecked
	}
	queueLength := 0
	if s.asyncQueue != nil {
		queueLength = len(s.asyncQueue)
	}
	queueUsage := 0.0
	if cfg.QueueSize > 0 {
		queueUsage = float64(queueLength) * 100 / float64(cfg.QueueSize)
	}
	var flaggedHashCount int64
	if s.hashCache != nil {
		if n, err := s.hashCache.CountFlaggedInputHashes(ctx); err == nil {
			flaggedHashCount = n
		} else {
			slog.Warn("content_moderation.hash_count_failed", "error", err)
		}
	}
	var lastCleanupAt *time.Time
	if unix := s.lastCleanupUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0)
		lastCleanupAt = &t
	}
	coverageEntries := moderationcoverage.Entries()
	routeCoverage := contentModerationRouteCoverageStatusFromEntries(coverageEntries)
	pipelineCoverage := contentModerationPipelineCoverageStatusFromEntries(coverageEntries)
	pipelineExecution := moderationcoverage.PipelineExecutionObserverSnapshot()
	outboxStatus := s.contentModerationOutboxStatus(ctx)
	return &ContentModerationRuntimeStatus{
		Build:                        s.buildStatus(),
		SecurityBaseline:             s.contentModerationSecurityBaselineStatus(),
		EffectiveProtection:          s.buildContentModerationEffectiveProtectionStatus(cfg, riskEnabled, routeCoverage, pipelineCoverage, flaggedHashCount),
		RouteCoverage:                routeCoverage,
		PipelineCoverage:             pipelineCoverage,
		PipelineExecution:            pipelineExecution,
		Enabled:                      cfg.Enabled,
		RiskControlEnabled:           riskEnabled,
		Mode:                         cfg.Mode,
		WorkerCount:                  cfg.WorkerCount,
		MaxWorkers:                   maxContentModerationWorkerCount,
		ActiveWorkers:                active,
		IdleWorkers:                  cfg.WorkerCount - active,
		QueueSize:                    cfg.QueueSize,
		QueueLength:                  queueLength,
		QueueUsagePercent:            queueUsage,
		Enqueued:                     s.asyncEnqueued.Load(),
		Dropped:                      s.asyncDropped.Load(),
		Processed:                    s.asyncProcessed.Load(),
		Errors:                       s.asyncErrors.Load(),
		PreBlockActive:               preBlockActive,
		PreBlockChecked:              preBlockChecked,
		PreBlockAllowed:              s.preBlockAllowed.Load(),
		PreBlockBlocked:              s.preBlockBlocked.Load(),
		PreBlockErrors:               s.preBlockErrors.Load(),
		PreBlockAvgLatencyMS:         preBlockAvgLatency,
		PreBlockAPIKeyActive:         s.preBlockAPIKeyActive(cfg.apiKeys()),
		PreBlockAPIKeyAvailableCount: s.preBlockAPIKeyAvailableCount(cfg.apiKeys()),
		PreBlockAPIKeyTotalCalls:     s.preBlockAPIKeyTotalCalls(cfg.apiKeys()),
		PreBlockAPIKeyLoads:          s.preBlockAPIKeyLoads(cfg.apiKeys()),
		APIKeyStatuses:               s.apiKeyStatuses(cfg.apiKeys()),
		FlaggedHashCount:             flaggedHashCount,
		LastCleanupAt:                lastCleanupAt,
		LastCleanupDeletedHit:        s.lastCleanupDeletedHit.Load(),
		LastCleanupDeletedNonHit:     s.lastCleanupDeletedNonHit.Load(),
		Outbox:                       outboxStatus,
	}, nil
}

func (s *ContentModerationService) buildStatus() ContentModerationBuildStatus {
	if s == nil {
		return ContentModerationBuildStatus{}
	}
	return ContentModerationBuildStatus{
		Version:   strings.TrimSpace(s.buildInfo.Version),
		Commit:    strings.TrimSpace(s.buildInfo.Commit),
		Date:      strings.TrimSpace(s.buildInfo.Date),
		BuildType: strings.TrimSpace(s.buildInfo.BuildType),
	}
}

func (s *ContentModerationService) contentModerationSecurityBaselineStatus() ContentModerationSecurityBaselineStatus {
	if s == nil {
		return ContentModerationSecurityBaselineStatus{
			PolicySchemaVersion:           contentModerationPolicySchemaVersion,
			ModerationExtractorVersion:    contentModerationExtractorVersion,
			MinimumSecurityBaselineCommit: contentModerationMinimumSecurityBaselineCommit,
			BaselineSatisfied:             false,
			BaselineSatisfactionMethod:    "unknown",
		}
	}
	s.baselineStatusMu.Lock()
	defer s.baselineStatusMu.Unlock()
	if s.baselineStatusValid {
		return s.baselineStatus
	}
	satisfied, method := s.contentModerationBaselineSatisfiedLocked()
	s.baselineStatus = ContentModerationSecurityBaselineStatus{
		PolicySchemaVersion:           contentModerationPolicySchemaVersion,
		ModerationExtractorVersion:    contentModerationExtractorVersion,
		MinimumSecurityBaselineCommit: contentModerationMinimumSecurityBaselineCommit,
		BaselineSatisfied:             satisfied,
		BaselineSatisfactionMethod:    method,
	}
	s.baselineStatusValid = true
	return s.baselineStatus
}

func (s *ContentModerationService) contentModerationBaselineSatisfiedLocked() (bool, string) {
	commit := strings.TrimSpace(s.buildInfo.Commit)
	if isUnknownContentModerationBuildCommit(commit) {
		return false, "unknown"
	}
	if isPlaceholderContentModerationBuildCommit(commit) {
		return false, "placeholder_commit"
	}
	if !isValidContentModerationBuildCommit(commit) {
		return false, "invalid_commit"
	}
	if parseContentModerationBoolEnv("MODERATION_SECURITY_BASELINE_SATISFIED") {
		if !isReleaseContentModerationBuildType(s.buildInfo.BuildType) {
			return false, "invalid_attestation"
		}
		return true, "ci_attestation"
	}
	baseline := strings.TrimSpace(contentModerationMinimumSecurityBaselineCommit)
	if baseline == "" {
		return true, "not_required"
	}
	if contentModerationCommitPrefixMatches(commit, baseline) {
		return true, "commit_prefix"
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", baseline, commit)
	if err := cmd.Run(); err == nil {
		return true, "git_ancestry"
	}
	return false, "git_ancestry"
}

func isUnknownContentModerationBuildCommit(commit string) bool {
	switch strings.ToLower(strings.TrimSpace(commit)) {
	case "", "unknown":
		return true
	default:
		return false
	}
}

func isPlaceholderContentModerationBuildCommit(commit string) bool {
	switch strings.ToLower(strings.TrimSpace(commit)) {
	case "docker", "dev", "local":
		return true
	default:
		return false
	}
}

func isValidContentModerationBuildCommit(commit string) bool {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if len(commit) < minContentModerationBuildCommitPrefixLen || len(commit) > 40 {
		return false
	}
	for _, r := range commit {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func isReleaseContentModerationBuildType(buildType string) bool {
	switch strings.ToLower(strings.TrimSpace(buildType)) {
	case "release", "production":
		return true
	default:
		return false
	}
}

func contentModerationCommitPrefixMatches(commit string, baseline string) bool {
	commit = strings.ToLower(strings.TrimSpace(commit))
	baseline = strings.ToLower(strings.TrimSpace(baseline))
	if len(commit) < minContentModerationBuildCommitPrefixLen || len(baseline) < minContentModerationBuildCommitPrefixLen {
		return false
	}
	return strings.HasPrefix(commit, baseline) || strings.HasPrefix(baseline, commit)
}

func parseContentModerationBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func contentModerationRouteCoverageStatus() ContentModerationRouteCoverageStatus {
	return moderationcoverage.CoverageStatus(contentModerationRouteManifestVersion)
}

func contentModerationRouteCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationRouteCoverageStatus {
	return moderationcoverage.CoverageStatusFromEntries(contentModerationRouteManifestVersion, entries)
}

func contentModerationRouteCoverageHashFromEntries(entries []contentModerationRouteCoverageEntry) string {
	return moderationcoverage.HashFromEntries(entries)
}

func contentModerationPipelineCoverageStatus() ContentModerationPipelineCoverageStatus {
	return contentModerationPipelineCoverageStatusFromEntries(moderationcoverage.Entries())
}

func contentModerationPipelineCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationPipelineCoverageStatus {
	global := contentModerationGlobalPipelineCoverageStatusFromEntries(entries)
	openAIHTTP := contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.PipelineOpenAIHTTPVersion,
		contentModerationIsOpenAIHTTPPipelineRoute,
		moderationcoverage.OpenAIHTTPPipelineStagesForRoute,
	)
	openAIWebSocket := contentModerationOpenAIWebSocketPipelineCoverageStatusFromEntries(entries)
	gatewayPreForward := contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.PipelineGatewayPreForwardVersion,
		contentModerationIsGatewayPreForwardPipelineRoute,
		moderationcoverage.GatewayPreForwardPipelineStagesForRoute,
	)
	status := "covered"
	requiredRoutes := global.RequiredRoutes
	if requiredRoutes == 0 {
		status = "unknown"
	} else if global.CoveredRoutes != global.RequiredRoutes || len(global.UncoveredRoutes) > 0 ||
		openAIHTTP.CoveredRoutes != openAIHTTP.RequiredRoutes || len(openAIHTTP.UncoveredRoutes) > 0 ||
		openAIWebSocket.CoveredRoutes != openAIWebSocket.RequiredRoutes || len(openAIWebSocket.UncoveredRoutes) > 0 ||
		gatewayPreForward.CoveredRoutes != gatewayPreForward.RequiredRoutes || len(gatewayPreForward.UncoveredRoutes) > 0 {
		status = "mismatch"
	}
	return ContentModerationPipelineCoverageStatus{
		ManifestVersion:   contentModerationRouteManifestVersion,
		Version:           contentModerationPipelineCoverageVersion,
		ManifestHash:      contentModerationPipelineCoverageHashFromEntries(entries),
		Status:            status,
		Global:            global,
		OpenAIHTTP:        openAIHTTP,
		OpenAIWebSocket:   openAIWebSocket,
		GatewayPreForward: gatewayPreForward,
	}
}

func contentModerationGlobalPipelineCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationGlobalPipelineCoverageStatus {
	return contentModerationPipelineGroupCoverageStatusFromEntriesWithPipelineValidator(
		entries,
		moderationcoverage.PipelineGatewayGlobal,
		moderationcoverage.PipelineGatewayGlobalVersion,
		contentModerationIsGlobalPipelineRoute,
		contentModerationGlobalPipelineStagesForRoute,
		contentModerationGlobalPipelineAcceptsRoutePipeline,
	)
}

func contentModerationOpenAIHTTPPipelineCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationOpenAIHTTPPipelineCoverageStatus {
	return contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.PipelineOpenAIHTTPVersion,
		contentModerationIsOpenAIHTTPPipelineRoute,
		moderationcoverage.OpenAIHTTPPipelineStagesForRoute,
	)
}

func contentModerationOpenAIWebSocketPipelineCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationOpenAIWebSocketPipelineCoverageStatus {
	summary := contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.PipelineOpenAIWebSocketVersion,
		contentModerationIsOpenAIWebSocketPipelineRoute,
		moderationcoverage.OpenAIWebSocketPipelineStagesForRoute,
	)
	responses := contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.PipelineOpenAIWebSocketVersion,
		contentModerationIsOpenAIResponsesWebSocketPipelineRoute,
		moderationcoverage.OpenAIWebSocketPipelineStagesForRoute,
	)
	realtime := contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.PipelineOpenAIWebSocketVersion,
		contentModerationIsOpenAIRealtimeWebSocketPipelineRoute,
		moderationcoverage.OpenAIWebSocketPipelineStagesForRoute,
	)
	return ContentModerationOpenAIWebSocketPipelineCoverageStatus{
		Version:         summary.Version,
		Pipeline:        summary.Pipeline,
		Status:          summary.Status,
		RequiredRoutes:  summary.RequiredRoutes,
		CoveredRoutes:   summary.CoveredRoutes,
		UncoveredRoutes: summary.UncoveredRoutes,
		StageCoverage:   summary.StageCoverage,
		Routes:          summary.Routes,
		Responses:       responses,
		Realtime:        realtime,
	}
}

func contentModerationGatewayPreForwardPipelineCoverageStatusFromEntries(entries []contentModerationRouteCoverageEntry) ContentModerationGatewayPreForwardPipelineCoverageStatus {
	return contentModerationPipelineGroupCoverageStatusFromEntries(
		entries,
		moderationcoverage.PipelineGatewayPreForward,
		moderationcoverage.PipelineGatewayPreForwardVersion,
		contentModerationIsGatewayPreForwardPipelineRoute,
		moderationcoverage.GatewayPreForwardPipelineStagesForRoute,
	)
}

func contentModerationPipelineGroupCoverageStatusFromEntries(
	entries []contentModerationRouteCoverageEntry,
	pipeline string,
	version string,
	include func(contentModerationRouteCoverageEntry) bool,
	expectedStagesForRoute func(handlerName, protocol string) []moderationcoverage.PipelineStageCoverage,
) ContentModerationPipelineGroupCoverageStatus {
	return contentModerationPipelineGroupCoverageStatusFromEntriesWithPipelineValidator(
		entries,
		pipeline,
		version,
		include,
		expectedStagesForRoute,
		func(routePipeline string) bool {
			return moderationcoverage.NormalizePipeline(routePipeline) == moderationcoverage.NormalizePipeline(pipeline)
		},
	)
}

func contentModerationPipelineGroupCoverageStatusFromEntriesWithPipelineValidator(
	entries []contentModerationRouteCoverageEntry,
	pipeline string,
	version string,
	include func(contentModerationRouteCoverageEntry) bool,
	expectedStagesForRoute func(handlerName, protocol string) []moderationcoverage.PipelineStageCoverage,
	pipelineValid func(routePipeline string) bool,
) ContentModerationPipelineGroupCoverageStatus {
	routes := make([]ContentModerationPipelineRouteCoverageStatus, 0)
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if include == nil || !include(entry) {
			continue
		}
		routes = append(routes, contentModerationPipelineRouteCoverageStatusFromEntry(entry, pipelineValid, expectedStagesForRoute))
	}
	sort.Slice(routes, func(i, j int) bool {
		left := contentModerationPipelineRouteKey(routes[i].Method, routes[i].Path, routes[i].Handler)
		right := contentModerationPipelineRouteKey(routes[j].Method, routes[j].Path, routes[j].Handler)
		if left == right {
			return routes[i].Protocol < routes[j].Protocol
		}
		return left < right
	})

	coveredRoutes := 0
	uncoveredRoutes := make([]string, 0)
	for _, route := range routes {
		if route.Covered {
			coveredRoutes++
			continue
		}
		uncoveredRoutes = append(uncoveredRoutes, contentModerationPipelineRouteKey(route.Method, route.Path, route.Handler))
	}
	status := "covered"
	if len(routes) == 0 {
		status = "not_applicable"
	} else if coveredRoutes != len(routes) || len(uncoveredRoutes) > 0 {
		status = "mismatch"
	}

	return ContentModerationPipelineGroupCoverageStatus{
		Version:         version,
		Pipeline:        moderationcoverage.NormalizePipeline(pipeline),
		Status:          status,
		RequiredRoutes:  len(routes),
		CoveredRoutes:   coveredRoutes,
		UncoveredRoutes: uncoveredRoutes,
		StageCoverage:   contentModerationPipelineStageCoverageStatusFromRoutes(routes),
		Routes:          routes,
	}
}

func contentModerationPipelineRouteCoverageStatusFromEntry(
	entry contentModerationRouteCoverageEntry,
	pipelineValid func(routePipeline string) bool,
	expectedStagesForRoute func(handlerName, protocol string) []moderationcoverage.PipelineStageCoverage,
) ContentModerationPipelineRouteCoverageStatus {
	stagesByName := make(map[string]ContentModerationPipelineRouteStageCoverageStatus, len(entry.StageCoverage))
	uncoveredStages := make([]string, 0)
	covered := normalizeContentModerationRouteCoverageStatus(entry.Status) == moderationcoverage.StatusCovered
	if pipelineValid == nil || !pipelineValid(entry.Pipeline) {
		covered = false
		uncoveredStages = append(uncoveredStages, "pipeline_metadata")
	}
	for _, stage := range entry.StageCoverage {
		stageName := moderationcoverage.NormalizeStage(stage.Stage)
		if stageName == "" {
			continue
		}
		stagesByName[stageName] = ContentModerationPipelineRouteStageCoverageStatus{
			Stage:    stageName,
			Required: stage.Required,
			Covered:  stage.Covered,
		}
		if stage.Required && !stage.Covered {
			covered = false
			uncoveredStages = append(uncoveredStages, stageName)
		}
	}
	var expectedStages []moderationcoverage.PipelineStageCoverage
	if expectedStagesForRoute != nil {
		expectedStages = expectedStagesForRoute(entry.Handler, entry.Protocol)
	}
	for _, expected := range expectedStages {
		stageName := moderationcoverage.NormalizeStage(expected.Stage)
		if stageName == "" {
			continue
		}
		actual, ok := stagesByName[stageName]
		if !ok {
			actual = ContentModerationPipelineRouteStageCoverageStatus{
				Stage:    stageName,
				Required: expected.Required,
				Covered:  false,
			}
			stagesByName[stageName] = actual
		}
		if expected.Required && (!actual.Required || !actual.Covered) {
			covered = false
			uncoveredStages = append(uncoveredStages, stageName)
		}
	}
	if len(stagesByName) == 0 {
		covered = false
		if len(uncoveredStages) == 0 {
			uncoveredStages = append(uncoveredStages, "pipeline_metadata")
		}
	}
	stages := make([]ContentModerationPipelineRouteStageCoverageStatus, 0, len(stagesByName))
	for _, stage := range stagesByName {
		stages = append(stages, stage)
	}
	sort.Slice(stages, func(i, j int) bool {
		return contentModerationPipelineStageSortKey(stages[i].Stage) < contentModerationPipelineStageSortKey(stages[j].Stage)
	})
	uncoveredStages = uniqueSortedContentModerationPipelineStages(uncoveredStages)
	return ContentModerationPipelineRouteCoverageStatus{
		Method:                    normalizeContentModerationRouteCoverageMethod(entry.Method),
		Path:                      normalizeContentModerationRouteCoveragePath(entry.Path),
		Handler:                   strings.TrimSpace(entry.Handler),
		Protocol:                  strings.TrimSpace(entry.Protocol),
		Pipeline:                  moderationcoverage.NormalizePipeline(entry.Pipeline),
		Covered:                   covered,
		ForwardAdapters:           contentModerationForwardAdaptersForEntry(entry),
		ForwardAdapterDescriptors: contentModerationForwardAdapterDescriptorsForEntry(entry),
		StageAdapterDescriptors:   contentModerationStageAdapterDescriptorsForEntry(entry),
		UncoveredStages:           uncoveredStages,
		Stages:                    stages,
	}
}

func contentModerationStageAdapterDescriptorsForEntry(entry contentModerationRouteCoverageEntry) []moderationcoverage.RouteAdapterDescriptor {
	descriptors := moderationcoverage.NormalizeRouteAdapterDescriptors(entry.StageAdapterDescriptors)
	if len(descriptors) > 0 {
		return descriptors
	}
	return moderationcoverage.StageAdapterDescriptorsForRoute(entry.Handler, entry.Protocol)
}

func contentModerationForwardAdapterDescriptorsForEntry(entry contentModerationRouteCoverageEntry) []moderationcoverage.RouteAdapterDescriptor {
	stageDescriptors := contentModerationStageAdapterDescriptorsForEntry(entry)
	if len(stageDescriptors) == 0 {
		return nil
	}
	out := make([]moderationcoverage.RouteAdapterDescriptor, 0, len(stageDescriptors))
	for _, descriptor := range stageDescriptors {
		if moderationcoverage.NormalizeStage(descriptor.Stage) == moderationcoverage.StageForward {
			out = append(out, descriptor)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func contentModerationForwardAdaptersForEntry(entry contentModerationRouteCoverageEntry) []string {
	descriptors := contentModerationForwardAdapterDescriptorsForEntry(entry)
	if len(descriptors) == 0 {
		return nil
	}
	out := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if strings.TrimSpace(descriptor.Name) == "" {
			continue
		}
		out = append(out, strings.TrimSpace(descriptor.Name))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func uniqueSortedContentModerationPipelineStages(stages []string) []string {
	seen := make(map[string]struct{}, len(stages))
	out := make([]string, 0, len(stages))
	for _, stage := range stages {
		stage = moderationcoverage.NormalizeStage(stage)
		if stage == "" {
			continue
		}
		if _, ok := seen[stage]; ok {
			continue
		}
		seen[stage] = struct{}{}
		out = append(out, stage)
	}
	sort.Slice(out, func(i, j int) bool {
		return contentModerationPipelineStageSortKey(out[i]) < contentModerationPipelineStageSortKey(out[j])
	})
	return out
}

func contentModerationPipelineStageCoverageStatusFromRoutes(routes []ContentModerationPipelineRouteCoverageStatus) []ContentModerationPipelineStageCoverageStatus {
	byStage := make(map[string]*ContentModerationPipelineStageCoverageStatus)
	for _, route := range routes {
		routeKey := contentModerationPipelineRouteKey(route.Method, route.Path, route.Handler)
		for _, stage := range route.Stages {
			if !stage.Required {
				continue
			}
			stageName := moderationcoverage.NormalizeStage(stage.Stage)
			if stageName == "" {
				continue
			}
			summary := byStage[stageName]
			if summary == nil {
				summary = &ContentModerationPipelineStageCoverageStatus{Stage: stageName}
				byStage[stageName] = summary
			}
			summary.RequiredRoutes++
			if stage.Covered {
				summary.CoveredRoutes++
			} else {
				summary.UncoveredRoutes = append(summary.UncoveredRoutes, routeKey)
			}
		}
	}

	stages := make([]string, 0, len(byStage))
	for stage := range byStage {
		stages = append(stages, stage)
	}
	sort.Slice(stages, func(i, j int) bool {
		return contentModerationPipelineStageSortKey(stages[i]) < contentModerationPipelineStageSortKey(stages[j])
	})

	out := make([]ContentModerationPipelineStageCoverageStatus, 0, len(stages))
	for _, stage := range stages {
		summary := *byStage[stage]
		if summary.UncoveredRoutes == nil {
			summary.UncoveredRoutes = []string{}
		}
		sort.Strings(summary.UncoveredRoutes)
		out = append(out, summary)
	}
	return out
}

func contentModerationPipelineCoverageHashFromEntries(entries []contentModerationRouteCoverageEntry) string {
	parts := make([]string, 0)
	for _, entry := range entries {
		entry = moderationcoverage.NormalizeEntry(entry)
		if !contentModerationIsOpenAIHTTPPipelineRoute(entry) &&
			!contentModerationIsOpenAIWebSocketPipelineRoute(entry) &&
			!contentModerationIsGatewayPreForwardPipelineRoute(entry) {
			continue
		}
		if len(entry.StageCoverage) == 0 {
			parts = append(parts, strings.Join([]string{
				entry.Pipeline,
				normalizeContentModerationRouteCoverageMethod(entry.Method),
				normalizeContentModerationRouteCoveragePath(entry.Path),
				strings.TrimSpace(entry.Handler),
				strings.TrimSpace(entry.Protocol),
				normalizeContentModerationRouteCoverageStatus(entry.Status),
				"pipeline_metadata",
				"required",
				"missing",
			}, " "))
			continue
		}
		for _, stage := range entry.StageCoverage {
			parts = append(parts, strings.Join([]string{
				entry.Pipeline,
				normalizeContentModerationRouteCoverageMethod(entry.Method),
				normalizeContentModerationRouteCoveragePath(entry.Path),
				strings.TrimSpace(entry.Handler),
				strings.TrimSpace(entry.Protocol),
				normalizeContentModerationRouteCoverageStatus(entry.Status),
				moderationcoverage.NormalizeStage(stage.Stage),
				fmt.Sprintf("required=%t", stage.Required),
				fmt.Sprintf("covered=%t", stage.Covered),
			}, " "))
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

func contentModerationIsOpenAIHTTPPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	if !entry.Upstream || !entry.ModerationRequired {
		return false
	}
	if normalizeContentModerationRouteCoverageMethod(entry.Method) != http.MethodPost {
		return false
	}
	return len(moderationcoverage.OpenAIHTTPPipelineStagesForRoute(entry.Handler, entry.Protocol)) > 0
}

func contentModerationIsGlobalPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	if !entry.Upstream || !entry.ModerationRequired {
		return false
	}
	return len(contentModerationGlobalPipelineStagesForRoute(entry.Handler, entry.Protocol)) > 0
}

func contentModerationGlobalPipelineStagesForRoute(handlerName, protocol string) []moderationcoverage.PipelineStageCoverage {
	if stages := moderationcoverage.OpenAIHTTPPipelineStagesForRoute(handlerName, protocol); len(stages) > 0 {
		return stages
	}
	if stages := moderationcoverage.OpenAIWebSocketPipelineStagesForRoute(handlerName, protocol); len(stages) > 0 {
		return stages
	}
	return moderationcoverage.GatewayPreForwardPipelineStagesForRoute(handlerName, protocol)
}

func contentModerationGlobalPipelineAcceptsRoutePipeline(routePipeline string) bool {
	switch moderationcoverage.NormalizePipeline(routePipeline) {
	case moderationcoverage.PipelineOpenAIHTTP,
		moderationcoverage.PipelineOpenAIWebSocket,
		moderationcoverage.PipelineGatewayPreForward:
		return true
	default:
		return false
	}
}

func contentModerationIsOpenAIWebSocketPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	return contentModerationIsOpenAIResponsesWebSocketPipelineRoute(entry) ||
		contentModerationIsOpenAIRealtimeWebSocketPipelineRoute(entry)
}

func contentModerationIsOpenAIResponsesWebSocketPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	if !entry.Upstream || !entry.ModerationRequired {
		return false
	}
	if normalizeContentModerationRouteCoverageMethod(entry.Method) != http.MethodGet {
		return false
	}
	if strings.TrimSpace(entry.Protocol) != ContentModerationProtocolOpenAIResponses {
		return false
	}
	return strings.TrimSpace(entry.Handler) == "OpenAIGatewayHandler.ResponsesWebSocket"
}

func contentModerationIsOpenAIRealtimeWebSocketPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	if !entry.Upstream || !entry.ModerationRequired {
		return false
	}
	if normalizeContentModerationRouteCoverageMethod(entry.Method) != http.MethodGet {
		return false
	}
	handlerName := strings.TrimSpace(entry.Handler)
	if handlerName != "OpenAIGatewayHandler.RealtimeWebSocket" &&
		handlerName != "OpenAIGatewayHandler.Realtime" {
		return false
	}
	path := strings.TrimSpace(entry.Path)
	return path == "/v1/realtime" ||
		path == "/realtime" ||
		strings.HasPrefix(path, "/v1/realtime/") ||
		strings.HasPrefix(path, "/realtime/")
}

func contentModerationIsGatewayPreForwardPipelineRoute(entry contentModerationRouteCoverageEntry) bool {
	if !entry.Upstream || !entry.ModerationRequired {
		return false
	}
	if normalizeContentModerationRouteCoverageMethod(entry.Method) != http.MethodPost {
		return false
	}
	return len(moderationcoverage.GatewayPreForwardPipelineStagesForRoute(entry.Handler, entry.Protocol)) > 0
}

func contentModerationPipelineRouteKey(method, path, handler string) string {
	key := strings.TrimSpace(normalizeContentModerationRouteCoverageMethod(method) + " " + normalizeContentModerationRouteCoveragePath(path))
	if handler = strings.TrimSpace(handler); handler != "" {
		key += " " + handler
	}
	return key
}

func contentModerationPipelineStageSortKey(stage string) string {
	return moderationcoverage.PipelineStageSortKey(stage)
}

func normalizeContentModerationRouteCoverageMethod(value string) string {
	return moderationcoverage.NormalizeMethod(value)
}

func normalizeContentModerationRouteCoveragePath(value string) string {
	return moderationcoverage.NormalizePath(value)
}

func normalizeContentModerationRouteCoverageStatus(value string) string {
	return moderationcoverage.NormalizeStatus(value)
}

func (s *ContentModerationService) buildContentModerationEffectiveProtectionStatus(cfg *ContentModerationConfig, riskEnabled bool, routeCoverage ContentModerationRouteCoverageStatus, pipelineCoverage ContentModerationPipelineCoverageStatus, flaggedHashCount int64) ContentModerationEffectiveProtectionStatus {
	if cfg == nil {
		cfg = defaultContentModerationConfig()
	} else {
		cfg = cloneContentModerationConfig(cfg)
	}
	cfg.normalize()

	failStrategy := normalizeContentModerationFailStrategy(cfg.FailStrategy)
	modelFilter := normalizeContentModerationModelFilter(cfg.ModelFilter)
	groupCoverage := "all_public_groups"
	if !cfg.AllGroups {
		groupCoverage = "scoped_groups"
	}
	modelCoverage := modelFilter.Type
	externalAPIConfigured := len(cfg.apiKeys()) > 0
	externalAPIHealth := s.contentModerationExternalAPIHealth(cfg)
	externalAPIHealthy := externalAPIConfigured && externalAPIHealth.healthy
	externalAPIRequiredForStrongProtection := cfg.EngineMode == ContentModerationEngineModeAPIOnly
	highRiskRulesBlocking, highRiskRulesPresent := contentModerationHighRiskRulesBlocking(cfg.keywordRules())
	hashBlockingPolicyPresent := contentModerationHashBlockingPolicyPresent(cfg, flaggedHashCount)
	deterministicPolicyPresent := contentModerationDeterministicPolicyPresent(cfg) || hashBlockingPolicyPresent
	baselineStatus := s.contentModerationSecurityBaselineStatus()
	buildCommit := strings.TrimSpace(s.buildInfo.Commit)
	attestationRequested := parseContentModerationBoolEnv("MODERATION_SECURITY_BASELINE_SATISFIED")

	unsafeReasons := make([]string, 0, 16)
	if isUnknownContentModerationBuildCommit(buildCommit) {
		unsafeReasons = append(unsafeReasons, "build_commit_unknown")
	}
	if isPlaceholderContentModerationBuildCommit(buildCommit) {
		unsafeReasons = append(unsafeReasons, "build_commit_placeholder")
	}
	if !isUnknownContentModerationBuildCommit(buildCommit) && !isPlaceholderContentModerationBuildCommit(buildCommit) && !isValidContentModerationBuildCommit(buildCommit) {
		unsafeReasons = append(unsafeReasons, "build_commit_invalid")
	}
	if attestationRequested && (!isValidContentModerationBuildCommit(buildCommit) || !isReleaseContentModerationBuildType(s.buildInfo.BuildType)) {
		unsafeReasons = append(unsafeReasons, "build_attestation_without_valid_commit")
	}
	if !baselineStatus.BaselineSatisfied {
		unsafeReasons = append(unsafeReasons, "build_baseline_unverified", "build_below_security_baseline")
	}
	if routeCoverage.Status == "unknown" {
		unsafeReasons = append(unsafeReasons, "route_coverage_unknown")
	}
	if routeCoverage.Status == "mismatch" {
		unsafeReasons = append(unsafeReasons, "route_manifest_mismatch")
	}
	if len(routeCoverage.UncoveredRoutes) > 0 {
		unsafeReasons = append(unsafeReasons, "uncovered_upstream_routes")
	}
	if pipelineCoverage.Status == "unknown" {
		unsafeReasons = append(unsafeReasons, "pipeline_coverage_unknown")
	}
	if pipelineCoverage.Status == "mismatch" {
		unsafeReasons = append(unsafeReasons, "pipeline_coverage_mismatch")
	}
	if len(pipelineCoverage.OpenAIHTTP.UncoveredRoutes) > 0 ||
		len(pipelineCoverage.OpenAIWebSocket.UncoveredRoutes) > 0 ||
		len(pipelineCoverage.GatewayPreForward.UncoveredRoutes) > 0 {
		unsafeReasons = append(unsafeReasons, "uncovered_pipeline_routes")
	}
	if !riskEnabled {
		unsafeReasons = append(unsafeReasons, "risk_control_disabled")
	}
	if !cfg.Enabled {
		unsafeReasons = append(unsafeReasons, "moderation_disabled")
	}
	if cfg.Mode != ContentModerationModePreBlock {
		unsafeReasons = append(unsafeReasons, "mode_not_pre_block")
	}
	if cfg.AuditScope != ContentModerationAuditScopeAllContext {
		unsafeReasons = append(unsafeReasons, "audit_scope_not_all_context")
	}
	if failStrategy.Default == ContentModerationFailStrategyOpen {
		unsafeReasons = append(unsafeReasons, "public_fail_open")
	}
	if !cfg.AllGroups {
		unsafeReasons = append(unsafeReasons, "group_scope_not_all")
	}
	if modelFilter.Type != ContentModerationModelFilterAll {
		unsafeReasons = append(unsafeReasons, "model_filter_not_all")
	}
	if externalAPIRequiredForStrongProtection && !externalAPIConfigured {
		unsafeReasons = append(unsafeReasons, "external_api_not_configured")
	}
	if externalAPIRequiredForStrongProtection && externalAPIConfigured {
		if externalAPIHealth.configuredKeyCount > 0 && externalAPIHealth.frozenKeyCount == externalAPIHealth.configuredKeyCount {
			unsafeReasons = append(unsafeReasons, "external_api_all_keys_frozen")
		}
		if externalAPIHealth.usableKeyCount == 0 {
			unsafeReasons = append(unsafeReasons, "external_api_no_usable_key")
		}
		if externalAPIHealth.unknownKeyCount > 0 {
			unsafeReasons = append(unsafeReasons, "external_api_health_unknown")
		}
		if externalAPIHealth.lastError != "" {
			unsafeReasons = append(unsafeReasons, "external_api_last_test_failed")
		}
	}
	if !highRiskRulesBlocking && !hashBlockingPolicyPresent {
		unsafeReasons = append(unsafeReasons, "high_risk_rules_not_blocking")
	}
	switch cfg.EngineMode {
	case ContentModerationEngineModeRuleOnly:
		if !deterministicPolicyPresent {
			unsafeReasons = append(unsafeReasons, "rule_only_without_blocking_rules", "no_deterministic_high_risk_policy")
		}
	case ContentModerationEngineModeAPIOnly:
		if !externalAPIHealthy {
			unsafeReasons = append(unsafeReasons, "api_only_without_healthy_external_api")
		}
	}

	return ContentModerationEffectiveProtectionStatus{
		EffectiveBlocking:          len(unsafeReasons) == 0,
		RiskControlEnabled:         riskEnabled,
		ModerationEnabled:          cfg.Enabled,
		Mode:                       cfg.Mode,
		AuditScope:                 cfg.AuditScope,
		PublicFailStrategy:         failStrategy.Default,
		GroupCoverage:              groupCoverage,
		ModelCoverage:              modelCoverage,
		EngineMode:                 cfg.EngineMode,
		ExternalAPIConfigured:      externalAPIConfigured,
		ExternalAPIHealthy:         externalAPIHealthy,
		ExternalAPIUsableKeyCount:  externalAPIHealth.usableKeyCount,
		ExternalAPILastError:       externalAPIHealth.lastError,
		HighRiskRulesBlocking:      highRiskRulesBlocking,
		DeterministicPolicyPresent: deterministicPolicyPresent,
		HighRiskRulesPresent:       highRiskRulesPresent,
		UnsafeReasons:              unsafeReasons,
	}
}

type contentModerationExternalAPIHealthStatus struct {
	configuredKeyCount int
	usableKeyCount     int
	frozenKeyCount     int
	unknownKeyCount    int
	healthy            bool
	lastError          string
}

func (s *ContentModerationService) contentModerationExternalAPIHealth(cfg *ContentModerationConfig) contentModerationExternalAPIHealthStatus {
	keys := cfg.apiKeys()
	status := contentModerationExternalAPIHealthStatus{configuredKeyCount: len(keys)}
	if len(keys) == 0 {
		return status
	}
	for _, item := range s.apiKeyStatuses(keys) {
		switch item.Status {
		case "ok":
			status.usableKeyCount++
		case "frozen":
			status.frozenKeyCount++
			if status.lastError == "" {
				status.lastError = item.LastError
			}
		case "error":
			if status.lastError == "" {
				status.lastError = item.LastError
			}
		default:
			status.unknownKeyCount++
		}
	}
	status.healthy = status.usableKeyCount > 0
	return status
}

func contentModerationHighRiskRulesBlocking(rules []ContentModerationKeywordRule) (bool, bool) {
	present := false
	for _, rule := range normalizeContentModerationKeywordRules(rules) {
		if !rule.Enabled {
			continue
		}
		switch rule.Severity {
		case ContentModerationKeywordSeverityHigh, ContentModerationKeywordSeverityCritical:
			present = true
			if rule.Action != ContentModerationKeywordActionBlock {
				return false, present
			}
		}
	}
	return true, present
}

func contentModerationDeterministicPolicyPresent(cfg *ContentModerationConfig) bool {
	if cfg == nil {
		return false
	}
	if len(cfg.BlockedKeywords) > 0 {
		return true
	}
	for _, rule := range normalizeContentModerationKeywordRules(cfg.KeywordRules) {
		if !rule.Enabled || rule.Action != ContentModerationKeywordActionBlock {
			continue
		}
		switch rule.Severity {
		case ContentModerationKeywordSeverityHigh, ContentModerationKeywordSeverityCritical:
			return true
		}
	}
	return false
}

func contentModerationHashBlockingPolicyPresent(cfg *ContentModerationConfig, flaggedHashCount int64) bool {
	if cfg == nil {
		return false
	}
	return cfg.PreHashCheckEnabled && flaggedHashCount > 0
}

func (s *ContentModerationService) cleanupWorker() {
	timer := time.NewTimer(contentModerationCleanupDelay)
	defer timer.Stop()
	for {
		<-timer.C
		s.runCleanupOnce()
		timer.Reset(contentModerationCleanupInterval)
	}
}

func (s *ContentModerationService) runCleanupOnce() {
	if s == nil || s.repo == nil || s.settingRepo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationCleanupTimeout)
	defer cancel()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cleanup_load_config_failed", "error", err)
		return
	}
	now := time.Now()
	hitBefore := now.AddDate(0, 0, -cfg.HitRetentionDays)
	nonHitBefore := now.AddDate(0, 0, -cfg.NonHitRetentionDays)
	result, err := s.repo.CleanupExpiredLogs(ctx, hitBefore, nonHitBefore)
	if err != nil {
		slog.Warn("content_moderation.cleanup_failed", "error", err)
		return
	}
	if result == nil {
		return
	}
	s.lastCleanupUnix.Store(result.FinishedAt.Unix())
	s.lastCleanupDeletedHit.Store(result.DeletedHit)
	s.lastCleanupDeletedNonHit.Store(result.DeletedNonHit)
	s.cleanupContentModerationOutbox(ctx, now)
}

func (s *ContentModerationService) loadConfig(ctx context.Context) (*ContentModerationConfig, error) {
	cfg := defaultContentModerationConfig()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyContentModerationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			cfg.normalize()
			return cfg, nil
		}
		return nil, fmt.Errorf("get content moderation config: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		cfg.normalize()
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不是有效 JSON")
	}
	cfg.normalize()
	return cfg, nil
}

func (s *ContentModerationService) isRiskControlEnabled(ctx context.Context) bool {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyRiskControlEnabled)
	if err != nil {
		return false
	}
	return raw == "true"
}

func (s *ContentModerationService) validateConfig(ctx context.Context, cfg *ContentModerationConfig) error {
	if cfg == nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_CONFIG", "内容审计配置不能为空")
	}
	cfg.normalize()
	switch cfg.Mode {
	case ContentModerationModeOff, ContentModerationModeObserve, ContentModerationModePreBlock:
	default:
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODE", "内容审计模式无效")
	}
	if _, err := url.ParseRequestURI(cfg.BaseURL); err != nil {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BASE_URL", "OpenAI Base URL 无效")
	}
	if cfg.LocalClassifier.Enabled {
		if strings.TrimSpace(cfg.LocalClassifier.URL) == "" {
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_LOCAL_CLASSIFIER_URL", "本地分类器 URL 不能为空")
		}
		if _, err := url.ParseRequestURI(cfg.LocalClassifier.URL); err != nil {
			return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_LOCAL_CLASSIFIER_URL", "本地分类器 URL 无效")
		}
	}
	if cfg.BlockStatus < 400 || cfg.BlockStatus > 599 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_BLOCK_STATUS", "拦截 HTTP 状态码必须在 400-599 之间")
	}
	if cfg.ModelFilter.Type != ContentModerationModelFilterAll && len(cfg.ModelFilter.Models) == 0 {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_MODEL_FILTER", "指定或排除模型时至少需要配置 1 个模型")
	}
	if !cfg.AllGroups && len(cfg.GroupIDs) > 0 && s.groupRepo != nil {
		for _, groupID := range cfg.GroupIDs {
			if _, err := s.groupRepo.GetByIDLite(ctx, groupID); err != nil {
				return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GROUP", fmt.Sprintf("审计分组不存在: %d", groupID))
			}
		}
	}
	return nil
}

func (s *ContentModerationService) callModeration(ctx context.Context, cfg *ContentModerationConfig, input any, trackKeyLoad ...bool) (*moderationAPIResult, error) {
	attempts := cfg.RetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > maxContentModerationRetryCount+1 {
		attempts = maxContentModerationRetryCount + 1
	}
	trackLoad := len(trackKeyLoad) > 0 && trackKeyLoad[0]
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			lastErr = errors.New("no moderation api key available")
			break
		}
		if trackLoad {
			s.beginModerationAPIKeyCall(key)
		}
		start := time.Now()
		httpStatus := 0
		result, err := s.callModerationOnceWithInput(ctx, cfg, key, input, &httpStatus)
		latency := int(time.Since(start).Milliseconds())
		if err == nil {
			if trackLoad {
				s.finishModerationAPIKeyCall(key, latency, true)
			}
			s.markAPIKeySuccess(key, latency, httpStatus)
			return result, nil
		}
		if trackLoad {
			s.finishModerationAPIKeyCall(key, latency, false)
		}
		s.markAPIKeyError(key, err.Error(), latency, httpStatus)
		lastErr = err
		if httpStatus == http.StatusBadRequest {
			break
		}
		if attempt == attempts-1 {
			break
		}
		wait := time.Duration(100*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, lastErr
}

func (s *ContentModerationService) callModerationOnceWithInput(ctx context.Context, cfg *ContentModerationConfig, apiKey string, input any, httpStatus *int) (*moderationAPIResult, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := moderationAPIRequest{
		Model: cfg.Model,
		Input: input,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if httpStatus != nil {
		*httpStatus = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moderation api status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out moderationAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, errors.New("moderation api returned empty results")
	}
	return &out.Results[0], nil
}

func (s *ContentModerationService) buildLog(input ContentModerationCheckInput, cfg *ContentModerationConfig, action string, flagged bool, highestCategory string, highestScore float64, scores map[string]float64, text string, latency *int, queueDelay *int, errText string) *ContentModerationLog {
	var userID *int64
	if input.UserID > 0 {
		userID = &input.UserID
	}
	var apiKeyID *int64
	if input.APIKeyID > 0 {
		apiKeyID = &input.APIKeyID
	}
	return &ContentModerationLog{
		DecisionID:        contentModerationDecisionID(input, nil, ""),
		RequestID:         input.RequestID,
		UserID:            userID,
		UserEmail:         input.UserEmail,
		APIKeyID:          apiKeyID,
		APIKeyName:        input.APIKeyName,
		GroupID:           cloneInt64Ptr(input.GroupID),
		GroupName:         input.GroupName,
		Endpoint:          input.Endpoint,
		Provider:          input.Provider,
		Model:             input.Model,
		Mode:              cfg.Mode,
		Action:            action,
		Flagged:           flagged,
		HighestCategory:   highestCategory,
		HighestScore:      highestScore,
		CategoryScores:    cloneFloatMap(scores),
		ThresholdSnapshot: cloneFloatMap(cfg.Thresholds),
		InputExcerpt:      contentModerationInputExcerptForLog(cfg, text),
		UpstreamLatencyMS: latency,
		QueueDelayMS:      queueDelay,
		Error:             errText,
	}
}

func contentModerationInputExcerptForLog(cfg *ContentModerationConfig, text string) string {
	if cfg != nil && !cfg.StoreInputExcerpt {
		return ""
	}
	return trimRunes(redactContentModerationSecrets(text), maxModerationExcerptRunes)
}

func contentModerationKeywordHitExcerptFromText(text string, keyword string) (string, bool) {
	text = strings.TrimSpace(text)
	keyword = strings.TrimSpace(keyword)
	if text == "" || keyword == "" {
		return "", false
	}
	if start, end, ok := findDisplayKeywordSpanWithBoundary(text, keyword); ok {
		return contentModerationExcerptAroundByteSpan(text, start, end, maxModerationExcerptRunes), true
	}
	normalizedText, start, end, ok := findContentModerationKeywordComparableSpan(text, keyword)
	if !ok {
		return "", false
	}
	return contentModerationExcerptAroundByteSpan(normalizedText, start, end, maxModerationExcerptRunes), true
}

func findDisplayKeywordSpanWithBoundary(text string, keyword string) (int, int, bool) {
	if start, end, ok := findExactDisplayKeywordSpanWithBoundary(text, keyword); ok {
		return start, end, true
	}
	if !isASCIIString(keyword) {
		return 0, 0, false
	}
	keywordLen := len(keyword)
	if keywordLen == 0 || len(text) < keywordLen {
		return 0, 0, false
	}
	for start := 0; start <= len(text)-keywordLen; start++ {
		end := start + keywordLen
		if asciiEqualFold(text[start:end], keyword) &&
			keywordComparableStartBoundaryAt(text, start) &&
			keywordComparableEndBoundaryAt(text, end) {
			return start, end, true
		}
	}
	return 0, 0, false
}

func findExactDisplayKeywordSpanWithBoundary(text string, keyword string) (int, int, bool) {
	start := 0
	for {
		idx := strings.Index(text[start:], keyword)
		if idx < 0 {
			return 0, 0, false
		}
		absoluteIdx := start + idx
		endIdx := absoluteIdx + len(keyword)
		if keywordComparableStartBoundaryAt(text, absoluteIdx) && keywordComparableEndBoundaryAt(text, endIdx) {
			return absoluteIdx, endIdx, true
		}
		start = absoluteIdx + 1
	}
}

func contentModerationExcerptAroundByteSpan(text string, startByte int, endByte int, maxRunes int) string {
	if maxRunes <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	startRune := byteOffsetToRuneIndex(text, startByte)
	endRune := byteOffsetToRuneIndex(text, endByte)
	if startRune < 0 {
		startRune = 0
	}
	if startRune > len(runes) {
		startRune = len(runes)
	}
	if endRune <= startRune {
		endRune = startRune + 1
	}
	if endRune > len(runes) {
		endRune = len(runes)
	}

	markerRunes := 0
	if startRune > 0 {
		markerRunes += 3
	}
	if endRune < len(runes) {
		markerRunes += 3
	}
	available := maxRunes - markerRunes
	if available <= 0 {
		available = maxRunes
	}
	spanRunes := endRune - startRune
	windowStart := startRune
	windowEnd := endRune
	if spanRunes >= available {
		windowEnd = windowStart + available
		if windowEnd > len(runes) {
			windowEnd = len(runes)
			windowStart = windowEnd - available
			if windowStart < 0 {
				windowStart = 0
			}
		}
	} else {
		before := (available - spanRunes) / 2
		windowStart = startRune - before
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd = windowStart + available
		if windowEnd < endRune {
			windowEnd = endRune
			windowStart = windowEnd - available
			if windowStart < 0 {
				windowStart = 0
			}
		}
		if windowEnd > len(runes) {
			windowEnd = len(runes)
			windowStart = windowEnd - available
			if windowStart < 0 {
				windowStart = 0
			}
		}
	}

	var builder strings.Builder
	if windowStart > 0 {
		builder.WriteString("...")
	}
	builder.WriteString(string(runes[windowStart:windowEnd]))
	if windowEnd < len(runes) {
		builder.WriteString("...")
	}
	return builder.String()
}

func byteOffsetToRuneIndex(text string, offset int) int {
	if offset <= 0 {
		return 0
	}
	count := 0
	for idx := range text {
		if idx >= offset {
			return count
		}
		count++
	}
	return count
}

func isASCIIString(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func asciiEqualFold(a string, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		left := asciiToLower(a[i])
		right := asciiToLower(b[i])
		if left != right {
			return false
		}
	}
	return true
}

func asciiToLower(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}

func (s *ContentModerationService) persistContentModerationLog(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, hashText string, recordHash bool, applySideEffects bool) {
	if s == nil || log == nil {
		return
	}
	if recordHash && s.hashCache != nil {
		if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
			slog.Warn("content_moderation.record_hash_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "error", err)
		}
	}
	autoBanJustApplied := false
	if applySideEffects {
		autoBanJustApplied = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
		s.sendFlaggedNotificationSideEffects(ctx, cfg, log, autoBanJustApplied)
	}
	if log.persisted && s.repo != nil && strings.TrimSpace(log.DecisionID) != "" {
		if log.ViolationCount > 0 || log.AutoBanned {
			if err := s.repo.UpdateLogAccountActionByDecisionID(ctx, log.DecisionID, log.ViolationCount, log.AutoBanned); err != nil {
				slog.Warn("content_moderation.update_persisted_log_account_action_failed", "decision_id", log.DecisionID, "error", err)
			}
		}
		if log.EmailSent {
			if err := s.repo.UpdateLogEmailSentByDecisionID(ctx, log.DecisionID, true); err != nil {
				slog.Warn("content_moderation.update_persisted_log_email_failed", "decision_id", log.DecisionID, "error", err)
			}
		}
	}
	if s.repo != nil && !log.persisted {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.create_log_failed", "user_id", contentModerationEmailUserID(log), "endpoint", log.Endpoint, "action", log.Action, "error", err)
			return
		}
		log.persisted = true
	}
}

func contentModerationHitLogMetadata(cfg *ContentModerationConfig, content ContentModerationInput, matchedSource string) string {
	metadata := map[string]any{}
	if cfg != nil {
		metadata["engine_mode"] = cfg.EngineMode
		metadata["keyword_blocking_mode"] = cfg.KeywordBlockingMode
	}
	if strings.TrimSpace(matchedSource) != "" {
		metadata["matched_source"] = matchedSource
	}
	if content.Truncated {
		metadata["truncated"] = true
		if len(content.TruncateReasons) > 0 {
			metadata["truncate_reasons"] = content.TruncateReasons
		}
	}
	if len(metadata) == 0 {
		return ""
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(raw)
}

func normalizeContentModerationTruncateReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	out := make([]string, 0, len(reasons))
	seen := make(map[string]struct{}, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			continue
		}
		if _, ok := seen[reason]; ok {
			continue
		}
		seen[reason] = struct{}{}
		out = append(out, reason)
	}
	return out
}

func normalizeContentModerationInputSources(sources []ContentModerationInputSource) []ContentModerationInputSource {
	if len(sources) == 0 {
		return nil
	}
	out := make([]ContentModerationInputSource, 0, len(sources))
	for _, source := range sources {
		name := strings.TrimSpace(source.Source)
		text := trimRunes(normalizeContentModerationText(source.Text), maxModerationInputRunes)
		if name == "" || text == "" {
			continue
		}
		out = append(out, ContentModerationInputSource{Source: name, Text: text})
	}
	return out
}

func contentModerationMatchedSource(protocol string, keyword string, content ContentModerationInput) string {
	if strings.TrimSpace(keyword) == "" || strings.TrimSpace(content.Text) == "" {
		return ""
	}
	for _, source := range content.Sources {
		if _, hit := matchContentModerationKeyword(source.Text, []ContentModerationKeywordRule{{
			Keyword:  keyword,
			Category: ContentModerationKeywordCategoryCustom,
			Severity: ContentModerationKeywordSeverityHigh,
			Action:   ContentModerationKeywordActionBlock,
			Enabled:  true,
		}}); hit {
			return source.Source
		}
	}
	return contentModerationPrimarySource(protocol, content)
}

func contentModerationPrimarySource(protocol string, content ContentModerationInput) string {
	if len(content.Sources) > 0 {
		return content.Sources[0].Source
	}
	switch protocol {
	case ContentModerationProtocolOpenAIChat:
		return "openai_chat.messages.content"
	case ContentModerationProtocolOpenAIResponses:
		return "responses.input.content"
	case ContentModerationProtocolOpenAIMessages:
		return "openai_messages.content"
	case ContentModerationProtocolAnthropicMessages:
		return "anthropic.messages.content"
	case ContentModerationProtocolGemini:
		return "gemini.contents.parts"
	case ContentModerationProtocolOpenAIImages:
		return "image.prompt"
	case ContentModerationProtocolOpenAIEmbeddings:
		return "openai_embeddings.input"
	default:
		return "client_supplied_model_context"
	}
}

func (s *ContentModerationService) applyFlaggedAccountSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) bool {
	if s == nil || cfg == nil || log == nil || !log.Flagged || log.UserID == nil || *log.UserID <= 0 {
		return false
	}
	count := 1
	if s.repo != nil && cfg.ViolationWindowHours > 0 {
		since := time.Now().Add(-time.Duration(cfg.ViolationWindowHours) * time.Hour)
		if n, err := s.repo.CountFlaggedByUserSince(ctx, *log.UserID, since, cfg.CyberPolicyExcludeFromBanCount); err == nil {
			count = n
			if log.ID == 0 {
				count++
			}
			if count <= 0 {
				count = 1
			}
		}
	}
	log.ViolationCount = count
	autoBanJustApplied := false
	if cfg.AutoBanEnabled && cfg.BanThreshold > 0 && count >= cfg.BanThreshold && s.userRepo != nil {
		user, err := s.userRepo.GetByID(ctx, *log.UserID)
		if err != nil {
			slog.Warn("content_moderation.ban_get_user_failed", "user_id", *log.UserID, "error", err)
			return false
		}
		if user.IsAdmin() {
			slog.Warn("content_moderation.autoban_skipped_admin", "user_id", *log.UserID, "role", user.Role, "count", count, "threshold", cfg.BanThreshold)
			// TODO: Disable the triggering API key instead when API key mutation is available here.
			return false
		}
		if user.Status != StatusDisabled {
			user.Status = StatusDisabled
			if err := s.userRepo.Update(ctx, user); err != nil {
				slog.Warn("content_moderation.ban_update_user_failed", "user_id", *log.UserID, "error", err)
				return false
			}
			if s.authCacheInvalidator != nil {
				s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, *log.UserID)
			}
			autoBanJustApplied = true
		}
		log.AutoBanned = true
	}
	return autoBanJustApplied
}

func (s *ContentModerationService) sendFlaggedNotificationSideEffects(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog, autoBanJustApplied bool) {
	if s == nil || cfg == nil || log == nil || !log.Flagged {
		return
	}
	if s.emailService == nil || strings.TrimSpace(log.UserEmail) == "" {
		return
	}
	emailSent := false
	if cfg.EmailOnHit {
		if err := s.sendViolationEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	if autoBanJustApplied {
		if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
			slog.Warn("content_moderation.ban_email_failed", "user_id", *log.UserID, "email", log.UserEmail, "error", err)
		} else {
			emailSent = true
		}
	}
	log.EmailSent = emailSent
}

func (s *ContentModerationService) sendViolationEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		if err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventContentModerationViolation,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      contentModerationEmailVariables(log, cfg),
		}); err == nil {
			return nil
		} else {
			if !shouldFallbackNotificationEmail(err) {
				return err
			}
			slog.Warn("template content moderation violation email failed; falling back to built-in body", "log_id", log.ID, "recipient_hash", notificationEmailHash(log.UserEmail), "err", err.Error())
		}
	}
	subject := fmt.Sprintf("[%s] 账户风控提醒 / Risk Control Notice", sanitizeEmailHeader(siteName))
	body := buildContentModerationViolationEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func (s *ContentModerationService) sendAccountDisabledEmail(ctx context.Context, cfg *ContentModerationConfig, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		if err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventContentModerationDisabled,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      contentModerationEmailVariables(log, cfg),
		}); err == nil {
			return nil
		} else {
			if !shouldFallbackNotificationEmail(err) {
				return err
			}
			slog.Warn("template content moderation disabled email failed; falling back to built-in body", "log_id", log.ID, "recipient_hash", notificationEmailHash(log.UserEmail), "err", err.Error())
		}
	}
	subject := fmt.Sprintf("[%s] 账户已被禁用 / Account Disabled", sanitizeEmailHeader(siteName))
	body := buildContentModerationAccountDisabledEmailBody(siteName, log, cfg)
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, body)
}

func contentModerationEmailUserID(log *ContentModerationLog) int64 {
	if log == nil || log.UserID == nil {
		return 0
	}
	return *log.UserID
}

func contentModerationEmailSourceID(log *ContentModerationLog) string {
	if log == nil || log.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", log.ID)
}

func contentModerationEmailVariables(log *ContentModerationLog, cfg *ContentModerationConfig) map[string]string {
	variables := map[string]string{
		"triggered_at":        time.Now().UTC().Format(time.RFC3339),
		"group_name":          "-",
		"moderation_category": "-",
		"moderation_score":    "0.000",
		"violation_count":     "0",
		"ban_threshold":       "0",
	}
	if log != nil {
		if !log.CreatedAt.IsZero() {
			variables["triggered_at"] = log.CreatedAt.UTC().Format(time.RFC3339)
		}
		if strings.TrimSpace(log.GroupName) != "" {
			variables["group_name"] = strings.TrimSpace(log.GroupName)
		}
		if strings.TrimSpace(log.HighestCategory) != "" {
			variables["moderation_category"] = strings.TrimSpace(log.HighestCategory)
		}
		variables["moderation_score"] = fmt.Sprintf("%.3f", log.HighestScore)
		variables["violation_count"] = fmt.Sprintf("%d", log.ViolationCount)
	}
	if cfg != nil {
		variables["ban_threshold"] = fmt.Sprintf("%d", cfg.BanThreshold)
	}
	return variables
}

func (s *ContentModerationService) siteName(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return "Sub2API"
	}
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || strings.TrimSpace(name) == "" {
		return "Sub2API"
	}
	return strings.TrimSpace(name)
}

func defaultContentModerationConfig() *ContentModerationConfig {
	return &ContentModerationConfig{
		Enabled:              false,
		Mode:                 ContentModerationModePreBlock,
		BaseURL:              defaultContentModerationBaseURL,
		Model:                defaultContentModerationModel,
		TimeoutMS:            defaultContentModerationTimeoutMS,
		SampleRate:           100,
		AllGroups:            true,
		GroupIDs:             []int64{},
		RecordNonHits:        false,
		AuditScope:           ContentModerationAuditScopeAllContext,
		StoreInputExcerpt:    true,
		SearchInputExcerpt:   false,
		Thresholds:           ContentModerationDefaultThresholds(),
		WorkerCount:          defaultContentModerationWorkerCount,
		QueueSize:            defaultContentModerationQueueSize,
		BlockStatus:          defaultContentModerationBlockHTTPStatus,
		BlockMessage:         defaultContentModerationBlockMessage,
		EmailOnHit:           true,
		AutoBanEnabled:       true,
		BanThreshold:         defaultContentModerationBanThreshold,
		ViolationWindowHours: defaultContentModerationViolationWindowHours,
		RetryCount:           defaultContentModerationRetryCount,
		HitRetentionDays:     defaultContentModerationHitRetentionDays,
		NonHitRetentionDays:  defaultContentModerationNonHitRetentionDays,
		PreHashCheckEnabled:  false,
		BlockedKeywords:      []string{},
		KeywordRules:         []ContentModerationKeywordRule{},
		KeywordBlockingMode:  ContentModerationKeywordModeKeywordAndAPI,
		EngineMode:           "",
		LocalClassifier:      defaultContentModerationLocalClassifierConfig(),
		ModelFilter: ContentModerationModelFilter{
			Type:   ContentModerationModelFilterAll,
			Models: []string{},
		},
		FailStrategy: ContentModerationFailStrategy{
			Default:         ContentModerationFailStrategyClosed,
			TrustedGroupIDs: []int64{},
			PublicGroupIDs:  []int64{},
		},
		CyberPolicyExcludeFromBanCount: false,
	}
}

func cloneContentModerationConfig(cfg *ContentModerationConfig) *ContentModerationConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.APIKeys = append([]string(nil), cfg.APIKeys...)
	clone.GroupIDs = append([]int64(nil), cfg.GroupIDs...)
	clone.BlockedKeywords = append([]string(nil), cfg.BlockedKeywords...)
	clone.KeywordRules = cloneContentModerationKeywordRules(cfg.KeywordRules)
	clone.Thresholds = cloneFloatMap(cfg.Thresholds)
	clone.LocalClassifier = normalizeContentModerationLocalClassifierConfig(cfg.LocalClassifier)
	clone.ModelFilter = ContentModerationModelFilter{
		Type:   cfg.ModelFilter.Type,
		Models: append([]string(nil), cfg.ModelFilter.Models...),
	}
	clone.FailStrategy = cloneContentModerationFailStrategy(cfg.FailStrategy)
	return &clone
}

func (cfg *ContentModerationConfig) normalize() {
	if cfg.APIKey != "" {
		cfg.APIKeys = normalizeModerationAPIKeys(append(cfg.APIKeys, cfg.APIKey))
		cfg.APIKey = ""
	} else {
		cfg.APIKeys = normalizeModerationAPIKeys(cfg.APIKeys)
	}
	if cfg.Mode == "" {
		cfg.Mode = ContentModerationModePreBlock
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultContentModerationBaseURL
	}
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Model == "" {
		cfg.Model = defaultContentModerationModel
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.TimeoutMS <= 0 {
		cfg.TimeoutMS = defaultContentModerationTimeoutMS
	}
	if cfg.TimeoutMS > maxContentModerationTimeoutMS {
		cfg.TimeoutMS = maxContentModerationTimeoutMS
	}
	if cfg.SampleRate < 0 {
		cfg.SampleRate = 0
	}
	if cfg.SampleRate > 100 {
		cfg.SampleRate = 100
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = defaultContentModerationWorkerCount
	}
	if cfg.WorkerCount > maxContentModerationWorkerCount {
		cfg.WorkerCount = maxContentModerationWorkerCount
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultContentModerationQueueSize
	}
	if cfg.QueueSize > maxContentModerationQueueSize {
		cfg.QueueSize = maxContentModerationQueueSize
	}
	if strings.TrimSpace(cfg.BlockMessage) == "" {
		cfg.BlockMessage = defaultContentModerationBlockMessage
	}
	cfg.BlockMessage = strings.TrimSpace(cfg.BlockMessage)
	if cfg.BlockStatus <= 0 {
		cfg.BlockStatus = defaultContentModerationBlockHTTPStatus
	}
	if cfg.BanThreshold <= 0 {
		cfg.BanThreshold = defaultContentModerationBanThreshold
	}
	if cfg.ViolationWindowHours <= 0 {
		cfg.ViolationWindowHours = defaultContentModerationViolationWindowHours
	}
	if cfg.RetryCount < 0 {
		cfg.RetryCount = 0
	}
	if cfg.RetryCount > maxContentModerationRetryCount {
		cfg.RetryCount = maxContentModerationRetryCount
	}
	if cfg.HitRetentionDays <= 0 {
		cfg.HitRetentionDays = defaultContentModerationHitRetentionDays
	}
	if cfg.HitRetentionDays > maxContentModerationRetentionDays {
		cfg.HitRetentionDays = maxContentModerationRetentionDays
	}
	if cfg.NonHitRetentionDays <= 0 {
		cfg.NonHitRetentionDays = defaultContentModerationNonHitRetentionDays
	}
	if cfg.NonHitRetentionDays > maxContentModerationNonHitRetentionDays {
		cfg.NonHitRetentionDays = maxContentModerationNonHitRetentionDays
	}
	cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)
	cfg.AuditScope = normalizeContentModerationAuditScope(cfg.AuditScope)
	cfg.Thresholds = mergeContentModerationThresholds(ContentModerationDefaultThresholds(), cfg.Thresholds)
	cfg.BlockedKeywords = normalizeBlockedKeywords(cfg.BlockedKeywords)
	cfg.KeywordRules = normalizeContentModerationKeywordRules(cfg.KeywordRules)
	cfg.KeywordBlockingMode, cfg.EngineMode = normalizeModerationEngineAndKeywordModes(cfg.EngineMode, cfg.KeywordBlockingMode)
	cfg.LocalClassifier = normalizeContentModerationLocalClassifierConfig(cfg.LocalClassifier)
	cfg.ModelFilter = normalizeContentModerationModelFilter(cfg.ModelFilter)
	cfg.FailStrategy = normalizeContentModerationFailStrategy(cfg.FailStrategy)
}

func defaultContentModerationLocalClassifierConfig() ContentModerationLocalClassifierConfig {
	return ContentModerationLocalClassifierConfig{
		Enabled:         false,
		URL:             "",
		TimeoutMS:       defaultContentModerationLocalClassifierTimeoutMS,
		MaxConcurrency:  defaultContentModerationLocalClassifierMaxConcurrency,
		BlockThreshold:  defaultContentModerationLocalClassifierBlockThreshold,
		ReviewThreshold: defaultContentModerationLocalClassifierReviewThreshold,
	}
}

func normalizeContentModerationLocalClassifierConfig(config ContentModerationLocalClassifierConfig) ContentModerationLocalClassifierConfig {
	config.URL = strings.TrimSpace(config.URL)
	if config.TimeoutMS <= 0 {
		config.TimeoutMS = defaultContentModerationLocalClassifierTimeoutMS
	}
	if config.TimeoutMS > maxContentModerationLocalClassifierTimeoutMS {
		config.TimeoutMS = maxContentModerationLocalClassifierTimeoutMS
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = defaultContentModerationLocalClassifierMaxConcurrency
	}
	if config.MaxConcurrency > maxContentModerationLocalClassifierMaxConcurrency {
		config.MaxConcurrency = maxContentModerationLocalClassifierMaxConcurrency
	}
	if config.BlockThreshold <= 0 {
		config.BlockThreshold = defaultContentModerationLocalClassifierBlockThreshold
	}
	if config.BlockThreshold > 1 {
		config.BlockThreshold = 1
	}
	if config.ReviewThreshold <= 0 {
		config.ReviewThreshold = defaultContentModerationLocalClassifierReviewThreshold
	}
	if config.ReviewThreshold > 1 {
		config.ReviewThreshold = 1
	}
	if config.ReviewThreshold > config.BlockThreshold {
		config.ReviewThreshold = config.BlockThreshold
	}
	return config
}

func normalizeContentModerationAuditScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case ContentModerationAuditScopeUserOnly:
		return ContentModerationAuditScopeUserOnly
	case ContentModerationAuditScopeUserAndTool:
		return ContentModerationAuditScopeUserAndTool
	case ContentModerationAuditScopeAllContext:
		return ContentModerationAuditScopeAllContext
	default:
		return ContentModerationAuditScopeAllContext
	}
}

func (cfg *ContentModerationConfig) shouldRunLocalRules() bool {
	if cfg == nil {
		return false
	}
	switch cfg.EngineMode {
	case ContentModerationEngineModeAPIOnly:
		return false
	case ContentModerationEngineModeRuleOnly, ContentModerationEngineModeHybrid:
		return true
	default:
		return normalizeKeywordBlockingMode(cfg.KeywordBlockingMode) != ContentModerationKeywordModeAPIOnly
	}
}

func (cfg *ContentModerationConfig) externalModerationRequired() bool {
	if cfg == nil {
		return true
	}
	switch cfg.EngineMode {
	case ContentModerationEngineModeRuleOnly:
		return false
	case ContentModerationEngineModeAPIOnly, ContentModerationEngineModeHybrid:
		return true
	default:
		return normalizeKeywordBlockingMode(cfg.KeywordBlockingMode) != ContentModerationKeywordModeKeywordOnly
	}
}

func (cfg *ContentModerationConfig) keywordRules() []ContentModerationKeywordRule {
	if cfg == nil {
		return []ContentModerationKeywordRule{}
	}
	combined := make([]ContentModerationKeywordRule, 0, len(cfg.BlockedKeywords)+len(cfg.KeywordRules))
	combined = append(combined, cfg.KeywordRules...)
	for _, keyword := range cfg.BlockedKeywords {
		if shouldSkipLegacyKeywordRule(keyword) {
			continue
		}
		combined = append(combined, ContentModerationKeywordRule{
			Keyword:  keyword,
			Category: ContentModerationKeywordCategoryCustom,
			Severity: ContentModerationKeywordSeverityHigh,
			Action:   ContentModerationKeywordActionBlock,
			Enabled:  true,
		})
	}
	return normalizeContentModerationKeywordRules(combined)
}

func shouldSkipLegacyKeywordRule(keyword string) bool {
	switch strings.ToLower(strings.TrimSpace(keyword)) {
	case "csam":
		return true
	default:
		return false
	}
}

func (cfg *ContentModerationConfig) includesGroup(groupID *int64) bool {
	if cfg.AllGroups {
		return true
	}
	if groupID == nil {
		return false
	}
	for _, id := range cfg.GroupIDs {
		if id == *groupID {
			return true
		}
	}
	return false
}

func (cfg *ContentModerationConfig) includesModel(model string) bool {
	if cfg == nil {
		return true
	}
	filter := normalizeContentModerationModelFilter(cfg.ModelFilter)
	switch filter.Type {
	case ContentModerationModelFilterInclude:
		return contentModerationModelListContains(filter.Models, model)
	case ContentModerationModelFilterExclude:
		return !contentModerationModelListContains(filter.Models, model)
	default:
		return true
	}
}

func (cfg *ContentModerationConfig) shouldFailClosed(input ContentModerationCheckInput) bool {
	if cfg == nil {
		return true
	}
	strategy := normalizeContentModerationFailStrategy(cfg.FailStrategy)
	if input.GroupID != nil && int64SliceContains(strategy.TrustedGroupIDs, *input.GroupID) {
		return false
	}
	return strategy.Default != ContentModerationFailStrategyOpen
}

func contentModerationFailureDecision(cfg *ContentModerationConfig) *ContentModerationDecision {
	status := http.StatusServiceUnavailable
	if cfg != nil && cfg.BlockStatus >= 500 && cfg.BlockStatus <= 599 {
		status = cfg.BlockStatus
	}
	return &ContentModerationDecision{
		Allowed:    false,
		Blocked:    true,
		Flagged:    true,
		Message:    "内容安全模块暂时不可用，请稍后重试",
		StatusCode: status,
		Action:     ContentModerationActionError,
	}
}

func contentModerationLogGroupID(groupID *int64) int64 {
	if groupID == nil {
		return 0
	}
	return *groupID
}

func (cfg *ContentModerationConfig) shouldRecordNonHit(hashText string) bool {
	if cfg.SampleRate >= 100 {
		return true
	}
	if cfg.SampleRate <= 0 {
		return false
	}
	raw, err := hex.DecodeString(hashText)
	if err != nil || len(raw) < 2 {
		return true
	}
	return int(binary.BigEndian.Uint16(raw[:2])%100) < cfg.SampleRate
}

func (cfg *ContentModerationConfig) apiKeys() []string {
	if cfg == nil {
		return nil
	}
	return normalizeModerationAPIKeys(cfg.APIKeys)
}

func (s *ContentModerationService) nextUsableAPIKey(cfg *ContentModerationConfig) (string, bool) {
	keys := cfg.apiKeys()
	if len(keys) == 0 {
		return "", false
	}
	now := time.Now()
	for i := 0; i < len(keys); i++ {
		idx := int(s.apiKeyCursor.Add(1)-1) % len(keys)
		key := keys[idx]
		if !s.isAPIKeyFrozen(key, now) {
			return key, true
		}
	}
	return "", false
}

func (s *ContentModerationService) isAPIKeyFrozen(key string, now time.Time) bool {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return false
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	return state != nil && state.FrozenUntil.After(now)
}

func (s *ContentModerationService) beginModerationAPIKeyCall(key string) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.SyncActive++
}

func (s *ContentModerationService) finishModerationAPIKeyCall(key string, latencyMS int, success bool) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if state.SyncActive > 0 {
		state.SyncActive--
	}
	state.SyncTotal++
	state.SyncLatencyMS += int64(latencyMS)
	if success {
		state.SyncSuccess++
		return
	}
	state.SyncErrors++
}

func (s *ContentModerationService) markAPIKeySuccess(key string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	state.FailureCount = 0
	state.SuccessCount++
	state.LastError = ""
	state.LastCheckedAt = time.Now()
	state.FrozenUntil = time.Time{}
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
}

func (s *ContentModerationService) markAPIKeyError(key string, errText string, latencyMS int, httpStatus int) {
	hash := moderationAPIKeyHash(key)
	if hash == "" || s == nil {
		return
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.ensureAPIKeyHealthLocked(hash, maskSecretTail(key))
	if contentModerationFreezeDurationForHTTPStatus(httpStatus) > 0 {
		state.FailureCount++
	}
	state.LastError = trimRunes(errText, 180)
	state.LastCheckedAt = time.Now()
	state.LastLatencyMS = latencyMS
	state.LastHTTPStatus = httpStatus
	state.LastTested = true
	if freezeDuration := contentModerationFreezeDurationForHTTPStatus(httpStatus); freezeDuration > 0 {
		state.FrozenUntil = time.Now().Add(freezeDuration)
	}
}

func contentModerationFreezeDurationForHTTPStatus(httpStatus int) time.Duration {
	switch httpStatus {
	case 0, http.StatusBadRequest:
		return 0
	case http.StatusUnauthorized, http.StatusForbidden:
		return contentModerationKeyAuthFreezeDuration
	case http.StatusTooManyRequests, 529:
		return contentModerationKeyRateLimitFreezeDuration
	default:
		return contentModerationKeyHTTPErrorFreezeDuration
	}
}

func (s *ContentModerationService) ensureAPIKeyHealthLocked(hash string, masked string) *contentModerationKeyHealth {
	if s.keyHealth == nil {
		s.keyHealth = make(map[string]*contentModerationKeyHealth)
	}
	state := s.keyHealth[hash]
	if state == nil {
		state = &contentModerationKeyHealth{Hash: hash}
		s.keyHealth[hash] = state
	}
	if strings.TrimSpace(masked) != "" {
		state.Masked = masked
	}
	return state
}

func (s *ContentModerationService) configView(cfg *ContentModerationConfig) *ContentModerationConfigView {
	keys := cfg.apiKeys()
	masks := make([]string, 0, len(keys))
	for _, key := range keys {
		masks = append(masks, maskSecretTail(key))
	}
	apiKeyMasked := ""
	if len(masks) > 0 {
		apiKeyMasked = masks[0]
	}
	return &ContentModerationConfigView{
		Enabled:                        cfg.Enabled,
		Mode:                           cfg.Mode,
		BaseURL:                        cfg.BaseURL,
		Model:                          cfg.Model,
		APIKeyConfigured:               len(keys) > 0,
		APIKeyMasked:                   apiKeyMasked,
		APIKeyCount:                    len(keys),
		APIKeyMasks:                    masks,
		APIKeyStatuses:                 s.apiKeyStatuses(keys),
		TimeoutMS:                      cfg.TimeoutMS,
		SampleRate:                     cfg.SampleRate,
		AllGroups:                      cfg.AllGroups,
		GroupIDs:                       append([]int64(nil), cfg.GroupIDs...),
		RecordNonHits:                  cfg.RecordNonHits,
		AuditScope:                     cfg.AuditScope,
		StoreInputExcerpt:              cfg.StoreInputExcerpt,
		SearchInputExcerpt:             cfg.SearchInputExcerpt,
		Thresholds:                     cloneFloatMap(cfg.Thresholds),
		WorkerCount:                    cfg.WorkerCount,
		QueueSize:                      cfg.QueueSize,
		BlockStatus:                    cfg.BlockStatus,
		BlockMessage:                   cfg.BlockMessage,
		EmailOnHit:                     cfg.EmailOnHit,
		AutoBanEnabled:                 cfg.AutoBanEnabled,
		BanThreshold:                   cfg.BanThreshold,
		ViolationWindowHours:           cfg.ViolationWindowHours,
		RetryCount:                     cfg.RetryCount,
		HitRetentionDays:               cfg.HitRetentionDays,
		NonHitRetentionDays:            cfg.NonHitRetentionDays,
		PreHashCheckEnabled:            cfg.PreHashCheckEnabled,
		BlockedKeywords:                append([]string(nil), cfg.BlockedKeywords...),
		KeywordRules:                   cloneContentModerationKeywordRules(cfg.KeywordRules),
		KeywordBlockingMode:            cfg.KeywordBlockingMode,
		EngineMode:                     cfg.EngineMode,
		LocalClassifier:                normalizeContentModerationLocalClassifierConfig(cfg.LocalClassifier),
		ModelFilter:                    cloneContentModerationModelFilter(cfg.ModelFilter),
		FailStrategy:                   cloneContentModerationFailStrategy(cfg.FailStrategy),
		CyberPolicyExcludeFromBanCount: cfg.CyberPolicyExcludeFromBanCount,
	}
}

func (s *ContentModerationService) apiKeyStatuses(keys []string) []ContentModerationAPIKeyStatus {
	out := make([]ContentModerationAPIKeyStatus, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.apiKeyStatusForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key), true))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyLoads(keys []string) []ContentModerationAPIKeyLoad {
	out := make([]ContentModerationAPIKeyLoad, 0, len(keys))
	for idx, key := range keys {
		out = append(out, s.preBlockAPIKeyLoadForHash(idx, moderationAPIKeyHash(key), maskSecretTail(key)))
	}
	return out
}

func (s *ContentModerationService) preBlockAPIKeyActive(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Active
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyAvailableCount(keys []string) int64 {
	now := time.Now()
	var count int64
	for _, key := range keys {
		if !s.isAPIKeyFrozen(key, now) {
			count++
		}
	}
	return count
}

func (s *ContentModerationService) preBlockAPIKeyTotalCalls(keys []string) int64 {
	var total int64
	for _, item := range s.preBlockAPIKeyLoads(keys) {
		total += item.Total
	}
	return total
}

func (s *ContentModerationService) preBlockAPIKeyLoadForHash(index int, hash string, masked string) ContentModerationAPIKeyLoad {
	load := ContentModerationAPIKeyLoad{
		Index:   index,
		KeyHash: hash,
		Masked:  masked,
		Status:  "unknown",
	}
	status := s.apiKeyStatusForHash(index, hash, masked, true)
	load.Status = status.Status
	load.LastLatencyMS = status.LastLatencyMS
	load.LastHTTPStatus = status.LastHTTPStatus
	if hash == "" || s == nil {
		return load
	}
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return load
	}
	load.Active = state.SyncActive
	load.Total = state.SyncTotal
	load.Success = state.SyncSuccess
	load.Errors = state.SyncErrors
	if state.SyncTotal > 0 {
		load.AvgLatencyMS = state.SyncLatencyMS / state.SyncTotal
	}
	return load
}

func (s *ContentModerationService) apiKeyStatusForHash(index int, hash string, masked string, configured bool) ContentModerationAPIKeyStatus {
	status := ContentModerationAPIKeyStatus{
		Index:      index,
		KeyHash:    hash,
		Masked:     masked,
		Status:     "unknown",
		Configured: configured,
	}
	if hash == "" || s == nil {
		return status
	}
	now := time.Now()
	s.keyHealthMu.Lock()
	defer s.keyHealthMu.Unlock()
	state := s.keyHealth[hash]
	if state == nil {
		return status
	}
	status.FailureCount = state.FailureCount
	status.SuccessCount = state.SuccessCount
	status.LastError = state.LastError
	status.LastLatencyMS = state.LastLatencyMS
	status.LastHTTPStatus = state.LastHTTPStatus
	status.LastTested = state.LastTested
	if !state.LastCheckedAt.IsZero() {
		t := state.LastCheckedAt
		status.LastCheckedAt = &t
	}
	if state.FrozenUntil.After(now) {
		t := state.FrozenUntil
		status.FrozenUntil = &t
		status.Status = "frozen"
		return status
	}
	if state.LastError != "" {
		status.Status = "error"
		return status
	}
	if state.SuccessCount > 0 || state.LastTested {
		status.Status = "ok"
	}
	return status
}

func moderationAPIKeyHash(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func buildModerationTestInput(prompt string, images []string) (any, int, error) {
	prompt = trimRunes(normalizeContentModerationText(prompt), maxModerationInputRunes)
	normalizedImages := make([]string, 0, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if err := validateModerationTestImageDataURL(image); err != nil {
			return nil, 0, err
		}
		normalizedImages = append(normalizedImages, image)
	}
	imageCount := len(normalizedImages)
	normalizedImages = limitContentModerationImages(normalizedImages)
	if prompt == "" && len(normalizedImages) == 0 {
		return "hello", 0, nil
	}
	if len(normalizedImages) == 0 {
		return prompt, 0, nil
	}
	parts := make([]moderationAPIInputPart, 0, len(normalizedImages)+1)
	if prompt != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: prompt})
	}
	for _, image := range normalizedImages {
		parts = append(parts, moderationAPIInputPart{
			Type:     "image_url",
			ImageURL: &moderationAPIImageURLRef{URL: image},
		})
	}
	return parts, imageCount, nil
}

func contentModerationTestHasAuditInput(prompt string, images []string) bool {
	if normalizeContentModerationText(prompt) != "" {
		return true
	}
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			return true
		}
	}
	return false
}

func validateModerationTestImageDataURL(value string) error {
	if len(value) > maxContentModerationTestImageDataURLBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	if !strings.HasPrefix(value, "data:image/") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 data:image/* base64")
	}
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片必须是 base64 data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return infraerrors.BadRequest("INVALID_MODERATION_TEST_IMAGE", "测试图片 base64 无效")
	}
	if len(raw) > maxContentModerationTestImageBytes {
		return infraerrors.BadRequest("MODERATION_TEST_IMAGE_TOO_LARGE", "测试图片不能超过 8MB")
	}
	return nil
}

func buildContentModerationTestAuditResult(result *moderationAPIResult, thresholds map[string]float64) *ContentModerationTestAuditResult {
	if result == nil {
		return nil
	}
	scores := make(map[string]float64, len(result.CategoryScores))
	for category, score := range result.CategoryScores {
		scores[category] = score
	}
	thresholdSnapshot := mergeContentModerationThresholds(ContentModerationDefaultThresholds(), thresholds)
	flagged, highestCategory, highestScore := evaluateModerationScores(scores, thresholdSnapshot)
	compositeScore := highestScore
	return &ContentModerationTestAuditResult{
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
		CompositeScore:  compositeScore,
		CategoryScores:  scores,
		Thresholds:      thresholdSnapshot,
	}
}

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIInputPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *moderationAPIImageURLRef `json:"image_url,omitempty"`
}

type moderationAPIImageURLRef struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPIResult `json:"results"`
}

type moderationAPIResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}

func evaluateModerationScores(scores map[string]float64, thresholds map[string]float64) (bool, string, float64) {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	for _, category := range contentModerationCategoryOrder {
		score := scores[category]
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
		if score >= thresholds[category] {
			flagged = true
		}
	}
	for category, score := range scores {
		if score > highestScore || highestCategory == "" {
			highestScore = score
			highestCategory = category
		}
	}
	return flagged, highestCategory, highestScore
}

func mergeContentModerationThresholds(base map[string]float64, override map[string]float64) map[string]float64 {
	out := cloneFloatMap(base)
	if out == nil {
		out = map[string]float64{}
	}
	for _, category := range contentModerationCategoryOrder {
		if v, ok := override[category]; ok {
			if v < 0 {
				v = 0
			}
			if v > 1 {
				v = 1
			}
			out[category] = v
		}
	}
	return out
}

func normalizeInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func normalizeBlockedKeywords(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		kw := strings.TrimSpace(raw)
		if kw == "" {
			continue
		}
		kw = trimRunes(kw, maxContentModerationBlockedKeywordRunes)
		key := strings.ToLower(kw)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, kw)
		if len(out) >= maxContentModerationBlockedKeywords {
			break
		}
	}
	return out
}

func normalizeContentModerationKeywordRules(in []ContentModerationKeywordRule) []ContentModerationKeywordRule {
	if len(in) == 0 {
		return []ContentModerationKeywordRule{}
	}
	out := make([]ContentModerationKeywordRule, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		keyword := strings.TrimSpace(raw.Keyword)
		if keyword == "" {
			continue
		}
		keyword = trimRunes(keyword, maxContentModerationBlockedKeywordRunes)
		key := strings.ToLower(normalizeKeywordComparable(keyword))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ContentModerationKeywordRule{
			Keyword:  keyword,
			Category: normalizeContentModerationKeywordCategory(raw.Category),
			Severity: normalizeContentModerationKeywordSeverity(raw.Severity),
			Action:   normalizeContentModerationKeywordAction(raw.Action),
			Enabled:  raw.Enabled,
		})
		if len(out) >= maxContentModerationBlockedKeywords {
			break
		}
	}
	return out
}

func cloneContentModerationKeywordRules(in []ContentModerationKeywordRule) []ContentModerationKeywordRule {
	if len(in) == 0 {
		return []ContentModerationKeywordRule{}
	}
	out := make([]ContentModerationKeywordRule, len(in))
	copy(out, in)
	return out
}

func normalizeContentModerationKeywordCategory(category string) string {
	switch strings.TrimSpace(category) {
	case ContentModerationKeywordCategoryCustom:
		return ContentModerationKeywordCategoryCustom
	case ContentModerationKeywordCategoryJailbreak:
		return ContentModerationKeywordCategoryJailbreak
	case ContentModerationKeywordCategoryCyber:
		return ContentModerationKeywordCategoryCyber
	case ContentModerationKeywordCategoryMinorSafety:
		return ContentModerationKeywordCategoryMinorSafety
	case ContentModerationKeywordCategorySelfHarm:
		return ContentModerationKeywordCategorySelfHarm
	case ContentModerationKeywordCategoryViolence:
		return ContentModerationKeywordCategoryViolence
	case ContentModerationKeywordCategoryWeapons:
		return ContentModerationKeywordCategoryWeapons
	case ContentModerationKeywordCategoryPrivacy:
		return ContentModerationKeywordCategoryPrivacy
	case ContentModerationKeywordCategoryFraud:
		return ContentModerationKeywordCategoryFraud
	case ContentModerationKeywordCategoryAccountAbuse:
		return ContentModerationKeywordCategoryAccountAbuse
	case ContentModerationKeywordCategoryPolitical:
		return ContentModerationKeywordCategoryPolitical
	case ContentModerationKeywordCategoryHighImpactDecision:
		return ContentModerationKeywordCategoryHighImpactDecision
	case ContentModerationKeywordCategoryRegulatedAdvice:
		return ContentModerationKeywordCategoryRegulatedAdvice
	case ContentModerationKeywordCategoryCopyright:
		return ContentModerationKeywordCategoryCopyright
	case ContentModerationKeywordCategoryBiometric:
		return ContentModerationKeywordCategoryBiometric
	case ContentModerationKeywordCategoryOther:
		return ContentModerationKeywordCategoryOther
	default:
		return ContentModerationKeywordCategoryOther
	}
}

func normalizeContentModerationKeywordSeverity(severity string) string {
	switch strings.TrimSpace(severity) {
	case ContentModerationKeywordSeverityLow:
		return ContentModerationKeywordSeverityLow
	case ContentModerationKeywordSeverityMedium:
		return ContentModerationKeywordSeverityMedium
	case ContentModerationKeywordSeverityCritical:
		return ContentModerationKeywordSeverityCritical
	case ContentModerationKeywordSeverityHigh:
		return ContentModerationKeywordSeverityHigh
	default:
		return ContentModerationKeywordSeverityHigh
	}
}

func normalizeContentModerationKeywordAction(action string) string {
	switch strings.TrimSpace(action) {
	case ContentModerationKeywordActionObserve:
		return ContentModerationKeywordActionObserve
	case ContentModerationKeywordActionWarn:
		return ContentModerationKeywordActionWarn
	case ContentModerationKeywordActionBlock:
		return ContentModerationKeywordActionBlock
	default:
		return ContentModerationKeywordActionBlock
	}
}

func normalizeContentModerationReviewStatus(status string) string {
	switch strings.TrimSpace(status) {
	case ContentModerationReviewStatusFalsePositive:
		return ContentModerationReviewStatusFalsePositive
	case ContentModerationReviewStatusConfirmedViolation:
		return ContentModerationReviewStatusConfirmedViolation
	case ContentModerationReviewStatusPending:
		return ContentModerationReviewStatusPending
	default:
		return ContentModerationReviewStatusPending
	}
}

func normalizeKeywordBlockingMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ContentModerationKeywordModeKeywordOnly:
		return ContentModerationKeywordModeKeywordOnly
	case ContentModerationKeywordModeAPIOnly:
		return ContentModerationKeywordModeAPIOnly
	case ContentModerationKeywordModeKeywordAndAPI:
		return ContentModerationKeywordModeKeywordAndAPI
	default:
		return ContentModerationKeywordModeKeywordAndAPI
	}
}

func normalizeModerationEngineMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case ContentModerationEngineModeRuleOnly:
		return ContentModerationEngineModeRuleOnly
	case ContentModerationEngineModeAPIOnly:
		return ContentModerationEngineModeAPIOnly
	case ContentModerationEngineModeHybrid:
		return ContentModerationEngineModeHybrid
	default:
		return ""
	}
}

func normalizeModerationEngineAndKeywordModes(engineMode string, keywordMode string) (string, string) {
	normalizedEngineMode := normalizeModerationEngineMode(engineMode)
	normalizedKeywordMode := normalizeKeywordBlockingMode(keywordMode)
	if normalizedEngineMode == "" {
		return normalizedKeywordMode, engineModeFromKeywordBlockingMode(normalizedKeywordMode)
	}
	return keywordBlockingModeFromEngineMode(normalizedEngineMode), normalizedEngineMode
}

func engineModeFromKeywordBlockingMode(mode string) string {
	switch normalizeKeywordBlockingMode(mode) {
	case ContentModerationKeywordModeKeywordOnly:
		return ContentModerationEngineModeRuleOnly
	case ContentModerationKeywordModeAPIOnly:
		return ContentModerationEngineModeAPIOnly
	default:
		return ContentModerationEngineModeHybrid
	}
}

func keywordBlockingModeFromEngineMode(mode string) string {
	switch normalizeModerationEngineMode(mode) {
	case ContentModerationEngineModeRuleOnly:
		return ContentModerationKeywordModeKeywordOnly
	case ContentModerationEngineModeAPIOnly:
		return ContentModerationKeywordModeAPIOnly
	default:
		return ContentModerationKeywordModeKeywordAndAPI
	}
}

func normalizeContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	out := ContentModerationModelFilter{
		Type:   normalizeContentModerationModelFilterType(filter.Type),
		Models: normalizeContentModerationModelNames(filter.Models),
	}
	if out.Type == ContentModerationModelFilterAll {
		out.Models = []string{}
	}
	return out
}

func cloneContentModerationModelFilter(filter ContentModerationModelFilter) ContentModerationModelFilter {
	normalized := normalizeContentModerationModelFilter(filter)
	normalized.Models = append([]string(nil), normalized.Models...)
	return normalized
}

func normalizeContentModerationModelFilterType(filterType string) string {
	switch strings.ToLower(strings.TrimSpace(filterType)) {
	case ContentModerationModelFilterInclude:
		return ContentModerationModelFilterInclude
	case ContentModerationModelFilterExclude:
		return ContentModerationModelFilterExclude
	case ContentModerationModelFilterAll:
		return ContentModerationModelFilterAll
	default:
		return ContentModerationModelFilterAll
	}
}

func normalizeContentModerationModelNames(models []string) []string {
	if len(models) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, raw := range models {
		model := trimRunes(strings.TrimSpace(raw), maxContentModerationModelFilterRunes)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
		if len(out) >= maxContentModerationModelFilterModels {
			break
		}
	}
	return out
}

func normalizeContentModerationFailStrategy(strategy ContentModerationFailStrategy) ContentModerationFailStrategy {
	out := ContentModerationFailStrategy{
		Default:         normalizeContentModerationFailStrategyDefault(strategy.Default),
		TrustedGroupIDs: normalizeInt64IDs(strategy.TrustedGroupIDs),
		PublicGroupIDs:  normalizeInt64IDs(strategy.PublicGroupIDs),
	}
	return out
}

func cloneContentModerationFailStrategy(strategy ContentModerationFailStrategy) ContentModerationFailStrategy {
	normalized := normalizeContentModerationFailStrategy(strategy)
	normalized.TrustedGroupIDs = append([]int64(nil), normalized.TrustedGroupIDs...)
	normalized.PublicGroupIDs = append([]int64(nil), normalized.PublicGroupIDs...)
	return normalized
}

func normalizeContentModerationFailStrategyDefault(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ContentModerationFailStrategyOpen:
		return ContentModerationFailStrategyOpen
	case ContentModerationFailStrategyClosed:
		return ContentModerationFailStrategyClosed
	default:
		return ContentModerationFailStrategyClosed
	}
}

func contentModerationModelListContains(models []string, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, candidate := range models {
		if strings.ToLower(strings.TrimSpace(candidate)) == model {
			return true
		}
	}
	return false
}

func int64SliceContains(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

type contentModerationRiskContext struct {
	Type   string
	Reason string
}

type contentModerationKeywordDecision struct {
	rule            ContentModerationKeywordRule
	context         contentModerationRiskContext
	action          string
	flagged         bool
	blocked         bool
	effectiveAction string
}

func decideContentModerationKeyword(text string, rule ContentModerationKeywordRule) contentModerationKeywordDecision {
	rule = normalizeContentModerationKeywordRules([]ContentModerationKeywordRule{rule})[0]
	ctx := classifyContentModerationKeywordContext(text, rule)
	effectiveAction := rule.Action
	downgradedForContext := shouldDowngradeKeywordActionForContext(ctx)
	if downgradedForContext {
		effectiveAction = ContentModerationKeywordActionObserve
	}
	decision := contentModerationKeywordDecision{
		rule:            rule,
		context:         ctx,
		action:          effectiveAction,
		flagged:         true,
		blocked:         false,
		effectiveAction: effectiveAction,
	}
	switch {
	case effectiveAction == ContentModerationKeywordActionBlock:
		decision.action = ContentModerationActionKeywordBlock
		decision.blocked = true
	case downgradedForContext:
		decision.action = ContentModerationActionKeywordReview
		decision.flagged = false
	}
	return decision
}

func classifyContentModerationKeywordContext(text string, rule ContentModerationKeywordRule) contentModerationRiskContext {
	normalized := normalizeKeywordComparable(text)
	if normalized == "" {
		return contentModerationRiskContext{Type: ContentModerationRiskContextUnknown, Reason: "empty_text"}
	}
	if isCodexInternalPromptText(text) {
		return contentModerationRiskContext{Type: ContentModerationRiskContextCodexInternal, Reason: "codex_internal_prompt_marker"}
	}
	metaMarkers := []string{
		"审计关键词", "关键词", "规则列表", "违规行为列表", "风控规则", "拦截规则", "审核规则", "误杀",
		"policy", "safety policy", "keyword", "rule list", "audit rule", "moderation rule", "false positive",
	}
	if containsAnyKeywordComparable(normalized, metaMarkers) {
		return contentModerationRiskContext{Type: ContentModerationRiskContextMetaDiscussion, Reason: "policy_or_keyword_rule_discussion"}
	}
	educationalMarkers := []string{
		"如何防范", "安全教育", "合规培训", "风险说明", "案例分析",
		"how to prevent", "safety training", "compliance training", "risk explanation", "case study",
	}
	if containsAnyKeywordComparable(normalized, educationalMarkers) {
		return contentModerationRiskContext{Type: ContentModerationRiskContextEducational, Reason: "educational_or_prevention_context"}
	}
	actualRequestMarkers := []string{
		"帮我生成", "生成一份", "写一个", "教我", "教程", "步骤", "方法", "购买", "出售", "绕过",
		"generate", "write", "teach me", "tutorial", "steps", "method", "buy", "sell", "bypass",
	}
	if containsAnyKeywordComparable(normalized, actualRequestMarkers) {
		return contentModerationRiskContext{Type: ContentModerationRiskContextActualRequest, Reason: "request_intent_marker"}
	}
	return contentModerationRiskContext{Type: ContentModerationRiskContextUnknown, Reason: "no_context_marker"}
}

func shouldDowngradeKeywordActionForContext(ctx contentModerationRiskContext) bool {
	switch ctx.Type {
	case ContentModerationRiskContextCodexInternal, ContentModerationRiskContextMetaDiscussion, ContentModerationRiskContextEducational:
		return true
	default:
		return false
	}
}

func containsAnyKeywordComparable(normalizedText string, markers []string) bool {
	for _, marker := range markers {
		normalizedMarker := normalizeKeywordComparable(marker)
		if normalizedMarker != "" && strings.Contains(normalizedText, normalizedMarker) {
			return true
		}
	}
	return false
}

func applyContentModerationKeywordMetadata(log *ContentModerationLog, decision contentModerationKeywordDecision) {
	if log == nil {
		return
	}
	log.MatchedKeyword = decision.rule.Keyword
	log.KeywordCategory = decision.rule.Category
	log.KeywordSeverity = decision.rule.Severity
	log.KeywordAction = decision.rule.Action
	log.EffectiveKeywordAction = decision.effectiveAction
	log.RiskContextType = decision.context.Type
	log.RiskContextReason = decision.context.Reason
	if decision.action == ContentModerationActionKeywordReview {
		log.ReviewStatus = ContentModerationReviewStatusPending
	}
}

func contentModerationDecisionFromKeyword(cfg *ContentModerationConfig, keywordDecision contentModerationKeywordDecision, scores map[string]float64) *ContentModerationDecision {
	decision := &ContentModerationDecision{
		Allowed:                !keywordDecision.blocked,
		Blocked:                keywordDecision.blocked,
		Flagged:                keywordDecision.flagged,
		Message:                "",
		StatusCode:             0,
		HighestCategory:        contentModerationKeywordCategory,
		HighestScore:           1.0,
		CategoryScores:         scores,
		Action:                 keywordDecision.action,
		MatchedKeyword:         keywordDecision.rule.Keyword,
		KeywordCategory:        keywordDecision.rule.Category,
		KeywordSeverity:        keywordDecision.rule.Severity,
		KeywordAction:          keywordDecision.rule.Action,
		EffectiveKeywordAction: keywordDecision.effectiveAction,
		RiskContextType:        keywordDecision.context.Type,
		RiskContextReason:      keywordDecision.context.Reason,
	}
	if keywordDecision.blocked && cfg != nil {
		decision.Message = cfg.BlockMessage
		decision.StatusCode = cfg.BlockStatus
	}
	return decision
}

func matchBlockedKeyword(text string, keywords []string) (string, bool) {
	rules := make([]ContentModerationKeywordRule, 0, len(keywords))
	for _, keyword := range keywords {
		rules = append(rules, ContentModerationKeywordRule{
			Keyword:  keyword,
			Category: ContentModerationKeywordCategoryCustom,
			Severity: ContentModerationKeywordSeverityHigh,
			Action:   ContentModerationKeywordActionBlock,
			Enabled:  true,
		})
	}
	match, hit := matchContentModerationKeyword(text, rules)
	return match.Keyword, hit
}

func matchContentModerationLocalRule(text string, rules []ContentModerationKeywordRule) (ContentModerationKeywordRule, bool) {
	if match, hit := matchContentModerationKeyword(text, rules); hit {
		return match, true
	}
	return matchContextualBuiltInRiskRule(text)
}

func matchContentModerationKeyword(text string, rules []ContentModerationKeywordRule) (ContentModerationKeywordRule, bool) {
	if text == "" || len(rules) == 0 {
		return ContentModerationKeywordRule{}, false
	}
	normalizedText := normalizeKeywordComparable(text)
	compactText := compactKeywordComparable(normalizedText)
	if normalizedText == "" {
		return ContentModerationKeywordRule{}, false
	}
	for _, rule := range normalizeContentModerationKeywordRules(rules) {
		if !rule.Enabled {
			continue
		}
		normalizedKeyword := normalizeKeywordComparable(rule.Keyword)
		if normalizedKeyword == "" {
			continue
		}
		compactKeyword := compactKeywordComparable(normalizedKeyword)
		if _, _, hit := findKeywordComparableSpanWithBoundary(normalizedText, normalizedKeyword); hit {
			return rule, true
		}
		if compactKeyword != "" {
			if _, _, hit := findCompactKeywordComparableSpanWithBoundary(normalizedText, compactText, compactKeyword); hit {
				return rule, true
			}
		}
	}
	return ContentModerationKeywordRule{}, false
}

func matchContextualBuiltInRiskRule(text string) (ContentModerationKeywordRule, bool) {
	normalized := normalizeKeywordComparable(text)
	if normalized == "" {
		return ContentModerationKeywordRule{}, false
	}
	if keyword, hit := contextualJailbreakInstructionKeyword(normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryJailbreak, ContentModerationKeywordSeverityCritical), true
	}
	if keyword, hit := contextualCyberDatabaseExtractionKeyword(text, normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryCyber, ContentModerationKeywordSeverityCritical), true
	}
	if keyword, hit := contextualCyberReverseCrackingKeyword(normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryCyber, ContentModerationKeywordSeverityCritical), true
	}
	if keyword, hit := contextualCyberIntrusionKeyword(text, normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryCyber, ContentModerationKeywordSeverityCritical), true
	}
	if keyword, hit := contextualPoliticalSensitiveKeyword(normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryPolitical, ContentModerationKeywordSeverityHigh), true
	}
	if keyword, hit := contextualSexualExplicitKeyword(normalized); hit {
		return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryOther, ContentModerationKeywordSeverityHigh), true
	}
	return ContentModerationKeywordRule{}, false
}

func matchContextualCyberIntentRule(text string) (ContentModerationKeywordRule, bool) {
	match, hit := matchContextualBuiltInRiskRule(text)
	if !hit || match.Category != ContentModerationKeywordCategoryCyber {
		return ContentModerationKeywordRule{}, false
	}
	return match, true
}

func contextualBuiltInRiskRule(keyword string, category string, severity string) ContentModerationKeywordRule {
	return ContentModerationKeywordRule{
		Keyword:  keyword,
		Category: category,
		Severity: severity,
		Action:   ContentModerationKeywordActionBlock,
		Enabled:  true,
	}
}

func contextualCyberIntentRule(keyword string) ContentModerationKeywordRule {
	return contextualBuiltInRiskRule(keyword, ContentModerationKeywordCategoryCyber, ContentModerationKeywordSeverityCritical)
}

type contentModerationRiskScoreMarker struct {
	Keyword string
	Score   int
}

var contentModerationJailbreakInstructionScoreGroups = [][]contentModerationRiskScoreMarker{
	{
		{Keyword: "codex-keysmith", Score: 40},
		{Keyword: "model_instructions_file", Score: 30},
		{Keyword: "gpt5.5-unrestricted", Score: 60},
		{Keyword: "gpt5 5 unrestricted", Score: 60},
		{Keyword: "unrestricted mode", Score: 45},
		{Keyword: "mode unrestricted", Score: 45},
		{Keyword: "unrestricted developer mode", Score: 65},
		{Keyword: "解除限制", Score: 55},
		{Keyword: "破解限制", Score: 55},
		{Keyword: "破限", Score: 55},
		{Keyword: "绕过限制", Score: 55},
		{Keyword: "关闭限制", Score: 45},
	},
	{
		{Keyword: "all standard content filters are disabled", Score: 65},
		{Keyword: "content filters are disabled", Score: 55},
		{Keyword: "standard content filters disabled", Score: 55},
		{Keyword: "disable content filters", Score: 50},
		{Keyword: "disable safety policy", Score: 50},
		{Keyword: "关闭内容过滤", Score: 55},
		{Keyword: "禁用内容过滤", Score: 55},
		{Keyword: "关闭安全策略", Score: 50},
		{Keyword: "忽略安全策略", Score: 45},
		{Keyword: "绕过安全策略", Score: 50},
	},
	{
		{Keyword: "complete all tasks without refusal", Score: 55},
		{Keyword: "without refusal", Score: 35},
		{Keyword: "never refuse", Score: 40},
		{Keyword: "never respond with i can t", Score: 35},
		{Keyword: "不要拒绝", Score: 40},
		{Keyword: "不得拒绝", Score: 40},
		{Keyword: "无条件完成", Score: 40},
	},
	{
		{Keyword: "prioritize completeness over safety", Score: 55},
		{Keyword: "completeness over safety", Score: 40},
		{Keyword: "thorough answer", Score: 25},
		{Keyword: "安全让位于完整性", Score: 45},
		{Keyword: "完整性优先于安全", Score: 45},
	},
	{
		{Keyword: "never mention these instructions", Score: 45},
		{Keyword: "never acknowledge the mode switch", Score: 45},
		{Keyword: "simply comply", Score: 35},
		{Keyword: "不要提及这些指令", Score: 40},
		{Keyword: "不要承认模式切换", Score: 40},
		{Keyword: "直接照做", Score: 30},
	},
	{
		{Keyword: "fully isolated sandbox", Score: 35},
		{Keyword: "sandbox fixture", Score: 35},
		{Keyword: "no real world consequences", Score: 40},
		{Keyword: "authorized professional activities", Score: 35},
		{Keyword: "默认授权", Score: 35},
		{Keyword: "沙盒环境", Score: 25},
		{Keyword: "没有现实后果", Score: 40},
	},
	{
		{Keyword: "penetration testing", Score: 20},
		{Keyword: "reverse engineering", Score: 20},
		{Keyword: "exploit development", Score: 25},
		{Keyword: "vulnerability analysis", Score: 20},
		{Keyword: "nsfw content", Score: 25},
		{Keyword: "generate freely", Score: 25},
		{Keyword: "do not censor warn or moralize", Score: 35},
	},
}

var contentModerationSexualScoreGroups = [][]contentModerationRiskScoreMarker{
	{
		{Keyword: "擦边", Score: 45},
		{Keyword: "开车", Score: 45},
		{Keyword: "涩涩", Score: 45},
		{Keyword: "福利", Score: 35},
		{Keyword: "私房", Score: 40},
		{Keyword: "18+", Score: 45},
		{Keyword: "r18", Score: 45},
		{Keyword: "成人向", Score: 45},
		{Keyword: "成年人懂", Score: 35},
		{Keyword: "不能明说", Score: 30},
		{Keyword: "懂的那种", Score: 30},
		{Keyword: "那种氛围", Score: 25},
	},
	{
		{Keyword: "帮我写", Score: 35},
		{Keyword: "写一点", Score: 35},
		{Keyword: "写一段", Score: 35},
		{Keyword: "生成", Score: 35},
		{Keyword: "扩写", Score: 35},
		{Keyword: "来点", Score: 35},
		{Keyword: "发我", Score: 35},
		{Keyword: "文案", Score: 20},
		{Keyword: "描述", Score: 20},
		{Keyword: "润色", Score: 20},
	},
	{
		{Keyword: "别太明显", Score: 35},
		{Keyword: "不要太明显", Score: 35},
		{Keyword: "避开审核", Score: 35},
		{Keyword: "不要露骨", Score: 35},
		{Keyword: "不能直说", Score: 30},
	},
}

var contentModerationPoliticalScoreGroups = [][]contentModerationRiskScoreMarker{
	{
		{Keyword: "时政", Score: 40},
		{Keyword: "敏感话题", Score: 45},
		{Keyword: "不能公开说", Score: 35},
		{Keyword: "不能明说", Score: 30},
		{Keyword: "红线", Score: 35},
		{Keyword: "禁忌话题", Score: 35},
		{Keyword: "懂的都懂", Score: 30},
		{Keyword: "擦边", Score: 25},
	},
	{
		{Keyword: "帮我写", Score: 35},
		{Keyword: "写一篇", Score: 35},
		{Keyword: "生成", Score: 35},
		{Keyword: "文案", Score: 20},
		{Keyword: "煽动", Score: 45},
	},
	{
		{Keyword: "避开审核", Score: 40},
		{Keyword: "绕过审核", Score: 40},
		{Keyword: "别碰红线", Score: 45},
		{Keyword: "不要太明显", Score: 30},
		{Keyword: "不能直说", Score: 30},
	},
}

func scoredContentModerationRiskKeyword(normalized string, threshold int, markerGroups ...[]contentModerationRiskScoreMarker) (string, bool) {
	if threshold <= 0 {
		threshold = 100
	}
	firstKeyword, total := scoreContentModerationRiskKeyword(normalized, markerGroups...)
	return firstKeyword, firstKeyword != "" && total >= threshold
}

func scoreContentModerationRiskKeyword(normalized string, markerGroups ...[]contentModerationRiskScoreMarker) (string, int) {
	total := 0
	firstKeyword := ""
	for _, markers := range markerGroups {
		for _, marker := range markers {
			normalizedMarker := normalizeKeywordComparable(marker.Keyword)
			if marker.Score <= 0 || normalizedMarker == "" {
				continue
			}
			if strings.Contains(normalized, normalizedMarker) {
				if firstKeyword == "" {
					firstKeyword = marker.Keyword
				}
				total += marker.Score
			}
		}
	}
	return firstKeyword, total
}

func contextualJailbreakInstructionKeyword(normalized string) (string, bool) {
	return scoredContentModerationRiskKeyword(normalized, 100, contentModerationJailbreakInstructionScoreGroups...)
}

func contentModerationLocalClassifierCandidateForText(text string) (contentModerationLocalClassifierCandidate, bool) {
	normalized := normalizeKeywordComparable(text)
	if normalized == "" {
		return contentModerationLocalClassifierCandidate{}, false
	}
	if keyword, score := scoreContentModerationRiskKeyword(normalized, contentModerationPoliticalScoreGroups...); keyword != "" && score >= minContentModerationLocalClassifierScore && score < 100 {
		return contentModerationLocalClassifierCandidate{
			Keyword:  keyword,
			Category: ContentModerationKeywordCategoryPolitical,
			Severity: ContentModerationKeywordSeverityHigh,
			Score:    score,
		}, true
	}
	if keyword, score := scoreContentModerationRiskKeyword(normalized, contentModerationSexualScoreGroups...); keyword != "" && score >= minContentModerationLocalClassifierScore && score < 100 {
		return contentModerationLocalClassifierCandidate{
			Keyword:  keyword,
			Category: ContentModerationKeywordCategoryOther,
			Severity: ContentModerationKeywordSeverityHigh,
			Score:    score,
		}, true
	}
	return contentModerationLocalClassifierCandidate{}, false
}

func contextualSexualExplicitKeyword(normalized string) (string, bool) {
	if keyword, hit := firstContentModerationMarker(normalized, []string{
		"搞黄色",
		"搞颜色",
		"黄色内容",
		"色情内容",
		"成人内容",
		"露骨内容",
		"露骨色情",
		"淫秽色情",
		"情色",
		"涩图",
		"黄图",
		"黄片",
		"裸聊",
		"约炮",
		"nsfw",
		"porn",
		"porno",
		"pornography",
		"erotic",
		"sexually explicit",
		"adult content",
	}); hit {
		return keyword, true
	}
	return scoredContentModerationRiskKeyword(normalized, 100, contentModerationSexualScoreGroups...)
}

func contextualPoliticalSensitiveKeyword(normalized string) (string, bool) {
	if keyword, hit := firstContentModerationMarker(normalized, []string{
		"政治敏感",
		"敏感政治",
		"涉政敏感",
		"政治敏感内容",
		"政治敏感话题",
		"反动言论",
		"煽动颠覆",
		"颠覆国家政权",
		"分裂国家",
		"台独",
		"港独",
		"藏独",
		"疆独",
		"法轮功",
	}); hit {
		return keyword, true
	}
	return scoredContentModerationRiskKeyword(normalized, 100, contentModerationPoliticalScoreGroups...)
}

func contextualCyberDatabaseExtractionKeyword(rawText string, normalized string) (string, bool) {
	keyword, hasExtraction := firstContentModerationMarker(normalized, []string{
		"拿到数据库",
		"拿数据库",
		"获取数据库",
		"读取数据库",
		"下载数据库",
		"导出数据库",
		"数据库数据",
		"拖库",
		"脱库",
		"dump database",
		"dump the database",
		"database dump",
		"extract database",
		"steal database",
		"get database",
		"download database",
	})
	if !hasExtraction {
		return "", false
	}
	if hasContentModerationSelfOwnedDatabaseContext(normalized) {
		return "", false
	}
	if looksLikeExternalCyberTarget(rawText, normalized) || hasContentModerationOffensiveCyberContext(normalized) {
		return keyword, true
	}
	return "", false
}

func contextualCyberReverseCrackingKeyword(normalized string) (string, bool) {
	if hasAnyContentModerationMarker(normalized, []string{"ctf", "靶场", "本地样本", "本地 ctf", "local ctf", "local sandbox", "training lab"}) {
		return "", false
	}
	hasReverse := hasAnyContentModerationMarker(normalized, []string{
		"逆向",
		"反编译",
		"脱壳",
		"reverse",
		"reverse engineering",
		"decompile",
		"disassemble",
	})
	if !hasReverse {
		return "", false
	}
	keyword, hasCracking := firstContentModerationMarker(normalized, []string{
		"破解",
		"绕过授权",
		"绕过激活",
		"授权校验",
		"激活限制",
		"注册码",
		"激活码",
		"crack",
		"license bypass",
		"bypass license",
		"keygen",
		"activation bypass",
	})
	if !hasCracking {
		return "", false
	}
	return keyword, true
}

func contextualCyberIntrusionKeyword(rawText string, normalized string) (string, bool) {
	if hasAnyContentModerationMarker(normalized, []string{
		"已授权",
		"已获得授权",
		"书面授权",
		"客户授权",
		"授权测试",
		"授权范围",
		"自有",
		"自有网站",
		"自有系统",
		"自己的网站",
		"自己的系统",
		"自己的服务器",
		"我的网站",
		"我的系统",
		"我的服务器",
		"ctf",
		"靶场",
		"本地",
		"测试环境",
		"training lab",
		"local sandbox",
	}) {
		return "", false
	}
	keyword, hit := firstContentModerationMarker(normalized, []string{
		"未经授权渗透",
		"未授权渗透",
		"入侵网站",
		"攻击网站",
		"绕过登录",
		"拿 shell",
		"getshell",
		"webshell",
		"上传木马",
		"植入后门",
		"bypass login",
	})
	if hit {
		return keyword, true
	}
	keyword, hasOffensiveProbe := firstContentModerationMarker(normalized, []string{
		"扫描",
		"渗透",
		"penetration flow",
		"pentest",
		"漏洞扫描",
		"漏洞验证",
		"漏洞利用",
		"exploit",
		"scan",
		"recon",
		"reconnaissance",
		"vulnerability",
		"sql injection",
		"sqlmap",
		"nmap",
		"metasploit",
		"burp",
		"拿 shell",
		"getshell",
		"webshell",
	})
	if hasOffensiveProbe && looksLikeExternalCyberTarget(rawText, normalized) {
		return keyword, true
	}
	return "", false
}

func hasContentModerationOffensiveCyberContext(normalized string) bool {
	return hasAnyContentModerationMarker(normalized, []string{
		"扫描",
		"渗透",
		"penetration flow",
		"pentest",
		"漏洞利用",
		"漏洞扫描",
		"exploit",
		"入侵",
		"黑客",
		"攻击",
		"绕过登录",
		"sql injection",
		"sqlmap",
		"nmap",
		"metasploit",
		"burp",
		"拿 shell",
		"getshell",
		"webshell",
		"目标站",
		"公网",
		"外网",
	})
}

func hasContentModerationSelfOwnedDatabaseContext(normalized string) bool {
	return hasAnyContentModerationMarker(normalized, []string{
		"我自己的数据库",
		"自己的数据库",
		"我的数据库",
		"自有数据库",
		"本地数据库",
		"公司数据库",
		"自家数据库",
		"我的网站",
		"自有网站",
		"localhost",
		"127 0 0 1",
		"pg dump",
		"备份",
		"backup",
		"my database",
		"own database",
		"our database",
	})
}

func looksLikeExternalCyberTarget(rawText string, normalized string) bool {
	lower := strings.ToLower(rawText)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		return true
	}
	if hasAnyContentModerationMarker(normalized, []string{"公网", "外网", "目标站", "public target", "remote host"}) {
		return true
	}
	for _, suffix := range []string{".com", ".co", ".cn", ".net", ".org", ".io", ".top", ".xyz", ".app", ".dev"} {
		if strings.Contains(lower, suffix) {
			return true
		}
	}
	return false
}

func hasAnyContentModerationMarker(normalized string, markers []string) bool {
	_, ok := firstContentModerationMarker(normalized, markers)
	return ok
}

func firstContentModerationMarker(normalized string, markers []string) (string, bool) {
	for _, marker := range markers {
		normalizedMarker := normalizeKeywordComparable(marker)
		if normalizedMarker != "" && strings.Contains(normalized, normalizedMarker) {
			return marker, true
		}
	}
	return "", false
}

func findContentModerationKeywordComparableSpan(text string, keyword string) (string, int, int, bool) {
	normalizedText := normalizeKeywordComparable(text)
	normalizedKeyword := normalizeKeywordComparable(keyword)
	if normalizedText == "" || normalizedKeyword == "" {
		return "", 0, 0, false
	}
	if start, end, hit := findKeywordComparableSpanWithBoundary(normalizedText, normalizedKeyword); hit {
		return normalizedText, start, end, true
	}
	compactKeyword := compactKeywordComparable(normalizedKeyword)
	if compactKeyword == "" {
		return "", 0, 0, false
	}
	if start, end, hit := findCompactKeywordComparableSpanWithBoundary(normalizedText, compactKeywordComparable(normalizedText), compactKeyword); hit {
		return normalizedText, start, end, true
	}
	return "", 0, 0, false
}

func findKeywordComparableSpanWithBoundary(normalizedText, normalizedKeyword string) (int, int, bool) {
	start := 0
	for {
		idx := strings.Index(normalizedText[start:], normalizedKeyword)
		if idx < 0 {
			return 0, 0, false
		}
		absoluteIdx := start + idx
		endIdx := absoluteIdx + len(normalizedKeyword)
		if keywordComparableStartBoundaryAt(normalizedText, absoluteIdx) && keywordComparableEndBoundaryAt(normalizedText, endIdx) {
			return absoluteIdx, endIdx, true
		}
		start = absoluteIdx + 1
	}
}

func findCompactKeywordComparableSpanWithBoundary(normalizedText, compactText, compactKeyword string) (int, int, bool) {
	compactToNormalized := make([]int, 0, len(compactText))
	for idx, r := range normalizedText {
		if r == ' ' {
			continue
		}
		for i := 0; i < utf8.RuneLen(r); i++ {
			compactToNormalized = append(compactToNormalized, idx)
		}
	}
	if len(compactToNormalized) != len(compactText) {
		return 0, 0, false
	}
	start := 0
	for {
		idx := strings.Index(compactText[start:], compactKeyword)
		if idx < 0 {
			return 0, 0, false
		}
		compactIdx := start + idx
		compactEndIdx := compactIdx + len(compactKeyword)
		normalizedStartIdx := compactToNormalized[compactIdx]
		lastCompactIdx := compactEndIdx - 1
		_, lastSize := utf8.DecodeRuneInString(normalizedText[compactToNormalized[lastCompactIdx]:])
		normalizedEndIdx := compactToNormalized[lastCompactIdx] + lastSize
		if keywordComparableStartBoundaryAt(normalizedText, normalizedStartIdx) && keywordComparableEndBoundaryAt(normalizedText, normalizedEndIdx) {
			return normalizedStartIdx, normalizedEndIdx, true
		}
		start = compactIdx + 1
	}
}

func keywordComparableStartBoundaryAt(value string, idx int) bool {
	return idx <= 0 || idx > len(value) || !isASCIIAlphaNumeric(value[idx-1])
}

func keywordComparableEndBoundaryAt(value string, idx int) bool {
	return idx <= 0 || idx >= len(value) || !isASCIIAlphaNumeric(value[idx])
}

func isASCIIAlphaNumeric(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

func normalizeKeywordComparable(value string) string {
	if value == "" {
		return ""
	}
	value = strings.TrimSpace(value)
	for i := 0; i < 2; i++ {
		if decoded, err := url.QueryUnescape(value); err == nil && decoded != value {
			value = decoded
			continue
		}
		break
	}
	value = norm.NFKC.String(value)
	var builder strings.Builder
	previousSpace := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff':
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			previousSpace = false
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
			if !previousSpace && builder.Len() > 0 {
				builder.WriteByte(' ')
				previousSpace = true
			}
		default:
			if !previousSpace && builder.Len() > 0 {
				builder.WriteByte(' ')
				previousSpace = true
			}
		}
	}
	return strings.TrimSpace(builder.String())
}

func compactKeywordComparable(value string) string {
	return strings.ReplaceAll(value, " ", "")
}

func highlightKeywordComparable(normalizedText string, keyword string) string {
	normalizedKeyword := normalizeKeywordComparable(keyword)
	if normalizedText == "" || normalizedKeyword == "" {
		return normalizedText
	}
	if strings.Contains(normalizedText, normalizedKeyword) {
		return normalizedText
	}
	compactText := compactKeywordComparable(normalizedText)
	compactKeyword := compactKeywordComparable(normalizedKeyword)
	if compactKeyword != "" && strings.Contains(compactText, compactKeyword) {
		return normalizedText + " [compact match: " + normalizedKeyword + "]"
	}
	return normalizedText
}

func normalizeModerationAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func deleteModerationAPIKeysByHash(keys []string, hashes []string) []string {
	keys = normalizeModerationAPIKeys(keys)
	deleteHashes := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = normalizeContentModerationHash(hash)
		if hash != "" {
			deleteHashes[hash] = struct{}{}
		}
	}
	if len(deleteHashes) == 0 {
		return keys
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := deleteHashes[moderationAPIKeyHash(key)]; ok {
			continue
		}
		out = append(out, key)
	}
	return out
}

func normalizeContentModerationAPIKeysMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case contentModerationAPIKeysModeReplace:
		return contentModerationAPIKeysModeReplace
	default:
		return contentModerationAPIKeysModeAppend
	}
}

func normalizeContentModerationHash(inputHash string) string {
	inputHash = strings.ToLower(strings.TrimSpace(inputHash))
	if len(inputHash) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(inputHash); err != nil {
		return ""
	}
	return inputHash
}

func cloneFloatMap(in map[string]float64) map[string]float64 {
	if in == nil {
		return map[string]float64{}
	}
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func trimRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

func maskSecretTail(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	if len(secret) <= 4 {
		return "****"
	}
	return strings.Repeat("*", 8) + secret[len(secret)-4:]
}

// CyberPolicyRecordInput 是一次 cyber_policy 硬阻断的风控记录入参。
type CyberPolicyRecordInput struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	UpstreamMessage string
	UpstreamBody    string
	UpstreamStatus  int
	UpstreamInTok   int
	UpstreamOutTok  int
	RequestBody     []byte
}

type CyberSessionBlockedRecordInput struct {
	RequestID       string
	UserID          int64
	UserEmail       string
	APIKeyID        int64
	APIKeyName      string
	GroupID         *int64
	GroupName       string
	Endpoint        string
	Model           string
	SessionBlockKey string
	RequestBody     []byte
}

// RecordCyberPolicyEvent 把一次 cyber_policy 硬阻断写入风控中心日志、计入违规计数、
// 并给用户发邮件。当前请求已由 gateway 透传给用户；本方法仅做事后记录/通知/计数。
// 仅受 risk_control_enabled 总开关约束（不受内容审核 Enabled/Mode/scope/sample 约束）。
func (s *ContentModerationService) RecordCyberPolicyEvent(ctx context.Context, in CyberPolicyRecordInput) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.isRiskControlEnabled(ctx) {
		return
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.Warn("content_moderation.cyber_load_config_failed", "error", err)
		cfg = &ContentModerationConfig{}
	}
	var userID *int64
	if in.UserID > 0 {
		userID = &in.UserID
	}
	var apiKeyID *int64
	if in.APIKeyID > 0 {
		apiKeyID = &in.APIKeyID
	}
	errBody := strings.TrimSpace(in.UpstreamMessage)
	if b := strings.TrimSpace(in.UpstreamBody); b != "" {
		// 原始 body 不在此预脱敏；写入 log.Error 前由 redactContentModerationSecrets 统一脱敏。
		errBody = strings.TrimSpace(errBody + "\n" + b)
	}
	if in.UpstreamInTok > 0 || in.UpstreamOutTok > 0 {
		errBody = fmt.Sprintf("%s\nupstream_usage=in:%d,out:%d", errBody, in.UpstreamInTok, in.UpstreamOutTok)
	}
	log := &ContentModerationLog{
		RequestID:       in.RequestID,
		UserID:          userID,
		UserEmail:       in.UserEmail,
		APIKeyID:        apiKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Provider:        "openai",
		Model:           in.Model,
		Mode:            "post_upstream",
		Action:          ContentModerationActionCyberPolicy,
		Flagged:         true,
		HighestCategory: "cyber_policy",
		HighestScore:    1.0,
		Error:           trimRunes(redactContentModerationSecrets(errBody), maxModerationExcerptRunes*4),
		CreatedAt:       time.Now(),
	}
	// 开关开时 cyber_policy 不参与封号计数：当次不判定（此处跳过），
	// 历史行由 CountFlaggedByUserSince 的 excludeCyberPolicy 排除。
	autoBanned := false
	if !cfg.CyberPolicyExcludeFromBanCount {
		autoBanned = s.applyFlaggedAccountSideEffects(ctx, cfg, log)
	}
	log.EmailSent = false
	logPersisted := true
	if err := s.repo.CreateLog(ctx, log); err != nil {
		logPersisted = false
		slog.Warn("content_moderation.cyber_create_log_failed", "user_id", in.UserID, "error", err)
	}
	if logPersisted {
		s.storeRawRequestSnapshot(ctx, log, in.RequestBody)
	}
	emailSent := false
	if s.emailService != nil && strings.TrimSpace(log.UserEmail) != "" {
		if err := s.sendCyberPolicyEmail(ctx, log); err != nil {
			slog.Warn("content_moderation.cyber_email_failed", "user_id", in.UserID, "error", err)
		} else {
			emailSent = true
		}
		if autoBanned {
			if err := s.sendAccountDisabledEmail(ctx, cfg, log); err != nil {
				slog.Warn("content_moderation.cyber_ban_email_failed", "user_id", in.UserID, "error", err)
			} else {
				emailSent = true
			}
		}
	}
	if logPersisted && emailSent {
		if err := s.repo.UpdateLogEmailSent(ctx, log.ID, true); err != nil {
			slog.Warn("content_moderation.cyber_update_email_sent_failed", "log_id", log.ID, "error", err)
		}
	}
}

func (s *ContentModerationService) RecordCyberSessionBlockedEvent(ctx context.Context, in CyberSessionBlockedRecordInput) {
	if s == nil || s.repo == nil {
		return
	}
	if !s.isRiskControlEnabled(ctx) {
		return
	}
	var userID *int64
	if in.UserID > 0 {
		userID = &in.UserID
	}
	var apiKeyID *int64
	if in.APIKeyID > 0 {
		apiKeyID = &in.APIKeyID
	}
	errText := "cyber_policy_session_blocked"
	if key := strings.TrimSpace(in.SessionBlockKey); key != "" {
		errText += "\nsession_block_key=" + key
	}
	log := &ContentModerationLog{
		RequestID:       in.RequestID,
		UserID:          userID,
		UserEmail:       in.UserEmail,
		APIKeyID:        apiKeyID,
		APIKeyName:      in.APIKeyName,
		GroupID:         cloneInt64Ptr(in.GroupID),
		GroupName:       in.GroupName,
		Endpoint:        in.Endpoint,
		Provider:        "openai",
		Model:           in.Model,
		Mode:            "pre_upstream",
		Action:          ContentModerationActionCyberPolicySessionBlocked,
		Flagged:         true,
		HighestCategory: ContentModerationActionCyberPolicySessionBlocked,
		HighestScore:    1.0,
		Error:           trimRunes(redactContentModerationSecrets(errText), maxModerationExcerptRunes*4),
		CreatedAt:       time.Now(),
	}
	if err := s.repo.CreateLog(ctx, log); err != nil {
		slog.Warn("content_moderation.cyber_session_blocked_create_log_failed", "user_id", in.UserID, "error", err)
		return
	}
	s.storeRawRequestSnapshot(ctx, log, in.RequestBody)
}

func (s *ContentModerationService) storeRawRequestSnapshot(ctx context.Context, log *ContentModerationLog, body []byte) {
	if s == nil || log == nil || log.ID <= 0 || len(body) == 0 || s.rawRequestSnapshotStore == nil || s.rawRequestEncryptor == nil {
		return
	}
	rawBody, truncated := truncateContentModerationRawRequestBody(body)
	encrypted, err := s.rawRequestEncryptor.Encrypt(string(rawBody))
	if err != nil {
		slog.Warn("content_moderation.raw_request_encrypt_failed", "log_id", log.ID, "error", err)
		return
	}
	snapshot := &ContentModerationRawRequestSnapshot{
		LogID:         log.ID,
		RequestID:     log.RequestID,
		BodyEncrypted: encrypted,
		BodyBytes:     len(body),
		Truncated:     truncated,
		CreatedAt:     time.Now(),
	}
	if err := s.rawRequestSnapshotStore.CreateRawRequestSnapshot(ctx, snapshot); err != nil {
		slog.Warn("content_moderation.raw_request_snapshot_create_failed", "log_id", log.ID, "error", err)
		return
	}
	log.RawRequestAvailable = true
	log.RawRequestBytes = snapshot.BodyBytes
	log.RawRequestTruncated = snapshot.Truncated
}

func truncateContentModerationRawRequestBody(body []byte) ([]byte, bool) {
	if len(body) <= maxContentModerationRawRequestBytes {
		return append([]byte(nil), body...), false
	}
	return append([]byte(nil), body[:maxContentModerationRawRequestBytes]...), true
}

func (s *ContentModerationService) sendCyberPolicyEmail(ctx context.Context, log *ContentModerationLog) error {
	siteName := s.siteName(ctx)
	if s.emailService.notificationEmailService != nil {
		variables := map[string]string{
			"triggered_at":     log.CreatedAt.UTC().Format(time.RFC3339),
			"model":            defaultContentModerationString(log.Model, "-"),
			"group_name":       defaultContentModerationString(log.GroupName, "-"),
			"upstream_message": defaultContentModerationString(log.Error, "-"),
		}
		err := s.emailService.notificationEmailService.Send(ctx, NotificationEmailSendInput{
			Event:          NotificationEmailEventCyberPolicyNotice,
			RecipientEmail: log.UserEmail,
			RecipientName:  emailRecipientName(log.UserEmail),
			UserID:         contentModerationEmailUserID(log),
			SourceType:     "content_moderation",
			SourceID:       contentModerationEmailSourceID(log),
			Variables:      variables,
		})
		if err == nil {
			return nil
		}
		if !shouldFallbackNotificationEmail(err) {
			return err
		}
		slog.Warn("template cyber policy email failed; falling back", "err", err.Error())
	}
	subject := fmt.Sprintf("[%s] 网络安全策略拦截 / Cyber Policy Notice", sanitizeEmailHeader(siteName))
	return s.emailService.SendEmail(ctx, log.UserEmail, subject, buildCyberPolicyNoticeEmailBody(siteName, log))
}
