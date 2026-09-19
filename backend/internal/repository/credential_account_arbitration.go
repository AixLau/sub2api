package repository

import (
	"context"
	"slices"
	"sort"
	"time"

	"database/sql"
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func ProvideAccountRepository(client *dbent.Client, db *sql.DB, cache service.SchedulerCache, cfg *config.Config) (service.AccountRepository, error) {
	return configuredCredentialAccountRepository(client, db, cache, cfg)
}
func ProvideAdminAccountRepository(client *dbent.Client, db *sql.DB, cache service.SchedulerCache, cfg *config.Config) (service.AdminAccountRepository, error) {
	return configuredCredentialAccountRepository(client, db, cache, cfg)
}
func configuredCredentialAccountRepository(client *dbent.Client, db *sql.DB, cache service.SchedulerCache, cfg *config.Config) (*accountRepository, error) {
	vault, _ := service.NewCredentialVaultWithFingerprintKey(cfg.Gateway.CredentialVaultKey, cfg.Gateway.CredentialFingerprintKey)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := configureCredentialArbitration(ctx, db, vault); err != nil {
		return nil, service.ErrCredentialVaultUnavailable
	}
	r := newAccountRepositoryWithSQL(client, db, cache)
	r.credentialVault = vault
	r.credentialArbitration = true
	return r, nil
}
func (r *accountRepository) lockCredentialWrite(ctx context.Context, q sqlExecutor) error {
	if !r.credentialArbitration {
		return nil
	}
	return checkArbitrationKey(ctx, q, r.credentialVault)
}
func (r *accountRepository) checkCredentialWrite(ctx context.Context, q sqlExecutor, id int64, credentials map[string]any) error {
	if !r.credentialArbitration {
		return nil
	}
	fp := credentialTokenFingerprints(r.credentialVault, credentials)
	if r.credentialVault == nil {
		return nil
	} // only before registry initialization, checked under mutation lock
	if err := checkLegacyCredentialWrite(ctx, q, id, fp); err != nil {
		return err
	}
	return r.checkLegacyRefreshResultWrite(ctx, q, id, fp)
}
func (r *accountRepository) recordCredentialWrite(ctx context.Context, q sqlExecutor, id int64, credentials map[string]any) error {
	if !r.credentialArbitration || r.credentialVault == nil {
		return nil
	}
	return finishLegacyCredentialWrite(ctx, q, id, credentialTokenFingerprints(r.credentialVault, credentials))
}

// Bulk updates merge credentials in SQL. Inspect the same merged documents
// while holding the ownership barrier, before any account UPDATE row locks.
func (r *accountRepository) checkBulkCredentialWrite(ctx context.Context, q sqlExecutor, ids []int64, updates map[string]any) (map[int64]map[string]any, error) {
	if !r.credentialArbitration || len(updates) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(updates)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(credentials,'{}'::jsonb)||$2::jsonb FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id`, pq.Array(ids), string(raw))
	if err != nil {
		return nil, err
	}
	values := map[int64]map[string]any{}
	for rows.Next() {
		var id int64
		var data []byte
		if err = rows.Scan(&id, &data); err != nil {
			rows.Close()
			return nil, err
		}
		var v map[string]any
		if err = json.Unmarshal(data, &v); err != nil {
			rows.Close()
			return nil, err
		}
		values[id] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range sortedCredentialAccountIDs(values) {
		if err = r.checkCredentialWrite(ctx, q, id, values[id]); err != nil {
			return nil, err
		}
		if err = r.recordCredentialWrite(ctx, q, id, values[id]); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func sortedCredentialAccountIDs(values map[int64]map[string]any) []int64 {
	ids := make([]int64, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Recheck at the Account write, not only at OAuth completion: an administrator
// may replace credentials between durable Finish and the worker's later write.
func (r *accountRepository) checkLegacyRefreshResultWrite(ctx context.Context, q sqlExecutor, id int64, desired []string) error {
	if id == 0 {
		return nil
	}
	rows, err := q.QueryContext(ctx, `SELECT account_fingerprints FROM credential_legacy_refresh_operations o WHERE account_id=$1 AND state='SUCCEEDED' AND NOT transferred
 AND EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a WHERE a.operation_id=o.id AND a.is_output)
 AND NOT EXISTS(SELECT 1 FROM credential_legacy_refresh_aliases a WHERE a.operation_id=o.id AND a.is_output AND NOT(a.fingerprint=ANY($2)))`, id, pq.Array(desired))
	if err != nil {
		return err
	}
	var expected [][]string
	for rows.Next() {
		var fp []string
		if err = rows.Scan(pq.Array(&fp)); err != nil {
			rows.Close()
			return err
		}
		expected = append(expected, fp)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(expected) == 0 {
		return nil
	}
	rows, err = q.QueryContext(ctx, `SELECT credentials FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR NO KEY UPDATE`, id)
	if err != nil {
		return err
	}
	if !rows.Next() {
		rows.Close()
		return service.ErrAccountNotFound
	}
	var raw []byte
	err = rows.Scan(&raw)
	rows.Close()
	if err != nil {
		return err
	}
	var document map[string]any
	if json.Unmarshal(raw, &document) != nil {
		return service.ErrCredentialVaultUnavailable
	}
	current := credentialTokenFingerprints(r.credentialVault, document)
	if slices.Equal(current, desired) {
		return nil
	}
	for _, initial := range expected {
		if !slices.Equal(current, initial) {
			return service.ErrCredentialLegacyBypass
		}
	}
	return nil
}
