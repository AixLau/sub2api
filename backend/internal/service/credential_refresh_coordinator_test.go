package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type refreshCoordinatorStoreMock struct {
	op          CredentialRefreshOperation
	completeErr error
	mu          sync.Mutex
	unknown     *CredentialRefreshResult
	begins      int
}

func (s *refreshCoordinatorStoreMock) BeginCredentialRefresh(context.Context, int64) (CredentialRefreshOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begins++
	return s.op, nil
}
func (s *refreshCoordinatorStoreMock) CompleteCredentialRefresh(context.Context, CredentialRefreshOperation, CredentialRefreshResult) error {
	return s.completeErr
}
func (s *refreshCoordinatorStoreMock) MarkCredentialRefreshUnknown(_ context.Context, _ CredentialRefreshOperation, r *CredentialRefreshResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unknown = r
	return nil
}

type refreshProviderMock struct {
	calls   atomic.Int64
	release chan struct{}
}

func (p *refreshProviderMock) Refresh(_ context.Context, _ CredentialRefreshOperation, s CredentialSecret) (CredentialSecret, time.Time, error) {
	p.calls.Add(1)
	<-p.release
	return CredentialSecret{AccessToken: "rotated-access", RefreshToken: "rotated-refresh"}, time.Now().Add(time.Hour), nil
}
func TestCredentialRefreshRemoteSuccessLocalFailureKeepsEncryptedResult(t *testing.T) {
	vault, err := NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	ciphertext, err := vault.Seal("old", CredentialSecret{AccessToken: "old-access", RefreshToken: "old-refresh", AccountSubject: "account", UserSubject: "user"})
	require.NoError(t, err)
	store := &refreshCoordinatorStoreMock{op: CredentialRefreshOperation{ID: "op", SecretAAD: "old", Ciphertext: ciphertext}, completeErr: errors.New("commit failed")}
	provider := &refreshProviderMock{release: make(chan struct{})}
	coordinator := NewCredentialRefreshCoordinator(store, vault, provider)
	done := make(chan error, 1)
	go func() { done <- coordinator.Refresh(context.Background(), 1) }()
	require.Eventually(t, func() bool { return provider.calls.Load() == 1 }, time.Second, time.Millisecond)
	close(provider.release)
	require.ErrorContains(t, <-done, "REFRESH_RESULT_UNKNOWN")
	require.NotNil(t, store.unknown)
	recovered, err := vault.Open(store.unknown.AAD, store.unknown.Ciphertext)
	require.NoError(t, err)
	require.Equal(t, "rotated-refresh", recovered.RefreshToken)
	require.Equal(t, "account", recovered.AccountSubject)
	require.NotContains(t, string(store.unknown.Ciphertext), "rotated")
}
