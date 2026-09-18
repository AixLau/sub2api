-- Offline envelope re-encryption records contain identifiers/counts, never keys.
-- The single active key pair fences nodes with old or missing key configuration.
CREATE TABLE credential_vault_state (
    id INTEGER PRIMARY KEY CHECK (id=1),
    encryption_key_id TEXT NOT NULL,
    fingerprint_key_id TEXT NOT NULL,
    operation_id UUID NOT NULL UNIQUE,
    actor_id BIGINT NOT NULL REFERENCES users(id),
    ciphertext_counts JSONB NOT NULL,
    rotated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
