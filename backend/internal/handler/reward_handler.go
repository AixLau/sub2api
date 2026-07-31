package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type RewardHandler struct {
	service *service.RewardService
}

func NewRewardHandler(rewardService *service.RewardService) *RewardHandler {
	return &RewardHandler{service: rewardService}
}

type pendingRewardSkinResponse struct {
	ID             int64  `json:"id,omitempty"`
	Name           string `json:"name,omitempty"`
	ImageURL       string `json:"image_url,omitempty"`
	CoverColor     string `json:"cover_color,omitempty"`
	CoverTextColor string `json:"cover_text_color,omitempty"`
	Alt            string `json:"alt,omitempty"`
}

type pendingRewardResponse struct {
	GrantID        int64                      `json:"grant_id"`
	CampaignID     int64                      `json:"campaign_id"`
	Title          string                     `json:"title"`
	Hint           string                     `json:"hint"`
	CoverText      string                     `json:"cover_text"`
	ClaimCTA       string                     `json:"claim_cta"`
	SuccessMessage string                     `json:"success_message"`
	Skin           *pendingRewardSkinResponse `json:"skin"`
	Priority       int                        `json:"priority"`
	ExpiresAt      *time.Time                 `json:"expires_at"`
}

func (h *RewardHandler) Pending(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	grants, err := h.service.Pending(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items := make([]pendingRewardResponse, 0, len(grants))
	for i := range grants {
		grant := &grants[i]
		copy := localizedRewardCopy(*grant, c.GetHeader("Accept-Language"))
		item := pendingRewardResponse{
			GrantID: grant.ID, CampaignID: grant.CampaignID, Title: copy.Title,
			Hint: copy.Prompt, CoverText: copy.CoverText, ClaimCTA: copy.ContinueText,
			SuccessMessage: copy.CreditedText, Priority: grant.Priority,
		}
		if !grant.ExpiresAt.IsZero() {
			expiresAt := grant.ExpiresAt
			item.ExpiresAt = &expiresAt
		}
		if grant.Skin.ID > 0 || grant.Skin.ImageURL != "" {
			item.Skin = &pendingRewardSkinResponse{
				ID: grant.Skin.ID, Name: grant.Skin.Name, ImageURL: grant.Skin.ImageURL,
				Alt: grant.Skin.Description,
			}
		}
		items = append(items, item)
	}
	response.Success(c, gin.H{"items": items})
}

func (h *RewardHandler) View(c *gin.Context) {
	subject, grantID, ok := rewardRequestIdentity(c)
	if !ok {
		return
	}
	if _, err := h.service.MarkViewed(c.Request.Context(), subject.UserID, grantID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"viewed": true})
}

func (h *RewardHandler) Claim(c *gin.Context) {
	subject, grantID, ok := rewardRequestIdentity(c)
	if !ok {
		return
	}
	result, err := h.service.Claim(c.Request.Context(), subject.UserID, grantID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *RewardHandler) SkinContent(c *gin.Context) {
	skinID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || skinID <= 0 {
		response.BadRequest(c, "Invalid reward skin id")
		return
	}
	mimeType, hash, content, err := h.service.SkinContent(c.Request.Context(), skinID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", `"`+hash+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if c.GetHeader("If-None-Match") == `"`+hash+`"` {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, mimeType, content)
}

func rewardRequestIdentity(c *gin.Context) (middleware2.AuthSubject, int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return middleware2.AuthSubject{}, 0, false
	}
	grantID, err := strconv.ParseInt(c.Param("grant_id"), 10, 64)
	if err != nil || grantID <= 0 {
		response.BadRequest(c, "Invalid reward grant id")
		return middleware2.AuthSubject{}, 0, false
	}
	return subject, grantID, true
}

func localizedRewardCopy(grant service.RewardGrant, acceptLanguage string) service.RewardCopy {
	language := strings.ToLower(strings.TrimSpace(acceptLanguage))
	if strings.HasPrefix(language, "zh") {
		if copy, ok := grant.CopyI18n["zh"]; ok {
			return mergeRewardCopy(copy, grant.Copy)
		}
	}
	if copy, ok := grant.CopyI18n["en"]; ok {
		return mergeRewardCopy(copy, grant.Copy)
	}
	return grant.Copy
}

func mergeRewardCopy(copy, fallback service.RewardCopy) service.RewardCopy {
	if copy.Title == "" {
		copy.Title = fallback.Title
	}
	if copy.Prompt == "" {
		copy.Prompt = fallback.Prompt
	}
	if copy.CoverText == "" {
		copy.CoverText = fallback.CoverText
	}
	if copy.GestureHint == "" {
		copy.GestureHint = fallback.GestureHint
	}
	if copy.RevealedHint == "" {
		copy.RevealedHint = fallback.RevealedHint
	}
	if copy.WonText == "" {
		copy.WonText = fallback.WonText
	}
	if copy.CreditedText == "" {
		copy.CreditedText = fallback.CreditedText
	}
	if copy.ContinueText == "" {
		copy.ContinueText = fallback.ContinueText
	}
	return copy
}
