//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"
)

func TestCredentialLifecycleReplacementDoesNotChangeOldIdentity(t *testing.T) {
	ctx := context.Background()
	repo := &credentialImportRepository{db: integrationDB}
	nonce := uuid.NewString()
	var owner int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, nonce+"@example.test").Scan(&owner))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	importer := service.NewCredentialImportService(repo, vault, mockCredentialVerifier{})
	first, err := importer.Import(ctx, owner, "first", service.CredentialSecret{AccessToken: "mock:" + nonce + ":first", RefreshToken: "refresh:" + nonce + ":first"})
	require.NoError(t, err)
	principal, err := repo.CreateCredentialPrincipal(ctx, owner, "create", service.CreateCredentialPrincipalInput{Name: "test", TotalConcurrency: 10, Instances: []service.CreateCredentialInstanceInput{{Name: "old", ImportID: first.ID, Weight: 1}}})
	require.NoError(t, err)
	var old int64
	var generation, installation string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.id,i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.principal_id=$1`, principal).Scan(&old, &generation, &installation))
	next, err := importer.Import(ctx, owner, "next", service.CredentialSecret{AccessToken: "mock:" + nonce + ":next", RefreshToken: "refresh:" + nonce + ":next"})
	require.NoError(t, err)
	deadline := time.Now().Add(time.Hour)
	input := service.CredentialInstanceAddInput{CreateCredentialInstanceInput: service.CreateCredentialInstanceInput{Name: "new", ImportID: next.ID, Weight: 1}, ReplaceInstanceID: old, DrainDeadline: &deadline}
	id, err := repo.AddCredentialInstance(ctx, owner, principal, 1, "replace", input)
	require.NoError(t, err)
	require.NotEqual(t, old, id)
	replay, err := repo.AddCredentialInstance(ctx, owner, principal, 1, "replace", input)
	require.NoError(t, err)
	require.Equal(t, id, replay)
	var oldGeneration, oldInstallation, newGeneration, state string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.identity_generation,p.installation_id,i.admin_state FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.id=$1`, old).Scan(&oldGeneration, &oldInstallation, &state))
	require.Equal(t, generation, oldGeneration)
	require.Equal(t, installation, oldInstallation)
	require.Equal(t, "DRAINING", state)
	require.NoError(t, integrationDB.QueryRow(`SELECT identity_generation FROM credential_instances WHERE id=$1`, id).Scan(&newGeneration))
	require.NotEqual(t, generation, newGeneration)
}
