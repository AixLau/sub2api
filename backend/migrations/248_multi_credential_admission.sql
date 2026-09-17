ALTER TABLE upstream_principals ADD COLUMN IF NOT EXISTS user_concurrency_limit INT CHECK(user_concurrency_limit>=0);
ALTER TABLE upstream_principals ADD COLUMN IF NOT EXISTS queue_limit INT NOT NULL DEFAULT 100 CHECK(queue_limit BETWEEN 0 AND 10000);
ALTER TABLE upstream_principals ADD COLUMN IF NOT EXISTS last_instance_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE upstream_principals ADD COLUMN IF NOT EXISTS protected_until TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS principal_user_capacity (
    user_id BIGINT NOT NULL REFERENCES users(id),
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    occupied INT NOT NULL DEFAULT 0 CHECK(occupied>=0),
    PRIMARY KEY(user_id,principal_id)
);
CREATE TABLE IF NOT EXISTS session_bindings (
    id UUID PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    caller_scope_hash TEXT NOT NULL,
    session_hash TEXT NOT NULL,
    instance_id BIGINT NOT NULL,
    generation UUID NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    state TEXT NOT NULL DEFAULT 'ACTIVE' CHECK(state IN ('ACTIVE','DRAINING','EXPIRED','REVOKED')),
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    idle_expires_at TIMESTAMPTZ NOT NULL DEFAULT(CURRENT_TIMESTAMP+INTERVAL '24 hours'),
    tombstone_until TIMESTAMPTZ,
    FOREIGN KEY(instance_id,principal_id,generation) REFERENCES credential_instances(id,principal_id,identity_generation)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_active_binding_scope ON session_bindings(principal_id,caller_scope_hash,session_hash) WHERE state IN ('ACTIVE','DRAINING');
CREATE INDEX IF NOT EXISTS binding_scope_history_idx ON session_bindings(principal_id,caller_scope_hash,session_hash,last_used_at DESC);
CREATE TABLE IF NOT EXISTS logical_requests (
    id UUID PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id),
    caller_scope_hash TEXT NOT NULL,
    idempotency_hash TEXT NOT NULL,
    payload_digest TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'QUEUED' CHECK(status IN ('QUEUED','EXECUTING','COMPLETED','FAILED','UNKNOWN','CANCELLED')),
    owner_node TEXT NOT NULL,
    deadline TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(principal_id,caller_scope_hash,endpoint,idempotency_hash),
    UNIQUE(id,principal_id,user_id)
);
CREATE TABLE IF NOT EXISTS admission_tickets (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL UNIQUE REFERENCES logical_requests(id),
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    binding_id UUID REFERENCES session_bindings(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    instance_id BIGINT REFERENCES credential_instances(id),
    owner_node TEXT NOT NULL,
    reason TEXT NOT NULL,
    ready_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deadline TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL DEFAULT 'QUEUED' CHECK(state IN ('QUEUED','ADMITTED','CANCELLED','EXPIRED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS admission_queue_idx ON admission_tickets(principal_id,state,ready_at,created_at);
CREATE TABLE IF NOT EXISTS request_leases (
    id UUID PRIMARY KEY,
    request_id UUID NOT NULL,
    attempt_no INT NOT NULL CHECK(attempt_no>0),
    principal_id BIGINT NOT NULL,
    instance_id BIGINT NOT NULL,
    generation UUID NOT NULL,
    user_id BIGINT NOT NULL,
    owner_nonce UUID NOT NULL,
    epoch BIGINT NOT NULL,
    credential_version BIGINT NOT NULL,
    config_version BIGINT NOT NULL,
    binding_id UUID REFERENCES session_bindings(id),
    state TEXT NOT NULL CHECK(state IN ('RESERVED','DISPATCHING','RUNNING','CANCELLING','ORPHANED','RELEASED')),
    outcome TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK(outcome IN ('UNKNOWN','COMPLETED','FAILED','NOT_SENT','CANCELLED')),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deadline TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    released_at TIMESTAMPTZ,
    FOREIGN KEY(request_id,principal_id,user_id) REFERENCES logical_requests(id,principal_id,user_id),
    FOREIGN KEY(instance_id,principal_id,generation) REFERENCES credential_instances(id,principal_id,identity_generation),
    UNIQUE(request_id,attempt_no)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_unfinished_attempt_per_request ON request_leases(request_id) WHERE state<>'RELEASED';
CREATE INDEX IF NOT EXISTS request_leases_principal_idx ON request_leases(principal_id,state);
CREATE INDEX IF NOT EXISTS request_leases_heartbeat_idx ON request_leases(heartbeat_at) WHERE state<>'RELEASED';
CREATE TABLE IF NOT EXISTS credential_usage_events (
    event_id UUID PRIMARY KEY,
    lease_id UUID NOT NULL UNIQUE REFERENCES request_leases(id),
    outcome TEXT NOT NULL CHECK(outcome IN ('UNKNOWN','COMPLETED','FAILED','NOT_SENT','CANCELLED')),
    usage_state TEXT NOT NULL DEFAULT 'UNKNOWN' CHECK(usage_state IN ('UNKNOWN','KNOWN')),
    input_tokens BIGINT,
    output_tokens BIGINT,
    upstream_request_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    settled_at TIMESTAMPTZ
);
