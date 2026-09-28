package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamPrincipalReader struct{ db *sql.DB }

func NewUpstreamPrincipalReader(db *sql.DB) service.UpstreamPrincipalReader {
	return &upstreamPrincipalReader{db: db}
}

const principalViewColumns = `id, name, provider, verification_state, requested_limit,
occupied, config_version, admin_state, routing_mode, CURRENT_TIMESTAMP`

func scanPrincipalView(row interface{ Scan(...any) error }) (*service.PrincipalView, error) {
	p := &service.PrincipalView{Instances: []service.CredentialInstanceView{}}
	err := row.Scan(&p.ID, &p.Name, &p.Provider, &p.VerificationState, &p.RequestedLimit,
		&p.Occupied, &p.ConfigVersion, &p.AdminState, &p.RoutingMode, &p.ObservedAt)
	return p, err
}

func (r *upstreamPrincipalReader) ListPrincipals(ctx context.Context, scope, after int64, limit int) ([]service.PrincipalView, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+principalViewColumns+` FROM upstream_principals
        WHERE tenant_id=$1 AND id>$2 ORDER BY id LIMIT $3`, scope, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []service.PrincipalView{}
	for rows.Next() {
		p, err := scanPrincipalView(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, rows.Err()
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
	rows, err := tx.QueryContext(ctx, `SELECT i.id, i.account_id, i.name, i.identity_generation,
        i.credential_version, i.weight, i.hard_max, i.health_capacity, i.occupied,
        i.admin_state, i.credential_state, i.transport_state, COALESCE(p.source,'')
        FROM credential_instances i LEFT JOIN credential_identity_profiles p ON p.instance_id=i.id
        WHERE i.principal_id=$1 ORDER BY i.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v service.CredentialInstanceView
		if err := rows.Scan(&v.ID, &v.AccountID, &v.Name, &v.Generation, &v.CredentialVersion, &v.Weight,
			&v.HardMax, &v.HealthCapacity, &v.Occupied, &v.AdminState, &v.CredentialState,
			&v.TransportState, &v.IdentitySource); err != nil {
			rows.Close()
			return nil, err
		}
		p.Instances = append(p.Instances, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}
