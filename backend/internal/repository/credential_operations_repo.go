package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

var errCredentialConfigConflict = errors.New("CONFIG_VERSION_CONFLICT")

type credentialOperations struct{ db *sql.DB }

func NewCredentialOperations(db *sql.DB) service.CredentialOperations {
	return &credentialOperations{db: db}
}
func validCredentialAdminState(state string) bool {
	switch state {
	case "", "ACTIVE", "DRAINING", "PAUSED", "DISABLED", "REVOKED":
		return true
	}
	return false
}
func (s *credentialOperations) UpdatePrincipal(ctx context.Context, actor, id, version int64, in service.PrincipalControlUpdate) (*service.PrincipalView, error) {
	if actor <= 0 || version <= 0 || !validCredentialAdminState(in.AdminState) || (in.RequestedLimit != nil && *in.RequestedLimit < 0) {
		return nil, errors.New("INVALID_CONTROL_CONFIGURATION")
	}
	if in.AdminState == "DRAINING" && (in.DrainDeadline == nil || !in.DrainDeadline.After(time.Now())) {
		return nil, errors.New("DRAIN_DEADLINE_REQUIRED")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT config_version FROM upstream_principals WHERE id=$1 AND tenant_id=1 FOR NO KEY UPDATE`, id).Scan(&current)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, errCredentialConfigConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET requested_limit=COALESCE($2,requested_limit),admin_state=COALESCE(NULLIF($3,''),admin_state),drain_deadline=COALESCE($4,drain_deadline),config_version=config_version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, in.RequestedLimit, in.AdminState, in.DrainDeadline)
	if err != nil {
		return nil, err
	}
	if err = controlAudit(ctx, tx, actor, id, 0, version+1, "PRINCIPAL_UPDATED", in); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return NewUpstreamPrincipalReader(s.db).GetPrincipal(ctx, 1, id)
}
func (s *credentialOperations) UpdateInstance(ctx context.Context, actor, id, version int64, in service.InstanceControlUpdate) (*service.PrincipalView, error) {
	if actor <= 0 || version <= 0 || !validCredentialAdminState(in.AdminState) || (in.Weight != nil && (*in.Weight <= 0 || math.IsNaN(*in.Weight) || math.IsInf(*in.Weight, 0))) || (in.HardMax != nil && *in.HardMax < 0) || (in.HealthCapacity != nil && *in.HealthCapacity < 0) || (in.ClearHardMax && in.HardMax != nil) {
		return nil, errors.New("INVALID_CONTROL_CONFIGURATION")
	}
	if in.AdminState == "DRAINING" && (in.DrainDeadline == nil || !in.DrainDeadline.After(time.Now())) {
		return nil, errors.New("DRAIN_DEADLINE_REQUIRED")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var principal, current int64
	err = tx.QueryRowContext(ctx, `SELECT principal_id FROM credential_instances WHERE id=$1`, id).Scan(&principal)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `SELECT config_version FROM upstream_principals WHERE id=$1 AND tenant_id=1 FOR NO KEY UPDATE`, principal).Scan(&current)
	if err != nil {
		return nil, err
	}
	if current != version {
		return nil, errCredentialConfigConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET weight=COALESCE($2,weight),hard_max=CASE WHEN $3 THEN NULL ELSE COALESCE($4,hard_max) END,
 health_capacity=COALESCE($5,health_capacity),admin_state=COALESCE(NULLIF($6,''),admin_state),drain_deadline=COALESCE($7,drain_deadline),updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, in.Weight, in.ClearHardMax, in.HardMax, in.HealthCapacity, in.AdminState, in.DrainDeadline)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET config_version=config_version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, principal)
	if err != nil {
		return nil, err
	}
	if in.AdminState == "REVOKED" {
		_, err = tx.ExecContext(ctx, `UPDATE session_bindings SET state='REVOKED',version=version+1 WHERE instance_id=$1 AND state IN ('ACTIVE','DRAINING')`, id)
		if err != nil {
			return nil, err
		}
	}
	if err = controlAudit(ctx, tx, actor, principal, id, version+1, "INSTANCE_UPDATED", in); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return NewUpstreamPrincipalReader(s.db).GetPrincipal(ctx, 1, principal)
}
func controlAudit(ctx context.Context, tx *sql.Tx, actor, principal, instance, version int64, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_audit_outbox(event_id,principal_id,instance_id,actor_id,version,event_type,safe_payload) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), principal, nullablePositive(instance), actor, version, event, string(data))
	return err
}
func (s *credentialOperations) CredentialRuntime(ctx context.Context, id int64) (service.CredentialRuntimeView, error) {
	var v service.CredentialRuntimeView
	v.PrincipalID = id
	err := s.db.QueryRowContext(ctx, `SELECT p.occupied,
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state='RESERVED'),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state='DISPATCHING'),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state='RUNNING'),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state='CANCELLING'),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state='ORPHANED'),
 (SELECT count(*) FROM admission_tickets WHERE principal_id=p.id AND state='QUEUED' AND deadline>CURRENT_TIMESTAMP),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state<>'RELEASED'),
 (SELECT COALESCE(sum(occupied),0) FROM credential_instances WHERE principal_id=p.id),
 (SELECT count(*) FROM credential_usage_events e JOIN request_leases l ON l.id=e.lease_id WHERE l.principal_id=p.id AND e.usage_state='UNKNOWN'),
 (SELECT count(*) FROM credential_refresh_ops o JOIN credential_instances i ON i.id=o.instance_id WHERE i.principal_id=p.id AND o.state='REFRESH_RESULT_UNKNOWN'),
 CURRENT_TIMESTAMP FROM upstream_principals p WHERE p.id=$1 AND p.tenant_id=1`, id).Scan(&v.Occupied, &v.Reserved, &v.Dispatching, &v.Running, &v.Cancelling, &v.Orphaned, &v.Queued, &v.LedgerOccupied, &v.InstanceOccupied, &v.UnknownUsage, &v.UnknownRefresh, &v.ObservedAt)
	v.CounterMismatch = v.Occupied != v.LedgerOccupied || v.Occupied != v.InstanceOccupied
	return v, err
}
func (s *credentialOperations) ResolveCredentialLease(ctx context.Context, actor int64, id string, in service.CredentialResolveInput) error {
	if actor <= 0 || !in.Confirm || len(in.Evidence) < 8 || len(in.Evidence) > 4096 || len(in.Reason) < 4 || len(in.Reason) > 512 {
		return errors.New("RESOLUTION_EVIDENCE_REQUIRED")
	}
	ref, err := s.leaseRef(ctx, id)
	if err != nil {
		return err
	}
	store := &principalAdmissionStore{db: s.db}
	tx, state, err := store.lockLease(ctx, ref)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if state == "RELEASED" {
		return nil
	}
	if state != "ORPHANED" {
		return errors.New("LEASE_NOT_ORPHANED")
	}
	if err = releaseAdmission(ctx, tx, ref, "FAILED"); err != nil {
		return err
	}
	// Never convert unknown usage into zero by resolving capacity.
	if err = controlAudit(ctx, tx, actor, ref.PrincipalID, ref.InstanceID, ref.Epoch, "ORPHAN_RESOLVED", in); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *credentialOperations) leaseRef(ctx context.Context, id string) (service.LeaseRef, error) {
	var ref service.LeaseRef
	err := s.db.QueryRowContext(ctx, `SELECT id,request_id,principal_id,instance_id,generation,user_id,owner_nonce,epoch FROM request_leases WHERE id=$1`, id).Scan(&ref.ID, &ref.RequestID, &ref.PrincipalID, &ref.InstanceID, &ref.Generation, &ref.UserID, &ref.OwnerNonce, &ref.Epoch)
	return ref, err
}
func (s *credentialOperations) ReconcileCredentialLeases(ctx context.Context) (int, error) {
	if err := s.deliverCredentialAudit(ctx); err != nil {
		return 0, err
	}
	if err := s.replayCredentialBilling(ctx); err != nil {
		return 0, err
	}
	if err := s.cancelExpiredCredentialTickets(ctx); err != nil {
		return 0, err
	}
	// Destroy expired import ciphertext, retain only opaque audit/dedup metadata.
	if _, err := s.db.ExecContext(ctx, `UPDATE credential_imports SET secret_ciphertext=''::bytea,secret_erased_at=CURRENT_TIMESTAMP WHERE expires_at<=CURRENT_TIMESTAMP AND secret_erased_at IS NULL`); err != nil {
		return 0, err
	}
	if err := s.reconcileUnknownRefresh(ctx); err != nil {
		return 0, err
	}
	if err := s.freezeLedgerMismatches(ctx); err != nil {
		return 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM request_leases WHERE state IN ('RESERVED','DISPATCHING','RUNNING','CANCELLING') AND heartbeat_at<CURRENT_TIMESTAMP-INTERVAL '30 seconds' ORDER BY heartbeat_at LIMIT 100`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		ref, err := s.leaseRef(ctx, id)
		if err != nil {
			return count, err
		}
		store := &principalAdmissionStore{db: s.db}
		tx, state, err := store.lockLease(ctx, ref)
		if err != nil {
			return count, err
		}
		var stale bool
		err = tx.QueryRowContext(ctx, `SELECT heartbeat_at<CURRENT_TIMESTAMP-INTERVAL '30 seconds' FROM request_leases WHERE id=$1`, id).Scan(&stale)
		if err != nil {
			tx.Rollback()
			return count, err
		}
		if !stale || state == "RELEASED" || state == "ORPHANED" {
			tx.Rollback()
			continue
		}
		if state == "RESERVED" {
			err = releaseAdmission(ctx, tx, ref, "NOT_SENT")
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE request_leases SET state='ORPHANED',outcome='UNKNOWN' WHERE id=$1`, id)
			if err == nil {
				_, err = tx.ExecContext(ctx, `UPDATE logical_requests SET status='UNKNOWN' WHERE id=$1`, ref.RequestID)
			}
			if err == nil {
				err = admissionAudit(ctx, tx, ref, "LEASE_ORPHANED")
			}
		}
		if err != nil {
			tx.Rollback()
			return count, err
		}
		if err = tx.Commit(); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *credentialOperations) freezeLedgerMismatches(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id FROM upstream_principals p WHERE p.routing_mode='GROUPED' AND p.admin_state<>'DISABLED' AND (
 p.occupied<>(SELECT count(*) FROM request_leases l WHERE l.principal_id=p.id AND l.state<>'RELEASED') OR
 p.occupied<>(SELECT COALESCE(sum(i.occupied),0) FROM credential_instances i WHERE i.principal_id=p.id)) ORDER BY p.id LIMIT 100`)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var version int64
		err = tx.QueryRowContext(ctx, `SELECT config_version FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&version)
		if err != nil {
			tx.Rollback()
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE upstream_principals p SET admin_state='DISABLED',config_version=config_version+1
 WHERE p.id=$1 AND (p.occupied<>(SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state<>'RELEASED') OR
 p.occupied<>(SELECT COALESCE(sum(occupied),0) FROM credential_instances WHERE principal_id=p.id))`, id)
		if err != nil {
			tx.Rollback()
			return err
		}
		n, _ := result.RowsAffected()
		if n > 0 {
			err = controlAudit(ctx, tx, 0, id, 0, version+1, "LEDGER_MISMATCH_FROZEN", struct{}{})
			if err != nil {
				tx.Rollback()
				return err
			}
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *credentialOperations) DueCredentialRefreshes(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id FROM credential_instances i JOIN credential_secrets c ON c.instance_id=i.id AND c.credential_version=i.credential_version
 JOIN upstream_principals p ON p.id=i.principal_id WHERE p.routing_mode='GROUPED' AND p.admin_state IN ('ACTIVE','DRAINING')
 AND i.admin_state IN ('ACTIVE','DRAINING') AND i.credential_state IN ('VALID','NEEDS_REAUTH') AND c.can_refresh AND c.refresh_family IS NOT NULL
 AND (c.expires_at<CURRENT_TIMESTAMP+INTERVAL '5 minutes' OR i.credential_state='NEEDS_REAUTH')
 AND NOT EXISTS(SELECT 1 FROM credential_refresh_ops o WHERE o.family_key=c.refresh_family AND o.state IN ('SENDING','REFRESH_RESULT_UNKNOWN')) ORDER BY c.expires_at LIMIT 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *credentialOperations) reconcileUnknownRefresh(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,instance_id,generation,family_key,expected_version,owner_nonce FROM credential_refresh_ops WHERE state='SENDING' AND started_at<CURRENT_TIMESTAMP-INTERVAL '60 seconds' ORDER BY started_at LIMIT 100`)
	if err != nil {
		return err
	}
	var ops []service.CredentialRefreshOperation
	for rows.Next() {
		var op service.CredentialRefreshOperation
		if err = rows.Scan(&op.ID, &op.InstanceID, &op.Generation, &op.Family, &op.ExpectedVersion, &op.OwnerNonce); err != nil {
			rows.Close()
			return err
		}
		ops = append(ops, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	store := &credentialRefreshStore{db: s.db}
	for _, op := range ops {
		if err = s.db.QueryRowContext(ctx, `SELECT principal_id FROM credential_instances WHERE id=$1`, op.InstanceID).Scan(&op.PrincipalID); err != nil {
			return err
		}
		if err = store.MarkCredentialRefreshUnknown(ctx, op, nil); err != nil && err != service.ErrAdmissionOwnership {
			return err
		}
	}
	return nil
}

func (s *credentialOperations) cancelExpiredCredentialTickets(ctx context.Context) error {
	// Tickets have no execution resource. A single SQL statement safely expires
	// only queued records; admitted tickets and their leases cannot be affected.
	_, err := s.db.ExecContext(ctx, `WITH expired AS (
 UPDATE admission_tickets SET state='EXPIRED' WHERE state='QUEUED' AND deadline<=CURRENT_TIMESTAMP RETURNING request_id
 ) UPDATE logical_requests r SET status='CANCELLED' FROM expired e WHERE r.id=e.request_id AND r.status='QUEUED'`)
	return err
}

func (s *credentialOperations) deliverCredentialAudit(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT event_id,principal_id,instance_id,actor_id,version,event_type FROM credential_audit_outbox WHERE delivered_at IS NULL ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	type event struct {
		id, kind                   string
		principal, instance, actor sql.NullInt64
		version                    int64
	}
	var events []event
	for rows.Next() {
		var e event
		if err = rows.Scan(&e.id, &e.principal, &e.instance, &e.actor, &e.version, &e.kind); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range events {
		// Public audit contains only typed internal identifiers; free-text evidence
		// remains in the access-controlled ledger and is not copied into log sinks.
		_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,actor_role,auth_method,action,request_id,status_code,extra)
 VALUES($1,'admin','credential_control',$2,$3,200,jsonb_build_object('principal_id',$4::bigint,'instance_id',$5::bigint,'version',$6::bigint))`, e.actor, "credential."+e.kind, e.id, e.principal, e.instance, e.version)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE credential_audit_outbox SET delivered_at=CURRENT_TIMESTAMP WHERE event_id=$1`, e.id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
