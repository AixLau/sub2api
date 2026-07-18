package middleware

import (
	"context"
	"errors"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func validateRequestSubscription(ctx context.Context, subscriptionService *service.SubscriptionService, sub *service.UserSubscription, group *service.Group) (*service.UserSubscription, error) {
	if subscriptionService == nil || sub == nil || group == nil {
		return nil, service.ErrSubscriptionNotFound
	}

	needsMaintenance, err := subscriptionService.ValidateAndCheckLimits(sub, group)
	if needsMaintenance {
		refreshed, maintenanceErr := subscriptionService.EnsureWindowMaintenance(ctx, sub)
		if maintenanceErr != nil {
			return nil, maintenanceErr
		}
		sub = refreshed
		if sub.Group != nil && sub.GroupID != group.ID {
			// FIFO renewal activation may switch tiers. The caller passes the API
			// key's group pointer, so updating it keeps this triggering request on
			// the newly activated tier after the DB/API-key migration commits.
			*group = *sub.Group
		}
		_, err = subscriptionService.ValidateAndCheckLimits(sub, group)
	}
	if err != nil {
		return nil, err
	}
	if group.HasDailyLimit() && sub.DailyUsageUSD >= *group.DailyLimitUSD {
		return nil, service.ErrDailyLimitExceeded
	}
	if group.HasWeeklyLimit() && sub.WeeklyUsageUSD >= *group.WeeklyLimitUSD {
		return nil, service.ErrWeeklyLimitExceeded
	}
	if group.HasMonthlyLimit() && sub.MonthlyUsageUSD >= *group.MonthlyLimitUSD+sub.MonthlyBonusUSD {
		return nil, service.ErrMonthlyLimitExceeded
	}
	return sub, nil
}

func canFallbackFromSubscriptionError(err error) bool {
	return errors.Is(err, service.ErrSubscriptionNotFound) ||
		errors.Is(err, service.ErrSubscriptionExpired) ||
		errors.Is(err, service.ErrSubscriptionSuspended) ||
		errors.Is(err, service.ErrDailyLimitExceeded) ||
		errors.Is(err, service.ErrWeeklyLimitExceeded) ||
		errors.Is(err, service.ErrMonthlyLimitExceeded)
}

func findFallbackSubscription(ctx context.Context, subscriptionService *service.SubscriptionService, apiKey *service.APIKey, excludedGroupID int64) (*service.APIKey, *service.UserSubscription, error) {
	if subscriptionService == nil || apiKey == nil || apiKey.User == nil || apiKey.Group == nil {
		return nil, nil, service.ErrSubscriptionNotFound
	}

	subs, err := subscriptionService.ListActiveUserSubscriptions(ctx, apiKey.User.ID)
	if err != nil {
		return nil, nil, err
	}
	sort.SliceStable(subs, func(i, j int) bool {
		left, right := subs[i], subs[j]
		if left.Group != nil && right.Group != nil && left.Group.SortOrder != right.Group.SortOrder {
			return left.Group.SortOrder < right.Group.SortOrder
		}
		if left.GroupID != right.GroupID {
			return left.GroupID < right.GroupID
		}
		return left.ID < right.ID
	})

	for i := range subs {
		candidate := &subs[i]
		group := candidate.Group
		if candidate.GroupID == excludedGroupID || group == nil || !group.IsActive() ||
			!group.IsSubscriptionType() || group.Platform != apiKey.Group.Platform {
			continue
		}

		validated, validateErr := validateRequestSubscription(ctx, subscriptionService, candidate, group)
		if validateErr != nil {
			continue
		}
		return cloneAPIKeyForBillingGroup(apiKey, group), validated, nil
	}

	return nil, nil, service.ErrSubscriptionNotFound
}

func cloneAPIKeyForBillingGroup(apiKey *service.APIKey, group *service.Group) *service.APIKey {
	cloned := *apiKey
	groupID := group.ID
	cloned.GroupID = &groupID
	cloned.Group = group
	if apiKey.User != nil {
		user := *apiKey.User
		user.UserGroupRPMOverride = nil
		cloned.User = &user
	}
	return &cloned
}
