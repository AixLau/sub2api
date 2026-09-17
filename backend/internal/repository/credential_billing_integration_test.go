//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCredentialBillingOutboxDuplicateAT32(t *testing.T) {
	f := newAdmissionFixture(t, 2)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	_, err := integrationDB.Exec(`UPDATE users SET balance=100 WHERE id=$1`, f.user)
	require.NoError(t, err)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.NoError(t, store.BeginDispatch(ctx, d.Snapshot.Lease))
	tokens := int64(1)
	require.NoError(t, store.Finish(ctx, service.FinishAdmissionInput{Lease: d.Snapshot.Lease, Complete: true, Outcome: "COMPLETED", InputTokens: &tokens, OutputTokens: &tokens}))
	cmd := &service.UsageBillingCommand{RequestID: service.CredentialBillingRequestID(d.Snapshot.Lease.ID), UserID: f.user, APIKeyID: f.key, AccountID: d.Snapshot.AccountID, AccountType: "oauth", BalanceCost: 1.25}
	repo := NewUsageBillingRepository(nil, integrationDB)
	one, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, one.Applied)
	two, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, two.Applied)
	require.NoError(t, (&credentialOperations{db: integrationDB}).replayCredentialBilling(ctx))
	var balance float64
	require.NoError(t, integrationDB.QueryRow(`SELECT balance FROM users WHERE id=$1`, f.user).Scan(&balance))
	require.InDelta(t, 98.75, balance, .000001)
	var settled bool
	require.NoError(t, integrationDB.QueryRow(`SELECT settled_at IS NOT NULL FROM credential_billing_outbox WHERE lease_id=$1`, d.Snapshot.Lease.ID).Scan(&settled))
	require.True(t, settled)
}

func TestCredentialAuditOutboxDeliveredOnce(t *testing.T) {
	f := newAdmissionFixture(t, 1)
	ctx := context.Background()
	store := NewPrincipalAdmissionStore(integrationDB)
	d, err := store.TryAdmit(ctx, f.input())
	require.NoError(t, err)
	require.Equal(t, service.AdmissionAdmitted, d.Code)
	ops := &credentialOperations{db: integrationDB}
	require.NoError(t, ops.deliverCredentialAudit(ctx))
	require.NoError(t, ops.deliverCredentialAudit(ctx))
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM audit_logs a JOIN credential_audit_outbox o ON a.request_id=o.event_id::text WHERE o.principal_id=$1`, f.principal).Scan(&count))
	require.Equal(t, 1, count)
}
