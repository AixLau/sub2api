ALTER TABLE request_leases ADD COLUMN IF NOT EXISTS global_user_slot TEXT;
ALTER TABLE request_leases ADD COLUMN IF NOT EXISTS global_user_acquired BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS credential_redis_epoch (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK(singleton),
    epoch UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
