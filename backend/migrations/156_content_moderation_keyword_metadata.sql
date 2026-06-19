-- 内容审计关键词命中元信息。
-- 旧日志不回填；后续 keyword_block 命中会写入 matched_keyword/category/severity 便于排障和调优。

ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS matched_keyword VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS keyword_category VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE content_moderation_logs ADD COLUMN IF NOT EXISTS keyword_severity VARCHAR(32) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_matched_keyword
  ON content_moderation_logs (matched_keyword);

CREATE INDEX IF NOT EXISTS idx_content_moderation_logs_keyword_category_created_at
  ON content_moderation_logs (keyword_category, created_at DESC);
