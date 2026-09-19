package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type credentialRefreshStore struct{ db *sql.DB }

func NewCredentialRefreshStore(db *sql.DB) service.CredentialRefreshStore {
	return &credentialRefreshStore{db: db}
}
func (s *credentialRefreshStore) BeginCredentialRefresh(ctx context.Context, instance int64) (service.CredentialRefreshOperation, error) {
	var op service.CredentialRefreshOperation
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return op, err
	}
	defer tx.Rollback()
	if _, err = lockCredentialArbitration(ctx, tx); err != nil {
		return op, err
	}
	err = tx.QueryRowContext(ctx, `SELECT principal_id FROM credential_instances WHERE id=$1`, instance).Scan(&op.PrincipalID)
	if err != nil {
		return op, err
	}
	var n int
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, op.PrincipalID).Scan(&n)
	if err != nil {
		return op, err
	}
	var state, admin string
	err = tx.QueryRowContext(ctx, `SELECT identity_generation,credential_version,credential_state,admin_state FROM credential_instances WHERE id=$1 FOR UPDATE`, instance).Scan(&op.Generation, &op.ExpectedVersion, &state, &admin)
	if err != nil {
		return op, err
	}
	if (state != "VALID" && state != "NEEDS_REAUTH") || (admin != "ACTIVE" && admin != "DRAINING" && admin != "PAUSED") {
		return op, errors.New("CREDENTIAL_REFRESH_UNAVAILABLE")
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(s.refresh_family,''),s.secret_aad,s.secret_ciphertext,a.proxy_id FROM credential_secrets s JOIN credential_instances i ON i.id=s.instance_id JOIN accounts a ON a.id=i.account_id WHERE s.instance_id=$1 AND s.credential_version=$2 AND s.can_refresh=true`, instance, op.ExpectedVersion).Scan(&op.Family, &op.SecretAAD, &op.Ciphertext, &op.ProxyID)
	if err != nil {
		return op, err
	}
	if op.Family == "" {
		return op, service.ErrCredentialUnverified
	} // unknown family must not get a new independent refresher
	op.ID = uuid.NewString()
	op.OwnerNonce = uuid.NewString()
	op.InstanceID = instance
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_refresh_ops(id,instance_id,generation,family_key,expected_version,owner_nonce,state) VALUES($1,$2,$3,$4,$5,$6,'SENDING')`, op.ID, instance, op.Generation, op.Family, op.ExpectedVersion, op.OwnerNonce)
	if err != nil {
		return op, err
	}
	// Input aliases remain blocked for a lost owner even when there is no
	// returned result to decrypt. The operation state, never age, ends the claim.
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_refresh_alias_claims(operation_id,fingerprint,kind)
 SELECT $1::uuid,fingerprint,kind FROM credential_fingerprints WHERE instance_id=$2 AND kind IN ('ACCESS','REFRESH')
 UNION SELECT $1::uuid,fingerprint,kind FROM credential_instance_alias_claims WHERE instance_id=$2
 ON CONFLICT DO NOTHING`, op.ID, instance)
	if err != nil {
		return op, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET credential_state='REFRESHING' WHERE id=$1`, instance)
	if err != nil {
		return op, err
	}
	return op, tx.Commit()
}
func (s *credentialRefreshStore) lockRefresh(ctx context.Context, op service.CredentialRefreshOperation) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*sql.Tx, error) { tx.Rollback(); return nil, err }
	if _, err = lockCredentialArbitration(ctx, tx); err != nil {
		return fail(err)
	}
	var n int
	err = tx.QueryRowContext(ctx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, op.PrincipalID).Scan(&n)
	if err != nil {
		return fail(err)
	}
	var version int64
	var generation string
	err = tx.QueryRowContext(ctx, `SELECT credential_version,identity_generation FROM credential_instances WHERE id=$1 AND principal_id=$2 FOR UPDATE`, op.InstanceID, op.PrincipalID).Scan(&version, &generation)
	if err != nil {
		return fail(err)
	}
	if version != op.ExpectedVersion || generation != op.Generation {
		return fail(service.ErrAdmissionOwnership)
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM credential_refresh_ops WHERE id=$1 AND owner_nonce=$2 AND expected_version=$3 AND generation=$4 AND instance_id=$5 FOR UPDATE`, op.ID, op.OwnerNonce, op.ExpectedVersion, op.Generation, op.InstanceID).Scan(&state)
	if err != nil {
		return fail(err)
	}
	if state != "SENDING" && state != "REFRESH_RESULT_UNKNOWN" {
		return fail(service.ErrAdmissionOwnership)
	}
	return tx, nil
}
func (s *credentialRefreshStore) CompleteCredentialRefresh(ctx context.Context, op service.CredentialRefreshOperation, result service.CredentialRefreshResult) error {
	tx, err := s.lockRefresh(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireNoLegacyCredentialOwner(ctx, tx, []string{result.AccessFingerprint, result.RefreshFingerprint}, 0); err != nil {
		return err
	}
	blocked, err := credentialBool(ctx, tx, `SELECT EXISTS(SELECT 1 FROM credential_import_fingerprints f JOIN credential_imports i ON i.id=f.import_id WHERE f.fingerprint=ANY($1) AND i.verification_state<>'CONSUMED')`, pq.Array([]string{result.AccessFingerprint, result.RefreshFingerprint}))
	if err != nil {
		return err
	}
	if blocked {
		return service.ErrCredentialDuplicate
	}

	aliases := credentialResultAliases(result)
	fingerprints := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		fingerprints = append(fingerprints, alias.fingerprint)
	}
	conflict, err := hasForeignCredentialAliasClaims(ctx, tx, fingerprints, op.InstanceID, op.ID)
	if err != nil {
		return err
	}
	if conflict {
		return service.ErrCredentialDuplicate
	}
	// Encrypted result and new version commit together. Never mutate profile/generation.
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_secrets(instance_id,credential_version,secret_ciphertext,secret_aad,expires_at,refresh_family,can_refresh)
 VALUES($1,$2,$3,$4,$5,$6,true)`, op.InstanceID, op.ExpectedVersion+1, result.Ciphertext, result.AAD, result.ExpiresAt, op.Family)
	if err != nil {
		return err
	}
	if err = putCredentialInstanceAliasClaims(ctx, tx, op.InstanceID, aliases); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET credential_version=credential_version+1,credential_state='VALID' WHERE id=$1`, op.InstanceID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_refresh_ops SET state='SUCCEEDED',completed_at=CURRENT_TIMESTAMP WHERE id=$1`, op.ID)
	if err != nil {
		return err
	}
	if err = admissionAudit(ctx, tx, service.LeaseRef{ID: op.ID, PrincipalID: op.PrincipalID, InstanceID: op.InstanceID, Epoch: op.ExpectedVersion + 1}, "CREDENTIAL_REFRESHED"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *credentialRefreshStore) MarkCredentialRefreshUnknown(ctx context.Context, op service.CredentialRefreshOperation, result *service.CredentialRefreshResult) error {
	tx, err := s.lockRefresh(ctx, op)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if result != nil {
		_, err = tx.ExecContext(ctx, `UPDATE credential_refresh_ops SET result_ciphertext=$2,result_aad=$3,result_expires_at=$4 WHERE id=$1`, op.ID, result.Ciphertext, result.AAD, result.ExpiresAt)
		if err != nil {
			return err
		}
		// Every unresolved result has its own claim. A historical owner may have
		// retired, or another operation may claim the same token; neither fact
		// permits dropping this operation's compensation or rewriting ownership.
		if err = putCredentialRefreshAliasClaims(ctx, tx, op.ID, credentialResultAliases(*result)); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_refresh_ops SET state='REFRESH_RESULT_UNKNOWN' WHERE id=$1`, op.ID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET credential_state='REFRESH_UNKNOWN' WHERE id=$1`, op.InstanceID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
