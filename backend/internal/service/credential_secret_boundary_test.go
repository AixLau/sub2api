package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestCredentialVaultRotationPreservesFingerprintKey(t *testing.T) {
	old, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	next, err := NewCredentialVaultWithFingerprintKey(strings.Repeat("cd", 32), old.FingerprintKeyHex())
	require.NoError(t, err)
	require.NotEqual(t, old.EncryptionKeyID(), next.EncryptionKeyID())
	require.Equal(t, old.FingerprintKeyID(), next.FingerprintKeyID())
	for _, kind := range []string{"token", "subject", "family", "import-operation", "import-payload"} {
		require.Equal(t, old.Fingerprint(kind, "existing-value"), next.Fingerprint(kind, "existing-value"))
	}
	secret := CredentialSecret{AccessToken: "canary-access-opaque", RefreshToken: "canary-refresh-opaque"}
	cipher, err := old.Seal("stable-aad", secret)
	require.NoError(t, err)
	plain, err := old.OpenData("stable-aad", cipher)
	require.NoError(t, err)
	defer clear(plain)
	rewrapped, err := next.SealData("stable-aad", plain)
	require.NoError(t, err)
	result, err := next.Open("stable-aad", rewrapped)
	require.NoError(t, err)
	require.Equal(t, secret, result)
	_, err = old.Open("stable-aad", rewrapped)
	require.Error(t, err)
	_, err = NewCredentialVaultWithFingerprintKey(strings.Repeat("cd", 32), "invalid")
	require.ErrorIs(t, err, ErrCredentialVaultUnavailable)
}
func TestCredentialSecretDiagnosticFormatting(t *testing.T) {
	secret := CredentialSecret{AccessToken: "opaque-access-canary", RefreshToken: "opaque-refresh-canary", AccountSubject: "opaque-subject-canary"}
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	logger.Info("APM-style structured event", "secret", secret, "vault", v)
	zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&logs), zap.DebugLevel)).Debug("debug hook", zap.Any("secret", secret), zap.Any("vault", v))
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		logs.WriteString(fmt.Sprintf(format, secret))
		logs.WriteString(fmt.Sprintf(format, v))
	}
	for _, canary := range []string{secret.AccessToken, secret.RefreshToken, secret.AccountSubject, strings.Repeat("ab", 32), v.FingerprintKeyHex()} {
		require.NotContains(t, logs.String(), canary)
	}
}
func TestCredentialImportPanicCannotReachDiagnostics(t *testing.T) {
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	store := &credentialImportMemory{}
	verifier := credentialVerifierFunc(func(context.Context, CredentialSecret) (VerifiedCredential, error) {
		panic("opaque-access-canary in provider panic")
	})
	svc := NewCredentialImportService(store, v, verifier)
	view, err := svc.Import(context.Background(), 1, "op", CredentialSecret{AccessToken: "opaque-access-canary"})
	require.ErrorIs(t, err, ErrCredentialUnverified)
	require.Empty(t, view.ID)
	require.Empty(t, store.record.ID)
}

type panicCredentialRefreshProvider struct{ calls int }

func (p *panicCredentialRefreshProvider) Refresh(context.Context, CredentialRefreshOperation, CredentialSecret) (CredentialSecret, time.Time, error) {
	p.calls++
	panic("opaque-refresh-canary in HTTP dump")
}

type panicCredentialRefreshStore struct {
	refreshCoordinatorStoreMock
	marked bool
}

func (s *panicCredentialRefreshStore) BeginCredentialRefresh(ctx context.Context, id int64) (CredentialRefreshOperation, error) {
	if s.marked {
		return CredentialRefreshOperation{}, fmt.Errorf("CREDENTIAL_REFRESH_UNAVAILABLE")
	}
	return s.refreshCoordinatorStoreMock.BeginCredentialRefresh(ctx, id)
}
func (s *panicCredentialRefreshStore) MarkCredentialRefreshUnknown(context.Context, CredentialRefreshOperation, *CredentialRefreshResult) error {
	s.marked = true
	return nil
}
func TestCredentialRefreshPanicPreservesUnknownWithoutReplay(t *testing.T) {
	v, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	ciphertext, err := v.Seal("old", CredentialSecret{AccessToken: "opaque-access-canary", RefreshToken: "opaque-refresh-canary"})
	require.NoError(t, err)
	store := &panicCredentialRefreshStore{refreshCoordinatorStoreMock: refreshCoordinatorStoreMock{op: CredentialRefreshOperation{ID: "op", SecretAAD: "old", Ciphertext: ciphertext}}}
	provider := &panicCredentialRefreshProvider{}
	coordinator := NewCredentialRefreshCoordinator(store, v, provider)
	require.ErrorContains(t, coordinator.Refresh(context.Background(), 1), "REFRESH_RESULT_UNKNOWN")
	require.True(t, store.marked)
	require.ErrorContains(t, coordinator.Refresh(context.Background(), 1), "CREDENTIAL_REFRESH_UNAVAILABLE")
	require.Equal(t, 1, provider.calls)
}

type panickingCredentialCommitStore struct{ refreshCoordinatorStoreMock }

func (s *panickingCredentialCommitStore) CompleteCredentialRefresh(context.Context, CredentialRefreshOperation, CredentialRefreshResult) error {
	panic("opaque-new-token in commit diagnostic")
}
func TestCredentialRefreshCommitPanicKeepsEncryptedNewToken(t *testing.T) {
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	cipher, err := vault.Seal("old", CredentialSecret{AccessToken: "old-access", RefreshToken: "old-refresh"})
	require.NoError(t, err)
	store := &panickingCredentialCommitStore{refreshCoordinatorStoreMock: refreshCoordinatorStoreMock{op: CredentialRefreshOperation{ID: "operation", SecretAAD: "old", Ciphertext: cipher}}}
	provider := &refreshProviderMock{release: make(chan struct{})}
	close(provider.release)
	require.ErrorContains(t, NewCredentialRefreshCoordinator(store, vault, provider).Refresh(context.Background(), 1), "REFRESH_RESULT_UNKNOWN")
	require.NotNil(t, store.unknown)
	result, err := vault.Open(store.unknown.AAD, store.unknown.Ciphertext)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh", result.RefreshToken)
	require.EqualValues(t, 1, provider.calls.Load())
}

type panicCredentialCompensationStore struct{ refreshCoordinatorStoreMock }

func (*panicCredentialCompensationStore) MarkCredentialRefreshUnknown(context.Context, CredentialRefreshOperation, *CredentialRefreshResult) error {
	panic("opaque-compensation-token")
}
func TestCredentialRefreshCompensationPanicCannotEscape(t *testing.T) {
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	cipher, err := vault.Seal("old", CredentialSecret{AccessToken: "old-access", RefreshToken: "old-refresh"})
	require.NoError(t, err)
	store := &panicCredentialCompensationStore{refreshCoordinatorStoreMock{op: CredentialRefreshOperation{ID: "operation", SecretAAD: "old", Ciphertext: cipher}}}
	provider := &panicCredentialRefreshProvider{}
	require.ErrorContains(t, NewCredentialRefreshCoordinator(store, vault, provider).Refresh(context.Background(), 1), "REFRESH_RESULT_UNKNOWN")
	require.Equal(t, 1, provider.calls)
}

type panicCredentialBeginStore struct{ CredentialRefreshStore }

func (panicCredentialBeginStore) BeginCredentialRefresh(context.Context, int64) (CredentialRefreshOperation, error) {
	panic("opaque-begin-token")
}
func TestCredentialRefreshBeginPanicCannotEscape(t *testing.T) {
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	provider := &panicCredentialRefreshProvider{}
	require.ErrorContains(t, NewCredentialRefreshCoordinator(panicCredentialBeginStore{}, vault, provider).Refresh(context.Background(), 1), "REFRESH_RESULT_UNKNOWN")
	require.Zero(t, provider.calls)
}
