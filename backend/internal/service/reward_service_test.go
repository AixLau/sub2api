package service

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type rewardFeatureSettingRepoStub struct {
	SettingRepository
	values []bool
	err    error
	calls  int
}

func (r *rewardFeatureSettingRepoStub) GetValue(context.Context, string) (string, error) {
	index := r.calls
	r.calls++
	if r.err != nil {
		return "", r.err
	}
	if len(r.values) == 0 {
		return "false", nil
	}
	if index >= len(r.values) {
		index = len(r.values) - 1
	}
	if r.values[index] {
		return "true", nil
	}
	return "false", nil
}

type rewardWorkerGateRepoStub struct {
	RewardRepository
	maintenanceCalls int
	enqueueCalls     int
	claimCalls       int
	listCalls        int
	evaluateCalls    int
	extendCalls      int
	extendedCursors  []int64
	releaseCalls     int
	releaseJobID     int64
	releaseWorkerID  string
	retryCalls       int
	retryCause       error
	job              *RewardCampaignJob
}

type legacyRewardReplayRepoStub struct {
	RewardRepository
	grant RewardGrant
	claim RewardClaimResult
}

func (r *legacyRewardReplayRepoStub) ImportLegacyPending(context.Context, int64, time.Time) error {
	return nil
}

func (r *legacyRewardReplayRepoStub) ExpirePendingForUser(context.Context, int64, time.Time) error {
	return nil
}

func (r *legacyRewardReplayRepoStub) FindPendingBySystemKey(context.Context, int64, string, time.Time) (*RewardGrant, error) {
	return nil, infraerrors.NotFound("REWARD_NOT_FOUND", "reward not found")
}

func (r *legacyRewardReplayRepoStub) ListRuntimeCampaigns(context.Context, string, time.Time) ([]RewardCampaign, error) {
	return nil, nil
}

func (r *legacyRewardReplayRepoStub) FindLatestBySystemKey(context.Context, int64, string) (*RewardGrant, error) {
	grant := r.grant
	return &grant, nil
}

func (r *legacyRewardReplayRepoStub) Claim(context.Context, int64, int64, time.Time) (*RewardClaimResult, error) {
	claim := r.claim
	return &claim, nil
}

func (r *rewardWorkerGateRepoStub) EndExpiredCampaigns(context.Context, time.Time) error {
	r.maintenanceCalls++
	return nil
}

func (r *rewardWorkerGateRepoStub) EnqueueScheduledCampaigns(context.Context, time.Time) (int64, error) {
	r.enqueueCalls++
	return 0, nil
}

func (r *rewardWorkerGateRepoStub) ClaimJobs(context.Context, string, time.Time, int, time.Duration) ([]RewardCampaignJob, error) {
	r.claimCalls++
	if r.job == nil {
		return nil, nil
	}
	return []RewardCampaignJob{*r.job}, nil
}

func (r *rewardWorkerGateRepoStub) GetCampaignVersion(context.Context, int64, int64) (*RewardCampaign, error) {
	campaign := validRewardCampaignForTest()
	campaign.ID = 1
	campaign.Status = RewardCampaignStatusActive
	campaign.IssuanceMode = RewardIssuanceModeScheduledBatch
	campaign.CurrentVersionID = 2
	campaign.WinProbability = 1
	campaign.Config.WinProbability = 1
	return &campaign, nil
}

func (r *rewardWorkerGateRepoStub) ListBatchCandidateUserIDs(context.Context, int64, int64, int64, int) ([]int64, error) {
	r.listCalls++
	return []int64{10, 20}, nil
}

func (r *rewardWorkerGateRepoStub) GetAudienceProfiles(_ context.Context, userIDs []int64, _ time.Time) (map[int64]RewardAudienceProfile, error) {
	profiles := make(map[int64]RewardAudienceProfile, len(userIDs))
	for _, userID := range userIDs {
		profiles[userID] = RewardAudienceProfile{UserID: userID}
	}
	return profiles, nil
}

func (r *rewardWorkerGateRepoStub) EvaluateAndMaybeGrant(_ context.Context, _ RewardCampaign, userID int64, _ time.Time, _, _ bool, _ string, _ *int64) (*RewardGrant, bool, error) {
	r.evaluateCalls++
	return &RewardGrant{ID: userID, UserID: userID}, true, nil
}

func (r *rewardWorkerGateRepoStub) ExtendJobLease(_ context.Context, _ int64, _ string, cursor int64, _ int64, _ int64, _ int64, _ int64, _ int64, _ time.Time, _ time.Duration) error {
	r.extendCalls++
	r.extendedCursors = append(r.extendedCursors, cursor)
	return nil
}

func (r *rewardWorkerGateRepoStub) ReleaseJob(_ context.Context, jobID int64, workerID string, _ time.Time) error {
	r.releaseCalls++
	r.releaseJobID = jobID
	r.releaseWorkerID = workerID
	return nil
}

func (r *rewardWorkerGateRepoStub) RetryJob(_ context.Context, _ int64, _ string, _ time.Time, cause error) error {
	r.retryCalls++
	r.retryCause = cause
	return nil
}

type rewardRepositoryTestStub struct {
	RewardRepository

	estimateAudienceFn          func(context.Context, RewardAudience, time.Time) (int64, time.Time, error)
	getCampaignVersionFn        func(context.Context, int64, int64) (*RewardCampaign, error)
	listBatchCandidateUserIDsFn func(context.Context, int64, int64, int64, int) ([]int64, error)
	getAudienceProfilesFn       func(context.Context, []int64, time.Time) (map[int64]RewardAudienceProfile, error)
	evaluateAndMaybeGrantFn     func(context.Context, RewardCampaign, int64, time.Time, bool, bool, string, *int64) (*RewardGrant, bool, error)
	extendJobLeaseFn            func(context.Context, int64, string, int64, int64, int64, int64, int64, int64, time.Time, time.Duration) error
	completeJobFn               func(context.Context, int64, string, time.Time) error
}

func (r *rewardRepositoryTestStub) EstimateAudience(
	ctx context.Context,
	audience RewardAudience,
	now time.Time,
) (int64, time.Time, error) {
	return r.estimateAudienceFn(ctx, audience, now)
}

func (r *rewardRepositoryTestStub) GetCampaignVersion(
	ctx context.Context,
	campaignID, versionID int64,
) (*RewardCampaign, error) {
	return r.getCampaignVersionFn(ctx, campaignID, versionID)
}

func (r *rewardRepositoryTestStub) ListBatchCandidateUserIDs(
	ctx context.Context,
	campaignID, afterUserID, maxUserID int64,
	limit int,
) ([]int64, error) {
	return r.listBatchCandidateUserIDsFn(ctx, campaignID, afterUserID, maxUserID, limit)
}

func (r *rewardRepositoryTestStub) GetAudienceProfiles(
	ctx context.Context,
	userIDs []int64,
	now time.Time,
) (map[int64]RewardAudienceProfile, error) {
	return r.getAudienceProfilesFn(ctx, userIDs, now)
}

func (r *rewardRepositoryTestStub) EvaluateAndMaybeGrant(
	ctx context.Context,
	campaign RewardCampaign,
	userID int64,
	now time.Time,
	shouldAward, controlGroup bool,
	source string,
	jobID *int64,
) (*RewardGrant, bool, error) {
	return r.evaluateAndMaybeGrantFn(
		ctx,
		campaign,
		userID,
		now,
		shouldAward,
		controlGroup,
		source,
		jobID,
	)
}

func (r *rewardRepositoryTestStub) ExtendJobLease(
	ctx context.Context,
	jobID int64,
	workerID string,
	cursorUserID int64,
	scanned, matched, granted, skipped, failed int64,
	now time.Time,
	lease time.Duration,
) error {
	return r.extendJobLeaseFn(
		ctx,
		jobID,
		workerID,
		cursorUserID,
		scanned,
		matched,
		granted,
		skipped,
		failed,
		now,
		lease,
	)
}

func (r *rewardRepositoryTestStub) CompleteJob(
	ctx context.Context,
	jobID int64,
	workerID string,
	now time.Time,
) error {
	return r.completeJobFn(ctx, jobID, workerID, now)
}

func TestRewardAudienceValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		audience RewardAudience
		wantErr  bool
	}{
		{
			name: "valid AND within OR groups",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{
				{AllOf: []RewardAudienceRule{
					{Field: "signup_source", Operator: "in", Value: []string{"email", "oidc"}},
					{Field: "requests_7d", Operator: "gte", Value: 5},
				}},
				{AllOf: []RewardAudienceRule{
					{Field: "user_id", Operator: "eq", Value: int64(42)},
				}},
			}},
		},
		{
			name: "unsupported field",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{{AllOf: []RewardAudienceRule{
				{Field: "password_hash", Operator: "eq", Value: "secret"},
			}}}},
			wantErr: true,
		},
		{
			name: "unsupported operator",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{{AllOf: []RewardAudienceRule{
				{Field: "balance", Operator: "contains", Value: 1},
			}}}},
			wantErr: true,
		},
		{
			name: "empty AND group",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{
				{},
			}},
			wantErr: true,
		},
		{
			name: "between requires two values",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{{AllOf: []RewardAudienceRule{
				{Field: "balance", Operator: "between", Value: []float64{1}},
			}}}},
			wantErr: true,
		},
		{
			name: "in requires a non-empty list",
			audience: RewardAudience{AnyOf: []RewardAudienceRuleGroup{{AllOf: []RewardAudienceRule{
				{Field: "signup_source", Operator: "in", Value: []string{}},
			}}}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.audience.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestRewardAudienceMatchesUsesANDWithinGroupsAndORAcrossGroups(t *testing.T) {
	t.Parallel()

	lastActive := time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
	profile := RewardAudienceProfile{
		UserID:               42,
		RegisteredAt:         time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC),
		SignupSource:         "oidc",
		LastActiveAt:         &lastActive,
		Balance:              12.5,
		SubscriptionGroupIDs: []int64{3, 9},
		Requests7D:           25,
		Requests30D:          80,
		ActualCost7D:         4.5,
		Recharge30D:          10,
	}
	audience := RewardAudience{AnyOf: []RewardAudienceRuleGroup{
		{AllOf: []RewardAudienceRule{
			{Field: "signup_source", Operator: "eq", Value: "email"},
			{Field: "requests_7d", Operator: "gte", Value: 20},
		}},
		{AllOf: []RewardAudienceRule{
			{Field: "signup_source", Operator: "in", Value: []string{"oidc", "github"}},
			{Field: "requests_7d", Operator: "between", Value: []int{20, 30}},
			{Field: "balance", Operator: "gt", Value: 10},
			{Field: "subscription_group_id", Operator: "in", Value: []int64{9, 11}},
			{Field: "user_id", Operator: "not_in", Value: []int64{7, 8}},
		}},
	}}

	if !RewardAudienceMatches(audience, profile) {
		t.Fatal("RewardAudienceMatches() = false, want true from second OR group")
	}

	audience.AnyOf[1].AllOf = append(audience.AnyOf[1].AllOf,
		RewardAudienceRule{Field: "recharge_30d", Operator: "gt", Value: 100},
	)
	if RewardAudienceMatches(audience, profile) {
		t.Fatal("RewardAudienceMatches() = true after one rule in every OR group failed")
	}

	if !RewardAudienceMatches(RewardAudience{}, profile) {
		t.Fatal("empty audience must match all users")
	}
}

func TestResolveRewardAudienceRelativeTimes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 8, 9, 30, 0, 0, time.UTC)
	audience := RewardAudience{AnyOf: []RewardAudienceRuleGroup{{AllOf: []RewardAudienceRule{
		{
			Field:    "registered_at",
			Operator: "before",
			Value:    map[string]any{"relative_days": -7},
		},
		{
			Field:    "last_active_at",
			Operator: "after",
			Value:    map[string]any{"relative_hours": -6},
		},
	}}}}

	resolved := ResolveRewardAudienceRelativeTimes(audience, now)
	if got, want := resolved.AnyOf[0].AllOf[0].Value, now.Add(-7*24*time.Hour).Format(time.RFC3339); got != want {
		t.Fatalf("relative_days resolved to %v, want %v", got, want)
	}
	if got, want := resolved.AnyOf[0].AllOf[1].Value, now.Add(-6*time.Hour).Format(time.RFC3339); got != want {
		t.Fatalf("relative_hours resolved to %v, want %v", got, want)
	}
	if _, ok := audience.AnyOf[0].AllOf[0].Value.(map[string]any); !ok {
		t.Fatal("ResolveRewardAudienceRelativeTimes mutated the input audience")
	}

	registeredAt := now.Add(-8 * 24 * time.Hour)
	lastActiveAt := now.Add(-time.Hour)
	if !RewardAudienceMatches(resolved, RewardAudienceProfile{
		RegisteredAt: registeredAt,
		LastActiveAt: &lastActiveAt,
	}) {
		t.Fatal("resolved relative-time rules did not match the expected profile")
	}
}

func TestRewardAmountHelpers(t *testing.T) {
	t.Parallel()

	tiers := []RewardAmountTier{
		{Amount: 1, Weight: 60},
		{Amount: 3, Weight: 30},
		{Amount: 5, Weight: 10},
	}
	if got, want := ExpectedRewardAmount(tiers), 2.0; math.Abs(got-want) > 1e-9 {
		t.Fatalf("ExpectedRewardAmount() = %v, want %v", got, want)
	}
	if got, want := MaximumRewardAmount(tiers), 5.0; got != want {
		t.Fatalf("MaximumRewardAmount() = %v, want %v", got, want)
	}
	if got := ExpectedRewardAmount([]RewardAmountTier{{Amount: -1, Weight: 1}, {Amount: 5, Weight: 0}}); got != 0 {
		t.Fatalf("ExpectedRewardAmount() with no valid weighted tiers = %v, want 0", got)
	}
}

func TestRewardServiceEstimateBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 30, 10, 0, 0, 0, time.UTC)
	dataUpdatedAt := now.Add(-time.Hour)
	var estimatedAudience RewardAudience
	repo := &rewardRepositoryTestStub{
		estimateAudienceFn: func(_ context.Context, audience RewardAudience, gotNow time.Time) (int64, time.Time, error) {
			estimatedAudience = audience
			if !gotNow.Equal(now) {
				t.Fatalf("EstimateAudience now = %v, want %v", gotNow, now)
			}
			return 100, dataUpdatedAt, nil
		},
	}
	rewardService := NewRewardService(repo, nil, nil)
	rewardService.now = func() time.Time { return now }
	campaign := validRewardCampaignForTest()
	campaign.WinProbability = 0.25
	campaign.PerUserLimit = 2
	campaign.ControlGroupPercent = 10
	campaign.Config.WinProbability = 0.25
	campaign.Config.PerUserLimit = 2
	campaign.Config.ControlGroupPercent = 10
	campaign.Config.AmountTiers = []RewardAmountTier{
		{Amount: 1, Weight: 60},
		{Amount: 3, Weight: 30},
		{Amount: 5, Weight: 10},
	}

	estimate, err := rewardService.Estimate(context.Background(), campaign)
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}
	if estimate.EligibleUsers != 100 {
		t.Fatalf("EligibleUsers = %d, want 100", estimate.EligibleUsers)
	}
	if estimate.ExpectedWinners != 23 {
		t.Fatalf("ExpectedWinners = %d, want 23", estimate.ExpectedWinners)
	}
	if estimate.ExpectedCost != 46 {
		t.Fatalf("ExpectedCost = %v, want 46", estimate.ExpectedCost)
	}
	if estimate.MaximumCost != 1000 {
		t.Fatalf("MaximumCost = %v, want 1000", estimate.MaximumCost)
	}
	if !estimate.DataUpdatedAt.Equal(dataUpdatedAt) {
		t.Fatalf("DataUpdatedAt = %v, want %v", estimate.DataUpdatedAt, dataUpdatedAt)
	}
	if !reflect.DeepEqual(estimatedAudience, campaign.Config.Audience) {
		t.Fatalf("EstimateAudience audience = %#v, want %#v", estimatedAudience, campaign.Config.Audience)
	}
}

func TestRewardCampaignAvailableBudgetClampsFloatingPointNoise(t *testing.T) {
	t.Parallel()

	campaign := RewardCampaign{TotalBudget: 0.3, ReservedBudget: 0.1, SpentBudget: 0.200000001}
	if got := campaign.AvailableBudget(); got != 0 {
		t.Fatalf("AvailableBudget() = %v, want 0", got)
	}
	campaign.SpentBudget = 0.21
	if got := campaign.AvailableBudget(); got >= 0 {
		t.Fatalf("AvailableBudget() = %v, want a material negative value", got)
	}
}

func TestRewardServiceClaimLegacyReplaysCommittedClaim(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	repo := &legacyRewardReplayRepoStub{
		grant: RewardGrant{ID: 91, UserID: 7, Status: RewardGrantStatusClaimed},
		claim: RewardClaimResult{
			GrantID: 91, Amount: 3, Balance: 13, ClaimedAt: now, AlreadyClaimed: true,
		},
	}
	svc := NewRewardService(repo, nil, nil)
	svc.now = func() time.Time { return now }

	result, err := svc.ClaimLegacy(context.Background(), 7, RewardSystemCampaignWelcome)
	if err != nil {
		t.Fatalf("ClaimLegacy() error = %v", err)
	}
	if result.GrantID != 91 || result.Amount != 3 || result.Balance != 13 || !result.AlreadyClaimed {
		t.Fatalf("ClaimLegacy() = %#v, want committed claim replay", result)
	}
}

func TestRewardJobWorkerResumesCursorAndUsesFrozenVersion(t *testing.T) {
	t.Parallel()

	const (
		campaignID = int64(17)
		versionID  = int64(23)
		jobID      = int64(31)
	)
	candidateCursors := make([]int64, 0, 2)
	extendedCursors := make([]int64, 0, 2)
	evaluatedUsers := make([]int64, 0, 3)
	completed := false
	repo := &rewardRepositoryTestStub{}
	repo.getCampaignVersionFn = func(_ context.Context, gotCampaignID, gotVersionID int64) (*RewardCampaign, error) {
		if gotCampaignID != campaignID || gotVersionID != versionID {
			t.Fatalf("GetCampaignVersion(%d, %d), want (%d, %d)", gotCampaignID, gotVersionID, campaignID, versionID)
		}
		campaign := validRewardCampaignForTest()
		campaign.ID = campaignID
		campaign.Status = RewardCampaignStatusActive
		campaign.IssuanceMode = RewardIssuanceModeScheduledBatch
		campaign.CurrentVersionID = versionID
		campaign.CurrentVersion = 4
		campaign.WinProbability = 1
		campaign.Config.WinProbability = 1
		campaign.Config.Copy.Title = "frozen v4"
		return &campaign, nil
	}
	repo.listBatchCandidateUserIDsFn = func(_ context.Context, gotCampaignID, afterUserID, maxUserID int64, limit int) ([]int64, error) {
		if gotCampaignID != campaignID || maxUserID != 100 || limit != 2 {
			t.Fatalf("ListBatchCandidateUserIDs campaign/max/limit = %d/%d/%d, want %d/100/2", gotCampaignID, maxUserID, limit, campaignID)
		}
		candidateCursors = append(candidateCursors, afterUserID)
		switch afterUserID {
		case 77:
			return []int64{80, 90}, nil
		case 90:
			return []int64{100}, nil
		default:
			t.Fatalf("unexpected cursor %d", afterUserID)
			return nil, nil
		}
	}
	repo.getAudienceProfilesFn = func(_ context.Context, userIDs []int64, _ time.Time) (map[int64]RewardAudienceProfile, error) {
		profiles := make(map[int64]RewardAudienceProfile, len(userIDs))
		for _, userID := range userIDs {
			profiles[userID] = RewardAudienceProfile{UserID: userID}
		}
		return profiles, nil
	}
	repo.evaluateAndMaybeGrantFn = func(
		_ context.Context,
		campaign RewardCampaign,
		userID int64,
		_ time.Time,
		shouldAward, controlGroup bool,
		source string,
		gotJobID *int64,
	) (*RewardGrant, bool, error) {
		if campaign.CurrentVersionID != versionID || campaign.Config.Copy.Title != "frozen v4" {
			t.Fatalf("grant evaluated with campaign version %d/%q, want frozen %d/%q",
				campaign.CurrentVersionID, campaign.Config.Copy.Title, versionID, "frozen v4")
		}
		if !shouldAward || controlGroup {
			t.Fatalf("award flags = %v/%v, want true/false", shouldAward, controlGroup)
		}
		if source != RewardGrantSourceScheduledBatch || gotJobID == nil || *gotJobID != jobID {
			t.Fatalf("source/job = %q/%v, want %q/%d", source, gotJobID, RewardGrantSourceScheduledBatch, jobID)
		}
		evaluatedUsers = append(evaluatedUsers, userID)
		return &RewardGrant{UserID: userID, VersionID: versionID}, true, nil
	}
	repo.extendJobLeaseFn = func(
		_ context.Context,
		gotJobID int64,
		workerID string,
		cursorUserID int64,
		scanned, matched, granted, skipped, failed int64,
		_ time.Time,
		lease time.Duration,
	) error {
		if gotJobID != jobID || workerID != "worker-a" || lease != time.Minute {
			t.Fatalf("ExtendJobLease identity/lease = %d/%q/%v", gotJobID, workerID, lease)
		}
		if scanned != matched || matched != granted || skipped != 0 || failed != 0 {
			t.Fatalf("batch counters = scanned:%d matched:%d granted:%d skipped:%d failed:%d",
				scanned, matched, granted, skipped, failed)
		}
		extendedCursors = append(extendedCursors, cursorUserID)
		return nil
	}
	repo.completeJobFn = func(_ context.Context, gotJobID int64, workerID string, _ time.Time) error {
		if gotJobID != jobID || workerID != "worker-a" {
			t.Fatalf("CompleteJob(%d, %q), want (%d, %q)", gotJobID, workerID, jobID, "worker-a")
		}
		completed = true
		return nil
	}

	worker := &RewardJobWorker{
		repo:      repo,
		settings:  NewSettingService(&rewardFeatureSettingRepoStub{values: []bool{true}}, &config.Config{}),
		workerID:  "worker-a",
		lease:     time.Minute,
		batchSize: 2,
		stop:      make(chan struct{}),
	}
	err := worker.processJob(context.Background(), RewardCampaignJob{
		ID:           jobID,
		CampaignID:   campaignID,
		VersionID:    versionID,
		CursorUserID: 77,
		MaxUserID:    100,
	})
	if err != nil {
		t.Fatalf("processJob() error = %v", err)
	}
	if !reflect.DeepEqual(candidateCursors, []int64{77, 90}) {
		t.Fatalf("candidate cursors = %v, want [77 90]", candidateCursors)
	}
	if !reflect.DeepEqual(extendedCursors, []int64{90, 100}) {
		t.Fatalf("extended cursors = %v, want [90 100]", extendedCursors)
	}
	if !reflect.DeepEqual(evaluatedUsers, []int64{80, 90, 100}) {
		t.Fatalf("evaluated users = %v, want [80 90 100]", evaluatedUsers)
	}
	if !completed {
		t.Fatal("processJob() did not complete the final partial page")
	}
}

func TestRewardJobWorkerStopsWhenLeaseIsLost(t *testing.T) {
	t.Parallel()

	leaseLost := errors.New("lease lost")
	completed := false
	repo := &rewardRepositoryTestStub{
		getCampaignVersionFn: func(context.Context, int64, int64) (*RewardCampaign, error) {
			campaign := validRewardCampaignForTest()
			campaign.ID = 1
			campaign.Status = RewardCampaignStatusActive
			campaign.IssuanceMode = RewardIssuanceModeScheduledBatch
			campaign.CurrentVersionID = 2
			campaign.WinProbability = 1
			campaign.Config.WinProbability = 1
			return &campaign, nil
		},
		listBatchCandidateUserIDsFn: func(context.Context, int64, int64, int64, int) ([]int64, error) {
			return []int64{10}, nil
		},
		getAudienceProfilesFn: func(context.Context, []int64, time.Time) (map[int64]RewardAudienceProfile, error) {
			return map[int64]RewardAudienceProfile{10: {UserID: 10}}, nil
		},
		evaluateAndMaybeGrantFn: func(
			context.Context,
			RewardCampaign,
			int64,
			time.Time,
			bool,
			bool,
			string,
			*int64,
		) (*RewardGrant, bool, error) {
			return &RewardGrant{}, true, nil
		},
		extendJobLeaseFn: func(
			context.Context,
			int64,
			string,
			int64,
			int64,
			int64,
			int64,
			int64,
			int64,
			time.Time,
			time.Duration,
		) error {
			return leaseLost
		},
		completeJobFn: func(context.Context, int64, string, time.Time) error {
			completed = true
			return nil
		},
	}
	worker := &RewardJobWorker{
		repo:      repo,
		settings:  NewSettingService(&rewardFeatureSettingRepoStub{values: []bool{true}}, &config.Config{}),
		workerID:  "worker-a",
		lease:     time.Minute,
		batchSize: 10,
		stop:      make(chan struct{}),
	}

	err := worker.processJob(context.Background(), RewardCampaignJob{
		ID: 9, CampaignID: 1, VersionID: 2, MaxUserID: 100,
	})
	if !errors.Is(err, leaseLost) {
		t.Fatalf("processJob() error = %v, want lease-lost error", err)
	}
	if completed {
		t.Fatal("processJob() completed after losing its lease")
	}
}

func TestRewardJobWorkerDisabledOnlyRunsExpirationMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		settingRepo *rewardFeatureSettingRepoStub
	}{
		{name: "disabled", settingRepo: &rewardFeatureSettingRepoStub{values: []bool{false}}},
		{name: "read failure", settingRepo: &rewardFeatureSettingRepoStub{err: errors.New("settings unavailable")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &rewardWorkerGateRepoStub{}
			worker := &RewardJobWorker{
				repo:     repo,
				settings: NewSettingService(tc.settingRepo, &config.Config{}),
				workerID: "worker-disabled",
				lease:    time.Minute,
			}

			worker.runOnce(context.Background())

			if repo.maintenanceCalls != 1 {
				t.Fatalf("maintenance calls = %d, want 1", repo.maintenanceCalls)
			}
			if repo.enqueueCalls != 0 || repo.claimCalls != 0 || repo.evaluateCalls != 0 {
				t.Fatalf("disabled worker performed issuance work: enqueue=%d claim=%d evaluate=%d",
					repo.enqueueCalls, repo.claimCalls, repo.evaluateCalls)
			}
		})
	}
}

func TestRewardJobWorkerStopsBetweenPagesWhenFeatureIsDisabled(t *testing.T) {
	repo := &rewardWorkerGateRepoStub{job: &RewardCampaignJob{
		ID: 7, CampaignID: 1, VersionID: 2, MaxUserID: 100,
	}}
	settingRepo := &rewardFeatureSettingRepoStub{values: []bool{true, true, false}}
	worker := &RewardJobWorker{
		repo:      repo,
		settings:  NewSettingService(settingRepo, &config.Config{}),
		workerID:  "worker-gated",
		lease:     time.Minute,
		batchSize: 2,
		stop:      make(chan struct{}),
	}

	worker.runOnce(context.Background())

	if repo.listCalls != 1 || repo.evaluateCalls != 2 || repo.extendCalls != 1 {
		t.Fatalf("first page work = list:%d evaluate:%d extend:%d, want 1/2/1",
			repo.listCalls, repo.evaluateCalls, repo.extendCalls)
	}
	if !reflect.DeepEqual(repo.extendedCursors, []int64{20}) {
		t.Fatalf("extended cursors = %v, want [20]", repo.extendedCursors)
	}
	if repo.releaseCalls != 1 || repo.releaseJobID != 7 || repo.releaseWorkerID != "worker-gated" {
		t.Fatalf("release = calls:%d job:%d worker:%q, want 1/7/worker-gated",
			repo.releaseCalls, repo.releaseJobID, repo.releaseWorkerID)
	}
	if repo.retryCalls != 0 {
		t.Fatalf("retry calls = %d, want 0 for a feature-gate release", repo.retryCalls)
	}
}

func validRewardCampaignForTest() RewardCampaign {
	start := time.Date(2026, time.July, 30, 0, 0, 0, 0, time.UTC)
	return RewardCampaign{
		Name:                      "reward test",
		Title:                     "Reward test",
		Status:                    RewardCampaignStatusDraft,
		IssuanceMode:              RewardIssuanceModeOnAccess,
		Timezone:                  "UTC",
		StartsAt:                  start,
		EndsAt:                    start.Add(24 * time.Hour),
		Priority:                  10,
		WinProbability:            1,
		PerUserLimit:              1,
		EvaluationIntervalMinutes: 0,
		ClaimCooldownMinutes:      0,
		TotalBudget:               100,
		Config: RewardCampaignConfig{
			Title:          "Reward test",
			Priority:       10,
			WinProbability: 1,
			PerUserLimit:   1,
			AmountTiers: []RewardAmountTier{
				{Amount: 1, Weight: 1},
			},
			Copy: RewardCopy{Title: "Reward test"},
		},
	}
}
