//go:build integration

package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialQueueThreeProcessesPausedOwner(t *testing.T) {
	ctx := context.Background()
	f := newAdmissionFixture(t, 1)
	s := NewPrincipalAdmissionStore(integrationDB)
	held, err := s.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	barrier := make(chan struct{})
	var ready, active, starts, over atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" {
			ready.Add(1)
			<-barrier
			w.WriteHeader(200)
			return
		}
		if active.Add(1) > 1 {
			over.Add(1)
		}
		defer active.Add(-1)
		starts.Add(1)
		time.Sleep(30 * time.Millisecond)
		fmt.Fprint(w, "completed")
	}))
	defer upstream.Close()
	exe, err := os.Executable()
	require.NoError(t, err)
	type child struct {
		cmd     *exec.Cmd
		scanner *bufio.Scanner
		in      service.AdmissionInput
	}
	children := make([]child, 3)
	for i := range children {
		in := f.input()
		in.OriginalSession = fmt.Sprintf("process-session-%d", i)
		data, _ := json.Marshal(credentialWorkerInput{Admission: in, Upstream: upstream.URL, Barrier: upstream.URL + "/ready", Wait: true})
		cmd := exec.Command(exe, "-test.run=^$")
		cmd.Env = append(os.Environ(), "SUB2API_CREDENTIAL_TEST_WORKER_DSN="+integrationDSN)
		cmd.Stdin = strings.NewReader(string(data))
		pipe, err := cmd.StdoutPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())
		children[i] = child{cmd, bufio.NewScanner(pipe), in}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
	}
	require.Eventually(t, func() bool { return ready.Load() == 3 }, 5*time.Second, time.Millisecond)
	close(barrier)
	for _, c := range children {
		require.True(t, c.scanner.Scan())
		var out credentialWorkerOutput
		require.NoError(t, json.Unmarshal(c.scanner.Bytes(), &out))
		require.Equal(t, service.AdmissionWait, out.Code)
	}
	var oldest string
	require.NoError(t, integrationDB.QueryRow(`SELECT request_id FROM admission_tickets WHERE principal_id=$1 ORDER BY created_at,id LIMIT 1`, f.principal).Scan(&oldest))
	paused := -1
	for i, c := range children {
		if c.in.RequestID == oldest {
			paused = i
		}
	}
	require.NotEqual(t, -1, paused)
	require.NoError(t, children[paused].cmd.Process.Signal(syscall.SIGSTOP))
	defer children[paused].cmd.Process.Signal(syscall.SIGCONT)
	started := time.Now()
	require.NoError(t, s.Cancel(ctx, held.Snapshot.Lease))
	require.Eventually(t, func() bool { return starts.Load() == 2 }, 5*time.Second, 5*time.Millisecond, "two live owners must progress while the earlier owner is stopped")
	t.Logf("two_live_owners_after_release=%s", time.Since(started))
	require.NoError(t, children[paused].cmd.Process.Signal(syscall.SIGCONT))
	leases := map[string]bool{}
	for _, c := range children {
		require.True(t, c.scanner.Scan())
		var out credentialWorkerOutput
		require.NoError(t, json.Unmarshal(c.scanner.Bytes(), &out))
		require.Equal(t, service.AdmissionAdmitted, out.Code)
		require.False(t, leases[out.Lease.ID])
		leases[out.Lease.ID] = true
		require.Equal(t, c.in.RequestID, out.Lease.RequestID)
		require.NoError(t, c.cmd.Wait())
	}
	require.Equal(t, int64(3), starts.Load())
	require.Zero(t, over.Load())
	assertAdmissionLedger(t, f, 0)
	var bindings int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM session_bindings WHERE principal_id=$1`, f.principal).Scan(&bindings))
	require.Equal(t, 3, bindings)
}
