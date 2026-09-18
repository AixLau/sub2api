package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type legacyCredentialRegistry struct {
	known map[string]bool
	err   error
	seen  []string
}

func (s *legacyCredentialRegistry) KnownCredentialTokenFingerprints(_ context.Context, values []string) (bool, error) {
	s.seen = append([]string(nil), values...)
	if s.err != nil {
		return false, s.err
	}
	if len(values) == 0 {
		return len(s.known) > 0, nil
	}
	for _, v := range values {
		if s.known[v] {
			return true, nil
		}
	}
	return false, nil
}
func TestCredentialLegacyKnownTokenGuard(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	v, err := NewCredentialVault(cfg.Gateway.CredentialVaultKey)
	require.NoError(t, err)
	registry := &legacyCredentialRegistry{known: map[string]bool{v.Fingerprint("token", "known-refresh"): true, v.Fingerprint("token", "historical-access"): true}}
	guard := NewCredentialLegacyTokenGuard(registry, cfg)
	require.ErrorIs(t, guard.Check(context.Background(), "known-refresh"), ErrCredentialLegacyBypass)
	require.NotContains(t, strings.Join(registry.seen, ","), "known-refresh")
	require.ErrorIs(t, guard.Check(context.Background(), " known-refresh "), ErrCredentialLegacyBypass)
	registry.known[v.Fingerprint("token", " whitespace-preserved ")] = true
	require.ErrorIs(t, guard.Check(context.Background(), " whitespace-preserved "), ErrCredentialLegacyBypass)
	require.ErrorIs(t, guard.CheckCredentials(context.Background(), map[string]any{"access_token": "historical-access"}), ErrCredentialLegacyBypass)
	require.NoError(t, guard.Check(context.Background(), "unknown-token"), "no registered equality match is not proof of independent provider authorization")
	require.ErrorIs(t, NewCredentialLegacyTokenGuard(registry, &config.Config{}).Check(context.Background(), "any-token"), ErrCredentialLegacyBypass)
	registry.err = errors.New("database error with opaque-secret-canary")
	require.Equal(t, "CREDENTIAL_VAULT_UNAVAILABLE", guard.Check(context.Background(), "known-refresh").Error())
	require.ErrorIs(t, NewCredentialLegacyTokenGuard(nil, cfg).Check(context.Background(), "raw"), ErrCredentialVaultUnavailable)
}
func TestCredentialLegacyAdminAndRawRefreshRejectBeforeSideEffects(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CredentialVaultKey = strings.Repeat("ab", 32)
	v, err := NewCredentialVault(cfg.Gateway.CredentialVaultKey)
	require.NoError(t, err)
	registry := &legacyCredentialRegistry{known: map[string]bool{v.Fingerprint("token", "known-refresh"): true}}
	guard := NewCredentialLegacyTokenGuard(registry, cfg)
	// nil repositories/client panic if a path gets beyond the guard.
	admin := &adminServiceImpl{credentialTokenGuard: guard}
	_, err = admin.CreateAccount(context.Background(), &CreateAccountInput{Credentials: map[string]any{"refresh_token": "known-refresh"}})
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	_, err = admin.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{"refresh_token": "known-refresh"}})
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	_, err = admin.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Credentials: map[string]any{"refresh_token": "known-refresh"}})
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	oauth := NewOpenAIOAuthService(nil, nil)
	defer oauth.Stop()
	oauth.credentialTokenGuard = guard
	_, err = oauth.RefreshToken(context.Background(), "known-refresh", "")
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	_, err = oauth.RefreshTokenWithClientID(context.Background(), "known-refresh", "", "client")
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
	_, err = oauth.RefreshAccountToken(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"refresh_token": "known-refresh"}})
	require.ErrorIs(t, err, ErrCredentialLegacyBypass)
}
