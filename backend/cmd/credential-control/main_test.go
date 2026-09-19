package main

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/credentialfence"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type countingFence struct {
	fenceCalls  int
	verifyCalls int
	evidence    credentialfence.Evidence
}

func (f *countingFence) Fence(context.Context) (credentialfence.Evidence, error) {
	f.fenceCalls++
	if f.evidence.Project == "" {
		f.evidence = credentialfence.Evidence{Project: "test", Service: "gateway", IDs: []string{"container"}}
	}
	return f.evidence, nil
}

func (f *countingFence) Verify(context.Context, credentialfence.Evidence) error {
	f.verifyCalls++
	return nil
}

func TestBatchFenceFencesOnceAndVerifiesEveryStep(t *testing.T) {
	delegate := &countingFence{}
	fence := &batchFence{delegate: delegate}
	first, err := fence.Fence(context.Background())
	require.NoError(t, err)
	second, err := fence.Fence(context.Background())
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, 1, delegate.fenceCalls)
	require.NoError(t, fence.Verify(context.Background(), first))
	require.NoError(t, fence.Verify(context.Background(), second))
	require.Equal(t, 2, delegate.verifyCalls)
}

func TestCredentialMigrationErrorCodeDoesNotExposeErrorText(t *testing.T) {
	err := errors.New("provider token leaked in driver diagnostics")
	require.Equal(t, "MIGRATION_PRECONDITION_FAILED", credentialMigrationErrorCode(err, ""))
	require.Equal(t, "CREDENTIAL_UNVERIFIED", credentialMigrationErrorCode(nil, "UNVERIFIED"))
	require.Equal(t, "CREDENTIAL_UNVERIFIED", credentialMigrationErrorCode(service.ErrCredentialUnverified, ""))
}
