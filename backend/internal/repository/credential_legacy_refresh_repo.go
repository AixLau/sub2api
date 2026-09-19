package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

func (r *accountRepository) BeginLegacyCredentialRefresh(ctx context.Context, input []string, requestHash string) (service.CredentialLegacyRefreshOperation, error) {
	var op service.CredentialLegacyRefreshOperation
	db, ok := r.sql.(*sql.DB)
	if !ok || r.credentialVault == nil {
		return op, service.ErrCredentialVaultUnavailable
	}
	input = sortedCredentialFingerprints(input)
	if len(input) == 0 || requestHash == "" {
		return op, service.ErrCredentialVaultUnavailable
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return op, err
	}
	defer tx.Rollback()
	if err = initializeCredentialArbitration(ctx, tx, r.credentialVault); err != nil {
		return op, err
	}
	if err = lockCredentialTokens(ctx, tx, input); err != nil {
		return op, err
	}
	blocked, err := hasForeignCredentialAliasClaims(ctx, tx, input, 0, "")
	if err != nil {
		return op, err
	}
	if blocked {
		return op, service.ErrCredentialLegacyBypass
	}
	blocked, err = credentialBool(ctx, tx, `SELECT EXISTS(SELECT 1 FROM credential_import_fingerprints f JOIN credential_imports i ON i.id=f.import_id WHERE f.fingerprint=ANY($1) AND i.verification_state<>'CONSUMED')`, pq.Array(input))
	if err != nil {
		return op, err
	}
	if blocked {
		return op, service.ErrCredentialLegacyBypass
	}
	var priorHash string
	var transferred bool
	err = tx.QueryRowContext(ctx, `SELECT id,owner_nonce,state,COALESCE(result_ciphertext,''::bytea),request_hash,transferred FROM credential_legacy_refresh_operations o WHERE EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a WHERE a.operation_id=o.id AND a.is_input AND a.fingerprint=ANY($1)) ORDER BY created_at LIMIT 1 FOR UPDATE`, pq.Array(input)).Scan(&op.ID, &op.OwnerNonce, &op.State, &op.Ciphertext, &priorHash, &transferred)
	if err == nil {
		if transferred || op.State != "SUCCEEDED" || priorHash != requestHash || len(op.Ciphertext) == 0 {
			return service.CredentialLegacyRefreshOperation{}, service.ErrCredentialLegacyBypass
		}
		return op, nil // durable result recovery; never another upstream attempt
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return op, err
	}
	if err = rejectPendingLegacyRefresh(ctx, tx, input); err != nil {
		return op, err
	}
	var owners int
	var account sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT account_id),min(account_id) FROM credential_legacy_account_claims WHERE fingerprint=ANY($1) AND NOT transferred`, pq.Array(input)).Scan(&owners, &account); err != nil {
		return op, err
	}
	if owners > 1 {
		return op, service.ErrCredentialLegacyBypass
	}

	var accountFingerprints []string
	if account.Valid {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR NO KEY UPDATE`, account.Int64).Scan(&raw); err != nil {
			return op, err
		}
		var document map[string]any
		if json.Unmarshal(raw, &document) != nil {
			return op, service.ErrCredentialVaultUnavailable
		}
		accountFingerprints = credentialTokenFingerprints(r.credentialVault, document)
		currentRefresh, _ := document["refresh_token"].(string)
		if currentRefresh == "" || !slices.Contains(input, r.credentialVault.Fingerprint("token", currentRefresh)) {
			return op, service.ErrCredentialLegacyBypass
		}
	}
	op.ID = uuid.NewString()
	op.OwnerNonce = uuid.NewString()
	op.State = "SENDING"
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_legacy_refresh_operations(id,input_fingerprint,request_hash,owner_nonce,account_id,account_fingerprints,state) VALUES($1,$2,$3,$4,$5,$6,'SENDING')`, op.ID, input[0], requestHash, op.OwnerNonce, account, pq.Array(accountFingerprints))
	if err != nil {
		return op, err
	}
	for _, fp := range input {
		if _, err = tx.ExecContext(ctx, `INSERT INTO credential_legacy_refresh_aliases(operation_id,fingerprint,is_input) VALUES($1,$2,true)`, op.ID, fp); err != nil {
			return op, err
		}
	}
	if err = tx.Commit(); err != nil {
		return service.CredentialLegacyRefreshOperation{}, service.ErrCredentialVaultUnavailable
	}
	return op, nil
}
func (r *accountRepository) FinishLegacyCredentialRefresh(ctx context.Context, op service.CredentialLegacyRefreshOperation, output []string, cipher []byte, success bool) error {
	db, ok := r.sql.(*sql.DB)
	if !ok || r.credentialVault == nil {
		return service.ErrCredentialVaultUnavailable
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = checkArbitrationKey(ctx, tx, r.credentialVault); err != nil {
		return err
	}
	if err = lockCredentialTokens(ctx, tx, output); err != nil {
		return err
	}
	var state string
	var account sql.NullInt64
	var initialFingerprints []string
	if err = tx.QueryRowContext(ctx, `SELECT state,account_id,account_fingerprints FROM credential_legacy_refresh_operations WHERE id=$1 AND owner_nonce=$2 FOR UPDATE`, op.ID, op.OwnerNonce).Scan(&state, &account, pq.Array(&initialFingerprints)); err != nil {
		return service.ErrAdmissionOwnership
	}
	if state == "SUCCEEDED" {
		return nil
	} // ACK-loss compensation cannot roll a durable success backward
	blocked, err := hasForeignCredentialAliasClaims(ctx, tx, output, 0, "")
	if err != nil {
		return err
	}
	other, err := credentialBool(ctx, tx, `SELECT EXISTS(SELECT 1 FROM credential_import_fingerprints f JOIN credential_imports i ON i.id=f.import_id WHERE f.fingerprint=ANY($1) AND i.verification_state<>'CONSUMED')
 OR EXISTS(SELECT 1 FROM credential_legacy_account_claims WHERE fingerprint=ANY($1) AND NOT transferred AND account_id<>$2)
 OR EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a JOIN credential_legacy_refresh_operations o ON o.id=a.operation_id WHERE a.fingerprint=ANY($1) AND o.id<>$3 AND (o.state<>'SUCCEEDED' OR COALESCE(o.account_id,0)<>$2))`, pq.Array(output), account.Int64, op.ID)
	if err != nil {
		return err
	}
	blocked = blocked || other

	var currentFingerprints []string
	if account.Valid {
		var raw []byte
		var deleted bool
		err = tx.QueryRowContext(ctx, `SELECT credentials,deleted_at IS NOT NULL FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, account.Int64).Scan(&raw, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			blocked = true
		} else if err != nil {
			return err
		} else {
			var document map[string]any
			if deleted || json.Unmarshal(raw, &document) != nil {
				blocked = true
			} else {
				currentFingerprints = credentialTokenFingerprints(r.credentialVault, document)
				if !slices.Equal(initialFingerprints, currentFingerprints) {
					blocked = true
				}
			}
		}
	}
	// A collision never discards a returned token. Store ciphertext plus an
	// independent operation alias, and keep UNKNOWN until explicitly resolved.
	for _, fp := range sortedCredentialFingerprints(output) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO credential_legacy_refresh_aliases(operation_id,fingerprint,is_output) VALUES($1,$2,true) ON CONFLICT(operation_id,fingerprint) DO UPDATE SET is_output=true`, op.ID, fp); err != nil {
			return err
		}
	}
	next := "UNKNOWN"
	if success && !blocked && len(cipher) > 0 {
		next = "SUCCEEDED"
	}

	if next == "SUCCEEDED" && account.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE credential_legacy_account_claims SET refresh_spent=true WHERE account_id=$1 AND fingerprint=ANY($2) AND NOT(fingerprint=ANY($3))`, account.Int64, pq.Array(currentFingerprints), pq.Array(output)); err != nil {
			return err
		}
	}

	_, err = tx.ExecContext(ctx, `UPDATE credential_legacy_refresh_operations SET state=$3,result_ciphertext=CASE WHEN octet_length($4::bytea)>0 THEN $4 ELSE result_ciphertext END,result_aad=CASE WHEN octet_length($4::bytea)>0 THEN id::text ELSE result_aad END,completed_at=CASE WHEN $3='SUCCEEDED' THEN clock_timestamp() ELSE NULL END WHERE id=$1 AND owner_nonce=$2`, op.ID, op.OwnerNonce, next, cipher)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return service.ErrCredentialVaultUnavailable
	}
	if blocked {
		return service.ErrCredentialLegacyBypass
	}
	return nil
}
