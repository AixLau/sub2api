package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type credentialImportMemory struct{ record CredentialImportRecord }

func (m *credentialImportMemory) PutImport(_ context.Context, r CredentialImportRecord) (CredentialImportView, error) {
	m.record = r
	return CredentialImportView{ID: r.ID, State: r.State}, nil
}
func (m *credentialImportMemory) GetImport(context.Context, int64, int64, string) (*CredentialImportView, error) {
	return nil, nil
}

type credentialVerifierFunc func(context.Context, CredentialSecret) (VerifiedCredential, error)

func (f credentialVerifierFunc) Verify(c context.Context, s CredentialSecret) (VerifiedCredential, error) {
	return f(c, s)
}

func TestCredentialVaultAuthentication(t *testing.T) {
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	secret := CredentialSecret{AccessToken: "mock-access", RefreshToken: "mock-refresh"}
	sealed, err := v.Seal("import-id", secret)
	require.NoError(t, err)
	require.NotContains(t, string(sealed), secret.AccessToken)
	opened, err := v.Open("import-id", sealed)
	require.NoError(t, err)
	require.Equal(t, secret, opened)
	_, err = v.Open("other-record", sealed)
	require.Error(t, err)
	sealed[len(sealed)-1] ^= 1
	_, err = v.Open("import-id", sealed)
	require.Error(t, err)
	require.NotEqual(t, v.Fingerprint("token", "same"), v.Fingerprint("subject", "same"))
	_, err = NewCredentialVault("")
	require.ErrorIs(t, err, ErrCredentialVaultUnavailable)
}
func TestCredentialImportRequiresProviderEvidence(t *testing.T) {
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	store := &credentialImportMemory{}
	svc := NewCredentialImportService(store, v, nil)
	view, err := svc.Import(context.Background(), 1, "op", CredentialSecret{AccessToken: "unsigned-jwt"})
	require.NoError(t, err)
	require.Equal(t, "UNVERIFIED", view.State)
	require.Empty(t, store.record.SubjectKey)
	svc.verifier = credentialVerifierFunc(func(context.Context, CredentialSecret) (VerifiedCredential, error) {
		return VerifiedCredential{State: "VERIFIED", Provider: "openai_oauth", AccountSubject: "workspace", ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	_, err = svc.Import(context.Background(), 1, "op", CredentialSecret{AccessToken: "mock"})
	require.ErrorIs(t, err, ErrCredentialUnverified)
	svc.verifier = credentialVerifierFunc(func(context.Context, CredentialSecret) (VerifiedCredential, error) {
		return VerifiedCredential{}, errors.New("mock-secret-in-provider-error")
	})
	_, err = svc.Import(context.Background(), 1, "op", CredentialSecret{AccessToken: "mock"})
	require.Equal(t, "CREDENTIAL_UNVERIFIED", err.Error())
}
func TestCredentialImportScopesWorkspaceAndUser(t *testing.T) {
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	store := &credentialImportMemory{}
	user := "user-a"
	verifier := credentialVerifierFunc(func(context.Context, CredentialSecret) (VerifiedCredential, error) {
		return VerifiedCredential{State: "VERIFIED", Provider: "openai_oauth", AccountSubject: "workspace", UserSubject: user, ExpiresAt: time.Now().Add(time.Hour)}, nil
	})
	svc := NewCredentialImportService(store, v, verifier)
	_, err = svc.Import(context.Background(), 1, "a", CredentialSecret{AccessToken: "token-a"})
	require.NoError(t, err)
	first := store.record.SubjectKey
	user = "user-b"
	_, err = svc.Import(context.Background(), 1, "b", CredentialSecret{AccessToken: "token-b"})
	require.NoError(t, err)
	require.NotEqual(t, first, store.record.SubjectKey)
}
