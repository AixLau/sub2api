//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCredentialRefreshFamilyVersionIdentityAT05AT28AT29(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := &credentialRefreshStore{db: integrationDB}
	family := uuid.NewString()
	_, err := integrationDB.Exec(`UPDATE credential_secrets SET refresh_family=$2,can_refresh=true WHERE instance_id=ANY($1)`, pq.Array(f.instances), family)
	require.NoError(t, err)
	var wg sync.WaitGroup
	ops := make(chan service.CredentialRefreshOperation, 3)
	for _, id := range f.instances {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			op, err := store.BeginCredentialRefresh(ctx, id)
			if err == nil {
				ops <- op
			}
		}(id)
	}
	wg.Wait()
	close(ops)
	var winner service.CredentialRefreshOperation
	count := 0
	for op := range ops {
		winner = op
		count++
	}
	require.Equal(t, 1, count)
	var install, generation string
	require.NoError(t, integrationDB.QueryRow(`SELECT installation_id,generation FROM credential_identity_profiles WHERE instance_id=$1`, winner.InstanceID).Scan(&install, &generation))
	result := service.CredentialRefreshResult{Ciphertext: []byte("encrypted-result"), AAD: winner.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: uuid.NewString(), RefreshFingerprint: uuid.NewString()}
	require.NoError(t, store.CompleteCredentialRefresh(ctx, winner, result))
	var currentInstall, currentGeneration string
	var version int64
	require.NoError(t, integrationDB.QueryRow(`SELECT p.installation_id,p.generation,i.credential_version FROM credential_identity_profiles p JOIN credential_instances i ON i.id=p.instance_id WHERE p.instance_id=$1`, winner.InstanceID).Scan(&currentInstall, &currentGeneration, &version))
	require.Equal(t, install, currentInstall)
	require.Equal(t, generation, currentGeneration)
	require.Equal(t, int64(2), version)
	require.Error(t, store.CompleteCredentialRefresh(ctx, winner, result), "old version cannot overwrite rotated credential")
	next, err := store.BeginCredentialRefresh(ctx, winner.InstanceID)
	require.NoError(t, err)
	require.NoError(t, store.MarkCredentialRefreshUnknown(ctx, next, &result))
	_, err = store.BeginCredentialRefresh(ctx, winner.InstanceID)
	require.Error(t, err)
	var state string
	var compensation []byte
	require.NoError(t, integrationDB.QueryRow(`SELECT state,result_ciphertext FROM credential_refresh_ops WHERE id=$1`, next.ID).Scan(&state, &compensation))
	require.Equal(t, "REFRESH_RESULT_UNKNOWN", state)
	require.Equal(t, result.Ciphertext, compensation)
}

func TestCredentialFailureOldVersionAndSharedQuotaAT28AT30(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := &principalAdmissionStore{db: integrationDB}
	var generation string
	require.NoError(t, integrationDB.QueryRow(`SELECT identity_generation FROM credential_instances WHERE id=$1`, f.instances[0]).Scan(&generation))
	_, err := integrationDB.Exec(`UPDATE credential_instances SET credential_version=2 WHERE id=$1`, f.instances[0])
	require.NoError(t, err)
	require.NoError(t, store.ObserveCredentialFailure(ctx, service.CredentialFailureObservation{PrincipalID: f.principal, InstanceID: f.instances[0], Generation: generation, CredentialVersion: 1, Status: 401, Origin: "UPSTREAM", Scope: "INSTANCE", Code: "CREDENTIAL_REJECTED", Confidence: "OBSERVED"}))
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT credential_state FROM credential_instances WHERE id=$1`, f.instances[0]).Scan(&state))
	require.Equal(t, "VALID", state)
	domain := uuid.NewString()
	_, err = integrationDB.Exec(`INSERT INTO upstream_quota_domains(id,dimension) VALUES($1,'mock')`, domain)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO upstream_principal_quota_domains(principal_id,quota_domain_id) VALUES($1,$2)`, f.principal, domain)
	require.NoError(t, err)
	require.NoError(t, store.BlockKnownQuotaDomain(ctx, domain, time.Now().Add(time.Hour), false))
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, "SHARED_QUOTA_PROTECTED", d.Reason)
	assertAdmissionLedger(t, f, 0)
}
