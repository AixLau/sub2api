package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
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

// This is a bound in completed access turns, not a promise about wall time:
// the current transaction may still be blocked outside this local queue.
func TestCredentialAdmissionTurnFreshAdvancesDuringContinuousOffersAndPumps(t *testing.T) {
	q := &credentialAdmissionTurn{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, q.acquire(ctx, false))
	type grant struct {
		name string
		err  error
	}
	grants := make(chan grant, 64)
	enqueue := func(name string, offered bool, total int) {
		go func() { grants <- grant{name, q.acquire(ctx, offered)} }()
		require.Eventually(t, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.offered)+len(q.fresh) == total
		}, time.Second, time.Millisecond)
	}
	enqueue("fresh-1", false, 1)
	enqueue("fresh-2", false, 2)
	// Pump work and offered owners use the same priority class. Keep it
	// continuously nonempty by replacing each granted high-priority request.
	enqueue("owner-0", true, 3)
	enqueue("pump-0", true, 4)
	freshSeen, highSinceFresh := 0, 0
	for turn := 0; turn < 18; turn++ {
		q.release()
		g := <-grants
		require.NoError(t, g.err)
		if g.name == fmt.Sprintf("fresh-%d", freshSeen+1) {
			freshSeen++
			require.LessOrEqual(t, highSinceFresh, 8, "fresh must follow at most one high-priority batch")
			highSinceFresh = 0
			if freshSeen == 2 {
				break
			}
		} else {
			require.True(t, strings.HasPrefix(g.name, "owner-") || strings.HasPrefix(g.name, "pump-"))
			highSinceFresh++
			require.LessOrEqual(t, highSinceFresh, 8, "continuous offers/pumps must not starve a fresh registration")
			enqueue(fmt.Sprintf("pump-%d", turn+1), true, 4-freshSeen)
		}
	}
	require.Equal(t, 2, freshSeen)
	q.release()
}

func TestCredentialAdmissionTurnCancellationReclaimsQueueCapacity(t *testing.T) {
	q := &credentialAdmissionTurn{}
	require.NoError(t, q.acquire(context.Background(), false))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 256)
	for i := 0; i < 256; i++ {
		go func(i int) { results <- q.acquire(ctx, i%2 == 0) }(i)
	}
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return len(q.offered)+len(q.fresh) == 256
	}, time.Second, time.Millisecond)
	cancel()
	for i := 0; i < 256; i++ {
		require.ErrorIs(t, <-results, context.Canceled)
	}
	q.mu.Lock()
	remaining := len(q.offered) + len(q.fresh)
	q.mu.Unlock()
	require.Zero(t, remaining, "cancelled calls must stop occupying local queue entries before the current holder releases")
	newCtx, end := context.WithCancel(context.Background())
	defer end()
	newCall := make(chan error, 1)
	go func() { newCall <- q.acquire(newCtx, false) }()
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return len(q.fresh) == 1
	}, time.Second, time.Millisecond)
	q.release()
	require.NoError(t, <-newCall)
	q.release()
}

func TestCredentialAdmissionTurnFullQueueIsNotStorageFailure(t *testing.T) {
	q := &credentialAdmissionTurn{}
	require.NoError(t, q.acquire(context.Background(), false))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 256)
	for i := 0; i < 256; i++ {
		go func() { results <- q.acquire(ctx, false) }()
	}
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return len(q.fresh) == 256
	}, time.Second, time.Millisecond)
	err := q.acquire(context.Background(), false)
	require.ErrorIs(t, err, service.ErrAdmissionLocalQueueFull)
	require.NotErrorIs(t, err, service.ErrAdmissionStoreUnavailable)
	cancel()
	for i := 0; i < 256; i++ {
		require.ErrorIs(t, <-results, context.Canceled)
	}
	q.release()
}

func TestCredentialAdmissionTurnCancellationGrantRace(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		q := &credentialAdmissionTurn{}
		require.NoError(t, q.acquire(context.Background(), false))
		ctx, cancel := context.WithCancel(context.Background())
		first := make(chan error, 1)
		go func() { first <- q.acquire(ctx, true) }()
		require.Eventually(t, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.offered) == 1
		}, time.Second, time.Millisecond)
		follower := make(chan error, 1)
		go func() { follower <- q.acquire(context.Background(), false) }()
		require.Eventually(t, func() bool {
			q.mu.Lock()
			defer q.mu.Unlock()
			return len(q.fresh) == 1
		}, time.Second, time.Millisecond)
		q.mu.Lock()
		// Alternate cancellation before grant and cancellation while the grant
		// is published but its recipient cannot re-enter the queue mutex.
		if iteration%2 == 0 {
			cancel()
			q.releaseLocked()
		} else {
			q.releaseLocked()
			cancel()
		}
		q.mu.Unlock()
		if err := <-first; err == nil {
			// If acquire already returned successfully, its caller still owns
			// the turn and is responsible for releasing exactly once.
			select {
			case <-follower:
				t.Fatal("cancellation released a turn still owned by a successful caller")
			default:
			}
			q.release()
		} else {
			require.ErrorIs(t, err, context.Canceled)
		}
		require.NoError(t, <-follower)
		// A late cancellation must not release the follower's turn.
		q.mu.Lock()
		require.True(t, q.busy)
		require.Empty(t, q.offered)
		require.Empty(t, q.fresh)
		q.mu.Unlock()
		q.release()
		require.NoError(t, q.acquire(context.Background(), false))
		q.release()
	}
}
