# Codex HTTP session fingerprint

`codex_fingerprint_mode=session` 在普通 HTTP `/responses` 和 HTTP passthrough 中使用以下身份层级：

- **Device**：复用账号级稳定 `installation_id`。
- **Session**：按 OAuth 账号、下游用户、固定时间周期生成 UUIDv7。同一用户更换自己的 API Key 不会新建 session。
- **Thread**：每个原始任务独立映射，当前周期内重试和继续请求保持不变。
- **Turn**：每个请求生成新的 UUIDv7，同一请求的 header 和 body 共用一个 snapshot。

下游用户优先取已认证的 `UserID`，只有缺少用户身份时才使用 API Key ID；不通过 IP 或 User-Agent 猜测使用者。任务身份优先取原始 `thread_id`，其次为原始 `session_id`，最后为 `x-client-request-id`。客户端 session 的 UUID 版本不影响最终 session。

周期长度为固定 **5～7 天**。账号 fingerprint seed 和用户 scope 决定周期长度与时间偏移，使用户的换代时刻分散。周期按固定时间轴划分，因此首次加入某个周期时只使用该周期的剩余时间。请求活跃度不会延长周期。

v3 映射复用 Redis 的原子 `SET NX`；首次创建时设置 TTL 为本周期剩余时间加 **1 小时 grace**。读取和并发创建失败均不续期，grace 也不推迟新周期的启用。存储不可用时返回错误，不临时生成会导致同周期身份分裂的 session。

Thread、parent thread 与默认 prompt cache 使用用户、账号和 epoch 的共同命名空间。`parent_thread_id` 与对应任务的 `thread_id` 使用完全相同的映射。**跨 epoch 后，同一个任务及其 parent 引用会映射到新 thread，cache 也随之换代。** 显式 prompt cache key 在任务内继续作为独立分区，不会把不同任务合并到同一个 cache。

最终 normalization 以 staged v3 snapshot 为准，只同步载体，不再恢复客户端 UUIDv7 或调用 v2 session mapper。已有 `sandbox` 标签按最终出站 UA 对齐，`sandbox_mode` 和权限保持原样。缺少可识别用户或任务时保守使用 device 投影。

新建 OAuth 账号仍默认使用 `device`；管理员需要显式选择 `session`。本次不修改 WS、旧 `/responses/compact`、兼容消息桥接和探测的 session 策略，也不调整 Spark shadow。`off`、`device` 的原有 v2 隔离映射和存量 key 保持现有行为。

实现复用现有 Redis 和 UUID 库；原子写入与到期语义参见 [Redis SET 文档](https://redis.io/docs/latest/commands/set/)。
