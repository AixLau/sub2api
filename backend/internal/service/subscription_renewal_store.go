package service

import (
	"context"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionrenewal"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
)

const (
	subscriptionRenewalPending   = "pending"
	subscriptionRenewalActivated = "activated"
)

type subscriptionRenewalRequest struct {
	SubscriptionID  int64
	UserID          int64
	TargetGroupID   int64
	PlanID          *int64
	SourceType      string
	SourceID        string
	ValidityDays    int
	MonthlyLimitUSD float64
	Notes           string
}

type subscriptionRenewalActivation struct {
	RenewalID    int64
	UserID       int64
	OldGroupID   int64
	NewGroupID   int64
	ValidityDays int
}

type subscriptionRenewalStore interface {
	Enqueue(context.Context, subscriptionRenewalRequest) error
	PendingCount(context.Context, int64) (int, error)
	ActivateNext(context.Context, int64, time.Time, time.Time) (*subscriptionRenewalActivation, error)
	ReassignPending(context.Context, []int64, int64) error
}

type entSubscriptionRenewalStore struct {
	client *dbent.Client
}

func newEntSubscriptionRenewalStore(client *dbent.Client) subscriptionRenewalStore {
	if client == nil {
		return nil
	}
	return &entSubscriptionRenewalStore{client: client}
}

func (s *entSubscriptionRenewalStore) clientForContext(ctx context.Context) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return s.client
}

func (s *entSubscriptionRenewalStore) Enqueue(ctx context.Context, req subscriptionRenewalRequest) error {
	create := s.clientForContext(ctx).SubscriptionRenewal.Create().
		SetSubscriptionID(req.SubscriptionID).
		SetUserID(req.UserID).
		SetTargetGroupID(req.TargetGroupID).
		SetNillablePlanID(req.PlanID).
		SetSourceType(req.SourceType).
		SetSourceID(req.SourceID).
		SetValidityDays(req.ValidityDays).
		SetMonthlyLimitUsd(req.MonthlyLimitUSD).
		SetStatus(subscriptionRenewalPending)
	if req.Notes != "" {
		create.SetNotes(req.Notes)
	}
	_, err := create.Save(ctx)
	return err
}

func (s *entSubscriptionRenewalStore) PendingCount(ctx context.Context, subscriptionID int64) (int, error) {
	return s.clientForContext(ctx).SubscriptionRenewal.Query().
		Where(
			subscriptionrenewal.SubscriptionIDEQ(subscriptionID),
			subscriptionrenewal.StatusEQ(subscriptionRenewalPending),
		).
		Count(ctx)
}

func (s *entSubscriptionRenewalStore) ActivateNext(ctx context.Context, subscriptionID int64, startsAt, windowStart time.Time) (*subscriptionRenewalActivation, error) {
	if existingTx := dbent.TxFromContext(ctx); existingTx != nil {
		return activateNextRenewal(ctx, existingTx.Client(), subscriptionID, startsAt, windowStart)
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	activation, err := activateNextRenewal(txCtx, tx.Client(), subscriptionID, startsAt, windowStart)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return activation, nil
}

func activateNextRenewal(ctx context.Context, client *dbent.Client, subscriptionID int64, startsAt, windowStart time.Time) (*subscriptionRenewalActivation, error) {
	sub, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		Only(ctx)
	if err != nil {
		return nil, err
	}
	renewal, err := client.SubscriptionRenewal.Query().
		Where(
			subscriptionrenewal.SubscriptionIDEQ(subscriptionID),
			subscriptionrenewal.StatusEQ(subscriptionRenewalPending),
		).
		Order(dbent.Asc(subscriptionrenewal.FieldID)).
		First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	expiresAt := startsAt.AddDate(0, 0, renewal.ValidityDays)
	// The status predicate is the concurrency guard: competing activations can
	// read the same FIFO row, but only one transaction can flip it from pending.
	updated, err := client.SubscriptionRenewal.Update().
		Where(
			subscriptionrenewal.IDEQ(renewal.ID),
			subscriptionrenewal.StatusEQ(subscriptionRenewalPending),
		).
		SetStatus(subscriptionRenewalActivated).
		SetActivatedAt(startsAt).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("mark queued subscription activated: %w", err)
	}
	if updated == 0 {
		return nil, nil
	}
	_, err = client.UserSubscription.UpdateOneID(subscriptionID).
		SetGroupID(renewal.TargetGroupID).
		SetStartsAt(startsAt).
		SetExpiresAt(expiresAt).
		SetStatus(SubscriptionStatusActive).
		SetDailyWindowStart(windowStart).
		SetWeeklyWindowStart(windowStart).
		SetMonthlyWindowStart(windowStart).
		SetDailyUsageUsd(0).
		SetWeeklyUsageUsd(0).
		SetMonthlyUsageUsd(0).
		SetMonthlyBonusUsd(0).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("activate queued subscription: %w", err)
	}
	if sub.GroupID != renewal.TargetGroupID {
		if _, err := client.APIKey.Update().
			Where(apikey.UserIDEQ(sub.UserID), apikey.GroupIDEQ(sub.GroupID)).
			SetGroupID(renewal.TargetGroupID).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("migrate queued subscription api keys: %w", err)
		}
	}
	return &subscriptionRenewalActivation{
		RenewalID:    renewal.ID,
		UserID:       sub.UserID,
		OldGroupID:   sub.GroupID,
		NewGroupID:   renewal.TargetGroupID,
		ValidityDays: renewal.ValidityDays,
	}, nil
}

func (s *entSubscriptionRenewalStore) ReassignPending(ctx context.Context, oldSubscriptionIDs []int64, newSubscriptionID int64) error {
	if len(oldSubscriptionIDs) == 0 {
		return nil
	}
	_, err := s.clientForContext(ctx).SubscriptionRenewal.Update().
		Where(
			subscriptionrenewal.SubscriptionIDIn(oldSubscriptionIDs...),
			subscriptionrenewal.StatusEQ(subscriptionRenewalPending),
		).
		SetSubscriptionID(newSubscriptionID).
		Save(ctx)
	return err
}
