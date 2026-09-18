package repository

import (
	"context"
	"slices"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// A bounded local database-access queue, not an execution-capacity authority.
// Durable offer owners and queue pumps have bounded priority: at most one
// offer batch can pass the oldest waiting fresh registration. Each class is FIFO. The
// bound is in completed access turns, not wall time or execution capacity.
type credentialAdmissionTurn struct {
	mu             sync.Mutex
	busy           bool
	offeredRun     int
	offered, fresh []*credentialTurnWaiter
}

type credentialTurnWaiter struct {
	ctx                context.Context
	ready              chan struct{}
	granted, cancelled bool
}

func (q *credentialAdmissionTurn) acquire(ctx context.Context, offered bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	if err := ctx.Err(); err != nil {
		q.mu.Unlock()
		return err
	}
	if !q.busy {
		q.busy = true
		q.offeredRun = 0
		if offered {
			q.offeredRun = 1
		}
		q.mu.Unlock()
		return nil
	}
	if len(q.offered)+len(q.fresh) >= 256 {
		// A cancelled caller may not yet have run its cancellation branch.
		// Queue capacity counts live waiters, even in that scheduling window.
		q.removeCancelledLocked()
	}
	if len(q.offered)+len(q.fresh) >= 256 {
		q.mu.Unlock()
		return service.ErrAdmissionLocalQueueFull
	}
	w := &credentialTurnWaiter{ctx: ctx, ready: make(chan struct{})}
	if offered {
		q.offered = append(q.offered, w)
	} else {
		q.fresh = append(q.fresh, w)
	}
	q.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		w.cancelled = true
		if w.granted {
			// Ownership has transferred, but acquire has not returned it to
			// the caller. Pass it on exactly once under the same mutex.
			q.releaseLocked()
		} else {
			q.removeCancelledLocked()
		}
		return err
	}
	return nil
}
func (q *credentialAdmissionTurn) release() { q.mu.Lock(); defer q.mu.Unlock(); q.releaseLocked() }
func (q *credentialAdmissionTurn) releaseLocked() {
	for len(q.offered) > 0 || len(q.fresh) > 0 {
		queue := &q.offered
		offered := len(q.offered) > 0 && (len(q.fresh) == 0 || q.offeredRun < credentialOfferBatch)
		if !offered {
			queue = &q.fresh
		}
		w := (*queue)[0]
		(*queue)[0] = nil
		*queue = (*queue)[1:]
		if w.cancelled || w.ctx.Err() != nil {
			continue
		}
		if offered {
			q.offeredRun = min(q.offeredRun+1, credentialOfferBatch)
		} else {
			q.offeredRun = 0
		}
		w.granted = true
		close(w.ready)
		return
	}
	q.busy = false
	q.offeredRun = 0
}

func (q *credentialAdmissionTurn) removeCancelledLocked() {
	cancelled := func(w *credentialTurnWaiter) bool { return w.cancelled || w.ctx.Err() != nil }
	q.offered = slices.DeleteFunc(q.offered, cancelled)
	q.fresh = slices.DeleteFunc(q.fresh, cancelled)
}
