package service

import (
	"context"
	"testing"
	"time"
)

// TestGetDashboardStatsExcludesGatewayFee covers the platform-revenue rule:
// dashboard totals reflect amount (recharge before fees), not pay_amount
// (the figure including the gateway fee that the platform never receives).
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
		SetAmount(100.00).
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
