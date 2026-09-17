package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"time"
)

// Called under the principal lock. A ticket is an eligibility hint; only the
// ticket's owner may subsequently win admission and send its retained body.
func credentialQueuePrecedes(ctx context.Context, tx *sql.Tx, principal, user int64, request string, selected int64, total int, now time.Time) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT i.id,i.weight,i.occupied,
 LEAST($2,COALESCE(i.hard_max,$2),COALESCE(i.health_capacity,$2)),
 (SELECT count(*) FROM admission_tickets t WHERE t.principal_id=i.principal_id AND t.state='QUEUED'
 AND t.deadline>CURRENT_TIMESTAMP AND t.ready_at<=CURRENT_TIMESTAMP AND t.instance_id=i.id)
 FROM credential_instances i WHERE i.principal_id=$1 AND i.admin_state IN ('ACTIVE','DRAINING') AND i.credential_state='VALID'
 AND i.transport_state IN ('HEALTHY','DEGRADED','HALF_OPEN') AND (i.cooldown_until IS NULL OR i.cooldown_until<=CURRENT_TIMESTAMP)`, principal, total)
	if err != nil {
		return false, err
	}
	var demands []service.CredentialDemand
	occupied := map[int64]int{}
	hard := map[int64]int{}
	for rows.Next() {
		var id int64
		var weight float64
		var o, h, d int
		if err = rows.Scan(&id, &weight, &o, &h, &d); err != nil {
			rows.Close()
			return false, err
		}
		demands = append(demands, service.CredentialDemand{ID: id, Weight: weight, Demand: float64(o + d), HardCapacity: float64(h)})
		occupied[id] = o
		hard[id] = h
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	targets := service.CredentialTargets(float64(total), demands)
	var currentLast sql.NullTime
	var currentCreated time.Time
	err = tx.QueryRowContext(ctx, `SELECT last_admitted_at,COALESCE((SELECT created_at FROM admission_tickets WHERE request_id=$3),CURRENT_TIMESTAMP)
 FROM principal_user_capacity WHERE principal_id=$1 AND user_id=$2`, principal, user, request).Scan(&currentLast, &currentCreated)
	if err != nil {
		return false, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT t.user_id,t.instance_id,t.created_at,c.last_admitted_at
 FROM admission_tickets t JOIN logical_requests r ON r.id=t.request_id JOIN api_keys k ON k.id=r.api_key_id
 JOIN principal_user_capacity c ON c.principal_id=t.principal_id AND c.user_id=t.user_id
 JOIN upstream_principals p ON p.id=t.principal_id
 WHERE t.principal_id=$1 AND t.request_id<>$2 AND t.state='QUEUED' AND t.deadline>CURRENT_TIMESTAMP AND t.ready_at<=CURRENT_TIMESTAMP
 AND k.status='active' AND k.deleted_at IS NULL AND (p.user_concurrency_limit IS NULL OR c.occupied<p.user_concurrency_limit)
 AND EXISTS(SELECT 1 FROM credential_instances i JOIN account_groups g ON g.account_id=i.account_id
 WHERE i.principal_id=t.principal_id AND g.group_id=k.group_id AND i.id=ANY(t.candidate_ids)
 AND (t.instance_id IS NULL OR t.instance_id=i.id) AND r.endpoint=ANY(i.capabilities)
 AND i.admin_state='ACTIVE' AND i.credential_state='VALID' AND i.occupied<LEAST(p.requested_limit,COALESCE(i.hard_max,p.requested_limit),COALESCE(i.health_capacity,p.requested_limit))
 AND i.transport_state IN ('HEALTHY','DEGRADED','HALF_OPEN') AND (i.cooldown_until IS NULL OR i.cooldown_until<=CURRENT_TIMESTAMP))
 ORDER BY c.last_admitted_at NULLS FIRST,t.created_at,t.id`, principal, request)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	under := func(id int64) bool { return hard[id] > occupied[id] && float64(occupied[id]) < targets[id] }
	for rows.Next() {
		var otherUser int64
		var instance sql.NullInt64
		var created time.Time
		var last sql.NullTime
		if err = rows.Scan(&otherUser, &instance, &created, &last); err != nil {
			return false, err
		}
		otherUnder := instance.Valid && under(instance.Int64)
		currentUnder := under(selected)
		if otherUnder && !currentUnder {
			return true, nil
		}
		if currentUnder && !otherUnder {
			continue
		}
		if (!last.Valid && currentLast.Valid) || (last.Valid && currentLast.Valid && last.Time.Before(currentLast.Time)) ||
			(last.Valid == currentLast.Valid && (!last.Valid || last.Time.Equal(currentLast.Time)) && created.Before(currentCreated)) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *principalAdmissionStore) CancelQueued(ctx context.Context, in service.AdmissionInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM principal_user_capacity WHERE user_id=$1 AND principal_id=$2 FOR UPDATE`, in.UserID, in.PrincipalID).Scan(&n)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, in.PrincipalID).Scan(&n)
	if err != nil {
		return err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM logical_requests WHERE id=$1 AND user_id=$2 AND api_key_id=$3 AND owner_node=$4 FOR UPDATE`, in.RequestID, in.UserID, in.APIKeyID, in.Node).Scan(&status)
	if err != nil {
		return err
	}
	if status == "EXECUTING" {
		return service.ErrAdmissionOwnership
	} // caller must resolve the lease cancellation race
	if status == "QUEUED" {
		_, err = tx.ExecContext(ctx, `UPDATE logical_requests SET status='CANCELLED' WHERE id=$1`, in.RequestID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE admission_tickets SET state='CANCELLED' WHERE request_id=$1 AND state='QUEUED'`, in.RequestID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
