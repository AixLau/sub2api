//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type bonusQuotaUserSubRepoStub struct {
	userSubRepoNoop

	sub                   *UserSubscription
	addMonthlyBonusCalled bool
	addMonthlyBonusAmount float64
	addMonthlyBonusErr    error
}

func (r *bonusQuotaUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *bonusQuotaUserSubRepoStub) AddMonthlyBonus(_ context.Context, id int64, amountUSD float64) error {
	r.addMonthlyBonusCalled = true
	r.addMonthlyBonusAmount = amountUSD
	if r.addMonthlyBonusErr != nil {
		return r.addMonthlyBonusErr
	}
	if r.sub == nil || r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.sub.MonthlyBonusUSD += amountUSD
	return nil
}

func TestAdminAddMonthlyBonus_AddsToCurrentWindowBonus(t *testing.T) {
	stub := &bonusQuotaUserSubRepoStub{
		sub: &UserSubscription{
			ID:              1,
			UserID:          10,
			GroupID:         20,
			MonthlyBonusUSD: 25,
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, stub, nil, nil, nil)

	result, err := svc.AdminAddMonthlyBonus(context.Background(), 1, 100)

	require.NoError(t, err)
	require.True(t, stub.addMonthlyBonusCalled)
	require.Equal(t, 100.0, stub.addMonthlyBonusAmount)
	require.Equal(t, 125.0, result.MonthlyBonusUSD)
}

func TestAdminAddMonthlyBonus_RejectsNonPositiveAmount(t *testing.T) {
	stub := &bonusQuotaUserSubRepoStub{
		sub: &UserSubscription{ID: 1, UserID: 10, GroupID: 20},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, stub, nil, nil, nil)

	_, err := svc.AdminAddMonthlyBonus(context.Background(), 1, 0)

	require.ErrorIs(t, err, ErrInvalidInput)
	require.False(t, stub.addMonthlyBonusCalled)
}

func TestUserSubscriptionCheckMonthlyLimit_UsesCurrentWindowBonus(t *testing.T) {
	monthlyLimit := 100.0
	sub := &UserSubscription{
		MonthlyUsageUSD: 150,
		MonthlyBonusUSD: 100,
	}
	group := &Group{MonthlyLimitUSD: &monthlyLimit}

	require.True(t, sub.CheckMonthlyLimit(group, 0))
	require.True(t, sub.CheckMonthlyLimit(group, 50))
	require.False(t, sub.CheckMonthlyLimit(group, 50.01))
}
