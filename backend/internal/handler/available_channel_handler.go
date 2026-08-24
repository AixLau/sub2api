package handler

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// AvailableChannelHandler 处理用户侧「可用渠道」查询。
//
// 用户侧接口委托 ChannelService.ListAvailable，并在返回前做四层过滤：
//  1. 行过滤：只保留状态为 Active 且与当前用户可访问分组有交集的渠道；
//  2. 分组过滤：渠道的 Groups 只保留用户可访问的那些；
//  3. 平台过滤：普通分组只保留自身平台模型；Composite 分组按渠道已配置的具体模型平台
//     展开。这样既防止普通分组跨平台泄漏，也让 Composite 正确展示其多平台能力；
//  4. 字段白名单：仅返回用户需要的字段（省略 BillingModelSource / RestrictModels
//     / 内部 ID / Status 等管理字段）。
type AvailableChannelHandler struct {
	channelService *service.ChannelService
	apiKeyService  *service.APIKeyService
	settingService *service.SettingService
	modelCatalog   systemModelCatalogSource
	modelPricing   systemModelPricingSource
}

type systemModelCatalogSource interface {
	ListSystemAvailableModelSets(ctx context.Context) ([]service.SystemAvailableModelSet, error)
}

type systemModelPricingSource interface {
	GetModelPricing(modelName string) *service.LiteLLMModelPricing
}

// NewAvailableChannelHandler 创建用户侧可用渠道 handler。
func NewAvailableChannelHandler(
	channelService *service.ChannelService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
	gatewayService *service.GatewayService,
	pricingService *service.PricingService,
) *AvailableChannelHandler {
	return &AvailableChannelHandler{
		channelService: channelService,
		apiKeyService:  apiKeyService,
		settingService: settingService,
		modelCatalog:   gatewayService,
		modelPricing:   pricingService,
	}
}

// featureEnabled 返回 available-channels 开关是否启用。默认关闭（opt-in）。
func (h *AvailableChannelHandler) featureEnabled(c *gin.Context) bool {
	if h.settingService == nil {
		return false
	}
	return h.settingService.GetAvailableChannelsRuntime(c.Request.Context()).Enabled
}

// userAvailableGroup 用户可见的分组概要（白名单字段）。
//
// 前端据此区分专属 vs 公开分组（IsExclusive）、订阅 vs 标准分组（SubscriptionType，
// 订阅视觉加深），并展示默认倍率与高峰倍率规则；用户专属倍率前端走
// /groups/rates，和 API 密钥页面保持一致。
type userAvailableGroup struct {
	ID                 int64   `json:"id"`
	Name               string  `json:"name"`
	Platform           string  `json:"platform"`
	SubscriptionType   string  `json:"subscription_type"`
	RateMultiplier     float64 `json:"rate_multiplier"`
	PeakRateEnabled    bool    `json:"peak_rate_enabled"`
	PeakStart          string  `json:"peak_start"`
	PeakEnd            string  `json:"peak_end"`
	PeakRateMultiplier float64 `json:"peak_rate_multiplier"`
	IsExclusive        bool    `json:"is_exclusive"`
}

// userSupportedModelPricing 用户可见的定价字段白名单。
type userSupportedModelPricing struct {
	BillingMode      string                   `json:"billing_mode"`
	InputPrice       *float64                 `json:"input_price"`
	OutputPrice      *float64                 `json:"output_price"`
	CacheWritePrice  *float64                 `json:"cache_write_price"`
	CacheReadPrice   *float64                 `json:"cache_read_price"`
	ImageInputPrice  *float64                 `json:"image_input_price"`
	ImageOutputPrice *float64                 `json:"image_output_price"`
	PerRequestPrice  *float64                 `json:"per_request_price"`
	Intervals        []userPricingIntervalDTO `json:"intervals"`
}

// userPricingIntervalDTO 定价区间白名单（去掉内部 ID、SortOrder 等前端不渲染的字段）。
type userPricingIntervalDTO struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
}

// userSupportedModel 用户可见的支持模型条目。
type userSupportedModel struct {
	Name     string                     `json:"name"`
	Platform string                     `json:"platform"`
	Pricing  *userSupportedModelPricing `json:"pricing"`
}

// userChannelPlatformSection 单渠道内某个平台的子视图：用户可见的分组 + 该平台
// 支持的模型。按 platform 聚合后让前端可以把渠道名作为 row-group 一次渲染，
// 后面的平台行按 sections 顺序铺开。
type userChannelPlatformSection struct {
	Platform        string               `json:"platform"`
	Groups          []userAvailableGroup `json:"groups"`
	SupportedModels []userSupportedModel `json:"supported_models"`
}

// userAvailableChannel 用户可见的渠道条目（白名单字段）。
//
// 每个渠道聚合为一条记录，内嵌 platforms 子数组：每个 section 对应一个平台，
// 包含该平台的 groups 和 supported_models。
type userAvailableChannel struct {
	Name        string                       `json:"name"`
	Description string                       `json:"description"`
	Platforms   []userChannelPlatformSection `json:"platforms"`
}

// publicModelCatalogResponse 是注册前模型广场使用的最小公开响应。
// 不暴露渠道、分组、倍率、内部 ID 或调度信息。
type publicModelCatalogResponse struct {
	Models []userSupportedModel `json:"models"`
}

// ListPublic 返回无需登录即可浏览的公开模型目录。
// GET /api/v1/models/public
func (h *AvailableChannelHandler) ListPublic(c *gin.Context) {
	modelSets, err := h.modelCatalog.ListSystemAvailableModelSets(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 首页目录仍以可调度账号决定模型可见性，但展示价格应与实际渠道配置一致。
	// 自定义价格读取失败时保留系统预设，避免可选的展示增强拖垮公开目录。
	var customPricing map[publicModelKey]*service.ChannelModelPricing
	if h.channelService != nil {
		channels, listErr := h.channelService.ListAvailable(c.Request.Context())
		if listErr != nil {
			slog.Warn("public_model_custom_pricing_failed", "error", listErr)
		} else {
			customPricing = collectPublicModelPricing(channels)
		}
	}

	c.Header("Cache-Control", "public, max-age=60")
	response.Success(c, publicModelCatalogResponse{
		Models: buildSystemPublicModelCatalog(modelSets, h.modelPricing, customPricing),
	})
}

type publicModelKey struct {
	platform string
	name     string
}

// collectPublicModelPricing collects deterministic pricing candidates from
// active channels attached to at least one public group. ListAvailable sorts
// channels by name, so the first candidate wins when several channels price the
// same model.
func collectPublicModelPricing(channels []service.AvailableChannel) map[publicModelKey]*service.ChannelModelPricing {
	result := make(map[publicModelKey]*service.ChannelModelPricing)
	for i := range channels {
		channel := &channels[i]
		if channel.Status != service.StatusActive {
			continue
		}
		for j := range channel.SupportedModels {
			model := &channel.SupportedModels[j]
			if model.Pricing == nil || !channelHasPublicGroupForPlatform(channel.Groups, model.Platform) {
				continue
			}
			key := newPublicModelKey(model.Platform, model.Name)
			if _, exists := result[key]; exists {
				continue
			}
			clone := model.Pricing.Clone()
			result[key] = &clone
		}
	}
	return result
}

func channelHasPublicGroupForPlatform(groups []service.AvailableGroupRef, platform string) bool {
	for i := range groups {
		group := &groups[i]
		if group.IsExclusive {
			continue
		}
		if group.Platform == platform || group.Platform == service.PlatformComposite {
			return true
		}
	}
	return false
}

func newPublicModelKey(platform, name string) publicModelKey {
	return publicModelKey{
		platform: strings.ToLower(strings.TrimSpace(platform)),
		name:     strings.ToLower(strings.TrimSpace(name)),
	}
}

// buildSystemPublicModelCatalog uses the same availability semantics as
// GET /v1/models. Channel pricing overrides the global PricingService preset;
// missing custom fields continue to use the preset, matching token billing.
func buildSystemPublicModelCatalog(
	modelSets []service.SystemAvailableModelSet,
	pricingSource systemModelPricingSource,
	customPricing map[publicModelKey]*service.ChannelModelPricing,
) []userSupportedModel {
	byKey := make(map[string]userSupportedModel)
	for _, set := range modelSets {
		platform := strings.TrimSpace(set.Platform)
		if platform == "" {
			continue
		}
		models := set.Models
		if len(models) == 0 {
			models = defaultModelIDsForPlatform(platform)
		}
		for _, name := range models {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(platform + "\x00" + name)
			if _, exists := byKey[key]; exists {
				continue
			}
			byKey[key] = userSupportedModel{
				Name:     name,
				Platform: platform,
				Pricing: mergePublicModelPricing(
					toSystemModelPricing(pricingSource.GetModelPricing(name)),
					toUserPricing(customPricing[newPublicModelKey(platform, name)]),
				),
			}
		}
	}

	models := make([]userSupportedModel, 0, len(byKey))
	for _, model := range byKey {
		models = append(models, model)
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Platform != models[j].Platform {
			return models[i].Platform < models[j].Platform
		}
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
	return models
}

// mergePublicModelPricing applies custom fields over the system preset. For
// non-token modes the custom configuration is authoritative because token
// preset fields are not meaningful for per-request media billing.
func mergePublicModelPricing(
	systemPricing, customPricing *userSupportedModelPricing,
) *userSupportedModelPricing {
	if customPricing == nil {
		return systemPricing
	}
	if customPricing.BillingMode != "" && customPricing.BillingMode != string(service.BillingModeToken) {
		return customPricing
	}
	if systemPricing == nil {
		return customPricing
	}

	merged := *systemPricing
	merged.BillingMode = string(service.BillingModeToken)
	if customPricing.InputPrice != nil {
		merged.InputPrice = customPricing.InputPrice
	}
	if customPricing.OutputPrice != nil {
		merged.OutputPrice = customPricing.OutputPrice
	}
	if customPricing.CacheWritePrice != nil {
		merged.CacheWritePrice = customPricing.CacheWritePrice
	}
	if customPricing.CacheReadPrice != nil {
		merged.CacheReadPrice = customPricing.CacheReadPrice
	}
	if customPricing.ImageInputPrice != nil {
		merged.ImageInputPrice = customPricing.ImageInputPrice
	}
	if customPricing.ImageOutputPrice != nil {
		merged.ImageOutputPrice = customPricing.ImageOutputPrice
	}
	if customPricing.PerRequestPrice != nil {
		merged.PerRequestPrice = customPricing.PerRequestPrice
	}
	if len(customPricing.Intervals) > 0 {
		merged.Intervals = customPricing.Intervals
	}
	return &merged
}

func toSystemModelPricing(p *service.LiteLLMModelPricing) *userSupportedModelPricing {
	if p == nil {
		return nil
	}
	mode := service.BillingModeToken
	if p.Mode == "image_generation" {
		mode = service.BillingModeImage
	}
	var inputPrice, outputPrice *float64
	if !p.TokenPricingAbsent {
		inputPrice = nonZeroPrice(p.InputCostPerToken)
		outputPrice = nonZeroPrice(p.OutputCostPerToken)
	}
	return &userSupportedModelPricing{
		BillingMode:      string(mode),
		InputPrice:       inputPrice,
		OutputPrice:      outputPrice,
		CacheWritePrice:  systemCacheWritePrice(p),
		CacheReadPrice:   nonZeroPrice(p.CacheReadInputTokenCost),
		ImageInputPrice:  nonZeroPrice(p.InputCostPerImageToken),
		ImageOutputPrice: nonZeroPrice(p.OutputCostPerImageToken),
		PerRequestPrice:  nonZeroPrice(p.OutputCostPerImage),
		Intervals:        []userPricingIntervalDTO{},
	}
}

func systemCacheWritePrice(p *service.LiteLLMModelPricing) *float64 {
	if p.CacheCreationInputTokenCost != 0 {
		return nonZeroPrice(p.CacheCreationInputTokenCost)
	}
	if p.SupportsPromptCaching && p.CacheReadInputTokenCost > 0 {
		zero := 0.0
		return &zero
	}
	return nil
}

func nonZeroPrice(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

// List 列出当前用户可见的「可用渠道」。
// GET /api/v1/channels/available
func (h *AvailableChannelHandler) List(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// Feature 未启用时返回空数组（不暴露渠道信息）。检查放在认证之后，
	// 保持与未开关前的 401 行为一致：未登录先 401，登录后再按开关决定。
	if !h.featureEnabled(c) {
		response.Success(c, []userAvailableChannel{})
		return
	}

	userGroups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	allowedGroupIDs := make(map[int64]struct{}, len(userGroups))
	for i := range userGroups {
		allowedGroupIDs[userGroups[i].ID] = struct{}{}
	}

	channels, err := h.channelService.ListAvailable(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]userAvailableChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != service.StatusActive {
			continue
		}
		visibleGroups := filterUserVisibleGroups(ch.Groups, allowedGroupIDs)
		if len(visibleGroups) == 0 {
			continue
		}
		sections := buildPlatformSections(ch, visibleGroups)
		if len(sections) == 0 {
			continue
		}
		out = append(out, userAvailableChannel{
			Name:        ch.Name,
			Description: ch.Description,
			Platforms:   sections,
		})
	}

	response.Success(c, out)
}

// buildPlatformSections 把一个渠道按 visibleGroups 的平台集合拆成有序的 section 列表：
// 每个 section 对应一个具体平台，只包含该平台的 groups 和 supported_models。
//
// Composite 分组可访问渠道中所有已配置的具体平台，因此会被展开到每个有支持模型的
// 平台 section。普通分组仍严格留在自身平台，避免跨平台模型信息泄漏。Composite 渠道
// 尚未配置任何模型时保留 composite section，以便前端继续展示该分组和“未配置模型”状态。
// 输出按 platform 字母序稳定排序，便于前端等效比较与回归测试。
func buildPlatformSections(
	ch service.AvailableChannel,
	visibleGroups []userAvailableGroup,
) []userChannelPlatformSection {
	groupsByPlatform := make(map[string][]userAvailableGroup, 4)
	compositeGroups := make([]userAvailableGroup, 0, 1)
	for _, g := range visibleGroups {
		if g.Platform == "" {
			continue
		}
		if g.Platform == service.PlatformComposite {
			compositeGroups = append(compositeGroups, g)
			continue
		}
		groupsByPlatform[g.Platform] = append(groupsByPlatform[g.Platform], g)
	}

	if len(compositeGroups) > 0 {
		modelPlatforms := make(map[string]struct{}, len(ch.SupportedModels))
		for i := range ch.SupportedModels {
			if platform := ch.SupportedModels[i].Platform; platform != "" {
				modelPlatforms[platform] = struct{}{}
			}
		}
		if len(modelPlatforms) == 0 {
			groupsByPlatform[service.PlatformComposite] = append(
				groupsByPlatform[service.PlatformComposite],
				compositeGroups...,
			)
		} else {
			for platform := range modelPlatforms {
				groupsByPlatform[platform] = append(groupsByPlatform[platform], compositeGroups...)
			}
		}
	}
	if len(groupsByPlatform) == 0 {
		return nil
	}

	platforms := make([]string, 0, len(groupsByPlatform))
	for p := range groupsByPlatform {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)

	sections := make([]userChannelPlatformSection, 0, len(platforms))
	for _, platform := range platforms {
		platformSet := map[string]struct{}{platform: {}}
		sections = append(sections, userChannelPlatformSection{
			Platform:        platform,
			Groups:          groupsByPlatform[platform],
			SupportedModels: toUserSupportedModels(ch.SupportedModels, platformSet),
		})
	}
	return sections
}

// filterUserVisibleGroups 仅保留用户可访问的分组。
func filterUserVisibleGroups(
	groups []service.AvailableGroupRef,
	allowed map[int64]struct{},
) []userAvailableGroup {
	visible := make([]userAvailableGroup, 0, len(groups))
	for _, g := range groups {
		if _, ok := allowed[g.ID]; !ok {
			continue
		}
		visible = append(visible, userAvailableGroup{
			ID:                 g.ID,
			Name:               g.Name,
			Platform:           g.Platform,
			SubscriptionType:   g.SubscriptionType,
			RateMultiplier:     g.RateMultiplier,
			PeakRateEnabled:    g.PeakRateEnabled,
			PeakStart:          g.PeakStart,
			PeakEnd:            g.PeakEnd,
			PeakRateMultiplier: g.PeakRateMultiplier,
			IsExclusive:        g.IsExclusive,
		})
	}
	return visible
}

// toUserSupportedModels 将 service 层支持模型转换为用户 DTO（字段白名单）。
// 仅保留平台在 allowedPlatforms 中的条目，防止跨平台模型信息泄漏。
// allowedPlatforms 为 nil 时不做平台过滤（保留全部，供测试或明确无过滤场景使用）。
func toUserSupportedModels(
	src []service.SupportedModel,
	allowedPlatforms map[string]struct{},
) []userSupportedModel {
	out := make([]userSupportedModel, 0, len(src))
	for i := range src {
		m := src[i]
		if allowedPlatforms != nil {
			if _, ok := allowedPlatforms[m.Platform]; !ok {
				continue
			}
		}
		out = append(out, userSupportedModel{
			Name:     m.Name,
			Platform: m.Platform,
			Pricing:  toUserPricing(m.Pricing),
		})
	}
	return out
}

// toUserPricingIntervals 将定价区间转换为用户 DTO 白名单形态；nil 入参返回 nil（JSON omitempty 可省略）。
func toUserPricingIntervals(src []service.PricingInterval) []userPricingIntervalDTO {
	if src == nil {
		return nil
	}
	intervals := make([]userPricingIntervalDTO, 0, len(src))
	for _, iv := range src {
		intervals = append(intervals, userPricingIntervalDTO{
			MinTokens:       iv.MinTokens,
			MaxTokens:       iv.MaxTokens,
			TierLabel:       iv.TierLabel,
			InputPrice:      iv.InputPrice,
			OutputPrice:     iv.OutputPrice,
			CacheWritePrice: iv.CacheWritePrice,
			CacheReadPrice:  iv.CacheReadPrice,
			PerRequestPrice: iv.PerRequestPrice,
		})
	}
	return intervals
}

// toUserPricing 将 service 层定价转换为用户 DTO；入参为 nil 时返回 nil。
func toUserPricing(p *service.ChannelModelPricing) *userSupportedModelPricing {
	if p == nil {
		return nil
	}
	intervals := toUserPricingIntervals(p.Intervals)
	if intervals == nil {
		// 用户侧定价的 intervals 固定输出数组（空配置为 []），保持既有契约。
		intervals = []userPricingIntervalDTO{}
	}
	billingMode := string(p.BillingMode)
	if billingMode == "" {
		billingMode = string(service.BillingModeToken)
	}
	return &userSupportedModelPricing{
		BillingMode:      billingMode,
		InputPrice:       p.InputPrice,
		OutputPrice:      p.OutputPrice,
		CacheWritePrice:  p.CacheWritePrice,
		CacheReadPrice:   p.CacheReadPrice,
		ImageInputPrice:  p.ImageInputPrice,
		ImageOutputPrice: p.ImageOutputPrice,
		PerRequestPrice:  p.PerRequestPrice,
		Intervals:        intervals,
	}
}
