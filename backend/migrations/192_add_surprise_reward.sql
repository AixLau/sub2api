ALTER TABLE users
    ADD COLUMN IF NOT EXISTS surprise_reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS surprise_reward_checked_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS surprise_reward_awarded_at TIMESTAMPTZ NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_surprise_reward_amount_valid'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_surprise_reward_amount_valid
            CHECK (
                surprise_reward_amount >= 0
                AND surprise_reward_amount <= 5
                AND surprise_reward_amount = TRUNC(surprise_reward_amount)
            );
    END IF;
END
$$;
