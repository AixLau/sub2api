package service

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type userDashboardStatsCacheRepo struct {
	UsageLogRepository
	calls atomic.Int32
}

func (r *userDashboardStatsCacheRepo) GetUserDashboardStats(context.Context, int64) (*usagestats.UserDashboardStats, error) {
	r.calls.Add(1)
	return &usagestats.UserDashboardStats{TotalRequests: 42}, nil
}

func TestUsageService_GetUserDashboardStats_UsesCache(t *testing.T) {
	repo := &userDashboardStatsCacheRepo{}
	svc := NewUsageService(repo, nil, nil, nil)

	first, err := svc.GetUserDashboardStats(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(42), first.TotalRequests)

	second, err := svc.GetUserDashboardStats(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(42), second.TotalRequests)
	require.Equal(t, int32(1), repo.calls.Load())

	_, err = svc.GetUserDashboardStats(context.Background(), 8)
	require.NoError(t, err)
	require.Equal(t, int32(2), repo.calls.Load())
}
