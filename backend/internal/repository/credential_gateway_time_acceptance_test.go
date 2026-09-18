//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type timeBoundaryGatewayStore struct {
	service.PrincipalAdmissionStore
	barrier  chan service.LeaseRef
	proceed  chan struct{}
	deadline time.Time
}

func (s *timeBoundaryGatewayStore) TryAdmit(ctx context.Context, in service.AdmissionInput) (service.AdmissionDecision, error) {
	in.Deadline = s.deadline
	return s.PrincipalAdmissionStore.TryAdmit(ctx, in)
}

func (s *timeBoundaryGatewayStore) BeginDispatch(ctx context.Context, ref service.LeaseRef) error {
	s.barrier <- ref
	<-s.proceed
	return s.PrincipalAdmissionStore.BeginDispatch(ctx, ref)
}

func TestCredentialGatewayTimeUnsentCompensation(t *testing.T) {
	for _, cancelClient := range []bool{false, true} {
		name := "deadline"
		if cancelClient {
			name = "client_cancel"
		}
		t.Run(name, func(t *testing.T) {
			f := newAdmissionFixture(t, 1)
			prepareAcceptanceIdentity(t, f, f.user)
			var sends atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); acceptanceTerminal(w) }))
			defer upstream.Close()
			store := &timeBoundaryGatewayStore{PrincipalAdmissionStore: NewPrincipalAdmissionStore(integrationDB), barrier: make(chan service.LeaseRef, 1), proceed: make(chan struct{}), deadline: admissionDatabaseClock(t).Add(2 * time.Second)}
			server := acceptanceGateway(t, &acceptanceUpstream{url: upstream.URL}, store)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			key := acceptanceKey(t, f.key)
			go func() { _, _, err := acceptanceRequest(ctx, server.URL+"/v1/responses", key, name); done <- err }()
			var ref service.LeaseRef
			select {
			case ref = <-store.barrier:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not reach dispatch")
			}
			tx, pid := admissionTimeBarrier(t, f.principal)
			close(store.proceed)
			waitAdmissionBlocked(t, pid)
			if cancelClient {
				cancel()
			} else {
				waitAdmissionDeadline(t, store.deadline)
			}
			require.NoError(t, tx.Commit())
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not finish")
			}
			require.Eventually(t, func() bool {
				var state, outcome string
				err := integrationDB.QueryRow(`SELECT state,COALESCE(outcome,'') FROM request_leases WHERE id=$1`, ref.ID).Scan(&state, &outcome)
				return err == nil && state == "RELEASED" && outcome == "NOT_SENT"
			}, 5*time.Second, 10*time.Millisecond)
			require.Zero(t, sends.Load())
			assertAdmissionLedger(t, f, 0)
		})
	}
}
