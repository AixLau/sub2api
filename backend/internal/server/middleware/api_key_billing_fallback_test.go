package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestValidateRequestSubscriptionActivatesQueuedRenewalAndUpdatesGroup(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_fk=1", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	oldLimit := 100.0
	newLimit := 300.0
	user := client.User.Create().
		SetEmail("queued-renewal@example.com").
		SetPasswordHash("hash").
		SetStatus(service.StatusActive).
		SetRole(service.RoleUser).
		SaveX(ctx)
	oldGroup := client.Group.Create().
		SetName("old subscription").
		SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetStatus(service.StatusActive).
		SetMonthlyLimitUsd(oldLimit).
		SaveX(ctx)
	newGroup := client.Group.Create().
		SetName("new subscription").
		SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetStatus(service.StatusActive).
		SetMonthlyLimitUsd(newLimit).
		SaveX(ctx)

	now := time.Now()
	windowStart := now.AddDate(0, 0, -1)
	subEntity := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(oldGroup.ID).
		SetStartsAt(now.AddDate(0, 0, -10)).
		SetExpiresAt(now.AddDate(0, 0, 20)).
		SetStatus(service.SubscriptionStatusActive).
		SetMonthlyWindowStart(windowStart).
		SetMonthlyUsageUsd(oldLimit).
		SaveX(ctx)
	apiKey := client.APIKey.Create().
		SetUserID(user.ID).
		SetGroupID(oldGroup.ID).
		SetKey("queued-renewal-key").
		SetName("queued renewal key").
		SetStatus(service.StatusActive).
		SaveX(ctx)
	client.SubscriptionRenewal.Create().
		SetSubscriptionID(subEntity.ID).
		SetUserID(user.ID).
		SetTargetGroupID(newGroup.ID).
		SetSourceType("payment_order").
		SetSourceID("middleware-activation").
		SetValidityDays(30).
		SetMonthlyLimitUsd(newLimit).
		SetStatus("pending").
		SaveX(ctx)

	groupRepo := repository.NewGroupRepository(client, db)
	subRepo := repository.NewUserSubscriptionRepository(client)
	subscriptionService := service.NewSubscriptionService(groupRepo, subRepo, nil, client, nil)
	sub, err := subscriptionService.GetActiveSubscription(ctx, user.ID, oldGroup.ID)
	require.NoError(t, err)
	group, err := groupRepo.GetByID(ctx, oldGroup.ID)
	require.NoError(t, err)

	refreshed, err := validateRequestSubscription(ctx, subscriptionService, sub, group)
	require.NoError(t, err)
	require.Equal(t, newGroup.ID, refreshed.GroupID)
	require.Equal(t, newGroup.ID, group.ID)
	require.InDelta(t, 0, refreshed.MonthlyUsageUSD, 0.001)
	require.Equal(t, 0, refreshed.PendingRenewalCount)
	require.InDelta(t, 30*24, time.Until(refreshed.ExpiresAt).Hours(), 0.1)

	migratedKey := client.APIKey.GetX(ctx, apiKey.ID)
	require.NotNil(t, migratedKey.GroupID)
	require.Equal(t, newGroup.ID, *migratedKey.GroupID)
	count, err := client.SubscriptionRenewal.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	activated, err := client.SubscriptionRenewal.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "activated", activated.Status)
	require.NotNil(t, activated.ActivatedAt)
}
