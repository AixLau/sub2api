package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type CredentialReconciler struct {
	closeAdmission  func() error
	globalUserSlots CredentialGlobalUserSlots
	usageStore      CredentialUsageReceiptStore
	gateway         *OpenAIGatewayService
	keys            APIKeyRepository
	updater         APIKeyQuotaUpdater
	refresh         *CredentialRefreshCoordinator
	ops             CredentialOperations
	stop            chan struct{}
	done            chan struct{}
	once            sync.Once
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
				if r.globalUserSlots != nil {
					if err := r.globalUserSlots.Reconcile(ctx); err != nil {
						slog.Error("credential_global_user_slots_unavailable")
					}
				}
				count, err := r.ops.ReconcileCredentialLeases(ctx)
				end()
				if err != nil {
					slog.Error("credential_ledger_reconcile_failed", "action", "stop_dispatch_until_store_recovers")
				} else if count > 0 {
					slog.Warn("credential_ledger_lost_owners", "leases", count, "action", "inspect_orphaned_capacity")
				}
				if r.usageStore != nil && r.gateway != nil {
					recovery, stop := context.WithTimeout(context.Background(), 8*time.Second)
					if err := r.gateway.RecoverCredentialUsage(recovery, r.usageStore, r.keys, r.updater); err != nil {
						slog.Error("credential_usage_recovery_failed")
					}
					stop()
				}
				if r.refresh != nil {
					lookup, stop := context.WithTimeout(context.Background(), 3*time.Second)
					ids, err := r.ops.DueCredentialRefreshes(lookup)
					stop()
					if err == nil {
						for _, id := range ids {
							work, done := context.WithTimeout(context.Background(), 15*time.Second)
							_ = r.refresh.Refresh(work, id)
							done()
						}
					}
				}
			}
		}
	}()
}
func (r *CredentialReconciler) Stop() {
	r.once.Do(func() {
		close(r.stop)
		<-r.done
		if r.closeAdmission != nil {
			_ = r.closeAdmission()
		}
	})
	<-r.done
}
