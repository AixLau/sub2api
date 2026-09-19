package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

var errCredentialVaultRotation = errors.New("CREDENTIAL_VAULT_ROTATION_REJECTED")

// CheckCredentialVaultKeys also runs when HTTP grouping is disabled. A fenced
// deployment must not restart an old writer after rotating its encryption key.
func (s *credentialRouteStore) CheckCredentialVaultKeys(ctx context.Context, encryptionID, fingerprintID string) error {
	return CheckCredentialVaultKeys(ctx, s.db, encryptionID, fingerprintID)
}
func CheckCredentialVaultKeys(ctx context.Context, db *sql.DB, encryptionID, fingerprintID string) error {
	var enc, fp string
	err := db.QueryRowContext(ctx, `SELECT encryption_key_id,fingerprint_key_id FROM credential_vault_state WHERE id=1`).Scan(&enc, &fp)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return service.ErrCredentialVaultUnavailable
	}
	if enc != encryptionID || fp != fingerprintID {
		return service.ErrCredentialVaultUnavailable
	}
	return nil
}

type CredentialVaultRotation struct {
	db    *sql.DB
	fence CredentialDeploymentFence
}

func NewCredentialVaultRotation(db *sql.DB, fence CredentialDeploymentFence) *CredentialVaultRotation {
	return &CredentialVaultRotation{db: db, fence: fence}
}

// Rotate is an offline, all-or-nothing re-encryption. It neither refreshes a
// token nor edits versions, identities, bindings, receipts or occupancy. The
// same operation ID resolves an uncertain commit without restoring snapshots.
func (r *CredentialVaultRotation) Rotate(ctx context.Context, actor int64, operation string, old, next *service.CredentialVault) (map[string]int64, error) {
	if old == nil || next == nil || r.fence == nil || actor <= 0 || old.EncryptionKeyID() == next.EncryptionKeyID() || old.FingerprintKeyID() != next.FingerprintKeyID() {
		return nil, errCredentialVaultRotation
	}
	if _, err := uuid.Parse(operation); err != nil {
		return nil, errCredentialVaultRotation
	}
	var admin bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, actor).Scan(&admin); err != nil || !admin {
		return nil, errCredentialVaultRotation
	}
	evidence, err := r.fence.Fence(ctx)
	if err != nil {
		return nil, errCredentialVaultRotation
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errCredentialVaultRotation
	}
	defer tx.Rollback()
	if _, err = lockCredentialArbitration(ctx, tx); err != nil {
		return nil, errCredentialVaultRotation
	}
	if err = checkArbitrationKey(ctx, tx, old); err != nil {
		return nil, errCredentialVaultRotation
	}
	// Offline lock order is parent metadata first, then child secrets. Table locks
	// exclude maintenance writers throughout the re-encryption transaction.
	if _, err = tx.ExecContext(ctx, `LOCK TABLE upstream_principals,credential_instances,credential_imports,credential_secrets,credential_refresh_ops,credential_migration_records,credential_vault_state,credential_legacy_refresh_operations IN EXCLUSIVE MODE`); err != nil {
		return nil, errCredentialVaultRotation
	}
	var enc, fp, prior string
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT encryption_key_id,fingerprint_key_id,operation_id,ciphertext_counts FROM credential_vault_state WHERE id=1`).Scan(&enc, &fp, &prior, &raw)
	if err == nil {
		if prior == operation && enc == next.EncryptionKeyID() && fp == next.FingerprintKeyID() {
			var counts map[string]int64
			if json.Unmarshal(raw, &counts) != nil {
				return nil, errCredentialVaultRotation
			}
			return counts, nil
		}
		if enc != old.EncryptionKeyID() || fp != old.FingerprintKeyID() {
			return nil, errCredentialVaultRotation
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, errCredentialVaultRotation
	}
	counts := map[string]int64{}
	for _, table := range credentialVaultCipherTables {
		n, err := rotateCredentialCipherTable(ctx, tx, table, old, next)
		if err != nil {
			return nil, errCredentialVaultRotation
		}
		counts[table.name] = n
	}
	if r.fence.Verify(ctx, evidence) != nil {
		return nil, errCredentialVaultRotation
	}
	raw, err = json.Marshal(counts)
	if err != nil {
		return nil, errCredentialVaultRotation
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_vault_state(id,encryption_key_id,fingerprint_key_id,operation_id,actor_id,ciphertext_counts) VALUES(1,$1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET encryption_key_id=EXCLUDED.encryption_key_id,fingerprint_key_id=EXCLUDED.fingerprint_key_id,operation_id=EXCLUDED.operation_id,actor_id=EXCLUDED.actor_id,ciphertext_counts=EXCLUDED.ciphertext_counts,rotated_at=CURRENT_TIMESTAMP`, next.EncryptionKeyID(), next.FingerprintKeyID(), operation, actor, string(raw))
	if err != nil {
		return nil, errCredentialVaultRotation
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_audit_outbox(event_id,actor_id,version,event_type,safe_payload) VALUES($1,$2,1,'CREDENTIAL_VAULT_ROTATED',$3)`, operation, actor, string(raw))
	if err != nil {
		return nil, errCredentialVaultRotation
	}
	if err = tx.Commit(); err != nil {
		return nil, errCredentialVaultRotation
	}
	return counts, nil
}

type credentialCipherTable struct{ name, key, aad, column string }

var credentialVaultCipherTables = []credentialCipherTable{
	{"credential_legacy_refresh_operations", "id::text", "result_aad", "result_ciphertext"},
	{"credential_imports", "id::text", "id::text", "secret_ciphertext"},
	{"credential_secrets", "instance_id::text||':'||credential_version::text", "secret_aad", "secret_ciphertext"},
	{"credential_refresh_ops", "id::text", "result_aad", "result_ciphertext"},
	// A merged principal has one migration snapshot per source account. The
	// account is the stable row key after the batch-history migration.
	{"credential_migration_records", "account_id::text", "credentials_aad", "credentials_ciphertext"},
}

func rotateCredentialCipherTable(ctx context.Context, tx *sql.Tx, table credentialCipherTable, old, next *service.CredentialVault) (int64, error) {
	// Identifiers are compile-time constants; no administrator input enters SQL.
	query := fmt.Sprintf(`SELECT %s,%s,%s FROM %s WHERE %s>$1 AND octet_length(%s)>0 ORDER BY %s LIMIT 128`, table.key, table.aad, table.column, table.name, table.key, table.column, table.key)
	update := fmt.Sprintf(`UPDATE %s SET %s=$1 WHERE %s=$2`, table.name, table.column, table.key)
	cursor := ""
	var count int64
	for {
		rows, err := tx.QueryContext(ctx, query, cursor)
		if err != nil {
			return 0, err
		}
		type item struct {
			key, aad string
			cipher   []byte
		}
		var batch []item
		for rows.Next() {
			var v item
			if err = rows.Scan(&v.key, &v.aad, &v.cipher); err != nil {
				rows.Close()
				return 0, err
			}
			batch = append(batch, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if len(batch) == 0 {
			return count, nil
		}
		for _, v := range batch {
			plain, err := old.OpenData(v.aad, v.cipher)
			if err != nil {
				return 0, err
			}
			sealed, err := next.SealData(v.aad, plain)
			clear(plain)
			if err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, update, sealed, v.key); err != nil {
				return 0, err
			}
			cursor = v.key
			count++
		}
	}
}
