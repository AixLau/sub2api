-- Short control-plane transactions serialize ownership changes, not admission.
-- A stable row also covers the absence of any token registration.
CREATE TABLE credential_arbitration_state (
    singleton BOOLEAN PRIMARY KEY CHECK(singleton),
    fingerprint_key_id TEXT,
    initialized_at TIMESTAMPTZ
);
INSERT INTO credential_arbitration_state(singleton) VALUES(true);
CREATE TABLE credential_token_registry (
    fingerprint TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE credential_legacy_account_claims (
    account_id BIGINT NOT NULL REFERENCES accounts(id),
    fingerprint TEXT NOT NULL REFERENCES credential_token_registry(fingerprint),
    transferred BOOLEAN NOT NULL DEFAULT false,
    refresh_spent BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(account_id,fingerprint)
);
CREATE INDEX credential_legacy_claim_by_fingerprint ON credential_legacy_account_claims(fingerprint) WHERE NOT transferred;
CREATE TABLE credential_legacy_refresh_operations (
    id UUID PRIMARY KEY,
    input_fingerprint TEXT NOT NULL UNIQUE REFERENCES credential_token_registry(fingerprint),
    request_hash TEXT NOT NULL,
    owner_nonce UUID NOT NULL,
    account_id BIGINT REFERENCES accounts(id),
    account_fingerprints TEXT[],
    state TEXT NOT NULL CHECK(state IN ('SENDING','UNKNOWN','SUCCEEDED')),
    result_ciphertext BYTEA,
    result_aad TEXT,
    transferred BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ
);
CREATE TABLE credential_legacy_refresh_aliases (
    operation_id UUID NOT NULL REFERENCES credential_legacy_refresh_operations(id),
    fingerprint TEXT NOT NULL REFERENCES credential_token_registry(fingerprint),
    is_input BOOLEAN NOT NULL DEFAULT false,
    is_output BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY(operation_id,fingerprint)
);
CREATE INDEX credential_legacy_refresh_alias_lookup ON credential_legacy_refresh_aliases(fingerprint);
