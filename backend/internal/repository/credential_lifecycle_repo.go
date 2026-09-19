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
	if actor <= 0 || principal <= 0 || version <= 0 || operation == "" || len(operation) > 128 || in.Name == "" || len(in.Name) > 100 || in.Weight <= 0 || math.IsNaN(in.Weight) || math.IsInf(in.Weight, 0) || (in.HardMax != nil && *in.HardMax < 0) || len(in.GroupIDs) > 100 {
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
	err = tx.QueryRowContext(ctx, `SELECT config_version,COALESCE(verified_subject_key,'') FROM upstream_principals WHERE id=$1 AND tenant_id=1 AND verification_state='VERIFIED' FOR NO KEY UPDATE`, principal).Scan(&current, &subject)
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
	if rec.State != "VERIFIED" || rec.SubjectKey != subject {
		return 0, service.ErrCredentialUnverified
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
