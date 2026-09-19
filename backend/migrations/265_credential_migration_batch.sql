-- A verified upstream identity may own multiple legacy account sources. Keep
-- one migration history row per source account so batch conversion preserves
-- the complete source history while the principal remains the merge key.
ALTER TABLE credential_migration_records
    DROP CONSTRAINT IF EXISTS credential_migration_records_pkey;

ALTER TABLE credential_migration_records
    ADD CONSTRAINT credential_migration_records_pkey PRIMARY KEY (account_id);

CREATE INDEX IF NOT EXISTS credential_migration_records_principal_idx
    ON credential_migration_records(principal_id);
