package service

import (
	"context"
	"time"
)

// DeploymentPrincipalScope is the existing deployment-wide admin boundary.
// The repository has no tenant model. Groups are permissions, not tenants.
const DeploymentPrincipalScope int64 = 1

// PrincipalView deliberately excludes external subject IDs, installation IDs,
// credentials and Account.Extra. It is safe for the existing admin read boundary.
type PrincipalView struct {
	ID                int64                    `json:"id"`
	Name              string                   `json:"name"`
	Provider          string                   `json:"provider"`
	VerificationState string                   `json:"verification_state"`
	RequestedLimit    int                      `json:"requested_limit"`
	Occupied          int                      `json:"occupied"`
	ConfigVersion     int64                    `json:"config_version"`
	AdminState        string                   `json:"admin_state"`
	RoutingMode       string                   `json:"routing_mode"`
	AdmissionState    string                   `json:"admission_state"`
	Overhang          int                      `json:"overhang"`
	Instances         []CredentialInstanceView `json:"instances"`
	ObservedAt        time.Time                `json:"observed_at"`
}

type CredentialInstanceView struct {
	ID                int64   `json:"id"`
	AccountID         int64   `json:"account_id"`
	Name              string  `json:"name"`
	Generation        string  `json:"generation"`
	CredentialVersion int64   `json:"credential_version"`
	Weight            float64 `json:"weight"`
	HardMax           *int    `json:"hard_max"`
	HealthCapacity    *int    `json:"health_capacity"`
	EffectiveHardMax  int     `json:"effective_hard_max"`
	Occupied          int     `json:"occupied"`
	AdminState        string  `json:"admin_state"`
	CredentialState   string  `json:"credential_state"`
	TransportState    string  `json:"transport_state"`
	IdentitySource    string  `json:"identity_source"`
}

func (p *PrincipalView) ComputeCapacityView(enabled bool) {
	p.Overhang = max(0, p.Occupied-p.RequestedLimit)
	p.AdmissionState = "PAUSED"
	if p.Overhang > 0 {
		p.AdmissionState = "DRAINING_TO_LIMIT"
	} else if enabled && p.RoutingMode == "GROUPED" && p.VerificationState == "VERIFIED" && p.AdminState == "ACTIVE" && p.RequestedLimit > 0 {
		p.AdmissionState = "ACTIVE"
	}
	for i := range p.Instances {
		v := &p.Instances[i]
		v.EffectiveHardMax = p.RequestedLimit
		if v.HardMax != nil {
			v.EffectiveHardMax = min(v.EffectiveHardMax, *v.HardMax)
		}
		if v.HealthCapacity != nil {
			v.EffectiveHardMax = min(v.EffectiveHardMax, *v.HealthCapacity)
		}
	}
}

type UpstreamPrincipalReader interface {
	ListPrincipals(context.Context, int64, int64, int) ([]PrincipalView, error)
	GetPrincipal(context.Context, int64, int64) (*PrincipalView, error)
}
