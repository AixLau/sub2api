package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialPrincipalTurnsGlobalBoundAndCleanup(t *testing.T) {
	var registry credentialPrincipalTurns
	var releases []func()
	for principal := int64(1); principal <= maxCredentialAdmissionCalls; principal++ {
		release, err := registry.acquire(context.Background(), principal, false)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	_, err := registry.acquire(context.Background(), maxCredentialAdmissionCalls+1, false)
	require.ErrorIs(t, err, service.ErrAdmissionLocalQueueFull, "principal isolation must not multiply the local queue budget")
	for _, release := range releases {
		release()
	}
	require.Zero(t, registry.active)
	require.Empty(t, registry.turns, "inactive principals must not accumulate in the registry")
}

func TestCredentialPrincipalTurnsCancellationDoesNotRetainEntry(t *testing.T) {
	var registry credentialPrincipalTurns
	release, err := registry.acquire(context.Background(), 1, false)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := registry.acquire(ctx, 1, false); done <- err }()
	require.Eventually(t, func() bool {
		registry.mu.Lock()
		defer registry.mu.Unlock()
		return registry.active == 2
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	registry.mu.Lock()
	active := registry.active
	registry.mu.Unlock()
	require.Equal(t, 1, active)
	otherRelease, err := registry.acquire(context.Background(), 2, false)
	require.NoError(t, err)
	otherRelease()
	release()
	require.Zero(t, registry.active)
	require.Empty(t, registry.turns)
}

func TestCredentialPrincipalTurnsCancelledCallsDoNotConsumeBound(t *testing.T) {
	var registry credentialPrincipalTurns
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var releases []func()
	for principal := int64(1); principal <= maxCredentialAdmissionCalls; principal++ {
		release, err := registry.acquire(ctx, principal, false)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	// Model cancellation being delivered before the owning goroutines can run
	// their release defers. Reclaim capacity without deleting their queue state.
	cancel()
	release, err := registry.acquire(context.Background(), maxCredentialAdmissionCalls+1, false)
	require.NoError(t, err)
	require.Equal(t, 1, registry.active)
	for _, oldRelease := range releases {
		oldRelease()
	}
	require.Equal(t, 1, registry.active)
	release()
	require.Zero(t, registry.active)
	require.Empty(t, registry.turns)
	require.Empty(t, registry.registrations)
}
