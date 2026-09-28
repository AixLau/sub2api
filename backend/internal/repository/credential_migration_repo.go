package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type credentialMigrationStore struct{ db *sql.DB }

func NewCredentialMigrationStore(db *sql.DB) service.CredentialMigrationStore {
	return &credentialMigrationStore{db: db}
}

// No JWT decoding or email matching can activate migration. Preview never writes
// credentials, seeds, permissions, budgets or occupancy.
func (s *credentialMigrationStore) PreviewCredentialMigration(ctx context.Context, ids []int64) ([]service.CredentialMigrationPreview, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, errors.New("INVALID_MIGRATION_BATCH")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,COALESCE(a.parent_account_id,a.id),
 CASE WHEN i.id IS NOT NULL THEN 'ALREADY_CONTROLLED' WHEN a.parent_account_id IS NOT NULL THEN 'SHADOW_ALIAS_REQUIRES_SOURCE_MAPPING' ELSE 'UNVERIFIED_REQUIRES_PROVIDER_EVIDENCE' END,
 COALESCE(a.extra->>'codex_fingerprint_seed','')<>'',COALESCE(a.extra->>'openai_device_id','')<>'',
 (SELECT count(*) FROM account_groups g WHERE g.account_id=a.id),a.concurrency
 FROM accounts a LEFT JOIN credential_instances i ON i.account_id=COALESCE(a.parent_account_id,a.id)
 WHERE a.id=ANY($1) AND a.deleted_at IS NULL AND a.platform='openai' AND a.type IN ('oauth','setup-token') ORDER BY a.id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []service.CredentialMigrationPreview{}
	for rows.Next() {
		v := service.CredentialMigrationPreview{RequiresExplicitTotal: true}
		if err = rows.Scan(&v.AccountID, &v.CredentialSourceID, &v.State, &v.HasLegacySeed, &v.HasInstallationOverride, &v.GroupCount, &v.ExistingConcurrency); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// Rollback enters a safe pause. OFF is allowed only after all uncertain work and
// bindings have been resolved. It never enables old independently counted rows.
func (s *credentialMigrationStore) PrepareCredentialRollback(ctx context.Context, actor, principal, version int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var occupied int
	err = tx.QueryRowContext(ctx, `SELECT config_version,occupied FROM upstream_principals WHERE id=$1 AND tenant_id=1 FOR NO KEY UPDATE`, principal).Scan(&current, &occupied)
	if err != nil {
		return err
	}
	if current != version {
		return errCredentialConfigConflict
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET admin_state='PAUSED',config_version=config_version+1 WHERE id=$1`, principal)
	if err != nil {
		return err
	}
	var active int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM session_bindings WHERE principal_id=$1 AND state IN ('ACTIVE','DRAINING'))+
 (SELECT count(*) FROM credential_refresh_ops o JOIN credential_instances i ON i.id=o.instance_id WHERE i.principal_id=$1 AND o.state IN ('SENDING','REFRESH_RESULT_UNKNOWN'))+
 (SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state<>'RELEASED')`, principal).Scan(&active)
	if err != nil {
		return err
	}
	event := "ROLLBACK_PAUSED_UNRESOLVED"
	if active == 0 && occupied == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET routing_mode='OFF' WHERE id=$1`, principal)
		if err != nil {
			return err
		}
		event = "ROLLBACK_READY_DISABLED"
	}
	if err = controlAudit(ctx, tx, actor, principal, 0, version+1, event, struct {
		Unresolved int `json:"unresolved"`
	}{active}); err != nil {
		return err
	}
	return tx.Commit()
}
