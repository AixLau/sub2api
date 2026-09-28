-- PR-01: additive control metadata only. No account, credential, seed, group,
-- billing or scheduler changes; this migration does not activate any account.
-- The current product has one deployment-wide administration scope, not tenants.
CREATE TABLE IF NOT EXISTS upstream_principals (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL DEFAULT 1 CHECK (tenant_id = 1),
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(40) NOT NULL CHECK (provider = 'openai_oauth'),
    verified_subject_key TEXT,
    verification_state TEXT NOT NULL DEFAULT 'UNVERIFIED'
        CHECK (verification_state IN ('UNVERIFIED', 'VERIFIED', 'INVALID')),
    requested_limit INT NOT NULL CHECK (requested_limit >= 0),
    occupied INT NOT NULL DEFAULT 0 CHECK (occupied >= 0),
    config_version BIGINT NOT NULL DEFAULT 1 CHECK (config_version > 0),
    admission_epoch BIGINT NOT NULL DEFAULT 1 CHECK (admission_epoch > 0),
    admin_state TEXT NOT NULL DEFAULT 'PAUSED'
        CHECK (admin_state IN ('ACTIVE','DRAINING','PAUSED','DISABLED','REVOKED')),
    routing_mode TEXT NOT NULL DEFAULT 'OFF' CHECK (routing_mode IN ('OFF','SHADOW','GROUPED')),
    drain_deadline TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (verification_state <> 'VERIFIED' OR (verified_subject_key IS NOT NULL AND length(verified_subject_key) > 0)),
    CHECK (routing_mode <> 'GROUPED' OR verification_state = 'VERIFIED'),
    UNIQUE (tenant_id, provider, verified_subject_key)
);

CREATE TABLE IF NOT EXISTS credential_instances (
    id BIGSERIAL PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    account_id BIGINT NOT NULL UNIQUE REFERENCES accounts(id),
    name VARCHAR(100) NOT NULL,
    identity_generation UUID NOT NULL,
    credential_version BIGINT NOT NULL DEFAULT 1 CHECK (credential_version > 0),
    weight DOUBLE PRECISION NOT NULL DEFAULT 1 CHECK (weight > 0 AND weight < 'Infinity'::float8),
    hard_max INT CHECK (hard_max >= 0),
    health_capacity INT CHECK (health_capacity >= 0),
    occupied INT NOT NULL DEFAULT 0 CHECK (occupied >= 0),
    admin_state TEXT NOT NULL DEFAULT 'PAUSED'
        CHECK (admin_state IN ('ACTIVE','DRAINING','PAUSED','DISABLED','REVOKED')),
    credential_state TEXT NOT NULL DEFAULT 'NEEDS_REAUTH'
        CHECK (credential_state IN ('VALID','REFRESHING','NEEDS_REAUTH','REFRESH_UNKNOWN')),
    transport_state TEXT NOT NULL DEFAULT 'HEALTHY'
        CHECK (transport_state IN ('HEALTHY','DEGRADED','COOLDOWN','HALF_OPEN')),
    drain_deadline TIMESTAMPTZ,
    cooldown_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (id, principal_id, identity_generation)
);
CREATE INDEX IF NOT EXISTS credential_instances_principal_idx ON credential_instances(principal_id, id);

CREATE TABLE IF NOT EXISTS credential_identity_profiles (
    instance_id BIGINT PRIMARY KEY REFERENCES credential_instances(id),
    principal_id BIGINT NOT NULL,
    generation UUID NOT NULL,
    installation_id TEXT NOT NULL CHECK (length(installation_id) BETWEEN 1 AND 256),
    legacy_seed_ref TEXT,
    source TEXT NOT NULL CHECK (source IN ('IMPORTED_CLIENT','LEGACY_SEED','LOCAL_LOGICAL')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id, principal_id, generation)
        REFERENCES credential_instances(id, principal_id, identity_generation)
);

-- Identity is an insert-only record. Refresh and control updates cannot mutate it.
CREATE OR REPLACE FUNCTION reject_credential_identity_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    RAISE EXCEPTION 'credential identity is immutable; create a new instance';
END $$;
CREATE OR REPLACE TRIGGER credential_identity_immutable
    BEFORE UPDATE OR DELETE ON credential_identity_profiles
    FOR EACH ROW EXECUTE FUNCTION reject_credential_identity_mutation();

CREATE OR REPLACE FUNCTION reject_credential_instance_reassignment() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    IF (NEW.principal_id, NEW.account_id, NEW.identity_generation)
        IS DISTINCT FROM (OLD.principal_id, OLD.account_id, OLD.identity_generation) THEN
        RAISE EXCEPTION 'credential instance ownership and generation are immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER credential_instance_ownership_immutable
    BEFORE UPDATE ON credential_instances
    FOR EACH ROW EXECUTE FUNCTION reject_credential_instance_reassignment();

-- Rollback keeps these additive tables; never restore older refresh credentials.
