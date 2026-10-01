package service

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const expiryCheckTimeout = 30 * time.Second

const ninePlusProductRefreshInterval = ninePlusProductCatalogCacheTTL

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc *PaymentService
	interval   time.Duration
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup

	lastNinePlusProductRefresh time.Time
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

func (s *PaymentOrderExpiryService) Start() {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *PaymentOrderExpiryService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *PaymentOrderExpiryService) runOnce() {
	s.refreshNinePlusProductsIfDue()

	reconcileCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	recovered, err := s.paymentSvc.ReconcilePendingPaymentOrders(reconcileCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending payment orders", "error", err)
	} else if recovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid orders", "count", recovered)
	}

	ninePlusCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	ninePlusRecovered, err := s.paymentSvc.ReconcilePendingNinePlusOrders(ninePlusCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending nineplus orders", "error", err)
	} else if ninePlusRecovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid nineplus orders", "count", ninePlusRecovered)
	}

	haozPayCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	haozPayRecovered, err := s.paymentSvc.ReconcilePendingHaozPayOrders(haozPayCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending haozpay orders", "error", err)
	} else if haozPayRecovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid haozpay orders", "count", haozPayRecovered)
	}

	ninePlusFulfillCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	ninePlusCompleted, err := s.paymentSvc.ReconcileNinePlusFulfillment(ninePlusFulfillCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile nineplus fulfillment orders", "error", err)
	} else if ninePlusCompleted > 0 {
		slog.Info("[PaymentOrderExpiry] completed nineplus fulfillment orders", "count", ninePlusCompleted)
	}

	expireCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	defer cancel()
	expired, err := s.paymentSvc.ExpireTimedOutOrders(expireCtx)
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
		return
	}
	if expired > 0 {
		slog.Info("[PaymentOrderExpiry] expired timed-out orders", "count", expired)
	}
}

func (s *PaymentOrderExpiryService) refreshNinePlusProductsIfDue() {
	if s == nil || s.paymentSvc == nil {
		return
	}
	now := time.Now()
	if !s.lastNinePlusProductRefresh.IsZero() && now.Sub(s.lastNinePlusProductRefresh) < ninePlusProductRefreshInterval {
		return
	}
	s.lastNinePlusProductRefresh = now

	refreshNinePlusCtx, cancel := context.WithTimeout(context.Background(), expiryCheckTimeout)
	refreshed, err := s.paymentSvc.RefreshNinePlusProductSnapshots(refreshNinePlusCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to refresh nineplus products", "error", err)
	} else if refreshed > 0 {
		slog.Info("[PaymentOrderExpiry] refreshed nineplus products", "count", refreshed)
	}
}
