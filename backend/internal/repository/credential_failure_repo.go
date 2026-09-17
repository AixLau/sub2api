package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"time"
)

func (s *principalAdmissionStore) ObserveCredentialFailure(ctx context.Context, o service.CredentialFailureObservation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT config_version FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, o.PrincipalID).Scan(&version)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_limit_observations(id,principal_id,instance_id,generation,credential_version,origin,scope,code,http_status,reset_at,confidence)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid.NewString(), o.PrincipalID, o.InstanceID, o.Generation, o.CredentialVersion, o.Origin, o.Scope, o.Code, o.Status, nullableCredentialTime(o.RetryAt), o.Confidence)
	if err != nil {
		return err
	}
	if o.Status == 401 {
		_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET credential_state='NEEDS_REAUTH' WHERE id=$1 AND principal_id=$2 AND identity_generation=$3 AND credential_version=$4`, o.InstanceID, o.PrincipalID, o.Generation, o.CredentialVersion)
	} else if o.Status == 429 {
		_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET protected_until=GREATEST(protected_until,$2) WHERE id=$1`, o.PrincipalID, o.RetryAt)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// BlockKnownQuotaDomain is a control-plane operation for verified provider
// evidence only. It propagates protection to every linked principal in order.
func (s *principalAdmissionStore) BlockKnownQuotaDomain(ctx context.Context, domain string, until time.Time, requiresReset bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT p.id FROM upstream_principals p JOIN upstream_principal_quota_domains q ON q.principal_id=p.id WHERE q.quota_domain_id=$1 ORDER BY p.id FOR NO KEY UPDATE OF p`, domain)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE upstream_quota_domains SET blocked_until=GREATEST(blocked_until,$2),requires_admin_reset=requires_admin_reset OR $3 WHERE id=$1`, domain, until, requiresReset)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
