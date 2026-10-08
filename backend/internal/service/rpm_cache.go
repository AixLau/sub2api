package service

import "context"

// RPMCache RPM 计数器缓存接口
// 滚动 60 秒计数用于监控；自然分钟计数保留供 Anthropic 限流使用。
type RPMCache interface {
	// IncrementRecentRPM records a successful completion at Redis server time.
	IncrementRecentRPM(ctx context.Context, accountID int64) (int, error)
	// GetRecentRPMBatch counts completions in (now - 60 seconds, now].
	GetRecentRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)

	// IncrementRPM 原子递增并返回当前分钟的计数
	// 使用 Redis 服务器时间确定 minute key，避免多实例时钟偏差
	IncrementRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPM 获取当前分钟的 RPM 计数
	GetRPM(ctx context.Context, accountID int64) (count int, err error)

	// GetRPMBatch 批量获取多个账号的 RPM 计数（使用 Pipeline）
	GetRPMBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)
}
