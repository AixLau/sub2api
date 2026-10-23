package admin

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// withDefaultAdminUserExclusions keeps admin reporting focused on end-user
// activity even when the caller does not provide an explicit exclusion list.
// Explicit exclusions remain supported and are merged without duplicates.
func withDefaultAdminUserExclusions(ctx context.Context, adminService service.AdminService, userID int64, excluded []int64) []int64 {
	// An explicit user drill-down is an intentional override of the default
	// reporting scope, including when the selected user is an administrator.
	if userID > 0 {
		return excluded
	}
	if adminService == nil {
		return excluded
	}
	admins, _, err := adminService.ListUsers(ctx, 1, 1000, service.UserListFilters{Role: service.RoleAdmin}, "id", "asc")
	if err != nil {
		logger.LegacyPrintf("handler.admin", "failed to load admin users for default exclusion: %v", err)
		return excluded
	}
	seen := make(map[int64]struct{}, len(excluded)+len(admins))
	merged := make([]int64, 0, len(excluded)+len(admins))
	for _, id := range excluded {
		if id > 0 {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				merged = append(merged, id)
			}
		}
	}
	for _, admin := range admins {
		if admin.ID > 0 {
			if _, ok := seen[admin.ID]; !ok {
				seen[admin.ID] = struct{}{}
				merged = append(merged, admin.ID)
			}
		}
	}
	return merged
}
