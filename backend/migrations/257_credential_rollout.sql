CREATE TABLE IF NOT EXISTS credential_migration_records (
    principal_id BIGINT PRIMARY KEY REFERENCES upstream_principals(id),
    account_id BIGINT NOT NULL UNIQUE REFERENCES accounts(id),
    actor_id BIGINT NOT NULL REFERENCES users(id),
    credentials_ciphertext BYTEA NOT NULL,
    credentials_aad TEXT NOT NULL,
    original_status TEXT NOT NULL,
    original_schedulable BOOLEAN NOT NULL,
    original_concurrency INT NOT NULL,
    fence_evidence JSONB NOT NULL,
    drain_evidence TEXT NOT NULL,
    operation_id UUID NOT NULL UNIQUE,
    state TEXT NOT NULL DEFAULT 'MIGRATED' CHECK(state IN ('MIGRATED','CANARY','ROLLED_BACK')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
ALTER TABLE credential_instances ADD COLUMN IF NOT EXISTS retired_to_legacy BOOLEAN NOT NULL DEFAULT false;
-- Archived control metadata stays inspectable after a drained single-instance rollback.
CREATE OR REPLACE FUNCTION guard_controlled_credential_account() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
    IF EXISTS (SELECT 1 FROM credential_instances WHERE account_id=OLD.id AND NOT retired_to_legacy)
       AND COALESCE(current_setting('sub2api.credential_control',true),'') <> 'on' THEN
        RAISE EXCEPTION 'controlled credential account requires principal API';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION guard_controlled_credential_groups() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE controlled BOOLEAN; BEGIN
    IF TG_OP='INSERT' THEN
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id=NEW.account_id AND NOT retired_to_legacy) INTO controlled;
    ELSIF TG_OP='DELETE' THEN
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id=OLD.account_id AND NOT retired_to_legacy) INTO controlled;
    ELSE
        SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id IN (OLD.account_id,NEW.account_id) AND NOT retired_to_legacy) INTO controlled;
    END IF;
    IF controlled AND COALESCE(current_setting('sub2api.credential_control',true),'') <> 'on' THEN
        RAISE EXCEPTION 'controlled credential permissions require principal API';
    END IF;
    IF TG_OP='DELETE' THEN RETURN OLD; END IF; RETURN NEW;
END $$;
