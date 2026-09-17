package repository

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type credentialRouteStore struct{ db *sql.DB }

func NewCredentialRouteStore(db *sql.DB) service.CredentialRouteStore {
	return &credentialRouteStore{db: db}
}
func (s *credentialRouteStore) CredentialRoutes(ctx context.Context, group int64) ([]service.CredentialRouteCandidate, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id,i.id,i.account_id FROM upstream_principals p JOIN credential_instances i ON i.principal_id=p.id
 JOIN account_groups g ON g.account_id=i.account_id WHERE g.group_id=$1 AND p.routing_mode='GROUPED' ORDER BY p.id,i.id`, group)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var candidates []service.CredentialRouteCandidate
	for rows.Next() {
		var v service.CredentialRouteCandidate
		if err = rows.Scan(&v.PrincipalID, &v.InstanceID, &v.AccountID); err != nil {
			return nil, false, err
		}
		candidates = append(candidates, v)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	var mixed bool
	if len(candidates) > 0 {
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_groups g JOIN accounts a ON a.id=g.account_id WHERE g.group_id=$1 AND a.schedulable=true AND a.status='active' AND a.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM credential_instances i WHERE i.account_id=a.id))`, group).Scan(&mixed)
	}
	return candidates, mixed, err
}
func (s *credentialRouteStore) BoundCredentialPrincipal(ctx context.Context, scope, session string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT principal_id FROM session_bindings WHERE caller_scope_hash=$1 AND session_hash=$2 ORDER BY last_used_at DESC LIMIT 1`, scope, session).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
func (s *credentialRouteStore) IsControlledCredentialAccount(ctx context.Context, id int64) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credential_instances WHERE account_id=$1)`, id).Scan(&found)
	return found, err
}
