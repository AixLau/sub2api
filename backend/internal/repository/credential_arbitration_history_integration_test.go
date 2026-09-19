//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// These pre-initialization fixtures deliberately represent already-conflicting
// persisted data. They do not call a provider or bypass verification in a server.
func TestCredentialArbitrationHistoryBootstrapRejectsExistingConflict(t *testing.T) {
	for _, state := range []string{"active_instance", "unknown_result"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			db, _ := vaultRotationTestDB(t)
			vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
			require.NoError(t, err)
			secret := arbitrationSecret()
			document, err := json.Marshal(arbitrationAccount(secret).Credentials)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO accounts(name,platform,type,credentials,status,schedulable) VALUES('pre-existing legacy','openai','oauth',$1,'active',true)`, string(document))
			require.NoError(t, err)
			var principal, carrier, instance int64
			generation := uuid.NewString()
			require.NoError(t, db.QueryRow(`INSERT INTO upstream_principals(name,provider,requested_limit) VALUES('pre-existing control','openai_oauth',1) RETURNING id`).Scan(&principal))
			require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,status,schedulable) VALUES('controlled carrier','openai','oauth','{}','inactive',false) RETURNING id`).Scan(&carrier))
			require.NoError(t, db.QueryRow(`INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,credential_state,admin_state,retired_to_legacy) VALUES($1,$2,'control',$3,'VALID','ACTIVE',$4) RETURNING id`, principal, carrier, generation, state == "unknown_result").Scan(&instance))
			if state == "active_instance" {
				_, err = db.Exec(`INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,'ACCESS')`, vault.Fingerprint("token", secret.AccessToken), instance)
				require.NoError(t, err)
			} else {
				operation := uuid.NewString()
				cipher, err := vault.Seal(operation, secret)
				require.NoError(t, err)
				// No alias rows: bootstrap must recover the independent UNKNOWN
				// claims from ciphertext before certifying the legacy index.
				_, err = db.Exec(`INSERT INTO credential_refresh_ops(id,instance_id,generation,family_key,expected_version,owner_nonce,state,result_ciphertext,result_aad) VALUES($1,$2,$3,$4,1,$5,'REFRESH_RESULT_UNKNOWN',$6,$7)`, operation, instance, generation, uuid.NewString(), uuid.NewString(), cipher, operation)
				require.NoError(t, err)
			}
			require.ErrorIs(t, configureCredentialArbitration(ctx, db, vault), service.ErrCredentialLegacyBypass)
			var initialized sql.NullTime
			var key sql.NullString
			require.NoError(t, db.QueryRow(`SELECT initialized_at,fingerprint_key_id FROM credential_arbitration_state WHERE singleton=true`).Scan(&initialized, &key))
			require.False(t, initialized.Valid)
			require.False(t, key.Valid)
			var claims int
			require.NoError(t, db.QueryRow(`SELECT count(*) FROM credential_legacy_account_claims`).Scan(&claims))
			require.Zero(t, claims, "failed bootstrap rolls back its partial index")
			var persisted string
			var schedulable bool
			require.NoError(t, db.QueryRow(`SELECT credentials->>'access_token',schedulable FROM accounts WHERE name='pre-existing legacy'`).Scan(&persisted, &schedulable))
			require.Equal(t, secret.AccessToken, persisted)
			require.True(t, schedulable, "bootstrap rejects startup instead of silently rewriting legacy identity or scheduling")
		})
	}
}

func TestCredentialArbitrationHistoryMigrationAndRollbackRetainOldAliases(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	original, latest := arbitrationSecret(), arbitrationSecret()
	account := arbitrationAccount(original)
	require.NoError(t, f.repo.Create(ctx, account))
	require.NoError(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(latest).Credentials))
	imported, err := f.imports.Import(ctx, f.actor, "history-migration", latest)
	require.NoError(t, err)
	rollout := NewCredentialRollout(f.db, f.vault, &vaultRotationFence{})
	principal, err := rollout.Migrate(ctx, CredentialMigrationInput{AccountID: account.ID, ActorID: f.actor, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 3, DrainEvidence: "isolated synthetic legacy history; no upstream execution occurred"})
	require.NoError(t, err)
	var instance int64
	require.NoError(t, f.db.QueryRow(`SELECT id FROM credential_instances WHERE principal_id=$1`, principal).Scan(&instance))
	aliases, err := credentialInstanceAliasFingerprints(ctx, f.db, instance)
	require.NoError(t, err)
	for _, fp := range append(f.fingerprints(original), f.fingerprints(latest)...) {
		require.Contains(t, aliases, fp, "migration must retain all known historical aliases")
	}
	require.ErrorIs(t, f.repo.Create(ctx, arbitrationAccount(original)), service.ErrCredentialLegacyBypass)
	require.NoError(t, rollout.Rollback(ctx, f.actor, principal, 1))
	var raw []byte
	var schedulable bool
	require.NoError(t, f.db.QueryRow(`SELECT credentials,schedulable FROM accounts WHERE id=$1`, account.ID).Scan(&raw, &schedulable))
	var restored map[string]any
	require.NoError(t, json.Unmarshal(raw, &restored))
	require.Equal(t, latest.AccessToken, restored["access_token"])
	require.Equal(t, latest.RefreshToken, restored["refresh_token"])
	require.False(t, schedulable)
	for _, fp := range f.fingerprints(original) {
		var spent, transferred bool
		require.NoError(t, f.db.QueryRow(`SELECT refresh_spent,transferred FROM credential_legacy_account_claims WHERE account_id=$1 AND fingerprint=$2`, account.ID, fp).Scan(&spent, &transferred))
		require.True(t, spent)
		require.False(t, transferred)
	}
	require.ErrorIs(t, f.repo.Create(ctx, arbitrationAccount(original)), service.ErrCredentialLegacyBypass)
	require.ErrorIs(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(original).Credentials), service.ErrCredentialLegacyBypass)
	require.NoError(t, f.repo.UpdateCredentials(ctx, account.ID, restored), "latest returned carrier can still persist its own current credentials")
	var accounts int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&accounts))
	require.Equal(t, 1, accounts, "rejected old-token creates cannot leave a second carrier")
}

func TestCredentialArbitrationHistoryRefreshCannotOverwriteAdminCredentials(t *testing.T) {
	for _, change := range []string{"after_finish", "during_provider"} {
		t.Run(change, func(t *testing.T) {
			f := newOperationArbitrationFixture(t)
			ctx := context.Background()
			original, result, replacement := arbitrationSecret(), arbitrationSecret(), arbitrationSecret()
			account := arbitrationAccount(original)
			require.NoError(t, f.repo.Create(ctx, account))
			input := []string{f.vault.Fingerprint("token", original.RefreshToken)}
			op, err := f.repo.BeginLegacyCredentialRefresh(ctx, input, "admin-write-window")
			require.NoError(t, err)
			plain, err := json.Marshal(map[string]any{"access_token": result.AccessToken, "refresh_token": result.RefreshToken})
			require.NoError(t, err)
			cipher, err := f.vault.SealData(op.ID, plain)
			require.NoError(t, err)
			if change == "after_finish" {
				require.NoError(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(result), cipher, true))
			}
			require.NoError(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(replacement).Credentials))
			if change == "during_provider" {
				require.ErrorIs(t, f.repo.FinishLegacyCredentialRefresh(ctx, op, f.fingerprints(result), cipher, true), service.ErrCredentialLegacyBypass)
			}
			require.ErrorIs(t, f.repo.UpdateCredentials(ctx, account.ID, arbitrationAccount(result).Credentials), service.ErrCredentialLegacyBypass)
			var persisted, refresh, state, aad string
			var retained []byte
			require.NoError(t, f.db.QueryRow(`SELECT credentials->>'access_token',credentials->>'refresh_token' FROM accounts WHERE id=$1`, account.ID).Scan(&persisted, &refresh))
			require.Equal(t, replacement.AccessToken, persisted)
			require.Equal(t, replacement.RefreshToken, refresh)
			require.NoError(t, f.db.QueryRow(`SELECT state,result_aad,result_ciphertext FROM credential_legacy_refresh_operations WHERE id=$1`, op.ID).Scan(&state, &aad, &retained))
			if change == "during_provider" {
				require.Equal(t, "UNKNOWN", state)
			} else {
				require.Equal(t, "SUCCEEDED", state, "rejecting a stale account write must not erase a reliable result")
			}
			decoded, err := f.vault.OpenData(aad, retained)
			require.NoError(t, err)
			require.JSONEq(t, string(plain), string(decoded), "rejected publication retains the rotated token without an upstream retry")
			var aliases int
			require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_refresh_aliases WHERE operation_id=$1 AND is_output`, op.ID).Scan(&aliases))
			require.Equal(t, 2, aliases)
		})
	}
}

func TestCredentialArbitrationHistoryBulkDuplicateRollsBack(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx := context.Background()
	first, second, duplicate := arbitrationSecret(), arbitrationSecret(), arbitrationSecret()
	a, b := arbitrationAccount(first), arbitrationAccount(second)
	require.NoError(t, f.repo.Create(ctx, a))
	require.NoError(t, f.repo.Create(ctx, b))
	_, err := f.repo.BulkUpdate(ctx, []int64{b.ID, a.ID}, service.AccountBulkUpdate{Credentials: arbitrationAccount(duplicate).Credentials})
	require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	for _, expected := range []struct {
		account int64
		secret  service.CredentialSecret
	}{{a.ID, first}, {b.ID, second}} {
		var access, refresh string
		require.NoError(t, f.db.QueryRow(`SELECT credentials->>'access_token',credentials->>'refresh_token' FROM accounts WHERE id=$1`, expected.account).Scan(&access, &refresh))
		require.Equal(t, expected.secret.AccessToken, access)
		require.Equal(t, expected.secret.RefreshToken, refresh)
	}
	var claims, registrations int
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_legacy_account_claims WHERE fingerprint=ANY($1)`, pq.Array(f.fingerprints(duplicate))).Scan(&claims))
	require.NoError(t, f.db.QueryRow(`SELECT count(*) FROM credential_token_registry WHERE fingerprint=ANY($1)`, pq.Array(f.fingerprints(duplicate))).Scan(&registrations))
	require.Zero(t, claims, "failed bulk update rolls back claims provisionally written for its first account")
	require.Zero(t, registrations, "the failed batch leaves no partial token registration")
}
