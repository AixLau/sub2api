//go:build unit

package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestBuildUserUsageRankingRedactsOthersAndHighlightsCurrentUser(t *testing.T) {
	items := []usagestats.UserBreakdownItem{
		{UserID: 11, Email: "alice@example.com", Requests: 8, TotalTokens: 900},
		{UserID: 22, Email: "me@example.com", Requests: 4, TotalTokens: 700},
	}

	got := BuildUserUsageRanking(items, 22)

	require.Equal(t, []UserUsageRankingItem{
		{Rank: 1, DisplayName: "a***@e***.com", Requests: 8, TotalTokens: 900, IsCurrent: false},
		{Rank: 2, DisplayName: "me@example.com", Requests: 4, TotalTokens: 700, IsCurrent: true},
	}, got)
}

func TestBuildUserUsageRankingCapsAtTwentyAndDoesNotAppendCurrentUser(t *testing.T) {
	items := make([]usagestats.UserBreakdownItem, 0, UserUsageRankingLimit+1)
	for i := 1; i <= UserUsageRankingLimit+1; i++ {
		items = append(items, usagestats.UserBreakdownItem{
			UserID:      int64(i),
			Email:       fmt.Sprintf("user%d@example.com", i),
			Requests:    int64(100 - i),
			TotalTokens: int64(1000 - i),
		})
	}

	got := BuildUserUsageRanking(items, UserUsageRankingLimit+1)

	require.Len(t, got, UserUsageRankingLimit)
	for _, item := range got {
		require.False(t, item.IsCurrent)
		require.False(t, strings.Contains(item.DisplayName, "user21"))
	}
}

func TestBuildUserUsageRankingUsesStableAnonymousLabelWithoutEmail(t *testing.T) {
	items := []usagestats.UserBreakdownItem{{UserID: 77, Requests: 1, TotalTokens: 10}}

	first := BuildUserUsageRanking(items, 99)
	second := BuildUserUsageRanking(items, 99)

	require.Equal(t, first, second)
	require.Regexp(t, `^User [0-9A-F]{6}$`, first[0].DisplayName)
}
