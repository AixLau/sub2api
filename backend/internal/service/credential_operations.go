package service

import (
	"context"
	"time"
)

type PrincipalControlUpdate struct {
	RequestedLimit *int       `json:"requested_limit"`
	AdminState     string     `json:"admin_state"`
	DrainDeadline  *time.Time `json:"drain_deadline"`
}
type InstanceControlUpdate struct {
	Weight         *float64   `json:"weight"`
	HardMax        *int       `json:"hard_max"`
	ClearHardMax   bool       `json:"clear_hard_max"`
	HealthCapacity *int       `json:"health_capacity"`
	AdminState     string     `json:"admin_state"`
	DrainDeadline  *time.Time `json:"drain_deadline"`
}
type CredentialRuntimeView struct {
	PrincipalID      int64     `json:"principal_id"`
	Occupied         int       `json:"occupied"`
	Reserved         int       `json:"reserved"`
	Dispatching      int       `json:"dispatching"`
	Running          int       `json:"running"`
	Cancelling       int       `json:"cancelling"`
	Orphaned         int       `json:"orphaned"`
	Queued           int       `json:"queued"`
	LedgerOccupied   int       `json:"ledger_occupied"`
	InstanceOccupied int       `json:"instance_occupied"`
	CounterMismatch  bool      `json:"counter_mismatch"`
	UnknownUsage     int       `json:"unknown_usage"`
	UnknownRefresh   int       `json:"unknown_refresh"`
	ObservedAt       time.Time `json:"observed_at"`
}
type CredentialResolveInput struct {
	Evidence          string `json:"evidence"`
	Reason            string `json:"reason"`
	Confirm           bool   `json:"confirm"`
	AcceptUnknownRisk bool   `json:"accept_unknown_risk"`
}
type CredentialOperations interface {
	UpdatePrincipal(context.Context, int64, int64, int64, PrincipalControlUpdate) (*PrincipalView, error)
	UpdateInstance(context.Context, int64, int64, int64, InstanceControlUpdate) (*PrincipalView, error)
	CredentialRuntime(context.Context, int64) (CredentialRuntimeView, error)
	ResolveCredentialLease(context.Context, int64, string, CredentialResolveInput) error
	ReconcileCredentialLeases(context.Context) (int, error)
}
