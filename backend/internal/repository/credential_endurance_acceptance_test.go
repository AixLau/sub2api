//go:build integration

package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Six sequential 10-minute scenarios. The actual HTTP mock records every start
// and end. Three independent admission clients share the real PostgreSQL ledger.
// This is an admission/transport endurance test, not the full-handler E2E test.
func TestCredentialAcceptanceEndurance(t *testing.T) {
	value := os.Getenv("SUB2API_CREDENTIAL_ENDURANCE")
	if value == "" {
		t.Skip("opt in with SUB2API_CREDENTIAL_ENDURANCE=10m")
	}
	duration, err := time.ParseDuration(value)
	require.NoError(t, err)
	for _, capacity := range []int{10, 50, 200} {
		for _, instances := range []int{3, 16} {
			if requested := os.Getenv("SUB2API_CREDENTIAL_MATRIX_CASE"); requested != "" && requested != fmt.Sprintf("C%d_I%d", capacity, instances) {
				continue
			}
			t.Run(fmt.Sprintf("C%d_I%d", capacity, instances), func(t *testing.T) {
				f := newAdmissionFixture(t, capacity)
				extendAdmissionFixture(t, &f, instances)
				var active, peak, starts, ends, duplicates, over atomic.Int64
				seen := sync.Map{}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if _, loaded := seen.LoadOrStore(r.Header.Get("X-Test-Lease"), true); loaded {
						duplicates.Add(1)
					}
					count := active.Add(1)
					for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
					}
					if count > int64(capacity) {
						over.Add(1)
					}
					n := starts.Add(1)
					defer func() { active.Add(-1); ends.Add(1) }()
					// deterministic short/long requests and a 5-second long tail.
					delay := 200 * time.Millisecond
					if n%5 == 0 {
						delay = 2 * time.Second
					}
					if n%17 == 0 {
						delay = 5 * time.Second
					}
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return
					}
					w.WriteHeader(200)
					io.WriteString(w, "completed")
				}))
				defer upstream.Close()
				// Clients use independent pools to model connection ownership of three nodes.
				stores := make([]*principalAdmissionStore, 3)
				for i := range stores {
					db, err := openSQLWithRetry(context.Background(), integrationDSN, 5*time.Second)
					require.NoError(t, err)
					db.SetMaxOpenConns(8)
					defer db.Close()
					stores[i] = &principalAdmissionStore{db: db}
				}
				until := time.Now().Add(duration)
				var wg sync.WaitGroup
				var mu sync.Mutex
				var timings, dispatchTimes, finishTimes []time.Duration
				var failures []string
				var waits atomic.Int64
				workerCount := capacity + 3
				for worker := range workerCount {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						store := stores[worker%3]
						recordErr := func(stage string, err error) {
							mu.Lock()
							failures = append(failures, stage+": "+err.Error())
							mu.Unlock()
						}
						for time.Now().Before(until) {
							in := f.input()
							in.Node = "endurance-" + strconv.Itoa(worker%3)
							in.Deadline = time.Now().Add(2 * time.Minute)
							var d service.AdmissionDecision
							var err error
							for {
								started := time.Now()
								d, err = store.TryAdmit(context.Background(), in)
								elapsed := time.Since(started)
								mu.Lock()
								timings = append(timings, elapsed)
								mu.Unlock()
								if err != nil {
									recordErr("admit", err)
									return
								}
								if d.Code != service.AdmissionWait {
									break
								}
								waits.Add(1)
								if time.Now().After(until) || time.Now().After(in.Deadline) {
									if err = store.CancelQueued(context.Background(), in); err != nil {
										recordErr("cancel_queue", err)
									}
									return
								}
								time.Sleep(100 * time.Millisecond)
							}
							if d.Code != service.AdmissionAdmitted {
								waits.Add(1)
								if d.Reason != "ADMISSION_QUEUE_TIMEOUT" && d.Reason != "ADMISSION_QUEUE_FULL" {
									recordErr("unexpected_rejection", fmt.Errorf("%s", d.Reason))
									return
								}
								continue
							}
							var started time.Time
							var elapsed time.Duration
							ref := d.Snapshot.Lease
							started = time.Now()
							err = store.BeginDispatch(context.Background(), ref)
							elapsed = time.Since(started)
							mu.Lock()
							dispatchTimes = append(dispatchTimes, elapsed)
							mu.Unlock()
							if err != nil {
								var debugState string
								_ = integrationDB.QueryRow(`SELECT state FROM request_leases WHERE id=$1`, ref.ID).Scan(&debugState)
								recordErr("dispatch "+ref.ID+" state="+debugState, err)
								_ = store.Cancel(context.Background(), ref)
								return
							}
							req, _ := http.NewRequest("POST", upstream.URL, nil)
							req.Header.Set("X-Test-Lease", ref.ID)
							resp, err := http.DefaultClient.Do(req)
							complete := false
							if err == nil {
								_, err = io.Copy(io.Discard, resp.Body)
								resp.Body.Close()
								complete = err == nil
							}
							started = time.Now()
							finishErr := store.Finish(context.Background(), service.FinishAdmissionInput{Lease: ref, Complete: complete, Outcome: "COMPLETED"})
							elapsed = time.Since(started)
							mu.Lock()
							finishTimes = append(finishTimes, elapsed)
							mu.Unlock()
							if err != nil {
								recordErr("http", err)
								return
							}
							if finishErr != nil {
								recordErr("finish", finishErr)
								return
							}
							// On each minute boundary all workers form a synchronized request burst.
							if time.Now().Unix()%60 == 0 {
								time.Sleep(100 * time.Millisecond)
							}
						}
					}(worker)
				}
				wg.Wait()
				require.Zero(t, over.Load())
				require.Zero(t, duplicates.Load())
				require.Equal(t, starts.Load(), ends.Load())
				assertAdmissionLedger(t, f, 0)
				percentile := func(v []time.Duration) time.Duration {
					if len(v) == 0 {
						return 0
					}
					sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
					return v[len(v)*95/100]
				}
				t.Logf("RESULT duration=%s C=%d instances=%d workers=%d admitted=%d denied=%d peak_mock=%d duplicates=%d over=%d admission_p95=%s dispatch_p95=%s finish_p95=%s", duration, capacity, instances, workerCount, starts.Load(), waits.Load(), peak.Load(), duplicates.Load(), over.Load(), percentile(timings), percentile(dispatchTimes), percentile(finishTimes))
				require.Empty(t, failures)
			})
		}
	}
}
