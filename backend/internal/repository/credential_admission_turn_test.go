package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCredentialAdmissionTurnOffersPrecedeFreshAndCancel(t *testing.T) {
	q := &credentialAdmissionTurn{}
	ctx := context.Background()
	require.NoError(t, q.acquire(ctx, false))
	fresh := make(chan error, 1)
	go func() { fresh <- q.acquire(ctx, false) }()
	require.Eventually(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.fresh) == 1 }, time.Second, time.Millisecond)
	offered := make(chan error, 1)
	go func() { offered <- q.acquire(ctx, true) }()
	require.Eventually(t, func() bool { q.mu.Lock(); defer q.mu.Unlock(); return len(q.offered) == 1 }, time.Second, time.Millisecond)
	q.release()
	require.NoError(t, <-offered)
	select {
	case <-fresh:
		t.Fatal("fresh registration bypassed an offered owner")
	default:
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.ErrorIs(t, q.acquire(cancelled, true), context.Canceled)
	q.release()
	require.NoError(t, <-fresh)
	q.release()
	require.NoError(t, q.acquire(ctx, false))
	q.release()
}
