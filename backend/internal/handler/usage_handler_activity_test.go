//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dashboardActivityRepoStub struct {
	service.UsageLogRepository
	userID      int64
	windowStart time.Time
	windowEnd   time.Time
	currentDay  time.Time
	timezone    string
}

func (s *dashboardActivityRepoStub) GetUserDashboardActivity(_ context.Context, userID int64, windowStart, windowEnd, currentDay time.Time, userTimezone string) (*usagestats.UserDashboardActivity, error) {
	s.userID = userID
	s.windowStart = windowStart
	s.windowEnd = windowEnd
	s.currentDay = currentDay
	s.timezone = userTimezone
	return &usagestats.UserDashboardActivity{
		WindowStart:       windowStart.Format("2006-01-02"),
		WindowEnd:         windowEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		TotalTokens:       120,
		PeakDailyTokens:   80,
		CurrentStreakDays: 2,
		LongestStreakDays: 3,
		Days: []usagestats.UserActivityDay{{
			Date:        windowStart.Format("2006-01-02"),
			TotalTokens: 40,
		}},
	}, nil
}

func TestDashboardActivityUsesAuthenticatedUserAndCalendarWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dashboardActivityRepoStub{}
	handler := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		c.Next()
	})
	router.GET("/usage/dashboard/activity", handler.DashboardActivity)

	req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/activity?timezone=Asia%2FShanghai", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), repo.userID)
	require.Equal(t, "Asia/Shanghai", repo.timezone)
	require.Equal(t, time.Monday, repo.windowStart.Weekday())
	require.Equal(t, 52*24*time.Hour, repo.windowEnd.Sub(repo.windowStart))
	require.Equal(t, 0, repo.currentDay.Hour())
	require.Contains(t, rec.Body.String(), `"total_tokens":120`)
	require.Contains(t, rec.Body.String(), `"current_streak_days":2`)
}
