-- A management account keeps its original ID even after its first instance exits.
ALTER TABLE upstream_principals ADD COLUMN management_account_id BIGINT UNIQUE REFERENCES accounts(id);
ALTER TABLE upstream_principals ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE credential_instances ADD COLUMN archived_at TIMESTAMPTZ;

UPDATE upstream_principals p SET management_account_id =
    (SELECT min(account_id) FROM credential_instances i WHERE i.principal_id=p.id);

-- Materialize the previously implicit ceiling once. Subsequent account limit
-- changes never change an instance's configured capacity.
UPDATE credential_instances i SET hard_max=p.requested_limit
FROM upstream_principals p WHERE p.id=i.principal_id AND i.hard_max IS NULL;
ALTER TABLE credential_instances ALTER COLUMN hard_max SET NOT NULL;

CREATE FUNCTION initialize_account_instance_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.hard_max IS NULL THEN
        SELECT requested_limit INTO NEW.hard_max FROM upstream_principals WHERE id=NEW.principal_id;
    END IF;
    UPDATE upstream_principals SET management_account_id=NEW.account_id
        WHERE id=NEW.principal_id AND management_account_id IS NULL;
    RETURN NEW;
END $$;
CREATE TRIGGER initialize_account_instance_capacity BEFORE INSERT ON credential_instances
    FOR EACH ROW EXECUTE FUNCTION initialize_account_instance_capacity();
