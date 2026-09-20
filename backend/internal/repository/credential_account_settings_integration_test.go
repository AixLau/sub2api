//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCredentialAccountSettingsAtomicSaveAndLegacyProtection(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 10)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	principal, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	account, err := repo.GetByID(ctx, principal.AccountID)
	require.NoError(t, err)

	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SET LOCAL sub2api.credential_control='on'`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET concurrency=0,credentials=jsonb_build_object('account_id',id::text),extra='{"codex_5h_used_percent":71}' WHERE id IN(SELECT account_id FROM credential_instances WHERE principal_id=$1)`, f.principal)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	var group, proxy int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, uuid.NewString()).Scan(&group))
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO proxies(name,protocol,host,port,status) VALUES($1,'http','127.0.0.1',8080,'active') RETURNING id`, uuid.NewString()).Scan(&proxy))
	notes, rate, limit, load := "ordinary account note", 0.6, 4, 8
	expires := time.Now().Add(time.Hour).Truncate(time.Second)
	account.Name, account.Notes, account.Priority = "edited", &notes, 17
	account.ProxyID, account.RateMultiplier, account.LoadFactor = &proxy, &rate, &load
	account.ExpiresAt, account.AutoPauseOnExpired = &expires, true
	account.Credentials = map[string]any{"model_mapping": map[string]any{"requested": "upstream"}, "account_id": "must-not-propagate"}
	account.Extra = map[string]any{"openai_passthrough": true, "codex_5h_used_percent": float64(1)}
	input := &service.UpdateAccountInput{
		CredentialEdit: &service.CredentialAccountEdit{ActorID: f.user, PrincipalID: f.principal, ConfigVersion: 1},
		Concurrency:    &limit, GroupIDs: &[]int64{group}, RateMultiplier: &rate,
		Credentials: map[string]any{}, Extra: map[string]any{}, Status: "inactive",
	}
	require.NoError(t, repo.UpdateCredentialAccountSettings(ctx, account, input))
	updated, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	require.Equal(t, "edited", updated.Name)
	require.Equal(t, "PAUSED", updated.AdminState)
	require.Equal(t, limit, updated.RequestedLimit)
	require.EqualValues(t, 2, updated.ConfigVersion)
	require.Equal(t, []int64{group}, updated.GroupIDs)
	for _, instance := range updated.Instances {
		carrier, err := repo.GetByID(ctx, instance.AccountID)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprint(instance.AccountID), carrier.Credentials["account_id"])
		require.Equal(t, "upstream", carrier.Credentials["model_mapping"].(map[string]any)["requested"])
		require.Equal(t, float64(71), carrier.Extra["codex_5h_used_percent"])
		require.Equal(t, true, carrier.Extra["openai_passthrough"])
		require.Equal(t, &notes, carrier.Notes)
		require.Equal(t, &proxy, carrier.ProxyID)
		require.Equal(t, &rate, carrier.RateMultiplier)
		require.Equal(t, &load, carrier.LoadFactor)
		require.Equal(t, []int64{group}, carrier.GroupIDs)
		require.Equal(t, "inactive", carrier.Status)
		require.False(t, carrier.Schedulable)
		require.Zero(t, carrier.Concurrency)
		require.Equal(t, 10, *instance.HardMax, "account limit must not overwrite each instance limit")
	}
	account.Name = "stale editor"
	require.ErrorIs(t, repo.UpdateCredentialAccountSettings(ctx, account, input), errCredentialConfigConflict)
	still, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	require.Equal(t, "edited", still.Name)

	// An incompatible group fails after changing grants inside the transaction.
	// Both the previous grants and the settings/version must survive rollback.
	var mixedGroup, legacy int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, uuid.NewString()).Scan(&mixedGroup))
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,status,schedulable) VALUES('legacy','openai','oauth','active',true) RETURNING id`).Scan(&legacy))
	_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, legacy, mixedGroup)
	require.NoError(t, err)
	input.CredentialEdit.ConfigVersion = 2
	input.GroupIDs = &[]int64{mixedGroup}
	err = repo.UpdateCredentialAccountSettings(ctx, account, input)
	require.EqualError(t, err, "GROUPED_ROUTE_MIXED_UNSUPPORTED")
	still, err = NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	require.Equal(t, []int64{group}, still.GroupIDs)
	require.EqualValues(t, 2, still.ConfigVersion)
	var grants int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM account_groups WHERE group_id=$1 AND account_id=ANY($2)`, group, pq.Array([]int64{updated.Instances[0].AccountID, updated.Instances[1].AccountID, updated.Instances[2].AccountID})).Scan(&grants))
	require.Equal(t, 3, grants)

	// The ordinary account repository remains guarded outside this operation.
	_, err = integrationDB.Exec(`UPDATE accounts SET status='active',schedulable=true WHERE id=$1`, account.ID)
	require.ErrorContains(t, err, "controlled credential account requires principal API")
}
