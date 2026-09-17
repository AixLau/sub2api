//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type mockCredentialVerifier struct{}

func (mockCredentialVerifier) Verify(_ context.Context, s service.CredentialSecret) (service.VerifiedCredential, error) {
	user := "user-a"
	if strings.HasPrefix(s.AccessToken, "different-user") {
		user = "user-b"
	}
	return service.VerifiedCredential{State: "VERIFIED", Provider: "openai_oauth", AccountSubject: "workspace-" + strings.Split(s.AccessToken, ":")[1], UserSubject: user, Family: s.RefreshToken, ExpiresAt: time.Now().Add(time.Hour), Capabilities: []string{"responses", "passthrough"}}, nil
}
func TestCredentialImportControlAT01To04(t *testing.T) {
	ctx := context.Background()
	repo := &credentialImportRepository{db: integrationDB}
	var owner int64
	nonce := uuid.NewString()
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture-only') RETURNING id`, nonce+"@example.test").Scan(&owner))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	importer := service.NewCredentialImportService(repo, vault, mockCredentialVerifier{})
	var entries []service.CreateCredentialInstanceInput
	for i := range 3 {
		secret := service.CredentialSecret{AccessToken: fmt.Sprintf("mock:%s:%d", nonce, i), RefreshToken: fmt.Sprintf("refresh:%s:%d", nonce, i)}
		view, err := importer.Import(ctx, owner, fmt.Sprint(i), secret)
		require.NoError(t, err)
		require.Equal(t, "VERIFIED", view.State)
		replay, err := importer.Import(ctx, owner, fmt.Sprint(i), secret)
		require.NoError(t, err)
		require.Equal(t, view.ID, replay.ID)
		_, err = importer.Import(ctx, owner, "duplicate", secret)
		require.ErrorIs(t, err, service.ErrCredentialDuplicate)
		_, err = importer.Import(ctx, owner, fmt.Sprint(i), service.CredentialSecret{AccessToken: "changed:" + nonce, RefreshToken: "changed"})
		require.ErrorIs(t, err, service.ErrCredentialConflict)
		_, err = importer.Get(ctx, owner+1000, view.ID)
		require.ErrorIs(t, err, service.ErrCredentialNotFound)
		entries = append(entries, service.CreateCredentialInstanceInput{ImportID: view.ID, Name: fmt.Sprint(i), Weight: 1})
	}
	different, err := importer.Import(ctx, owner, "different", service.CredentialSecret{AccessToken: "different-user:" + nonce, RefreshToken: "different-family" + nonce})
	require.NoError(t, err)
	input := service.CreateCredentialPrincipalInput{Name: "fixture", TotalConcurrency: 10, Instances: []service.CreateCredentialInstanceInput{entries[0], {ImportID: different.ID, Name: "different", Weight: 1}}}
	_, err = repo.CreateCredentialPrincipal(ctx, owner, "bad", input)
	require.ErrorIs(t, err, service.ErrCredentialUnverified)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM credential_imports WHERE owner_id=$1 AND verification_state='CONSUMED'`, owner).Scan(&count))
	require.Zero(t, count)
	input.Instances = entries
	id, err := repo.CreateCredentialPrincipal(ctx, owner, "create", input)
	require.NoError(t, err)
	again, err := repo.CreateCredentialPrincipal(ctx, owner, "create", input)
	require.NoError(t, err)
	require.Equal(t, id, again)
	var versions, identities int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*),count(DISTINCT identity_generation) FROM credential_instances WHERE principal_id=$1`, id).Scan(&versions, &identities))
	require.Equal(t, 3, versions)
	require.Equal(t, 3, identities)
	reader := NewUpstreamPrincipalReader(integrationDB)
	view, err := reader.GetPrincipal(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, "OFF", view.RoutingMode)
	require.Len(t, view.Instances, 3)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT count(*) FROM accounts a JOIN credential_instances i ON i.account_id=a.id WHERE i.principal_id=$1 AND (a.credentials<>'{}'::jsonb OR a.schedulable)`, id).Scan(&count))
	require.Zero(t, count)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET schedulable=true WHERE id=$1`, view.Instances[0].AccountID)
	require.Error(t, err, "AT-40 old admin write must fail")
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials='{"access_token":"bypass"}' WHERE id=$1`, view.Instances[0].AccountID)
	require.Error(t, err)
}
func TestCredentialImportUnverifiedCannotActivate(t *testing.T) {
	ctx := context.Background()
	repo := &credentialImportRepository{db: integrationDB}
	nonce := uuid.NewString()
	var owner int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture-only') RETURNING id`, nonce+"@example.test").Scan(&owner))
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	importer := service.NewCredentialImportService(repo, vault, nil)
	v, err := importer.Import(ctx, owner, "unverified", service.CredentialSecret{AccessToken: nonce})
	require.NoError(t, err)
	require.Equal(t, "UNVERIFIED", v.State)
	_, err = repo.CreateCredentialPrincipal(ctx, owner, "create", service.CreateCredentialPrincipalInput{Name: "test", TotalConcurrency: 1, Instances: []service.CreateCredentialInstanceInput{{ImportID: v.ID, Name: "test", Weight: 1}}})
	require.Error(t, err)
}
