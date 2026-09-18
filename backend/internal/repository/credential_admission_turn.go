package repository

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// A bounded local database-access queue, not an execution-capacity authority.
// Durable offer owners and the queue pump go before fresh registrations so a
// new burst cannot consume the owners' entire response window before they run.
// At most one admission/advance transaction leaves this queue per node.
type credentialAdmissionTurn struct {
	mu             sync.Mutex
	busy           bool
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
	if !q.busy {
		q.busy = true
		q.mu.Unlock()
		return nil
	}
	if len(q.offered)+len(q.fresh) >= 256 {
		q.mu.Unlock()
		return service.ErrAdmissionStoreUnavailable
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
		return nil
	case <-ctx.Done():
		q.mu.Lock()
		w.cancelled = true
		if w.granted {
			q.releaseLocked()
		}
		q.mu.Unlock()
		return ctx.Err()
	}
}
func (q *credentialAdmissionTurn) release() { q.mu.Lock(); defer q.mu.Unlock(); q.releaseLocked() }
func (q *credentialAdmissionTurn) releaseLocked() {
	for len(q.offered) > 0 || len(q.fresh) > 0 {
		queue := &q.offered
		if len(*queue) == 0 {
			queue = &q.fresh
		}
		w := (*queue)[0]
		(*queue)[0] = nil
		*queue = (*queue)[1:]
		if w.cancelled || w.ctx.Err() != nil {
			continue
		}
		w.granted = true
		close(w.ready)
		return
	}
	q.busy = false
}

// Contention before registration stays in the bounded local access queue. It
// must not return a public WAIT with no durable ticket (which would lose queue
// age and its 15s TTL). Release the connection between nonblocking attempts.
func (s *principalAdmissionStore) beginAdmissionTurn(ctx context.Context, principal int64) (*sql.Tx, error) {
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, service.ErrAdmissionStoreUnavailable
		}
		var turn bool
		err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, -principal).Scan(&turn)
		if err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if turn {
			return tx, nil
		}
		_ = tx.Rollback()
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
