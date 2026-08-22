package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ContentModerationHandler struct {
	service *service.ContentModerationService
}

func NewContentModerationHandler(svc *service.ContentModerationService) *ContentModerationHandler {
	return &ContentModerationHandler{service: svc}
}

type contentModerationConfigRequest struct {
	MaxRequestBodyMiB        *int    `json:"max_request_body_mib"`
	InflightMemoryBudgetMiB  *int    `json:"inflight_memory_budget_mib"`
	RequestMemoryMultiplier  *int    `json:"request_memory_multiplier"`
	MinimumRequestChargeKiB  *int    `json:"minimum_request_charge_kib"`
	SmallRequestThresholdMiB *int    `json:"small_request_threshold_mib"`
	SmallRequestReserveMiB   *int    `json:"small_request_reserve_mib"`
	AdmissionWaitTimeoutMS   *int    `json:"admission_wait_timeout_ms"`
	ImageAuditMaxConcurrency *int    `json:"image_audit_max_concurrency"`
	RequestAuditTimeoutMS    *int    `json:"request_audit_timeout_ms"`
	Enabled                  *bool   `json:"enabled"`
	Mode                     *string `json:"mode"`
	Provider                 *string `json:"provider"`
	BaseURL                  *string `json:"base_url"`
	Model                    *string `json:"model"`
	// null 不修改；0 清除（直连）；>0 指定审计请求代理。
	ProxyID                 *int64              `json:"proxy_id"`
	PassCacheEnabled        *bool               `json:"pass_cache_enabled"`
	PassCacheTTLSeconds     *int                `json:"pass_cache_ttl_seconds"`
	DecisionCacheEnabled    *bool               `json:"decision_cache_enabled"`
	DecisionCacheTTLSeconds *int                `json:"decision_cache_ttl_seconds"`
	CandidateFragmentRunes  *int                `json:"candidate_fragment_runes"`
	APIKey                  *string             `json:"api_key"`
	APIKeys                 *[]string           `json:"api_keys"`
	APIKeysMode             string              `json:"api_keys_mode"`
	DeleteAPIKeyHashes      *[]string           `json:"delete_api_key_hashes"`
	ClearAPIKey             bool                `json:"clear_api_key"`
	TimeoutMS               *int                `json:"timeout_ms"`
	SampleRate              *int                `json:"sample_rate"`
	AllGroups               *bool               `json:"all_groups"`
	GroupIDs                *[]int64            `json:"group_ids"`
	AccountScope            *string             `json:"account_scope"`
	AccountIDs              *[]int64            `json:"account_ids"`
	RecordNonHits           *bool               `json:"record_non_hits"`
	AuditScope              *string             `json:"audit_scope"`
	StoreInputExcerpt       *bool               `json:"store_input_excerpt"`
	SearchInputExcerpt      *bool               `json:"search_input_excerpt"`
	Thresholds              *map[string]float64 `json:"thresholds"`
	WorkerCount             *int                `json:"worker_count"`
	QueueSize               *int                `json:"queue_size"`
	BlockStatus             *int                `json:"block_status"`
	BlockMessage            *string             `json:"block_message"`
	EmailOnHit              *bool               `json:"email_on_hit"`
	AutoBanEnabled          *bool               `json:"auto_ban_enabled"`
	BanThreshold            *int                `json:"ban_threshold"`
	ViolationWindowHours    *int                `json:"violation_window_hours"`
	// cyber_policy 命中是否排除出自动封号计数；前端 RiskControlView 已发送该字段，
	// service.UpdateContentModerationConfigInput 已支持，此前 handler 层缺透传导致开关静默失效。
	CyberPolicyExcludeFromBanCount *bool                                          `json:"cyber_policy_exclude_from_ban_count"`
	RetryCount                     *int                                           `json:"retry_count"`
	HitRetentionDays               *int                                           `json:"hit_retention_days"`
	NonHitRetentionDays            *int                                           `json:"non_hit_retention_days"`
	PreHashCheckEnabled            *bool                                          `json:"pre_hash_check_enabled"`
	BlockedKeywords                *[]string                                      `json:"blocked_keywords"`
	KeywordRules                   *[]service.ContentModerationKeywordRule        `json:"keyword_rules"`
	KeywordBlockingMode            *string                                        `json:"keyword_blocking_mode"`
	EngineMode                     *string                                        `json:"engine_mode"`
	PromptFilterMode               *string                                        `json:"prompt_filter_mode"`
	PromptFilterThreshold          *int                                           `json:"prompt_filter_threshold"`
	PromptFilterStrictThreshold    *int                                           `json:"prompt_filter_strict_threshold"`
	SemanticReview                 *service.ContentModerationSemanticReviewConfig `json:"semantic_review"`
	ModelFilter                    *service.ContentModerationModelFilter          `json:"model_filter"`
	FailStrategy                   *service.ContentModerationFailStrategy         `json:"fail_strategy"`
}

type contentModerationAPIKeyTestRequest struct {
	APIKeys   []string `json:"api_keys"`
	Provider  string   `json:"provider"`
	BaseURL   string   `json:"base_url"`
	Model     string   `json:"model"`
	TimeoutMS int      `json:"timeout_ms"`
	ProxyID   *int64   `json:"proxy_id"`
	Prompt    string   `json:"prompt"`
	Images    []string `json:"images"`
}

type contentModerationHashRequest struct {
	InputHash string `json:"input_hash"`
}

type contentModerationKeywordTestRequest struct {
	Prompt string `json:"prompt"`
}

type contentModerationLogReviewRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (h *ContentModerationHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) GetSemanticReviewModels(c *gin.Context) {
	models, err := h.service.GetSemanticReviewModels(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"models": models})
}

func (h *ContentModerationHandler) UpdateConfig(c *gin.Context) {
	var req contentModerationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	auditAfter := map[string]any{"changed_fields": contentModerationConfigChangedFields(req)}
	emitContentModerationAdminAudit(c, "risk_control.config.update", "content_moderation_config", "global", "attempt", auditAfter, nil)
	cfg, err := h.service.UpdateConfig(c.Request.Context(), service.UpdateContentModerationConfigInput{
		MaxRequestBodyMiB:              req.MaxRequestBodyMiB,
		InflightMemoryBudgetMiB:        req.InflightMemoryBudgetMiB,
		RequestMemoryMultiplier:        req.RequestMemoryMultiplier,
		MinimumRequestChargeKiB:        req.MinimumRequestChargeKiB,
		SmallRequestThresholdMiB:       req.SmallRequestThresholdMiB,
		SmallRequestReserveMiB:         req.SmallRequestReserveMiB,
		AdmissionWaitTimeoutMS:         req.AdmissionWaitTimeoutMS,
		ImageAuditMaxConcurrency:       req.ImageAuditMaxConcurrency,
		RequestAuditTimeoutMS:          req.RequestAuditTimeoutMS,
		Enabled:                        req.Enabled,
		Mode:                           req.Mode,
		Provider:                       req.Provider,
		BaseURL:                        req.BaseURL,
		Model:                          req.Model,
		ProxyID:                        req.ProxyID,
		PassCacheEnabled:               req.PassCacheEnabled,
		PassCacheTTLSeconds:            req.PassCacheTTLSeconds,
		DecisionCacheEnabled:           req.DecisionCacheEnabled,
		DecisionCacheTTLSeconds:        req.DecisionCacheTTLSeconds,
		CandidateFragmentRunes:         req.CandidateFragmentRunes,
		APIKey:                         req.APIKey,
		APIKeys:                        req.APIKeys,
		APIKeysMode:                    req.APIKeysMode,
		DeleteAPIKeyHashes:             req.DeleteAPIKeyHashes,
		ClearAPIKey:                    req.ClearAPIKey,
		TimeoutMS:                      req.TimeoutMS,
		SampleRate:                     req.SampleRate,
		AllGroups:                      req.AllGroups,
		GroupIDs:                       req.GroupIDs,
		AccountScope:                   req.AccountScope,
		AccountIDs:                     req.AccountIDs,
		RecordNonHits:                  req.RecordNonHits,
		AuditScope:                     req.AuditScope,
		StoreInputExcerpt:              req.StoreInputExcerpt,
		SearchInputExcerpt:             req.SearchInputExcerpt,
		Thresholds:                     req.Thresholds,
		WorkerCount:                    req.WorkerCount,
		QueueSize:                      req.QueueSize,
		BlockStatus:                    req.BlockStatus,
		BlockMessage:                   req.BlockMessage,
		EmailOnHit:                     req.EmailOnHit,
		AutoBanEnabled:                 req.AutoBanEnabled,
		BanThreshold:                   req.BanThreshold,
		ViolationWindowHours:           req.ViolationWindowHours,
		CyberPolicyExcludeFromBanCount: req.CyberPolicyExcludeFromBanCount,
		RetryCount:                     req.RetryCount,
		HitRetentionDays:               req.HitRetentionDays,
		NonHitRetentionDays:            req.NonHitRetentionDays,
		PreHashCheckEnabled:            req.PreHashCheckEnabled,
		BlockedKeywords:                req.BlockedKeywords,
		KeywordRules:                   req.KeywordRules,
		KeywordBlockingMode:            req.KeywordBlockingMode,
		EngineMode:                     req.EngineMode,
		PromptFilterMode:               req.PromptFilterMode,
		PromptFilterThreshold:          req.PromptFilterThreshold,
		PromptFilterStrictThreshold:    req.PromptFilterStrictThreshold,
		SemanticReview:                 req.SemanticReview,
		ModelFilter:                    req.ModelFilter,
		FailStrategy:                   req.FailStrategy,
	})
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.config.update", "content_moderation_config", "global", "failed", auditAfter, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.config.update", "content_moderation_config", "global", "success", auditAfter, nil)
	response.Success(c, cfg)
}

func (h *ContentModerationHandler) TestAPIKeys(c *gin.Context) {
	var req contentModerationAPIKeyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.TestAPIKeys(c.Request.Context(), service.TestContentModerationAPIKeysInput{
		APIKeys:   req.APIKeys,
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		Model:     req.Model,
		TimeoutMS: req.TimeoutMS,
		ProxyID:   req.ProxyID,
		Prompt:    req.Prompt,
		Images:    req.Images,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) TestKeywords(c *gin.Context) {
	var req contentModerationKeywordTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.TestKeywords(c.Request.Context(), req.Prompt)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetStatus(c *gin.Context) {
	status, err := h.service.GetStatus(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *ContentModerationHandler) GetMetrics(c *gin.Context) {
	h.service.ModerationMetricsHandler().ServeHTTP(c.Writer, c.Request)
}

func (h *ContentModerationHandler) ListLogs(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.ContentModerationLogFilter{
		Pagination: pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortOrder: pagination.SortOrderDesc,
		},
		Result:         c.Query("result"),
		DecisionSource: c.Query("decision_source"),
		ReviewStatus:   c.Query("review_status"),
		Endpoint:       c.Query("endpoint"),
		Search:         c.Query("search"),
	}
	if raw := strings.TrimSpace(c.Query("group_id")); raw != "" {
		groupID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || groupID <= 0 {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		filter.GroupID = &groupID
	}
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, _, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid from")
			return
		}
		filter.From = &t
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, dateOnly, err := parseContentModerationDate(raw)
		if err != nil {
			response.BadRequest(c, "Invalid to")
			return
		}
		if dateOnly {
			t = t.Add(24*time.Hour - time.Nanosecond)
		}
		filter.To = &t
	}
	items, pageResult, err := h.service.ListLogs(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, pageResult.Total, pageResult.Page, pageResult.PageSize)
}

func (h *ContentModerationHandler) UnbanUser(c *gin.Context) {
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("user_id")), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user_id")
		return
	}
	targetID := strconv.FormatInt(userID, 10)
	emitContentModerationAdminAudit(c, "risk_control.users.unban", "content_moderation_user_ban", targetID, "attempt", nil, nil)
	result, err := h.service.UnbanUser(c.Request.Context(), userID)
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.users.unban", "content_moderation_user_ban", targetID, "failed", nil, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.users.unban", "content_moderation_user_ban", targetID, "success", map[string]any{"status": result.Status}, nil)
	response.Success(c, result)
}

func (h *ContentModerationHandler) ReviewLog(c *gin.Context) {
	logID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || logID <= 0 {
		response.BadRequest(c, "Invalid log id")
		return
	}
	var req contentModerationLogReviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	targetID := strconv.FormatInt(logID, 10)
	auditAfter := map[string]any{
		"review_status":        strings.TrimSpace(req.Status),
		"review_note_supplied": strings.TrimSpace(req.Note) != "",
	}
	emitContentModerationAdminAudit(c, "risk_control.logs.review", "content_moderation_log", targetID, "attempt", auditAfter, nil)
	var reviewerID int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		reviewerID = subject.UserID
	}
	result, err := h.service.ReviewLog(c.Request.Context(), logID, service.ContentModerationLogReviewInput{
		Status:     req.Status,
		Note:       req.Note,
		ReviewedBy: reviewerID,
	})
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.logs.review", "content_moderation_log", targetID, "failed", auditAfter, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.logs.review", "content_moderation_log", targetID, "success", auditAfter, nil)
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetRawRequestSnapshot(c *gin.Context) {
	logID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || logID <= 0 {
		response.BadRequest(c, "Invalid log id")
		return
	}
	result, err := h.service.GetRawRequestSnapshot(c.Request.Context(), logID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) GetEvidenceSnapshot(c *gin.Context) {
	logID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || logID <= 0 {
		response.BadRequest(c, "Invalid log id")
		return
	}
	result, err := h.service.GetEvidenceSnapshot(c.Request.Context(), logID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *ContentModerationHandler) DeleteFlaggedHash(c *gin.Context) {
	var req contentModerationHashRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.hash.delete", "content_moderation_hash", "single", "attempt", nil, nil)
	result, err := h.service.DeleteFlaggedInputHash(c.Request.Context(), req.InputHash)
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.hash.delete", "content_moderation_hash", "single", "failed", nil, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.hash.delete", "content_moderation_hash", "single", "success", map[string]any{"deleted": result.Deleted}, nil)
	response.Success(c, result)
}

func (h *ContentModerationHandler) ClearFlaggedHashes(c *gin.Context) {
	emitContentModerationAdminAudit(c, "risk_control.hash.clear_all", "content_moderation_hash", "all", "attempt", nil, nil)
	result, err := h.service.ClearFlaggedInputHashes(c.Request.Context())
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.hash.clear_all", "content_moderation_hash", "all", "failed", nil, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.hash.clear_all", "content_moderation_hash", "all", "success", map[string]any{"deleted_count": result.Deleted}, nil)
	response.Success(c, result)
}

func (h *ContentModerationHandler) ListOutboxDeadLetters(c *gin.Context) {
	limit := 50
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "Invalid limit")
			return
		}
		limit = parsed
	}
	items, err := h.service.ListContentModerationOutboxDeadLetters(c.Request.Context(), limit)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *ContentModerationHandler) ReplayOutboxDeadLetter(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid dead-letter id")
		return
	}
	targetID := strconv.FormatInt(id, 10)
	emitContentModerationAdminAudit(c, "risk_control.outbox.replay", "content_moderation_outbox_event", targetID, "attempt", nil, nil)
	replayed, err := h.service.ReplayContentModerationOutboxDeadLetter(c.Request.Context(), id)
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.outbox.replay", "content_moderation_outbox_event", targetID, "failed", nil, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.outbox.replay", "content_moderation_outbox_event", targetID, "success", map[string]any{"replayed": replayed}, nil)
	response.Success(c, gin.H{"replayed": replayed})
}

func (h *ContentModerationHandler) CleanupOutbox(c *gin.Context) {
	emitContentModerationAdminAudit(c, "risk_control.outbox.cleanup", "content_moderation_outbox", "expired_succeeded", "attempt", nil, nil)
	deleted, err := h.service.CleanupContentModerationOutbox(c.Request.Context())
	if err != nil {
		emitContentModerationAdminAudit(c, "risk_control.outbox.cleanup", "content_moderation_outbox", "expired_succeeded", "failed", nil, err)
		response.ErrorFrom(c, err)
		return
	}
	emitContentModerationAdminAudit(c, "risk_control.outbox.cleanup", "content_moderation_outbox", "expired_succeeded", "success", map[string]any{"deleted_count": deleted}, nil)
	response.Success(c, gin.H{"deleted": deleted})
}

func parseContentModerationDate(raw string) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, false, nil
	}
	t, err := time.Parse("2006-01-02", raw)
	return t, err == nil, err
}
