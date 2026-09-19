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

// The original direct-SQL schedule is preserved in the recorded a212 baseline
// overlay. This regression exercises the configured production writer: an old
// read-only precheck does not grant permission to bypass write-time arbitration.
func TestCredentialArbitrationLegacyCheckImportWrite(t *testing.T) {
	f := newOperationArbitrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	secret := arbitrationSecret()
	checked := make(chan bool, 1)
	resume := make(chan struct{})
	completed := make(chan error, 1)
	go func() {
		known, err := f.repo.KnownCredentialTokenFingerprints(ctx, f.fingerprints(secret))
		if err != nil {
			completed <- err
			return
		}
		checked <- known
		select {
		case <-resume:
		case <-ctx.Done():
			completed <- ctx.Err()
			return
		}
		completed <- f.repo.Create(ctx, arbitrationAccount(secret))
	}()
	select {
	case known := <-checked:
		require.False(t, known)
	case err := <-completed:
		t.Fatalf("check failed: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	imported, err := f.imports.Import(ctx, f.actor, "controlled-after-legacy-check", secret)
	require.NoError(t, err)
	var cipher []byte
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT secret_ciphertext FROM credential_imports WHERE id=$1`, imported.ID).Scan(&cipher))
	retained, err := f.vault.Open(imported.ID, cipher)
	require.NoError(t, err)
	require.Equal(t, secret.AccessToken, retained.AccessToken)
	close(resume)
	select {
	case err := <-completed:
		require.ErrorIs(t, err, service.ErrCredentialLegacyBypass)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var accounts int
	require.NoError(t, f.db.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&accounts))
	require.Zero(t, accounts)
	_, err = f.createPrincipal(ctx, imported)
	require.NoError(t, err, "the controlled carrier alone may be created")
}

func TestCredentialArbitrationBaselineUnknownAliasRetiredCollision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	active := newAdmissionFixture(t, 3)
	retired := newAdmissionFixture(t, 3)
	instance, oldOwner := active.instances[0], retired.instances[0]
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	initial := service.CredentialSecret{AccessToken: "mock:" + uuid.NewString() + ":old", RefreshToken: "refresh:" + uuid.NewString() + ":old"}
	initialCipher, err := vault.Seal("arbitration-initial", initial)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE credential_secrets SET secret_ciphertext=$2,secret_aad='arbitration-initial',refresh_family=$3,can_refresh=true WHERE instance_id=$1`, instance, initialCipher, uuid.NewString())
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE credential_instances SET retired_to_legacy=true,admin_state='REVOKED' WHERE id=$1`, oldOwner)
	require.NoError(t, err)
	rotated := service.CredentialSecret{AccessToken: "mock:" + uuid.NewString() + ":rotated", RefreshToken: "refresh:" + uuid.NewString() + ":rotated"}
	accessFingerprint := vault.Fingerprint("token", rotated.AccessToken)
	refreshFingerprint := vault.Fingerprint("token", rotated.RefreshToken)
	for _, alias := range []struct{ fingerprint, kind string }{{accessFingerprint, "ACCESS"}, {refreshFingerprint, "REFRESH"}} {
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO credential_fingerprints(fingerprint,instance_id,kind) VALUES($1,$2,$3)`, alias.fingerprint, oldOwner, alias.kind)
		require.NoError(t, err)
	}
	registry := &accountRepository{sql: integrationDB}
	known, err := registry.KnownCredentialTokenFingerprints(ctx, []string{accessFingerprint, refreshFingerprint})
	require.NoError(t, err)
	require.False(t, known, "a retired historical owner alone is exempt in the baseline legacy lookup")
	refresh := &credentialRefreshStore{db: integrationDB}
	op, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	cipher, err := vault.Seal(op.ID, rotated)
	require.NoError(t, err)
	result := service.CredentialRefreshResult{Ciphertext: cipher, AAD: op.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: accessFingerprint, RefreshFingerprint: refreshFingerprint}
	require.NoError(t, refresh.MarkCredentialRefreshUnknown(ctx, op, &result))
	var state, aad, instanceState string
	var savedCipher []byte
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT o.state,o.result_aad,o.result_ciphertext,i.credential_state FROM credential_refresh_ops o JOIN credential_instances i ON i.id=o.instance_id WHERE o.id=$1`, op.ID).Scan(&state, &aad, &savedCipher, &instanceState))
	require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
	require.Equal(t, "REFRESH_UNKNOWN", instanceState)
	retained, err := vault.Open(aad, savedCipher)
	require.NoError(t, err)
	require.Equal(t, rotated, retained, "the unknown remote refresh result is durably retained")
	var fingerprintOwner int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT instance_id FROM credential_fingerprints WHERE fingerprint=$1`, accessFingerprint).Scan(&fingerprintOwner))
	require.Equal(t, oldOwner, fingerprintOwner, "conflicting historical ownership must not be stolen")
	known, err = registry.KnownCredentialTokenFingerprints(ctx, []string{accessFingerprint, refreshFingerprint})
	require.NoError(t, err)
	t.Logf("baseline unknown refresh ciphertext_retained=true active_unknown_owner=true alias_owner_retired=true known_lookup=%t", known)
	require.True(t, known, "active UNKNOWN compensation must block legacy use even when its alias collides with a retired owner")
}
