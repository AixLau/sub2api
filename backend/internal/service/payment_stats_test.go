package service

import (
	"context"
	"testing"
	"time"
)

// TestGetDashboardStatsExcludesGatewayFee covers the revenue rule:
// dashboard totals reflect the payment principal before gateway fees, not
// pay_amount, and not any credited balance/quota stored on amount.
func TestGetDashboardStatsExcludesGatewayFee(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetUsername("buyer").
		SetEmail("buyer@example.com").
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	paidAt := time.Now().Add(-1 * time.Hour)
	if _, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail("buyer@example.com").
		SetUserName("buyer").
		SetAmount(120.00).
		SetPayAmount(105.00).
		SetFeeRate(5).
		SetRechargeCode("RC_FEE").
		SetPaymentType("alipay").
		SetPaymentTradeNo("TRADE_FEE").
		SetClientIP("127.0.0.1").
		SetSrcHost("test").
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(paidAt).
		SetStatus(OrderStatusCompleted).
		Save(ctx); err != nil {
		t.Fatalf("create paid order: %v", err)
	}

	svc := &PaymentService{entClient: client}
	stats, err := svc.GetDashboardStats(ctx, 30)
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}

	if stats.TotalAmount != 100.00 {
		t.Fatalf("total_amount = %.2f, want 100.00", stats.TotalAmount)
	}
	if stats.TodayAmount != 100.00 {
		t.Fatalf("today_amount = %.2f, want 100.00", stats.TodayAmount)
	}
	if stats.AvgAmount != 100.00 {
		t.Fatalf("avg_amount = %.2f, want 100.00", stats.AvgAmount)
	}
	if len(stats.PaymentMethods) != 1 || stats.PaymentMethods[0].Amount != 100.00 {
		t.Fatalf("payment_methods = %+v, want amount=100.00 in alipay bucket", stats.PaymentMethods)
	}
	if len(stats.TopUsers) != 1 || stats.TopUsers[0].Amount != 100.00 {
		t.Fatalf("top_users = %+v, want 100.00", stats.TopUsers)
	}
}

func TestGetDashboardStatsUsesPaymentPrincipalNotCreditedBalance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	user, err := client.User.Create().
		SetUsername("buyer").
		SetEmail("buyer@example.com").
		SetPasswordHash("x").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	paidAt := time.Now().Add(-1 * time.Hour)
	if _, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail("buyer@example.com").
		SetUserName("buyer").
		SetAmount(35.00).
		SetPayAmount(5.10).
		SetFeeRate(2).
		SetRechargeCode("RC_PRINCIPAL").
		SetPaymentType("alipay").
		SetPaymentTradeNo("TRADE_PRINCIPAL").
		SetClientIP("127.0.0.1").
		SetSrcHost("test").
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(paidAt).
		SetStatus(OrderStatusCompleted).
		Save(ctx); err != nil {
		t.Fatalf("create paid order: %v", err)
	}

	svc := &PaymentService{entClient: client}
	stats, err := svc.GetDashboardStats(ctx, 30)
	if err != nil {
		t.Fatalf("GetDashboardStats: %v", err)
	}

	if stats.TotalAmount != 5.00 {
		t.Fatalf("total_amount = %.2f, want 5.00", stats.TotalAmount)
	}
	if stats.TodayAmount != 5.00 {
		t.Fatalf("today_amount = %.2f, want 5.00", stats.TodayAmount)
	}
	if stats.AvgAmount != 5.00 {
		t.Fatalf("avg_amount = %.2f, want 5.00", stats.AvgAmount)
	}
	if len(stats.DailySeries) == 0 || stats.DailySeries[len(stats.DailySeries)-1].Amount != 5.00 {
		t.Fatalf("last daily_series = %+v, want amount=5.00", stats.DailySeries)
	}
	if len(stats.PaymentMethods) != 1 || stats.PaymentMethods[0].Amount != 5.00 {
		t.Fatalf("payment_methods = %+v, want amount=5.00 in alipay bucket", stats.PaymentMethods)
	}
	if len(stats.TopUsers) != 1 || stats.TopUsers[0].Amount != 5.00 {
		t.Fatalf("top_users = %+v, want 5.00", stats.TopUsers)
	}
}
