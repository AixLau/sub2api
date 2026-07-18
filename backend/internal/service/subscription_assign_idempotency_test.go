package service

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/dgraph-io/ristretto"
	"github.com/stretchr/testify/require"
)

func TestWithSubscriptionUpdateTx_ReusesExistingTransaction(t *testing.T) {
	existingTx := &dbent.Tx{}
	ctx := dbent.NewTxContext(context.Background(), existingTx)
	svc := &SubscriptionService{entClient: &dbent.Client{}}

	called := false
	err := svc.withSubscriptionUpdateTx(ctx, func(txCtx context.Context) error {
		called = true
		require.Same(t, existingTx, dbent.TxFromContext(txCtx))
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
}

func TestMaybeInvalidateAssignmentCaches_DefersForOuterTransactionOwner(t *testing.T) {
	cache, err := ristretto.NewCache(&ristretto.Config{NumCounters: 1_000, MaxCost: 100, BufferItems: 64})
	require.NoError(t, err)
	t.Cleanup(cache.Close)

	svc := &SubscriptionService{subCacheL1: cache}
	key := subCacheKey(7, 9)
	require.True(t, cache.Set(key, &UserSubscription{ID: 42}, 1))
	cache.Wait()

	svc.maybeInvalidateAssignmentCaches(7, 9, true)
	_, cachedBeforeCommit := cache.Get(key)
	require.True(t, cachedBeforeCommit, "outer transaction must retain caches until its owner commits")

	svc.maybeInvalidateAssignmentCaches(7, 9, false)
	cache.Wait()
	_, cachedAfterCommit := cache.Get(key)
	require.False(t, cachedAfterCommit, "post-commit invalidation must remove the cached subscription")
}

type groupRepoNoop struct{}

func (groupRepoNoop) Create(context.Context, *Group) error { panic("unexpected Create call") }
func (groupRepoNoop) GetByID(context.Context, int64) (*Group, error) {
	panic("unexpected GetByID call")
}
func (groupRepoNoop) GetByIDLite(context.Context, int64) (*Group, error) {
	panic("unexpected GetByIDLite call")
}
func (groupRepoNoop) Update(context.Context, *Group) error { panic("unexpected Update call") }
func (groupRepoNoop) Delete(context.Context, int64) error  { panic("unexpected Delete call") }
func (groupRepoNoop) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected DeleteCascade call")
}
func (groupRepoNoop) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (groupRepoNoop) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}
func (groupRepoNoop) ListActive(context.Context) ([]Group, error) {
	panic("unexpected ListActive call")
}
func (groupRepoNoop) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected ListActiveByPlatform call")
}
func (groupRepoNoop) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected ExistsByName call")
}
func (groupRepoNoop) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected GetAccountCount call")
}
func (groupRepoNoop) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected DeleteAccountGroupsByGroupID call")
}
func (groupRepoNoop) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected GetAccountIDsByGroupIDs call")
}
func (groupRepoNoop) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected BindAccountsToGroup call")
}
func (groupRepoNoop) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected UpdateSortOrders call")
}

type subscriptionGroupRepoStub struct {
	groupRepoNoop
	group *Group
}

func (s *subscriptionGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	return s.group, nil
}

type subscriptionGroupRepoMapStub struct {
	groupRepoNoop
	groups map[int64]*Group
}

func (s *subscriptionGroupRepoMapStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if s == nil || s.groups == nil {
		return nil, ErrGroupNotFound
	}
	group := s.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cp := *group
	return &cp, nil
}

type userSubRepoNoop struct{}

func (userSubRepoNoop) Create(context.Context, *UserSubscription) error {
	panic("unexpected Create call")
}
func (userSubRepoNoop) GetByID(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByID call")
}
func (userSubRepoNoop) GetByIDIncludeDeleted(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByIDIncludeDeleted call")
}
func (userSubRepoNoop) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDAndGroupID call")
}
func (userSubRepoNoop) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetActiveByUserIDAndGroupID call")
}
func (userSubRepoNoop) Update(context.Context, *UserSubscription) error {
	panic("unexpected Update call")
}
func (userSubRepoNoop) Delete(context.Context, int64) error { panic("unexpected Delete call") }
func (userSubRepoNoop) Restore(context.Context, int64, string) (*UserSubscription, error) {
	panic("unexpected Restore call")
}
func (userSubRepoNoop) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListByUserID call")
}
func (userSubRepoNoop) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListActiveByUserID call")
}
func (userSubRepoNoop) ListActiveByUserIDPlatformSubscriptionType(context.Context, int64, string, string) ([]UserSubscription, error) {
	panic("unexpected ListActiveByUserIDPlatformSubscriptionType call")
}
func (userSubRepoNoop) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected ListByGroupID call")
}
func (userSubRepoNoop) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (userSubRepoNoop) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsByUserIDAndGroupID call")
}
func (userSubRepoNoop) ExistsActiveByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsActiveByUserIDAndGroupID call")
}
func (userSubRepoNoop) ExtendExpiry(context.Context, int64, time.Time) error {
	panic("unexpected ExtendExpiry call")
}
func (userSubRepoNoop) UpdateStatus(context.Context, int64, string) error {
	panic("unexpected UpdateStatus call")
}
func (userSubRepoNoop) UpdateNotes(context.Context, int64, string) error {
	panic("unexpected UpdateNotes call")
}
func (userSubRepoNoop) ActivateWindows(context.Context, int64, time.Time) error {
	panic("unexpected ActivateWindows call")
}
func (userSubRepoNoop) ResetUsageWindows(context.Context, int64, bool, bool, bool, time.Time) error {
	panic("unexpected ResetUsageWindows call")
}
func (userSubRepoNoop) ResetDailyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetDailyUsage call")
}
func (userSubRepoNoop) ResetWeeklyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetWeeklyUsage call")
}
func (userSubRepoNoop) ResetMonthlyUsage(context.Context, int64, *time.Time, time.Time) error {
	panic("unexpected ResetMonthlyUsage call")
}
func (userSubRepoNoop) AddMonthlyBonus(context.Context, int64, float64) error {
	panic("unexpected AddMonthlyBonus call")
}
func (userSubRepoNoop) IncrementUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementUsage call")
}
func (userSubRepoNoop) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	panic("unexpected BatchUpdateExpiredStatus call")
}

type subscriptionUserSubRepoStub struct {
	userSubRepoNoop

	nextID      int64
	byID        map[int64]*UserSubscription
	byUserGroup map[string]*UserSubscription
	createCalls int
}

func newSubscriptionUserSubRepoStub() *subscriptionUserSubRepoStub {
	return &subscriptionUserSubRepoStub{
		nextID:      1,
		byID:        make(map[int64]*UserSubscription),
		byUserGroup: make(map[string]*UserSubscription),
	}
}

func (s *subscriptionUserSubRepoStub) key(userID, groupID int64) string {
	return strconvFormatInt(userID) + ":" + strconvFormatInt(groupID)
}

func (s *subscriptionUserSubRepoStub) seed(sub *UserSubscription) {
	if sub == nil {
		return
	}
	cp := *sub
	if cp.ID == 0 {
		cp.ID = s.nextID
		s.nextID++
	}
	s.byID[cp.ID] = &cp
	s.byUserGroup[s.key(cp.UserID, cp.GroupID)] = &cp
}

func (s *subscriptionUserSubRepoStub) ExistsByUserIDAndGroupID(_ context.Context, userID, groupID int64) (bool, error) {
	_, ok := s.byUserGroup[s.key(userID, groupID)]
	return ok, nil
}

func (s *subscriptionUserSubRepoStub) GetByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	sub := s.byUserGroup[s.key(userID, groupID)]
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	sub := s.byUserGroup[s.key(userID, groupID)]
	if sub == nil || sub.Status != SubscriptionStatusActive || !sub.ExpiresAt.After(time.Now()) {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) ListActiveByUserIDPlatformSubscriptionType(_ context.Context, userID int64, platform, subscriptionType string) ([]UserSubscription, error) {
	var out []UserSubscription
	for _, sub := range s.byID {
		if sub == nil || sub.UserID != userID || sub.Group == nil {
			continue
		}
		if sub.Status != SubscriptionStatusActive && sub.Status != SubscriptionStatusExpired {
			continue
		}
		if sub.Group.Platform != platform || sub.Group.SubscriptionType != subscriptionType {
			continue
		}
		cp := *sub
		group := *sub.Group
		cp.Group = &group
		out = append(out, cp)
	}
	return out, nil
}

func (s *subscriptionUserSubRepoStub) Create(_ context.Context, sub *UserSubscription) error {
	if sub == nil {
		return nil
	}
	s.createCalls++
	cp := *sub
	if cp.ID == 0 {
		cp.ID = s.nextID
		s.nextID++
	}
	sub.ID = cp.ID
	s.byID[cp.ID] = &cp
	s.byUserGroup[s.key(cp.UserID, cp.GroupID)] = &cp
	return nil
}

func (s *subscriptionUserSubRepoStub) Delete(_ context.Context, id int64) error {
	sub := s.byID[id]
	if sub == nil {
		return ErrSubscriptionNotFound
	}
	delete(s.byID, id)
	delete(s.byUserGroup, s.key(sub.UserID, sub.GroupID))
	return nil
}

type renewalStoreStubRow struct {
	ID     int64
	Req    subscriptionRenewalRequest
	Status string
}

type subscriptionRenewalStoreStub struct {
	mu         sync.Mutex
	nextID     int64
	rows       []renewalStoreStubRow
	subRepo    *subscriptionUserSubRepoStub
	groups     map[int64]*Group
	activated  []int64
	reassigned []struct {
		OldIDs []int64
		NewID  int64
	}
}

func newSubscriptionRenewalStoreStub(subRepo *subscriptionUserSubRepoStub, groups map[int64]*Group) *subscriptionRenewalStoreStub {
	return &subscriptionRenewalStoreStub{
		nextID:  1,
		subRepo: subRepo,
		groups:  groups,
	}
}

func (s *subscriptionRenewalStoreStub) Enqueue(_ context.Context, req subscriptionRenewalRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := renewalStoreStubRow{
		ID:     s.nextID,
		Req:    req,
		Status: subscriptionRenewalPending,
	}
	s.nextID++
	s.rows = append(s.rows, row)
	return nil
}

func (s *subscriptionRenewalStoreStub) PendingCount(_ context.Context, subscriptionID int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, row := range s.rows {
		if row.Req.SubscriptionID == subscriptionID && row.Status == subscriptionRenewalPending {
			count++
		}
	}
	return count, nil
}

func (s *subscriptionRenewalStoreStub) ActivateNext(ctx context.Context, subscriptionID int64, startsAt, windowStart time.Time) (*subscriptionRenewalActivation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.rows {
		row := &s.rows[i]
		if row.Req.SubscriptionID != subscriptionID || row.Status != subscriptionRenewalPending {
			continue
		}
		sub, err := s.subRepo.GetByID(ctx, subscriptionID)
		if err != nil {
			return nil, err
		}
		oldGroupID := sub.GroupID
		expiresAt := startsAt.AddDate(0, 0, row.Req.ValidityDays)
		sub.GroupID = row.Req.TargetGroupID
		if group := s.groups[row.Req.TargetGroupID]; group != nil {
			groupCopy := *group
			sub.Group = &groupCopy
		}
		sub.StartsAt = startsAt
		sub.ExpiresAt = expiresAt
		sub.Status = SubscriptionStatusActive
		sub.DailyWindowStart = &windowStart
		sub.WeeklyWindowStart = &windowStart
		sub.MonthlyWindowStart = &windowStart
		sub.DailyUsageUSD = 0
		sub.WeeklyUsageUSD = 0
		sub.MonthlyUsageUSD = 0
		sub.MonthlyBonusUSD = 0
		if err := s.subRepo.Update(ctx, sub); err != nil {
			return nil, err
		}
		row.Status = subscriptionRenewalActivated
		s.activated = append(s.activated, row.ID)
		return &subscriptionRenewalActivation{
			RenewalID:    row.ID,
			UserID:       sub.UserID,
			OldGroupID:   oldGroupID,
			NewGroupID:   row.Req.TargetGroupID,
			ValidityDays: row.Req.ValidityDays,
		}, nil
	}
	return nil, nil
}

func (s *subscriptionRenewalStoreStub) ReassignPending(_ context.Context, oldSubscriptionIDs []int64, newSubscriptionID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldSet := make(map[int64]struct{}, len(oldSubscriptionIDs))
	for _, id := range oldSubscriptionIDs {
		oldSet[id] = struct{}{}
	}
	for i := range s.rows {
		if s.rows[i].Status != subscriptionRenewalPending {
			continue
		}
		if _, ok := oldSet[s.rows[i].Req.SubscriptionID]; ok {
			s.rows[i].Req.SubscriptionID = newSubscriptionID
		}
	}
	copiedOldIDs := append([]int64(nil), oldSubscriptionIDs...)
	s.reassigned = append(s.reassigned, struct {
		OldIDs []int64
		NewID  int64
	}{OldIDs: copiedOldIDs, NewID: newSubscriptionID})
	return nil
}

func (s *subscriptionRenewalStoreStub) pendingRows(subscriptionID int64) []renewalStoreStubRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []renewalStoreStubRow
	for _, row := range s.rows {
		if row.Req.SubscriptionID == subscriptionID && row.Status == subscriptionRenewalPending {
			out = append(out, row)
		}
	}
	return out
}

func (s *subscriptionUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	sub := s.byID[id]
	if sub == nil {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

func (s *subscriptionUserSubRepoStub) Update(_ context.Context, sub *UserSubscription) error {
	if sub == nil {
		return ErrSubscriptionNilInput
	}
	existing := s.byID[sub.ID]
	if existing == nil {
		return ErrSubscriptionNotFound
	}
	oldKey := s.key(existing.UserID, existing.GroupID)
	cp := *sub
	s.byID[cp.ID] = &cp
	if oldKey != s.key(cp.UserID, cp.GroupID) {
		delete(s.byUserGroup, oldKey)
	}
	s.byUserGroup[s.key(cp.UserID, cp.GroupID)] = &cp
	return nil
}

func TestAssignSubscriptionReuseWhenSemanticsMatch(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        10,
		UserID:    1001,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "init",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "init",
	})
	require.NoError(t, err)
	require.Equal(t, int64(10), sub.ID)
	require.Equal(t, 0, subRepo.createCalls, "reuse should not create new subscription")
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, start.AddDate(0, 0, 30), sub.ExpiresAt)
}

func TestAssignSubscriptionDoesNotReactivateFutureSuspendedSubscription(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        13,
		UserID:    1003,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusSuspended,
		Notes:     "assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1003,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "assignment",
	})

	require.NoError(t, err)
	require.Equal(t, int64(13), sub.ID)
	require.Equal(t, SubscriptionStatusSuspended, sub.Status)
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, start.AddDate(0, 0, 30), sub.ExpiresAt)
	require.Equal(t, "assignment", sub.Notes)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignSubscriptionDoesNotReactivatePastExpirySuspendedSubscription(t *testing.T) {
	start := time.Now().AddDate(0, 0, -31)
	expiresAt := start.AddDate(0, 0, 30)
	windowStart := startOfDay(start)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 15,
		UserID:             1005,
		GroupID:            1,
		StartsAt:           start,
		ExpiresAt:          expiresAt,
		Status:             SubscriptionStatusSuspended,
		DailyWindowStart:   &windowStart,
		WeeklyWindowStart:  &windowStart,
		MonthlyWindowStart: &windowStart,
		DailyUsageUSD:      1,
		WeeklyUsageUSD:     2,
		MonthlyUsageUSD:    3,
		Notes:              "suspended assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1005,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "suspended assignment",
	})

	require.NoError(t, err)
	require.Equal(t, int64(15), sub.ID)
	require.Equal(t, SubscriptionStatusSuspended, sub.Status)
	require.Equal(t, start, sub.StartsAt)
	require.Equal(t, expiresAt, sub.ExpiresAt)
	require.Equal(t, "suspended assignment", sub.Notes)
	require.Equal(t, &windowStart, sub.DailyWindowStart)
	require.Equal(t, &windowStart, sub.WeeklyWindowStart)
	require.Equal(t, &windowStart, sub.MonthlyWindowStart)
	require.Equal(t, float64(1), sub.DailyUsageUSD)
	require.Equal(t, float64(2), sub.WeeklyUsageUSD)
	require.Equal(t, float64(3), sub.MonthlyUsageUSD)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignSubscriptionRenewsExpiredSemanticMatch(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Now().Add(-time.Hour)
	oldWindowStart := startOfDay(oldStart)
	subRepo.seed(&UserSubscription{
		ID:                 12,
		UserID:             1002,
		GroupID:            1,
		StartsAt:           oldStart,
		ExpiresAt:          oldStart.AddDate(0, 0, 30),
		Status:             SubscriptionStatusExpired,
		DailyWindowStart:   &oldWindowStart,
		WeeklyWindowStart:  &oldWindowStart,
		MonthlyWindowStart: &oldWindowStart,
		DailyUsageUSD:      1,
		WeeklyUsageUSD:     2,
		MonthlyUsageUSD:    3,
		Notes:              " assignment ",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	before := time.Now()
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1002,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "assignment",
	})
	after := time.Now()

	require.NoError(t, err)
	require.Equal(t, int64(12), sub.ID)
	require.Equal(t, 0, subRepo.createCalls)
	require.Equal(t, SubscriptionStatusActive, sub.Status)
	require.False(t, sub.StartsAt.Before(before))
	require.False(t, sub.StartsAt.After(after))
	require.Equal(t, sub.StartsAt.AddDate(0, 0, 30), sub.ExpiresAt)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.DailyWindowStart)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.WeeklyWindowStart)
	require.Equal(t, startOfDay(sub.StartsAt), *sub.MonthlyWindowStart)
	require.Zero(t, sub.DailyUsageUSD)
	require.Zero(t, sub.WeeklyUsageUSD)
	require.Zero(t, sub.MonthlyUsageUSD)
	require.Equal(t, " assignment ", sub.Notes)
}

func TestAssignSubscriptionRenewsExpiredAndAppendsDifferentNotes(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	subRepo.seed(&UserSubscription{
		ID:        14,
		UserID:    1004,
		GroupID:   1,
		StartsAt:  oldStart,
		ExpiresAt: oldStart.AddDate(0, 0, 30),
		Status:    SubscriptionStatusExpired,
		Notes:     "old assignment",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1004,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new assignment",
	})

	require.NoError(t, err)
	require.Equal(t, "old assignment\nnew assignment", sub.Notes)
}

func TestAssignSubscriptionConflictWhenSemanticsMismatch(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        11,
		UserID:    2001,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "old-note",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       2001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new-note",
	})
	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_ASSIGN_CONFLICT", infraerrorsReason(err))
	require.Equal(t, 0, subRepo.createCalls, "conflict should not create or mutate existing subscription")
}

func TestBulkAssignSubscriptionCreatedReusedAndConflict(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	// user 1: 语义一致，可 reused
	subRepo.seed(&UserSubscription{
		ID:        21,
		UserID:    1,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Status:    SubscriptionStatusActive,
		Notes:     "same-note",
	})
	// user 3: 语义冲突（有效期不一致），应 failed
	subRepo.seed(&UserSubscription{
		ID:        23,
		UserID:    3,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 60),
		Status:    SubscriptionStatusActive,
		Notes:     "same-note",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	result, err := svc.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs:      []int64{1, 2, 3},
		GroupID:      1,
		ValidityDays: 30,
		AssignedBy:   9,
		Notes:        "same-note",
	})
	require.NoError(t, err)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 1, result.CreatedCount)
	require.Equal(t, 1, result.ReusedCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, "reused", result.Statuses[1])
	require.Equal(t, "created", result.Statuses[2])
	require.Equal(t, "failed", result.Statuses[3])
	require.Equal(t, 1, subRepo.createCalls)
}

func TestBulkAssignSubscriptionRenewsExpiredSemanticMatch(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	oldStart := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	subRepo.seed(&UserSubscription{
		ID:              24,
		UserID:          4,
		GroupID:         1,
		StartsAt:        oldStart,
		ExpiresAt:       oldStart.AddDate(0, 0, 7),
		Status:          SubscriptionStatusExpired,
		DailyUsageUSD:   1,
		WeeklyUsageUSD:  2,
		MonthlyUsageUSD: 3,
		Notes:           "bulk",
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	before := time.Now()
	result, err := svc.BulkAssignSubscription(context.Background(), &BulkAssignSubscriptionInput{
		UserIDs:      []int64{4},
		GroupID:      1,
		ValidityDays: 7,
		Notes:        "bulk",
	})
	after := time.Now()

	require.NoError(t, err)
	require.Equal(t, 1, result.SuccessCount)
	require.Equal(t, 0, result.CreatedCount)
	require.Equal(t, 1, result.ReusedCount)
	require.Equal(t, "reused", result.Statuses[4])
	require.Len(t, result.Subscriptions, 1)
	renewed := result.Subscriptions[0]
	require.Equal(t, SubscriptionStatusActive, renewed.Status)
	require.False(t, renewed.StartsAt.Before(before))
	require.False(t, renewed.StartsAt.After(after))
	require.Equal(t, renewed.StartsAt.AddDate(0, 0, 7), renewed.ExpiresAt)
	require.Zero(t, renewed.DailyUsageUSD)
	require.Zero(t, renewed.WeeklyUsageUSD)
	require.Zero(t, renewed.MonthlyUsageUSD)
	require.Equal(t, "bulk", renewed.Notes)
}

func TestAssignSubscriptionKeepsWorkingWhenIdempotencyStoreUnavailable(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeSubscription},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	SetDefaultIdempotencyCoordinator(NewIdempotencyCoordinator(failingIdempotencyRepo{}, DefaultIdempotencyConfig()))
	t.Cleanup(func() {
		SetDefaultIdempotencyCoordinator(nil)
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	sub, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       9001,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "new",
	})
	require.NoError(t, err)
	require.NotNil(t, sub)
	require.Equal(t, 1, subRepo.createCalls, "semantic idempotent endpoint should not depend on idempotency store availability")
}

func TestNormalizeAssignValidityDays(t *testing.T) {
	require.Equal(t, 30, normalizeAssignValidityDays(0))
	require.Equal(t, 30, normalizeAssignValidityDays(-5))
	require.Equal(t, MaxValidityDays, normalizeAssignValidityDays(MaxValidityDays+100))
	require.Equal(t, 7, normalizeAssignValidityDays(7))
}

func TestDetectAssignSemanticConflictCases(t *testing.T) {
	start := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	base := &UserSubscription{
		UserID:    1,
		GroupID:   1,
		StartsAt:  start,
		ExpiresAt: start.AddDate(0, 0, 30),
		Notes:     "same",
	}

	reason, conflict := detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "same",
	})
	require.False(t, conflict)
	require.Equal(t, "", reason)

	reason, conflict = detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 60,
		Notes:        "same",
	})
	require.True(t, conflict)
	require.Equal(t, "validity_days_mismatch", reason)

	reason, conflict = detectAssignSemanticConflict(base, &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
		Notes:        "other",
	})
	require.True(t, conflict)
	require.Equal(t, "notes_mismatch", reason)
}

func TestAssignSubscriptionGroupTypeValidation(t *testing.T) {
	groupRepo := &subscriptionGroupRepoStub{
		group: &Group{ID: 1, SubscriptionType: SubscriptionTypeStandard},
	}
	subRepo := newSubscriptionUserSubRepoStub()
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)

	_, err := svc.AssignSubscription(context.Background(), &AssignSubscriptionInput{
		UserID:       1,
		GroupID:      1,
		ValidityDays: 30,
	})
	require.Error(t, err)
	require.Equal(t, infraerrors.Code(ErrGroupNotSubscriptionType), infraerrors.Code(err))
}

func TestAssignOrMergeSubscriptionPurchaseSameTierQueuesRenewal(t *testing.T) {
	now := time.Now()
	monthlyStart := startOfDay(now.AddDate(0, 0, -20))
	group := &Group{
		ID:               11,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{11: group}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 101,
		UserID:             9001,
		GroupID:            11,
		StartsAt:           now.AddDate(0, 0, -20),
		ExpiresAt:          now.AddDate(0, 0, 10),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    40,
		Group:              group,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore
	planID := int64(501)
	result, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9001,
		GroupID:      11,
		ValidityDays: 30,
		Notes:        "same tier purchase",
		PlanID:       &planID,
		SourceType:   "payment_order",
		SourceID:     "order-same-tier",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Reused)
	require.True(t, result.Queued)
	require.Equal(t, int64(101), result.Subscription.ID)
	require.Equal(t, int64(11), result.Subscription.GroupID)
	require.False(t, result.ShouldMigrateAPIKeys)
	require.InDelta(t, 60, result.PreservedMonthlyRemainingUSD, 0.001)
	require.InDelta(t, 100, result.PurchasedMonthlyLimitUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.MonthlyBonusUSD, 0.001)
	require.InDelta(t, 40, result.Subscription.MonthlyUsageUSD, 0.001)
	require.WithinDuration(t, monthlyStart, *result.Subscription.MonthlyWindowStart, time.Second)
	require.WithinDuration(t, now.AddDate(0, 0, 10), result.Subscription.ExpiresAt, time.Second)
	require.Equal(t, 1, result.Subscription.PendingRenewalCount)
	require.Len(t, renewalStore.activated, 0)
	pending := renewalStore.pendingRows(101)
	require.Len(t, pending, 1)
	require.Equal(t, int64(11), pending[0].Req.TargetGroupID)
	require.Equal(t, 30, pending[0].Req.ValidityDays)
	require.InDelta(t, 100, pending[0].Req.MonthlyLimitUSD, 0.001)
	require.Equal(t, &planID, pending[0].Req.PlanID)
	require.Equal(t, "payment_order", pending[0].Req.SourceType)
	require.Equal(t, "order-same-tier", pending[0].Req.SourceID)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignOrMergeSubscriptionPurchaseExhaustedSameTierRestartsTerm(t *testing.T) {
	now := time.Now()
	oldWindowStart := startOfDay(now.AddDate(0, 0, -27))
	group := &Group{
		ID:               12,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{12: group}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 102,
		UserID:             9004,
		GroupID:            12,
		StartsAt:           now.AddDate(0, 0, -20),
		ExpiresAt:          now.AddDate(0, 0, 10),
		Status:             SubscriptionStatusActive,
		DailyWindowStart:   &oldWindowStart,
		WeeklyWindowStart:  &oldWindowStart,
		MonthlyWindowStart: &oldWindowStart,
		DailyUsageUSD:      12,
		WeeklyUsageUSD:     34,
		MonthlyUsageUSD:    100,
		Group:              group,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore
	result, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9004,
		GroupID:      12,
		ValidityDays: 30,
		Notes:        "exhausted same tier purchase",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Reused)
	require.False(t, result.Queued)
	require.Equal(t, int64(12), result.Subscription.GroupID)
	require.InDelta(t, 0, result.PreservedMonthlyRemainingUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.MonthlyBonusUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.DailyUsageUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.WeeklyUsageUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.MonthlyUsageUSD, 0.001)
	require.WithinDuration(t, startOfDay(time.Now()), *result.Subscription.DailyWindowStart, time.Second)
	require.WithinDuration(t, startOfDay(time.Now()), *result.Subscription.WeeklyWindowStart, time.Second)
	require.WithinDuration(t, startOfDay(time.Now()), *result.Subscription.MonthlyWindowStart, time.Second)
	require.InDelta(t, 30*24, time.Until(result.Subscription.ExpiresAt).Hours(), 0.1)
	require.Equal(t, 0, result.Subscription.PendingRenewalCount)
	require.Len(t, renewalStore.activated, 1)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignOrMergeSubscriptionPurchaseUpgradeCarriesQuotaAndRequestsKeyMigration(t *testing.T) {
	now := time.Now()
	monthlyStart := startOfDay(now)
	basic := &Group{
		ID:               21,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	pro := &Group{
		ID:               22,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(300),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{21: basic, 22: pro}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 201,
		UserID:             9002,
		GroupID:            21,
		StartsAt:           now.AddDate(0, 0, -20),
		ExpiresAt:          now.AddDate(0, 0, 10),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    40,
		Group:              basic,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore
	result, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9002,
		GroupID:      22,
		ValidityDays: 30,
		Notes:        "upgrade purchase",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Reused)
	require.Equal(t, int64(201), result.Subscription.ID)
	require.Equal(t, int64(22), result.Subscription.GroupID)
	require.True(t, result.ShouldMigrateAPIKeys)
	require.Equal(t, int64(21), result.MigrateAPIKeysFromGroupID)
	require.Equal(t, int64(22), result.MigrateAPIKeysToGroupID)
	require.InDelta(t, 60, result.PreservedMonthlyRemainingUSD, 0.001)
	require.InDelta(t, 300, result.PurchasedMonthlyLimitUSD, 0.001)
	require.InDelta(t, 60, result.Subscription.MonthlyBonusUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.MonthlyUsageUSD, 0.001)
	require.InDelta(t, 30*24, time.Until(result.Subscription.ExpiresAt).Hours(), 0.1)
	require.Len(t, renewalStore.activated, 0)
	require.Equal(t, 0, subRepo.createCalls)

	_, err = subRepo.GetByUserIDAndGroupID(context.Background(), 9002, 21)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
}

func TestAssignOrMergeSubscriptionPurchaseLowerTierQueuesDowngrade(t *testing.T) {
	now := time.Now()
	monthlyStart := startOfDay(now)
	basic := &Group{
		ID:               31,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	pro := &Group{
		ID:               32,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(300),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{31: basic, 32: pro}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 301,
		UserID:             9003,
		GroupID:            32,
		StartsAt:           now.AddDate(0, 0, -20),
		ExpiresAt:          now.AddDate(0, 0, 10),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    40,
		Group:              pro,
	})

	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore
	result, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9003,
		GroupID:      31,
		ValidityDays: 30,
		Notes:        "lower tier renewal",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Reused)
	require.Equal(t, int64(301), result.Subscription.ID)
	require.Equal(t, int64(32), result.Subscription.GroupID)
	require.True(t, result.Queued)
	require.False(t, result.ShouldMigrateAPIKeys)
	require.InDelta(t, 260, result.PreservedMonthlyRemainingUSD, 0.001)
	require.InDelta(t, 100, result.PurchasedMonthlyLimitUSD, 0.001)
	require.InDelta(t, 0, result.Subscription.MonthlyBonusUSD, 0.001)
	require.InDelta(t, 40, result.Subscription.MonthlyUsageUSD, 0.001)
	require.WithinDuration(t, monthlyStart, *result.Subscription.MonthlyWindowStart, time.Second)
	require.WithinDuration(t, now.AddDate(0, 0, 10), result.Subscription.ExpiresAt, time.Second)
	require.Equal(t, 1, result.Subscription.PendingRenewalCount)
	require.Len(t, renewalStore.activated, 0)
	pending := renewalStore.pendingRows(301)
	require.Len(t, pending, 1)
	require.Equal(t, int64(31), pending[0].Req.TargetGroupID)
	require.InDelta(t, 100, pending[0].Req.MonthlyLimitUSD, 0.001)
	require.Equal(t, 0, subRepo.createCalls)
}

func TestAssignOrMergeSubscriptionPurchaseMultipleQueuedPurchasesActivateFIFO(t *testing.T) {
	now := time.Now()
	monthlyStart := startOfDay(now)
	basic := &Group{
		ID:               41,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	pro := &Group{
		ID:               42,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(300),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{41: basic, 42: pro}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 401,
		UserID:             9005,
		GroupID:            42,
		StartsAt:           now.AddDate(0, 0, -10),
		ExpiresAt:          now.AddDate(0, 0, 20),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    25,
		Group:              pro,
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore

	first, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9005,
		GroupID:      41,
		ValidityDays: 15,
		Notes:        "first queued downgrade",
		SourceType:   "payment_order",
		SourceID:     "fifo-1",
	})
	require.NoError(t, err)
	require.True(t, first.Queued)
	second, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9005,
		GroupID:      42,
		ValidityDays: 45,
		Notes:        "second queued renewal",
		SourceType:   "payment_order",
		SourceID:     "fifo-2",
	})
	require.NoError(t, err)
	require.True(t, second.Queued)

	pending := renewalStore.pendingRows(401)
	require.Len(t, pending, 2)
	require.Equal(t, int64(41), pending[0].Req.TargetGroupID)
	require.Equal(t, 15, pending[0].Req.ValidityDays)
	require.Equal(t, "fifo-1", pending[0].Req.SourceID)
	require.Equal(t, int64(42), pending[1].Req.TargetGroupID)
	require.Equal(t, 45, pending[1].Req.ValidityDays)
	require.Equal(t, "fifo-2", pending[1].Req.SourceID)

	stored := subRepo.byID[401]
	stored.MonthlyUsageUSD = 300
	activatedFirst, err := svc.EnsureWindowMaintenance(context.Background(), stored)
	require.NoError(t, err)
	require.Equal(t, int64(41), activatedFirst.GroupID)
	require.InDelta(t, 15*24, time.Until(activatedFirst.ExpiresAt).Hours(), 0.1)
	require.InDelta(t, 0, activatedFirst.MonthlyUsageUSD, 0.001)
	require.Equal(t, 1, activatedFirst.PendingRenewalCount)

	stored = subRepo.byID[401]
	stored.MonthlyUsageUSD = 100
	activatedSecond, err := svc.EnsureWindowMaintenance(context.Background(), stored)
	require.NoError(t, err)
	require.Equal(t, int64(42), activatedSecond.GroupID)
	require.InDelta(t, 45*24, time.Until(activatedSecond.ExpiresAt).Hours(), 0.1)
	require.Equal(t, 0, activatedSecond.PendingRenewalCount)
	require.Equal(t, []int64{1, 2}, renewalStore.activated)
}

func TestGetActiveSubscriptionAllowsExpiredTermWithPendingRenewal(t *testing.T) {
	now := time.Now()
	group := &Group{
		ID:               51,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{51: group}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:        501,
		UserID:    9006,
		GroupID:   51,
		StartsAt:  now.AddDate(0, 0, -31),
		ExpiresAt: now.AddDate(0, 0, -1),
		Status:    SubscriptionStatusActive,
		Group:     group,
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore

	_, err := svc.GetActiveSubscription(context.Background(), 9006, 51)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)

	require.NoError(t, renewalStore.Enqueue(context.Background(), subscriptionRenewalRequest{
		SubscriptionID:  501,
		UserID:          9006,
		TargetGroupID:   51,
		SourceType:      "payment_order",
		SourceID:        "expired-loadable",
		ValidityDays:    30,
		MonthlyLimitUSD: 100,
	}))
	sub, err := svc.GetActiveSubscription(context.Background(), 9006, 51)
	require.NoError(t, err)
	require.Equal(t, int64(501), sub.ID)
	require.Equal(t, 1, sub.PendingRenewalCount)
	require.True(t, sub.ExpiresAt.Before(now))
}

func TestAssignOrMergeSubscriptionPurchaseUpgradeReassignsPendingRenewals(t *testing.T) {
	now := time.Now()
	monthlyStart := startOfDay(now)
	basic := &Group{
		ID:               61,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(100),
	}
	pro := &Group{
		ID:               62,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(300),
	}
	ultra := &Group{
		ID:               63,
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		MonthlyLimitUSD:  testFloat64Ptr(500),
	}
	groupRepo := &subscriptionGroupRepoMapStub{groups: map[int64]*Group{61: basic, 62: pro, 63: ultra}}
	subRepo := newSubscriptionUserSubRepoStub()
	subRepo.seed(&UserSubscription{
		ID:                 601,
		UserID:             9007,
		GroupID:            61,
		StartsAt:           now.AddDate(0, 0, -5),
		ExpiresAt:          now.AddDate(0, 0, 25),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    20,
		Group:              basic,
	})
	subRepo.seed(&UserSubscription{
		ID:                 602,
		UserID:             9007,
		GroupID:            62,
		StartsAt:           now.AddDate(0, 0, -3),
		ExpiresAt:          now.AddDate(0, 0, 27),
		Status:             SubscriptionStatusActive,
		MonthlyWindowStart: &monthlyStart,
		MonthlyUsageUSD:    40,
		Group:              pro,
	})
	svc := NewSubscriptionService(groupRepo, subRepo, nil, nil, nil)
	renewalStore := newSubscriptionRenewalStoreStub(subRepo, groupRepo.groups)
	svc.renewalStore = renewalStore
	require.NoError(t, renewalStore.Enqueue(context.Background(), subscriptionRenewalRequest{
		SubscriptionID:  601,
		UserID:          9007,
		TargetGroupID:   61,
		SourceType:      "payment_order",
		SourceID:        "queued-before-upgrade-1",
		ValidityDays:    30,
		MonthlyLimitUSD: 100,
	}))
	require.NoError(t, renewalStore.Enqueue(context.Background(), subscriptionRenewalRequest{
		SubscriptionID:  602,
		UserID:          9007,
		TargetGroupID:   62,
		SourceType:      "payment_order",
		SourceID:        "queued-before-upgrade-2",
		ValidityDays:    30,
		MonthlyLimitUSD: 300,
	}))

	result, err := svc.AssignOrMergeSubscriptionPurchase(context.Background(), &AssignSubscriptionInput{
		UserID:       9007,
		GroupID:      63,
		ValidityDays: 30,
		Notes:        "upgrade after queued renewals",
		SourceType:   "payment_order",
		SourceID:     "upgrade-after-queue",
	})
	require.NoError(t, err)
	require.Equal(t, int64(602), result.Subscription.ID)
	require.Equal(t, int64(63), result.Subscription.GroupID)
	require.True(t, result.ShouldMigrateAPIKeys)
	require.InDelta(t, 340, result.PreservedMonthlyRemainingUSD, 0.001)

	_, err = subRepo.GetByID(context.Background(), 601)
	require.ErrorIs(t, err, ErrSubscriptionNotFound)
	pending := renewalStore.pendingRows(602)
	require.Len(t, pending, 2)
	require.Equal(t, "queued-before-upgrade-1", pending[0].Req.SourceID)
	require.Equal(t, "queued-before-upgrade-2", pending[1].Req.SourceID)
	require.Len(t, renewalStore.reassigned, 1)
	require.ElementsMatch(t, []int64{601, 602}, renewalStore.reassigned[0].OldIDs)
	require.Equal(t, int64(602), renewalStore.reassigned[0].NewID)
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}

func infraerrorsReason(err error) string {
	return infraerrors.Reason(err)
}

func testFloat64Ptr(v float64) *float64 {
	return &v
}
