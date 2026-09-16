-- Persist the enforcement outcome separately from the content verdict.
--
-- action records what the moderation pipeline concluded about the content
-- (allow / review / reject / block). It does not record what the gateway did
-- with the request: an observe-mode semantic reject logs
-- action = 'semantic_review_reject' while the request is forwarded, and the admin
-- UI used to re-derive "blocked" from the action alone, so an observed request
-- was displayed as if it had been blocked. Storing the outcome explicitly lets an
-- audit record state what actually happened.
--
-- ''      the row predates this column
-- allowed the request was forwarded
-- blocked the request was rejected (including fail-closed reviewer unavailability)
-- error   a technical failure that failed open and forwarded the request
ALTER TABLE content_moderation_logs
    ADD COLUMN IF NOT EXISTS enforcement TEXT NOT NULL DEFAULT '';

ALTER TABLE content_moderation_logs
    DROP CONSTRAINT IF EXISTS content_moderation_logs_enforcement_values_check;

ALTER TABLE content_moderation_logs
    ADD CONSTRAINT content_moderation_logs_enforcement_values_check
    CHECK (enforcement IN ('', 'allowed', 'blocked', 'error')) NOT VALID;

ALTER TABLE content_moderation_logs
    VALIDATE CONSTRAINT content_moderation_logs_enforcement_values_check;