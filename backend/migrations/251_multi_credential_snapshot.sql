ALTER TABLE request_leases ADD COLUMN IF NOT EXISTS account_updated_at TIMESTAMPTZ;
ALTER TABLE request_leases ADD COLUMN IF NOT EXISTS proxy_id BIGINT REFERENCES proxies(id);
ALTER TABLE request_leases ADD COLUMN IF NOT EXISTS proxy_updated_at TIMESTAMPTZ;
ALTER TABLE credential_imports ADD COLUMN IF NOT EXISTS secret_erased_at TIMESTAMPTZ;
