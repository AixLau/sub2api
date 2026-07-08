package middleware

import (
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/googleapi"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyAuthGoogle is a Google-style error wrapper for API key auth.
func APIKeyAuthGoogle(apiKeyService *service.APIKeyService, cfg *config.Config) gin.HandlerFunc {
	return APIKeyAuthWithSubscriptionGoogle(apiKeyService, nil, cfg)
}

// APIKeyAuthWithSubscriptionGoogle behaves like ApiKeyAuthWithSubscription but returns Google-style errors:
// {"error":{"code":401,"message":"...","status":"UNAUTHENTICATED"}}
//
// It is intended for Gemini native endpoints (/v1beta) to match Gemini SDK expectations.
func APIKeyAuthWithSubscriptionGoogle(apiKeyService *service.APIKeyService, subscriptionService *service.SubscriptionService, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if v := strings.TrimSpace(c.Query("api_key")); v != "" {
			markOpsAPIKeyAuthDiagnostic(c, "api_key_in_query_deprecated", "api_key_in_query", nil, map[string]string{"parameter": "api_key"})
			abortWithGoogleError(c, 400, localizedAPIKeyAuthMessage("api_key_in_query_deprecated"))
			return
		}
		apiKeyString := extractAPIKeyForGoogle(c)
		if apiKeyString == "" {
			markOpsAPIKeyAuthDiagnostic(c, "API_KEY_REQUIRED", "missing_api_key", nil, map[string]string{
				"accepted_headers": "x-goog-api-key,Authorization Bearer,x-api-key",
			})
			abortWithGoogleError(c, 401, localizedAPIKeyAuthMessage("API_KEY_REQUIRED"))
			return
		}

		apiKey, err := apiKeyService.GetByKey(c.Request.Context(), apiKeyString)
		if err != nil {
			if errors.Is(err, service.ErrAPIKeyNotFound) {
				markOpsAPIKeyAuthDiagnostic(c, "INVALID_API_KEY", "api_key_not_found", nil, map[string]string{
					"attempted_key_prefix": apiKeyPrefixForOps(apiKeyString),
				})
				abortWithGoogleError(c, 401, localizedAPIKeyAuthMessage("INVALID_API_KEY"))
				return
			}
			markOpsAPIKeyAuthDiagnostic(c, "INTERNAL_ERROR", "api_key_lookup_failed", nil, map[string]string{
				"raw_error": strings.TrimSpace(err.Error()),
			})
			abortWithGoogleError(c, 500, localizedAPIKeyAuthMessage("INTERNAL_ERROR"))
			return
		}

		// 同 api_key_auth.go：早退中断前也写入 Ops 回退 key，便于错误日志展示
		// user/group/platform。
		SetOpsFallbackAPIKey(c, apiKey)

		if !apiKey.IsActive() {
			markOpsAPIKeyAuthDiagnostic(c, "API_KEY_DISABLED", "api_key_disabled", apiKey, nil)
			abortWithGoogleError(c, 401, localizedAPIKeyAuthMessage("API_KEY_DISABLED"))
			return
		}
		if apiKey.User == nil {
			markOpsAPIKeyAuthDiagnostic(c, "USER_NOT_FOUND", "api_key_user_missing", apiKey, nil)
			abortWithGoogleError(c, 401, localizedAPIKeyAuthMessage("USER_NOT_FOUND"))
			return
		}
		if !apiKey.User.IsActive() {
			markOpsAPIKeyAuthDiagnostic(c, "USER_INACTIVE", "user_inactive", apiKey, nil)
			abortWithGoogleError(c, 401, localizedAPIKeyAuthMessage("USER_INACTIVE"))
			return
		}
		if code, message, ok := validateAPIKeyGroupAvailable(apiKey); !ok {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonAPIKeyGroupUnavailable)
			markOpsAPIKeyAuthDiagnostic(c, code, strings.ToLower(code), apiKey, nil)
			abortWithGoogleError(c, 403, message)
			return
		}

		// 简易模式：跳过余额和订阅检查
		if cfg.RunMode == config.RunModeSimple {
			c.Set(string(ContextKeyAPIKey), apiKey)
			c.Set(string(ContextKeyUser), AuthSubject{
				UserID:      apiKey.User.ID,
				Concurrency: apiKey.User.Concurrency,
			})
			c.Set(string(ContextKeyUserRole), apiKey.User.Role)
			setGroupContext(c, apiKey.Group)
			_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
			c.Next()
			return
		}

		isSubscriptionType := apiKey.Group != nil && apiKey.Group.IsSubscriptionType()
		if isSubscriptionType && subscriptionService != nil {
			subscription, err := subscriptionService.GetActiveSubscription(
				c.Request.Context(),
				apiKey.User.ID,
				apiKey.Group.ID,
			)
			if err != nil {
				markOpsAPIKeyAuthDiagnostic(c, "SUBSCRIPTION_NOT_FOUND", "subscription_not_found", apiKey, map[string]string{
					"raw_error": strings.TrimSpace(err.Error()),
				})
				abortWithGoogleError(c, 403, localizedAPIKeyAuthMessage("SUBSCRIPTION_NOT_FOUND"))
				return
			}

			needsMaintenance, err := subscriptionService.ValidateAndCheckLimits(subscription, apiKey.Group)
			if err != nil {
				status := 403
				if errors.Is(err, service.ErrDailyLimitExceeded) ||
					errors.Is(err, service.ErrWeeklyLimitExceeded) ||
					errors.Is(err, service.ErrMonthlyLimitExceeded) {
					status = 429
				}
				code := "SUBSCRIPTION_INVALID"
				if status == 429 {
					code = "USAGE_LIMIT_EXCEEDED"
				}
				markOpsAPIKeyAuthDiagnostic(c, code, strings.ToLower(code), apiKey, map[string]string{
					"raw_error": strings.TrimSpace(err.Error()),
				})
				abortWithGoogleError(c, status, localizedSubscriptionErrorMessage(err))
				return
			}

			c.Set(string(ContextKeySubscription), subscription)

			if needsMaintenance {
				maintenanceCopy := *subscription
				subscriptionService.DoWindowMaintenance(&maintenanceCopy)
			}
		} else {
			if apiKey.User.Balance <= 0 {
				markOpsAPIKeyAuthDiagnostic(c, "INSUFFICIENT_BALANCE", "insufficient_balance", apiKey, nil)
				abortWithGoogleError(c, 403, localizedAPIKeyAuthMessage("INSUFFICIENT_BALANCE"))
				return
			}
		}

		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Set(string(ContextKeyUser), AuthSubject{
			UserID:      apiKey.User.ID,
			Concurrency: apiKey.User.Concurrency,
		})
		c.Set(string(ContextKeyUserRole), apiKey.User.Role)
		setGroupContext(c, apiKey.Group)
		_ = apiKeyService.TouchLastUsed(c.Request.Context(), apiKey.ID)
		c.Next()
	}
}

// extractAPIKeyForGoogle extracts API key for Google/Gemini endpoints.
// Priority: x-goog-api-key > Authorization: Bearer > x-api-key > query key
// This allows OpenClaw and other clients using Bearer auth to work with Gemini endpoints.
func extractAPIKeyForGoogle(c *gin.Context) string {
	// 1) preferred: Gemini native header
	if k := strings.TrimSpace(c.GetHeader("x-goog-api-key")); k != "" {
		return k
	}

	// 2) fallback: Authorization: Bearer <key>
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if k := strings.TrimSpace(parts[1]); k != "" {
				return k
			}
		}
	}

	// 3) x-api-key header (backward compatibility)
	if k := strings.TrimSpace(c.GetHeader("x-api-key")); k != "" {
		return k
	}

	// 4) query parameter key (for specific paths)
	if allowGoogleQueryKey(c.Request.URL.Path) {
		if v := strings.TrimSpace(c.Query("key")); v != "" {
			return v
		}
	}

	return ""
}

func allowGoogleQueryKey(path string) bool {
	return strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/antigravity/v1beta")
}

func abortWithGoogleError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    status,
			"message": message,
			"status":  googleapi.HTTPStatusToGoogleStatus(status),
		},
	})
	c.Abort()
}
