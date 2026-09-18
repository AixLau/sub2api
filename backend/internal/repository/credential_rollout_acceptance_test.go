//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCredentialComposeFenceMigrationCanaryRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	project := "credential-acceptance-" + uuid.NewString()
	var containers []string
	runDocker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).Output()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	// Each synthetic old node holds a process and its own network namespace.
	network := runDocker("network", "create", project)
	for range 3 {
		id := runDocker("run", "--detach", "--restart=always", "--network", network, "--label", "com.docker.compose.project="+project, "--label", "com.docker.compose.service=gateway", "alpine:3.20", "sh", "-c", `trap "exit 0" TERM; while :; do sleep 1 & wait $!; done`)
		containers = append(containers, id)
	}
	t.Cleanup(func() {
		for _, id := range containers {
			_ = exec.Command("docker", "stop", "--time", "1", id).Run()
		}
	})
	var actor, account, group int64
	nonce := uuid.NewString()
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES($1,'fixture','admin') RETURNING id`, nonce+"@example.test").Scan(&actor))
	credentials := map[string]any{"access_token": "mock:" + nonce + ":old", "refresh_token": "refresh:" + nonce + ":old", "chatgpt_account_id": "workspace-" + nonce, "chatgpt_user_id": "user-a", "model_mapping": map[string]string{"alias": "gpt-5.4"}}
	data, _ := json.Marshal(credentials)
	seed := uuid.NewString()
	extra := fmt.Sprintf(`{"codex_fingerprint_seed":%q,"codex_fingerprint_mode":"device","custom":"preserved"}`, seed)
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,credentials,extra,status,schedulable,concurrency,rate_multiplier) VALUES('old','openai','oauth',$1,$2,'active',true,5,1.25) RETURNING id`, string(data), extra).Scan(&account))
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, nonce).Scan(&group))
	_, err := integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account, group)
	require.NoError(t, err)
	require.NoError(t, integrationDB.QueryRow(`SELECT extra::text FROM accounts WHERE id=$1`, account).Scan(&extra))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	imports := service.NewCredentialImportService(NewCredentialImportRepository(integrationDB), vault, mockCredentialVerifier{})
	imported, err := imports.Import(ctx, actor, "migration", service.CredentialSecret{AccessToken: credentials["access_token"].(string), RefreshToken: credentials["refresh_token"].(string)})
	require.NoError(t, err)
	fence := credentialfence.Docker{Project: project, Service: "gateway"}
	rollout := NewCredentialRollout(integrationDB, vault, fence)
	input := CredentialMigrationInput{ActorID: actor, AccountID: account, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 5, DrainEvidence: "mock fixture has never sent upstream requests; active execution count=0"}
	principal, err := rollout.Migrate(ctx, input)
	require.NoError(t, err)
	replay, err := rollout.Migrate(ctx, input)
	require.NoError(t, err)
	require.Equal(t, principal, replay)
	evidence := credentialfence.Evidence{IDs: append([]string(nil), containers...), Project: project, Service: "gateway"}
	sort.Strings(evidence.IDs)
	require.NoError(t, fence.Verify(ctx, evidence))
	// Restarting a fenced old process does not restore a network. Verification
	// still rejects the running stale node until it is stopped again.
	runDocker("start", containers[0])
	require.Error(t, fence.Verify(ctx, evidence))
	out := runDocker("inspect", "--format", "{{len .NetworkSettings.Networks}}", containers[0])
	require.Equal(t, "0", out)
	runDocker("stop", "--time", "1", containers[0])
	require.NoError(t, fence.Verify(ctx, evidence))
	var savedExtra string
	var multiplier float64
	var grants int
	var currentID int64
	require.NoError(t, integrationDB.QueryRow(`SELECT id,extra::text,rate_multiplier FROM accounts WHERE id=$1`, account).Scan(&currentID, &savedExtra, &multiplier))
	require.Equal(t, account, currentID)
	require.JSONEq(t, extra, savedExtra)
	require.Equal(t, 1.25, multiplier)
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=$2`, account, group).Scan(&grants))
	require.Equal(t, 1, grants)
	require.NoError(t, rollout.Canary(ctx, actor, principal, 1))
	var instance int64
	var generation, installation string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.id,i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.principal_id=$1`, principal).Scan(&instance, &generation, &installation))
	// Rotate via the versioned coordinator store, then rollback must retain it.
	refresh := &credentialRefreshStore{db: integrationDB}
	op, err := refresh.BeginCredentialRefresh(ctx, instance)
	require.NoError(t, err)
	sealed, err := vault.Seal(op.ID, service.CredentialSecret{AccessToken: "mock-new-access", RefreshToken: "mock-new-refresh"})
	require.NoError(t, err)
	require.NoError(t, refresh.CompleteCredentialRefresh(ctx, op, service.CredentialRefreshResult{Ciphertext: sealed, AAD: op.ID, ExpiresAt: time.Now().Add(time.Hour), AccessFingerprint: vault.Fingerprint("token", "mock-new-access"), RefreshFingerprint: vault.Fingerprint("token", "mock-new-refresh")}))
	require.NoError(t, rollout.Rollback(ctx, actor, principal, 2))
	var returned []byte
	var schedulable bool
	require.NoError(t, integrationDB.QueryRow(`SELECT credentials,schedulable,extra::text FROM accounts WHERE id=$1`, account).Scan(&returned, &schedulable, &savedExtra))
	require.False(t, schedulable)
	require.JSONEq(t, extra, savedExtra)
	var got map[string]any
	require.NoError(t, json.Unmarshal(returned, &got))
	require.Equal(t, "mock-new-refresh", got["refresh_token"])
	require.Equal(t, "mock-new-access", got["access_token"])
	require.Equal(t, "gpt-5.4", got["model_mapping"].(map[string]any)["alias"])
	var oldGeneration, oldInstallation string
	require.NoError(t, integrationDB.QueryRow(`SELECT i.identity_generation,p.installation_id FROM credential_instances i JOIN credential_identity_profiles p ON p.instance_id=i.id WHERE i.id=$1`, instance).Scan(&oldGeneration, &oldInstallation))
	require.Equal(t, generation, oldGeneration)
	require.Equal(t, installation, oldInstallation)
}

type forbiddenAcceptanceFence struct{ calls int }

func (f *forbiddenAcceptanceFence) Fence(context.Context) (credentialfence.Evidence, error) {
	f.calls++
	return credentialfence.Evidence{}, fmt.Errorf("must not fence unverified migration")
}
func (f *forbiddenAcceptanceFence) Verify(context.Context, credentialfence.Evidence) error {
	return fmt.Errorf("must not verify")
}
func TestCredentialRolloutUnverifiedBlockedBeforeFence(t *testing.T) {
	ctx := context.Background()
	var actor int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES($1,'fixture','admin') RETURNING id`, uuid.NewString()+"@example.test").Scan(&actor))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	imports := service.NewCredentialImportService(NewCredentialImportRepository(integrationDB), vault, nil)
	imported, err := imports.Import(ctx, actor, "unverified", service.CredentialSecret{AccessToken: uuid.NewString()})
	require.NoError(t, err)
	require.Equal(t, "UNVERIFIED", imported.State)
	fence := &forbiddenAcceptanceFence{}
	rollout := NewCredentialRollout(integrationDB, vault, fence)
	_, err = rollout.Migrate(ctx, CredentialMigrationInput{ActorID: actor, AccountID: 1, ImportID: imported.ID, OperationID: uuid.NewString(), RequestedLimit: 5, DrainEvidence: "fixture proof cannot replace provider verification"})
	require.ErrorIs(t, err, service.ErrCredentialUnverified)
	require.Zero(t, fence.calls)
}
