package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestMergeBalanceHistoryCodesIncludesAffiliateTransfersByDefault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(time.Hour)

	usedBy := int64(10)
	redeemCodes := []RedeemCode{
		{
			ID:        1,
			Type:      RedeemTypeBalance,
			Value:     8,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &now,
			CreatedAt: now,
		},
		{
			ID:        2,
			Type:      RedeemTypeConcurrency,
			Value:     1,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &older,
			CreatedAt: older,
		},
	}
	affiliateCodes := []RedeemCode{
		{
			ID:        -20,
			Type:      RedeemTypeAffiliateBalance,
			Value:     3.5,
			Status:    StatusUsed,
			UsedBy:    &usedBy,
			UsedAt:    &newer,
			CreatedAt: newer,
		},
	}

	got := mergeBalanceHistoryCodes(redeemCodes, affiliateCodes, pagination.PaginationParams{
		Page:     1,
		PageSize: 2,
	})

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeAffiliateBalance, got[0].Type)
	require.Equal(t, RedeemTypeBalance, got[1].Type)
}

func TestMergeBalanceHistoryCodesPaginatesAfterCombiningSources(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	usedBy := int64(10)
	at := func(hours int) *time.Time {
		v := base.Add(time.Duration(hours) * time.Hour)
		return &v
	}

	got := mergeBalanceHistoryCodes(
		[]RedeemCode{
			{ID: 1, Type: RedeemTypeBalance, UsedBy: &usedBy, UsedAt: at(4), CreatedAt: *at(4)},
			{ID: 2, Type: RedeemTypeConcurrency, UsedBy: &usedBy, UsedAt: at(2), CreatedAt: *at(2)},
		},
		[]RedeemCode{
			{ID: -3, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(3), CreatedAt: *at(3)},
			{ID: -4, Type: RedeemTypeAffiliateBalance, UsedBy: &usedBy, UsedAt: at(1), CreatedAt: *at(1)},
		},
		pagination.PaginationParams{Page: 2, PageSize: 2},
	)

	require.Len(t, got, 2)
	require.Equal(t, RedeemTypeConcurrency, got[0].Type)
	require.Equal(t, int64(-4), got[1].ID)
}

func TestUserSubscriptionHistoryCodeIncludesSubscriptionDetails(t *testing.T) {
	t.Parallel()

	startsAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	assignedAt := startsAt.Add(-time.Hour)
	expiresAt := startsAt.AddDate(0, 0, 30)
	deletedAt := startsAt.AddDate(0, 0, 10)
	sub := &UserSubscription{
		ID:         42,
		UserID:     10,
		GroupID:    7,
		StartsAt:   startsAt,
		ExpiresAt:  expiresAt,
		Status:     SubscriptionStatusActive,
		AssignedAt: assignedAt,
		Notes:      "manual assignment",
		CreatedAt:  assignedAt,
		DeletedAt:  &deletedAt,
		Group:      &Group{ID: 7, Name: "Pro"},
	}

	got := userSubscriptionHistoryCode(sub)

	require.Equal(t, int64(42), got.ID)
	require.Equal(t, RedeemTypeSubscription, got.Type)
	require.Equal(t, SubscriptionStatusRevoked, got.Status)
	require.Equal(t, 30, got.ValidityDays)
	require.Equal(t, float64(30), got.Value)
	require.Equal(t, "manual assignment", got.Notes)
	require.Equal(t, assignedAt, *got.UsedAt)
	require.Equal(t, expiresAt, *got.ExpiresAt)
	require.Equal(t, "Pro", got.Group.Name)
}

func TestMergeUserHistoryCodesIncludesSubscriptionsInChronologicalOrder(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	at := func(hours int) *time.Time {
		value := base.Add(time.Duration(hours) * time.Hour)
		return &value
	}

	got := mergeUserHistoryCodes(
		pagination.PaginationParams{Page: 1, PageSize: 3},
		[]RedeemCode{{ID: 1, Type: RedeemTypeBalance, UsedAt: at(1), CreatedAt: *at(1)}},
		[]RedeemCode{{ID: -2, Type: RedeemTypeAffiliateBalance, UsedAt: at(3), CreatedAt: *at(3)}},
		[]RedeemCode{{ID: 3, Type: RedeemTypeSubscription, UsedAt: at(2), CreatedAt: *at(2)}},
	)

	require.Len(t, got, 3)
	require.Equal(t, RedeemTypeAffiliateBalance, got[0].Type)
	require.Equal(t, RedeemTypeSubscription, got[1].Type)
	require.Equal(t, RedeemTypeBalance, got[2].Type)
}
