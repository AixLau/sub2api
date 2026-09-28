ALTER TABLE principal_user_capacity ADD COLUMN IF NOT EXISTS last_admitted_at TIMESTAMPTZ;
ALTER TABLE admission_tickets ADD COLUMN IF NOT EXISTS candidate_ids BIGINT[] NOT NULL DEFAULT '{}';
ALTER TABLE admission_tickets ADD COLUMN IF NOT EXISTS session_hash TEXT;
