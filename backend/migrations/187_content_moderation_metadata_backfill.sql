-- One-time, auditable repair for legacy audit rows written before metadata had
-- its own column. This migration intentionally recognizes only the metadata
-- shapes emitted by the moderation service; other JSON errors remain errors.
CREATE TABLE IF NOT EXISTS content_moderation_metadata_backfill_audit (
    migration_name       VARCHAR(128) PRIMARY KEY,
    started_at            TIMESTAMPTZ NOT NULL,
    completed_at          TIMESTAMPTZ,
    scanned_count         BIGINT NOT NULL DEFAULT 0,
    migrated_count        BIGINT NOT NULL DEFAULT 0,
    skipped_count         BIGINT NOT NULL DEFAULT 0,
    invalid_json_count    BIGINT NOT NULL DEFAULT 0,
    migration_version     VARCHAR(32) NOT NULL DEFAULT 'v1'
);

DO $$
DECLARE
    v_migration_name CONSTANT VARCHAR(128) := '187_content_moderation_metadata_backfill.sql';
    log_row RECORD;
    v_metadata JSONB;
    v_started_at TIMESTAMPTZ := clock_timestamp();
    v_scanned_count BIGINT := 0;
    v_migrated_count BIGINT := 0;
    v_skipped_count BIGINT := 0;
    v_invalid_json_count BIGINT := 0;
BEGIN
    INSERT INTO content_moderation_metadata_backfill_audit (migration_name, started_at)
    VALUES (v_migration_name, v_started_at)
    ON CONFLICT (migration_name) DO NOTHING;

    FOR log_row IN
        SELECT id, error AS legacy_error
        FROM content_moderation_logs
        WHERE error <> ''
        ORDER BY id
    LOOP
        v_scanned_count := v_scanned_count + 1;
        BEGIN
            v_metadata := log_row.legacy_error::jsonb;
        EXCEPTION WHEN others THEN
            v_invalid_json_count := v_invalid_json_count + 1;
            CONTINUE;
        END;

        IF jsonb_typeof(v_metadata) <> 'object'
           OR v_metadata ? 'availability_failure'
           OR v_metadata ? 'error'
           OR (v_metadata ? 'extraction_complete' AND v_metadata->>'extraction_complete' = 'false')
           OR NOT (
               (v_metadata ? 'semantic_review_verdict' AND v_metadata ? 'semantic_review_model')
               OR (v_metadata ? 'selection_schema_version' AND v_metadata ? 'candidate_kind')
               OR (v_metadata ? 'engine_mode' AND v_metadata ? 'keyword_blocking_mode')
               OR (v_metadata ? 'prompt_filter_source_revision' AND v_metadata ? 'prompt_filter_score')
           )
        THEN
            v_skipped_count := v_skipped_count + 1;
            CONTINUE;
        END IF;

        UPDATE content_moderation_logs AS l
        SET metadata = v_metadata,
            error = ''
        WHERE l.id = log_row.id
          AND l.error = log_row.legacy_error;

        IF FOUND THEN
            v_migrated_count := v_migrated_count + 1;
        ELSE
            v_skipped_count := v_skipped_count + 1;
        END IF;
    END LOOP;

    UPDATE content_moderation_metadata_backfill_audit AS a
    SET completed_at = clock_timestamp(),
        scanned_count = v_scanned_count,
        migrated_count = v_migrated_count,
        skipped_count = v_skipped_count,
        invalid_json_count = v_invalid_json_count
    WHERE a.migration_name = v_migration_name;
END $$;
