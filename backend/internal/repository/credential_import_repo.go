package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type credentialImportRepository struct{ db *sql.DB }

func NewCredentialImportRepository(db *sql.DB) service.CredentialImportStore {
	return &credentialImportRepository{db: db}
}
func (r *credentialImportRepository) PutImport(ctx context.Context, in service.CredentialImportRecord) (service.CredentialImportView, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.CredentialImportView{}, err
	}
	defer tx.Rollback()
	if _, err = lockCredentialArbitration(ctx, tx); err != nil {
		return service.CredentialImportView{}, err
	}
	if err = lockCredentialTokens(ctx, tx, []string{in.AccessFingerprint, in.RefreshFingerprint, in.Family}); err != nil {
		return service.CredentialImportView{}, err
	}
	if err = rejectPendingLegacyRefresh(ctx, tx, []string{in.AccessFingerprint, in.RefreshFingerprint}); err != nil {
		return service.CredentialImportView{}, err
	}

	// Unique owner/operation provides response-loss recovery without retaining plaintext.
	var id, state, payload string
	var valid bool
	var canRefresh bool
	var view service.CredentialImportView
	err = tx.QueryRowContext(ctx, `SELECT id,verification_state,payload_hash,refresh_fingerprint IS NOT NULL,expires_at,expires_at>CURRENT_TIMESTAMP
 FROM credential_imports WHERE tenant_id=$1 AND owner_id=$2 AND operation_hash=$3`, in.Scope, in.OwnerID, in.OperationHash).Scan(&id, &state, &payload, &canRefresh, &view.ExpiresAt, &valid)
	if err == nil {
		if payload != in.PayloadHash {
			return view, service.ErrCredentialConflict
		}
		if !valid {
			return view, service.ErrCredentialImportExpired
		}
		view.ID = id
		view.State = state
		view.CanRefresh = canRefresh
		return view, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return view, err
	}
	var location service.CredentialDuplicateLocation
	err = tx.QueryRowContext(ctx, `SELECT p.management_account_id,i.principal_id,i.id FROM credential_fingerprints f JOIN credential_instances i ON i.id=f.instance_id JOIN upstream_principals p ON p.id=i.principal_id WHERE f.fingerprint IN ($1,$2,$3) ORDER BY i.id LIMIT 1`, in.AccessFingerprint, in.RefreshFingerprint, in.Family).Scan(&location.AccountID, &location.PrincipalID, &location.InstanceID)
	if err == nil {
		return view, &location
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return view, err
	}
	blocked, err := hasForeignCredentialAliasClaims(ctx, tx, []string{in.AccessFingerprint, in.RefreshFingerprint}, 0, "")
	if err != nil {
		return service.CredentialImportView{}, err
	}
	if blocked {
		return service.CredentialImportView{}, service.ErrCredentialDuplicate
	}
	var duplicate bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credential_fingerprints WHERE fingerprint IN ($1,$2,$3))`, in.AccessFingerprint, in.RefreshFingerprint, in.Family).Scan(&duplicate)
	if err != nil {
		return view, err
	}
	if duplicate {
		return view, service.ErrCredentialDuplicate
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO credential_imports(id,tenant_id,owner_id,operation_hash,payload_hash,
 secret_ciphertext,access_fingerprint,refresh_fingerprint,verification_state,verified_subject_key,refresh_family,capabilities,token_expires_at,expires_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,NULLIF($10,''),NULLIF($11,''),$12,$13,$14)
 ON CONFLICT(tenant_id,owner_id,operation_hash) DO NOTHING RETURNING id`, in.ID, in.Scope, in.OwnerID, in.OperationHash, in.PayloadHash,
		in.Ciphertext, in.AccessFingerprint, in.RefreshFingerprint, in.State, in.SubjectKey, in.Family, pq.Array(in.Capabilities), nullableCredentialTime(in.TokenExpiresAt), in.ExpiresAt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		// Another node won this import operation. Read its durable result after rollback.
		tx.Rollback()
		return r.PutImport(ctx, in)
	}
	if err != nil {
		var p *pq.Error
		if errors.As(err, &p) && p.Code == "23505" {
			return view, service.ErrCredentialDuplicate
		}
		return view, err
	}
	for _, fp := range []struct{ kind, value string }{{"TOKEN", in.AccessFingerprint}, {"TOKEN", in.RefreshFingerprint}, {"FAMILY", in.Family}} {
		if fp.value == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_import_fingerprints(fingerprint,import_id,kind) VALUES($1,$2,$3)`, fp.value, id, fp.kind)
		if err != nil {
			return view, credentialControlError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return view, err
	}
	return service.CredentialImportView{ID: id, State: in.State, CanRefresh: in.RefreshFingerprint != "", ExpiresAt: in.ExpiresAt}, nil
}
func (r *credentialImportRepository) GetImport(ctx context.Context, scope, owner int64, id string) (*service.CredentialImportView, error) {
	var view service.CredentialImportView
	err := r.db.QueryRowContext(ctx, `SELECT id,verification_state,refresh_fingerprint IS NOT NULL,expires_at FROM credential_imports
 WHERE id=$1 AND tenant_id=$2 AND owner_id=$3 AND expires_at>CURRENT_TIMESTAMP`, id, scope, owner).Scan(&view.ID, &view.State, &view.CanRefresh, &view.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func nullableCredentialTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func (r *credentialImportRepository) ConfigureCredentialArbitration(ctx context.Context, vault *service.CredentialVault) error {
	return configureCredentialArbitration(ctx, r.db, vault)
}

func (r *credentialImportRepository) PendingCredentialImport(ctx context.Context, owner int64, id string) (service.CredentialImportRecord, error) {
	var rec service.CredentialImportRecord
	err := r.db.QueryRowContext(ctx, `SELECT id,verification_state,secret_ciphertext,expires_at FROM credential_imports WHERE id=$1 AND owner_id=$2 AND tenant_id=1 AND expires_at>CURRENT_TIMESTAMP`, id, owner).Scan(&rec.ID, &rec.State, &rec.Ciphertext, &rec.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = service.ErrCredentialImportExpired
	}
	return rec, err
}
func (r *credentialImportRepository) VerifyCredentialImport(ctx context.Context, owner int64, rec service.CredentialImportRecord) (service.CredentialImportView, error) {
	var view service.CredentialImportView
	err := r.db.QueryRowContext(ctx, `UPDATE credential_imports SET verification_state='VERIFIED',verified_subject_key=$3,secret_ciphertext=$4,capabilities=$5,token_expires_at=$6 WHERE id=$1 AND owner_id=$2 AND tenant_id=1 AND expires_at>CURRENT_TIMESTAMP AND verification_state='UNVERIFIED' RETURNING id,verification_state,expires_at,refresh_fingerprint IS NOT NULL`, rec.ID, owner, rec.SubjectKey, rec.Ciphertext, pq.Array(rec.Capabilities), rec.TokenExpiresAt).Scan(&view.ID, &view.State, &view.ExpiresAt, &view.CanRefresh)
	if errors.Is(err, sql.ErrNoRows) {
		current, e := r.GetImport(ctx, 1, owner, rec.ID)
		if e != nil {
			return view, e
		}
		return *current, nil
	}
	return view, err
}
