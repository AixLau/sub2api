package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ShadowCredentialDecision is deliberately read-only: no lease, binding, token
// refresh or upstream request. It is intended for controlled migration review.
func (s *credentialMigrationStore) ShadowCredentialDecision(ctx context.Context, principal int64) ([]service.CredentialDemand, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,i.weight,i.occupied,
 LEAST(p.requested_limit,COALESCE(i.hard_max,p.requested_limit),COALESCE(i.health_capacity,p.requested_limit))
 FROM credential_instances i JOIN upstream_principals p ON p.id=i.principal_id
 WHERE p.id=$1 AND p.tenant_id=1 ORDER BY i.id`, principal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []service.CredentialDemand
	for rows.Next() {
		var d service.CredentialDemand
		if err = rows.Scan(&d.ID, &d.Weight, &d.Demand, &d.HardCapacity); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
