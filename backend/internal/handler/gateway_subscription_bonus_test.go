package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCalculateSubscriptionRemaining_UsesMonthlyBonus(t *testing.T) {
	monthlyLimit := 100.0
	group := &service.Group{MonthlyLimitUSD: &monthlyLimit}
	sub := &service.UserSubscription{
		MonthlyUsageUSD: 50,
		MonthlyBonusUSD: 100,
	}

	remaining := (&GatewayHandler{}).calculateSubscriptionRemaining(group, sub)

	require.Equal(t, 150.0, remaining)
}
