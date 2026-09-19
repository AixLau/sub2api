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
	GroupIDs                    []int64                  `json:"group_ids"`
	ProxyID                     *int64                   `json:"proxy_id"`
	AccountID                   int64                    `json:"account_id"`
	AccountMaxConcurrency       int                      `json:"account_max_concurrency"`
	ConfiguredCapacity          int                      `json:"configured_capacity"`
	EffectiveConfiguredCapacity int                      `json:"effective_configured_capacity"`
	AvailableCapacity           int                      `json:"available_capacity"`
	HealthState                 string                   `json:"health_state"`
	UnknownOccupied             int                      `json:"unknown_occupied"`
	ArchivedAt                  *time.Time               `json:"archived_at,omitempty"`
	ID                          int64                    `json:"id"`
	Name                        string                   `json:"name"`
	Provider                    string                   `json:"provider"`
	VerificationState           string                   `json:"verification_state"`
	RequestedLimit              int                      `json:"requested_limit"`
	Occupied                    int                      `json:"occupied"`
	ConfigVersion               int64                    `json:"config_version"`
	AdminState                  string                   `json:"admin_state"`
	RoutingMode                 string                   `json:"routing_mode"`
	AdmissionState              string                   `json:"admission_state"`
	Overhang                    int                      `json:"overhang"`
	Instances                   []CredentialInstanceView `json:"instances"`
	ObservedAt                  time.Time                `json:"observed_at"`
}

type CredentialInstanceView struct {
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	MaxConcurrency    int        `json:"max_concurrency"`
	State             string     `json:"state"`
	Overhang          int        `json:"overhang"`
	UnknownOccupied   int        `json:"unknown_occupied"`
	ActiveBindings    int        `json:"active_bindings"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`
	CooldownUntil     *time.Time `json:"cooldown_until,omitempty"`
	ID                int64      `json:"id"`
	AccountID         int64      `json:"account_id"`
	Name              string     `json:"name"`
	Generation        string     `json:"generation"`
	CredentialVersion int64      `json:"credential_version"`
	Weight            float64    `json:"weight"`
	HardMax           *int       `json:"hard_max"`
	HealthCapacity    *int       `json:"health_capacity"`
	EffectiveHardMax  int        `json:"effective_hard_max"`
	Occupied          int        `json:"occupied"`
	AdminState        string     `json:"admin_state"`
	CredentialState   string     `json:"credential_state"`
	TransportState    string     `json:"transport_state"`
	IdentitySource    string     `json:"identity_source"`
}

func (p *PrincipalView) ComputeCapacityView(enabled bool) {
	p.AccountMaxConcurrency = p.RequestedLimit
	p.ConfiguredCapacity, p.AvailableCapacity, p.UnknownOccupied = 0, 0, 0
	healthy, configured := 0, 0
	p.Overhang = max(0, p.Occupied-p.RequestedLimit)
	p.AdmissionState = "PAUSED"
	if p.Overhang > 0 {
		p.AdmissionState = "DRAINING_TO_LIMIT"
	} else if enabled && p.RoutingMode == "GROUPED" && p.VerificationState == "VERIFIED" && p.AdminState == "ACTIVE" && p.RequestedLimit > 0 {
		p.AdmissionState = "ACTIVE"
	}
	for i := range p.Instances {
		v := &p.Instances[i]
		v.MaxConcurrency = 0
		if v.HardMax != nil {
			v.MaxConcurrency = *v.HardMax
		}
		v.Overhang = max(0, v.Occupied-v.MaxConcurrency)
		p.UnknownOccupied += v.UnknownOccupied
		if v.ArchivedAt == nil {
			p.ConfiguredCapacity += v.MaxConcurrency
			configured++
		}
		v.EffectiveHardMax = p.RequestedLimit
		if v.HardMax != nil {
			v.EffectiveHardMax = min(v.EffectiveHardMax, *v.HardMax)
		}
		if v.HealthCapacity != nil {
			v.EffectiveHardMax = min(v.EffectiveHardMax, *v.HealthCapacity)
		}
		switch {
		case v.ArchivedAt != nil:
			v.State = "ARCHIVED"
		case v.CredentialState == "REFRESH_UNKNOWN":
			v.State = "UNKNOWN"
		case v.AdminState == "DRAINING":
			v.State = "DRAINING"
		case v.AdminState != "ACTIVE" || v.MaxConcurrency == 0:
			v.State = "PAUSED"
		case v.CredentialState == "REFRESHING":
			v.State = "REFRESHING"
		case v.CredentialState != "VALID" || v.ExpiresAt == nil || !v.ExpiresAt.After(p.ObservedAt):
			v.State = "NEEDS_REAUTH"
		case v.CooldownUntil != nil && v.CooldownUntil.After(p.ObservedAt):
			v.State = "COOLDOWN"
		case v.TransportState != "HEALTHY" && v.TransportState != "DEGRADED" && v.TransportState != "HALF_OPEN":
			v.State = "COOLDOWN"
		default:
			healthy++
			p.AvailableCapacity += max(0, v.EffectiveHardMax-v.Occupied)
			v.State = "ACTIVE"
			if v.Occupied >= v.EffectiveHardMax {
				v.State = "FULL"
			}
			if v.Overhang > 0 {
				v.State = "DRAINING_TO_LIMIT"
			}
			if v.UnknownOccupied > 0 {
				v.State = "UNKNOWN"
			}
		}
	}
	p.EffectiveConfiguredCapacity = min(p.RequestedLimit, p.ConfiguredCapacity)
	p.AvailableCapacity = min(max(0, p.RequestedLimit-p.Occupied), p.AvailableCapacity)
	p.HealthState = "AVAILABLE"
	if healthy == 0 {
		p.HealthState = "UNAVAILABLE"
	} else if healthy < configured {
		p.HealthState = "PARTIAL"
	}
	if p.AdminState != "ACTIVE" || p.AdmissionState == "PAUSED" {
		p.AvailableCapacity = 0
		p.HealthState = p.AdminState
		if p.AdminState == "ACTIVE" {
			p.HealthState = "PAUSED"
		}
	}
}

type UpstreamPrincipalReader interface {
	AccountPrincipals(context.Context, []int64) ([]PrincipalView, error)
	ListPrincipals(context.Context, int64, int64, int) ([]PrincipalView, error)
	GetPrincipal(context.Context, int64, int64) (*PrincipalView, error)
}
