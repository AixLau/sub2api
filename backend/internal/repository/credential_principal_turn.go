package repository

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Retain the original node-wide bound: one running admission and at most 256
// other calls. Splitting the scheduling scope must not multiply this bound by
// the number of principals or retain entries after their callers have left.
const maxCredentialAdmissionCalls = 257

type credentialPrincipalTurns struct {
	mu            sync.Mutex
	active        int
	turns         map[int64]*credentialPrincipalTurn
	registrations map[*credentialPrincipalRegistration]struct{}
}

type credentialPrincipalRegistration struct {
	ctx     context.Context
	counted bool
}

type credentialPrincipalTurn struct {
	queue credentialAdmissionTurn
	refs  int
}

func (r *credentialPrincipalTurns) acquire(ctx context.Context, principal int64, priority bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.active >= maxCredentialAdmissionCalls {
		// Cancellation can precede the waiting goroutine's cleanup. Remove
		// its capacity claim immediately, but retain its principal entry until
		// that goroutine leaves so two local turns cannot coexist for one ID.
		for registration := range r.registrations {
			if registration.counted && registration.ctx.Err() != nil {
				registration.counted = false
				r.active--
			}
		}
	}
	if r.active >= maxCredentialAdmissionCalls {
		r.mu.Unlock()
		return nil, service.ErrAdmissionLocalQueueFull
	}
	if r.turns == nil {
		r.turns = make(map[int64]*credentialPrincipalTurn)
		r.registrations = make(map[*credentialPrincipalRegistration]struct{})
	}
	turn := r.turns[principal]
	if turn == nil {
		turn = &credentialPrincipalTurn{}
		r.turns[principal] = turn
	}
	r.active++
	turn.refs++
	registration := &credentialPrincipalRegistration{ctx: ctx, counted: true}
	r.registrations[registration] = struct{}{}
	r.mu.Unlock()
	leave := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if registration.counted {
			r.active--
		}
		delete(r.registrations, registration)
		turn.refs--
		if turn.refs == 0 {
			delete(r.turns, principal)
		}
	}
	if err := turn.queue.acquire(ctx, priority); err != nil {
		leave()
		return nil, err
	}
	return func() { turn.queue.release(); leave() }, nil
}

// The caller retains its principal turn, queue age and priority while another
// node holds that principal's advisory lock. Each failed nonblocking attempt
// releases the node turn AND transaction before waiting, so another principal
// can use the unchanged single-transaction node budget. No execution capacity
// is reserved here and no ticketless public WAIT is returned.
func (s *principalAdmissionStore) beginAdmissionTurn(ctx context.Context, principal int64, priority bool) (*sql.Tx, func(), error) {
	for {
		if err := s.admissionGate.acquire(ctx, priority); err != nil {
			return nil, nil, err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			s.admissionGate.release()
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, nil, service.ErrAdmissionStoreUnavailable
		}
		var turn bool
		err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, -principal).Scan(&turn)
		if err == nil && turn {
			return tx, s.admissionGate.release, nil
		}
		_ = tx.Rollback()
		s.admissionGate.release()
		if err != nil {
			return nil, nil, err
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}
