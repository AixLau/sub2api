-- 内容审计关键词上下文与人工复核元信息。
-- 旧日志不回填；后续 keyword_review/keyword_block 会写入这些字段。

ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS keyword_action VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS effective_keyword_action VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS risk_context_type VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS risk_context_reason VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS review_status VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS review_note TEXT NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS reviewed_by BIGINT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_review_status_created_at
  ON content_moderation_logs (review_status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_risk_context_created_at
  ON content_moderation_logs (risk_context_type, created_at DESC);
