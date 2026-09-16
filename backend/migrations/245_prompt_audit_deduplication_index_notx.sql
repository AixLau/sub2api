-- Keep admission lookups fast even when a prompt has many historical jobs.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_prompt_audit_jobs_deduplication
    ON prompt_audit_jobs (prompt_hash, config_version, id DESC)
    WHERE execution_mode = 'async_audit'
      AND (status IN ('staging', 'queued', 'processing', 'retry')
           OR (status = 'done' AND attempts > 0));
