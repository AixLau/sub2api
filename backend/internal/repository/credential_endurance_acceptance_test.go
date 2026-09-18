//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type enduranceDistribution struct {
	Count  int     `json:"count"`
	MeanMS float64 `json:"mean_ms"`
	P50MS  float64 `json:"p50_ms"`
	P95MS  float64 `json:"p95_ms"`
	MaxMS  float64 `json:"max_ms"`
}

func enduranceSummary(v []time.Duration) enduranceDistribution {
	if len(v) == 0 {
		return enduranceDistribution{}
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	var sum time.Duration
	for _, d := range v {
		sum += d
	}
	return enduranceDistribution{len(v), float64(sum) / float64(len(v)) / 1e6, float64(v[len(v)*50/100]) / 1e6, float64(v[(len(v)-1)*95/100]) / 1e6, float64(v[len(v)-1]) / 1e6}
}

type enduranceMeasurements struct {
	mu        sync.Mutex
	durations map[string][]time.Duration
	sqlCalls  map[string]int
	counts    map[string]int
	failures  []string
}

func (m *enduranceMeasurements) duration(key string, d time.Duration) {
	m.mu.Lock()
	m.durations[key] = append(m.durations[key], d)
	m.mu.Unlock()
}
func (m *enduranceMeasurements) call(operation, result string, trace *admissionTrace) {
	elapsed := time.Since(trace.started)
	trace.mu.Lock()
	defer trace.mu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := operation + "." + result
	m.durations[prefix+".call"] = append(m.durations[prefix+".call"], elapsed)
	if trace.txStarted.IsZero() {
		phase := "connection_acquire_failed"
		if operation == "admit" {
			// Admission can time out in a local queue before asking sql.DB
			// for any connection. Do not mislabel that as pool exhaustion.
			phase = "pre_transaction_wait_failed"
		}
		m.durations[prefix+"."+phase] = append(m.durations[prefix+"."+phase], elapsed)
	}
	rounds := 0
	for phase, d := range trace.values {
		if operation == "admit" && phase == "connection_acquire" {
			phase = "pre_transaction_wait"
		}
		m.durations[prefix+"."+phase] = append(m.durations[prefix+"."+phase], d)
		if strings.HasPrefix(phase, "sql.") {
			rounds += trace.counts[phase]
		}
	}
	m.sqlCalls[prefix] += rounds
	for name, count := range trace.counts {
		if !strings.HasPrefix(name, "sql.") {
			m.counts[prefix+"."+name] += count
		}
	}
}
func (m *enduranceMeasurements) failure(stage string, err error) {
	m.mu.Lock()
	m.failures = append(m.failures, stage+": "+err.Error())
	m.mu.Unlock()
}

// Sequential scenarios, three independent 8-connection pools and one primary.
// This exercises the admission lifecycle + HTTP mock, not three OS gateway
// processes or the full handler/billing path (covered by separate tests).
func TestCredentialAcceptanceEndurance(t *testing.T) {
	value := os.Getenv("SUB2API_CREDENTIAL_ENDURANCE")
	if value == "" {
		t.Skip("opt in with SUB2API_CREDENTIAL_ENDURANCE=10m")
	}
	duration, err := time.ParseDuration(value)
	require.NoError(t, err)
	for _, capacity := range []int{10, 50, 200} {
		for _, instances := range []int{3, 16} {
			name := fmt.Sprintf("C%d_I%d", capacity, instances)
			if requested := os.Getenv("SUB2API_CREDENTIAL_MATRIX_CASE"); requested != "" && requested != name {
				continue
			}
			t.Run(name, func(t *testing.T) {
				f := newAdmissionFixture(t, capacity)
				extendAdmissionFixture(t, &f, instances)
				m := &enduranceMeasurements{durations: map[string][]time.Duration{}, sqlCalls: map[string]int{}, counts: map[string]int{}}
				var active, peak, starts, ends, duplicates, over, serviceNanos, timeouts, requests atomic.Int64
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
					started := time.Now()
					defer func() { serviceNanos.Add(int64(time.Since(started))); active.Add(-1); ends.Add(1) }()
					delay := 200 * time.Millisecond
					if n%5 == 0 {
						delay = 2 * time.Second
					}
					if n%17 == 0 {
						delay = 5 * time.Second
					}
					if n%51 == 0 {
						delay = 12 * time.Second
					}
					timer := time.NewTimer(delay)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return
					}
					w.WriteHeader(200)
					_, _ = io.WriteString(w, "completed")
				}))
				defer upstream.Close()
				stores := make([]*principalAdmissionStore, 3)
				totals := &admissionSQLCounts{}
				for i := range stores {
					parsed, err := url.Parse(integrationDSN)
					require.NoError(t, err)
					values := parsed.Query()
					values.Set("application_name", "admission_endurance")
					parsed.RawQuery = values.Encode()
					connector, err := pq.NewConnector(parsed.String())
					require.NoError(t, err)
					db := sql.OpenDB(admissionTraceConnector{Connector: connector, totals: totals})
					db.SetMaxOpenConns(6)
					db.SetMaxIdleConns(6)
					defer db.Close()
					require.NoError(t, db.Ping())
					critical := sql.OpenDB(admissionTraceConnector{Connector: connector, totals: totals})
					critical.SetMaxOpenConns(2)
					critical.SetMaxIdleConns(2)
					defer critical.Close()
					require.NoError(t, critical.Ping())
					stores[i] = &principalAdmissionStore{db: db, lifecycleDB: critical}
					// Opt-in test policy, never a product configuration.
					switch os.Getenv("SUB2API_CREDENTIAL_RETRY_EXPERIMENT") {
					case "", "fixed5":
					case "bounded":
						stores[i].advisoryRetryDelay = func(attempt int) time.Duration {
							ceiling := (5 * time.Millisecond) << min(attempt, 3)
							return ceiling/2 + time.Duration(rand.Int64N(int64(ceiling/2)+1))
						}
					default:
						t.Fatal("unknown retry experiment")
					}
				}
				var version string
				require.NoError(t, integrationDB.QueryRow(`SHOW server_version`).Scan(&version))
				t.Logf("ENV postgres=%s go=%s cpu=%d gomaxprocs=%d nodes=3 pools_per_node=6+2 total_max_open=24 workers=%d request_deadline=2m context_deadline=2m poll=100ms burst=capacity+3-at-minute-boundary", version, runtime.Version(), runtime.NumCPU(), runtime.GOMAXPROCS(0), capacity+3)
				// Sample actual database wait events. Counts are backend-samples, not exact
				// wait durations. SQL phase wall times must not be labelled pure lock waits.
				samplingCtx, stopSampling := context.WithCancel(context.Background())
				sampled := make(chan struct{})
				pgWait := map[string]int{}
				var idleOfferSamples, idleApplicationSamples, allSamples int64
				go func() {
					defer close(sampled)
					ticker := time.NewTicker(50 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-samplingCtx.Done():
							return
						case <-ticker.C:
							rows, err := integrationDB.QueryContext(samplingCtx, `SELECT query,COALESCE(wait_event_type,'CPU'),COALESCE(wait_event,''),count(*) FROM pg_stat_activity WHERE application_name='admission_endurance' AND state='active' GROUP BY 1,2,3`)
							if err != nil {
								if samplingCtx.Err() == nil {
									m.failure("pg_sample", err)
								}
								continue
							}
							for rows.Next() {
								var query, kind, event string
								var n int
								if err = rows.Scan(&query, &kind, &event, &n); err != nil {
									m.failure("pg_scan", err)
									break
								}
								pgWait[admissionSQLPhase(query)+"/"+kind+"/"+event] += n
							}
							rows.Close()
							var hasIdleOffer, hasCapacity bool
							sampleErr := integrationDB.QueryRowContext(samplingCtx, `SELECT EXISTS(SELECT 1 FROM upstream_principals p JOIN admission_tickets t ON t.principal_id=p.id JOIN credential_instances i ON i.id=t.offer_instance_id
 WHERE p.id=$1 AND p.occupied<p.requested_limit AND t.state='QUEUED' AND t.offer_until>clock_timestamp()
 AND i.occupied<LEAST(p.requested_limit,COALESCE(i.hard_max,p.requested_limit),COALESCE(i.health_capacity,p.requested_limit))), (SELECT occupied<requested_limit FROM upstream_principals WHERE id=$1)`, f.principal).Scan(&hasIdleOffer, &hasCapacity)
							if sampleErr == nil {
								allSamples++
								var pending int
								for _, store := range stores {
									store.principalTurns.mu.Lock()
									activeCalls := store.principalTurns.active
									store.principalTurns.mu.Unlock()
									store.admissionGate.mu.Lock()
									if store.admissionGate.busy && activeCalls > 0 {
										activeCalls--
									}
									store.admissionGate.mu.Unlock()
									pending += activeCalls
								}
								if hasCapacity && pending > 0 {
									idleApplicationSamples++
								}
								if hasIdleOffer {
									idleOfferSamples++
								}
							}
						}
					}
				}()
				begun := time.Now()
				until := begun.Add(duration)
				var wg sync.WaitGroup
				execute := func(worker int, arrival time.Time, burst bool) {
					requests.Add(1)
					if burst {
						m.duration("burst_arrival_jitter", time.Since(arrival))
					} else {
						m.duration("steady_arrival_jitter", time.Since(arrival))
					}
					store := stores[worker%3]
					in := f.input()
					in.Node = "endurance-" + strconv.Itoa(worker%3)
					in.Deadline = time.Now().Add(2 * time.Minute)
					ctx, cancel := context.WithDeadline(context.Background(), in.Deadline)
					defer cancel()
					requestStarted := time.Now()
					var firstWait time.Time
					for {
						callCtx, trace := newAdmissionTrace(ctx)
						d, err := store.TryAdmit(callCtx, in)
						result := string(d.Code)
						if err != nil {
							result = "ERROR"
						}
						m.call("admit", result, trace)
						if err != nil {
							m.failure("admit", err)
							return
						}
						if d.Code != service.AdmissionWait {
							if d.Code != service.AdmissionAdmitted {
								if d.Reason == "ADMISSION_QUEUE_TIMEOUT" {
									timeouts.Add(1)
								} else if d.Reason != "ADMISSION_QUEUE_FULL" {
									m.failure("rejection", fmt.Errorf("%s", d.Reason))
								}
								// Ticket cancellation is terminal, not a new execution.
								cleanup, end := context.WithTimeout(context.Background(), 30*time.Second)
								_ = store.CancelQueued(cleanup, in)
								end()
								return
							}
							ref := d.Snapshot.Lease
							m.duration("arrival_to_admit", time.Since(requestStarted))
							if !firstWait.IsZero() {
								m.duration("first_wait_to_admit", time.Since(firstWait))
							}
							dispatchCtx, dispatchTrace := newAdmissionTrace(ctx)
							err = store.BeginDispatch(dispatchCtx, ref)
							result = "OK"
							if err != nil {
								result = "ERROR"
							}
							m.call("dispatch", result, dispatchTrace)
							if err != nil {
								m.failure("dispatch", err)
								cleanup, end := context.WithTimeout(context.Background(), 30*time.Second)
								_ = store.Cancel(cleanup, ref)
								end()
								return
							}
							m.duration("arrival_to_dispatch", time.Since(requestStarted))
							// Match the executor's 10s renewal / 3s timeout. A small 12s tail
							// below makes real heartbeats observable without renewing every request.
							heartbeatDone := make(chan struct{})
							heartbeatStopped := make(chan struct{})
							go func() {
								defer close(heartbeatStopped)
								ticker := time.NewTicker(10 * time.Second)
								defer ticker.Stop()
								for {
									select {
									case <-heartbeatDone:
										return
									case <-ticker.C:
										hbCtx, hbEnd := context.WithTimeout(context.Background(), 3*time.Second)
										trCtx, tr := newAdmissionTrace(hbCtx)
										hbErr := store.Heartbeat(trCtx, ref)
										hbEnd()
										outcome := "OK"
										if hbErr != nil {
											outcome = "ERROR"
										}
										m.call("heartbeat", outcome, tr)
										if hbErr != nil {
											m.failure("heartbeat", hbErr)
											cancel()
											return
										}
									}
								}
							}()
							req, _ := http.NewRequestWithContext(ctx, "POST", upstream.URL, nil)
							req.Header.Set("X-Test-Lease", ref.ID)
							resp, httpErr := http.DefaultClient.Do(req)
							complete := false
							if httpErr == nil {
								_, httpErr = io.Copy(io.Discard, resp.Body)
								resp.Body.Close()
								complete = httpErr == nil
							}
							close(heartbeatDone)
							<-heartbeatStopped
							cleanup, end := context.WithTimeout(context.Background(), 5*time.Second)
							finishCtx, finishTrace := newAdmissionTrace(cleanup)
							finishErr := store.Finish(finishCtx, service.FinishAdmissionInput{Lease: ref, Complete: complete, Outcome: "COMPLETED"})
							end()
							result = "OK"
							if finishErr != nil {
								result = "ERROR"
							}
							m.call("finish", result, finishTrace)
							m.duration("arrival_to_finish", time.Since(requestStarted))
							if httpErr != nil {
								m.failure("http", httpErr)
							}
							if finishErr != nil {
								m.failure("finish", finishErr)
							}
							return
						}
						if firstWait.IsZero() {
							firstWait = time.Now()
						}
						if time.Now().After(until) || ctx.Err() != nil {
							cleanup, end := context.WithTimeout(context.Background(), 30*time.Second)
							cancelErr := store.CancelQueued(cleanup, in)
							end()
							if cancelErr != nil {
								m.failure("cancel_queue", cancelErr)
							}
							return
						}
						waitCtx, end := context.WithDeadline(ctx, until)
						err = store.WaitAdmission(waitCtx, in)
						end()
						if err != nil {
							cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
							_ = store.CancelQueued(cleanup, in)
							stop()
							return
						}

					}
				}
				// Initial arrival barrier, then closed-loop sustained load.
				start := make(chan struct{})
				for worker := range capacity + 3 {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						<-start
						for time.Now().Before(until) {
							execute(worker, time.Now(), false)
						}
					}(worker)
				}
				close(start)
				// Explicit scheduled bursts: a separate finite batch is released by one
				// barrier each minute, independent of existing workers finishing requests.
				for at := begun.Add(time.Minute); at.Before(until); at = at.Add(time.Minute) {
					timer := time.NewTimer(time.Until(at))
					<-timer.C
					gate := make(chan struct{})
					for worker := range capacity + 3 {
						wg.Add(1)
						go func(worker int, arrival time.Time) { defer wg.Done(); <-gate; execute(worker, arrival, true) }(worker, at)
					}
					close(gate)
					var waits int64
					for _, store := range stores {
						waits += store.db.Stats().WaitCount
					}
					t.Logf("BURST case=%s scheduled_s=%.0f released_s=%.3f batch=%d dispatches=%d active_mock=%d peak_mock=%d pool_waits=%d", name, at.Sub(begun).Seconds(), time.Since(begun).Seconds(), capacity+3, starts.Load(), active.Load(), peak.Load(), waits)
				}
				wg.Wait()
				elapsed := time.Since(begun)
				stopSampling()
				<-sampled
				summary := map[string]enduranceDistribution{}
				for _, outcome := range []string{"ADMITTED", "WAIT", "REJECTED", "ERROR"} {
					summary["admit."+outcome+".call"] = enduranceDistribution{}
				}
				for k, v := range m.durations {
					summary[k] = enduranceSummary(v)
				}
				poolStats := make([]map[string]any, 0, 3)
				for _, store := range stores {
					for _, pool := range []*sql.DB{store.db, store.criticalDB()} {
						v := pool.Stats()
						poolStats = append(poolStats, map[string]any{"max_open": v.MaxOpenConnections, "wait_count": v.WaitCount, "wait_ms": float64(v.WaitDuration) / 1e6, "open": v.OpenConnections, "idle": v.Idle})
					}
				}
				states := map[string]int{}
				rows, err := integrationDB.Query(`SELECT state,count(*) FROM request_leases WHERE principal_id=$1 GROUP BY state`, f.principal)
				require.NoError(t, err)
				for rows.Next() {
					var state string
					var count int
					require.NoError(t, rows.Scan(&state, &count))
					states[state] = count
				}
				require.NoError(t, rows.Err())
				rows.Close()
				totals.mu.Lock()
				allSQL, queueSQL := totals.calls, totals.queueCalls
				transactionStats := map[string]any{
					"attempts": totals.transactions, "commits": totals.commits, "rollbacks": totals.rollbacks, "errors": totals.transactionErrors,
					"try_lock_attempts": totals.tryLocks, "try_lock_acquired": totals.tryLockAcquired, "try_lock_misses": totals.tryLockMisses,
					"all_attempt_mean_wall_ms": float64(totals.txWall) / float64(max(totals.transactions, 1)) / 1e6,
					"background_sql_wall_ms":   float64(totals.backgroundSQLWall) / 1e6,
				}
				totals.mu.Unlock()
				report := map[string]any{"retry_experiment": os.Getenv("SUB2API_CREDENTIAL_RETRY_EXPERIMENT"), "idle_capacity_with_application_waiters_sample_seconds": float64(idleApplicationSamples) * 0.05, "idle_capacity_with_live_offer_samples": idleOfferSamples, "capacity_samples": allSamples, "idle_capacity_with_live_offer_sample_seconds": float64(idleOfferSamples) * 0.05, "all_store_sql_calls": allSQL, "queue_control_sql_calls": queueSQL, "queue_sql_per_dispatch": float64(queueSQL+int64(m.sqlCalls["admit.WAIT"])) / float64(max(starts.Load(), 1)), "final_lease_states": states, "case": name, "scheduled_seconds": duration.Seconds(), "elapsed_seconds": elapsed.Seconds(), "requests": requests.Load(), "dispatches": starts.Load(), "dispatch_per_second": float64(starts.Load()) / elapsed.Seconds(), "timeout_count": timeouts.Load(), "timeout_rate": float64(timeouts.Load()) / float64(requests.Load()), "peak_mock": peak.Load(), "mock_utilization": float64(serviceNanos.Load()) / float64(elapsed) / float64(capacity), "duplicates": duplicates.Load(), "over": over.Load(), "pool_stats": poolStats, "pg_wait_backend_samples_50ms": pgWait, "sql_calls": m.sqlCalls, "durations": summary, "errors": m.failures, "admitted_transaction_p95_target_20ms_met": summary["admit.ADMITTED.transaction"].P95MS <= 20}
				report["transaction_activity"] = transactionStats
				report["operation_counts"] = m.counts
				admitted := summary["admit.ADMITTED.call"].Count
				report["effective_admission_commits_per_second"] = float64(admitted) / elapsed.Seconds()
				// A failed call may already have made several advisory attempts.
				// Include ERROR/CONFIG_STALE/ALREADY_RUNNING as well as grants.
				admissionAttempts := 0
				for key, count := range m.counts {
					if strings.HasPrefix(key, "admit.") && (strings.HasSuffix(key, ".try_lock.acquired") || strings.HasSuffix(key, ".try_lock.missed")) {
						admissionAttempts += count
					}
				}
				report["successful_admissions_per_advisory_attempt"] = float64(admitted) / float64(max(admissionAttempts, 1))
				data, err := json.Marshal(report)
				require.NoError(t, err)
				t.Logf("MEASUREMENTS %s", data)
				// Retain query plans to show whether history/queue size changes access cost.
				for _, q := range []string{`SELECT count(*) FROM request_leases WHERE principal_id=$1 AND state<>'RELEASED'`, `SELECT count(*) FROM admission_tickets WHERE principal_id=$1 AND state='QUEUED' AND deadline>statement_timestamp()`} {
					var plan []byte
					require.NoError(t, integrationDB.QueryRow("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+q, f.principal).Scan(&plan))
					t.Logf("PLAN %s", plan)
				}
				require.Zero(t, over.Load())
				require.Zero(t, duplicates.Load())
				require.Equal(t, starts.Load(), ends.Load())
				assertAdmissionLedger(t, f, 0)
				require.Empty(t, m.failures)
			})
		}
	}
}
