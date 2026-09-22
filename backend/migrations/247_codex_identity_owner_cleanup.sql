-- Reuse the durable invalidation outbox for Codex HTTP identity ownership.
-- Tokens contain only an internal user ID or a SHA-256 credential namespace;
-- credentials and raw client session/thread identifiers never enter the outbox.
ALTER TABLE auth_cache_invalidation_outbox
    ADD COLUMN event_type TEXT NOT NULL DEFAULT 'auth',
    ADD COLUMN owner_token TEXT,
    ADD CONSTRAINT auth_cache_invalidation_event_kind CHECK (
        (event_type = 'auth' AND owner_token IS NULL)
        OR (event_type = 'codex_identity_owner'
            AND owner_token IS NOT NULL
            AND (owner_token ~ '^account:[0-9a-f]{64}$' OR owner_token ~ '^user:[1-9][0-9]*$'))
    );

-- This projection matches codexAccountIdentityNamespace and
-- CodexIdentityAccountOwner. An expression index makes the worker's live-owner
-- recheck bounded without searching all account JSON documents.
CREATE FUNCTION codex_identity_credential_text(value JSONB)
RETURNS TEXT
LANGUAGE sql IMMUTABLE PARALLEL SAFE
AS $$
    -- Go strings.TrimSpace's Unicode whitespace set. JSON numeric credentials
    -- are decoded as float64 by Ent and GetCredential truncates to an integer.
    SELECT CASE jsonb_typeof(value)
        WHEN 'string' THEN btrim(value #>> '{}', U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000')
        WHEN 'number' THEN trunc((value #>> '{}')::numeric)::TEXT
        ELSE NULL
    END
$$;

CREATE FUNCTION codex_identity_account_owner(account_id BIGINT, account_platform TEXT, account_type TEXT, credentials JSONB, extra JSONB)
RETURNS TEXT
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE
AS $$
DECLARE
    upstream_account TEXT;
    upstream_user TEXT;
    seed TEXT;
    token TEXT;
    namespace TEXT;
BEGIN
    IF account_platform <> 'openai' OR account_type NOT IN ('oauth', 'setup-token') THEN
        RETURN NULL;
    END IF;
    upstream_account := codex_identity_credential_text(credentials -> 'chatgpt_account_id');
    IF COALESCE(upstream_account, '') <> '' THEN
        namespace := 'chatgpt:' || upstream_account;
        upstream_user := codex_identity_credential_text(credentials -> 'chatgpt_user_id');
        IF COALESCE(upstream_user, '') <> '' THEN
            namespace := namespace || ':user:' || upstream_user;
        END IF;
    ELSE
        IF jsonb_typeof(extra -> 'codex_fingerprint_seed') = 'string' THEN
            seed := codex_identity_credential_text(extra -> 'codex_fingerprint_seed');
        END IF;
        IF seed ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
           AND seed <> '00000000-0000-0000-0000-000000000000' THEN
            namespace := 'seed:' || seed;
        ELSIF account_type = 'setup-token' THEN
            token := codex_identity_credential_text(credentials -> 'access_token');
            IF COALESCE(token, '') <> '' THEN
                namespace := 'setup-token:' || substr(encode(sha256(convert_to('openai-setup-token:' || token, 'UTF8')), 'hex'), 1, 32);
            END IF;
        END IF;
    END IF;
    IF namespace IS NULL THEN
        namespace := 'platform:' || account_platform || ':account:' || account_id::TEXT;
    END IF;
    RETURN 'account:' || encode(sha256(convert_to(namespace, 'UTF8')), 'hex');
END;
$$;

CREATE INDEX idx_accounts_codex_identity_owner
    ON accounts (codex_identity_account_owner(id, platform, type, credentials, extra))
    WHERE deleted_at IS NULL;

CREATE FUNCTION enqueue_codex_account_identity_cleanup()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    old_owner TEXT;
    new_owner TEXT;
    lock_owner TEXT;
BEGIN
    IF TG_OP <> 'INSERT' AND OLD.deleted_at IS NULL THEN
        old_owner := codex_identity_account_owner(OLD.id, OLD.platform, OLD.type, OLD.credentials, OLD.extra);
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.deleted_at IS NULL THEN
        new_owner := codex_identity_account_owner(NEW.id, NEW.platform, NEW.type, NEW.credentials, NEW.extra);
    END IF;
    IF old_owner IS DISTINCT FROM new_owner THEN
        -- Cleanup holds the same lock until Redis batches complete. A new
        -- account/import sharing this credential cannot race that cleanup.
        FOR lock_owner IN SELECT DISTINCT owner FROM unnest(ARRAY[old_owner, new_owner]) AS owner
                          WHERE owner IS NOT NULL ORDER BY owner LOOP
            PERFORM pg_advisory_xact_lock(hashtextextended('codex-identity-owner:' || lock_owner, 0));
        END LOOP;
        IF old_owner IS NOT NULL THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key, event_type, owner_token)
            VALUES (encode(sha256(convert_to(old_owner, 'UTF8')), 'hex'), 'codex_identity_owner', old_owner);
        END IF;
        IF new_owner IS NOT NULL THEN
            -- Reimport/restoration can reactivate a namespace only after the
            -- worker has rechecked its live database ownership under this lock.
            INSERT INTO auth_cache_invalidation_outbox (cache_key, event_type, owner_token)
            VALUES (encode(sha256(convert_to(new_owner, 'UTF8')), 'hex'), 'codex_identity_owner', new_owner);
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_accounts_codex_identity_cleanup
BEFORE INSERT OR UPDATE OF platform, type, credentials, extra, deleted_at OR DELETE ON accounts
FOR EACH ROW EXECUTE FUNCTION enqueue_codex_account_identity_cleanup();

CREATE FUNCTION enqueue_codex_user_identity_cleanup()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    owner TEXT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        owner := 'user:' || NEW.id::TEXT;
    ELSE
        owner := 'user:' || OLD.id::TEXT;
        IF TG_OP = 'UPDATE' AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
            RETURN NEW;
        END IF;
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended('codex-identity-owner:' || owner, 0));
    IF (TG_OP = 'INSERT' AND NEW.deleted_at IS NULL)
       OR (TG_OP = 'UPDATE' AND OLD.deleted_at IS DISTINCT FROM NEW.deleted_at)
       OR (TG_OP = 'DELETE' AND OLD.deleted_at IS NULL) THEN
        INSERT INTO auth_cache_invalidation_outbox (cache_key, event_type, owner_token)
        VALUES (encode(sha256(convert_to(owner, 'UTF8')), 'hex'), 'codex_identity_owner', owner);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_users_codex_identity_cleanup
BEFORE INSERT OR UPDATE OF deleted_at OR DELETE ON users
FOR EACH ROW EXECUTE FUNCTION enqueue_codex_user_identity_cleanup();

COMMENT ON COLUMN auth_cache_invalidation_outbox.owner_token IS
    'Codex identity cleanup owner: internal user ID or SHA-256 upstream credential namespace; no raw identity data';
