package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type pendingRewardRepositoryStub struct {
	service.RewardRepository
	grant service.RewardGrant
}

func (r *pendingRewardRepositoryStub) ImportLegacyPending(context.Context, int64, time.Time) error {
	return nil
}

func (r *pendingRewardRepositoryStub) ExpirePendingForUser(context.Context, int64, time.Time) error {
	return nil
}

func (r *pendingRewardRepositoryStub) ListRuntimeCampaigns(
	context.Context,
	string,
	time.Time,
) ([]service.RewardCampaign, error) {
	return nil, nil
}

func (r *pendingRewardRepositoryStub) GetAudienceProfile(
	context.Context,
	int64,
	time.Time,
) (*service.RewardAudienceProfile, error) {
	return &service.RewardAudienceProfile{Email: "user@example.com"}, nil
}

func (r *pendingRewardRepositoryStub) ListPending(
	context.Context,
	int64,
	time.Time,
) ([]service.RewardGrant, error) {
	return []service.RewardGrant{r.grant}, nil
}

func TestRewardPendingResponseDoesNotLeakAmount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	expiresAt := time.Date(2026, time.July, 31, 12, 0, 0, 0, time.UTC)
	balanceAfter := 1000.0
	repo := &pendingRewardRepositoryStub{grant: service.RewardGrant{
		ID:            41,
		CampaignID:    7,
		CampaignTitle: "Internal campaign title",
		UserID:        99,
		Amount:        888.88,
		BalanceAfter:  &balanceAfter,
		Priority:      20,
		Copy: service.RewardCopy{
			Title:     "Fallback title",
			Prompt:    "Fallback hint",
			CoverText: "Fallback cover",
		},
		CopyI18n: map[string]service.RewardCopy{
			"zh": {
				Title:        "夏日奖励",
				Prompt:       "刮开领取",
				CoverText:    "刮开这里",
				ContinueText: "继续使用",
				CreditedText: "已到账余额",
			},
		},
		Skin: service.RewardSkinSnapshot{
			ID:          3,
			Name:        "Summer",
			Description: "Summer reward card",
			ImageURL:    "/api/v1/reward-skins/3/content?v=abc",
		},
		ExpiresAt: expiresAt,
	}}
	rewardService := service.NewRewardService(repo, nil, nil)
	handler := NewRewardHandler(rewardService)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/user/rewards/pending", nil)
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99})

	handler.Pending(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v, want object", body["data"])
	}
	items, ok := data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", data["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v, want object", items[0])
	}
	for _, forbidden := range []string{"amount", "balance", "balance_after", "user_id"} {
		if _, exists := item[forbidden]; exists {
			t.Fatalf("pending item leaked forbidden field %q: %#v", forbidden, item)
		}
	}
	if got := item["title"]; got != "夏日奖励" {
		t.Fatalf("title = %#v, want localized title", got)
	}
	if got := item["hint"]; got != "刮开领取" {
		t.Fatalf("hint = %#v, want localized hint", got)
	}
	if got := item["claim_cta"]; got != "继续使用" {
		t.Fatalf("claim_cta = %#v, want localized CTA", got)
	}
	if got := item["success_message"]; got != "已到账余额" {
		t.Fatalf("success_message = %#v, want localized success message", got)
	}
	if strings.Contains(recorder.Body.String(), "888.88") || strings.Contains(recorder.Body.String(), "1000") {
		t.Fatalf("raw pending response leaked financial values: %s", recorder.Body.String())
	}
}
