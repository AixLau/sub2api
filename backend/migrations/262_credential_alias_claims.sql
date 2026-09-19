-- A historical fingerprint owner is immutable. Independent claims preserve a
-- current instance or an unresolved refresh even if that owner was retired.
CREATE TABLE credential_instance_alias_claims (
    instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    fingerprint TEXT NOT NULL CHECK (length(fingerprint)>0),
    kind TEXT NOT NULL CHECK (kind IN ('ACCESS','REFRESH')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, fingerprint, kind)
);
CREATE INDEX credential_instance_alias_by_fingerprint ON credential_instance_alias_claims(fingerprint);

CREATE TABLE credential_refresh_alias_claims (
    operation_id UUID NOT NULL REFERENCES credential_refresh_ops(id),
    fingerprint TEXT NOT NULL CHECK (length(fingerprint)>0),
    kind TEXT NOT NULL CHECK (kind IN ('ACCESS','REFRESH')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (operation_id, fingerprint, kind)
);
CREATE INDEX credential_refresh_alias_by_fingerprint ON credential_refresh_alias_claims(fingerprint);

INSERT INTO credential_instance_alias_claims(instance_id,fingerprint,kind)
SELECT instance_id,fingerprint,kind FROM credential_fingerprints WHERE kind IN ('ACCESS','REFRESH');

-- Refresh claims remain blocking while their operation is SENDING or
-- REFRESH_RESULT_UNKNOWN. There is deliberately no expiry or automatic unlock.
-- Historical encrypted UNKNOWN results require vault-aware backfill before
-- arbitration can initialize: SQL cannot derive their keyed token aliases.
