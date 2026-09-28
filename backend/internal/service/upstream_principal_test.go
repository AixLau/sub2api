package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrincipalCapacityView(t *testing.T) {
	zero, eight := 0, 8
	p := PrincipalView{RequestedLimit: 5, Occupied: 8, AdminState: "ACTIVE", RoutingMode: "GROUPED", VerificationState: "VERIFIED",
		Instances: []CredentialInstanceView{{HardMax: &eight}, {HardMax: &zero}, {}}}
	p.ComputeCapacityView(true)
	require.Equal(t, "DRAINING_TO_LIMIT", p.AdmissionState)
	require.Equal(t, 3, p.Overhang)
	require.Equal(t, 5, p.Instances[0].EffectiveHardMax)
	require.Zero(t, p.Instances[1].EffectiveHardMax)
	require.Equal(t, 5, p.Instances[2].EffectiveHardMax)
	p.Occupied = 0
	p.ComputeCapacityView(false)
	require.Equal(t, "PAUSED", p.AdmissionState)
	p.ComputeCapacityView(true)
	require.Equal(t, "ACTIVE", p.AdmissionState)
	p.RequestedLimit = 0
	p.ComputeCapacityView(true)
	require.Equal(t, "PAUSED", p.AdmissionState)
	b, err := json.Marshal(p)
	require.NoError(t, err)
	require.NotContains(t, string(b), "installation_id")
	require.NotContains(t, string(b), "credentials")
}
