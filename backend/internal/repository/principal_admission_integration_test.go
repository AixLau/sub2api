//go:build integration

package repository

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type admissionFixture struct {
	principal, user, key int64
	instances            []int64
}

func newAdmissionFixture(t *testing.T, limit int) admissionFixture {
	t.Helper()
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	var f admissionFixture
	var group int64
	nonce := uuid.NewString()
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO users(email,password_hash) VALUES($1,'fixture') RETURNING id`, nonce+"@example.test").Scan(&f.user))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO groups(name,platform) VALUES($1,'openai') RETURNING id`, nonce).Scan(&group))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name,group_id) VALUES($1,$2,'fixture',$3) RETURNING id`, f.user, nonce, group).Scan(&f.key))
	require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO upstream_principals(name,provider,verified_subject_key,verification_state,requested_limit,admin_state,routing_mode)
 VALUES('fixture','openai_oauth',$1,'VERIFIED',$2,'ACTIVE','GROUPED') RETURNING id`, nonce, limit).Scan(&f.principal))
	for i := range 3 {
		var account, instance int64
		generation := uuid.NewString()
		require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,status,schedulable) VALUES('fixture','openai','oauth','inactive',false) RETURNING id`).Scan(&account))
		_, err = tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account, group)
		require.NoError(t, err)
		require.NoError(t, tx.QueryRowContext(ctx, `INSERT INTO credential_instances(principal_id,account_id,name,identity_generation,admin_state,credential_state,capabilities)
 VALUES($1,$2,$3,$4,'ACTIVE','VALID',ARRAY['responses','passthrough','compact','probe']) RETURNING id`, f.principal, account, fmt.Sprint(i), generation).Scan(&instance))
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_identity_profiles(instance_id,principal_id,generation,installation_id,source) VALUES($1,$2,$3,$4,'LOCAL_LOGICAL')`, instance, f.principal, generation, uuid.NewString())
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, `INSERT INTO credential_secrets(instance_id,credential_version,secret_ciphertext,secret_aad,expires_at) VALUES($1,1,'mock','fixture',CURRENT_TIMESTAMP+INTERVAL '1 hour')`, instance)
		require.NoError(t, err)
		f.instances = append(f.instances, instance)
	}
	require.NoError(t, tx.Commit())
	return f
}
func (f admissionFixture) input() service.AdmissionInput {
	return service.AdmissionInput{RequestID: uuid.NewString(), OwnerNonce: uuid.NewString(), PayloadDigest: "fixture-body", Node: uuid.NewString(), PrincipalID: f.principal, UserID: f.user, APIKeyID: f.key, CandidateIDs: f.instances, Endpoint: "responses", Model: "mock", Deadline: time.Now().Add(time.Minute)}
}
func assertAdmissionLedger(t *testing.T, f admissionFixture, want int) {
	t.Helper()
	var p, i, l, u int
	require.NoError(t, integrationDB.QueryRow(`SELECT p.occupied,(SELECT COALESCE(sum(occupied),0) FROM credential_instances WHERE principal_id=p.id),
 (SELECT count(*) FROM request_leases WHERE principal_id=p.id AND state<>'RELEASED'),
 (SELECT COALESCE(sum(occupied),0) FROM principal_user_capacity WHERE principal_id=p.id) FROM upstream_principals p WHERE p.id=$1`, f.principal).Scan(&p, &i, &l, &u))
	require.Equal(t, want, p)
	require.Equal(t, p, i)
	require.Equal(t, p, l)
	require.Equal(t, p, u)
}
func TestPrincipalAdmissionThreeNodesLastSlotAT08AT25(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	var began, ended atomic.Int64
	block := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		began.Add(1)
		<-block
		fmt.Fprint(w, `{"status":"completed"}`)
		ended.Add(1)
	}))
	defer upstream.Close()
	var wg sync.WaitGroup
	decisions := make(chan service.AdmissionDecision, 3)
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store := NewPrincipalAdmissionStore(integrationDB)
			d, e := store.TryAdmit(ctx, f.input())
			decisions <- d
			errs <- e
		}()
	}
	wg.Wait()
	close(decisions)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	store := NewPrincipalAdmissionStore(integrationDB)
	var winner *service.CredentialExecutionSnapshot
	waits := 0
	for d := range decisions {
		if d.Code == service.AdmissionAdmitted {
			require.Nil(t, winner)
			winner = d.Snapshot
		} else {
			require.Equal(t, service.AdmissionWait, d.Code)
			waits++
		}
	}
	require.NotNil(t, winner)
	require.Equal(t, 2, waits)
	assertAdmissionLedger(t, f, 1)
	require.NoError(t, store.BeginDispatch(ctx, winner.Lease))
	done := make(chan error, 1)
	go func() {
		resp, err := http.Get(upstream.URL)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	require.Eventually(t, func() bool { return began.Load() == 1 }, time.Second, time.Millisecond)
	assertAdmissionLedger(t, f, 1)
	require.Zero(t, ended.Load())
	close(block)
	require.NoError(t, <-done)
	require.Equal(t, int64(1), ended.Load())
	finish := service.FinishAdmissionInput{Lease: winner.Lease, Complete: true, Outcome: "COMPLETED"}
	require.NoError(t, store.Finish(ctx, finish))
	require.NoError(t, store.Finish(ctx, finish))
	assertAdmissionLedger(t, f, 0)
	stale := winner.Lease
	stale.OwnerNonce = uuid.NewString()
	require.ErrorIs(t, store.Cancel(ctx, stale), service.ErrAdmissionOwnership)
	assertAdmissionLedger(t, f, 0)
}
func TestPrincipalAdmissionBorrowShrinkUnknownAT09AT12AT26(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	_, err := integrationDB.Exec(`UPDATE credential_instances SET hard_max=8 WHERE principal_id=$1`, f.principal)
	require.NoError(t, err)
	var leases []service.LeaseRef
	for i := range 10 {
		in := f.input()
		if i < 8 {
			in.CandidateIDs = f.instances[:1]
		} else {
			in.CandidateIDs = f.instances[1:2]
		}
		d, err := store.TryAdmit(ctx, in)
		require.NoError(t, err)
		require.Equal(t, service.AdmissionAdmitted, d.Code)
		leases = append(leases, d.Snapshot.Lease)
	}
	assertAdmissionLedger(t, f, 10)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, d.Code)
	_, err = integrationDB.Exec(`UPDATE upstream_principals SET requested_limit=5,config_version=config_version+1 WHERE id=$1`, f.principal)
	require.NoError(t, err)
	d, err = store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, d.Code)
	assertAdmissionLedger(t, f, 10)
	require.NoError(t, store.BeginDispatch(ctx, leases[0]))
	require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: leases[0]}))
	assertAdmissionLedger(t, f, 10)
	var state string
	require.NoError(t, integrationDB.QueryRow(`SELECT state FROM request_leases WHERE id=$1`, leases[0].ID).Scan(&state))
	require.Equal(t, "ORPHANED", state)
	require.NoError(t, store.Cancel(ctx, leases[0]))
	assertAdmissionLedger(t, f, 10)
	for _, lease := range leases[1:] {
		require.NoError(t, store.Cancel(ctx, lease))
	}
	assertAdmissionLedger(t, f, 1)
}
func TestPrincipalAdmissionBindingAndReplayAT15AT16AT31(t *testing.T) {
	f := newAdmissionFixture(t, 10)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	in := f.input()
	in.OriginalSession = "same-session"
	in.IdempotencyKey = "same-key"
	d, err := store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	replay, err := store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAlreadyRunning, replay.Code)
	in.PayloadDigest = "different"
	conflict, err := store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "IDEMPOTENCY_PAYLOAD_MISMATCH", conflict.Reason)
	other := f.input()
	other.OriginalSession = "same-session"
	d2, err := store.TryAdmit(ctx, other)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d2.Code)
	require.Equal(t, d.Snapshot.Lease.InstanceID, d2.Snapshot.Lease.InstanceID)
	assertAdmissionLedger(t, f, 2)
	_, err = integrationDB.Exec(`UPDATE api_keys SET status='inactive' WHERE id=$1`, f.key)
	require.NoError(t, err)
	require.Error(t, store.BeginDispatch(ctx, d.Snapshot.Lease))
	require.NoError(t, store.Cancel(ctx, d.Snapshot.Lease))
	assertAdmissionLedger(t, f, 1)
}

func TestPrincipalAdmissionRecoveryAndZeroAT11AT24(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	store := &principalAdmissionStore{db: integrationDB}
	in := f.input()
	d, err := store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	recovered, err := store.RecoverReserved(ctx, in)
	require.NoError(t, err)
	require.Equal(t, d.Snapshot.Lease, recovered.Lease)
	assertAdmissionLedger(t, f, 1)
	require.NoError(t, store.BeginDispatch(ctx, recovered.Lease))
	require.ErrorIs(t, store.BeginDispatch(ctx, recovered.Lease), service.ErrAdmissionOwnership)
	_, err = store.RecoverReserved(ctx, in)
	require.ErrorIs(t, err, service.ErrAdmissionOwnership)
	f0 := newAdmissionFixture(t, 0)
	d, err = store.TryAdmit(ctx, f0.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, d.Code)
	assertAdmissionLedger(t, f0, 0)
}
