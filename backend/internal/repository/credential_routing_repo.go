package repository

import (
	"context"
	"database/sql"
	"errors"

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

func (s *credentialRouteStore) CheckCredentialRuntime(ctx context.Context, enabled bool) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM upstream_principals WHERE routing_mode='GROUPED'`).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 && !enabled {
		return errors.New("grouped principals require multi_credential_http_enabled; pause and drain before rollback")
	}
	return nil
}

func (s *credentialRouteStore) CredentialProbeRoute(ctx context.Context, account int64) (service.CredentialRouteCandidate, int64, error) {
	var route service.CredentialRouteCandidate
	var actor int64
	err := s.db.QueryRowContext(ctx, `SELECT p.id,i.id,i.account_id,COALESCE((SELECT actor_id FROM credential_audit_outbox WHERE principal_id=p.id AND event_type='PRINCIPAL_CREATED' ORDER BY created_at LIMIT 1),0)
 FROM credential_instances i JOIN upstream_principals p ON p.id=i.principal_id WHERE i.account_id=$1 AND p.routing_mode='GROUPED'`, account).Scan(&route.PrincipalID, &route.InstanceID, &route.AccountID, &actor)
	return route, actor, err
}
