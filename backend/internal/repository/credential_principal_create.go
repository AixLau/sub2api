package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func NewCredentialPrincipalCreator(db *sql.DB) service.CredentialPrincipalCreator {
	return &credentialImportRepository{db: db}
}

func (r *credentialImportRepository) CreateCredentialPrincipal(ctx context.Context, owner int64, operation string, in service.CreateCredentialPrincipalInput) (int64, error) {
	if owner <= 0 || operation == "" || len(operation) > 128 || in.Name == "" || len(in.Name) > 100 || in.TotalConcurrency < 0 || len(in.Instances) < 1 || len(in.Instances) > 16 {
		return 0, errors.New("INVALID_PRINCIPAL_CONFIGURATION")
	}
	seen := map[string]bool{}
	for _, v := range in.Instances {
		if _, err := uuid.Parse(v.ImportID); err != nil {
			return 0, service.ErrCredentialNotFound
		}
		if seen[v.ImportID] || v.Name == "" || len(v.Name) > 100 || v.Weight <= 0 || math.IsNaN(v.Weight) || math.IsInf(v.Weight, 0) || (v.HardMax != nil && *v.HardMax < 0) {
			return 0, errors.New("INVALID_PRINCIPAL_CONFIGURATION")
		}
		seen[v.ImportID] = true
	}
	// Inputs contain only opaque references/configuration. No credentials in the key.
	op, _ := json.Marshal([]any{owner, operation})
	h := sha256.Sum256(op)
	creationKey := hex.EncodeToString(h[:])
	payload, _ := json.Marshal(in)
	digest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(digest[:])
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Serialize only identical admin operations. No network work while locked.
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, creationKey)
	if err != nil {
		return 0, err
	}
	var id int64
	var oldPayload string
	err = tx.QueryRowContext(ctx, `SELECT p.id,o.safe_payload->>'payload_hash' FROM upstream_principals p
 JOIN credential_audit_outbox o ON o.principal_id=p.id AND o.event_type='PRINCIPAL_CREATED' WHERE creation_key=$1`, creationKey).Scan(&id, &oldPayload)
	if err == nil {
		if oldPayload != payloadHash {
			return 0, service.ErrCredentialConflict
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// Imports are locked in stable order; all must validate before any account exists.
	entries := append([]service.CreateCredentialInstanceInput(nil), in.Instances...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].ImportID < entries[j].ImportID })
	type imported struct {
		input  service.CreateCredentialInstanceInput
		record service.CredentialImportRecord
	}
	imports := make([]imported, 0, len(entries))
	subject := ""
	for _, entry := range entries {
		var rec service.CredentialImportRecord
		var family, refresh sql.NullString
		var tokenExpiry sql.NullTime
		err = tx.QueryRowContext(ctx, `SELECT id,verification_state,COALESCE(verified_subject_key,''),secret_ciphertext,
 access_fingerprint,refresh_fingerprint,refresh_family,capabilities,token_expires_at
 FROM credential_imports WHERE id=$1 AND owner_id=$2 AND tenant_id=$3 AND expires_at>CURRENT_TIMESTAMP
 FOR UPDATE`, entry.ImportID, owner, service.DeploymentPrincipalScope).Scan(&rec.ID, &rec.State, &rec.SubjectKey, &rec.Ciphertext, &rec.AccessFingerprint, &refresh, &family, pq.Array(&rec.Capabilities), &tokenExpiry)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, service.ErrCredentialImportExpired
		}
		if err != nil {
			return 0, err
		}
		if rec.State != "VERIFIED" || rec.SubjectKey == "" {
			return 0, service.ErrCredentialUnverified
		}
		if !tokenExpiry.Valid || !tokenExpiry.Time.After(time.Now()) {
			return 0, service.ErrCredentialImportExpired
		}
		rec.TokenExpiresAt = tokenExpiry.Time
		if subject != "" && subject != rec.SubjectKey {
			return 0, service.ErrCredentialUnverified
		}
		subject = rec.SubjectKey
		rec.RefreshFingerprint = refresh.String
		rec.Family = family.String
		imports = append(imports, imported{entry, rec})
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO upstream_principals(name,provider,verified_subject_key,verification_state,requested_limit,creation_key)
 VALUES($1,'openai_oauth',$2,'VERIFIED',$3,$4) RETURNING id`, in.Name, subject, in.TotalConcurrency, creationKey).Scan(&id)
	if err != nil {
		return 0, credentialControlError(err)
	}
	for _, imp := range imports {
		if _, err = createCredentialInstance(ctx, tx, id, imp.input, imp.record); err != nil {
			return 0, err
		}
	}

	audit, _ := json.Marshal(map[string]string{"payload_hash": payloadHash})
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_audit_outbox(event_id,principal_id,actor_id,version,event_type,safe_payload)
 VALUES($1,$2,$3,1,'PRINCIPAL_CREATED',$4)`, uuid.NewString(), id, owner, string(audit))
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
func credentialControlError(err error) error {
	var p *pq.Error
	if errors.As(err, &p) && p.Code == "23505" {
		return service.ErrCredentialDuplicate
	}
	return err
}

func createCredentialInstance(ctx context.Context, tx *sql.Tx, principal int64, input service.CreateCredentialInstanceInput, record service.CredentialImportRecord) (int64, error) {
	var err error
	generation := uuid.NewString()
	installation := uuid.NewString()
	var accountID, instanceID int64
	// Empty credential carrier: no token is duplicated into legacy JSONB/cache.
	// It is unschedulable and has no group grants until controlled activation.
	err = tx.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,status,schedulable,concurrency)
 VALUES($1,'openai','oauth','{}','{}','inactive',false,0) RETURNING id`, input.Name).Scan(&accountID)
	if err != nil {
		return 0, err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,weight,hard_max,credential_state,capabilities)
 VALUES($1,$2,$3,$4,$5,$6,'VALID',$7) RETURNING id`, principal, accountID, input.Name, generation, input.Weight, input.HardMax, pq.Array(record.Capabilities)).Scan(&instanceID)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_identity_profiles(instance_id,principal_id,generation,installation_id,source)
 VALUES($1,$2,$3,$4,'LOCAL_LOGICAL')`, instanceID, principal, generation, installation)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_secrets(instance_id,credential_version,secret_ciphertext,secret_aad,expires_at,refresh_family,can_refresh)
 VALUES($1,1,$2,$3,$4,NULLIF($5,''),$6)`, instanceID, record.Ciphertext, record.ID, record.TokenExpiresAt, record.Family, record.RefreshFingerprint != "")
	if err != nil {
		return 0, err
	}
	for _, fp := range []struct{ kind, value string }{{"ACCESS", record.AccessFingerprint}, {"REFRESH", record.RefreshFingerprint}, {"FAMILY", record.Family}} {
		if fp.value == "" {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,$3)`, fp.value, instanceID, fp.kind)
		if err != nil {
			return 0, credentialControlError(err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_imports SET verification_state='CONSUMED',secret_ciphertext=''::bytea WHERE id=$1`, record.ID)
	if err != nil {
		return 0, err
	}
	return instanceID, nil
}
