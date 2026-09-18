package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type credentialAdmissionWaiter struct {
	in    service.AdmissionInput
	ready chan struct{}
}

// WaitAdmission coalesces all local waiting owners into one batched poll. The
// channel is a lossy hint: the offer persists in PostgreSQL, and the next poll
// recovers a lost wake. It never holds a connection/transaction/execution slot.
func (s *principalAdmissionStore) WaitAdmission(ctx context.Context, in service.AdmissionInput) error {
	s.initializeQueue()
	s.waitMu.Lock()
	w := s.waiters[in.RequestID]
	if w == nil {
		w = &credentialAdmissionWaiter{in: in, ready: make(chan struct{})}
		s.waiters[in.RequestID] = w
	}
	if !s.pumpRunning {
		s.pumpRunning = true
		go s.pumpAdmissionQueue()
	}
	s.waitMu.Unlock()
	defer func() {
		s.waitMu.Lock()
		if ctx.Err() != nil {
			delete(s.retryReady, in.RequestID)
		}
		if s.waiters[in.RequestID] == w {
			delete(s.waiters, in.RequestID)
		}
		s.waitMu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.ready:
		return nil
	}
}
func (s *principalAdmissionStore) pumpAdmissionQueue() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.waitMu.Lock()
		if len(s.waiters) == 0 {
			s.pumpRunning = false
			s.waitMu.Unlock()
			return
		}
		pending := make([]*credentialAdmissionWaiter, 0, len(s.waiters))
		for _, w := range s.waiters {
			pending = append(pending, w)
		}
		s.waitMu.Unlock()
		ctx, end := context.WithTimeout(context.Background(), time.Second)
		s.pollAdmissionQueue(ctx, pending)
		end()
		select {
		case <-ticker.C:
		case <-s.wake:
		}
	}
}
func (s *principalAdmissionStore) pollAdmissionQueue(ctx context.Context, pending []*credentialAdmissionWaiter) {
	// One new admission/queue-advance transaction per process, one per principal
	// across processes. No queued background transactions can flood row locks.
	if err := s.admissionGate.acquire(ctx, true); err != nil {
		return
	}
	defer s.admissionGate.release()
	principals := map[int64]bool{}
	var ids, nodes []string
	for _, w := range pending {
		principals[w.in.PrincipalID] = true
		ids = append(ids, w.in.RequestID)
		nodes = append(nodes, w.in.Node)
	}
	for principal := range principals {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		var turn bool
		err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1)`, -principal).Scan(&turn)
		if err != nil || !turn {
			_ = tx.Rollback()
			continue
		}
		var n int
		// SKIP LOCKED is appropriate only for hint generation. It is never used to
		// read partial authoritative capacity when approving an execution.
		err = tx.QueryRowContext(ctx, `SELECT occupied FROM upstream_principals WHERE id=$1 FOR NO KEY UPDATE SKIP LOCKED`, principal).Scan(&n)
		if err == nil {
			err = advanceCredentialOffers(ctx, tx, principal)
		}
		if err != nil {
			_ = tx.Rollback()
			continue
		}
		_ = tx.Commit()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT x.request,COALESCE(t.offer_until>clock_timestamp(),false) FROM unnest($1::uuid[],$2::text[]) AS x(request,node)
 LEFT JOIN logical_requests r ON r.id=x.request LEFT JOIN admission_tickets t ON t.request_id=x.request
 WHERE r.id IS NULL OR (r.owner_node=x.node AND (r.status<>'QUEUED' OR t.state<>'QUEUED' OR t.deadline<=clock_timestamp() OR t.offer_until>clock_timestamp()))`, pq.Array(ids), pq.Array(nodes))
	if err != nil {
		return
	}
	var ready []string
	for rows.Next() {
		var id string
		var offered bool
		if err = rows.Scan(&id, &offered); err != nil {
			break
		}
		s.waitMu.Lock()
		if s.waiters[id] != nil {
			s.retryReady[id] = offered
		}
		s.waitMu.Unlock()
		ready = append(ready, id)
	}
	if rows.Err() != nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return
	}
	s.deliverAdmissionHints(ready)
}
func (s *principalAdmissionStore) deliverAdmissionHints(ids []string) {
	s.waitMu.Lock()
	defer s.waitMu.Unlock()
	for _, id := range ids {
		if w := s.waiters[id]; w != nil {
			delete(s.waiters, id)
			close(w.ready)
		}
	}
}
