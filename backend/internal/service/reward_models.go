package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	RewardCampaignStatusDraft     = "draft"
	RewardCampaignStatusScheduled = "scheduled"
	RewardCampaignStatusActive    = "active"
	RewardCampaignStatusPaused    = "paused"
	RewardCampaignStatusEnded     = "ended"
	RewardCampaignStatusArchived  = "archived"

	RewardIssuanceModeOnAccess       = "on_access"
	RewardIssuanceModeScheduledBatch = "scheduled_batch"

	RewardGrantStatusPending   = "pending"
	RewardGrantStatusClaimed   = "claimed"
	RewardGrantStatusExpired   = "expired"
	RewardGrantStatusCancelled = "cancelled"

	RewardGrantSourceOnAccess       = "on_access"
	RewardGrantSourceScheduledBatch = "scheduled_batch"
	RewardGrantSourceLegacyWelcome  = "legacy_welcome"
	RewardGrantSourceLegacySurprise = "legacy_surprise"

	RewardJobStatusPending    = "pending"
	RewardJobStatusProcessing = "processing"
	RewardJobStatusRetry      = "retry"
	RewardJobStatusSucceeded  = "succeeded"
	RewardJobStatusFailed     = "failed"
	RewardJobStatusDeadLetter = "dead_letter"
	RewardJobStatusPaused     = "paused"
	RewardJobStatusCancelled  = "cancelled"

	RewardSkinStatusActive   = "active"
	RewardSkinStatusInactive = "inactive"
	RewardSkinStatusArchived = "archived"

	RewardSystemCampaignWelcome  = "system_welcome"
	RewardSystemCampaignSurprise = "system_surprise"
	RedeemTypeCampaignReward     = "campaign_reward"
)

type RewardAmountTier struct {
	Amount float64 `json:"amount"`
	Weight int     `json:"weight"`
}

type RewardSkinWeight struct {
	SkinID int64 `json:"skin_id"`
	Weight int   `json:"weight"`
}

type RewardCopy struct {
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	CoverText    string `json:"cover_text"`
	GestureHint  string `json:"gesture_hint"`
	RevealedHint string `json:"revealed_hint"`
	WonText      string `json:"won_text"`
	CreditedText string `json:"credited_text"`
	ContinueText string `json:"continue_text"`
}

type RewardAudienceRule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type RewardAudienceRuleGroup struct {
	AllOf []RewardAudienceRule `json:"all_of"`
}

// RewardAudience uses OR between groups and AND within a group.
type RewardAudience struct {
	AnyOf []RewardAudienceRuleGroup `json:"any_of"`
}

type RewardCampaignConfig struct {
	Title                     string                `json:"title"`
	Priority                  int                   `json:"priority"`
	WinProbability            float64               `json:"win_probability"`
	PerUserLimit              int                   `json:"per_user_limit"`
	EvaluationIntervalMinutes int                   `json:"evaluation_interval_minutes"`
	ClaimCooldownMinutes      int                   `json:"claim_cooldown_minutes"`
	ControlGroupPercent       float64               `json:"control_group_percent"`
	AmountTiers               []RewardAmountTier    `json:"amount_tiers"`
	Audience                  RewardAudience        `json:"audience"`
	Copy                      RewardCopy            `json:"copy"`
	CopyI18n                  map[string]RewardCopy `json:"copy_i18n,omitempty"`
	SkinWeights               []RewardSkinWeight    `json:"skin_weights"`
}

type RewardCampaign struct {
	ID                        int64                `json:"id"`
	CampaignKey               string               `json:"campaign_key"`
	Name                      string               `json:"name"`
	Title                     string               `json:"title"`
	Description               string               `json:"description"`
	System                    bool                 `json:"system"`
	Status                    string               `json:"status"`
	IssuanceMode              string               `json:"issuance_mode"`
	Timezone                  string               `json:"timezone"`
	StartsAt                  time.Time            `json:"starts_at"`
	EndsAt                    time.Time            `json:"ends_at"`
	Priority                  int                  `json:"priority"`
	WinProbability            float64              `json:"win_probability"`
	PerUserLimit              int                  `json:"per_user_limit"`
	EvaluationIntervalMinutes int                  `json:"evaluation_interval_minutes"`
	ClaimCooldownMinutes      int                  `json:"claim_cooldown_minutes"`
	TotalBudget               float64              `json:"total_budget"`
	ReservedBudget            float64              `json:"reserved_budget"`
	SpentBudget               float64              `json:"spent_budget"`
	ReleasedBudget            float64              `json:"released_budget"`
	ControlGroupPercent       float64              `json:"control_group_percent"`
	CurrentVersionID          int64                `json:"current_version_id"`
	CurrentVersion            int                  `json:"current_version"`
	Config                    RewardCampaignConfig `json:"config"`
	CreatedBy                 *int64               `json:"created_by,omitempty"`
	UpdatedBy                 *int64               `json:"updated_by,omitempty"`
	CreatedAt                 time.Time            `json:"created_at"`
	UpdatedAt                 time.Time            `json:"updated_at"`
}

func (c RewardCampaign) AvailableBudget() float64 {
	available := c.TotalBudget - c.ReservedBudget - c.SpentBudget
	if available < 0 && available > -1e-8 {
		return 0
	}
	return available
}

type RewardCampaignVersion struct {
	ID         int64                `json:"id"`
	CampaignID int64                `json:"campaign_id"`
	Version    int                  `json:"version"`
	Config     RewardCampaignConfig `json:"config"`
	CreatedBy  *int64               `json:"created_by,omitempty"`
	CreatedAt  time.Time            `json:"created_at"`
}

type RewardSkin struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	AltText     string     `json:"alt_text"`
	Status      string     `json:"status"`
	MIMEType    string     `json:"mime_type"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	SizeBytes   int64      `json:"size_bytes"`
	SHA256      string     `json:"sha256"`
	ImageURL    string     `json:"image_url"`
	CreatedBy   *int64     `json:"created_by,omitempty"`
	UpdatedBy   *int64     `json:"updated_by,omitempty"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type RewardSkinSnapshot struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
	SHA256      string `json:"sha256"`
}

type RewardGrant struct {
	ID            int64                 `json:"grant_id"`
	CampaignID    int64                 `json:"campaign_id"`
	CampaignTitle string                `json:"campaign_title"`
	CampaignKey   string                `json:"-"`
	VersionID     int64                 `json:"campaign_version_id"`
	Version       int                   `json:"campaign_version"`
	UserID        int64                 `json:"user_id,omitempty"`
	CycleKey      string                `json:"cycle_key,omitempty"`
	Amount        float64               `json:"-"`
	Status        string                `json:"status"`
	Source        string                `json:"source"`
	Priority      int                   `json:"priority"`
	Copy          RewardCopy            `json:"copy"`
	CopyI18n      map[string]RewardCopy `json:"-"`
	Skin          RewardSkinSnapshot    `json:"skin"`
	ExpiresAt     time.Time             `json:"expires_at"`
	ViewedAt      *time.Time            `json:"viewed_at,omitempty"`
	ClaimedAt     *time.Time            `json:"claimed_at,omitempty"`
	BalanceAfter  *float64              `json:"-"`
	CreatedAt     time.Time             `json:"created_at"`
	UpdatedAt     time.Time             `json:"updated_at"`
}

type RewardClaimResult struct {
	GrantID        int64     `json:"grant_id"`
	Amount         float64   `json:"amount"`
	Balance        float64   `json:"balance"`
	ClaimedAt      time.Time `json:"claimed_at"`
	AlreadyClaimed bool      `json:"already_claimed"`
}

type RewardAudienceEstimate struct {
	EligibleUsers   int64     `json:"eligible_users"`
	ExpectedWinners int64     `json:"expected_winners"`
	ExpectedCost    float64   `json:"expected_cost"`
	MaximumCost     float64   `json:"maximum_cost"`
	DataUpdatedAt   time.Time `json:"data_updated_at"`
}

type RewardCampaignStats struct {
	Evaluated    int64             `json:"evaluated"`
	Won          int64             `json:"won"`
	Viewed       int64             `json:"viewed"`
	Claimed      int64             `json:"claimed"`
	Expired      int64             `json:"expired"`
	Pending      int64             `json:"pending"`
	ControlGroup int64             `json:"control_group"`
	Amounts      map[string]int64  `json:"amount_distribution"`
	Budget       RewardBudgetStats `json:"budget"`
}

type RewardBudgetStats struct {
	Total     float64 `json:"total"`
	Reserved  float64 `json:"reserved"`
	Spent     float64 `json:"spent"`
	Released  float64 `json:"released"`
	Available float64 `json:"available"`
}

type RewardCampaignJob struct {
	ID             int64      `json:"id"`
	CampaignID     int64      `json:"campaign_id"`
	VersionID      int64      `json:"campaign_version_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Status         string     `json:"status"`
	ScheduledAt    time.Time  `json:"scheduled_at"`
	AvailableAt    time.Time  `json:"available_at"`
	CursorUserID   int64      `json:"cursor_user_id"`
	MaxUserID      int64      `json:"max_user_id"`
	ProcessedCount int64      `json:"processed_count"`
	EligibleCount  int64      `json:"eligible_count"`
	GrantedCount   int64      `json:"granted_count"`
	RetryCount     int        `json:"retry_count"`
	MaxRetries     int        `json:"max_retries"`
	LockedBy       *string    `json:"locked_by,omitempty"`
	LockedUntil    *time.Time `json:"locked_until,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type RewardCampaignListFilter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type RewardGrantListFilter struct {
	Status string
	Search string
	Limit  int
	Offset int
}

type CreateRewardCampaignInput struct {
	Campaign RewardCampaign
	ActorID  *int64
}

type UpdateRewardCampaignInput struct {
	Campaign RewardCampaign
	ActorID  *int64
}

func (c *RewardCampaign) NormalizeAndValidate() error {
	c.Name = strings.TrimSpace(c.Name)
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	c.CampaignKey = strings.TrimSpace(c.CampaignKey)
	c.Timezone = strings.TrimSpace(c.Timezone)
	if c.Name == "" || len(c.Name) > 120 {
		return fmt.Errorf("campaign name is required and must not exceed 120 characters")
	}
	if c.Title == "" || len(c.Title) > 200 {
		return fmt.Errorf("campaign title is required and must not exceed 200 characters")
	}
	if c.IssuanceMode != RewardIssuanceModeOnAccess && c.IssuanceMode != RewardIssuanceModeScheduledBatch {
		return fmt.Errorf("invalid issuance mode")
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid IANA timezone: %w", err)
	}
	if c.EndsAt.IsZero() || c.StartsAt.IsZero() || !c.StartsAt.Before(c.EndsAt) {
		return fmt.Errorf("campaign start time must be before end time")
	}
	if c.Priority < 0 || c.Priority > 10000 {
		return fmt.Errorf("priority must be between 0 and 10000")
	}
	if c.WinProbability < 0 || c.WinProbability > 1 || math.IsNaN(c.WinProbability) {
		return fmt.Errorf("win probability must be between 0 and 1")
	}
	if c.PerUserLimit <= 0 || c.PerUserLimit > 100 {
		return fmt.Errorf("per-user limit must be between 1 and 100")
	}
	if c.EvaluationIntervalMinutes < 0 || c.ClaimCooldownMinutes < 0 {
		return fmt.Errorf("evaluation interval and cooldown cannot be negative")
	}
	if c.TotalBudget < 0 || math.IsNaN(c.TotalBudget) || math.IsInf(c.TotalBudget, 0) {
		return fmt.Errorf("total budget must be a finite non-negative amount")
	}
	if c.TotalBudget+1e-8 < c.ReservedBudget+c.SpentBudget {
		return fmt.Errorf("total budget cannot be lower than reserved plus spent budget")
	}
	if c.ControlGroupPercent < 0 || c.ControlGroupPercent > 100 {
		return fmt.Errorf("control group percent must be between 0 and 100")
	}
	if err := c.Config.NormalizeAndValidate(); err != nil {
		return err
	}
	return nil
}

func (c *RewardCampaignConfig) NormalizeAndValidate() error {
	c.Title = strings.TrimSpace(c.Title)
	if c.Priority < 0 || c.Priority > 10000 {
		return fmt.Errorf("priority must be between 0 and 10000")
	}
	if c.WinProbability < 0 || c.WinProbability > 1 || math.IsNaN(c.WinProbability) {
		return fmt.Errorf("win probability must be between 0 and 1")
	}
	if c.PerUserLimit <= 0 || c.PerUserLimit > 100 {
		return fmt.Errorf("per-user limit must be between 1 and 100")
	}
	if c.EvaluationIntervalMinutes < 0 || c.ClaimCooldownMinutes < 0 {
		return fmt.Errorf("evaluation interval and cooldown cannot be negative")
	}
	if c.ControlGroupPercent < 0 || c.ControlGroupPercent > 100 {
		return fmt.Errorf("control group percent must be between 0 and 100")
	}
	if len(c.AmountTiers) == 0 || len(c.AmountTiers) > 50 {
		return fmt.Errorf("at least one and at most 50 amount tiers are required")
	}
	seenAmounts := make(map[int64]struct{}, len(c.AmountTiers))
	for i := range c.AmountTiers {
		tier := &c.AmountTiers[i]
		if tier.Amount <= 0 || tier.Amount > 1_000_000 || math.IsNaN(tier.Amount) || math.IsInf(tier.Amount, 0) {
			return fmt.Errorf("amount tier %d has an invalid amount", i+1)
		}
		if tier.Weight <= 0 || tier.Weight > 1_000_000 {
			return fmt.Errorf("amount tier %d has an invalid weight", i+1)
		}
		minor := int64(math.Round(tier.Amount * 1e8))
		if _, exists := seenAmounts[minor]; exists {
			return fmt.Errorf("amount tiers must be unique")
		}
		seenAmounts[minor] = struct{}{}
		tier.Amount = float64(minor) / 1e8
	}
	sort.Slice(c.AmountTiers, func(i, j int) bool { return c.AmountTiers[i].Amount < c.AmountTiers[j].Amount })
	if err := c.Audience.Validate(); err != nil {
		return err
	}
	seenSkins := make(map[int64]struct{}, len(c.SkinWeights))
	for i, skin := range c.SkinWeights {
		if skin.SkinID <= 0 || skin.Weight <= 0 || skin.Weight > 1_000_000 {
			return fmt.Errorf("skin weight %d is invalid", i+1)
		}
		if _, exists := seenSkins[skin.SkinID]; exists {
			return fmt.Errorf("skin IDs must be unique")
		}
		seenSkins[skin.SkinID] = struct{}{}
	}
	c.Copy.Title = strings.TrimSpace(c.Copy.Title)
	if c.Title == "" {
		c.Title = c.Copy.Title
	}
	c.Copy.Prompt = strings.TrimSpace(c.Copy.Prompt)
	c.Copy.CoverText = strings.TrimSpace(c.Copy.CoverText)
	if c.Copy.Title == "" {
		return fmt.Errorf("reward copy title is required")
	}
	if len(c.Copy.Title) > 200 || len(c.Copy.Prompt) > 500 || len(c.Copy.CoverText) > 100 {
		return fmt.Errorf("reward copy exceeds the maximum length")
	}
	for locale, copy := range c.CopyI18n {
		locale = strings.TrimSpace(locale)
		if locale == "" || len(locale) > 16 || strings.TrimSpace(copy.Title) == "" {
			return fmt.Errorf("localized reward copy is invalid")
		}
		if len(copy.Title) > 200 || len(copy.Prompt) > 500 || len(copy.CoverText) > 100 {
			return fmt.Errorf("localized reward copy exceeds the maximum length")
		}
	}
	return nil
}

var rewardAudienceFields = map[string]struct{}{
	"registered_at": {}, "signup_source": {}, "last_active_at": {}, "balance": {},
	"subscription_group_id": {}, "user_id": {}, "requests_7d": {}, "requests_30d": {},
	"actual_cost_7d": {}, "actual_cost_30d": {}, "last_api_used_at": {},
	"recharge_30d": {}, "total_recharged": {},
}

var rewardAudienceOperators = map[string]struct{}{
	"eq": {}, "neq": {}, "gt": {}, "gte": {}, "lt": {}, "lte": {},
	"in": {}, "not_in": {}, "between": {}, "before": {}, "after": {},
}

func (a RewardAudience) Validate() error {
	if len(a.AnyOf) > 20 {
		return fmt.Errorf("audience supports at most 20 OR groups")
	}
	for groupIndex, group := range a.AnyOf {
		if len(group.AllOf) == 0 || len(group.AllOf) > 20 {
			return fmt.Errorf("audience group %d must contain 1 to 20 rules", groupIndex+1)
		}
		for ruleIndex, rule := range group.AllOf {
			if _, ok := rewardAudienceFields[rule.Field]; !ok {
				return fmt.Errorf("audience rule %d.%d has an unsupported field", groupIndex+1, ruleIndex+1)
			}
			if _, ok := rewardAudienceOperators[rule.Operator]; !ok {
				return fmt.Errorf("audience rule %d.%d has an unsupported operator", groupIndex+1, ruleIndex+1)
			}
			if !rewardAudienceOperatorAllowed(rule.Field, rule.Operator) {
				return fmt.Errorf("audience rule %d.%d uses an operator that is invalid for its field", groupIndex+1, ruleIndex+1)
			}
			if rule.Value == nil {
				return fmt.Errorf("audience rule %d.%d requires a value", groupIndex+1, ruleIndex+1)
			}
			if rule.Operator == "between" || rule.Operator == "in" || rule.Operator == "not_in" {
				var values []any
				raw, _ := json.Marshal(rule.Value)
				if json.Unmarshal(raw, &values) != nil || len(values) == 0 || (rule.Operator == "between" && len(values) != 2) {
					return fmt.Errorf("audience rule %d.%d requires a non-empty list value", groupIndex+1, ruleIndex+1)
				}
			}
			if err := validateRewardAudienceRuleValue(rule); err != nil {
				return fmt.Errorf("audience rule %d.%d %w", groupIndex+1, ruleIndex+1, err)
			}
		}
	}
	return nil
}

func rewardAudienceOperatorAllowed(field, operator string) bool {
	switch field {
	case "registered_at", "last_active_at", "last_api_used_at":
		return operator == "eq" || operator == "neq" || operator == "before" || operator == "after" ||
			operator == "gt" || operator == "gte" || operator == "lt" || operator == "lte" || operator == "between"
	case "signup_source", "subscription_group_id", "user_id":
		return operator == "eq" || operator == "neq" || operator == "in" || operator == "not_in"
	default:
		return operator == "eq" || operator == "neq" || operator == "gt" || operator == "gte" ||
			operator == "lt" || operator == "lte" || operator == "between"
	}
}

func validateRewardAudienceRuleValue(rule RewardAudienceRule) error {
	values := []any{rule.Value}
	if rule.Operator == "between" || rule.Operator == "in" || rule.Operator == "not_in" {
		values = rewardSlice(rule.Value)
	}
	for _, value := range values {
		switch rule.Field {
		case "registered_at", "last_active_at", "last_api_used_at":
			if relative, ok := value.(map[string]any); ok {
				days, hasDays := rewardFloat(relative["relative_days"])
				hours, hasHours := rewardFloat(relative["relative_hours"])
				if (hasDays == hasHours) || (hasDays && (math.IsNaN(days) || math.IsInf(days, 0))) ||
					(hasHours && (math.IsNaN(hours) || math.IsInf(hours, 0))) {
					return fmt.Errorf("requires one finite relative_days or relative_hours value")
				}
				continue
			}
			if _, ok := rewardTime(value); !ok {
				return fmt.Errorf("requires an RFC3339 time")
			}
		case "signup_source":
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("requires a non-empty string")
			}
		case "subscription_group_id", "user_id":
			number, ok := rewardFloat(value)
			if !ok || number <= 0 || math.Trunc(number) != number {
				return fmt.Errorf("requires a positive integer")
			}
		default:
			number, ok := rewardFloat(value)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
				return fmt.Errorf("requires a finite number")
			}
			if (rule.Field == "requests_7d" || rule.Field == "requests_30d") && (number < 0 || math.Trunc(number) != number) {
				return fmt.Errorf("requires a non-negative integer")
			}
		}
	}
	return nil
}

func ExpectedRewardAmount(tiers []RewardAmountTier) float64 {
	var weighted float64
	var totalWeight int64
	for _, tier := range tiers {
		if tier.Amount <= 0 || tier.Weight <= 0 {
			continue
		}
		weighted += tier.Amount * float64(tier.Weight)
		totalWeight += int64(tier.Weight)
	}
	if totalWeight == 0 {
		return 0
	}
	return weighted / float64(totalWeight)
}

func MaximumRewardAmount(tiers []RewardAmountTier) float64 {
	var maximum float64
	for _, tier := range tiers {
		if tier.Amount > maximum {
			maximum = tier.Amount
		}
	}
	return maximum
}

func IsRewardCampaignStatus(status string) bool {
	switch status {
	case RewardCampaignStatusDraft, RewardCampaignStatusScheduled, RewardCampaignStatusActive,
		RewardCampaignStatusPaused, RewardCampaignStatusEnded, RewardCampaignStatusArchived:
		return true
	default:
		return false
	}
}
