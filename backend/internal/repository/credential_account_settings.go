package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// UpdateCredentialAccountSettings saves the original account editor's fields
// under the same principal lock as instance operations. Credential carriers stay
// outside legacy scheduling; only account policy is copied between instances.
func (r *accountRepository) UpdateCredentialAccountSettings(ctx context.Context, account *service.Account, input *service.UpdateAccountInput) error {
	if account == nil || input == nil || input.CredentialEdit == nil {
		return service.ErrCredentialNotFound
	}
	edit := input.CredentialEdit
	if edit.ActorID <= 0 || edit.ConfigVersion <= 0 || edit.PrincipalID <= 0 {
		return service.ErrCredentialNotFound
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := tx.Client()
	var version int64
	var verified bool
	var routing string
	err = scanSingleRow(ctx, q, `SELECT config_version,verification_state='VERIFIED',routing_mode
 FROM upstream_principals p WHERE id=$1 AND tenant_id=1 AND management_account_id=$2 AND archived_at IS NULL
 AND EXISTS(SELECT 1 FROM credential_instances i WHERE i.principal_id=p.id AND NOT i.retired_to_legacy)
 FOR NO KEY UPDATE`, []any{edit.PrincipalID, account.ID}, &version, &verified, &routing)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrCredentialNotFound
	}
	if err != nil {
		return err
	}
	if version != edit.ConfigVersion {
		return errCredentialConfigConflict
	}
	if edit.Activate && !verified {
		return service.ErrCredentialUnverified
	}
	if _, err = q.ExecContext(ctx, `SET LOCAL sub2api.credential_control='on'`); err != nil {
		return err
	}
	var previousGroups []int64
	if err = scanSingleRow(ctx, q, `SELECT COALESCE(array_agg(group_id),'{}'::bigint[]) FROM account_groups WHERE account_id=$1`, []any{account.ID}, pq.Array(&previousGroups)); err != nil {
		return err
	}
	if input.GroupIDs != nil {
		if len(*input.GroupIDs) > 100 {
			return infraerrors.BadRequest("INVALID_CONTROL_CONFIGURATION", "Too many account groups")
		}
		if err = lockLiveGroups(ctx, q, *input.GroupIDs); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id IN (SELECT account_id FROM credential_instances WHERE principal_id=$1 AND NOT retired_to_legacy)`, edit.PrincipalID); err != nil {
			return err
		}
		if _, err = q.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id)
 SELECT i.account_id,g.id FROM credential_instances i CROSS JOIN groups g
 WHERE i.principal_id=$1 AND NOT i.retired_to_legacy AND g.id=ANY($2)`, edit.PrincipalID, pq.Array(*input.GroupIDs)); err != nil {
			return err
		}
	}
	if (input.GroupIDs != nil && routing == "GROUPED") || edit.Activate {
		var mixed bool
		if err = scanSingleRow(ctx, q, `SELECT EXISTS(
 SELECT 1 FROM account_groups own JOIN account_groups other ON other.group_id=own.group_id
 JOIN accounts a ON a.id=other.account_id WHERE own.account_id=$1 AND a.schedulable AND a.status='active' AND a.deleted_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM credential_instances i WHERE i.account_id=a.id AND NOT i.retired_to_legacy))`, []any{account.ID}, &mixed); err != nil {
			return err
		}
		if mixed {
			return errors.New("GROUPED_ROUTE_MIXED_UNSUPPORTED")
		}
	}
	credentials, err := credentialPolicyJSON(account.Credentials, service.CredentialAccountPolicyKeys)
	if err != nil {
		return err
	}
	extra, err := credentialPolicyJSON(account.Extra, service.CredentialAccountExtraPolicyKeys)
	if err != nil {
		return err
	}
	// Explicit columns exclude authentication, carrier status and concurrency.
	// JSON updates touch only allowed policy keys, preserving per-instance
	// identity, observations and asynchronously updated runtime state.
	_, err = q.ExecContext(ctx, `UPDATE accounts a SET
 name=CASE WHEN a.id=$2 THEN $3 ELSE a.name END,notes=$4,proxy_id=$5,priority=$6,
 rate_multiplier=COALESCE($7,a.rate_multiplier),load_factor=$8,expires_at=$9,auto_pause_on_expired=$10,
 credentials=CASE WHEN $11 THEN (COALESCE(a.credentials,'{}'::jsonb)-$12::text[])||$13::jsonb ELSE a.credentials END,
 extra=CASE WHEN $14 THEN (COALESCE(a.extra,'{}'::jsonb)-$15::text[])||$16::jsonb ELSE a.extra END,
 updated_at=CURRENT_TIMESTAMP
 WHERE a.deleted_at IS NULL AND a.id IN(SELECT account_id FROM credential_instances WHERE principal_id=$1 AND NOT retired_to_legacy)`,
		edit.PrincipalID, account.ID, account.Name, account.Notes, account.ProxyID, account.Priority,
		input.RateMultiplier, account.LoadFactor, account.ExpiresAt, account.AutoPauseOnExpired,
		input.Credentials != nil, pq.Array(service.CredentialAccountPolicyKeys), credentials,
		input.Extra != nil, pq.Array(service.CredentialAccountExtraPolicyKeys), extra)
	if err != nil {
		return err
	}
	state := ""
	if input.Status != "" {
		state = "PAUSED"
		if input.Status == service.StatusActive {
			state = "ACTIVE"
		}
	}
	_, err = q.ExecContext(ctx, `UPDATE upstream_principals SET name=$2,requested_limit=COALESCE($3,requested_limit),
 admin_state=COALESCE(NULLIF($4,''),admin_state),routing_mode=CASE WHEN $5 THEN 'GROUPED' ELSE routing_mode END,
 config_version=config_version+1,updated_at=CURRENT_TIMESTAMP WHERE id=$1`,
		edit.PrincipalID, account.Name, input.Concurrency, state, edit.Activate)
	if err != nil {
		return err
	}
	if state == "ACTIVE" {
		// Only undo a principal drain; individually paused instances stay paused.
		if _, err = q.ExecContext(ctx, `UPDATE credential_instances i SET admin_state='ACTIVE'
 WHERE principal_id=$1 AND admin_state='DRAINING' AND archived_at IS NULL AND NOT retired_to_legacy AND credential_state='VALID'
 AND EXISTS(SELECT 1 FROM credential_secrets s WHERE s.instance_id=i.id AND s.credential_version=i.credential_version AND s.expires_at>CURRENT_TIMESTAMP)`, edit.PrincipalID); err != nil {
			return err
		}
	}
	groups := previousGroups
	if input.GroupIDs != nil {
		groups = mergeGroupIDs(groups, *input.GroupIDs)
	}
	var ids []int64
	if err = scanSingleRow(ctx, q, `SELECT COALESCE(array_agg(account_id),'{}'::bigint[]) FROM credential_instances WHERE principal_id=$1 AND NOT retired_to_legacy`, []any{edit.PrincipalID}, pq.Array(&ids)); err != nil {
		return err
	}
	for _, id := range ids {
		if err = enqueueSchedulerOutbox(ctx, q, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(groups)); err != nil {
			return err
		}
	}
	if err = controlAudit(ctx, q, edit.ActorID, edit.PrincipalID, 0, version+1, "ACCOUNT_SETTINGS_UPDATED", map[string]any{"account_id": account.ID}); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	for _, id := range ids {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

func credentialPolicyJSON(values map[string]any, keys []string) (string, error) {
	policy := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := values[key]; ok {
			policy[key] = value
		}
	}
	data, err := json.Marshal(policy)
	return string(data), err
}

func inheritCredentialAccountSettings(ctx context.Context, q sqlExecutor, principal, instance int64) error {
	_, err := q.ExecContext(ctx, `UPDATE accounts a SET notes=m.notes,proxy_id=m.proxy_id,priority=m.priority,
 rate_multiplier=m.rate_multiplier,load_factor=m.load_factor,expires_at=m.expires_at,auto_pause_on_expired=m.auto_pause_on_expired,
 credentials=COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(m.credentials) WHERE key=ANY($3)),'{}'::jsonb),
 extra=COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(m.extra) WHERE key=ANY($4)),'{}'::jsonb)
 FROM upstream_principals p JOIN accounts m ON m.id=p.management_account_id
 WHERE p.id=$1 AND a.id=(SELECT account_id FROM credential_instances WHERE id=$2 AND principal_id=$1)`,
		principal, instance, pq.Array(service.CredentialAccountPolicyKeys), pq.Array(service.CredentialAccountExtraPolicyKeys))
	return err
}
