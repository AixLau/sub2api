CREATE TABLE IF NOT EXISTS subscription_renewals (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE RESTRICT,
    plan_id BIGINT REFERENCES subscription_plans(id) ON DELETE SET NULL,
    source_type VARCHAR(20) NOT NULL,
    source_id VARCHAR(64) NOT NULL,
    validity_days INTEGER NOT NULL CHECK (validity_days > 0),
    monthly_limit_usd DECIMAL(20,10) NOT NULL DEFAULT 0 CHECK (monthly_limit_usd >= 0),
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    activated_at TIMESTAMPTZ,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscription_renewals_source_unique UNIQUE (source_type, source_id),
    CONSTRAINT subscription_renewals_status_check CHECK (status IN ('pending', 'activated', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_subscription_renewals_pending_fifo
ON subscription_renewals(subscription_id, status, id);

CREATE INDEX IF NOT EXISTS idx_subscription_renewals_user
ON subscription_renewals(user_id, created_at DESC);
