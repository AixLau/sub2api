//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func aliasClaimRefresh(t *testing.T, instance int64) service.CredentialRefreshOperation {
	t.Helper()
	_, err := integrationDB.Exec(`UPDATE credential_secrets SET refresh_family=$2,can_refresh=true WHERE instance_id=$1`, instance, uuid.NewString())
	require.NoError(t, err)
	op, err := (&credentialRefreshStore{db: integrationDB}).BeginCredentialRefresh(context.Background(), instance)
	require.NoError(t, err)
	return op
}

func TestCredentialAliasClaimsUnknownConflictRetainsBothResults(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 3)
	fingerprint := uuid.NewString()
	_, err := integrationDB.Exec(`UPDATE credential_instances SET retired_to_legacy=true,admin_state='REVOKED' WHERE id=$1`, f.instances[0])
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,'ACCESS')`, fingerprint, f.instances[0])
	require.NoError(t, err)
	first, second := aliasClaimRefresh(t, f.instances[1]), aliasClaimRefresh(t, f.instances[2])
	store := &credentialRefreshStore{db: integrationDB}
	r1 := service.CredentialRefreshResult{AAD: first.ID, Ciphertext: []byte("encrypted-first"), AccessFingerprint: fingerprint, ExpiresAt: time.Now().Add(time.Hour)}
	r2 := service.CredentialRefreshResult{AAD: second.ID, Ciphertext: []byte("encrypted-second"), AccessFingerprint: fingerprint, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, first, &r1))
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, second, &r2))
	// Age is deliberately extreme: no expiry may release either unknown claim.
	_, err = integrationDB.Exec(`UPDATE credential_refresh_ops SET started_at=CURRENT_TIMESTAMP-INTERVAL '10 years' WHERE id IN ($1,$2)`, first.ID, second.ID)
	require.NoError(t, err)
	known, err := (&accountRepository{sql: integrationDB}).KnownCredentialTokenFingerprints(ctx, []string{fingerprint})
	require.NoError(t, err)
	require.True(t, known)
	require.ErrorIs(t, store.CompleteCredentialRefresh(ctx, first, r1), service.ErrCredentialDuplicate)
	// The coordinator follows a failed Complete with this same compensation.
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, first, &r1))
	for _, expected := range []struct {
		op     service.CredentialRefreshOperation
		result service.CredentialRefreshResult
	}{{first, r1}, {second, r2}} {
		var state string
		var cipher []byte
		var version int64
		require.NoError(t, integrationDB.QueryRow(`SELECT o.state,o.result_ciphertext,i.credential_version FROM credential_refresh_ops o JOIN credential_instances i ON i.id=o.instance_id WHERE o.id=$1`, expected.op.ID).Scan(&state, &cipher, &version))
		require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
		require.Equal(t, expected.result.Ciphertext, cipher)
		require.EqualValues(t, 1, version)
	}
	var owner int64
	require.NoError(t, integrationDB.QueryRow(`SELECT instance_id FROM credential_fingerprints WHERE fingerprint=$1`, fingerprint).Scan(&owner))
	require.Equal(t, f.instances[0], owner, "historical owner is immutable")
	var claims int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_refresh_alias_claims WHERE fingerprint=$1`, fingerprint).Scan(&claims))
	require.Equal(t, 2, claims)
}

func TestCredentialAliasClaimsCompleteTransfersOnlyOwnOperation(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 3)
	fingerprint, unrelated := uuid.NewString(), uuid.NewString()
	_, err := integrationDB.Exec(`UPDATE credential_instances SET retired_to_legacy=true,admin_state='REVOKED' WHERE id=$1`, f.instances[0])
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,'ACCESS')`, fingerprint, f.instances[0])
	require.NoError(t, err)
	first, second := aliasClaimRefresh(t, f.instances[1]), aliasClaimRefresh(t, f.instances[2])
	store := &credentialRefreshStore{db: integrationDB}
	r1 := service.CredentialRefreshResult{AAD: first.ID, Ciphertext: []byte("encrypted-first"), AccessFingerprint: fingerprint, ExpiresAt: time.Now().Add(time.Hour)}
	r2 := service.CredentialRefreshResult{AAD: second.ID, Ciphertext: []byte("encrypted-second"), AccessFingerprint: unrelated, ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, first, &r1))
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, second, &r2))
	require.NoError(t, store.CompleteCredentialRefresh(ctx, first, r1))
	var historicalOwner int64
	require.NoError(t, integrationDB.QueryRow(`SELECT instance_id FROM credential_fingerprints WHERE fingerprint=$1`, fingerprint).Scan(&historicalOwner))
	require.Equal(t, f.instances[0], historicalOwner)
	var activeOwner int64
	require.NoError(t, integrationDB.QueryRow(`SELECT instance_id FROM credential_instance_alias_claims WHERE fingerprint=$1`, fingerprint).Scan(&activeOwner))
	require.Equal(t, f.instances[1], activeOwner)
	registry := &accountRepository{sql: integrationDB}
	known, err := registry.KnownCredentialTokenFingerprints(ctx, []string{fingerprint})
	require.NoError(t, err)
	require.True(t, known, "new instance claim survives the retired historical owner")
	foreign, err := hasForeignCredentialAliasClaims(ctx, integrationDB, []string{fingerprint}, f.instances[2], "")
	require.NoError(t, err)
	require.True(t, foreign)
	aliases, err := credentialInstanceAliasFingerprints(ctx, integrationDB, f.instances[1])
	require.NoError(t, err)
	require.Contains(t, aliases, fingerprint)
	_, err = integrationDB.Exec(`UPDATE credential_instances SET retired_to_legacy=true,admin_state='REVOKED' WHERE id=$1`, f.instances[1])
	require.NoError(t, err)
	known, err = registry.KnownCredentialTokenFingerprints(ctx, []string{fingerprint})
	require.NoError(t, err)
	require.False(t, known, "SUCCEEDED operation does not leave an active operation claim")
	known, err = registry.KnownCredentialTokenFingerprints(ctx, []string{unrelated})
	require.NoError(t, err)
	require.True(t, known, "completing one operation cannot clear another UNKNOWN claim")
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM credential_refresh_ops WHERE id=$1`, second.ID).Scan(&state))
	require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
}

func TestCredentialAliasClaimsUnknownBackfillFailsClosed(t *testing.T) {
	ctx := context.Background()
	db, _ := vaultRotationTestDB(t)
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	var principal, account, instance int64
	generation := uuid.NewString()
	require.NoError(t, db.QueryRow(`INSERT INTO upstream_principals(name,provider,requested_limit) VALUES('backfill','openai_oauth',1) RETURNING id`).Scan(&principal))
	require.NoError(t, db.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES('backfill','openai','oauth') RETURNING id`).Scan(&account))
	require.NoError(t, db.QueryRow(`INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,retired_to_legacy) VALUES($1,$2,'backfill',$3,true) RETURNING id`, principal, account, generation).Scan(&instance))
	operation := uuid.NewString()
	secret := service.CredentialSecret{AccessToken: "historical-unknown-access", RefreshToken: " historical-unknown-refresh "}
	cipher, err := vault.Seal(operation, secret)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO credential_refresh_ops(id,instance_id,generation,family_key,expected_version,owner_nonce,state,result_ciphertext,result_aad) VALUES($1,$2,$3,$4,1,$5,'REFRESH_RESULT_UNKNOWN',$6,$7)`, operation, instance, generation, uuid.NewString(), uuid.NewString(), cipher, operation)
	require.NoError(t, err)
	registry := &accountRepository{sql: db}
	fingerprint := vault.Fingerprint("token", secret.AccessToken)
	known, err := registry.KnownCredentialTokenFingerprints(ctx, []string{fingerprint})
	require.NoError(t, err)
	require.False(t, known, "fixture represents a historical missing alias, before initialization")
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.ErrorIs(t, backfillUnknownAliasClaims(ctx, tx, nil), service.ErrCredentialVaultUnavailable)
	require.NoError(t, tx.Rollback())
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, backfillUnknownAliasClaims(ctx, tx, vault))
	require.NoError(t, tx.Commit())
	for _, token := range []string{secret.AccessToken, secret.RefreshToken, strings.TrimSpace(secret.RefreshToken)} {
		known, err = registry.KnownCredentialTokenFingerprints(ctx, []string{vault.Fingerprint("token", token)})
		require.NoError(t, err)
		require.True(t, known)
	}
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, backfillUnknownAliasClaims(ctx, tx, vault), "repeat backfill is idempotent")
	require.NoError(t, tx.Commit())
	_, err = db.Exec(`UPDATE credential_refresh_ops SET result_ciphertext='corrupt' WHERE id=$1`, operation)
	require.NoError(t, err)
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.ErrorIs(t, backfillUnknownAliasClaims(ctx, tx, vault), service.ErrCredentialVaultUnavailable)
	require.NoError(t, tx.Rollback())
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM credential_refresh_ops WHERE id=$1`, operation).Scan(&state))
	require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
}
