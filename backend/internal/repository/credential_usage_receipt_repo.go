package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *principalAdmissionStore) SaveCredentialUsageReceipt(ctx context.Context, in service.CredentialUsageReceipt) error {
	payload, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if len(payload) > 65536 {
		return errors.New("CREDENTIAL_USAGE_RECEIPT_TOO_LARGE")
	}
	result, err := s.criticalDB().ExecContext(ctx, `INSERT INTO credential_usage_receipts(lease_id,receipt,state,reason)
 SELECT l.id,$2::jsonb,CASE WHEN $6 THEN 'PENDING' ELSE 'REVIEW_REQUIRED' END,CASE WHEN $6 THEN '' ELSE 'USAGE_UNKNOWN' END FROM request_leases l JOIN logical_requests r ON r.id=l.request_id JOIN credential_instances i ON i.id=l.instance_id
 WHERE l.id=$1 AND r.user_id=$3 AND r.api_key_id=$4 AND i.account_id=$5
 ON CONFLICT(lease_id) DO NOTHING`, in.LeaseID, string(payload), in.UserID, in.APIKeyID, in.AccountID, in.Result.Usage.HasBillableUsage() || in.Result.ImageCount > 0)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var stored []byte
		if err = s.criticalDB().QueryRowContext(ctx, `SELECT receipt FROM credential_usage_receipts WHERE lease_id=$1`, in.LeaseID).Scan(&stored); err != nil {
			return err
		}
		var original service.CredentialUsageReceipt
		if json.Unmarshal(stored, &original) != nil || original.UserID != in.UserID || original.APIKeyID != in.APIKeyID || original.AccountID != in.AccountID {
			return service.ErrUsageBillingRequestConflict
		}
		canonical, _ := json.Marshal(original)
		if !bytes.Equal(canonical, payload) {
			return service.ErrUsageBillingRequestConflict
		}
	}
	return nil
}
func (s *principalAdmissionStore) PendingCredentialUsageReceipts(ctx context.Context) ([]service.CredentialUsageReceipt, error) {
	rows, err := s.criticalDB().QueryContext(ctx, `SELECT c.receipt,c.state<>'PENDING' FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id
 WHERE c.state='PENDING' OR (l.state<>'RELEASED' AND (c.receipt->>'Complete')::boolean IS TRUE) ORDER BY c.created_at LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []service.CredentialUsageReceipt
	for rows.Next() {
		var data []byte
		var r service.CredentialUsageReceipt
		var skipSettlement bool
		if err = rows.Scan(&data, &skipSettlement); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		r.SkipSettlement = skipSettlement
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *principalAdmissionStore) CompleteCredentialUsageReceipt(ctx context.Context, lease, state, reason string) error {
	_, err := s.criticalDB().ExecContext(ctx, `UPDATE credential_usage_receipts SET state=$2,reason=$3,recorded_at=CASE WHEN $2='RECORDED' THEN CURRENT_TIMESTAMP END WHERE lease_id=$1 AND state='PENDING'`, lease, state, reason)
	return err
}

func (s *usageBillingRepository) CompleteCredentialUsageReceipt(ctx context.Context, lease, state, reason string) error {
	return (&principalAdmissionStore{db: s.db}).CompleteCredentialUsageReceipt(ctx, lease, state, reason)
}
