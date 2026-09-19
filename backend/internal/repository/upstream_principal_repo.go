package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/lib/pq"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamPrincipalReader struct{ db *sql.DB }

func NewUpstreamPrincipalReader(db *sql.DB) service.UpstreamPrincipalReader {
	return &upstreamPrincipalReader{db: db}
}

const principalViewColumns = `id, name, provider, verification_state, requested_limit,
occupied, config_version, admin_state, routing_mode, CURRENT_TIMESTAMP, COALESCE(management_account_id,0), archived_at,
 (SELECT COALESCE(array_agg(group_id ORDER BY group_id),'{}'::bigint[]) FROM account_groups WHERE account_id=management_account_id),
 (SELECT proxy_id FROM accounts WHERE id=management_account_id)`

func scanPrincipalView(row interface{ Scan(...any) error }) (*service.PrincipalView, error) {
	p := &service.PrincipalView{Instances: []service.CredentialInstanceView{}}
	err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.VerificationState, &p.RequestedLimit,
		&p.Occupied, &p.ConfigVersion, &p.AdminState, &p.RoutingMode, &p.ObservedAt, &p.AccountID, &p.ArchivedAt, pq.Array(&p.GroupIDs), &p.ProxyID)
	return p, err
}

func (r *upstreamPrincipalReader) ListPrincipals(ctx context.Context, scope, after int64, limit int) ([]service.PrincipalView, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	return r.readPrincipals(ctx, `tenant_id=$1 AND id>$2 AND archived_at IS NULL ORDER BY id LIMIT $3`, scope, after, limit)
}

func (r *upstreamPrincipalReader) AccountPrincipals(ctx context.Context, ids []int64) ([]service.PrincipalView, error) {
	return r.readPrincipals(ctx, `tenant_id=1 AND management_account_id=ANY($1) AND EXISTS(SELECT 1 FROM credential_instances i WHERE i.principal_id=upstream_principals.id AND NOT i.retired_to_legacy) ORDER BY id`, pq.Array(ids))
}

func (r *upstreamPrincipalReader) readPrincipals(ctx context.Context, predicate string, args ...any) ([]service.PrincipalView, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+principalViewColumns+` FROM upstream_principals WHERE `+predicate, args...)
	if err != nil {
		return nil, err
	}
	result := []service.PrincipalView{}
	for rows.Next() {
		p, err := scanPrincipalView(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, *p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// All summaries share one database snapshot, including retained unknown work.
	for i := range result {
		if err = readPrincipalInstances(ctx, tx, &result[i]); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *upstreamPrincipalReader) GetPrincipal(ctx context.Context, scope, id int64) (*service.PrincipalView, error) {
	// One MVCC snapshot: the counters and their instance breakdown must agree.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := scanPrincipalView(tx.QueryRowContext(ctx, `SELECT `+principalViewColumns+`
        FROM upstream_principals WHERE tenant_id=$1 AND id=$2`, scope, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = readPrincipalInstances(ctx, tx, p); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

func readPrincipalInstances(ctx context.Context, tx *sql.Tx, p *service.PrincipalView) error {
	rows, err := tx.QueryContext(ctx, `SELECT i.id, i.account_id, i.name, i.identity_generation,
        i.credential_version, i.weight, i.hard_max, i.health_capacity, i.occupied,
        i.admin_state, i.credential_state, i.transport_state, COALESCE(ip.source,''), i.archived_at, i.cooldown_until,
        (SELECT count(*) FROM request_leases l WHERE l.instance_id=i.id AND l.state='ORPHANED'),
        (SELECT count(*) FROM session_bindings b WHERE b.instance_id=i.id AND b.state IN ('ACTIVE','DRAINING') AND b.idle_expires_at>CURRENT_TIMESTAMP),
        (SELECT expires_at FROM credential_secrets s WHERE s.instance_id=i.id AND s.credential_version=i.credential_version)
        FROM credential_instances i LEFT JOIN credential_identity_profiles ip ON ip.instance_id=i.id
        WHERE i.principal_id=$1 ORDER BY i.id`, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v service.CredentialInstanceView
		if err = rows.Scan(&v.ID, &v.AccountID, &v.Name, &v.Generation, &v.CredentialVersion, &v.Weight,
			&v.HardMax, &v.HealthCapacity, &v.Occupied, &v.AdminState, &v.CredentialState,
			&v.TransportState, &v.IdentitySource, &v.ArchivedAt, &v.CooldownUntil, &v.UnknownOccupied, &v.ActiveBindings, &v.ExpiresAt); err != nil {
			return err
		}
		p.Instances = append(p.Instances, v)
	}
	return rows.Err()
}
