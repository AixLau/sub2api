-- Keep the periodic oldest-pending lookup off the full moderation log table.
-- This migration is non-transactional so PostgreSQL can build the index without
-- blocking production writes.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_content_moderation_logs_pending_review_created_at
    ON content_moderation_logs (created_at)
    WHERE review_status = 'pending';
