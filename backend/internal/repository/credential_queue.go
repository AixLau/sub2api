package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *principalAdmissionStore) CancelQueued(ctx context.Context, in service.AdmissionInput) error {
	defer s.signalQueue()
	s.initializeQueue()
	s.waitMu.Lock()
	delete(s.retryReady, in.RequestID)
	s.waitMu.Unlock()
	tx, err := s.criticalDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM principal_user_capacity WHERE user_id=$1 AND principal_id=$2 FOR UPDATE`, in.UserID, in.PrincipalID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, in.PrincipalID).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM logical_requests WHERE id=$1 AND user_id=$2 AND api_key_id IS NOT DISTINCT FROM $3 AND owner_node=$4 FOR UPDATE`, in.RequestID, in.UserID, nullablePositive(in.APIKeyID), in.Node).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
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
