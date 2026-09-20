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
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.Exec(`SET LOCAL sub2api.credential_control='on'`)
	require.NoError(t, err)
	_, err = tx.Exec(`UPDATE accounts SET notes='account settings',credentials='{"account_id":"provider-identity","model_mapping":{"requested":"upstream"}}',extra='{"openai_passthrough":true,"codex_5h_used_percent":71}' WHERE id=(SELECT management_account_id FROM upstream_principals WHERE id=$1)`, principal)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
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
	var inheritedNotes string
	var inheritedPolicy, copiedIdentity, copiedRuntime bool
	require.NoError(t, integrationDB.QueryRow(`SELECT notes,credentials->'model_mapping'->>'requested'='upstream' AND extra->>'openai_passthrough'='true',credentials ? 'account_id',extra ? 'codex_5h_used_percent' FROM accounts WHERE id=(SELECT account_id FROM credential_instances WHERE id=$1)`, id).Scan(&inheritedNotes, &inheritedPolicy, &copiedIdentity, &copiedRuntime))
	require.Equal(t, "account settings", inheritedNotes)
	require.True(t, inheritedPolicy)
	require.False(t, copiedIdentity, "new authorization must own its identity")
	require.False(t, copiedRuntime, "new authorization must not inherit usage observations")

	attached, err := importer.Import(ctx, owner, "attach", service.CredentialSecret{AccessToken: "mock:" + nonce + ":attached", RefreshToken: "refresh:" + nonce + ":attached"})
	require.NoError(t, err)
	attachedPrincipal, err := repo.CreateCredentialPrincipal(ctx, owner, "attach-account", service.CreateCredentialPrincipalInput{Name: "ignored incoming name", TotalConcurrency: 99, Instances: []service.CreateCredentialInstanceInput{{Name: "attached", ImportID: attached.ID, Weight: 1}}})
	require.NoError(t, err)
	require.Equal(t, principal, attachedPrincipal)
	var configVersion, auditVersion int64
	require.NoError(t, integrationDB.QueryRow(`SELECT p.config_version,a.version FROM upstream_principals p JOIN credential_audit_outbox a ON a.principal_id=p.id AND a.event_type='PRINCIPAL_INSTANCE_ATTACHED' WHERE p.id=$1`, principal).Scan(&configVersion, &auditVersion))
	require.EqualValues(t, 3, configVersion)
	require.Equal(t, configVersion, auditVersion)
}
