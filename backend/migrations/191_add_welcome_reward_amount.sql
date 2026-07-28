ALTER TABLE users
    ADD COLUMN IF NOT EXISTS welcome_reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'users_welcome_reward_amount_valid'
    ) THEN
        ALTER TABLE users
            ADD CONSTRAINT users_welcome_reward_amount_valid
            CHECK (
                welcome_reward_amount >= 0
                AND welcome_reward_amount <= 5
                AND welcome_reward_amount = TRUNC(welcome_reward_amount)
            );
    END IF;
END $$;
