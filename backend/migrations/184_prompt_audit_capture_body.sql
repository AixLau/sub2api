-- Full request bodies retained for explicitly selected users only.
ALTER TABLE prompt_audit_events
    ADD COLUMN IF NOT EXISTS full_request_body TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_prompt_audit_events_capture_user_created
    ON prompt_audit_events (user_id, created_at DESC, id DESC);
