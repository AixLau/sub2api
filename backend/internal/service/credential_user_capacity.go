package service

import "context"

type CredentialGlobalUserSlots interface {
	Acquire(context.Context, LeaseRef, int) (bool, error)
	Release(context.Context, LeaseRef) error
	Reconcile(context.Context) error
}
