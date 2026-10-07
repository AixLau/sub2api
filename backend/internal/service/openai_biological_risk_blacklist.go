package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const biologicalRiskBlacklistMessage = "content was flagged for possible biological risk"

func isOpenAIBiologicalRiskMessage(message string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(message)), biologicalRiskBlacklistMessage)
}

func (s *OpenAIGatewayService) maybeBlacklistCodexBiologicalRiskUser(ctx context.Context, c *gin.Context, account *Account, message string, body []byte) {
	if s == nil || s.settingService == nil || account == nil || account.Platform != PlatformOpenAI {
		return
	}
	cyberPolicy, _, _ := detectOpenAICyberPolicy(body)
	if !cyberPolicy && !isOpenAIBiologicalRiskMessage(message) && !isOpenAIBiologicalRiskMessage(extractUpstreamErrorMessage(body)) {
		return
	}
	userID := codexSessionIdentityUserID(c)
	if userID <= 0 {
		return
	}
	if err := s.settingService.AddCodexCLIOnlyUserToBlacklist(ctx, userID); err != nil {
		logger.FromContext(ctx).Warn("failed to auto-blacklist Codex biological-risk user",
			zap.Int64("user_id", userID), zap.Int64("account_id", account.ID), zap.Error(err))
	}
}
