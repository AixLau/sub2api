//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
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

	if stats.TotalAmount["CNY"] != 100.00 {
		t.Fatalf("total_amount = %+v, want CNY=100.00", stats.TotalAmount)
	}
	if stats.TodayAmount["CNY"] != 100.00 {
		t.Fatalf("today_amount = %+v, want CNY=100.00", stats.TodayAmount)
	}
	if stats.AvgAmount["CNY"] != 100.00 {
		t.Fatalf("avg_amount = %+v, want CNY=100.00", stats.AvgAmount)
	}
	if len(stats.PaymentMethods) != 1 || stats.PaymentMethods[0].Amount["CNY"] != 100.00 {
		t.Fatalf("payment_methods = %+v, want CNY=100.00 in alipay bucket", stats.PaymentMethods)
	}
	if len(stats.TopUsers["CNY"]) != 1 || stats.TopUsers["CNY"][0].Amount != 100.00 {
		t.Fatalf("top_users = %+v, want CNY=100.00", stats.TopUsers)
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

	if stats.TotalAmount["CNY"] != 5.00 {
		t.Fatalf("total_amount = %+v, want CNY=5.00", stats.TotalAmount)
	}
	if stats.TodayAmount["CNY"] != 5.00 {
		t.Fatalf("today_amount = %+v, want CNY=5.00", stats.TodayAmount)
	}
	if stats.AvgAmount["CNY"] != 5.00 {
		t.Fatalf("avg_amount = %+v, want CNY=5.00", stats.AvgAmount)
	}
	if len(stats.DailySeries) == 0 || stats.DailySeries[len(stats.DailySeries)-1].Amount["CNY"] != 5.00 {
		t.Fatalf("last daily_series = %+v, want CNY=5.00", stats.DailySeries)
	}
	if len(stats.PaymentMethods) != 1 || stats.PaymentMethods[0].Amount["CNY"] != 5.00 {
		t.Fatalf("payment_methods = %+v, want CNY=5.00 in alipay bucket", stats.PaymentMethods)
	}
	if len(stats.TopUsers["CNY"]) != 1 || stats.TopUsers["CNY"][0].Amount != 5.00 {
		t.Fatalf("top_users = %+v, want CNY=5.00", stats.TopUsers)
	}
}

func TestComputeBasicStatsGroupsAmountsByCurrency(t *testing.T) {
	t.Parallel()

	todayStart := time.Date(2026, time.July, 25, 0, 0, 0, 0, time.UTC)
	yesterday := todayStart.Add(-time.Hour)
	today := todayStart.Add(time.Hour)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 10, &today),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &today),
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5, &yesterday),
	}

	stats := &DashboardStats{}
	computeBasicStats(stats, orders, todayStart)

	require.Equal(t, CurrencyAmounts{"CNY": 15, "USD": 10}, stats.TotalAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 10, "USD": 10}, stats.TodayAmount)
	require.Equal(t, CurrencyAmounts{"CNY": 7.5, "USD": 10}, stats.AvgAmount)
	require.Equal(t, 3, stats.TotalCount)
	require.Equal(t, 2, stats.TodayCount)
}

func TestPaymentDashboardBreakdownsGroupAmountsAndRankingsByCurrency(t *testing.T) {
	t.Parallel()

	firstDay := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	secondDay := firstDay.AddDate(0, 0, 1)
	orders := []*dbent.PaymentOrder{
		paymentStatsTestOrder(1, "alice@example.com", "CNY", 5.555, &firstDay),
		paymentStatsTestOrder(2, "bob@example.com", "CNY", 10, &firstDay),
		paymentStatsTestOrder(1, "alice@example.com", "USD", 20, &secondDay),
		paymentStatsTestOrder(2, "bob@example.com", "USD", 10, &secondDay),
	}
	orders[0].PaymentType = "stripe"
	orders[1].PaymentType = "stripe"
	orders[2].PaymentType = "stripe"
	orders[3].PaymentType = "alipay"

	daily := buildDailySeries(orders, firstDay.AddDate(0, 0, -1), 2)
	require.Equal(t, []DailyStats{
		{Date: "2026-07-24", Amount: CurrencyAmounts{"CNY": 15.56}, Count: 2},
		{Date: "2026-07-25", Amount: CurrencyAmounts{"USD": 30}, Count: 2},
	}, daily)

	methods := buildMethodDistribution(orders)
	require.Equal(t, []PaymentMethodStat{
		{Type: "alipay", Amount: CurrencyAmounts{"USD": 10}, Count: 1},
		{Type: "stripe", Amount: CurrencyAmounts{"CNY": 15.56, "USD": 20}, Count: 3},
	}, methods)

	users := buildTopUsers(orders)
	require.Equal(t, TopUsersByCurrency{
		"CNY": {
			{UserID: 2, Email: "bob@example.com", Amount: 10},
			{UserID: 1, Email: "alice@example.com", Amount: 5.56},
		},
		"USD": {
			{UserID: 1, Email: "alice@example.com", Amount: 20},
			{UserID: 2, Email: "bob@example.com", Amount: 10},
		},
	}, users)
}

func paymentStatsTestOrder(userID int64, email, currency string, amount float64, paidAt *time.Time) *dbent.PaymentOrder {
	return &dbent.PaymentOrder{
		UserID:           userID,
		UserEmail:        email,
		PayAmount:        amount,
		PaidAt:           paidAt,
		ProviderSnapshot: map[string]any{"currency": currency},
	}
}
