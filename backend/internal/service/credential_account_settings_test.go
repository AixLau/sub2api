//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type credentialSettingsRepoStub struct {
	updateAccountCredsRepoStub
	settingsCalls int
	input         *UpdateAccountInput
}

func (r *credentialSettingsRepoStub) UpdateCredentialAccountSettings(_ context.Context, account *Account, input *UpdateAccountInput) error {
	r.settingsCalls++
	r.account = account
	r.input = input
	return nil
}

func TestCredentialAccountEditUsesControlledSettingsAndPreservesIdentity(t *testing.T) {
	repo := &credentialSettingsRepoStub{updateAccountCredsRepoStub: updateAccountCredsRepoStub{account: &Account{
		ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: "inactive",
		Credentials: map[string]any{"account_id": "provider-identity", "model_mapping": map[string]any{"old": "model"}},
		Extra:       map[string]any{"codex_5h_used_percent": float64(71), "openai_passthrough": true},
	}}}
	zero := 0
	input := &UpdateAccountInput{
		CredentialEdit: &CredentialAccountEdit{ActorID: 1, PrincipalID: 2, ConfigVersion: 3},
		Name:           "edited account", Concurrency: &zero,
		Credentials: map[string]any{}, Extra: map[string]any{"codex_5h_used_percent": float64(12)},
	}
	updated, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 7, input)
	require.NoError(t, err)
	require.Equal(t, 1, repo.settingsCalls)
	require.Zero(t, repo.updateCalls, "controlled edits must never use legacy account writes")
	require.Equal(t, 0, updated.Concurrency, "zero is a valid controlled capacity")
	require.Equal(t, "provider-identity", updated.Credentials["account_id"])
	require.NotContains(t, updated.Credentials, "model_mapping")
	require.NotContains(t, updated.Extra, "openai_passthrough")
	require.Equal(t, float64(71), updated.Extra["codex_5h_used_percent"])
}

func TestCredentialAccountEditRejectsAuthenticationAndIdentityWrites(t *testing.T) {
	for _, credentials := range []map[string]any{
		{"access_token": "unknown-token"},
		{"refresh_token": ""},
		{"account_id": "different-owner"},
		{"installation_id": "new-installation"},
	} {
		t.Run(firstCredentialKey(credentials), func(t *testing.T) {
			repo := &credentialSettingsRepoStub{updateAccountCredsRepoStub: updateAccountCredsRepoStub{account: &Account{
				ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"account_id": "original"},
			}}}
			_, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 7, &UpdateAccountInput{
				CredentialEdit: &CredentialAccountEdit{ActorID: 1, PrincipalID: 2, ConfigVersion: 3}, Credentials: credentials,
			})
			require.Equal(t, "CONTROLLED_CREDENTIAL_IDENTITY_IMMUTABLE", infraerrors.Reason(err))
			require.Zero(t, repo.settingsCalls)
			require.Zero(t, repo.updateCalls)
		})
	}
}

func firstCredentialKey(values map[string]any) string {
	for key := range values {
		return key
	}
	return ""
}
