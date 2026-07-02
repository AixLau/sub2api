//go:build unit

package service

import (
	"context"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestGenerateOutTradeNoOmitsLegacySub2Prefix(t *testing.T) {
	t.Parallel()

	got := generateOutTradeNo()

	if regexp.MustCompile(`^sub2_`).MatchString(got) {
		t.Fatalf("generateOutTradeNo() = %q, want no legacy sub2_ prefix", got)
	}
	if !regexp.MustCompile(`^\d{8}[A-Za-z0-9]{8}$`).MatchString(got) {
		t.Fatalf("generateOutTradeNo() = %q, want yyyymmdd plus 8 random alphanumeric chars", got)
	}
}

func TestAdminListOrdersKeywordMatchesUserID(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	firstUser, err := client.User.Create().
		SetEmail("first-user@example.com").
		SetPasswordHash("hash").
		SetUsername("first-user").
		Save(ctx)
	require.NoError(t, err)
	secondUser, err := client.User.Create().
		SetEmail("second-user@example.com").
		SetPasswordHash("hash").
		SetUsername("second-user").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.PaymentOrder.Create().
		SetUserID(firstUser.ID).
		SetUserEmail(firstUser.Email).
		SetUserName(firstUser.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("FIRST-CODE").
		SetOutTradeNo("20260702first001").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)
	secondOrder, err := client.PaymentOrder.Create().
		SetUserID(secondUser.ID).
		SetUserEmail(secondUser.Email).
		SetUserName(secondUser.Username).
		SetAmount(20).
		SetPayAmount(20).
		SetFeeRate(0).
		SetRechargeCode("SECOND-CODE").
		SetOutTradeNo("20260702second01").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	orders, total, err := svc.AdminListOrders(ctx, 0, OrderListParams{UserQuery: strconv.FormatInt(secondUser.ID, 10)})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, orders, 1)
	require.Equal(t, secondOrder.ID, orders[0].ID)

	orders, total, err = svc.AdminListOrders(ctx, 0, OrderListParams{UserQuery: secondUser.Email})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, orders, 1)
	require.Equal(t, secondOrder.ID, orders[0].ID)

	orders, total, err = svc.AdminListOrders(ctx, 0, OrderListParams{Keyword: secondUser.Email})
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, orders)
}
