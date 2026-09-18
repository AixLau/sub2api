-- Scheduling hints only. An offer never changes occupied or creates a lease.
ALTER TABLE admission_tickets ADD COLUMN offer_until TIMESTAMPTZ;
ALTER TABLE admission_tickets ADD COLUMN offer_instance_id BIGINT REFERENCES credential_instances(id);
CREATE INDEX admission_offer_idx ON admission_tickets(principal_id,offer_until) WHERE state='QUEUED';
