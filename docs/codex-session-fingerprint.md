# Codex HTTP session fingerprint

`codex_fingerprint_mode=session` 在普通 HTTP `/responses` 和 HTTP passthrough 中使用以下身份层级：

- **Device**：复用账号级稳定 `installation_id`。
- **Session**：按 OAuth 账号、下游用户、固定时间周期生成 UUIDv7。同一用户更换自己的 API Key 不会新建 session。
- **Thread**：每个原始任务独立映射，当前周期内重试和继续请求保持不变。
- **Turn**：保留客户端原始 `turn_id`、`parent_turn_id`、`root_turn_id` 和 `turn_started_at_unix_ms`，维持同一棵 turn 树。仅缺少 `turn_id` 时生成 UUIDv7，缺少开始时间时补当前时间；同一请求的 header 和 body 共用一个 snapshot。

下游用户优先取已认证的 `UserID`，只有缺少用户身份时才使用 API Key ID；不通过 IP 或 User-Agent 猜测使用者。任务身份优先取原始 `thread_id`，其次为原始 `session_id`，最后为 `x-client-request-id`。客户端 session 的 UUID 版本不影响最终 session。

周期长度为固定 **5～7 天**。账号 fingerprint seed 和用户 scope 决定周期长度与时间偏移，使用户的换代时刻分散。周期按固定时间轴划分，因此首次加入某个周期时只使用该周期的剩余时间。请求活跃度不会延长周期。

v3 映射复用 Redis 的原子 `SET NX`；首次创建时设置 TTL 为本周期剩余时间加 **1 小时 grace**。读取和并发创建失败均不续期，grace 也不推迟新周期的启用。存储不可用时返回错误，不临时生成会导致同周期身份分裂的 session。

Thread、parent thread 和 `forked_from_thread_id` 使用同一个用户、账号和 epoch 命名空间及 thread mapper。`forked_from_ordinal_exclusive`、`thread_source`、`subagent_kind`、`agent_name`、`request_kind` 保持原值。**跨 epoch 后，同一个任务及其 parent/fork 引用会映射到新 thread，cache 也随之换代；客户端 turn 树仍原样保留。**

Cache 按「用户、账号、epoch、原始 session root、原始 prompt cache partition」确定性映射，不能使用压缩后的 session 或当前 child thread。缺少原始 session 时依次使用原始 cache key、task identity 作为 root；缺少 cache key 时使用原始 root 作为默认 partition。显式 cache key 在各自原始 root 内保留独立分区。

| 客户端拓扑 | 压缩 session | 映射后的 thread 关系 | Cache |
| --- | --- | --- | --- |
| Root：session=A、thread=A、cache=A | S | TA | CA |
| spawn_agent：session=A、thread=B、parent=A、cache=A | S | TB，parent=TA | CA，与 root 共享 |
| /side：session=C、thread=C、forked_from=A、cache=C | S | TC，forked_from=TA | CC，与 root 隔离 |

Root 和 side 首次请求的 `turn_id == root_turn_id` 保持成立；child 的 `parent_turn_id`、`root_turn_id` 继续指向客户端原始 turn。重试同一个客户端 turn 不会人为产生新的 turn ID。

最终 normalization 以 staged v3 snapshot 为准，只同步载体，不再恢复客户端 UUIDv7 或调用 v2 session mapper。已有 `sandbox` 标签按最终出站 UA 对齐，`sandbox_mode` 和权限保持原样。缺少可识别用户或任务时保守使用 device 投影。

新建 OAuth 账号仍默认使用 `device`；管理员需要显式选择 `session`。本次不修改 WS、旧 `/responses/compact`、兼容消息桥接和探测的 session 策略，也不调整 Spark shadow。`off`、`device` 的原有 v2 隔离映射和存量 key 保持现有行为。

实现复用现有 Redis 和 UUID 库；原子写入与到期语义参见 [Redis SET 文档](https://redis.io/docs/latest/commands/set/)。
