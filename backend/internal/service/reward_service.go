package service

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type RewardAudienceProfile struct {
	UserID               int64
	Role                 string
	Status               string
	RegisteredAt         time.Time
	SignupSource         string
	LastActiveAt         *time.Time
	Balance              float64
	SubscriptionGroupIDs []int64
	Requests7D           int64
	Requests30D          int64
	ActualCost7D         float64
	ActualCost30D        float64
	LastAPIUsedAt        *time.Time
	Recharge30D          float64
	TotalRecharged       float64
}

type RewardRepository interface {
	ImportLegacyPending(ctx context.Context, userID int64, now time.Time) error
	ListRuntimeCampaigns(ctx context.Context, issuanceMode string, now time.Time) ([]RewardCampaign, error)
	GetAudienceProfile(ctx context.Context, userID int64, now time.Time) (*RewardAudienceProfile, error)
	GetAudienceProfiles(ctx context.Context, userIDs []int64, now time.Time) (map[int64]RewardAudienceProfile, error)
	EvaluateAndMaybeGrant(ctx context.Context, campaign RewardCampaign, userID int64, now time.Time, shouldAward, controlGroup bool, source string, jobID *int64) (*RewardGrant, bool, error)
	ExpirePendingForUser(ctx context.Context, userID int64, now time.Time) error
	ExpirePendingCampaign(ctx context.Context, campaignID int64, now time.Time, limit int) (int64, error)
	ListPending(ctx context.Context, userID int64, now time.Time) ([]RewardGrant, error)
	MarkViewed(ctx context.Context, userID, grantID int64, now time.Time) (*RewardGrant, error)
	Claim(ctx context.Context, userID, grantID int64, now time.Time) (*RewardClaimResult, error)
	FindPendingBySystemKey(ctx context.Context, userID int64, systemKey string, now time.Time) (*RewardGrant, error)
	FindLatestBySystemKey(ctx context.Context, userID int64, systemKey string) (*RewardGrant, error)

	CreateCampaign(ctx context.Context, campaign RewardCampaign, actorID *int64) (*RewardCampaign, error)
	UpdateCampaign(ctx context.Context, campaign RewardCampaign, actorID *int64) (*RewardCampaign, error)
	CloneCampaign(ctx context.Context, campaignID int64, actorID *int64) (*RewardCampaign, error)
	GetCampaign(ctx context.Context, campaignID int64) (*RewardCampaign, error)
	GetCampaignVersion(ctx context.Context, campaignID, versionID int64) (*RewardCampaign, error)
	ListCampaigns(ctx context.Context, filter RewardCampaignListFilter) ([]RewardCampaign, int64, error)
	TransitionCampaign(ctx context.Context, campaignID int64, action string, actorID *int64, now time.Time) (*RewardCampaign, error)
	EstimateAudience(ctx context.Context, audience RewardAudience, now time.Time) (int64, time.Time, error)
	CampaignStats(ctx context.Context, campaignID int64) (*RewardCampaignStats, error)
	ListCampaignGrants(ctx context.Context, campaignID int64, filter RewardGrantListFilter) ([]RewardGrant, int64, error)
	ListCampaignJobs(ctx context.Context, campaignID int64, limit, offset int) ([]RewardCampaignJob, int64, error)

	CreateSkin(ctx context.Context, skin RewardSkin, content []byte, actorID *int64) (*RewardSkin, error)
	ListSkins(ctx context.Context, includeArchived bool) ([]RewardSkin, error)
	UpdateSkin(ctx context.Context, skinID int64, name, description, altText, status *string, actorID *int64) (*RewardSkin, error)
	GetSkinContent(ctx context.Context, skinID int64) (mimeType, sha256 string, content []byte, err error)

	EndExpiredCampaigns(ctx context.Context, now time.Time) error
	EnqueueScheduledCampaigns(ctx context.Context, now time.Time) (int64, error)
	ClaimJobs(ctx context.Context, workerID string, now time.Time, limit int, lease time.Duration) ([]RewardCampaignJob, error)
	ListBatchCandidateUserIDs(ctx context.Context, campaignID, afterUserID, maxUserID int64, limit int) ([]int64, error)
	ExtendJobLease(ctx context.Context, jobID int64, workerID string, cursorUserID int64, scanned, matched, granted, skipped, failed int64, now time.Time, lease time.Duration) error
	CompleteJob(ctx context.Context, jobID int64, workerID string, now time.Time) error
	ReleaseJob(ctx context.Context, jobID int64, workerID string, now time.Time) error
	RetryJob(ctx context.Context, jobID int64, workerID string, now time.Time, cause error) error
}

type RewardService struct {
	repo                 RewardRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCache         BillingCache
	now                  func() time.Time
}

func NewRewardService(repo RewardRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, billingCache BillingCache) *RewardService {
	return &RewardService{
		repo:                 repo,
		authCacheInvalidator: authCacheInvalidator,
		billingCache:         billingCache,
		now:                  time.Now,
	}
}

func (s *RewardService) Pending(ctx context.Context, userID int64) ([]RewardGrant, error) {
	if userID <= 0 {
		return nil, infraerrors.BadRequest("REWARD_INVALID_USER", "invalid user")
	}
	now := s.now().UTC()
	if err := s.repo.ImportLegacyPending(ctx, userID, now); err != nil {
		return nil, fmt.Errorf("import legacy rewards: %w", err)
	}
	if err := s.repo.ExpirePendingForUser(ctx, userID, now); err != nil {
		return nil, fmt.Errorf("expire pending rewards: %w", err)
	}
	campaigns, err := s.repo.ListRuntimeCampaigns(ctx, RewardIssuanceModeOnAccess, now)
	if err != nil {
		return nil, fmt.Errorf("list active reward campaigns: %w", err)
	}
	if len(campaigns) > 0 {
		profile, err := s.repo.GetAudienceProfile(ctx, userID, now)
		if err != nil {
			return nil, fmt.Errorf("load reward audience profile: %w", err)
		}
		for _, campaign := range campaigns {
			if (profile.Role != "" && profile.Role != "user") ||
				(profile.Status != "" && profile.Status != "active") {
				break
			}
			if !RewardAudienceMatches(ResolveRewardAudienceRelativeTimes(campaign.Config.Audience, now), *profile) {
				continue
			}
			control := rewardControlGroup(campaign.ID, userID, campaign.ControlGroupPercent)
			shouldAward := !control && secureProbability(campaign.WinProbability)
			if _, _, err := s.repo.EvaluateAndMaybeGrant(
				ctx, campaign, userID, now, shouldAward, control, RewardGrantSourceOnAccess, nil,
			); err != nil {
				return nil, fmt.Errorf("evaluate reward campaign %d: %w", campaign.ID, err)
			}
		}
	}
	grants, err := s.repo.ListPending(ctx, userID, now)
	if err != nil {
		return nil, fmt.Errorf("list pending rewards: %w", err)
	}
	return grants, nil
}

func (s *RewardService) MarkViewed(ctx context.Context, userID, grantID int64) (*RewardGrant, error) {
	if grantID <= 0 {
		return nil, infraerrors.BadRequest("REWARD_INVALID_GRANT", "invalid reward grant")
	}
	grant, err := s.repo.MarkViewed(ctx, userID, grantID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	return grant, nil
}

func (s *RewardService) Claim(ctx context.Context, userID, grantID int64) (*RewardClaimResult, error) {
	if grantID <= 0 {
		return nil, infraerrors.BadRequest("REWARD_INVALID_GRANT", "invalid reward grant")
	}
	result, err := s.repo.Claim(ctx, userID, grantID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	s.invalidateBalanceCaches(ctx, userID)
	return result, nil
}

func (s *RewardService) LegacyPending(ctx context.Context, userID int64, systemKey string) (bool, error) {
	now := s.now().UTC()
	if err := s.repo.ImportLegacyPending(ctx, userID, now); err != nil {
		return false, err
	}
	if err := s.repo.ExpirePendingForUser(ctx, userID, now); err != nil {
		return false, err
	}
	grant, err := s.repo.FindPendingBySystemKey(ctx, userID, systemKey, now)
	if err == nil {
		return grant != nil, nil
	}
	if !infraerrors.IsNotFound(err) {
		return false, err
	}

	campaigns, err := s.repo.ListRuntimeCampaigns(ctx, RewardIssuanceModeOnAccess, now)
	if err != nil {
		return false, err
	}
	var campaign *RewardCampaign
	for i := range campaigns {
		if campaigns[i].CampaignKey == systemKey {
			campaign = &campaigns[i]
			break
		}
	}
	if campaign == nil {
		return false, nil
	}
	profile, err := s.repo.GetAudienceProfile(ctx, userID, now)
	if err != nil {
		return false, err
	}
	if (profile.Role != "" && profile.Role != "user") ||
		(profile.Status != "" && profile.Status != "active") ||
		!RewardAudienceMatches(ResolveRewardAudienceRelativeTimes(campaign.Config.Audience, now), *profile) {
		return false, nil
	}
	control := rewardControlGroup(campaign.ID, userID, campaign.ControlGroupPercent)
	shouldAward := !control && secureProbability(campaign.WinProbability)
	if _, _, err := s.repo.EvaluateAndMaybeGrant(
		ctx, *campaign, userID, now, shouldAward, control, RewardGrantSourceOnAccess, nil,
	); err != nil {
		return false, err
	}
	grant, err = s.repo.FindPendingBySystemKey(ctx, userID, systemKey, now)
	if infraerrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return grant != nil, nil
}

func (s *RewardService) ClaimLegacy(ctx context.Context, userID int64, systemKey string) (*RewardClaimResult, error) {
	pending, err := s.LegacyPending(ctx, userID, systemKey)
	if err != nil {
		return nil, err
	}
	var grant *RewardGrant
	if pending {
		grant, err = s.repo.FindPendingBySystemKey(ctx, userID, systemKey, s.now().UTC())
	} else {
		// A legacy client can retry after the first claim committed but its HTTP
		// response was lost. Replay the latest claimed grant through Claim so the
		// original amount and post-claim balance are returned without another credit.
		grant, err = s.repo.FindLatestBySystemKey(ctx, userID, systemKey)
	}
	if infraerrors.IsNotFound(err) {
		return nil, infraerrors.Conflict("REWARD_UNAVAILABLE", "reward is unavailable")
	}
	if err != nil {
		return nil, err
	}
	return s.Claim(ctx, userID, grant.ID)
}

func (s *RewardService) CreateCampaign(ctx context.Context, input CreateRewardCampaignInput) (*RewardCampaign, error) {
	campaign := input.Campaign
	campaign.Status = RewardCampaignStatusDraft
	if err := normalizeRewardCampaign(&campaign); err != nil {
		return nil, infraerrors.BadRequest("REWARD_CAMPAIGN_INVALID", err.Error())
	}
	return s.repo.CreateCampaign(ctx, campaign, input.ActorID)
}

func (s *RewardService) UpdateCampaign(ctx context.Context, id int64, input UpdateRewardCampaignInput) (*RewardCampaign, error) {
	current, err := s.repo.GetCampaign(ctx, id)
	if err != nil {
		return nil, err
	}
	campaign := input.Campaign
	campaign.ID = id
	campaign.Status = current.Status
	campaign.System = current.System
	campaign.CampaignKey = current.CampaignKey
	campaign.ReservedBudget = current.ReservedBudget
	campaign.SpentBudget = current.SpentBudget
	campaign.ReleasedBudget = current.ReleasedBudget
	if err := normalizeRewardCampaign(&campaign); err != nil {
		return nil, infraerrors.BadRequest("REWARD_CAMPAIGN_INVALID", err.Error())
	}
	return s.repo.UpdateCampaign(ctx, campaign, input.ActorID)
}

func (s *RewardService) CloneCampaign(ctx context.Context, id int64, actorID *int64) (*RewardCampaign, error) {
	return s.repo.CloneCampaign(ctx, id, actorID)
}

func (s *RewardService) GetCampaign(ctx context.Context, id int64) (*RewardCampaign, error) {
	return s.repo.GetCampaign(ctx, id)
}

func (s *RewardService) ListCampaigns(ctx context.Context, filter RewardCampaignListFilter) ([]RewardCampaign, int64, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 20
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return s.repo.ListCampaigns(ctx, filter)
}

func (s *RewardService) TransitionCampaign(ctx context.Context, id int64, action string, actorID *int64) (*RewardCampaign, error) {
	switch action {
	case "publish", "pause", "resume", "end", "archive":
	default:
		return nil, infraerrors.BadRequest("REWARD_CAMPAIGN_ACTION_INVALID", "invalid campaign action")
	}
	return s.repo.TransitionCampaign(ctx, id, action, actorID, s.now().UTC())
}

func (s *RewardService) Estimate(ctx context.Context, campaign RewardCampaign) (*RewardAudienceEstimate, error) {
	if err := normalizeRewardCampaign(&campaign); err != nil {
		return nil, infraerrors.BadRequest("REWARD_CAMPAIGN_INVALID", err.Error())
	}
	eligible, updatedAt, err := s.repo.EstimateAudience(ctx, campaign.Config.Audience, s.now().UTC())
	if err != nil {
		return nil, err
	}
	reachable := float64(eligible) * (1 - campaign.ControlGroupPercent/100)
	expectedWinners := int64(math.Round(reachable * campaign.WinProbability))
	if expectedWinners < 0 {
		expectedWinners = 0
	}
	maximumWinners := eligible * int64(campaign.PerUserLimit)
	return &RewardAudienceEstimate{
		EligibleUsers:   eligible,
		ExpectedWinners: expectedWinners,
		ExpectedCost:    float64(expectedWinners) * ExpectedRewardAmount(campaign.Config.AmountTiers),
		MaximumCost:     float64(maximumWinners) * MaximumRewardAmount(campaign.Config.AmountTiers),
		DataUpdatedAt:   updatedAt,
	}, nil
}

func (s *RewardService) CampaignStats(ctx context.Context, id int64) (*RewardCampaignStats, error) {
	return s.repo.CampaignStats(ctx, id)
}

func (s *RewardService) CampaignGrants(ctx context.Context, id int64, filter RewardGrantListFilter) ([]RewardGrant, int64, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	filter.Offset = max(filter.Offset, 0)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Search = strings.TrimSpace(filter.Search)
	return s.repo.ListCampaignGrants(ctx, id, filter)
}

func (s *RewardService) CampaignJobs(ctx context.Context, id int64, limit, offset int) ([]RewardCampaignJob, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListCampaignJobs(ctx, id, limit, max(offset, 0))
}

func (s *RewardService) EnqueueScheduled(ctx context.Context) (int64, error) {
	return s.repo.EnqueueScheduledCampaigns(ctx, s.now().UTC())
}

func (s *RewardService) CreateSkin(ctx context.Context, skin RewardSkin, content []byte, actorID *int64) (*RewardSkin, error) {
	if strings.TrimSpace(skin.Name) == "" || len(skin.Name) > 120 {
		return nil, infraerrors.BadRequest("REWARD_SKIN_INVALID", "skin name is required")
	}
	if skin.Width != 1320 || skin.Height != 500 {
		return nil, infraerrors.BadRequest("REWARD_SKIN_DIMENSIONS_INVALID", "skin image must be 1320x500")
	}
	if len(content) == 0 || len(content) > 1024*1024 {
		return nil, infraerrors.BadRequest("REWARD_SKIN_SIZE_INVALID", "skin image must not exceed 1 MB")
	}
	switch skin.MIMEType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return nil, infraerrors.BadRequest("REWARD_SKIN_MIME_INVALID", "skin image must be PNG, JPEG, or WebP")
	}
	return s.repo.CreateSkin(ctx, skin, content, actorID)
}

func (s *RewardService) ListSkins(ctx context.Context, includeArchived bool) ([]RewardSkin, error) {
	return s.repo.ListSkins(ctx, includeArchived)
}

func (s *RewardService) UpdateSkin(ctx context.Context, id int64, name, description, altText, status *string, actorID *int64) (*RewardSkin, error) {
	if status != nil && *status != RewardSkinStatusActive && *status != RewardSkinStatusInactive && *status != RewardSkinStatusArchived {
		return nil, infraerrors.BadRequest("REWARD_SKIN_STATUS_INVALID", "invalid reward skin status")
	}
	return s.repo.UpdateSkin(ctx, id, name, description, altText, status, actorID)
}

func (s *RewardService) SkinContent(ctx context.Context, id int64) (string, string, []byte, error) {
	return s.repo.GetSkinContent(ctx, id)
}

func normalizeRewardCampaign(campaign *RewardCampaign) error {
	if campaign.Config.Title == "" {
		campaign.Config.Title = campaign.Title
	}
	if campaign.Config.Priority == 0 && campaign.Priority != 0 {
		campaign.Config.Priority = campaign.Priority
	}
	if campaign.Config.WinProbability == 0 && campaign.WinProbability != 0 {
		campaign.Config.WinProbability = campaign.WinProbability
	}
	if campaign.Config.PerUserLimit == 0 {
		campaign.Config.PerUserLimit = campaign.PerUserLimit
	}
	if campaign.Config.EvaluationIntervalMinutes == 0 {
		campaign.Config.EvaluationIntervalMinutes = campaign.EvaluationIntervalMinutes
	}
	if campaign.Config.ClaimCooldownMinutes == 0 {
		campaign.Config.ClaimCooldownMinutes = campaign.ClaimCooldownMinutes
	}
	if campaign.Config.ControlGroupPercent == 0 {
		campaign.Config.ControlGroupPercent = campaign.ControlGroupPercent
	}
	if err := campaign.NormalizeAndValidate(); err != nil {
		return err
	}
	campaign.Title = campaign.Config.Title
	campaign.Priority = campaign.Config.Priority
	campaign.WinProbability = campaign.Config.WinProbability
	campaign.PerUserLimit = campaign.Config.PerUserLimit
	campaign.EvaluationIntervalMinutes = campaign.Config.EvaluationIntervalMinutes
	campaign.ClaimCooldownMinutes = campaign.Config.ClaimCooldownMinutes
	campaign.ControlGroupPercent = campaign.Config.ControlGroupPercent
	return nil
}

func (s *RewardService) invalidateBalanceCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCache != nil {
		if err := s.billingCache.InvalidateUserBalance(ctx, userID); err != nil {
			slog.Error("invalidate reward balance cache failed", "user_id", userID, "error", err)
		}
	}
}

func secureProbability(probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return false
	}
	value := float64(binary.BigEndian.Uint64(raw[:])>>11) / float64(uint64(1)<<53)
	return value < probability
}

func rewardControlGroup(campaignID, userID int64, percent float64) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d", campaignID, userID)
	return float64(h.Sum64()%10000) < percent*100
}

func RewardAudienceMatches(audience RewardAudience, profile RewardAudienceProfile) bool {
	if len(audience.AnyOf) == 0 {
		return true
	}
	for _, group := range audience.AnyOf {
		matches := true
		for _, rule := range group.AllOf {
			if !rewardAudienceRuleMatches(rule, profile) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func ResolveRewardAudienceRelativeTimes(audience RewardAudience, now time.Time) RewardAudience {
	resolved := RewardAudience{AnyOf: make([]RewardAudienceRuleGroup, len(audience.AnyOf))}
	for groupIndex, group := range audience.AnyOf {
		resolved.AnyOf[groupIndex].AllOf = append([]RewardAudienceRule(nil), group.AllOf...)
	}
	audience = resolved
	for groupIndex := range audience.AnyOf {
		for ruleIndex := range audience.AnyOf[groupIndex].AllOf {
			rule := &audience.AnyOf[groupIndex].AllOf[ruleIndex]
			value, ok := rule.Value.(map[string]any)
			if !ok {
				continue
			}
			if days, valid := rewardFloat(value["relative_days"]); valid {
				rule.Value = now.Add(time.Duration(days*24) * time.Hour).Format(time.RFC3339)
				continue
			}
			if hours, valid := rewardFloat(value["relative_hours"]); valid {
				rule.Value = now.Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)
			}
		}
	}
	return audience
}

func rewardAudienceRuleMatches(rule RewardAudienceRule, profile RewardAudienceProfile) bool {
	var value any
	switch rule.Field {
	case "registered_at":
		value = profile.RegisteredAt
	case "signup_source":
		value = profile.SignupSource
	case "last_active_at":
		value = profile.LastActiveAt
	case "balance":
		value = profile.Balance
	case "subscription_group_id":
		return compareInt64Membership(profile.SubscriptionGroupIDs, rule.Operator, rule.Value)
	case "user_id":
		value = profile.UserID
	case "requests_7d":
		value = profile.Requests7D
	case "requests_30d":
		value = profile.Requests30D
	case "actual_cost_7d":
		value = profile.ActualCost7D
	case "actual_cost_30d":
		value = profile.ActualCost30D
	case "last_api_used_at":
		value = profile.LastAPIUsedAt
	case "recharge_30d":
		value = profile.Recharge30D
	case "total_recharged":
		value = profile.TotalRecharged
	default:
		return false
	}
	return compareRewardValue(value, rule.Operator, rule.Value)
}

func compareRewardValue(actual any, operator string, expected any) bool {
	if actualTime, ok := rewardTime(actual); ok {
		if operator == "between" {
			values := rewardSlice(expected)
			if len(values) != 2 {
				return false
			}
			low, okLow := rewardTime(values[0])
			high, okHigh := rewardTime(values[1])
			return okLow && okHigh && !actualTime.Before(low) && !actualTime.After(high)
		}
		expectedTime, ok := rewardTime(expected)
		if !ok {
			return false
		}
		switch operator {
		case "eq":
			return actualTime.Equal(expectedTime)
		case "neq":
			return !actualTime.Equal(expectedTime)
		case "before", "lt":
			return actualTime.Before(expectedTime)
		case "after", "gt":
			return actualTime.After(expectedTime)
		case "lte":
			return !actualTime.After(expectedTime)
		case "gte":
			return !actualTime.Before(expectedTime)
		}
	}
	if actualNumber, ok := rewardFloat(actual); ok {
		if operator == "in" || operator == "not_in" {
			found := false
			for _, item := range rewardSlice(expected) {
				if number, valid := rewardFloat(item); valid && actualNumber == number {
					found = true
					break
				}
			}
			if operator == "not_in" {
				return !found
			}
			return found
		}
		if operator == "between" {
			values := rewardSlice(expected)
			if len(values) != 2 {
				return false
			}
			low, lowOK := rewardFloat(values[0])
			high, highOK := rewardFloat(values[1])
			return lowOK && highOK && actualNumber >= low && actualNumber <= high
		}
		expectedNumber, ok := rewardFloat(expected)
		if !ok {
			return false
		}
		switch operator {
		case "eq":
			return actualNumber == expectedNumber
		case "neq":
			return actualNumber != expectedNumber
		case "gt":
			return actualNumber > expectedNumber
		case "gte":
			return actualNumber >= expectedNumber
		case "lt":
			return actualNumber < expectedNumber
		case "lte":
			return actualNumber <= expectedNumber
		}
	}
	actualString, ok := actual.(string)
	if !ok {
		return false
	}
	if operator == "in" || operator == "not_in" {
		found := false
		for _, item := range rewardSlice(expected) {
			if text, valid := item.(string); valid && actualString == text {
				found = true
				break
			}
		}
		if operator == "not_in" {
			return !found
		}
		return found
	}
	expectedString, ok := expected.(string)
	if !ok {
		return false
	}
	if operator == "eq" {
		return actualString == expectedString
	}
	if operator == "neq" {
		return actualString != expectedString
	}
	return false
}

func compareInt64Membership(actual []int64, operator string, expected any) bool {
	expectedValues := rewardSlice(expected)
	if operator != "in" && operator != "not_in" && operator != "eq" && operator != "neq" {
		return false
	}
	if operator == "eq" || operator == "neq" {
		expectedValues = []any{expected}
	}
	found := false
	for _, candidate := range actual {
		for _, raw := range expectedValues {
			value, ok := rewardFloat(raw)
			if ok && candidate == int64(value) {
				found = true
				break
			}
		}
	}
	if operator == "not_in" || operator == "neq" {
		return !found
	}
	return found
}

func rewardSlice(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var values []any
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func rewardFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func rewardTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, !typed.IsZero()
	case *time.Time:
		if typed == nil {
			return time.Time{}, false
		}
		return *typed, !typed.IsZero()
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		return parsed, err == nil
	default:
		return time.Time{}, false
	}
}
