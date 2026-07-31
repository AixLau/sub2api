-- Reward campaign foundation: immutable versions, budget reservations, durable
-- batch jobs, same-origin skin assets, and hourly user behavior aggregates.

CREATE TABLE IF NOT EXISTS reward_skins (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(120) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    alt_text        VARCHAR(255) NOT NULL DEFAULT '',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    mime_type       VARCHAR(32) NOT NULL,
    width           INT NOT NULL,
    height          INT NOT NULL,
    byte_size       BIGINT NOT NULL,
    sha256          VARCHAR(64) NOT NULL,
    content         BYTEA NOT NULL,
    created_by      BIGINT,
    updated_by      BIGINT,
    archived_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reward_skins_status_check
        CHECK (status IN ('active', 'inactive', 'archived')),
    CONSTRAINT reward_skins_name_check
        CHECK (BTRIM(name) <> ''),
    CONSTRAINT reward_skins_mime_type_check
        CHECK (mime_type IN ('image/png', 'image/jpeg', 'image/webp')),
    CONSTRAINT reward_skins_dimensions_check
        CHECK (width = 1320 AND height = 500),
    CONSTRAINT reward_skins_byte_size_check
        CHECK (byte_size > 0 AND byte_size <= 1048576 AND byte_size = OCTET_LENGTH(content)),
    CONSTRAINT reward_skins_sha256_check
        CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reward_skins_sha256
    ON reward_skins (sha256);
CREATE INDEX IF NOT EXISTS idx_reward_skins_status_created
    ON reward_skins (status, created_at DESC);

CREATE OR REPLACE FUNCTION protect_reward_skin_content()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.mime_type IS DISTINCT FROM OLD.mime_type
       OR NEW.width IS DISTINCT FROM OLD.width
       OR NEW.height IS DISTINCT FROM OLD.height
       OR NEW.byte_size IS DISTINCT FROM OLD.byte_size
       OR NEW.sha256 IS DISTINCT FROM OLD.sha256
       OR NEW.content IS DISTINCT FROM OLD.content THEN
        RAISE EXCEPTION 'reward skin image content is immutable; upload a new skin instead';
    END IF;
    RETURN NEW;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'reward_skins_content_immutable'
          AND tgrelid = 'reward_skins'::regclass
    ) THEN
        CREATE TRIGGER reward_skins_content_immutable
            BEFORE UPDATE ON reward_skins
            FOR EACH ROW EXECUTE FUNCTION protect_reward_skin_content();
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS reward_campaigns (
    id                  BIGSERIAL PRIMARY KEY,
    system_key          VARCHAR(64),
    name                VARCHAR(200) NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              VARCHAR(20) NOT NULL DEFAULT 'draft',
    issuance_mode       VARCHAR(32) NOT NULL DEFAULT 'on_access',
    timezone            VARCHAR(64) NOT NULL DEFAULT 'UTC',
    starts_at           TIMESTAMPTZ,
    ends_at             TIMESTAMPTZ,
    priority            INT NOT NULL DEFAULT 0,
    total_budget        DECIMAL(20,8) NOT NULL DEFAULT 0,
    reserved_budget     DECIMAL(20,8) NOT NULL DEFAULT 0,
    spent_budget        DECIMAL(20,8) NOT NULL DEFAULT 0,
    released_budget     DECIMAL(20,8) NOT NULL DEFAULT 0,
    current_version_id  BIGINT,
    created_by          BIGINT,
    updated_by          BIGINT,
    published_at        TIMESTAMPTZ,
    paused_at           TIMESTAMPTZ,
    ended_at            TIMESTAMPTZ,
    archived_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reward_campaigns_status_check
        CHECK (status IN ('draft', 'scheduled', 'active', 'paused', 'ended', 'archived')),
    CONSTRAINT reward_campaigns_identity_check
        CHECK (BTRIM(name) <> '' AND (system_key IS NULL OR BTRIM(system_key) <> '')),
    CONSTRAINT reward_campaigns_issuance_mode_check
        CHECK (issuance_mode IN ('on_access', 'scheduled_batch')),
    CONSTRAINT reward_campaigns_timezone_check
        CHECK (BTRIM(timezone) <> ''),
    CONSTRAINT reward_campaigns_schedule_check
        CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT reward_campaigns_priority_check
        CHECK (priority BETWEEN 0 AND 10000),
    CONSTRAINT reward_campaigns_budget_check
        CHECK (
            total_budget >= 0
            AND reserved_budget >= 0
            AND spent_budget >= 0
            AND released_budget >= 0
            AND reserved_budget + spent_budget <= total_budget
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_reward_campaigns_system_key
    ON reward_campaigns (system_key)
    WHERE system_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_reward_campaigns_status_schedule
    ON reward_campaigns (status, starts_at, ends_at);
CREATE INDEX IF NOT EXISTS idx_reward_campaigns_mode_status
    ON reward_campaigns (issuance_mode, status);
CREATE INDEX IF NOT EXISTS idx_reward_campaigns_priority
    ON reward_campaigns (priority DESC);

CREATE OR REPLACE FUNCTION reject_reward_campaign_deletion()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'reward campaigns must be archived instead of deleted';
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'reward_campaigns_no_delete'
          AND tgrelid = 'reward_campaigns'::regclass
    ) THEN
        CREATE TRIGGER reward_campaigns_no_delete
            BEFORE DELETE ON reward_campaigns
            FOR EACH ROW EXECUTE FUNCTION reject_reward_campaign_deletion();
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS reward_campaign_versions (
    id              BIGSERIAL PRIMARY KEY,
    campaign_id     BIGINT NOT NULL,
    version_number  INT NOT NULL,
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    config_hash     VARCHAR(64) NOT NULL,
    created_by      BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reward_campaign_versions_campaign_fk
        FOREIGN KEY (campaign_id) REFERENCES reward_campaigns(id) ON DELETE RESTRICT,
    CONSTRAINT reward_campaign_versions_number_check
        CHECK (version_number > 0),
    CONSTRAINT reward_campaign_versions_config_check
        CHECK (jsonb_typeof(config) = 'object'),
    CONSTRAINT reward_campaign_versions_hash_check
        CHECK (config_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT reward_campaign_versions_campaign_number_unique
        UNIQUE (campaign_id, version_number),
    CONSTRAINT reward_campaign_versions_campaign_id_unique
        UNIQUE (campaign_id, id)
);

CREATE INDEX IF NOT EXISTS idx_reward_campaign_versions_campaign_created
    ON reward_campaign_versions (campaign_id, created_at DESC);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'reward_campaigns_current_version_fk'
    ) THEN
        ALTER TABLE reward_campaigns
            ADD CONSTRAINT reward_campaigns_current_version_fk
            FOREIGN KEY (id, current_version_id)
            REFERENCES reward_campaign_versions(campaign_id, id)
            ON DELETE RESTRICT;
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION reject_reward_campaign_version_mutation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'reward campaign versions are immutable';
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'reward_campaign_versions_immutable'
          AND tgrelid = 'reward_campaign_versions'::regclass
    ) THEN
        CREATE TRIGGER reward_campaign_versions_immutable
            BEFORE UPDATE OR DELETE ON reward_campaign_versions
            FOR EACH ROW EXECUTE FUNCTION reject_reward_campaign_version_mutation();
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS reward_campaign_jobs (
    id                      BIGSERIAL PRIMARY KEY,
    campaign_id             BIGINT NOT NULL,
    campaign_version_id     BIGINT NOT NULL,
    job_type                VARCHAR(32) NOT NULL DEFAULT 'issue_batch',
    idempotency_key         VARCHAR(128) NOT NULL,
    status                  VARCHAR(24) NOT NULL DEFAULT 'pending',
    cursor_user_id          BIGINT NOT NULL DEFAULT 0,
    max_user_id             BIGINT NOT NULL DEFAULT 0,
    lease_owner             VARCHAR(128) NOT NULL DEFAULT '',
    lease_expires_at        TIMESTAMPTZ,
    scheduled_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_attempt_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at              TIMESTAMPTZ,
    finished_at             TIMESTAMPTZ,
    attempt_count           INT NOT NULL DEFAULT 0,
    max_attempts            INT NOT NULL DEFAULT 20,
    total_users             BIGINT NOT NULL DEFAULT 0,
    scanned_users           BIGINT NOT NULL DEFAULT 0,
    matched_users           BIGINT NOT NULL DEFAULT 0,
    granted_users           BIGINT NOT NULL DEFAULT 0,
    skipped_users           BIGINT NOT NULL DEFAULT 0,
    failed_users            BIGINT NOT NULL DEFAULT 0,
    last_error              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reward_campaign_jobs_campaign_fk
        FOREIGN KEY (campaign_id) REFERENCES reward_campaigns(id) ON DELETE RESTRICT,
    CONSTRAINT reward_campaign_jobs_version_fk
        FOREIGN KEY (campaign_id, campaign_version_id)
        REFERENCES reward_campaign_versions(campaign_id, id) ON DELETE RESTRICT,
    CONSTRAINT reward_campaign_jobs_type_check
        CHECK (job_type IN ('issue_batch', 'expire_grants', 'rollup_behavior')),
    CONSTRAINT reward_campaign_jobs_status_check
        CHECK (status IN ('pending', 'processing', 'paused', 'retry', 'succeeded', 'failed', 'dead_letter', 'cancelled')),
    CONSTRAINT reward_campaign_jobs_idempotency_key_check
        CHECK (BTRIM(idempotency_key) <> ''),
    CONSTRAINT reward_campaign_jobs_cursor_check
        CHECK (cursor_user_id >= 0 AND max_user_id >= 0),
    CONSTRAINT reward_campaign_jobs_attempts_check
        CHECK (attempt_count >= 0 AND max_attempts > 0),
    CONSTRAINT reward_campaign_jobs_counts_check
        CHECK (
            total_users >= 0
            AND scanned_users >= 0
            AND matched_users >= 0
            AND granted_users >= 0
            AND skipped_users >= 0
            AND failed_users >= 0
        ),
    CONSTRAINT reward_campaign_jobs_idempotency_unique
        UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_reward_campaign_jobs_campaign_status
    ON reward_campaign_jobs (campaign_id, status);
CREATE INDEX IF NOT EXISTS idx_reward_campaign_jobs_due
    ON reward_campaign_jobs (next_attempt_at, id)
    WHERE status IN ('pending', 'retry', 'processing');
CREATE INDEX IF NOT EXISTS idx_reward_campaign_jobs_expired_lease
    ON reward_campaign_jobs (lease_expires_at, id)
    WHERE status = 'processing';

CREATE TABLE IF NOT EXISTS user_reward_grants (
    id                      BIGSERIAL PRIMARY KEY,
    campaign_id             BIGINT NOT NULL,
    campaign_version_id     BIGINT NOT NULL,
    user_id                 BIGINT NOT NULL,
    skin_id                 BIGINT,
    job_id                  BIGINT,
    cycle_key               VARCHAR(128) NOT NULL,
    source                  VARCHAR(32) NOT NULL,
    status                  VARCHAR(20) NOT NULL DEFAULT 'pending',
    amount                  DECIMAL(20,8) NOT NULL,
    priority                INT NOT NULL DEFAULT 0,
    copy_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    skin_snapshot           JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,
    expires_at              TIMESTAMPTZ,
    viewed_at               TIMESTAMPTZ,
    claimed_at              TIMESTAMPTZ,
    expired_at              TIMESTAMPTZ,
    balance_after           DECIMAL(20,8),
    claim_record_id         BIGINT,
    claim_reference         VARCHAR(128) NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_reward_grants_campaign_fk
        FOREIGN KEY (campaign_id) REFERENCES reward_campaigns(id) ON DELETE RESTRICT,
    CONSTRAINT user_reward_grants_version_fk
        FOREIGN KEY (campaign_id, campaign_version_id)
        REFERENCES reward_campaign_versions(campaign_id, id) ON DELETE RESTRICT,
    CONSTRAINT user_reward_grants_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT user_reward_grants_skin_fk
        FOREIGN KEY (skin_id) REFERENCES reward_skins(id) ON DELETE SET NULL,
    CONSTRAINT user_reward_grants_job_fk
        FOREIGN KEY (job_id) REFERENCES reward_campaign_jobs(id) ON DELETE SET NULL,
    CONSTRAINT user_reward_grants_claim_record_fk
        FOREIGN KEY (claim_record_id) REFERENCES redeem_codes(id) ON DELETE RESTRICT,
    CONSTRAINT user_reward_grants_source_check
        CHECK (source IN ('on_access', 'scheduled_batch', 'legacy_welcome', 'legacy_surprise', 'manual')),
    CONSTRAINT user_reward_grants_status_check
        CHECK (status IN ('pending', 'claimed', 'expired', 'cancelled')),
    CONSTRAINT user_reward_grants_cycle_key_check
        CHECK (BTRIM(cycle_key) <> ''),
    CONSTRAINT user_reward_grants_amount_check
        CHECK (amount > 0),
    CONSTRAINT user_reward_grants_priority_check
        CHECK (priority BETWEEN 0 AND 10000),
    CONSTRAINT user_reward_grants_snapshots_check
        CHECK (
            jsonb_typeof(copy_snapshot) = 'object'
            AND jsonb_typeof(skin_snapshot) = 'object'
            AND jsonb_typeof(metadata) = 'object'
        ),
    CONSTRAINT user_reward_grants_state_timestamps_check
        CHECK (
            (status <> 'claimed' OR (
                claimed_at IS NOT NULL
                AND balance_after IS NOT NULL
                AND claim_record_id IS NOT NULL
                AND BTRIM(claim_reference) <> ''
                AND expired_at IS NULL
            ))
            AND (status <> 'expired' OR (
                expired_at IS NOT NULL
                AND claimed_at IS NULL
                AND balance_after IS NULL
                AND claim_record_id IS NULL
                AND claim_reference = ''
            ))
            AND (status <> 'cancelled' OR (
                claimed_at IS NULL
                AND expired_at IS NULL
                AND balance_after IS NULL
                AND claim_record_id IS NULL
                AND claim_reference = ''
            ))
            AND (status <> 'pending' OR (
                claimed_at IS NULL
                AND expired_at IS NULL
                AND balance_after IS NULL
                AND claim_record_id IS NULL
                AND claim_reference = ''
            ))
        ),
    CONSTRAINT user_reward_grants_campaign_user_cycle_unique
        UNIQUE (campaign_id, user_id, cycle_key)
);

CREATE INDEX IF NOT EXISTS idx_user_reward_grants_pending_queue
    ON user_reward_grants (user_id, priority DESC, expires_at ASC NULLS LAST, id)
    WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_user_reward_grants_campaign_status_created
    ON user_reward_grants (campaign_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_reward_grants_expiry
    ON user_reward_grants (expires_at, id)
    WHERE status = 'pending' AND expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_reward_grants_job
    ON user_reward_grants (job_id, id)
    WHERE job_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_reward_grants_claim_record
    ON user_reward_grants (claim_record_id)
    WHERE claim_record_id IS NOT NULL;

CREATE OR REPLACE FUNCTION protect_user_reward_grant_snapshot()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.campaign_id IS DISTINCT FROM OLD.campaign_id
       OR NEW.campaign_version_id IS DISTINCT FROM OLD.campaign_version_id
       OR NEW.user_id IS DISTINCT FROM OLD.user_id
       OR NEW.cycle_key IS DISTINCT FROM OLD.cycle_key
       OR NEW.source IS DISTINCT FROM OLD.source
       OR NEW.amount IS DISTINCT FROM OLD.amount
       OR NEW.priority IS DISTINCT FROM OLD.priority
       OR NEW.copy_snapshot IS DISTINCT FROM OLD.copy_snapshot
       OR NEW.skin_snapshot IS DISTINCT FROM OLD.skin_snapshot
       OR NEW.metadata IS DISTINCT FROM OLD.metadata
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'issued reward value and presentation snapshot are immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enforce_user_reward_grant_lifecycle()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.status <> 'pending' AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'terminal reward grant status cannot be changed';
    END IF;
    IF OLD.status = 'claimed' AND (
        NEW.claimed_at IS DISTINCT FROM OLD.claimed_at
        OR NEW.balance_after IS DISTINCT FROM OLD.balance_after
        OR NEW.claim_record_id IS DISTINCT FROM OLD.claim_record_id
        OR NEW.claim_reference IS DISTINCT FROM OLD.claim_reference
    ) THEN
        RAISE EXCEPTION 'claimed reward result cannot be changed';
    END IF;
    IF OLD.status = 'expired' AND NEW.expired_at IS DISTINCT FROM OLD.expired_at THEN
        RAISE EXCEPTION 'expired reward timestamp cannot be changed';
    END IF;
    IF OLD.status = 'pending'
       AND NEW.status NOT IN ('pending', 'claimed', 'expired', 'cancelled') THEN
        RAISE EXCEPTION 'invalid reward grant status transition';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION reject_user_reward_grant_deletion()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'reward grants are financial records and cannot be deleted';
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'user_reward_grants_snapshot_immutable'
          AND tgrelid = 'user_reward_grants'::regclass
    ) THEN
        CREATE TRIGGER user_reward_grants_snapshot_immutable
            BEFORE UPDATE ON user_reward_grants
            FOR EACH ROW EXECUTE FUNCTION protect_user_reward_grant_snapshot();
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'user_reward_grants_lifecycle'
          AND tgrelid = 'user_reward_grants'::regclass
    ) THEN
        CREATE TRIGGER user_reward_grants_lifecycle
            BEFORE UPDATE ON user_reward_grants
            FOR EACH ROW EXECUTE FUNCTION enforce_user_reward_grant_lifecycle();
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_trigger
        WHERE tgname = 'user_reward_grants_no_delete'
          AND tgrelid = 'user_reward_grants'::regclass
    ) THEN
        CREATE TRIGGER user_reward_grants_no_delete
            BEFORE DELETE ON user_reward_grants
            FOR EACH ROW EXECUTE FUNCTION reject_user_reward_grant_deletion();
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS reward_campaign_user_states (
    id                  BIGSERIAL PRIMARY KEY,
    campaign_id         BIGINT NOT NULL,
    user_id             BIGINT NOT NULL,
    last_evaluated_at   TIMESTAMPTZ,
    last_won_at         TIMESTAMPTZ,
    last_granted_at     TIMESTAMPTZ,
    last_claimed_at     TIMESTAMPTZ,
    next_eligible_at    TIMESTAMPTZ,
    evaluation_count    BIGINT NOT NULL DEFAULT 0,
    win_count           BIGINT NOT NULL DEFAULT 0,
    grant_count         BIGINT NOT NULL DEFAULT 0,
    claim_count         BIGINT NOT NULL DEFAULT 0,
    control_group       BOOLEAN NOT NULL DEFAULT FALSE,
    current_cycle_key   VARCHAR(128) NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT reward_campaign_user_states_campaign_fk
        FOREIGN KEY (campaign_id) REFERENCES reward_campaigns(id) ON DELETE RESTRICT,
    CONSTRAINT reward_campaign_user_states_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT reward_campaign_user_states_counts_check
        CHECK (
            evaluation_count >= 0
            AND win_count >= 0
            AND grant_count >= 0
            AND claim_count >= 0
            AND claim_count <= grant_count
            AND grant_count <= win_count
            AND win_count <= evaluation_count
        ),
    CONSTRAINT reward_campaign_user_states_campaign_user_unique
        UNIQUE (campaign_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_reward_campaign_user_states_next_eligible
    ON reward_campaign_user_states (campaign_id, next_eligible_at, user_id);
CREATE INDEX IF NOT EXISTS idx_reward_campaign_user_states_user_updated
    ON reward_campaign_user_states (user_id, updated_at DESC);

-- Despite its legacy-compatible name, each row is one UTC hour. Request-time
-- targeting reads these buckets and never scans usage_logs.
CREATE TABLE IF NOT EXISTS user_behavior_daily (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL,
    bucket_start        TIMESTAMPTZ NOT NULL,
    request_count       BIGINT NOT NULL DEFAULT 0,
    actual_cost         DECIMAL(20,8) NOT NULL DEFAULT 0,
    recharge_amount     DECIMAL(20,8) NOT NULL DEFAULT 0,
    last_api_use_at     TIMESTAMPTZ,
    last_active_at      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_behavior_daily_user_fk
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT user_behavior_daily_hour_check
        CHECK (
            bucket_start = DATE_TRUNC('hour', bucket_start AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
        ),
    CONSTRAINT user_behavior_daily_metrics_check
        CHECK (request_count >= 0 AND actual_cost >= 0 AND recharge_amount >= 0),
    CONSTRAINT user_behavior_daily_user_bucket_unique
        UNIQUE (user_id, bucket_start)
);

CREATE INDEX IF NOT EXISTS idx_user_behavior_daily_bucket_user
    ON user_behavior_daily (bucket_start DESC, user_id);
CREATE INDEX IF NOT EXISTS idx_user_behavior_daily_user_last_api
    ON user_behavior_daily (user_id, last_api_use_at DESC);

-- Prime the rollup so existing users can be targeted immediately after the
-- migration. Assigning the recomputed usage columns (instead of adding them)
-- makes a partially retried migration idempotent while preserving recharge
-- values that may already share the same hourly row.
WITH usage_events AS (
    SELECT
        user_id,
        DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS bucket_start,
        COUNT(*) AS request_count,
        SUM(GREATEST(COALESCE(actual_cost, 0), 0)) AS actual_cost,
        MAX(created_at) AS last_api_use_at
    FROM usage_logs
    WHERE user_id IS NOT NULL
      AND source = 'gateway'
      AND api_key_id IS NOT NULL
      AND created_at >= NOW() - INTERVAL '30 days'
    GROUP BY user_id, DATE_TRUNC('hour', created_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'
)
INSERT INTO user_behavior_daily (
    user_id, bucket_start, request_count, actual_cost,
    last_api_use_at, last_active_at, created_at, updated_at
)
SELECT
    user_id, bucket_start, request_count, actual_cost,
    last_api_use_at, last_api_use_at, NOW(), NOW()
FROM usage_events
ON CONFLICT (user_id, bucket_start) DO UPDATE
SET request_count = EXCLUDED.request_count,
    actual_cost = EXCLUDED.actual_cost,
    last_api_use_at = EXCLUDED.last_api_use_at,
    last_active_at = GREATEST(user_behavior_daily.last_active_at, EXCLUDED.last_active_at),
    updated_at = NOW();

-- Keep one marker for each user's historical last gateway call. Recent hourly
-- metrics remain bounded to 30 days, while recall rules can still target users
-- whose last API use is older than that window without querying usage_logs at
-- request time.
WITH latest_usage AS (
    SELECT DISTINCT ON (user_id)
        user_id,
        created_at AS last_api_use_at
    FROM usage_logs
    WHERE user_id IS NOT NULL
      AND source = 'gateway'
      AND api_key_id IS NOT NULL
    ORDER BY user_id, created_at DESC
)
INSERT INTO user_behavior_daily (
    user_id, bucket_start, request_count, actual_cost,
    last_api_use_at, last_active_at, created_at, updated_at
)
SELECT
    user_id,
    DATE_TRUNC('hour', last_api_use_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
    0,
    0,
    last_api_use_at,
    last_api_use_at,
    NOW(),
    NOW()
FROM latest_usage
ON CONFLICT (user_id, bucket_start) DO UPDATE
SET last_api_use_at = GREATEST(user_behavior_daily.last_api_use_at, EXCLUDED.last_api_use_at),
    last_active_at = GREATEST(user_behavior_daily.last_active_at, EXCLUDED.last_active_at),
    updated_at = NOW();

WITH recharge_events AS (
    SELECT
        user_id,
        DATE_TRUNC(
            'hour',
            COALESCE(completed_at, updated_at) AT TIME ZONE 'UTC'
        ) AT TIME ZONE 'UTC' AS bucket_start,
        SUM(amount) AS recharge_amount,
        MAX(COALESCE(completed_at, updated_at)) AS last_active_at
    FROM payment_orders
    WHERE status = 'COMPLETED'
      AND order_type = 'balance'
      AND amount > 0
      AND COALESCE(completed_at, updated_at) >= NOW() - INTERVAL '30 days'
    GROUP BY
        user_id,
        DATE_TRUNC(
            'hour',
            COALESCE(completed_at, updated_at) AT TIME ZONE 'UTC'
        ) AT TIME ZONE 'UTC'
)
INSERT INTO user_behavior_daily (
    user_id, bucket_start, recharge_amount, last_active_at, created_at, updated_at
)
SELECT user_id, bucket_start, recharge_amount, last_active_at, NOW(), NOW()
FROM recharge_events
ON CONFLICT (user_id, bucket_start) DO UPDATE
SET recharge_amount = EXCLUDED.recharge_amount,
    last_active_at = GREATEST(user_behavior_daily.last_active_at, EXCLUDED.last_active_at),
    updated_at = NOW();

-- Keep targeting data current in the same transaction that persists the
-- source event. INSERT triggers only run for rows that survive the usage-log
-- idempotency constraint, so retries cannot inflate request or spend totals.
CREATE OR REPLACE FUNCTION reward_aggregate_usage_log_hourly()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    event_at TIMESTAMPTZ;
    hour_bucket TIMESTAMPTZ;
BEGIN
    IF NEW.user_id IS NULL
        OR NEW.source IS DISTINCT FROM 'gateway'
        OR NEW.api_key_id IS NULL THEN
        RETURN NEW;
    END IF;

    event_at := COALESCE(NEW.created_at, NOW());
    hour_bucket := DATE_TRUNC('hour', event_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

    INSERT INTO user_behavior_daily (
        user_id,
        bucket_start,
        request_count,
        actual_cost,
        recharge_amount,
        last_api_use_at,
        last_active_at,
        created_at,
        updated_at
    )
    VALUES (
        NEW.user_id,
        hour_bucket,
        1,
        GREATEST(COALESCE(NEW.actual_cost, 0), 0),
        0,
        event_at,
        event_at,
        NOW(),
        NOW()
    )
    ON CONFLICT (user_id, bucket_start) DO UPDATE
    SET request_count = user_behavior_daily.request_count + EXCLUDED.request_count,
        actual_cost = user_behavior_daily.actual_cost + EXCLUDED.actual_cost,
        last_api_use_at = GREATEST(user_behavior_daily.last_api_use_at, EXCLUDED.last_api_use_at),
        last_active_at = GREATEST(user_behavior_daily.last_active_at, EXCLUDED.last_active_at),
        updated_at = NOW();

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_reward_aggregate_usage_log_hourly ON usage_logs;
CREATE TRIGGER trg_reward_aggregate_usage_log_hourly
AFTER INSERT ON usage_logs
FOR EACH ROW
WHEN (NEW.user_id IS NOT NULL AND NEW.source = 'gateway' AND NEW.api_key_id IS NOT NULL)
EXECUTE FUNCTION reward_aggregate_usage_log_hourly();

-- A real recharge is a positive balance payment that reached COMPLETED. The
-- status transition is the existing fulfillment idempotency boundary; tying
-- aggregation to it excludes subscriptions, admin balance edits, redeem codes,
-- rewards, and duplicate payment callbacks.
CREATE OR REPLACE FUNCTION reward_aggregate_completed_recharge_hourly()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    event_at TIMESTAMPTZ;
    hour_bucket TIMESTAMPTZ;
BEGIN
    IF NEW.status <> 'COMPLETED'
        OR OLD.status = 'COMPLETED'
        OR NEW.order_type <> 'balance'
        OR COALESCE(NEW.amount, 0) <= 0 THEN
        RETURN NEW;
    END IF;

    event_at := COALESCE(NEW.completed_at, NEW.updated_at, NOW());
    hour_bucket := DATE_TRUNC('hour', event_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

    INSERT INTO user_behavior_daily (
        user_id,
        bucket_start,
        request_count,
        actual_cost,
        recharge_amount,
        last_active_at,
        created_at,
        updated_at
    )
    VALUES (
        NEW.user_id,
        hour_bucket,
        0,
        0,
        NEW.amount,
        event_at,
        NOW(),
        NOW()
    )
    ON CONFLICT (user_id, bucket_start) DO UPDATE
    SET recharge_amount = user_behavior_daily.recharge_amount + EXCLUDED.recharge_amount,
        last_active_at = GREATEST(user_behavior_daily.last_active_at, EXCLUDED.last_active_at),
        updated_at = NOW();

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_reward_aggregate_completed_recharge_hourly ON payment_orders;
CREATE TRIGGER trg_reward_aggregate_completed_recharge_hourly
AFTER UPDATE OF status ON payment_orders
FOR EACH ROW
WHEN (NEW.status = 'COMPLETED' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION reward_aggregate_completed_recharge_hourly();

-- Preserve the existing welcome and active-user surprise programs as system
-- campaigns. They remain on-access campaigns and include every supported
-- registration source.
INSERT INTO reward_campaigns (
    system_key,
    name,
    description,
    status,
    issuance_mode,
    timezone,
    starts_at,
    ends_at,
    priority,
    total_budget,
    published_at
)
VALUES
    (
        'system_welcome',
        'Welcome scratch reward',
        'System campaign migrated from the legacy welcome reward.',
        'active',
        'on_access',
        'UTC',
        NOW(),
        TIMESTAMPTZ '2099-12-31 23:59:59+00',
        100,
        1000000,
        NOW()
    ),
    (
        'system_surprise',
        'Active user surprise reward',
        'System campaign migrated from the legacy daily surprise reward.',
        'active',
        'on_access',
        'UTC',
        NOW(),
        TIMESTAMPTZ '2099-12-31 23:59:59+00',
        50,
        1000000,
        NOW()
    )
ON CONFLICT DO NOTHING;

INSERT INTO reward_campaign_versions (
    campaign_id,
    version_number,
    config,
    config_hash,
    created_at
)
SELECT
    seeded.campaign_id,
    1,
    seeded.config,
    ENCODE(SHA256(CONVERT_TO(seeded.config::TEXT, 'UTF8')), 'hex'),
    seeded.created_at
FROM (
SELECT
    c.id AS campaign_id,
    CASE c.system_key
        WHEN 'system_welcome' THEN
            JSONB_INSERT(
            '{
              "schema_version": 1,
              "system_type": "welcome",
              "title": "Welcome gift",
              "priority": 100,
              "win_probability": 1,
              "per_user_limit": 1,
              "evaluation_interval_minutes": 0,
              "claim_cooldown_minutes": 0,
              "control_group_percent": 0,
              "copy": {
                "title": "Welcome gift",
                "prompt": "Scratch to claim your balance reward",
                "cover_text": "Scratch here",
                "gesture_hint": "Swipe across the card",
                "revealed_hint": "Reward revealed",
                "won_text": "You received ${amount}",
                "credited_text": "Added to your balance",
                "continue_text": "Continue"
              },
              "copy_i18n": {
                "zh": {
                  "title": "新人见面礼",
                  "prompt": "刮开领取你的专属余额奖励",
                  "cover_text": "刮开这里",
                  "gesture_hint": "滑动刮开奖励",
                  "revealed_hint": "奖励已揭晓",
                  "won_text": "你获得了 ${amount}",
                  "credited_text": "已存入账户余额",
                  "continue_text": "继续"
                },
                "en": {
                  "title": "Welcome gift",
                  "prompt": "Scratch to claim your balance reward",
                  "cover_text": "Scratch here",
                  "gesture_hint": "Swipe across the card",
                  "revealed_hint": "Reward revealed",
                  "won_text": "You received ${amount}",
                  "credited_text": "Added to your balance",
                  "continue_text": "Continue"
                }
              },
              "audience": {
                "any_of": [{
                  "all_of": [{
                    "field": "signup_source",
                    "operator": "in",
                    "value": ["email", "linuxdo", "wechat", "oidc", "github", "google", "dingtalk"]
                  }]
                }]
              },
              "amount_tiers": [
                {"amount": 1, "weight": 20},
                {"amount": 2, "weight": 20},
                {"amount": 3, "weight": 20},
                {"amount": 4, "weight": 20},
                {"amount": 5, "weight": 20}
              ],
              "skin_weights": []
            }'::jsonb,
            '{audience,any_of,0,all_of,1}',
            jsonb_build_object(
                'field', 'registered_at',
                'operator', 'after',
                'value', TO_CHAR(c.starts_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
            ),
            FALSE)
        ELSE
            '{
              "schema_version": 1,
              "system_type": "surprise",
              "title": "Activity surprise",
              "priority": 50,
              "win_probability": 0.1,
              "per_user_limit": 100,
              "evaluation_interval_minutes": 1440,
              "claim_cooldown_minutes": 43200,
              "control_group_percent": 0,
              "copy": {
                "title": "Activity surprise",
                "prompt": "Thanks for staying active. Scratch to claim",
                "cover_text": "Scratch here",
                "gesture_hint": "Swipe across the card",
                "revealed_hint": "Reward revealed",
                "won_text": "You received ${amount}",
                "credited_text": "Added to your balance",
                "continue_text": "Continue"
              },
              "copy_i18n": {
                "zh": {
                  "title": "活跃惊喜",
                  "prompt": "感谢常来，刮开领取余额奖励",
                  "cover_text": "刮开这里",
                  "gesture_hint": "滑动刮开奖励",
                  "revealed_hint": "奖励已揭晓",
                  "won_text": "你获得了 ${amount}",
                  "credited_text": "已存入账户余额",
                  "continue_text": "继续"
                },
                "en": {
                  "title": "Activity surprise",
                  "prompt": "Thanks for staying active. Scratch to claim",
                  "cover_text": "Scratch here",
                  "gesture_hint": "Swipe across the card",
                  "revealed_hint": "Reward revealed",
                  "won_text": "You received ${amount}",
                  "credited_text": "Added to your balance",
                  "continue_text": "Continue"
                }
              },
              "audience": {
                "any_of": [{
                  "all_of": [{
                    "field": "signup_source",
                    "operator": "in",
                    "value": ["email", "linuxdo", "wechat", "oidc", "github", "google", "dingtalk"]
                  }, {
                    "field": "registered_at",
                    "operator": "before",
                    "value": {"relative_days": -7}
                  }, {
                    "field": "last_active_at",
                    "operator": "after",
                    "value": {"relative_days": -7}
                  }]
                }]
              },
              "amount_tiers": [
                {"amount": 1, "weight": 20},
                {"amount": 2, "weight": 20},
                {"amount": 3, "weight": 20},
                {"amount": 4, "weight": 20},
                {"amount": 5, "weight": 20}
              ],
              "skin_weights": []
            }'::jsonb
    END AS config,
    COALESCE(c.published_at, NOW()) AS created_at
FROM reward_campaigns c
WHERE c.system_key IN ('system_welcome', 'system_surprise')
) AS seeded
ON CONFLICT (campaign_id, version_number) DO NOTHING;

UPDATE reward_campaigns c
SET current_version_id = v.id,
    updated_at = NOW()
FROM reward_campaign_versions v
WHERE v.campaign_id = c.id
  AND v.version_number = 1
  AND c.system_key IN ('system_welcome', 'system_surprise')
  AND c.current_version_id IS NULL;

-- Move legacy pending welcome rewards into reserved grants. The CTE only adds
-- budget for rows inserted by this run, so rerunning the migration is harmless.
WITH inserted AS (
    INSERT INTO user_reward_grants (
        campaign_id,
        campaign_version_id,
        user_id,
        cycle_key,
        source,
        status,
        amount,
        priority,
        copy_snapshot,
        skin_snapshot,
        metadata,
        expires_at,
        created_at,
        updated_at
    )
    SELECT
        c.id,
        v.id,
        u.id,
        'legacy:welcome',
        'legacy_welcome',
        'pending',
        u.welcome_reward_amount,
        c.priority,
        jsonb_build_object(
            'default', v.config->'copy',
            'i18n', COALESCE(v.config->'copy_i18n', '{}'::jsonb)
        ),
        '{}'::jsonb,
        jsonb_build_object('legacy_field', 'welcome_reward_amount'),
        c.ends_at,
        u.created_at,
        NOW()
    FROM users u
    CROSS JOIN reward_campaigns c
    JOIN reward_campaign_versions v
      ON v.campaign_id = c.id
     AND v.id = c.current_version_id
    WHERE c.system_key = 'system_welcome'
      AND u.welcome_reward_amount > 0
    ON CONFLICT (campaign_id, user_id, cycle_key) DO NOTHING
    RETURNING campaign_id, amount
), totals AS (
    SELECT campaign_id, SUM(amount) AS amount
    FROM inserted
    GROUP BY campaign_id
)
UPDATE reward_campaigns c
SET total_budget = GREATEST(c.total_budget, c.reserved_budget + c.spent_budget + totals.amount),
    reserved_budget = c.reserved_budget + totals.amount,
    updated_at = NOW()
FROM totals
WHERE c.id = totals.campaign_id;

WITH inserted AS (
    INSERT INTO user_reward_grants (
        campaign_id,
        campaign_version_id,
        user_id,
        cycle_key,
        source,
        status,
        amount,
        priority,
        copy_snapshot,
        skin_snapshot,
        metadata,
        expires_at,
        created_at,
        updated_at
    )
    SELECT
        c.id,
        v.id,
        u.id,
        'legacy:surprise',
        'legacy_surprise',
        'pending',
        u.surprise_reward_amount,
        c.priority,
        jsonb_build_object(
            'default', v.config->'copy',
            'i18n', COALESCE(v.config->'copy_i18n', '{}'::jsonb)
        ),
        '{}'::jsonb,
        jsonb_build_object('legacy_field', 'surprise_reward_amount'),
        c.ends_at,
        COALESCE(u.surprise_reward_checked_at, NOW()),
        NOW()
    FROM users u
    CROSS JOIN reward_campaigns c
    JOIN reward_campaign_versions v
      ON v.campaign_id = c.id
     AND v.id = c.current_version_id
    WHERE c.system_key = 'system_surprise'
      AND u.surprise_reward_amount > 0
    ON CONFLICT (campaign_id, user_id, cycle_key) DO NOTHING
    RETURNING campaign_id, amount
), totals AS (
    SELECT campaign_id, SUM(amount) AS amount
    FROM inserted
    GROUP BY campaign_id
)
UPDATE reward_campaigns c
SET total_budget = GREATEST(c.total_budget, c.reserved_budget + c.spent_budget + totals.amount),
    reserved_budget = c.reserved_budget + totals.amount,
    updated_at = NOW()
FROM totals
WHERE c.id = totals.campaign_id;

-- Every pre-existing account is marked evaluated for the one-time welcome
-- campaign, preventing existing users with a zero legacy field from being
-- accidentally treated as newly registered.
INSERT INTO reward_campaign_user_states (
    campaign_id,
    user_id,
    last_evaluated_at,
    last_won_at,
    last_granted_at,
    last_claimed_at,
    evaluation_count,
    win_count,
    grant_count,
    claim_count,
    current_cycle_key,
    created_at,
    updated_at
)
SELECT
    c.id,
    u.id,
    u.created_at,
    COALESCE(g.created_at, h.last_claimed_at),
    COALESCE(g.created_at, h.last_claimed_at),
    h.last_claimed_at,
    1,
    CASE WHEN g.id IS NOT NULL OR h.claim_count > 0 THEN 1 ELSE 0 END,
    CASE WHEN g.id IS NOT NULL OR h.claim_count > 0 THEN 1 ELSE 0 END,
    LEAST(h.claim_count, 1),
    CASE WHEN g.id IS NOT NULL THEN 'legacy:welcome' ELSE '' END,
    NOW(),
    NOW()
FROM users u
CROSS JOIN reward_campaigns c
LEFT JOIN user_reward_grants g
  ON g.campaign_id = c.id
 AND g.user_id = u.id
 AND g.cycle_key = 'legacy:welcome'
LEFT JOIN LATERAL (
    SELECT
        COUNT(*)::BIGINT AS claim_count,
        MAX(used_at) AS last_claimed_at
    FROM redeem_codes r
    WHERE r.used_by = u.id
      AND r.type = 'welcome_scratch'
) h ON TRUE
WHERE c.system_key = 'system_welcome'
ON CONFLICT (campaign_id, user_id) DO NOTHING;

INSERT INTO reward_campaign_user_states (
    campaign_id,
    user_id,
    last_evaluated_at,
    last_won_at,
    last_granted_at,
    last_claimed_at,
    next_eligible_at,
    evaluation_count,
    win_count,
    grant_count,
    claim_count,
    current_cycle_key,
    created_at,
    updated_at
)
SELECT
    c.id,
    u.id,
    u.surprise_reward_checked_at,
    COALESCE(g.created_at, u.surprise_reward_awarded_at, h.last_claimed_at),
    COALESCE(g.created_at, u.surprise_reward_awarded_at, h.last_claimed_at),
    COALESCE(u.surprise_reward_awarded_at, h.last_claimed_at),
    CASE
        WHEN COALESCE(u.surprise_reward_awarded_at, h.last_claimed_at) IS NOT NULL
            THEN COALESCE(u.surprise_reward_awarded_at, h.last_claimed_at) + INTERVAL '30 days'
        ELSE NULL
    END,
    GREATEST(
        CASE WHEN u.surprise_reward_checked_at IS NOT NULL THEN 1 ELSE 0 END,
        h.claim_count + CASE WHEN g.id IS NOT NULL THEN 1 ELSE 0 END
    ),
    h.claim_count + CASE WHEN g.id IS NOT NULL THEN 1 ELSE 0 END,
    h.claim_count + CASE WHEN g.id IS NOT NULL THEN 1 ELSE 0 END,
    h.claim_count,
    CASE WHEN g.id IS NOT NULL THEN 'legacy:surprise' ELSE '' END,
    NOW(),
    NOW()
FROM users u
CROSS JOIN reward_campaigns c
LEFT JOIN user_reward_grants g
  ON g.campaign_id = c.id
 AND g.user_id = u.id
 AND g.cycle_key = 'legacy:surprise'
LEFT JOIN LATERAL (
    SELECT
        GREATEST(
            COUNT(*)::BIGINT,
            CASE WHEN u.surprise_reward_awarded_at IS NOT NULL THEN 1 ELSE 0 END
        ) AS claim_count,
        MAX(used_at) AS last_claimed_at
    FROM redeem_codes r
    WHERE r.used_by = u.id
      AND r.type = 'surprise_scratch'
) h ON TRUE
WHERE c.system_key = 'system_surprise'
  AND (
      u.surprise_reward_checked_at IS NOT NULL
      OR u.surprise_reward_awarded_at IS NOT NULL
      OR g.id IS NOT NULL
      OR h.claim_count > 0
  )
ON CONFLICT (campaign_id, user_id) DO NOTHING;

-- The values now live in grants. Clearing the legacy pending columns closes
-- the double-credit window while the old HTTP endpoints are adapted to the new
-- reward service. Historical redeem-code rows are intentionally untouched.
UPDATE users u
SET welcome_reward_amount = 0,
    updated_at = NOW()
WHERE u.welcome_reward_amount > 0
  AND EXISTS (
      SELECT 1
      FROM user_reward_grants g
      JOIN reward_campaigns c ON c.id = g.campaign_id
      WHERE g.user_id = u.id
        AND g.cycle_key = 'legacy:welcome'
        AND c.system_key = 'system_welcome'
  );

UPDATE users u
SET surprise_reward_amount = 0,
    updated_at = NOW()
WHERE u.surprise_reward_amount > 0
  AND EXISTS (
      SELECT 1
      FROM user_reward_grants g
      JOIN reward_campaigns c ON c.id = g.campaign_id
      WHERE g.user_id = u.id
        AND g.cycle_key = 'legacy:surprise'
        AND c.system_key = 'system_surprise'
  );
