-- Digest of the exact text persisted in submitted_text.
--
-- submitted_text follows the store_input_excerpt privacy gate, so it can be
-- absent even when the record is otherwise complete. This digest is written under
-- the same gate, so it never widens the privacy surface: with the gate off, no
-- text and no digest are retained.
--
-- It answers a different question from reviewed_text_sha256 in log metadata, which
-- the asynchronous dead-letter replay computes over the pre-router decrypted
-- payload. This column hashes the post-redaction, post-cap text the semantic
-- reviewer actually received, matching submitted_text and submitted_runes.
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS submitted_text_sha256 TEXT NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_submitted_text_sha256_format_check;

ALTER TABLE content_moderation_logs
    ADD CONSTRAINT content_moderation_logs_submitted_text_sha256_format_check
    CHECK (submitted_text_sha256 = '' OR submitted_text_sha256 ~ '^[0-9a-f]{64}$') NOT VALID;

ALTER TABLE content_moderation_logs
    VALIDATE CONSTRAINT content_moderation_logs_submitted_text_sha256_format_check;