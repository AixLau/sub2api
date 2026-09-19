package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// This row is only used for credential ownership mutations. No admission,
// Heartbeat or Finish operation takes it. It precedes existing domain locks.
// A row, rather than SELECT FOR UPDATE on absent fingerprints, arbitrates first use.
func lockCredentialArbitration(ctx context.Context, q sqlExecutor) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(fingerprint_key_id,'') FROM credential_arbitration_state WHERE singleton=true FOR UPDATE`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", service.ErrCredentialVaultUnavailable
	}
	var key string
	err = rows.Scan(&key)
	return key, err
}
func credentialTokenFingerprints(vault *service.CredentialVault, credentials map[string]any) []string {
	if vault == nil {
		return nil
	}
	var values []string
	for _, key := range []string{"access_token", "refresh_token", "api_key", "auth_token"} {
		value, _ := credentials[key].(string)
		if strings.TrimSpace(value) == "" {
			continue
		}
		values = append(values, vault.Fingerprint("token", value))
		if trimmed := strings.TrimSpace(value); trimmed != value {
			values = append(values, vault.Fingerprint("token", trimmed))
		}
	}
	return sortedCredentialFingerprints(values)
}
func sortedCredentialFingerprints(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// Always insert/lock in sorted order, including the previously absent row case.
func lockCredentialTokens(ctx context.Context, q sqlExecutor, values []string) error {
	for _, fp := range sortedCredentialFingerprints(values) {
		if _, err := q.ExecContext(ctx, `INSERT INTO credential_token_registry(fingerprint) VALUES($1) ON CONFLICT DO NOTHING`, fp); err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `SELECT fingerprint FROM credential_token_registry WHERE fingerprint=$1 FOR UPDATE`, fp)
		if err != nil {
			return err
		}
		if !rows.Next() {
			rows.Close()
			return service.ErrCredentialVaultUnavailable
		}
		rows.Close()
	}
	return nil
}
func credentialBool(ctx context.Context, q sqlExecutor, query string, args ...any) (bool, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, service.ErrCredentialVaultUnavailable
	}
	var value bool
	err = rows.Scan(&value)
	return value, err
}

// Bootstrap only while holding the mutation row. Legacy writers with no key
// also acquire it; after initialization they fail closed without the same key.
// Existing credentials/profile/identity are never rewritten by indexing.
func initializeCredentialArbitration(ctx context.Context, q sqlExecutor, vault *service.CredentialVault) error {
	key, err := lockCredentialArbitration(ctx, q)
	if err != nil {
		return err
	}

	valid, err := credentialBool(ctx, q, `SELECT NOT EXISTS(SELECT 1 FROM credential_vault_state WHERE id=1 AND (encryption_key_id<>$1 OR fingerprint_key_id<>$2))`, vault.EncryptionKeyID(), vault.FingerprintKeyID())
	if err != nil {
		return err
	}
	if !valid {
		return service.ErrCredentialVaultUnavailable
	}
	if vault == nil {
		if key != "" {
			return service.ErrCredentialVaultUnavailable
		}

		controlled, err := credentialBool(ctx, q, `SELECT EXISTS(SELECT 1 FROM credential_instances WHERE NOT retired_to_legacy) OR EXISTS(SELECT 1 FROM credential_imports WHERE verification_state<>'CONSUMED') OR EXISTS(SELECT 1 FROM credential_legacy_refresh_operations)`)
		if err != nil {
			return err
		}
		if controlled {
			return service.ErrCredentialVaultUnavailable
		}
		return nil
	}
	if key != "" {
		if key != vault.FingerprintKeyID() {
			return service.ErrCredentialVaultUnavailable
		}
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id,credentials FROM accounts a WHERE deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM credential_instances i WHERE i.account_id=a.id AND NOT retired_to_legacy) ORDER BY id`)
	if err != nil {
		return err
	}
	type entry struct {
		id          int64
		credentials map[string]any
	}
	var entries []entry
	for rows.Next() {
		var v entry
		var raw []byte
		if err = rows.Scan(&v.id, &raw); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal(raw, &v.credentials) != nil {
			rows.Close()
			return service.ErrCredentialVaultUnavailable
		}
		entries = append(entries, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range entries {
		if err = recordLegacyClaims(ctx, q, v.id, credentialTokenFingerprints(vault, v.credentials)); err != nil {
			return err
		}
	}
	if err = backfillUnknownAliasClaims(ctx, q, vault); err != nil {
		return service.ErrCredentialVaultUnavailable
	}

	for _, v := range entries {
		conflict, err := hasForeignCredentialAliasClaims(ctx, q, credentialTokenFingerprints(vault, v.credentials), 0, "")
		if err != nil {
			return err
		}
		if conflict {
			return service.ErrCredentialLegacyBypass
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE credential_arbitration_state SET fingerprint_key_id=$1,initialized_at=clock_timestamp() WHERE singleton=true`, vault.FingerprintKeyID())
	return err
}
func recordLegacyClaims(ctx context.Context, q sqlExecutor, account int64, fp []string) error {
	if err := lockCredentialTokens(ctx, q, fp); err != nil {
		return err
	}
	for _, v := range sortedCredentialFingerprints(fp) {
		if _, err := q.ExecContext(ctx, `INSERT INTO credential_legacy_account_claims(account_id,fingerprint) VALUES($1,$2) ON CONFLICT(account_id,fingerprint) DO UPDATE SET transferred=false,refresh_spent=false`, account, v); err != nil {
			return err
		}
	}
	return nil
}

// Staging a verified import of an existing legacy account is required for the
// fenced migration flow. It grants no right to create another active carrier.
func requireNoLegacyCredentialOwner(ctx context.Context, q sqlExecutor, fp []string, allowedAccount int64) error {
	if err := lockCredentialTokens(ctx, q, fp); err != nil {
		return err
	}
	blocked, err := credentialBool(ctx, q, `SELECT EXISTS(SELECT 1 FROM credential_legacy_account_claims WHERE fingerprint=ANY($1) AND NOT transferred AND account_id<>$2)

 OR EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a JOIN credential_legacy_refresh_operations o ON o.id=a.operation_id WHERE a.fingerprint=ANY($1) AND NOT o.transferred AND (o.state<>'SUCCEEDED' OR o.account_id IS NULL OR o.account_id<>$2))`, pq.Array(fp), allowedAccount)
	if err != nil {
		return err
	}
	if blocked {
		return service.ErrCredentialLegacyBypass
	}
	return nil
}
func rejectPendingLegacyRefresh(ctx context.Context, q sqlExecutor, fp []string) error {
	blocked, err := credentialBool(ctx, q, `SELECT EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a JOIN credential_legacy_refresh_operations o ON o.id=a.operation_id WHERE a.fingerprint=ANY($1) AND o.state IN ('SENDING','UNKNOWN'))`, pq.Array(fp))
	if err != nil {
		return err
	}
	if blocked {
		return service.ErrCredentialLegacyBypass
	}
	return nil
}
func checkLegacyCredentialWrite(ctx context.Context, q sqlExecutor, account int64, fp []string) error {
	if err := lockCredentialTokens(ctx, q, fp); err != nil {
		return err
	}
	// Current controlled claims are separate from their historical owner.
	blocked, err := hasForeignCredentialAliasClaims(ctx, q, fp, 0, "")
	if err != nil {
		return err
	}
	if blocked {
		return service.ErrCredentialLegacyBypass
	}
	blocked, err = credentialBool(ctx, q, `SELECT EXISTS(SELECT 1 FROM credential_import_fingerprints f JOIN credential_imports i ON i.id=f.import_id WHERE f.fingerprint=ANY($1) AND i.verification_state<>'CONSUMED')
 OR EXISTS(SELECT 1 FROM credential_legacy_account_claims WHERE fingerprint=ANY($1) AND NOT transferred AND (account_id<>$2 OR refresh_spent))
 OR EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a JOIN credential_legacy_refresh_operations o ON o.id=a.operation_id WHERE a.fingerprint=ANY($1) AND a.is_input AND NOT a.is_output AND o.state='SUCCEEDED')
 OR EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a JOIN credential_legacy_refresh_operations o ON o.id=a.operation_id WHERE a.fingerprint=ANY($1) AND NOT o.transferred AND (o.state<>'SUCCEEDED' OR (o.account_id IS NOT NULL AND o.account_id<>$2)))`, pq.Array(fp), account)
	if err != nil {
		return err
	}
	if blocked {
		return service.ErrCredentialLegacyBypass
	}
	return nil
}
func finishLegacyCredentialWrite(ctx context.Context, q sqlExecutor, account int64, fp []string) error {
	if err := recordLegacyClaims(ctx, q, account, fp); err != nil {
		return err
	}
	// An unbound completed raw refresh may be adopted once by the first legacy
	// account write, in the same transaction. It can never authorize control use.
	_, err := q.ExecContext(ctx, `UPDATE credential_legacy_refresh_operations o SET account_id=$2 WHERE account_id IS NULL AND state='SUCCEEDED' AND EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a WHERE a.operation_id=o.id AND a.fingerprint=ANY($1))`, pq.Array(fp), account)
	return err
}

// Enable configured production writers before exposing any HTTP routes. This
// includes feature-off deployments; a nil key cannot open another digest space.
func configureCredentialArbitration(ctx context.Context, db *sql.DB, vault *service.CredentialVault) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = initializeCredentialArbitration(ctx, tx, vault); err != nil {
		return err
	}
	return tx.Commit()
}
func checkArbitrationKey(ctx context.Context, q sqlExecutor, vault *service.CredentialVault) error {
	key, err := lockCredentialArbitration(ctx, q)
	if err != nil {
		return err
	}
	if key != "" && (vault == nil || key != vault.FingerprintKeyID()) {
		return service.ErrCredentialVaultUnavailable
	}
	return nil
}

func legacyAccountClaimFingerprints(ctx context.Context, q sqlExecutor, account int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT fingerprint FROM credential_legacy_account_claims WHERE account_id=$1 ORDER BY fingerprint`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fp []string
	for rows.Next() {
		var f string
		if err = rows.Scan(&f); err != nil {
			return nil, err
		}
		fp = append(fp, f)
	}
	return fp, rows.Err()
}
