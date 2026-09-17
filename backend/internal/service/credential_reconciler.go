package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type CredentialReconciler struct {
	ops  CredentialOperations
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func NewCredentialReconciler(ops CredentialOperations) *CredentialReconciler {
	return &CredentialReconciler{ops: ops, stop: make(chan struct{}), done: make(chan struct{})}
}
func (r *CredentialReconciler) Start() {
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-ticker.C:
				ctx, end := context.WithTimeout(context.Background(), 8*time.Second)
				count, err := r.ops.ReconcileCredentialLeases(ctx)
				end()
				if err != nil {
					slog.Error("credential_ledger_reconcile_failed", "action", "stop_dispatch_until_store_recovers")
				} else if count > 0 {
					slog.Warn("credential_ledger_lost_owners", "leases", count, "action", "inspect_orphaned_capacity")
				}
			}
		}
	}()
}
func (r *CredentialReconciler) Stop() { r.once.Do(func() { close(r.stop) }); <-r.done }
