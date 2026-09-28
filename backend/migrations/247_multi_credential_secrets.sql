-- Secrets are separate from public control metadata. Ciphertext is AES-256-GCM
-- with record identity as AAD; HMAC fingerprints never leave the server.
CREATE TABLE IF NOT EXISTS credential_imports (
    id UUID PRIMARY KEY,
    tenant_id BIGINT NOT NULL CHECK (tenant_id = 1),
    owner_id BIGINT NOT NULL REFERENCES users(id),
    operation_hash TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    secret_ciphertext BYTEA NOT NULL,
    access_fingerprint TEXT NOT NULL UNIQUE,
    refresh_fingerprint TEXT UNIQUE,
    verification_state TEXT NOT NULL CHECK (verification_state IN ('UNVERIFIED','VERIFIED','INVALID','CONSUMED')),
    verified_subject_key TEXT,
    refresh_family TEXT UNIQUE,
    capabilities TEXT[] NOT NULL DEFAULT '{}',
    token_expires_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP + INTERVAL '15 minutes'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tenant_id,owner_id,operation_hash),
    CHECK (verification_state <> 'VERIFIED' OR (verified_subject_key IS NOT NULL AND length(verified_subject_key)>0))
);
CREATE INDEX IF NOT EXISTS credential_import_expiry_idx ON credential_imports(expires_at);

CREATE TABLE IF NOT EXISTS credential_secrets (
    instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    credential_version BIGINT NOT NULL CHECK (credential_version > 0),
    secret_ciphertext BYTEA NOT NULL,
    secret_aad TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    refresh_family TEXT,
    can_refresh BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(instance_id,credential_version)
);
-- Fingerprints survive rotation and re-import; one known refresh family must
-- never gain a second instance. No token material is retained in this registry.
CREATE TABLE IF NOT EXISTS credential_fingerprints (
    fingerprint TEXT PRIMARY KEY,
    instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    kind TEXT NOT NULL CHECK (kind IN ('ACCESS','REFRESH','FAMILY')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS credential_audit_outbox (
    event_id UUID PRIMARY KEY,
    principal_id BIGINT REFERENCES upstream_principals(id),
    instance_id BIGINT REFERENCES credential_instances(id),
    actor_id BIGINT,
    version BIGINT NOT NULL,
    event_type TEXT NOT NULL,
    safe_payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS credential_audit_pending_idx ON credential_audit_outbox(created_at) WHERE delivered_at IS NULL;
ALTER TABLE credential_instances ADD COLUMN IF NOT EXISTS capabilities TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE upstream_principals ADD COLUMN IF NOT EXISTS creation_key TEXT UNIQUE;
CREATE TABLE IF NOT EXISTS credential_import_fingerprints (
    fingerprint TEXT PRIMARY KEY,
    import_id UUID NOT NULL REFERENCES credential_imports(id),
    kind TEXT NOT NULL CHECK (kind IN ('TOKEN','FAMILY'))
);

-- Old admin APIs and background writers must not turn encrypted, controlled
-- carriers into independently callable accounts. Controlled repositories opt in
-- only inside their audited transaction. Never toggle this on a pooled session.
CREATE OR REPLACE FUNCTION guard_controlled_credential_account() RETURNS trigger
LANGUAGE plpgsql AS $$ BEGIN
    IF EXISTS (SELECT 1 FROM credential_instances WHERE account_id=OLD.id)
       AND COALESCE(current_setting('sub2api.credential_control',true),'') <> 'on' THEN
        RAISE EXCEPTION 'controlled credential account requires principal API';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER controlled_credential_account_guard BEFORE UPDATE OR DELETE ON accounts
FOR EACH ROW EXECUTE FUNCTION guard_controlled_credential_account();

CREATE OR REPLACE FUNCTION guard_controlled_credential_groups() RETURNS trigger
LANGUAGE plpgsql AS $$ DECLARE controlled BOOLEAN; BEGIN
    IF TG_OP='INSERT' THEN
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id=NEW.account_id) INTO controlled;
    ELSIF TG_OP='DELETE' THEN
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id=OLD.account_id) INTO controlled;
    ELSE
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id IN (OLD.account_id,NEW.account_id)) INTO controlled;
    END IF;
    IF controlled AND COALESCE(current_setting('sub2api.credential_control',true),'') <> 'on' THEN
        RAISE EXCEPTION 'controlled credential permissions require principal API';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER controlled_credential_groups_guard BEFORE INSERT OR UPDATE OR DELETE ON account_groups
FOR EACH ROW EXECUTE FUNCTION guard_controlled_credential_groups();
