package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

type RewardHandler struct {
	service *service.RewardService
}

func NewRewardHandler(rewardService *service.RewardService) *RewardHandler {
	return &RewardHandler{service: rewardService}
}

type rewardCampaignCopyRequest struct {
	Title          string `json:"title"`
	Hint           string `json:"hint"`
	ScratchPrompt  string `json:"scratch_prompt"`
	ClaimCTA       string `json:"claim_cta"`
	SuccessMessage string `json:"success_message"`
}

type rewardCampaignCopySetRequest struct {
	ZH rewardCampaignCopyRequest `json:"zh"`
	EN rewardCampaignCopyRequest `json:"en"`
}

type rewardCampaignDraftRequest struct {
	Name                      string                       `json:"name" binding:"required"`
	Description               string                       `json:"description"`
	IssuanceMode              string                       `json:"issuance_mode" binding:"required"`
	Timezone                  string                       `json:"timezone" binding:"required"`
	StartsAt                  string                       `json:"starts_at" binding:"required"`
	EndsAt                    string                       `json:"ends_at" binding:"required"`
	Priority                  int                          `json:"priority"`
	WinProbability            float64                      `json:"win_probability"`
	MaxGrantsPerUser          int                          `json:"max_grants_per_user"`
	EvaluationIntervalMinutes int                          `json:"evaluation_interval_minutes"`
	CooldownDays              int                          `json:"cooldown_days"`
	ControlGroupPercent       float64                      `json:"control_group_percent"`
	TotalBudget               float64                      `json:"total_budget"`
	AmountTiers               []service.RewardAmountTier   `json:"amount_tiers"`
	Audience                  service.RewardAudience       `json:"audience"`
	SkinAllocations           []service.RewardSkinWeight   `json:"skin_allocations"`
	Copy                      rewardCampaignCopySetRequest `json:"copy"`
}

type rewardCampaignResponse struct {
	ID                        int64                        `json:"id"`
	Name                      string                       `json:"name"`
	Description               string                       `json:"description"`
	Status                    string                       `json:"status"`
	IssuanceMode              string                       `json:"issuance_mode"`
	Timezone                  string                       `json:"timezone"`
	StartsAt                  string                       `json:"starts_at"`
	EndsAt                    string                       `json:"ends_at"`
	Priority                  int                          `json:"priority"`
	WinProbability            float64                      `json:"win_probability"`
	MaxGrantsPerUser          int                          `json:"max_grants_per_user"`
	EvaluationIntervalMinutes int                          `json:"evaluation_interval_minutes"`
	CooldownDays              float64                      `json:"cooldown_days"`
	ControlGroupPercent       float64                      `json:"control_group_percent"`
	TotalBudget               float64                      `json:"total_budget"`
	ReservedBudget            float64                      `json:"reserved_budget"`
	SpentBudget               float64                      `json:"spent_budget"`
	ReleasedBudget            float64                      `json:"released_budget"`
	CurrentVersion            int                          `json:"current_version"`
	AmountTiers               []service.RewardAmountTier   `json:"amount_tiers"`
	Audience                  service.RewardAudience       `json:"audience"`
	SkinAllocations           []service.RewardSkinWeight   `json:"skin_allocations"`
	Copy                      rewardCampaignCopySetRequest `json:"copy"`
	CreatedAt                 time.Time                    `json:"created_at"`
	UpdatedAt                 time.Time                    `json:"updated_at"`
}

func (h *RewardHandler) ListCampaigns(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	campaigns, total, err := h.service.ListCampaigns(c.Request.Context(), service.RewardCampaignListFilter{
		Status: strings.TrimSpace(c.Query("status")), Search: strings.TrimSpace(c.Query("search")),
		Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]rewardCampaignResponse, 0, len(campaigns))
	for i := range campaigns {
		items = append(items, rewardCampaignToResponse(campaigns[i]))
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *RewardHandler) GetCampaign(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	campaign, err := h.service.GetCampaign(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rewardCampaignToResponse(*campaign))
}

func (h *RewardHandler) CreateCampaign(c *gin.Context) {
	var req rewardCampaignDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid reward campaign: "+err.Error())
		return
	}
	campaign, err := rewardCampaignFromRequest(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	created, err := h.service.CreateCampaign(c.Request.Context(), service.CreateRewardCampaignInput{Campaign: campaign, ActorID: rewardActorID(c)})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, rewardCampaignToResponse(*created))
}

func (h *RewardHandler) UpdateCampaign(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	var req rewardCampaignDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid reward campaign: "+err.Error())
		return
	}
	campaign, err := rewardCampaignFromRequest(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	updated, err := h.service.UpdateCampaign(c.Request.Context(), id, service.UpdateRewardCampaignInput{Campaign: campaign, ActorID: rewardActorID(c)})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rewardCampaignToResponse(*updated))
}

func (h *RewardHandler) CloneCampaign(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	created, err := h.service.CloneCampaign(c.Request.Context(), id, rewardActorID(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, rewardCampaignToResponse(*created))
}

func (h *RewardHandler) Estimate(c *gin.Context) {
	var req rewardCampaignDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid reward campaign: "+err.Error())
		return
	}
	campaign, err := rewardCampaignFromRequest(req)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	estimate, err := h.service.Estimate(c.Request.Context(), campaign)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	controlUsers := int64(math.Round(float64(estimate.EligibleUsers) * campaign.ControlGroupPercent / 100))
	response.Success(c, gin.H{
		"matched_users": estimate.EligibleUsers, "expected_winners": estimate.ExpectedWinners,
		"expected_cost": estimate.ExpectedCost, "maximum_cost": estimate.MaximumCost,
		"control_group_users": controlUsers, "data_updated_at": estimate.DataUpdatedAt,
		"warnings": []string{},
	})
}

func (h *RewardHandler) CampaignAction(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	action := c.Param("action")
	campaign, err := h.service.TransitionCampaign(c.Request.Context(), id, action, rewardActorID(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, rewardCampaignToResponse(*campaign))
}

func (h *RewardHandler) Stats(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	stats, err := h.service.CampaignStats(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	distribution := make([]gin.H, 0, len(stats.Amounts))
	for amount, count := range stats.Amounts {
		value, _ := strconv.ParseFloat(amount, 64)
		distribution = append(distribution, gin.H{"amount": value, "count": count, "total": value * float64(count)})
	}
	response.Success(c, gin.H{
		"evaluated": stats.Evaluated, "granted": stats.Won, "viewed": stats.Viewed,
		"scratched": stats.Claimed, "claimed": stats.Claimed, "expired": stats.Expired,
		"pending": stats.Pending, "control_group": stats.ControlGroup,
		"total_budget": stats.Budget.Total, "reserved_budget": stats.Budget.Reserved,
		"spent_budget": stats.Budget.Spent, "released_budget": stats.Budget.Released,
		"amount_distribution": distribution, "updated_at": time.Now().UTC(),
	})
}

func (h *RewardHandler) Grants(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	grants, total, err := h.service.CampaignGrants(c.Request.Context(), id, service.RewardGrantListFilter{
		Status: strings.TrimSpace(c.Query("status")),
		Search: strings.TrimSpace(c.Query("search")),
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	type adminGrantResponse struct {
		ID              string     `json:"id"`
		GrantID         int64      `json:"grant_id"`
		UserID          int64      `json:"user_id"`
		UserEmail       string     `json:"user_email"`
		CampaignVersion int        `json:"campaign_version"`
		Source          string     `json:"source"`
		Status          string     `json:"status"`
		Amount          float64    `json:"amount"`
		ExpiresAt       *time.Time `json:"expires_at"`
		ViewedAt        *time.Time `json:"viewed_at"`
		ClaimedAt       *time.Time `json:"claimed_at"`
		BalanceAfter    *float64   `json:"balance_after"`
		CreatedAt       time.Time  `json:"created_at"`
	}
	items := make([]adminGrantResponse, 0, len(grants))
	for i := range grants {
		grant := &grants[i]
		var expiresAt *time.Time
		if !grant.ExpiresAt.IsZero() {
			value := grant.ExpiresAt
			expiresAt = &value
		}
		items = append(items, adminGrantResponse{
			ID: strconv.FormatInt(grant.ID, 10), GrantID: grant.ID, UserID: grant.UserID, UserEmail: grant.UserEmail,
			CampaignVersion: grant.Version, Source: grant.Source, Status: grant.Status,
			Amount: grant.Amount, ExpiresAt: expiresAt, ViewedAt: grant.ViewedAt,
			ClaimedAt: grant.ClaimedAt, BalanceAfter: grant.BalanceAfter, CreatedAt: grant.CreatedAt,
		})
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *RewardHandler) Jobs(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	jobs, total, err := h.service.CampaignJobs(c.Request.Context(), id, pageSize, (page-1)*pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, jobs, total, page, pageSize)
}

func (h *RewardHandler) CreateJob(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	if _, err := h.service.GetCampaign(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if _, err := h.service.EnqueueScheduled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	jobs, _, err := h.service.CampaignJobs(c.Request.Context(), id, 1, 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if len(jobs) == 0 {
		response.BadRequest(c, "Campaign is not ready for batch issuance")
		return
	}
	response.Success(c, jobs[0])
}

func (h *RewardHandler) ListSkins(c *gin.Context) {
	includeArchived := c.Query("status") == "archived" || c.Query("include_archived") == "true"
	skins, err := h.service.ListSkins(c.Request.Context(), includeArchived)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]service.RewardSkin, len(skins))
	copy(items, skins)
	for i := range items {
		items[i].Status = rewardSkinStatusForClient(items[i].Status)
	}
	response.Paginated(c, items, int64(len(items)), 1, max(len(items), 1))
}

func (h *RewardHandler) UploadSkin(c *gin.Context) {
	header, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "Skin image is required")
		return
	}
	file, err := header.Open()
	if err != nil {
		response.BadRequest(c, "Unable to read skin image")
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(content) == 0 || len(content) > 1024*1024 {
		response.BadRequest(c, "Skin image must not exceed 1 MB")
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		response.BadRequest(c, "Skin image is invalid")
		return
	}
	mimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if mimeType == "" {
		response.BadRequest(c, "Skin image must be PNG, JPEG, or WebP")
		return
	}
	digest := sha256.Sum256(content)
	skin, err := h.service.CreateSkin(c.Request.Context(), service.RewardSkin{
		Name: strings.TrimSpace(c.PostForm("name")), Description: strings.TrimSpace(c.PostForm("description")),
		AltText: strings.TrimSpace(c.PostForm("alt_text")), MIMEType: mimeType,
		Width: config.Width, Height: config.Height, SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:]),
	}, content, rewardActorID(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	skin.Status = rewardSkinStatusForClient(skin.Status)
	response.Created(c, skin)
}

type rewardSkinUpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	AltText     *string `json:"alt_text"`
	Status      *string `json:"status"`
}

func (h *RewardHandler) UpdateSkin(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	var req rewardSkinUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid reward skin")
		return
	}
	status := rewardSkinStatusFromClient(req.Status)
	skin, err := h.service.UpdateSkin(c.Request.Context(), id, req.Name, req.Description, req.AltText, status, rewardActorID(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	skin.Status = rewardSkinStatusForClient(skin.Status)
	response.Success(c, skin)
}

func (h *RewardHandler) ArchiveSkin(c *gin.Context) {
	id, ok := rewardPathID(c, "id")
	if !ok {
		return
	}
	status := service.RewardSkinStatusArchived
	skin, err := h.service.UpdateSkin(c.Request.Context(), id, nil, nil, nil, &status, rewardActorID(c))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	skin.Status = rewardSkinStatusForClient(skin.Status)
	response.Success(c, skin)
}

func rewardCampaignFromRequest(req rewardCampaignDraftRequest) (service.RewardCampaign, error) {
	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		return service.RewardCampaign{}, err
	}
	endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		return service.RewardCampaign{}, err
	}
	audience := normalizeRewardAudienceAliases(req.Audience)
	zh := rewardCopyFromRequest(req.Copy.ZH)
	en := rewardCopyFromRequest(req.Copy.EN)
	primary := zh
	if primary.Title == "" {
		primary = en
	}
	config := service.RewardCampaignConfig{
		Title: primary.Title, Priority: req.Priority, WinProbability: req.WinProbability,
		PerUserLimit: req.MaxGrantsPerUser, EvaluationIntervalMinutes: req.EvaluationIntervalMinutes,
		ClaimCooldownMinutes: req.CooldownDays * 24 * 60, ControlGroupPercent: req.ControlGroupPercent,
		AmountTiers: req.AmountTiers, Audience: audience, Copy: primary,
		CopyI18n: map[string]service.RewardCopy{"zh": zh, "en": en}, SkinWeights: req.SkinAllocations,
	}
	return service.RewardCampaign{
		Name: req.Name, Title: primary.Title, Description: req.Description,
		Status: service.RewardCampaignStatusDraft, IssuanceMode: req.IssuanceMode, Timezone: req.Timezone,
		StartsAt: startsAt, EndsAt: endsAt, Priority: req.Priority, WinProbability: req.WinProbability,
		PerUserLimit: req.MaxGrantsPerUser, EvaluationIntervalMinutes: req.EvaluationIntervalMinutes,
		ClaimCooldownMinutes: req.CooldownDays * 24 * 60, TotalBudget: req.TotalBudget,
		ControlGroupPercent: req.ControlGroupPercent, Config: config,
	}, nil
}

func rewardCampaignToResponse(campaign service.RewardCampaign) rewardCampaignResponse {
	zh := rewardCopyToRequest(campaign.Config.CopyI18n["zh"])
	en := rewardCopyToRequest(campaign.Config.CopyI18n["en"])
	if zh.Title == "" {
		zh = rewardCopyToRequest(campaign.Config.Copy)
	}
	if en.Title == "" {
		en = rewardCopyToRequest(campaign.Config.Copy)
	}
	return rewardCampaignResponse{
		ID: campaign.ID, Name: campaign.Name, Description: campaign.Description, Status: campaign.Status,
		IssuanceMode: campaign.IssuanceMode, Timezone: campaign.Timezone,
		StartsAt: campaign.StartsAt.Format(time.RFC3339), EndsAt: campaign.EndsAt.Format(time.RFC3339),
		Priority: campaign.Priority, WinProbability: campaign.WinProbability,
		MaxGrantsPerUser: campaign.PerUserLimit, EvaluationIntervalMinutes: campaign.EvaluationIntervalMinutes,
		CooldownDays:        float64(campaign.ClaimCooldownMinutes) / 1440,
		ControlGroupPercent: campaign.ControlGroupPercent, TotalBudget: campaign.TotalBudget,
		ReservedBudget: campaign.ReservedBudget, SpentBudget: campaign.SpentBudget, ReleasedBudget: campaign.ReleasedBudget,
		CurrentVersion: campaign.CurrentVersion, AmountTiers: campaign.Config.AmountTiers,
		Audience: denormalizeRewardAudienceAliases(campaign.Config.Audience), SkinAllocations: campaign.Config.SkinWeights,
		Copy: rewardCampaignCopySetRequest{ZH: zh, EN: en}, CreatedAt: campaign.CreatedAt, UpdatedAt: campaign.UpdatedAt,
	}
}

func rewardCopyFromRequest(copy rewardCampaignCopyRequest) service.RewardCopy {
	return service.RewardCopy{Title: copy.Title, Prompt: copy.Hint, CoverText: copy.ScratchPrompt, ContinueText: copy.ClaimCTA, CreditedText: copy.SuccessMessage}
}

func rewardCopyToRequest(copy service.RewardCopy) rewardCampaignCopyRequest {
	return rewardCampaignCopyRequest{Title: copy.Title, Hint: copy.Prompt, ScratchPrompt: copy.CoverText, ClaimCTA: copy.ContinueText, SuccessMessage: copy.CreditedText}
}

var rewardAudienceFieldAliases = map[string]string{
	"registration_source": "signup_source", "request_count_7d": "requests_7d",
	"request_count_30d": "requests_30d", "recharge_amount_30d": "recharge_30d",
	"recharge_amount_total": "total_recharged",
}

func normalizeRewardAudienceAliases(audience service.RewardAudience) service.RewardAudience {
	for groupIndex := range audience.AnyOf {
		for ruleIndex := range audience.AnyOf[groupIndex].AllOf {
			rule := &audience.AnyOf[groupIndex].AllOf[ruleIndex]
			if alias, ok := rewardAudienceFieldAliases[rule.Field]; ok {
				rule.Field = alias
			}
			if rule.Operator == "within_days" {
				rule.Operator = "after"
				days, _ := strconv.ParseFloat(strings.TrimSpace(toString(rule.Value)), 64)
				rule.Value = map[string]any{"relative_days": -days}
			}
		}
	}
	return audience
}

func denormalizeRewardAudienceAliases(audience service.RewardAudience) service.RewardAudience {
	reverse := make(map[string]string, len(rewardAudienceFieldAliases))
	for from, to := range rewardAudienceFieldAliases {
		reverse[to] = from
	}
	for groupIndex := range audience.AnyOf {
		for ruleIndex := range audience.AnyOf[groupIndex].AllOf {
			rule := &audience.AnyOf[groupIndex].AllOf[ruleIndex]
			if alias, ok := reverse[rule.Field]; ok {
				rule.Field = alias
			}
			if relative, ok := rule.Value.(map[string]any); ok && rule.Operator == "after" {
				if days, exists := relative["relative_days"]; exists {
					rule.Operator = "within_days"
					parsed, _ := strconv.ParseFloat(toString(days), 64)
					rule.Value = math.Abs(parsed)
				}
			}
		}
	}
	return audience
}

func toString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func rewardPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid reward resource id")
		return 0, false
	}
	return id, true
}

func rewardActorID(c *gin.Context) *int64 {
	id := getAdminIDFromContext(c)
	if id <= 0 {
		return nil
	}
	return &id
}

func rewardSkinStatusForClient(status string) string {
	switch status {
	case service.RewardSkinStatusActive:
		return "enabled"
	case service.RewardSkinStatusInactive:
		return "disabled"
	default:
		return status
	}
}

func rewardSkinStatusFromClient(status *string) *string {
	if status == nil {
		return nil
	}
	value := *status
	switch value {
	case "enabled":
		value = service.RewardSkinStatusActive
	case "disabled":
		value = service.RewardSkinStatusInactive
	}
	return &value
}
