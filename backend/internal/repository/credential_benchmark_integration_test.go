//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Opt-in endurance test. Reported timings describe only this machine and DB
// topology; this is not a production capacity recommendation.
func TestCredentialAdmissionLoadMatrix(t *testing.T) {
	duration := os.Getenv("SUB2API_CREDENTIAL_LOAD_DURATION")
	if duration == "" {
		t.Skip("set SUB2API_CREDENTIAL_LOAD_DURATION=10m for the release matrix")
	}
	period, err := time.ParseDuration(duration)
	require.NoError(t, err)
	for _, capacity := range []int{10, 50, 200} {
		for _, instances := range []int{3, 16} {
			t.Run(fmt.Sprintf("C%d_I%d", capacity, instances), func(t *testing.T) {
				f := newAdmissionFixture(t, capacity)
				extendAdmissionFixture(t, &f, instances)
				deadline := time.Now().Add(period)
				var wg sync.WaitGroup
				var mu sync.Mutex
				var durations []time.Duration
				var errors []error
				for node := range 3 {
					wg.Add(1)
					go func(node int) {
						defer wg.Done()
						store := NewPrincipalAdmissionStore(integrationDB)
						for time.Now().Before(deadline) {
							in := f.input()
							in.Node = fmt.Sprint("node-", node)
							start := time.Now()
							d, err := store.TryAdmit(context.Background(), in)
							elapsed := time.Since(start)
							mu.Lock()
							durations = append(durations, elapsed)
							if err != nil {
								errors = append(errors, err)
							}
							mu.Unlock()
							if err != nil {
								return
							}
							if d.Code == service.AdmissionAdmitted {
								if err = store.BeginDispatch(context.Background(), d.Snapshot.Lease); err == nil {
									err = store.Finish(context.Background(), service.FinishAdmissionInput{Lease: d.Snapshot.Lease, Complete: true, Outcome: "COMPLETED"})
								}
								if err != nil {
									mu.Lock()
									errors = append(errors, err)
									mu.Unlock()
									return
								}
							}
						}
					}(node)
				}
				wg.Wait()
				require.Empty(t, errors)
				assertAdmissionLedger(t, f, 0)
				sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
				require.NotEmpty(t, durations)
				t.Logf("duration=%s transactions=%d admission_p95=%s topology=local-PostgreSQL18.4-3-workers", period, len(durations), durations[len(durations)*95/100])
			})
		}
	}
}
