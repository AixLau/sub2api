-- Durable, content-free usage facts bridge terminal observation to pricing.
CREATE TABLE IF NOT EXISTS credential_usage_receipts (
    lease_id UUID PRIMARY KEY REFERENCES request_leases(id),
    receipt JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'PENDING' CHECK(state IN ('PENDING','RECORDED','REVIEW_REQUIRED')),
    reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    recorded_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS credential_usage_receipt_pending ON credential_usage_receipts(created_at) WHERE state='PENDING';
