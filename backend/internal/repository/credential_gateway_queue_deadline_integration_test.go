//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// The real handler and admission store still own their original deadlines and
// state transitions. Hooks only observe phases or delay an acknowledgement;
// they cannot register a ticket, approve a lease or send an upstream request.
type acceptanceQueueBudgetStore struct {
	*principalAdmissionStore
	entered, waiting, offered chan service.AdmissionInput
	admitted                  chan service.LeaseRef
	cancelled                 chan string
	holdOffer, holdAdmission  bool
	loseAdmissionAck          bool
	localQueueFull            bool
	localQueueFullOnRetry     bool
	tryCount                  atomic.Int64
}

func newAcceptanceQueueBudgetStore() *acceptanceQueueBudgetStore {
	return &acceptanceQueueBudgetStore{
		principalAdmissionStore: &principalAdmissionStore{db: integrationDB},
		entered:                 make(chan service.AdmissionInput, 32), waiting: make(chan service.AdmissionInput, 32),
		offered: make(chan service.AdmissionInput, 32), admitted: make(chan service.LeaseRef, 32), cancelled: make(chan string, 32),
	}
}

func (s *acceptanceQueueBudgetStore) TryAdmit(ctx context.Context, in service.AdmissionInput) (service.AdmissionDecision, error) {
	s.entered <- in
	attempt := s.tryCount.Add(1)
	if s.localQueueFull || (s.localQueueFullOnRetry && attempt > 1) {
		return service.AdmissionDecision{Code: service.AdmissionRejected, Reason: "ADMISSION_LOCAL_QUEUE_FULL"}, nil
	}
	d, err := s.principalAdmissionStore.TryAdmit(ctx, in)
	if err == nil && d.Code == service.AdmissionAdmitted {
		s.admitted <- d.Snapshot.Lease
		if s.holdAdmission {
			<-ctx.Done()
			if s.loseAdmissionAck {
				return service.AdmissionDecision{}, service.ErrAdmissionStoreUnavailable
			}
		}
	}
	return d, err
}

func (s *acceptanceQueueBudgetStore) WaitAdmission(ctx context.Context, in service.AdmissionInput) error {
	s.waiting <- in
	err := s.principalAdmissionStore.WaitAdmission(ctx, in)
	if err == nil {
		s.offered <- in
		if s.holdOffer {
			<-ctx.Done()
			return ctx.Err()
		}
	}
	return err
}

func (s *acceptanceQueueBudgetStore) CancelQueued(ctx context.Context, in service.AdmissionInput) error {
	err := s.principalAdmissionStore.CancelQueued(ctx, in)
	s.cancelled <- in.RequestID
	return err
}

func (s *acceptanceQueueBudgetStore) Cancel(ctx context.Context, ref service.LeaseRef) error {
	err := s.principalAdmissionStore.Cancel(ctx, ref)
	s.cancelled <- ref.RequestID
	return err
}

type acceptanceQueueHTTPResult struct {
	status  int
	body    string
	err     error
	elapsed time.Duration
}

func acceptanceQueueRequest(ctx context.Context, url, key, session string) <-chan acceptanceQueueHTTPResult {
	done := make(chan acceptanceQueueHTTPResult, 1)
	go func() {
		started := time.Now()
		status, body, err := acceptanceRequest(ctx, url, key, session)
		done <- acceptanceQueueHTTPResult{status, body, err, time.Since(started)}
	}()
	return done
}

func acceptanceQueueInput(t *testing.T, ch <-chan service.AdmissionInput) service.AdmissionInput {
	t.Helper()
	select {
	case in := <-ch:
		return in
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach the expected handler/admission phase")
		return service.AdmissionInput{}
	}
}

func assertAcceptanceQueueEmpty(t *testing.T, principal int64, exclude string) {
	t.Helper()
	var queued, leases int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED'`, principal).Scan(&queued))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM request_leases WHERE principal_id=$1 AND id::text<>$2 AND state<>'RELEASED'`, principal, exclude).Scan(&leases))
	require.Zero(t, queued)
	require.Zero(t, leases)
	var receipts, bills int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_usage_receipts c JOIN request_leases l ON c.lease_id=l.id WHERE l.principal_id=$1`, principal).Scan(&receipts))
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM credential_billing_outbox b JOIN request_leases l ON b.lease_id=l.id WHERE l.principal_id=$1`, principal).Scan(&bills))
	require.Zero(t, receipts, "unsent work has no upstream usage fact")
	require.Zero(t, bills, "unsent work must not be billed")
}

// This is a finite real HTTP burst. There are no durable waiting tickets while
// the principal's cross-node turn is held, and no client context shortens or
// extends the handler's actual 15-second admission budget.
func TestCredentialGatewayQueueBudgetUnregisteredBurst(t *testing.T) {
	f := newAdmissionFixture(t, 3)
	prepareAcceptanceIdentity(t, f, f.user)
	store := newAcceptanceQueueBudgetStore()
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	key := acceptanceKey(t, f.key)
	lock, err := integrationDB.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	defer lock.Rollback()
	_, err = lock.Exec(`SELECT pg_advisory_xact_lock($1)`, -f.principal)
	require.NoError(t, err)
	var results []<-chan acceptanceQueueHTTPResult
	for i := range 3 {
		results = append(results, acceptanceQueueRequest(context.Background(), server.URL+"/v1/responses", key, fmt.Sprintf("unregistered-%d", i)))
	}
	for range results {
		acceptanceQueueInput(t, store.entered)
	}
	assertAcceptanceQueueEmpty(t, f.principal, "")
	for _, done := range results {
		r := <-done
		require.NoError(t, r.err)
		require.Equal(t, http.StatusServiceUnavailable, r.status, r.body)
		require.Contains(t, r.body, "ADMISSION_QUEUE_TIMEOUT")
		require.GreaterOrEqual(t, r.elapsed, 15*time.Second)
		require.Less(t, r.elapsed, 19*time.Second)
		t.Logf("phase=unregistered result=queue_timeout elapsed=%s", r.elapsed)
	}
	require.Zero(t, starts.Load())
	require.Empty(t, store.waiting)
	require.Empty(t, store.admitted)
	assertAcceptanceQueueEmpty(t, f.principal, "")
	assertAdmissionLedger(t, f, 0)
}

// All three requests have durable tickets but no execution leases. The held
// reservation is fixture setup, not a request made or replayed by the handler.
func TestCredentialGatewayQueueBudgetRegisteredBurst(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	prepareAcceptanceIdentity(t, f, f.user)
	store := newAcceptanceQueueBudgetStore()
	held, err := store.principalAdmissionStore.TryAdmit(context.Background(), f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, held.Code)
	defer store.principalAdmissionStore.Cancel(context.Background(), held.Snapshot.Lease)
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	key := acceptanceKey(t, f.key)
	var results []<-chan acceptanceQueueHTTPResult
	for i := range 3 {
		results = append(results, acceptanceQueueRequest(context.Background(), server.URL+"/v1/responses", key, fmt.Sprintf("registered-%d", i)))
	}
	for range results {
		acceptanceQueueInput(t, store.waiting)
	}
	var queued int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED'`, f.principal).Scan(&queued))
	require.Equal(t, 3, queued)
	assertAdmissionLedger(t, f, 1)
	for _, done := range results {
		r := <-done
		require.NoError(t, r.err)
		require.Equal(t, http.StatusServiceUnavailable, r.status, r.body)
		require.Contains(t, r.body, "ADMISSION_QUEUE_TIMEOUT")
		require.GreaterOrEqual(t, r.elapsed, 15*time.Second)
		require.Less(t, r.elapsed, 19*time.Second)
		t.Logf("phase=registered result=queue_timeout elapsed=%s", r.elapsed)
	}
	require.Zero(t, starts.Load())
	require.Empty(t, store.admitted)
	assertAcceptanceQueueEmpty(t, f.principal, held.Snapshot.Lease.ID)
	require.NoError(t, store.principalAdmissionStore.Cancel(context.Background(), held.Snapshot.Lease))
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialGatewayQueueBudgetLateAdmissionAcknowledgement(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_ack=%t", lost), func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			prepareAcceptanceIdentity(t, f, f.user)
			store := newAcceptanceQueueBudgetStore()
			store.holdAdmission, store.loseAdmissionAck = true, lost
			var starts atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
			defer upstream.Close()
			server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
			done := acceptanceQueueRequest(context.Background(), server.URL+"/v1/responses", acceptanceKey(t, f.key), "late-admission")
			ref := <-store.admitted
			assertAdmissionLedger(t, f, 1)
			r := <-done
			require.NoError(t, r.err)
			require.Equal(t, http.StatusServiceUnavailable, r.status, r.body)
			require.Contains(t, r.body, "ADMISSION_QUEUE_TIMEOUT")
			require.GreaterOrEqual(t, r.elapsed, 15*time.Second)
			require.Less(t, r.elapsed, 19*time.Second)
			require.Zero(t, starts.Load(), "a late acknowledged or recovered reservation must not be sent")
			var state, outcome string
			require.NoError(t, integrationDB.QueryRow(`SELECT state,outcome FROM request_leases WHERE id=$1`, ref.ID).Scan(&state, &outcome))
			require.Equal(t, "RELEASED", state)
			require.Equal(t, "NOT_SENT", outcome)
			assertAcceptanceQueueEmpty(t, f.principal, "")
			assertAdmissionLedger(t, f, 0)
			t.Logf("phase=admitted lost_ack=%t result=queue_timeout_not_sent elapsed=%s", lost, r.elapsed)
		})
	}
}

func TestCredentialGatewayQueueBudgetCancellationPhases(t *testing.T) {
	for _, phase := range []string{"unregistered", "registered", "offered", "admitted", "admitted_lost_ack"} {
		t.Run(phase, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			prepareAcceptanceIdentity(t, f, f.user)
			store := newAcceptanceQueueBudgetStore()
			store.holdOffer = phase == "offered"
			store.holdAdmission = phase == "admitted" || phase == "admitted_lost_ack"
			store.loseAdmissionAck = phase == "admitted_lost_ack"
			var held *service.CredentialExecutionSnapshot
			if phase == "registered" || phase == "offered" {
				d, err := store.principalAdmissionStore.TryAdmit(context.Background(), f.input())
				require.NoError(t, err)
				require.Equal(t, service.AdmissionAdmitted, d.Code)
				held = d.Snapshot
				defer store.principalAdmissionStore.Cancel(context.Background(), held.Lease)
			}
			if phase == "unregistered" {
				lock, err := integrationDB.BeginTx(context.Background(), nil)
				require.NoError(t, err)
				defer lock.Rollback()
				_, err = lock.Exec(`SELECT pg_advisory_xact_lock($1)`, -f.principal)
				require.NoError(t, err)
			}
			var starts atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
			defer upstream.Close()
			server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := acceptanceQueueRequest(ctx, server.URL+"/v1/responses", acceptanceKey(t, f.key), "cancel-"+phase)
			in := acceptanceQueueInput(t, store.entered)
			switch phase {
			case "registered", "offered":
				acceptanceQueueInput(t, store.waiting)
				if phase == "offered" {
					require.NoError(t, store.principalAdmissionStore.Cancel(context.Background(), held.Lease))
					acceptanceQueueInput(t, store.offered)
				}
			case "admitted", "admitted_lost_ack":
				<-store.admitted
			}
			started := time.Now()
			cancel()
			r := <-done
			require.Error(t, r.err)
			require.True(t, errors.Is(r.err, context.Canceled), r.err)
			select {
			case request := <-store.cancelled:
				require.Equal(t, in.RequestID, request)
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not finish cancellation compensation")
			}
			var active int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM request_leases WHERE request_id=$1 AND state<>'RELEASED'`, in.RequestID).Scan(&active))
			require.Zero(t, active)
			var waiting int
			require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM admission_tickets WHERE request_id=$1 AND state='QUEUED'`, in.RequestID).Scan(&waiting))
			require.Zero(t, waiting)
			require.Zero(t, starts.Load())
			t.Logf("phase=%s result=client_cancelled compensation=%s upstream_starts=%d", phase, time.Since(started), starts.Load())
		})
	}
}

func TestCredentialGatewayQueueBudgetLocalOverloadReason(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	prepareAcceptanceIdentity(t, f, f.user)
	store := newAcceptanceQueueBudgetStore()
	store.localQueueFull = true
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	status, body, err := acceptanceRequest(context.Background(), server.URL+"/v1/responses", acceptanceKey(t, f.key), "local-overload")
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, status, body)
	require.Contains(t, body, "ADMISSION_LOCAL_QUEUE_FULL")
	require.NotContains(t, body, "ADMISSION_STORE_UNAVAILABLE")
	require.Zero(t, starts.Load())
	assertAcceptanceQueueEmpty(t, f.principal, "")
	assertAdmissionLedger(t, f, 0)
}

func TestCredentialGatewayQueueBudgetLocalOverloadAfterOffer(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	prepareAcceptanceIdentity(t, f, f.user)
	store := newAcceptanceQueueBudgetStore()
	store.localQueueFullOnRetry = true
	held, err := store.principalAdmissionStore.TryAdmit(context.Background(), f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, held.Code)
	defer store.principalAdmissionStore.Cancel(context.Background(), held.Snapshot.Lease)
	var starts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { starts.Add(1); acceptanceTerminal(w) }))
	defer upstream.Close()
	server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
	done := acceptanceQueueRequest(context.Background(), server.URL+"/v1/responses", acceptanceKey(t, f.key), "offered-local-overload")
	acceptanceQueueInput(t, store.waiting)
	require.NoError(t, store.principalAdmissionStore.Cancel(context.Background(), held.Snapshot.Lease))
	r := <-done
	require.NoError(t, r.err)
	require.Equal(t, http.StatusServiceUnavailable, r.status, r.body)
	require.Contains(t, r.body, "ADMISSION_LOCAL_QUEUE_FULL")
	require.NotContains(t, r.body, "ADMISSION_STORE_UNAVAILABLE")
	require.Equal(t, int64(2), store.tryCount.Load())
	require.Zero(t, starts.Load())
	assertAcceptanceQueueEmpty(t, f.principal, "")
	assertAdmissionLedger(t, f, 0)
}
