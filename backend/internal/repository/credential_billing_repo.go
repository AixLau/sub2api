package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

func (r *usageBillingRepository) applyCredentialBilling(ctx context.Context, lease string, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if _, err := uuid.Parse(lease); err != nil {
		return nil, service.ErrUsageBillingRequestConflict
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}
	// Persist the exact priced command before applying. This outbox can survive a
	// process loss and replay through the existing billing dedup transaction.
	_, err = r.db.ExecContext(ctx, `INSERT INTO credential_billing_outbox(lease_id,command) SELECT $1,$2::jsonb
 WHERE EXISTS(SELECT 1 FROM request_leases l JOIN logical_requests q ON q.id=l.request_id JOIN credential_instances i ON i.id=l.instance_id
 WHERE l.id=$1 AND q.user_id=$3 AND q.api_key_id=$4 AND i.account_id=$5)
 ON CONFLICT(lease_id) DO NOTHING`, lease, string(payload), cmd.UserID, cmd.APIKeyID, cmd.AccountID)
	if err != nil {
		return nil, err
	}
	var stored []byte
	if err = r.db.QueryRowContext(ctx, `SELECT command FROM credential_billing_outbox WHERE lease_id=$1`, lease).Scan(&stored); err != nil {
		return nil, err
	}
	var original service.UsageBillingCommand
	if err = json.Unmarshal(stored, &original); err != nil {
		return nil, err
	}
	if original.RequestFingerprint != cmd.RequestFingerprint {
		return nil, service.ErrUsageBillingRequestConflict
	}
	return r.settleCredentialBilling(ctx, lease, &original)
}
func (r *usageBillingRepository) settleCredentialBilling(ctx context.Context, lease string, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// User balance first to avoid an outbox->user reversal across billing paths.
	var user int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE`, cmd.UserID).Scan(&user)
	if err != nil {
		return nil, err
	}
	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	result := &service.UsageBillingApplyResult{Applied: applied}
	if applied {
		if err = r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_billing_outbox SET settled_at=CURRENT_TIMESTAMP WHERE lease_id=$1`, lease)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_usage_events SET settled_at=CURRENT_TIMESTAMP WHERE lease_id=$1`, lease)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *credentialOperations) replayCredentialBilling(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT lease_id,command FROM credential_billing_outbox WHERE settled_at IS NULL ORDER BY created_at LIMIT 20`)
	if err != nil {
		return err
	}
	type entry struct {
		id      string
		command service.UsageBillingCommand
	}
	var entries []entry
	for rows.Next() {
		var e entry
		var raw []byte
		if err = rows.Scan(&e.id, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &e.command); err != nil {
			rows.Close()
			return errors.New("invalid persisted billing command")
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	repo := &usageBillingRepository{db: s.db}
	for _, e := range entries {
		if _, err = repo.settleCredentialBilling(ctx, e.id, &e.command); err != nil {
			return err
		}
	}
	return nil
}
