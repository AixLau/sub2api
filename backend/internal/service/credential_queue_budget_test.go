package service

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCredentialQueueBudgetBoundsAndIdempotentRelease(t *testing.T) {
	b := &CredentialQueueBudget{}
	var releases []func()
	for range 10 {
		release, ok := b.TryReserve(1, 1)
		require.True(t, ok)
		releases = append(releases, release)
	}
	_, ok := b.TryReserve(1, 1)
	require.False(t, ok)
	other, ok := b.TryReserve(2, 1)
	require.True(t, ok)
	other()
	other()
	for _, release := range releases {
		release()
		release()
	}
	require.Zero(t, b.requests)
	require.Zero(t, b.bytes)
	_, ok = b.TryReserve(1, 9<<20)
	require.False(t, ok)
}
