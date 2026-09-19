//go:build integration && account_capacity

package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountInstanceVerifiedCreateAppendAndPendingRecovery(t *testing.T) {
	ctx := context.Background()
	owner := newAdmissionFixture(t, 1).user
	vault, err := service.NewCredentialVault(strings.Repeat("ab", 32))
	require.NoError(t, err)
	repo := &credentialImportRepository{db: integrationDB}
	importer := service.NewCredentialImportService(repo, vault, mockCredentialVerifier{})
	nonce := uuid.NewString()
	pendingImporter := service.NewCredentialImportService(repo, vault, nil)
	pending, err := pendingImporter.Import(ctx, owner, "pending", service.CredentialSecret{AccessToken: "mock:" + nonce + ":first"})
	require.NoError(t, err)
	require.Equal(t, "UNVERIFIED", pending.State)
	verified, err := importer.Reverify(ctx, owner, pending.ID)
	require.NoError(t, err)
	require.Equal(t, "VERIFIED", verified.State)
	five := 5
	input := service.CreateCredentialPrincipalInput{Name: "account-" + nonce, TotalConcurrency: 12, Activate: true, Instances: []service.CreateCredentialInstanceInput{{Name: "first", ImportID: pending.ID, HardMax: &five}}}
	id, err := repo.CreateCredentialPrincipal(ctx, owner, "create", input)
	require.NoError(t, err)
	repeated, err := repo.CreateCredentialPrincipal(ctx, owner, "create", input)
	require.NoError(t, err)
	require.Equal(t, id, repeated)
	reader := NewUpstreamPrincipalReader(integrationDB)
	before, err := reader.GetPrincipal(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, "GROUPED", before.RoutingMode)
	next, err := importer.Import(ctx, owner, "second", service.CredentialSecret{AccessToken: "mock:" + nonce + ":second"})
	require.NoError(t, err)
	add := service.CredentialInstanceAddInput{CreateCredentialInstanceInput: service.CreateCredentialInstanceInput{Name: "second", ImportID: next.ID, HardMax: &five}}
	instance, err := repo.AddCredentialInstance(ctx, owner, id, before.ConfigVersion, "add", add)
	require.NoError(t, err)
	repeatedInstance, err := repo.AddCredentialInstance(ctx, owner, id, before.ConfigVersion, "add", add)
	require.NoError(t, err)
	require.Equal(t, instance, repeatedInstance)
	after, err := reader.GetPrincipal(ctx, 1, id)
	require.NoError(t, err)
	after.ComputeCapacityView(true)
	require.Equal(t, 12, after.RequestedLimit)
	require.Equal(t, 10, after.ConfiguredCapacity)
	require.Equal(t, before.AccountID, after.AccountID)
	require.Equal(t, "ACTIVE", after.Instances[1].AdminState)
	foreign, err := importer.Import(ctx, owner, "foreign", service.CredentialSecret{AccessToken: "mock:" + uuid.NewString() + ":other"})
	require.NoError(t, err)
	add.ImportID = foreign.ID
	_, err = repo.AddCredentialInstance(ctx, owner, id, after.ConfigVersion, "foreign", add)
	require.ErrorIs(t, err, service.ErrCredentialOwnershipMismatch)
	final, err := reader.GetPrincipal(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, after.ConfigVersion, final.ConfigVersion)
	require.Len(t, final.Instances, 2)
}

func TestAccountInstanceTwoLevelCapacity(t *testing.T) {
	for _, limit := range []int{12, 20} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			f := newAdmissionFixture(t, limit)
			ctx := context.Background()
			_, err := integrationDB.Exec(`UPDATE credential_instances SET hard_max=5 WHERE principal_id=$1`, f.principal)
			require.NoError(t, err)
			var wg sync.WaitGroup
			results := make(chan service.AdmissionDecision, 24)
			errs := make(chan error, 24)
			for range 24 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					d, e := NewPrincipalAdmissionStore(integrationDB).TryAdmit(ctx, f.input())
					results <- d
					errs <- e
				}()
			}
			wg.Wait()
			close(results)
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			admitted := 0
			for d := range results {
				if d.Code == service.AdmissionAdmitted {
					admitted++
				} else {
					require.Equal(t, service.AdmissionWait, d.Code)
				}
			}
			require.Equal(t, min(limit, 15), admitted)
			assertAdmissionLedger(t, f, admitted)
			p, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
			require.NoError(t, err)
			p.ComputeCapacityView(true)
			require.Equal(t, 15, p.ConfiguredCapacity)
			require.Equal(t, min(limit, 15), p.EffectiveConfiguredCapacity)
			require.Zero(t, p.AvailableCapacity)
			for _, i := range p.Instances {
				require.LessOrEqual(t, i.Occupied, 5)
				require.Equal(t, 5, i.MaxConcurrency)
			}
		})
	}
}

func TestAccountInstanceAtomicConfigurationAndStableManagementID(t *testing.T) {
	f := newAdmissionFixture(t, 12)
	ctx := context.Background()
	ops := &credentialOperations{db: integrationDB}
	before, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	limit := 20
	updates := []service.InstanceCapacityUpdate{}
	for index, id := range f.instances {
		updates = append(updates, service.InstanceCapacityUpdate{ID: id, Name: fmt.Sprint(index), MaxConcurrency: 5})
	}
	invalid := append(append([]service.InstanceCapacityUpdate{}, updates...), service.InstanceCapacityUpdate{ID: 99999999, Name: "foreign", MaxConcurrency: 1})
	_, err = ops.UpdatePrincipal(ctx, f.user, f.principal, 1, service.PrincipalControlUpdate{AccountMaxConcurrency: &limit, Instances: invalid})
	require.Error(t, err)
	p, err := NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	require.Equal(t, 12, p.RequestedLimit)
	require.Equal(t, int64(1), p.ConfigVersion)
	require.Equal(t, 12, *p.Instances[0].HardMax)
	input := service.PrincipalControlUpdate{AccountMaxConcurrency: &limit, Instances: updates}
	p, err = ops.UpdatePrincipal(ctx, f.user, f.principal, 1, input)
	require.NoError(t, err)
	p.ComputeCapacityView(true)
	require.Equal(t, 15, p.EffectiveConfiguredCapacity)
	require.Equal(t, int64(2), p.ConfigVersion)
	replay, err := ops.UpdatePrincipal(ctx, f.user, f.principal, 1, input)
	require.NoError(t, err)
	require.Equal(t, p.ConfigVersion, replay.ConfigVersion)
	limit = 8
	_, err = ops.UpdatePrincipal(ctx, f.user, f.principal, 1, service.PrincipalControlUpdate{AccountMaxConcurrency: &limit})
	require.ErrorIs(t, err, errCredentialConfigConflict)
	// The representative remains a management row after its own authorization exits.
	p, err = ops.UpdateInstance(ctx, f.user, f.instances[0], 2, service.InstanceControlUpdate{AdminState: "PAUSED"})
	require.NoError(t, err)
	p, err = ops.UpdateInstance(ctx, f.user, f.instances[0], p.ConfigVersion, service.InstanceControlUpdate{Archive: true})
	require.NoError(t, err)
	p.ComputeCapacityView(true)
	require.Equal(t, before.AccountID, p.AccountID)
	require.Equal(t, 20, p.RequestedLimit)
	require.Equal(t, 10, p.ConfiguredCapacity)
	views, err := NewUpstreamPrincipalReader(integrationDB).AccountPrincipals(ctx, []int64{before.AccountID})
	require.NoError(t, err)
	require.Len(t, views, 1)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	accounts, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 100}, "openai", "", "", "", 0, "")
	require.NoError(t, err)
	found := map[int64]bool{}
	for _, a := range accounts {
		found[a.ID] = true
	}
	active, _, err := repo.ListWithFilters(ctx, pagination.PaginationParams{Page: 1, PageSize: 100}, "openai", "", service.StatusActive, "", 0, "")
	require.NoError(t, err)
	activeFound := false
	for _, a := range active {
		if a.ID == before.AccountID {
			activeFound = true
		}
	}
	require.True(t, activeFound, "managed health must not use the inactive credential carrier status")
	require.True(t, found[before.AccountID])
	require.False(t, found[p.Instances[1].AccountID])
	require.False(t, found[p.Instances[2].AccountID])
}

func TestAccountInstanceLoadRatioAndShrink(t *testing.T) {
	f := newAdmissionFixture(t, 12)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	ops := &credentialOperations{db: integrationDB}
	_, err := integrationDB.Exec(`UPDATE credential_instances SET hard_max=CASE WHEN id=$2 THEN 2 ELSE 8 END WHERE principal_id=$1`, f.principal, f.instances[0])
	require.NoError(t, err)
	// 1/2 versus 2/8: the larger instance wins despite having more work.
	leases := []service.LeaseRef{}
	for _, index := range []int{0, 1, 1} {
		in := f.input()
		in.CandidateIDs = []int64{f.instances[index]}
		d, e := store.TryAdmit(ctx, in)
		require.NoError(t, e)
		require.Equal(t, service.AdmissionAdmitted, d.Code)
		leases = append(leases, d.Snapshot.Lease)
	}
	in := f.input()
	in.CandidateIDs = f.instances[:2]
	d, err := store.TryAdmit(ctx, in)
	require.NoError(t, err)
	require.Equal(t, f.instances[1], d.Snapshot.Lease.InstanceID)
	leases = append(leases, d.Snapshot.Lease)
	limit := 2
	p, err := ops.UpdatePrincipal(ctx, f.user, f.principal, 1, service.PrincipalControlUpdate{AccountMaxConcurrency: &limit})
	require.NoError(t, err)
	p.ComputeCapacityView(true)
	require.Equal(t, 4, p.Occupied)
	require.Equal(t, 2, p.Overhang)
	d, err = store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionWait, d.Code)
	for _, lease := range leases[:3] {
		require.NoError(t, store.Cancel(ctx, lease))
		require.NoError(t, store.Cancel(ctx, lease))
	}
	assertAdmissionLedger(t, f, 1)
	p, err = NewUpstreamPrincipalReader(integrationDB).GetPrincipal(ctx, 1, f.principal)
	require.NoError(t, err)
	p.ComputeCapacityView(true)
	require.Equal(t, 1, p.AvailableCapacity)
	// Pause a loaded instance: its occupancy still contributes to the account.
	p, err = ops.UpdateInstance(ctx, f.user, f.instances[1], 2, service.InstanceControlUpdate{AdminState: "PAUSED"})
	require.NoError(t, err)
	_, err = ops.UpdateInstance(ctx, f.user, f.instances[1], p.ConfigVersion, service.InstanceControlUpdate{Archive: true})
	require.Error(t, err)
	require.Equal(t, 1, p.Occupied)
	deadline := time.Now().Add(time.Hour)
	_, err = ops.UpdateInstance(ctx, f.user, f.instances[0], p.ConfigVersion, service.InstanceControlUpdate{AdminState: "DRAINING", DrainDeadline: &deadline})
	require.NoError(t, err)
}
