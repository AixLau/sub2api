-- Reliable outbox for content moderation side effects.

ALTER TABLE content_moderation_logs
  ADD COLUMN IF NOT EXISTS decision_id VARCHAR(128) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_logs_decision_id
  ON content_moderation_logs (decision_id)
  WHERE decision_id <> '';

CREATE TABLE IF NOT EXISTS content_moderation_outbox (
    id              BIGSERIAL PRIMARY KEY,
    decision_id     VARCHAR(128) NOT NULL,
    event_type      VARCHAR(64) NOT NULL,
    event_key       VARCHAR(64) NOT NULL DEFAULT '',
    priority        VARCHAR(16) NOT NULL DEFAULT 'strong',
    status          VARCHAR(24) NOT NULL DEFAULT 'pending',
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    retry_count     INT NOT NULL DEFAULT 0,
    max_retries     INT NOT NULL DEFAULT 20,
    next_retry_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_until    TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    succeeded_at    TIMESTAMPTZ,
    dead_letter_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_moderation_outbox_priority_check
      CHECK (priority IN ('strong', 'weak')),
    CONSTRAINT content_moderation_outbox_status_check
      CHECK (status IN ('pending', 'processing', 'retry', 'succeeded', 'dead_letter'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_content_moderation_outbox_decision_event
  ON content_moderation_outbox (decision_id, event_type, event_key);

CREATE INDEX IF NOT EXISTS idx_content_moderation_outbox_due
  ON content_moderation_outbox (status, next_retry_at, id)
  WHERE status IN ('pending', 'retry', 'processing');

CREATE INDEX IF NOT EXISTS idx_content_moderation_outbox_dead_letter
  ON content_moderation_outbox (dead_letter_at DESC)
  WHERE status = 'dead_letter';
