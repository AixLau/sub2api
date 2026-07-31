//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type rewardIntegrationFixture struct {
	t           *testing.T
	ctx         context.Context
	repo        *rewardRepository
	userIDs     []int64
	campaignIDs []int64
}

func newRewardIntegrationFixture(t *testing.T) *rewardIntegrationFixture {
	t.Helper()
	fixture := &rewardIntegrationFixture{
		t:    t,
		ctx:  context.Background(),
		repo: &rewardRepository{db: integrationDB},
	}
	t.Cleanup(fixture.cleanup)
	return fixture
}

func (f *rewardIntegrationFixture) createUser(balance float64) int64 {
	f.t.Helper()
	email := fmt.Sprintf("reward-%d-%d@example.com", time.Now().UnixNano(), len(f.userIDs))
	var userID int64
	require.NoError(f.t, integrationDB.QueryRowContext(f.ctx, `
INSERT INTO users (email, password_hash, balance, role, status, signup_source, created_at, updated_at)
VALUES ($1, 'reward-test-password-hash', $2, 'user', 'active', 'email', NOW(), NOW())
RETURNING id
`, email, balance).Scan(&userID))
	f.userIDs = append(f.userIDs, userID)
	return userID
}

func (f *rewardIntegrationFixture) createActiveCampaign(
	mode string,
	budget, amount float64,
	startsAt, endsAt time.Time,
) *service.RewardCampaign {
	f.t.Helper()
	name := fmt.Sprintf("reward-integration-%d-%d", time.Now().UnixNano(), len(f.campaignIDs))
	campaign, err := f.repo.CreateCampaign(f.ctx, service.RewardCampaign{
		Name:                      name,
		Title:                     "Version one",
		Description:               "reward integration test",
		IssuanceMode:              mode,
		Timezone:                  "UTC",
		StartsAt:                  startsAt,
		EndsAt:                    endsAt,
		Priority:                  100,
		WinProbability:            1,
		PerUserLimit:              10,
		EvaluationIntervalMinutes: 0,
		ClaimCooldownMinutes:      0,
		TotalBudget:               budget,
		Config: service.RewardCampaignConfig{
			Title:          "Version one",
			Priority:       100,
			WinProbability: 1,
			PerUserLimit:   10,
			AmountTiers: []service.RewardAmountTier{
				{Amount: amount, Weight: 1},
			},
			Copy: service.RewardCopy{
				Title:     "Version one",
				Prompt:    "Claim version one",
				CoverText: "Scratch",
			},
		},
	}, nil)
	require.NoError(f.t, err)
	f.campaignIDs = append(f.campaignIDs, campaign.ID)
	published, err := f.repo.TransitionCampaign(f.ctx, campaign.ID, "publish", nil, startsAt.Add(time.Minute))
	require.NoError(f.t, err)
	require.Equal(f.t, service.RewardCampaignStatusActive, published.Status)
	return published
}

func (f *rewardIntegrationFixture) grant(
	campaign service.RewardCampaign,
	userID int64,
	now time.Time,
) *service.RewardGrant {
	f.t.Helper()
	grant, created, err := f.repo.EvaluateAndMaybeGrant(
		f.ctx,
		campaign,
		userID,
		now,
		true,
		false,
		service.RewardGrantSourceOnAccess,
		nil,
	)
	require.NoError(f.t, err)
	require.True(f.t, created)
	require.NotNil(f.t, grant)
	return grant
}

func (f *rewardIntegrationFixture) cleanup() {
	if len(f.userIDs) == 0 && len(f.campaignIDs) == 0 {
		return
	}
	conn, err := integrationDB.Conn(context.Background())
	if err != nil {
		f.t.Errorf("reward integration cleanup connection: %v", err)
		return
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "SET session_replication_role = replica"); err != nil {
		f.t.Errorf("disable triggers for reward integration cleanup: %v", err)
		return
	}
	defer func() {
		if _, err := conn.ExecContext(context.Background(), "SET session_replication_role = origin"); err != nil {
			f.t.Errorf("restore triggers after reward integration cleanup: %v", err)
		}
	}()
	for _, statement := range []string{
		"DELETE FROM user_reward_grants WHERE campaign_id = ANY($1::bigint[])",
		"DELETE FROM reward_campaign_user_states WHERE campaign_id = ANY($1::bigint[])",
		"DELETE FROM reward_campaign_jobs WHERE campaign_id = ANY($1::bigint[])",
		"DELETE FROM reward_campaigns WHERE id = ANY($1::bigint[])",
		"DELETE FROM reward_campaign_versions WHERE campaign_id = ANY($1::bigint[])",
	} {
		if len(f.campaignIDs) == 0 {
			break
		}
		if _, err := conn.ExecContext(context.Background(), statement, pq.Array(f.campaignIDs)); err != nil {
			f.t.Errorf("reward integration cleanup %q: %v", statement, err)
		}
	}
	if len(f.userIDs) > 0 {
		if _, err := conn.ExecContext(
			context.Background(),
			"DELETE FROM redeem_codes WHERE used_by = ANY($1::bigint[]) AND type = $2",
			pq.Array(f.userIDs),
			service.RedeemTypeCampaignReward,
		); err != nil {
			f.t.Errorf("reward integration cleanup redeem codes: %v", err)
		}
		if _, err := conn.ExecContext(
			context.Background(),
			"DELETE FROM users WHERE id = ANY($1::bigint[])",
			pq.Array(f.userIDs),
		); err != nil {
			f.t.Errorf("reward integration cleanup users: %v", err)
		}
	}
}

func TestRewardRepositoryConcurrentEvaluationDoesNotExceedBudget(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		1,
		1,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userIDs := []int64{fixture.createUser(0), fixture.createUser(0)}

	type result struct {
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, len(userIDs))
	var workers sync.WaitGroup
	for _, userID := range userIDs {
		userID := userID
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, created, err := fixture.repo.EvaluateAndMaybeGrant(
				fixture.ctx,
				*campaign,
				userID,
				now,
				true,
				false,
				service.RewardGrantSourceOnAccess,
				nil,
			)
			results <- result{created: created, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	createdCount := 0
	for got := range results {
		require.NoError(t, got.err)
		if got.created {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount)

	var total, reserved, spent float64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT total_budget, reserved_budget, spent_budget
FROM reward_campaigns
WHERE id = $1
`, campaign.ID).Scan(&total, &reserved, &spent))
	require.Equal(t, 1.0, total)
	require.Equal(t, 1.0, reserved)
	require.Zero(t, spent)

	var grantCount int
	require.NoError(t, integrationDB.QueryRowContext(
		fixture.ctx,
		"SELECT COUNT(*) FROM user_reward_grants WHERE campaign_id = $1",
		campaign.ID,
	).Scan(&grantCount))
	require.Equal(t, 1, grantCount)
}

func TestRewardRepositoryConcurrentUserEvaluationHonorsIntervalAndCooldown(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		10,
		1,
		now.Add(-time.Hour),
		now.Add(3*time.Hour),
	)
	updatedInput := *campaign
	updatedInput.EvaluationIntervalMinutes = 60
	updatedInput.ClaimCooldownMinutes = 120
	updatedInput.Config.EvaluationIntervalMinutes = 60
	updatedInput.Config.ClaimCooldownMinutes = 120
	campaign, err := fixture.repo.UpdateCampaign(fixture.ctx, updatedInput, nil)
	require.NoError(t, err)
	userID := fixture.createUser(0)

	type result struct {
		created bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for index := 0; index < 2; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_, created, err := fixture.repo.EvaluateAndMaybeGrant(
				fixture.ctx,
				*campaign,
				userID,
				now,
				true,
				false,
				service.RewardGrantSourceOnAccess,
				nil,
			)
			results <- result{created: created, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	createdCount := 0
	for got := range results {
		require.NoError(t, got.err)
		if got.created {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount, "concurrent evaluation in one interval must create one grant")

	_, created, err := fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx,
		*campaign,
		userID,
		now.Add(30*time.Minute),
		true,
		false,
		service.RewardGrantSourceOnAccess,
		nil,
	)
	require.NoError(t, err)
	require.False(t, created, "evaluation interval has not elapsed")

	_, created, err = fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx,
		*campaign,
		userID,
		now.Add(61*time.Minute),
		true,
		false,
		service.RewardGrantSourceOnAccess,
		nil,
	)
	require.NoError(t, err)
	require.False(t, created, "win cooldown has not elapsed")

	_, created, err = fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx,
		*campaign,
		userID,
		now.Add(121*time.Minute),
		true,
		false,
		service.RewardGrantSourceOnAccess,
		nil,
	)
	require.NoError(t, err)
	require.True(t, created, "user should be eligible when interval and cooldown have elapsed")

	var evaluations, wins, grants int64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT evaluation_count, win_count, grant_count
FROM reward_campaign_user_states
WHERE campaign_id = $1 AND user_id = $2
`, campaign.ID, userID).Scan(&evaluations, &wins, &grants))
	require.Equal(t, int64(3), evaluations)
	require.Equal(t, int64(2), wins)
	require.Equal(t, int64(2), grants)
}

func TestRewardRepositoryBatchRetryDoesNotLeakReservedBudget(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeScheduledBatch,
		10,
		2,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userID := fixture.createUser(0)

	var jobID int64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT id
FROM reward_campaign_jobs
WHERE campaign_id = $1 AND campaign_version_id = $2
ORDER BY id
LIMIT 1
`, campaign.ID, campaign.CurrentVersionID).Scan(&jobID))

	first, created, err := fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx,
		*campaign,
		userID,
		now,
		true,
		false,
		service.RewardGrantSourceScheduledBatch,
		&jobID,
	)
	require.NoError(t, err)
	require.True(t, created)
	require.NotNil(t, first)

	replayed, created, err := fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx,
		*campaign,
		userID,
		now.Add(time.Second),
		true,
		false,
		service.RewardGrantSourceScheduledBatch,
		&jobID,
	)
	require.NoError(t, err)
	require.False(t, created)
	require.NotNil(t, replayed)
	require.Equal(t, first.ID, replayed.ID)

	var reserved, spent float64
	var grants int
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT reserved_budget, spent_budget
FROM reward_campaigns
WHERE id = $1
`, campaign.ID).Scan(&reserved, &spent))
	require.NoError(t, integrationDB.QueryRowContext(
		fixture.ctx,
		"SELECT COUNT(*) FROM user_reward_grants WHERE campaign_id = $1 AND user_id = $2",
		campaign.ID,
		userID,
	).Scan(&grants))
	require.Equal(t, 2.0, reserved, "a duplicate batch cycle must not reserve the reward twice")
	require.Zero(t, spent)
	require.Equal(t, 1, grants)
}

func TestRewardRepositoryBatchRetryDoesNotRedrawLosingDecision(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeScheduledBatch,
		10,
		2,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userID := fixture.createUser(0)

	var jobID int64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT id FROM reward_campaign_jobs WHERE campaign_id = $1 ORDER BY id LIMIT 1
`, campaign.ID).Scan(&jobID))

	grant, created, err := fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx, *campaign, userID, now, false, false,
		service.RewardGrantSourceScheduledBatch, &jobID,
	)
	require.NoError(t, err)
	require.Nil(t, grant)
	require.False(t, created)

	grant, created, err = fixture.repo.EvaluateAndMaybeGrant(
		fixture.ctx, *campaign, userID, now.Add(time.Second), true, false,
		service.RewardGrantSourceScheduledBatch, &jobID,
	)
	require.NoError(t, err)
	require.Nil(t, grant, "retry must replay the stored losing decision")
	require.False(t, created)

	var evaluations, grants int64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT evaluation_count, grant_count
FROM reward_campaign_user_states
WHERE campaign_id = $1 AND user_id = $2
`, campaign.ID, userID).Scan(&evaluations, &grants))
	require.Equal(t, int64(1), evaluations)
	require.Zero(t, grants)
}

func TestRewardRepositoryConcurrentClaimCreditsBalanceOnce(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		2,
		2,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userID := fixture.createUser(10)
	grant := fixture.grant(*campaign, userID, now)

	const callers = 8
	type result struct {
		claim *service.RewardClaimResult
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, callers)
	var workers sync.WaitGroup
	for index := 0; index < callers; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			claim, err := fixture.repo.Claim(fixture.ctx, userID, grant.ID, now.Add(time.Minute))
			results <- result{claim: claim, err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	firstClaims := 0
	replayedClaims := 0
	for got := range results {
		require.NoError(t, got.err)
		require.NotNil(t, got.claim)
		require.Equal(t, 2.0, got.claim.Amount)
		require.Equal(t, 12.0, got.claim.Balance)
		if got.claim.AlreadyClaimed {
			replayedClaims++
		} else {
			firstClaims++
		}
	}
	require.Equal(t, 1, firstClaims)
	require.Equal(t, callers-1, replayedClaims)

	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(
		fixture.ctx,
		"SELECT balance FROM users WHERE id = $1",
		userID,
	).Scan(&balance))
	require.Equal(t, 12.0, balance)

	var reserved, spent float64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT reserved_budget, spent_budget
FROM reward_campaigns
WHERE id = $1
`, campaign.ID).Scan(&reserved, &spent))
	require.Zero(t, reserved)
	require.Equal(t, 2.0, spent)

	var claimRecords int
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT COUNT(*)
FROM redeem_codes
WHERE used_by = $1 AND type = $2
`, userID, service.RedeemTypeCampaignReward).Scan(&claimRecords))
	require.Equal(t, 1, claimRecords)
}

func TestRewardRepositoryViewAfterClaimStillRecordsImpression(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		2,
		2,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userID := fixture.createUser(0)
	grant := fixture.grant(*campaign, userID, now)

	_, err := fixture.repo.Claim(fixture.ctx, userID, grant.ID, now.Add(time.Second))
	require.NoError(t, err)
	viewed, err := fixture.repo.MarkViewed(fixture.ctx, userID, grant.ID, now.Add(2*time.Second))
	require.NoError(t, err)
	require.Equal(t, service.RewardGrantStatusClaimed, viewed.Status)
	require.NotNil(t, viewed.ViewedAt)
}

func TestRewardRepositoryClaimAndExpiryRaceHasOneTerminalOutcome(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	endsAt := now.Add(10 * time.Minute)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		3,
		3,
		now.Add(-time.Hour),
		endsAt,
	)
	userID := fixture.createUser(0)
	grant := fixture.grant(*campaign, userID, now)

	start := make(chan struct{})
	var claimResult *service.RewardClaimResult
	var claimErr error
	var expiredCount int64
	var expireErr error
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		claimResult, claimErr = fixture.repo.Claim(
			fixture.ctx,
			userID,
			grant.ID,
			endsAt.Add(-time.Second),
		)
	}()
	go func() {
		defer workers.Done()
		<-start
		expiredCount, expireErr = fixture.repo.ExpirePendingCampaign(
			fixture.ctx,
			campaign.ID,
			endsAt,
			10,
		)
	}()
	close(start)
	workers.Wait()
	require.NoError(t, expireErr)

	var status string
	var balance, reserved, spent, released float64
	require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT g.status, u.balance, c.reserved_budget, c.spent_budget, c.released_budget
FROM user_reward_grants g
JOIN users u ON u.id = g.user_id
JOIN reward_campaigns c ON c.id = g.campaign_id
WHERE g.id = $1
`, grant.ID).Scan(&status, &balance, &reserved, &spent, &released))
	require.Zero(t, reserved)

	switch status {
	case service.RewardGrantStatusClaimed:
		require.NoError(t, claimErr)
		require.NotNil(t, claimResult)
		require.Equal(t, int64(0), expiredCount)
		require.Equal(t, 3.0, balance)
		require.Equal(t, 3.0, spent)
		require.Zero(t, released)
	case service.RewardGrantStatusExpired:
		require.Error(t, claimErr)
		require.Nil(t, claimResult)
		require.Equal(t, int64(1), expiredCount)
		require.Zero(t, balance)
		require.Zero(t, spent)
		require.Equal(t, 3.0, released)
	default:
		t.Fatalf("grant status = %q, want claimed or expired", status)
	}
}

func TestRewardRepositoryGrantKeepsVersionAndSnapshotAfterCampaignEdit(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeOnAccess,
		100,
		1,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)
	userID := fixture.createUser(0)
	grant := fixture.grant(*campaign, userID, now)
	require.Equal(t, "Version one", grant.Copy.Title)
	require.Equal(t, "Claim version one", grant.Copy.Prompt)
	oldVersionID := grant.VersionID

	updatedInput := *campaign
	updatedInput.Title = "Version two"
	updatedInput.Config.Title = "Version two"
	updatedInput.Config.Copy.Title = "Version two"
	updatedInput.Config.Copy.Prompt = "Claim version two"
	updatedInput.Config.AmountTiers = []service.RewardAmountTier{{Amount: 9, Weight: 1}}
	updated, err := fixture.repo.UpdateCampaign(fixture.ctx, updatedInput, nil)
	require.NoError(t, err)
	require.NotEqual(t, oldVersionID, updated.CurrentVersionID)
	require.Equal(t, 2, updated.CurrentVersion)

	pending, err := fixture.repo.ListPending(fixture.ctx, userID, now)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, oldVersionID, pending[0].VersionID)
	require.Equal(t, 1, pending[0].Version)
	require.Equal(t, 1.0, pending[0].Amount)
	require.Equal(t, "Version one", pending[0].Copy.Title)
	require.Equal(t, "Claim version one", pending[0].Copy.Prompt)

	frozen, err := fixture.repo.GetCampaignVersion(fixture.ctx, campaign.ID, oldVersionID)
	require.NoError(t, err)
	require.Equal(t, oldVersionID, frozen.CurrentVersionID)
	require.Equal(t, 1, frozen.CurrentVersion)
	require.Equal(t, 1.0, frozen.Config.AmountTiers[0].Amount)
	require.Equal(t, "Version one", frozen.Config.Copy.Title)
}

func TestRewardRepositoryLastAPIUsedAtSurvivesThirtyDayMetricWindow(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	userID := fixture.createUser(0)
	lastAPIUsedAt := now.Add(-60 * 24 * time.Hour)
	bucketStart := lastAPIUsedAt.Truncate(time.Hour)
	_, err := integrationDB.ExecContext(fixture.ctx, `
INSERT INTO user_behavior_daily (
    user_id, bucket_start, request_count, actual_cost, recharge_amount,
    last_api_use_at, last_active_at, created_at, updated_at
) VALUES ($1, $2, 9, 4.5, 3, $3, $3, NOW(), NOW())
`, userID, bucketStart, lastAPIUsedAt)
	require.NoError(t, err)

	profile, err := fixture.repo.GetAudienceProfile(fixture.ctx, userID, now)
	require.NoError(t, err)
	require.NotNil(t, profile.LastAPIUsedAt)
	require.WithinDuration(t, lastAPIUsedAt, *profile.LastAPIUsedAt, time.Microsecond)
	require.Zero(t, profile.Requests30D)
	require.Zero(t, profile.ActualCost30D)
	require.Zero(t, profile.Recharge30D)

	profiles, err := fixture.repo.GetAudienceProfiles(fixture.ctx, []int64{userID}, now)
	require.NoError(t, err)
	require.NotNil(t, profiles[userID].LastAPIUsedAt)
	require.WithinDuration(t, lastAPIUsedAt, *profiles[userID].LastAPIUsedAt, time.Microsecond)
	require.Zero(t, profiles[userID].Requests30D)

	eligible, _, err := fixture.repo.EstimateAudience(fixture.ctx, service.RewardAudience{
		AnyOf: []service.RewardAudienceRuleGroup{{AllOf: []service.RewardAudienceRule{
			{Field: "user_id", Operator: "eq", Value: userID},
			{Field: "last_api_used_at", Operator: "before", Value: now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)},
		}}},
	}, now)
	require.NoError(t, err)
	require.Equal(t, int64(1), eligible)
}

func TestRewardRepositoryJobLeaseCanBeReclaimedAndResumed(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeScheduledBatch,
		100,
		1,
		now.Add(-time.Hour),
		now.Add(time.Hour),
	)

	claimed, err := fixture.repo.ClaimJobs(fixture.ctx, "worker-a", now, 100, time.Minute)
	require.NoError(t, err)
	first := findRewardJobForCampaign(claimed, campaign.ID)
	require.NotNil(t, first)
	require.Equal(t, service.RewardJobStatusProcessing, first.Status)
	require.Equal(t, campaign.CurrentVersionID, first.VersionID)
	require.NotNil(t, first.LockedBy)
	require.Equal(t, "worker-a", *first.LockedBy)

	claimedBeforeExpiry, err := fixture.repo.ClaimJobs(
		fixture.ctx,
		"worker-b",
		now.Add(30*time.Second),
		100,
		time.Minute,
	)
	require.NoError(t, err)
	require.Nil(t, findRewardJobForCampaign(claimedBeforeExpiry, campaign.ID))

	reclaimed, err := fixture.repo.ClaimJobs(
		fixture.ctx,
		"worker-b",
		now.Add(2*time.Minute),
		100,
		time.Minute,
	)
	require.NoError(t, err)
	second := findRewardJobForCampaign(reclaimed, campaign.ID)
	require.NotNil(t, second)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.CursorUserID, second.CursorUserID)
	require.Equal(t, first.RetryCount+1, second.RetryCount)
	require.NotNil(t, second.LockedBy)
	require.Equal(t, "worker-b", *second.LockedBy)

	err = fixture.repo.ExtendJobLease(
		fixture.ctx,
		second.ID,
		"worker-a",
		50,
		10,
		8,
		4,
		6,
		0,
		now.Add(2*time.Minute),
		time.Minute,
	)
	require.Error(t, err)
	require.True(t, infraerrors.IsConflict(err), "unexpected lease-loss error: %v", err)

	require.NoError(t, fixture.repo.ExtendJobLease(
		fixture.ctx,
		second.ID,
		"worker-b",
		50,
		10,
		8,
		4,
		6,
		0,
		now.Add(2*time.Minute),
		time.Minute,
	))
	resumed, err := fixture.repo.getJob(fixture.ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, int64(50), resumed.CursorUserID)
	require.Equal(t, int64(10), resumed.ProcessedCount)
	require.Equal(t, int64(8), resumed.EligibleCount)
	require.Equal(t, int64(4), resumed.GrantedCount)
	require.NoError(t, fixture.repo.CompleteJob(
		fixture.ctx,
		second.ID,
		"worker-b",
		now.Add(3*time.Minute),
	))
}

func TestRewardRepositoryScheduledBatchFreezesUserBoundAtActivation(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	startsAt := now.Add(time.Hour)
	endsAt := startsAt.Add(time.Hour)
	fixture.createUser(0)

	campaign, err := fixture.repo.CreateCampaign(fixture.ctx, service.RewardCampaign{
		Name:           fmt.Sprintf("reward-scheduled-%d", time.Now().UnixNano()),
		Title:          "Scheduled reward",
		IssuanceMode:   service.RewardIssuanceModeScheduledBatch,
		Timezone:       "UTC",
		StartsAt:       startsAt,
		EndsAt:         endsAt,
		Priority:       100,
		WinProbability: 1,
		PerUserLimit:   1,
		TotalBudget:    100,
		Config: service.RewardCampaignConfig{
			Title:          "Scheduled reward",
			Priority:       100,
			WinProbability: 1,
			PerUserLimit:   1,
			AmountTiers:    []service.RewardAmountTier{{Amount: 1, Weight: 1}},
			Copy:           service.RewardCopy{Title: "Scheduled reward", Prompt: "Claim", CoverText: "Scratch"},
		},
	}, nil)
	require.NoError(t, err)
	fixture.campaignIDs = append(fixture.campaignIDs, campaign.ID)

	published, err := fixture.repo.TransitionCampaign(fixture.ctx, campaign.ID, "publish", nil, now)
	require.NoError(t, err)
	require.Equal(t, service.RewardCampaignStatusScheduled, published.Status)
	jobs, _, err := fixture.repo.ListCampaignJobs(fixture.ctx, campaign.ID, 20, 0)
	require.NoError(t, err)
	require.Empty(t, jobs)

	registeredBeforeActivation := fixture.createUser(0)
	inserted, err := fixture.repo.EnqueueScheduledCampaigns(fixture.ctx, startsAt.Add(time.Second))
	require.NoError(t, err)
	require.GreaterOrEqual(t, inserted, int64(1))
	jobs, _, err = fixture.repo.ListCampaignJobs(fixture.ctx, campaign.ID, 20, 0)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.GreaterOrEqual(t, jobs[0].MaxUserID, registeredBeforeActivation)
}

func TestRewardRepositoryPausedBatchEditRecoversExpiredProcessingLease(t *testing.T) {
	fixture := newRewardIntegrationFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	campaign := fixture.createActiveCampaign(
		service.RewardIssuanceModeScheduledBatch,
		100,
		1,
		now.Add(-2*time.Hour),
		now.Add(time.Hour),
	)

	claimed, err := fixture.repo.ClaimJobs(fixture.ctx, "crashed-worker", now.Add(-time.Minute), 100, time.Second)
	require.NoError(t, err)
	job := findRewardJobForCampaign(claimed, campaign.ID)
	require.NotNil(t, job)
	require.Equal(t, service.RewardJobStatusProcessing, job.Status)

	paused, err := fixture.repo.TransitionCampaign(fixture.ctx, campaign.ID, "pause", nil, now)
	require.NoError(t, err)
	require.Equal(t, service.RewardCampaignStatusPaused, paused.Status)

	updatedInput := *paused
	updatedInput.Name += " updated"
	updated, err := fixture.repo.UpdateCampaign(fixture.ctx, updatedInput, nil)
	require.NoError(t, err)
	require.Equal(t, 2, updated.CurrentVersion)

	oldJob, err := fixture.repo.getJob(fixture.ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, service.RewardJobStatusCancelled, oldJob.Status)
	jobs, _, err := fixture.repo.ListCampaignJobs(fixture.ctx, campaign.ID, 20, 0)
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	require.Equal(t, service.RewardJobStatusPaused, jobs[0].Status)
	require.Equal(t, updated.CurrentVersionID, jobs[0].VersionID)
	require.Equal(t, job.CursorUserID, jobs[0].CursorUserID)
	require.Equal(t, job.MaxUserID, jobs[0].MaxUserID)
}

func findRewardJobForCampaign(jobs []service.RewardCampaignJob, campaignID int64) *service.RewardCampaignJob {
	for index := range jobs {
		if jobs[index].CampaignID == campaignID {
			return &jobs[index]
		}
	}
	return nil
}
