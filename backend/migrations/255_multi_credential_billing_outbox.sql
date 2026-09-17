CREATE TABLE IF NOT EXISTS credential_billing_outbox (
    lease_id UUID PRIMARY KEY REFERENCES request_leases(id),
    command JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    settled_at TIMESTAMPTZ
);
