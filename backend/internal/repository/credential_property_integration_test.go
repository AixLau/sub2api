//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
)

// Deterministic randomized transitions observe ledger conservation after every
// operation, including shrink overhang and unresolved remote execution.
func TestCredentialLedgerRandomTransitions(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	rng := rand.New(rand.NewSource(9427))
	live := map[string]service.LeaseRef{}
	sent := map[string]bool{}
	for range 150 {
		action := rng.Intn(5)
		if action == 0 {
			_, err := integrationDB.Exec(`UPDATE upstream_principals SET requested_limit=$2,config_version=config_version+1 WHERE id=$1`, f.principal, rng.Intn(12))
			require.NoError(t, err)
		} else if action <= 2 || len(live) == 0 {
			d, err := store.TryAdmit(ctx, f.input())
			require.NoError(t, err)
			if d.Code == service.AdmissionAdmitted {
				live[d.Snapshot.Lease.ID] = d.Snapshot.Lease
			}
		} else {
			for id, ref := range live {
				if !sent[id] && action == 3 {
					require.NoError(t, store.BeginDispatch(ctx, ref))
					sent[id] = true
					require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: ref}))
				} else if !sent[id] {
					require.NoError(t, store.Cancel(ctx, ref))
					delete(live, id)
				} else {
					require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: ref, Complete: true, Outcome: "COMPLETED"}))
					delete(live, id)
					delete(sent, id)
				}
				break
			}
		}
		assertAdmissionLedger(t, f, len(live))
	}
}
