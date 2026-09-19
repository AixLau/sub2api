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
	"github.com/lib/pq"
)

func NewCredentialInstanceLifecycle(db *sql.DB) service.CredentialInstanceLifecycle {
	return &credentialImportRepository{db: db}
}
func (r *credentialImportRepository) AddCredentialInstance(ctx context.Context, actor, principal, version int64, operation string, in service.CredentialInstanceAddInput) (int64, error) {
	in.Weight = 1
	if in.HardMax == nil {
		value := 10
		in.HardMax = &value
	}
	if actor <= 0 || principal <= 0 || version <= 0 || operation == "" || len(operation) > 128 || in.Name == "" || len(in.Name) > 100 || in.Weight <= 0 || math.IsNaN(in.Weight) || math.IsInf(in.Weight, 0) || (in.HardMax == nil || *in.HardMax < 0 || *in.HardMax > 2147483647) || len(in.GroupIDs) > 100 {
		return 0, errors.New("INVALID_INSTANCE_CONFIGURATION")
	}
	if _, err := uuid.Parse(in.ImportID); err != nil {
		return 0, service.ErrCredentialNotFound
	}
	if in.ReplaceInstanceID > 0 && (in.DrainDeadline == nil || !in.DrainDeadline.After(time.Now())) {
		return 0, errors.New("DRAIN_DEADLINE_REQUIRED")
	}
	op, _ := json.Marshal([]any{actor, principal, operation})
	key := service.CredentialDigest(op)
	payload, _ := json.Marshal(in)
	digest := service.CredentialDigest(payload)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = lockCredentialArbitration(ctx, tx); err != nil {
		return 0, err
	}
	var current int64
	var subject string
	err = tx.QueryRowContext(ctx, `SELECT config_version,COALESCE(verified_subject_key,'') FROM upstream_principals WHERE id=$1 AND tenant_id=1 AND verification_state='VERIFIED' AND archived_at IS NULL FOR NO KEY UPDATE`, principal).Scan(&current, &subject)
	if err != nil {
		return 0, err
	}
	var result int64
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT result_instance_id,payload_hash FROM credential_control_operations WHERE operation_hash=$1 AND actor_id=$2 AND principal_id=$3`, key, actor, principal).Scan(&result, &prior)
	if err == nil {
		if prior != digest {
			return 0, service.ErrCredentialConflict
		}
		return result, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if current != version {
		return 0, errCredentialConfigConflict
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_instances WHERE principal_id=$1 AND admin_state<>'REVOKED'`, principal).Scan(&count)
	if err != nil {
		return 0, err
	}
	if count >= 16 {
		return 0, errors.New("INSTANCE_LIMIT_REACHED")
	}
	var oldAccount int64
	if in.ReplaceInstanceID > 0 {
		err = tx.QueryRowContext(ctx, `SELECT account_id FROM credential_instances WHERE id=$1 AND principal_id=$2 FOR UPDATE`, in.ReplaceInstanceID, principal).Scan(&oldAccount)
		if err != nil {
			return 0, err
		}
	}
	var rec service.CredentialImportRecord
	var refresh, family sql.NullString
	var expires sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,verification_state,COALESCE(verified_subject_key,''),secret_ciphertext,access_fingerprint,refresh_fingerprint,refresh_family,capabilities,token_expires_at
 FROM credential_imports WHERE id=$1 AND owner_id=$2 AND tenant_id=1 AND expires_at>CURRENT_TIMESTAMP FOR UPDATE`, in.ImportID, actor).Scan(&rec.ID, &rec.State, &rec.SubjectKey, &rec.Ciphertext, &rec.AccessFingerprint, &refresh, &family, pq.Array(&rec.Capabilities), &expires)
	if err != nil {
		return 0, service.ErrCredentialImportExpired
	}
	if rec.State != "VERIFIED" {
		return 0, service.ErrCredentialUnverified
	}
	if rec.SubjectKey != subject {
		return 0, service.ErrCredentialOwnershipMismatch
	}
	if !expires.Valid || !expires.Time.After(time.Now()) {
		return 0, service.ErrCredentialImportExpired
	}
	rec.TokenExpiresAt = expires.Time
	rec.RefreshFingerprint = refresh.String
	rec.Family = family.String
	instance, err := createCredentialInstance(ctx, tx, principal, in.CreateCredentialInstanceInput, rec)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `SET LOCAL sub2api.credential_control='on'`)
	if err != nil {
		return 0, err
	}
	if oldAccount > 0 {
		// Replacement explicitly inherits only the old instance's existing grants.
		_, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id,priority) SELECT (SELECT account_id FROM credential_instances WHERE id=$1),group_id,priority FROM account_groups WHERE account_id=$2`, instance, oldAccount)
		if err != nil {
			return 0, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET admin_state='DRAINING',drain_deadline=$2 WHERE id=$1`, in.ReplaceInstanceID, in.DrainDeadline)
		if err != nil {
			return 0, err
		}
	} else if len(in.GroupIDs) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) SELECT (SELECT account_id FROM credential_instances WHERE id=$1),id FROM groups WHERE id=ANY($2) AND deleted_at IS NULL ON CONFLICT DO NOTHING`, instance, pq.Array(in.GroupIDs))
		if err != nil {
			return 0, err
		}
	}
	// Groups and routing policy belong to the management account. Adding an
	// authorization cannot broaden grants or change the account's limit.
	if oldAccount == 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id=(SELECT account_id FROM credential_instances WHERE id=$1)`, instance); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id,priority) SELECT (SELECT account_id FROM credential_instances WHERE id=$1),g.group_id,g.priority FROM account_groups g JOIN upstream_principals p ON p.management_account_id=g.account_id WHERE p.id=$2`, instance, principal); err != nil {
			return 0, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts a SET proxy_id=m.proxy_id,priority=m.priority,rate_multiplier=m.rate_multiplier,extra=m.extra FROM upstream_principals p JOIN accounts m ON m.id=p.management_account_id WHERE p.id=$2 AND a.id=(SELECT account_id FROM credential_instances WHERE id=$1)`, instance, principal); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE credential_instances i SET admin_state='ACTIVE' FROM upstream_principals p WHERE i.id=$1 AND p.id=i.principal_id AND p.routing_mode='GROUPED' AND p.admin_state='ACTIVE' AND p.archived_at IS NULL`, instance); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET config_version=config_version+1 WHERE id=$1`, principal)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_control_operations(operation_hash,principal_id,actor_id,payload_hash,result_instance_id) VALUES($1,$2,$3,$4,$5)`, key, principal, actor, digest, instance)
	if err != nil {
		return 0, err
	}
	if err = controlAudit(ctx, tx, actor, principal, instance, version+1, "INSTANCE_ADDED", in); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return instance, nil
}
