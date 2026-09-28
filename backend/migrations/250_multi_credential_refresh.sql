CREATE TABLE IF NOT EXISTS credential_refresh_ops (
    id UUID PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    generation UUID NOT NULL,
    family_key TEXT NOT NULL,
    expected_version BIGINT NOT NULL,
    owner_nonce UUID NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('SENDING','SUCCEEDED','NEEDS_REAUTH','REFRESH_RESULT_UNKNOWN')),
    result_ciphertext BYTEA,
    result_aad TEXT,
    result_expires_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS credential_refresh_family_active ON credential_refresh_ops(family_key)
WHERE state IN ('SENDING','REFRESH_RESULT_UNKNOWN');
CREATE TABLE IF NOT EXISTS upstream_quota_domains (
    id TEXT PRIMARY KEY,
    dimension TEXT NOT NULL,
    blocked_until TIMESTAMPTZ,
    requires_admin_reset BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS upstream_principal_quota_domains (
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    quota_domain_id TEXT NOT NULL REFERENCES upstream_quota_domains(id),
    PRIMARY KEY(principal_id,quota_domain_id)
);
CREATE TABLE IF NOT EXISTS upstream_limit_observations (
    id UUID PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    generation UUID NOT NULL,
    credential_version BIGINT NOT NULL,
    quota_domain_id TEXT REFERENCES upstream_quota_domains(id),
    origin TEXT NOT NULL,
    scope TEXT NOT NULL,
    dimension TEXT NOT NULL DEFAULT 'unknown',
    code TEXT NOT NULL,
    http_status INT NOT NULL,
    reset_at TIMESTAMPTZ,
    confidence TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
