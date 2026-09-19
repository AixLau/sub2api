package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CredentialDeploymentFence interface {
	Fence(context.Context) (credentialfence.Evidence, error)
	Verify(context.Context, credentialfence.Evidence) error
}
type CredentialRollout struct {
	db    *sql.DB
	vault *service.CredentialVault
	fence CredentialDeploymentFence
}

func NewCredentialRollout(db *sql.DB, vault *service.CredentialVault, fence CredentialDeploymentFence) *CredentialRollout {
	return &CredentialRollout{db: db, vault: vault, fence: fence}
}

type CredentialMigrationInput struct {
	AccountID, ActorID                   int64
	ImportID, OperationID, DrainEvidence string
	RequestedLimit                       int
}

func (r *CredentialRollout) Migrate(ctx context.Context, in CredentialMigrationInput) (int64, error) {
	if r.vault == nil || r.fence == nil || in.AccountID <= 0 || in.ActorID <= 0 || in.RequestedLimit < 0 || len(in.DrainEvidence) < 16 {
		return 0, errors.New("MIGRATION_PRECONDITION_REQUIRED")
	}
	if err := r.requireAdmin(ctx, in.ActorID); err != nil {
		return 0, err
	}
	if _, err := uuid.Parse(in.OperationID); err != nil {
		return 0, err
	}
	if _, err := uuid.Parse(in.ImportID); err != nil {
		return 0, err
	}
	var prior int64
	if err := r.db.QueryRowContext(ctx, `SELECT principal_id FROM credential_migration_records WHERE operation_id=$1 AND actor_id=$2 AND account_id=$3`, in.OperationID, in.ActorID, in.AccountID).Scan(&prior); err == nil {
		return prior, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	// Check verified provider evidence BEFORE stopping any gateways. There is no
	// caller-supplied verified=true option or production mock verifier.
	var verified bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM credential_imports WHERE id=$1 AND owner_id=$2 AND verification_state='VERIFIED' AND expires_at>CURRENT_TIMESTAMP)`, in.ImportID, in.ActorID).Scan(&verified)
	if err != nil {
		return 0, err
	}
	if !verified {
		return 0, service.ErrCredentialUnverified
	}
	fence, err := r.fence.Fence(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err = initializeCredentialArbitration(ctx, tx, r.vault); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(9182026)`)
	if err != nil {
		return 0, err
	}
	var isAdmin bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, in.ActorID).Scan(&isAdmin)
	if err != nil {
		return 0, err
	}
	if !isAdmin {
		return 0, errors.New("ADMIN_REQUIRED")
	}
	var principal int64
	err = tx.QueryRowContext(ctx, `SELECT principal_id FROM credential_migration_records WHERE operation_id=$1 AND actor_id=$2 AND account_id=$3`, in.OperationID, in.ActorID, in.AccountID).Scan(&principal)
	if err == nil {
		return principal, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var credentials, extra []byte
	var accountName, status, platform, kind string
	var schedulable bool
	var concurrency int
	var parent sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT name,credentials,extra,status,schedulable,concurrency,platform,type,parent_account_id FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, in.AccountID).Scan(&accountName, &credentials, &extra, &status, &schedulable, &concurrency, &platform, &kind, &parent)
	if err != nil {
		return 0, err
	}
	if concurrency < 0 {
		return 0, errors.New("INVALID_LEGACY_ACCOUNT")
	}
	// Shadows refer to one credential source; migrating the source with existing
	// aliases needs explicit alias mapping, so default remains blocked.
	var shadows int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE parent_account_id=$1 AND deleted_at IS NULL`, in.AccountID).Scan(&shadows)
	if err != nil {
		return 0, err
	}
	if parent.Valid || shadows > 0 {
		return 0, errors.New("SHADOW_ALIAS_MIGRATION_REQUIRES_MAPPING")
	}
	account := &service.Account{ID: in.AccountID, Platform: platform, Type: kind}
	if json.Unmarshal(credentials, &account.Credentials) != nil || json.Unmarshal(extra, &account.Extra) != nil {
		return 0, errors.New("INVALID_LEGACY_ACCOUNT")
	}
	installation, seed, source, err := service.ResolveCredentialMigrationProfile(account)
	if err != nil {
		return 0, err
	}
	var subject, accessFingerprint, refreshFingerprint, family string
	var cipher []byte
	var capabilities []string
	var expiry time.Time
	err = tx.QueryRowContext(ctx, `SELECT verified_subject_key,access_fingerprint,COALESCE(refresh_fingerprint,''),COALESCE(refresh_family,''),secret_ciphertext,capabilities,token_expires_at FROM credential_imports WHERE id=$1 AND owner_id=$2 AND verification_state='VERIFIED' AND expires_at>CURRENT_TIMESTAMP AND token_expires_at>CURRENT_TIMESTAMP FOR UPDATE`, in.ImportID, in.ActorID).Scan(&subject, &accessFingerprint, &refreshFingerprint, &family, &cipher, pq.Array(&capabilities), &expiry)
	if err != nil {
		return 0, service.ErrCredentialUnverified
	}
	if r.vault.Fingerprint("token", account.GetCredential("access_token")) != accessFingerprint {
		return 0, errors.New("MIGRATION_CREDENTIAL_MISMATCH")
	}
	if refreshFingerprint != "" && r.vault.Fingerprint("token", account.GetCredential("refresh_token")) != refreshFingerprint {
		return 0, errors.New("MIGRATION_CREDENTIAL_MISMATCH")
	}
	if err = requireNoLegacyCredentialOwner(ctx, tx, []string{accessFingerprint, refreshFingerprint}, in.AccountID); err != nil {
		return 0, err
	}

	pending, err := credentialBool(ctx, tx, `SELECT EXISTS(SELECT 1 FROM credential_legacy_refresh_operations WHERE account_id=$1 AND state IN ('SENDING','UNKNOWN')) OR EXISTS(SELECT 1 FROM credential_legacy_account_claims WHERE account_id=$1 AND refresh_spent AND fingerprint=ANY($2))`, in.AccountID, pq.Array([]string{accessFingerprint, refreshFingerprint}))
	if err != nil {
		return 0, err
	}
	if pending {
		return 0, service.ErrCredentialLegacyBypass
	}

	historical, err := legacyAccountClaimFingerprints(ctx, tx, in.AccountID)
	if err != nil {
		return 0, err
	}
	if err = requireNoLegacyCredentialOwner(ctx, tx, historical, in.AccountID); err != nil {
		return 0, err
	}
	conflicts, err := hasForeignCredentialAliasClaims(ctx, tx, historical, 0, "")
	if err != nil {
		return 0, err
	}
	if conflicts {
		return 0, service.ErrCredentialLegacyBypass
	}
	aad := "credential-migration:" + in.OperationID
	archived, err := r.vault.SealData(aad, credentials)
	if err != nil {
		return 0, err
	}
	// A provider subject is the merge key. An already controlled principal keeps
	// its management account (and therefore its capacity, proxy, priority and
	// groups); this legacy source is attached as another logical instance.
	var existingManagement sql.NullInt64
	var principalVersion int64
	var principalState, principalRouting string
	err = tx.QueryRowContext(ctx, `SELECT id,management_account_id,config_version,admin_state,routing_mode FROM upstream_principals WHERE tenant_id=1 AND provider='openai_oauth' AND verified_subject_key=$1 FOR UPDATE`, subject).Scan(&principal, &existingManagement, &principalVersion, &principalState, &principalRouting)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_principals(name,provider,verified_subject_key,verification_state,requested_limit,admin_state,routing_mode) VALUES($1,'openai_oauth',$2,'VERIFIED',$3,'PAUSED','SHADOW') RETURNING id`, accountName, subject, in.RequestedLimit).Scan(&principal)
		principalVersion = 1
		principalState, principalRouting = "PAUSED", "SHADOW"
	}
	if err != nil {
		return 0, err
	}
	if existingManagement.Valid {
		// Preserve the primary account's policy on an attached source. The source
		// row itself remains named as imported so operators can audit its origin.
		var proxyID sql.NullInt64
		var priority int
		var rateMultiplier float64
		var primaryExtra []byte
		if err = tx.QueryRowContext(ctx, `SELECT a.proxy_id,a.priority,COALESCE(a.rate_multiplier,1),a.extra FROM accounts a WHERE a.id=$1 FOR UPDATE`, existingManagement.Int64).Scan(&proxyID, &priority, &rateMultiplier, &primaryExtra); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE accounts SET proxy_id=$2,priority=$3,rate_multiplier=$4,extra=$5 WHERE id=$1`, in.AccountID, nullableInt64(proxyID), priority, rateMultiplier, string(primaryExtra)); err != nil {
			return 0, err
		}
	}
	generation := uuid.NewString()
	instanceState := "PAUSED"
	if existingManagement.Valid && principalState == "ACTIVE" && principalRouting == "GROUPED" {
		instanceState = "ACTIVE"
	}
	var instance int64
	err = tx.QueryRowContext(ctx, `INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,hard_max,admin_state,credential_state,capabilities) VALUES($1,$2,$3,$4,$5,$6,'VALID',$7) RETURNING id`, principal, in.AccountID, accountName, generation, concurrency, instanceState, pq.Array(capabilities)).Scan(&instance)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_identity_profiles(instance_id,principal_id,generation,installation_id,legacy_seed_ref,source) VALUES($1,$2,$3,$4,$5,$6)`, instance, principal, generation, installation, nullableString(seed), source)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_secrets(instance_id,credential_version,secret_ciphertext,secret_aad,expires_at,refresh_family,can_refresh) VALUES($1,1,$2,$3,$4,NULLIF($5,''),$6)`, instance, cipher, in.ImportID, expiry, family, refreshFingerprint != "")
	if err != nil {
		return 0, err
	}
	for _, fp := range []struct{ kind, value string }{{"ACCESS", accessFingerprint}, {"REFRESH", refreshFingerprint}, {"FAMILY", family}} {
		if fp.value != "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,$3)`, fp.value, instance, fp.kind); err != nil {
				return 0, err
			}
		}
	}

	var aliases []credentialTokenAlias
	for _, fp := range historical {
		aliases = append(aliases, credentialTokenAlias{fingerprint: fp, kind: "ACCESS"})
	}
	if err = putCredentialInstanceAliasClaims(ctx, tx, instance, aliases); err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `SET LOCAL sub2api.credential_control='on'`)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=credentials-ARRAY['access_token','refresh_token','id_token'],extra=extra-'multi_credential_migration',schedulable=false,status='inactive' WHERE id=$1`, in.AccountID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE credential_legacy_account_claims SET transferred=true WHERE account_id=$1`, in.AccountID); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE credential_legacy_refresh_operations SET transferred=true WHERE account_id=$1 AND state='SUCCEEDED'`, in.AccountID); err != nil {
		return 0, err
	}
	evidence, _ := json.Marshal(fence)
	_, err = tx.ExecContext(ctx, `INSERT INTO credential_migration_records(principal_id,account_id,actor_id,credentials_ciphertext,credentials_aad,original_status,original_schedulable,original_concurrency,fence_evidence,drain_evidence,operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, principal, in.AccountID, in.ActorID, archived, aad, status, schedulable, concurrency, string(evidence), in.DrainEvidence, in.OperationID)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_imports SET verification_state='CONSUMED',secret_ciphertext=''::bytea WHERE id=$1`, in.ImportID)
	if err != nil {
		return 0, err
	}
	if err = controlAudit(ctx, tx, in.ActorID, principal, instance, principalVersion, "LEGACY_ACCOUNT_MIGRATED", map[string]any{"account_id": in.AccountID, "operation_id": in.OperationID}); err != nil {
		return 0, err
	}
	// No network I/O in the admission transaction. This is an offline control
	// transaction, all application nodes already fenced; final inventory check.
	if err = r.fence.Verify(ctx, fence); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return principal, nil
}

// ActivateMigratedBatch restores the scheduling state of migrated management
// accounts that were active before the cutover. It runs while the same
// deployment fence is held by the caller, so the new gateway can start with a
// complete principal set instead of exposing accounts one at a time.
func (r *CredentialRollout) ActivateMigratedBatch(ctx context.Context, actor int64, accountIDs []int64) ([]int64, error) {
	if r.vault == nil || r.fence == nil || actor <= 0 || len(accountIDs) == 0 {
		return nil, errors.New("MIGRATION_PRECONDITION_REQUIRED")
	}
	if err := r.requireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	fence, err := r.fence.Fence(ctx)
	if err != nil {
		return nil, err
	}
	if err = r.fence.Verify(ctx, fence); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(9182026)`); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT p.id,p.admin_state
FROM upstream_principals p
JOIN credential_instances i ON i.principal_id=p.id AND NOT i.retired_to_legacy
JOIN credential_migration_records m ON m.account_id=i.account_id
WHERE i.account_id=ANY($1) AND p.verification_state='VERIFIED'
  AND p.routing_mode IN ('SHADOW','GROUPED')
  AND p.admin_state IN ('PAUSED','ACTIVE')
	  AND EXISTS (SELECT 1 FROM credential_migration_records primary_m
	              WHERE primary_m.account_id=p.management_account_id
	                AND primary_m.state<>'ROLLED_BACK'
	                AND primary_m.original_status='active'
                AND primary_m.original_schedulable)`, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	type activationPrincipal struct {
		id    int64
		state string
	}
	var principals []activationPrincipal
	for rows.Next() {
		var principal activationPrincipal
		if err = rows.Scan(&principal.id, &principal.state); err != nil {
			rows.Close()
			return nil, err
		}
		principals = append(principals, principal)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	activated := make([]int64, 0, len(principals))
	for _, principal := range principals {
		var occupied int
		var version int64
		if err = tx.QueryRowContext(ctx, `SELECT occupied,config_version FROM upstream_principals WHERE id=$1 FOR UPDATE`, principal.id).Scan(&occupied, &version); err != nil {
			return nil, err
		}
		if occupied != 0 {
			return nil, errors.New("UNRESOLVED_EXECUTION")
		}
		if principal.state == "PAUSED" {
			if _, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET routing_mode='GROUPED',admin_state='ACTIVE',config_version=config_version+1,admission_epoch=admission_epoch+1 WHERE id=$1`, principal.id); err != nil {
				return nil, err
			}
			version++
		}
		if _, err = tx.ExecContext(ctx, `UPDATE credential_instances SET admin_state='ACTIVE' WHERE principal_id=$1 AND admin_state='PAUSED' AND credential_state='VALID' AND NOT retired_to_legacy`, principal.id); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE credential_migration_records SET state='CANARY',fence_evidence=$2 WHERE principal_id=$1 AND state='MIGRATED'`, principal.id, mustFenceJSON(fence)); err != nil {
			return nil, err
		}
		if err = controlAudit(ctx, tx, actor, principal.id, 0, version, "PRINCIPAL_BATCH_ACTIVATED", map[string]any{"fence": fence}); err != nil {
			return nil, err
		}
		activated = append(activated, principal.id)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return activated, nil
}

// Canary activates only the requested verified principal, with every old
// gateway already fenced. Operators start the new capable binaries afterwards.
func (r *CredentialRollout) Canary(ctx context.Context, actor, principal, version int64) error {
	if err := r.requireAdmin(ctx, actor); err != nil {
		return err
	}
	var verifiedIdentity bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_principals WHERE id=$1 AND verification_state='VERIFIED' AND config_version=$2)`, principal, version).Scan(&verifiedIdentity); err != nil {
		return err
	}
	if !verifiedIdentity {
		return service.ErrCredentialUnverified
	}
	if r.fence == nil {
		return errors.New("FENCE_REQUIRED")
	}
	fence, err := r.fence.Fence(ctx)
	if err != nil {
		return err
	}
	if err = r.fence.Verify(ctx, fence); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var verified, mode string
	var occupied int
	err = tx.QueryRowContext(ctx, `SELECT config_version,verification_state,routing_mode,occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, principal).Scan(&current, &verified, &mode, &occupied)
	if err != nil {
		return err
	}
	if current != version {
		return errCredentialConfigConflict
	}
	if verified != "VERIFIED" {
		return service.ErrCredentialUnverified
	}
	if occupied != 0 {
		return errors.New("UNRESOLVED_EXECUTION")
	}
	var authorized bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, actor).Scan(&authorized)
	if err != nil {
		return err
	}
	if !authorized {
		return errors.New("ADMIN_REQUIRED")
	}
	var unavailable int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_instances i WHERE principal_id=$1 AND (i.credential_state<>'VALID' OR i.retired_to_legacy OR NOT ('responses'=ANY(i.capabilities)))`, principal).Scan(&unavailable)
	if err != nil {
		return err
	}
	if unavailable > 0 {
		return service.ErrCredentialUnverified
	}
	// No activation for unrelated principals or unverified compact capability.
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET routing_mode='GROUPED',admin_state='ACTIVE',config_version=config_version+1,admission_epoch=admission_epoch+1 WHERE id=$1`, principal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET admin_state='ACTIVE' WHERE principal_id=$1 AND admin_state='PAUSED'`, principal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_migration_records SET state='CANARY',fence_evidence=$2 WHERE principal_id=$1`, principal, mustFenceJSON(fence))
	if err != nil {
		return err
	}
	if err = controlAudit(ctx, tx, actor, principal, 0, current+1, "PRINCIPAL_CANARY_ENABLED", map[string]any{"mode_before": mode, "fence": fence}); err != nil {
		return err
	}
	return tx.Commit()
}
func mustFenceJSON(e credentialfence.Evidence) string {
	data, _ := json.Marshal(e)
	return string(data)
}

// Rollback restores one original Account only after all attempts, queued work,
// refresh operations and billing facts are resolved. It preserves the latest
// rotated token, archives bindings, and leaves other instances disabled.
func (r *CredentialRollout) Rollback(ctx context.Context, actor, principal, version int64) error {
	if err := r.requireAdmin(ctx, actor); err != nil {
		return err
	}
	if r.fence == nil || r.vault == nil {
		return errors.New("FENCE_REQUIRED")
	}
	// Persist pause before the fence: failure at any subsequent step stays closed.
	ops := &credentialOperations{db: r.db}
	paused, err := ops.UpdatePrincipal(ctx, actor, principal, version, service.PrincipalControlUpdate{AdminState: "PAUSED"})
	if err != nil {
		return err
	}
	fence, err := r.fence.Fence(ctx)
	if err != nil {
		return err
	}
	if err = r.fence.Verify(ctx, fence); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = initializeCredentialArbitration(ctx, tx, r.vault); err != nil {
		return err
	}
	var current int64
	var occupied int
	err = tx.QueryRowContext(ctx, `SELECT config_version,occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE`, principal).Scan(&current, &occupied)
	if err != nil {
		return err
	}
	if current != paused.ConfigVersion {
		return errCredentialConfigConflict
	}
	var unresolved int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state<>'RELEASED')+
 (SELECT count(*) FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED')+
 (SELECT count(*) FROM credential_refresh_ops o JOIN credential_instances i ON i.id=o.instance_id WHERE i.principal_id=$1 AND o.state IN ('SENDING','REFRESH_RESULT_UNKNOWN'))+
 (SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON l.id=c.lease_id WHERE l.principal_id=$1 AND c.state<>'RECORDED')+
 (SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON l.id=b.lease_id WHERE l.principal_id=$1 AND b.settled_at IS NULL)`, principal).Scan(&unresolved)
	if err != nil {
		return err
	}
	if occupied != 0 || unresolved != 0 {
		return errors.New("ROLLBACK_PAUSED_UNRESOLVED")
	}
	var account, instance int64
	var status, aad string
	var schedulable bool
	var concurrency int
	var archived []byte
	var migrationCount int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_migration_records WHERE principal_id=$1 AND state<>'ROLLED_BACK'`, principal).Scan(&migrationCount); err != nil {
		return err
	}
	if migrationCount != 1 {
		return errors.New("ROLLBACK_REQUIRES_SINGLE_SOURCE")
	}
	err = tx.QueryRowContext(ctx, `SELECT m.account_id,i.id,m.original_status,m.original_schedulable,m.original_concurrency,m.credentials_ciphertext,m.credentials_aad FROM credential_migration_records m JOIN credential_instances i ON i.account_id=m.account_id WHERE m.principal_id=$1 AND m.state<>'ROLLED_BACK' FOR UPDATE OF i,m`, principal).Scan(&account, &instance, &status, &schedulable, &concurrency, &archived, &aad)
	if err != nil {
		return err
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM credential_instances WHERE principal_id=$1 AND admin_state NOT IN ('DISABLED','REVOKED') AND id<>$2`, principal, instance).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("ROLLBACK_REQUIRES_SINGLE_INSTANCE")
	}
	plain, err := r.vault.OpenData(aad, archived)
	if err != nil {
		return err
	}
	defer clear(plain)
	var credentials map[string]any
	if err = json.Unmarshal(plain, &credentials); err != nil {
		return err
	}
	var secret []byte
	var secretAAD string
	err = tx.QueryRowContext(ctx, `SELECT s.secret_ciphertext,s.secret_aad FROM credential_instances i JOIN credential_secrets s ON s.instance_id=i.id AND s.credential_version=i.credential_version WHERE i.id=$1`, instance).Scan(&secret, &secretAAD)
	if err != nil {
		return err
	}
	latest, err := r.vault.Open(secretAAD, secret)
	if err != nil {
		return err
	}
	if credentials["access_token"] != latest.AccessToken {
		delete(credentials, "id_token")
	}
	credentials["access_token"] = latest.AccessToken
	if latest.RefreshToken != "" {
		credentials["refresh_token"] = latest.RefreshToken
	} else {
		delete(credentials, "refresh_token")
	}
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT s.expires_at FROM credential_instances i JOIN credential_secrets s ON s.instance_id=i.id AND s.credential_version=i.credential_version WHERE i.id=$1`, instance).Scan(&expires)
	if err != nil {
		return err
	}
	credentials["expires_at"] = expires.UTC().Format(time.RFC3339)
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return err
	}

	fps, err := credentialInstanceAliasFingerprints(ctx, tx, instance)
	if err != nil {
		return err
	}
	latestFingerprints := credentialTokenFingerprints(r.vault, credentials)
	fps = sortedCredentialFingerprints(append(fps, latestFingerprints...))
	if err = lockCredentialTokens(ctx, tx, fps); err != nil {
		return err
	}
	conflict, err := hasForeignCredentialAliasClaims(ctx, tx, fps, instance, "")
	if err != nil {
		return err
	}
	if conflict {
		return service.ErrCredentialLegacyBypass
	}
	if err = requireNoLegacyCredentialOwner(ctx, tx, fps, account); err != nil {
		return err
	}
	if err = recordLegacyClaims(ctx, tx, account, fps); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE credential_legacy_account_claims SET refresh_spent=NOT(fingerprint=ANY($2)) WHERE account_id=$1`, account, pq.Array(latestFingerprints)); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `SET LOCAL sub2api.credential_control='on'`)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE session_bindings SET state='REVOKED',version=version+1,tombstone_until=CURRENT_TIMESTAMP+INTERVAL '7 days' WHERE principal_id=$1 AND state IN ('ACTIVE','DRAINING')`, principal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_instances SET admin_state='REVOKED',retired_to_legacy=true WHERE id=$1`, instance)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=$2,status=$3,schedulable=$4,concurrency=$5 WHERE id=$1`, account, string(encoded), status, false, concurrency)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_principals SET routing_mode='OFF',admin_state='PAUSED',config_version=config_version+1,admission_epoch=admission_epoch+1 WHERE id=$1`, principal)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE credential_migration_records SET state='ROLLED_BACK',fence_evidence=$2 WHERE principal_id=$1`, principal, mustFenceJSON(fence))
	if err != nil {
		return err
	}
	if err = controlAudit(ctx, tx, actor, principal, instance, current+1, "PRINCIPAL_ROLLED_BACK", map[string]any{"retained_account_id": account, "fence": fence}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *CredentialRollout) requireAdmin(ctx context.Context, id int64) error {
	var allowed bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin' AND status='active' AND deleted_at IS NULL)`, id).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return errors.New("ADMIN_REQUIRED")
	}
	return nil
}
