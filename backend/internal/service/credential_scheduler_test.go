package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCredentialTargetsAT10(t *testing.T) {
	result := CredentialTargets(12, []CredentialDemand{{1, 1, 10, 12}, {2, 2, 2, 12}, {3, 1, 10, 12}})
	require.Equal(t, map[int64]float64{1: 5, 2: 2, 3: 5}, result)
	require.Equal(t, map[int64]float64{1: 1, 2: 1, 3: 1}, CredentialTargets(12, []CredentialDemand{{1, 1, 1, 12}, {2, 2, 1, 12}, {3, 1, 1, 12}}))
	require.Equal(t, map[int64]float64{1: 10, 2: 0, 3: 0}, CredentialTargets(10, []CredentialDemand{{1, 1, 20, 10}, {2, 1, 0, 10}, {3, 1, 0, 10}}))
	require.Equal(t, map[int64]float64{1: 0}, CredentialTargets(10, []CredentialDemand{{1, 1, 20, 0}}))
}
