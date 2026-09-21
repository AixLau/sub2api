# Codex HTTP session fingerprint

`codex_fingerprint_mode=session` 在普通 HTTP `/responses` 和 HTTP passthrough 中使用以下身份层级：

- **Device**：复用账号级稳定 `installation_id`。
- **Session**：普通任务按 OAuth 账号、下游用户、固定时间周期生成 UUIDv7；`/side` 按原始 session 单独登记持久 UUIDv7。同一用户更换自己的 API Key 不会新建 session。
- **Thread**：每个原始任务独立映射。首次选定的最终 thread ID 持久保存，跨周期、重试和后续引用保持不变。
- **Turn**：保留客户端原始 `turn_id`、`parent_turn_id`、`root_turn_id` 和 `turn_started_at_unix_ms`，维持同一棵 turn 树。仅缺少 `turn_id` 时生成 UUIDv7，缺少开始时间时补当前时间；同一请求的 header 和 body 共用一个 snapshot。

下游用户优先取已认证的 `UserID`，只有缺少用户身份时才使用 API Key ID；不通过 IP 或 User-Agent 猜测使用者。任务身份优先取原始 `thread_id`，其次为原始 `session_id`，最后为 `x-client-request-id`。客户端 session 的 UUID 版本不影响最终 session。

周期长度为固定 **5～7 天**。账号 fingerprint seed 和用户 scope 决定周期长度与时间偏移，使用户的换代时刻分散。周期按固定时间轴划分，因此首次加入某个周期时只使用该周期的剩余时间。请求活跃度不会延长周期。

普通周期 session 的 v3 映射继续复用 Redis 原子 `SET NX`；首次创建时设置 TTL 为本周期剩余时间加 **1 小时 grace**。读取和并发创建失败均不续期，grace 也不推迟新周期的启用。

HTTP session resolver 优先查「用户、OAuth 账号、原始 session」的 side 登记。已登记时，继续聊天和 spawn_agent 即使不再携带 fork 字段，也沿用 side session；未登记且原始 `forked_from_thread_id` 非空时创建 side 登记；其余请求继续使用普通周期 session。不使用 IP、UA 或 thread_source 判断 side，也不增加管理员配置。

Side 使用独立的 `v3:side-session` 命名空间，复用现有 durable store 和 `SET NX`，不带 epoch、不设置到期时间。存储读取失败会返回错误，不会将可能属于 side 的请求降回普通周期 session。缺少原始 session 的 side 请求无法可靠登记，会返回错误。

Thread 和 parent/fork 引用共用「用户、账号、原始 thread」的 `v3:thread` 持久记录，不带 epoch。首次选择仍使用现有 thread 投影作为候选，通过 `SET NX` 固定结果；后续请求始终读取已选结果。父线程在 epoch 1 使用 TA 后，epoch 2 直接创建 side 时会引用 TA，不会计算新的 TA2。

Parent/fork 来源必须已有 thread 记录，引用本身不会创建记录。缺失时返回错误，不猜测它过去属于哪个 epoch。旧版本未保存的历史 thread 无法据此还原；记录从本实现首次处理该 thread 开始保存。`forked_from_ordinal_exclusive`、`thread_source`、`subagent_kind`、`agent_name`、`request_kind` 保持原值。

普通任务的 Cache 继续按「用户、账号、epoch、原始 session root、原始 prompt cache partition」确定性映射；side 使用不带 epoch 的 side 命名空间。Cache 不能使用当前 child thread：普通 root 与 child 共享 CA，side root 与自己的 child 共享 CC，两者隔离。缺少原始 session 时依次使用原始 cache key、task identity 作为 root；缺少 cache key 时使用原始 root 作为默认 partition。显式 cache key 在各自原始 root 内保留独立分区。

| 客户端拓扑 | 上游 Session | 映射后的 thread 关系 | Cache |
| --- | --- | --- | --- |
| Root：session=A、thread=A、cache=A | S | TA | CA |
| spawn_agent：session=A、thread=B、parent=A、cache=A | S | TB，parent=TA | CA，与 root 共享 |
| /side：session=C、thread=C、forked_from=A、cache=C | SC（不同于 S） | TC，forked_from=TA | CC，与 root 隔离 |
| Side child：session=C、thread=D、parent=C、cache=C | SC | TD，parent=TC | CC，与 side root 共享 |

跨普通 epoch 时，普通 session 和 cache 换代，thread 记录不变；side session、thread 和 cache 均保持稳定。

Root 和 side 首次请求的 `turn_id == root_turn_id` 保持成立；child 的 `parent_turn_id`、`root_turn_id` 继续指向客户端原始 turn。重试同一个客户端 turn 不会人为产生新的 turn ID。

最终 normalization 以 staged v3 snapshot 为准，只同步载体，不再恢复客户端 UUIDv7 或调用 v2 session mapper。已有 `sandbox` 标签按最终出站 UA 对齐，`sandbox_mode` 和权限保持原样。缺少可识别用户或任务时保守使用 device 投影。

新建 OAuth 账号仍默认使用 `device`；管理员需要显式选择 `session`。本次不修改 WS、旧 `/responses/compact`、兼容消息桥接和探测的 session 策略，也不调整 Spark shadow。`off`、`device` 的原有 v2 隔离映射和存量 key 保持现有行为。

实现复用现有 Redis 和 UUID 库；原子写入与到期语义参见 [Redis SET 文档](https://redis.io/docs/latest/commands/set/)。
