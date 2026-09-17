-- Idempotency is caller + route scoped, not reset by principal reselection.
CREATE UNIQUE INDEX IF NOT EXISTS logical_requests_caller_route_idempotency
ON logical_requests(caller_scope_hash,endpoint,idempotency_hash);
