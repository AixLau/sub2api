package securityaudit

import (
	"context"
	"errors"
	"strings"
)

type Enqueuer struct {
	config  ConfigStore
	repo    JobRepository
	payload PayloadStore
	metrics Metrics
}

func NewEnqueuer(config ConfigStore, repo JobRepository, payload PayloadStore, metrics ...Metrics) *Enqueuer {
	var metric Metrics
	if len(metrics) > 0 {
		metric = metrics[0]
	}
	return &Enqueuer{config: config, repo: repo, payload: payload, metrics: metric}
}

func (e *Enqueuer) Enqueue(ctx context.Context, req Request) error {
	if e == nil || e.config == nil || e.repo == nil || e.payload == nil {
		return errors.New("prompt audit enqueuer unavailable")
	}
	cfg, ok := e.config.Active()
	if shouldCaptureNonClient(req.UserAgent) {
		snapshot, err := ExtractPromptSnapshot(req)
		if errors.Is(err, ErrNoPromptText) {
			return nil
		}
		if err != nil {
			e.recordDropped()
			return err
		}
		if _, err := e.repo.RecordCapture(ctx, snapshot, 0, 100); err != nil {
			e.recordDropped()
			return err
		}
		return nil
	}
	baseFields := requestLogFields(req)
	if !ok || (cfg.EffectiveMode() != ModeAsync && !cfg.CapturesUser(req)) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "mode_not_async"}))
		return nil
	}
	baseFields["config_version"] = cfg.ConfigVersion
	if cfg.CapturesUser(req) {
		snapshot, err := ExtractPromptSnapshot(req)
		if errors.Is(err, ErrNoPromptText) {
			return nil
		}
		if err != nil {
			e.recordDropped()
			return err
		}
		if _, err := e.repo.RecordCapture(ctx, snapshot, cfg.ConfigVersion, cfg.CaptureMaxRecords); err != nil {
			e.recordDropped()
			LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "capture_record_failed"}))
			return err
		}
		LogInfo(EventJobEnqueued, mergeLogFields(baseFields, map[string]any{"status": "captured", "capture_max_records": cfg.CaptureMaxRecords}))
		return nil
	}
	if !cfg.IncludesGroup(req.GroupID) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "group_out_of_scope"}))
		return nil
	}
	if len(cfg.EnabledEndpoints()) == 0 {
		e.recordDropped()
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "no_enabled_endpoint"}))
		return nil
	}
	snapshot, err := ExtractPromptSnapshot(req)
	if errors.Is(err, ErrNoPromptText) {
		LogInfo(EventEnqueueSkipped, mergeLogFields(baseFields, map[string]any{"status": "skipped", "error_code": "no_user_text"}))
		return nil
	}
	if err != nil {
		e.recordDropped()
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{"status": "dropped", "error_code": "snapshot_invalid"}))
		return nil
	}
	job, err := e.repo.CreateStagingWithCapacity(ctx, snapshot.Redacted(), cfg.ConfigVersion, cfg.MaxAttempts, cfg.QueueCapacity)
	if err != nil {
		code := "database_unavailable"
		if errors.Is(err, ErrQueueFull) {
			code = "queue_full"
		}
		if errors.Is(err, ErrQueueAdmissionBusy) {
			code = "queue_admission_busy"
		}
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"queue_capacity": cfg.QueueCapacity, "status": "dropped", "error_code": code,
		}))
		e.recordDropped()
		return err
	}
	if err := e.payload.Set(ctx, job.ID, snapshot.ScanText, DefaultPayloadTTL); err != nil {
		_ = e.repo.MarkStagingFailed(ctx, job.ID, "payload_store_failed", "payload store unavailable")
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"job_id": job.ID, "status": "dropped", "error_code": "payload_store_failed",
		}))
		e.recordDropped()
		return err
	}
	if err := e.repo.PublishQueued(ctx, job.ID); err != nil {
		_ = e.payload.Delete(ctx, job.ID)
		_ = e.repo.MarkStagingFailed(ctx, job.ID, "queue_publish_failed", "queue publish failed")
		LogWarn(EventEnqueueDropped, mergeLogFields(baseFields, map[string]any{
			"job_id": job.ID, "status": "dropped", "error_code": "queue_publish_failed",
		}))
		e.recordDropped()
		return err
	}
	LogInfo(EventJobEnqueued, mergeLogFields(baseFields, map[string]any{
		"job_id":         job.ID,
		"queue_capacity": cfg.QueueCapacity, "status": "queued",
	}))
	if e.metrics != nil {
		e.metrics.IncEnqueued()
	}
	return nil
}

func shouldCaptureNonClient(userAgent string) bool {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return false
	}
	// Agent products use a wide range of UA spellings (hyphen, underscore,
	// product name, or an SDK name). Keep this list deliberately explicit so a
	// generic HTTP client is still captured while known coding agents are not.
	for _, marker := range []string{
		"codex", "workbuddy", "zcode", "小龙虾", "lobster",
		"claude-code", "claude_code", "claudecode", "cursor", "windsurf",
		"cline", "roo-cline", "roo_code", "roo-code", "aider", "continue",
		"opencode", "gemini-cli", "gemini_cli", "amazon-q", "amazon_q",
		"q-developer", "q_developer", "kiro", "goose", "openhands",
		"openhands-agent", "swe-agent", "swe_agent", "devin", "replit-agent",
		"replit_agent", "bolt.new", "lovable", "v0.dev", "copilot",
	} {
		if strings.Contains(ua, marker) {
			return false
		}
	}
	for _, marker := range []string{"node", "axios", "python", "go-http-client", "curl", "java", "ruby", "php", "httpclient"} {
		if strings.Contains(ua, marker) {
			return true
		}
	}
	return false
}

func (e *Enqueuer) recordDropped() {
	if e != nil && e.metrics != nil {
		e.metrics.IncDropped()
	}
}
