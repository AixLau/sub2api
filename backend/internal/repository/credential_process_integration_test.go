//go:build integration

package repository

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type credentialWorkerInput struct {
	Admission service.AdmissionInput
	Upstream  string
	Barrier   string
	Wait      bool
}
type credentialWorkerOutput struct {
	Code  service.AdmissionDecisionCode
	Lease service.LeaseRef
	Error string
}

// TestMain invokes this before starting containers. Each child has its own Go
// runtime and SQL pool, while sharing only the authoritative test PostgreSQL.
func runCredentialAdmissionTestWorker() bool {
	dsn := os.Getenv("SUB2API_CREDENTIAL_TEST_WORKER_DSN")
	if dsn == "" {
		return false
	}
	var in credentialWorkerInput
	if json.NewDecoder(os.Stdin).Decode(&in) != nil {
		os.Exit(2)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		os.Exit(3)
	}
	defer db.Close()
	store := NewPrincipalAdmissionStore(db)
	if in.Barrier != "" {
		response, err := http.Get(in.Barrier)
		if err != nil {
			os.Exit(6)
		}
		response.Body.Close()
	}
	decision, err := store.TryAdmit(context.Background(), in.Admission)
	output := credentialWorkerOutput{Code: decision.Code}
	if err != nil {
		output.Error = "admission error"
	}
	if decision.Snapshot != nil {
		output.Lease = decision.Snapshot.Lease
	}
	if err = json.NewEncoder(os.Stdout).Encode(output); err != nil {
		os.Exit(4)
	}
	if in.Wait && decision.Code == service.AdmissionWait {
		waitCtx, end := context.WithTimeout(context.Background(), 20*time.Second)
		defer end()
		for decision.Code == service.AdmissionWait {
			if store.WaitAdmission(waitCtx, in.Admission) != nil {
				os.Exit(7)
			}
			decision, err = store.TryAdmit(waitCtx, in.Admission)
			if err != nil {
				os.Exit(8)
			}
		}
		output = credentialWorkerOutput{Code: decision.Code}
		if decision.Snapshot != nil {
			output.Lease = decision.Snapshot.Lease
		}
		_ = json.NewEncoder(os.Stdout).Encode(output)
	}
	if decision.Code != service.AdmissionAdmitted {
		return true
	}
	if err = store.BeginDispatch(context.Background(), decision.Snapshot.Lease); err != nil {
		os.Exit(5)
	}
	response, err := http.Get(in.Upstream)
	if err == nil {
		response.Body.Close()
		_ = store.Finish(context.Background(), service.FinishAdmissionInput{Lease: decision.Snapshot.Lease, Complete: true, Outcome: "COMPLETED"})
	} else {
		_ = store.Finish(context.Background(), service.FinishAdmissionInput{Lease: decision.Snapshot.Lease})
	}
	return true
}
func TestCredentialThreeProcessesSIGKILLUnknownAT08AT26AT34(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	var starts, ends atomic.Int64
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		starts.Add(1)
		<-release
		ends.Add(1)
		fmt.Fprint(w, "completed")
	}))
	defer upstream.Close()
	defer close(release)
	exe, err := os.Executable()
	require.NoError(t, err)
	var commands []*exec.Cmd
	var outputs []credentialWorkerOutput
	for range 3 {
		payload, _ := json.Marshal(credentialWorkerInput{Admission: f.input(), Upstream: upstream.URL})
		cmd := exec.Command(exe, "-test.run=^$")
		cmd.Env = append(os.Environ(), "SUB2API_CREDENTIAL_TEST_WORKER_DSN="+integrationDSN)
		cmd.Stdin = strings.NewReader(string(payload))
		pipe, err := cmd.StdoutPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())
		commands = append(commands, cmd)
		scanner := bufio.NewScanner(pipe)
		require.True(t, scanner.Scan())
		var output credentialWorkerOutput
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &output))
		require.Empty(t, output.Error)
		outputs = append(outputs, output)
	}
	winner := -1
	var lease service.LeaseRef
	for i, output := range outputs {
		if output.Code == service.AdmissionAdmitted {
			require.Equal(t, -1, winner)
			winner = i
			lease = output.Lease
		} else {
			require.Equal(t, service.AdmissionWait, output.Code)
			require.NoError(t, commands[i].Wait())
		}
	}
	require.GreaterOrEqual(t, winner, 0)
	require.Eventually(t, func() bool { return starts.Load() == 1 }, 2*time.Second, time.Millisecond)
	require.NoError(t, commands[winner].Process.Kill())
	require.Error(t, commands[winner].Wait())
	require.Zero(t, ends.Load())
	assertAdmissionLedger(t, f, 1)
	_, err = integrationDB.Exec(`UPDATE request_leases SET heartbeat_at=CURRENT_TIMESTAMP-INTERVAL '40 seconds' WHERE id=$1`, lease.ID)
	require.NoError(t, err)
	_, err = (&credentialOperations{db: integrationDB}).ReconcileCredentialLeases(context.Background())
	require.NoError(t, err)
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM request_leases WHERE id=$1`, lease.ID).Scan(&state))
	require.Equal(t, "ORPHANED", state)
	assertAdmissionLedger(t, f, 1)
	denied, err := NewPrincipalAdmissionStore(integrationDB).TryAdmit(context.Background(), f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, denied.Code)
	require.Equal(t, int64(1), starts.Load())
}
