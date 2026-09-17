CREATE TABLE IF NOT EXISTS credential_control_operations (
    operation_hash TEXT PRIMARY KEY,
    principal_id BIGINT NOT NULL REFERENCES upstream_principals(id),
    actor_id BIGINT NOT NULL REFERENCES users(id),
    payload_hash TEXT NOT NULL,
    result_instance_id BIGINT NOT NULL REFERENCES credential_instances(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
