-- Admission rechecks the authoritative live ledger on every attempt. Exclude
-- terminal history from that lookup without changing which states count.
-- Apply during the supported offline migration window; CREATE INDEX takes a
-- write-blocking lock. The migration is additive and safe to retain on rollback.
CREATE INDEX IF NOT EXISTS request_leases_active_principal_idx
    ON request_leases(principal_id) WHERE state <> 'RELEASED';
