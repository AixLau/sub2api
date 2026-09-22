# Codex HTTP session fingerprint

`codex_fingerprint_mode=session` 在普通 HTTP `/responses` 和 HTTP passthrough 中使用以下身份层级：

- **Device**：复用账号级稳定 `installation_id`。
- **Session**：普通任务按 OAuth 账号、下游用户、固定时间周期生成 UUIDv7；`/side` 按原始 session 单独登记持久 UUIDv7。同一用户更换自己的 API Key 不会新建 session。
- **Thread**：每个原始任务在其最终 session 内独立映射。普通 session 换代时 thread 一起换代；side 的 session 和 thread 长期稳定。同一 session 内重试保持相同 thread。
- **Turn**：保留客户端原始 `turn_id`、`parent_turn_id`、`root_turn_id` 和 `turn_started_at_unix_ms`，维持同一棵 turn 树。仅缺少 `turn_id` 时生成 UUIDv7，缺少开始时间时补当前时间；同一请求的 header 和 body 共用一个 snapshot。

下游用户优先取已认证的 `UserID`，只有缺少用户身份时才使用 API Key ID；不通过 IP 或 User-Agent 猜测使用者。任务身份优先取原始 `thread_id`，其次为原始 `session_id`，最后为 `x-client-request-id`。客户端 session 的 UUID 版本不影响最终 session。

周期长度为固定 **5～7 天**。账号 fingerprint seed 和用户 scope 决定周期长度与时间偏移，使用户的换代时刻分散。周期按固定时间轴划分，因此首次加入某个周期时只使用该周期的剩余时间。请求活跃度不会延长周期。

普通周期 session 的 v3 映射继续复用 Redis 原子 `SET NX`；首次创建时设置 TTL 为本周期剩余时间加 **1 小时 grace**。读取和并发创建失败均不续期，grace 也不推迟新周期的启用。

HTTP session resolver 优先查「用户、OAuth 账号、原始 session」的 side 登记。已登记时，继续聊天和 spawn_agent 即使不再携带 fork 字段，也沿用 side session；未登记且原始 `forked_from_thread_id` 非空时创建 side 登记；其余请求继续使用普通周期 session。不使用 IP、UA 或 thread_source 判断 side，也不增加管理员配置。

Side 使用独立的 `v3:side-session` 命名空间，复用现有 durable store 和 `SET NX`，不带 epoch、不设置到期时间。存储读取失败会返回错误，不会将可能属于 side 的请求降回普通周期 session。缺少原始 session 的 side 请求无法可靠登记，会返回错误。

当前 Thread 按「最终 session、原始 thread」确定性生成。`v4:thread-current` 记录按用户、账号、最终 session 和原始 thread 隔离，用于确认该 thread 已在所属 session 中完成身份选择。普通记录的 TTL 为本周期剩余时间加 1 小时 grace，读取不续期；side 记录不带普通 epoch、不设置到期时间。普通请求不再用旧的全局永久 thread 记录决定当前 thread。

普通 child 的 `parent_thread_id` 必须引用当前最终 session 中已登记的 parent。周期换代后，如果先收到 child、尚未收到 root 的请求，则返回缺少当前 session parent 的错误。需要 root 先继续一次请求，才能建立新周期的 parent 身份；不能用旧周期的 parent 代替，也不能仅凭引用登记一个尚未选择过的 parent。

新建 side 的 `forked_from_thread_id` 可以引用历史 session 中的 thread。为此，`v4:thread-history` 为每个「用户、账号、原始 thread」仅保留最近一次已选择的最终 session、thread 和服务端捕获的请求时间，不积累逐 epoch 历史。更新使用原子 compare-and-swap 和请求时间排序，旧周期延迟完成的请求不能覆盖新周期的记录。这个索引只负责寻找新建 side 的 fork 来源，不决定普通请求的当前 thread。

每个 side 首次创建时通过 `SET NX` 将确切的 fork 目标固定在 `v4:side-fork` 中。假设 root 在 epoch 1 使用 TA1，side C 随后引用 TA1；epoch 2 的 root 换成 TA2 后，C 的重试仍引用 TA1，而此后新建的 side 才引用历史索引里的 TA2。如果进入 epoch 2 后 root 尚未再次请求，直接创建 side 仍引用实际登记过的 TA1，不按新周期猜测一个 TA2。同一原始 side session 后续声明不同的 fork 来源会报冲突。

已有的 `v3:thread` 记录可以作为确切的历史引用证据；已经存在的 side 也可继续采用已知的 v3 thread 投影，维持其存续期间的身份。这些记录不会用于选择新的普通当前 thread。完全没有历史记录的 parent/fork 请求仍返回明确错误，不根据当前周期、前一周期或 UUID 时间猜测。旧版本未保存的 thread 无法可靠恢复；让源线程重新请求可以建立一个新的可确认引用目标，但不等于还原旧身份，因此不保证无感迁移。`forked_from_ordinal_exclusive`、`thread_source`、`subagent_kind`、`agent_name`、`request_kind` 保持原值。

普通 thread-current 记录随 epoch 到期回收，但每个原始 thread 的最新历史索引，以及 side session、side thread 和固定 fork 记录仍持久保存。这次不包含完整 GC；持续产生新的原始任务或 side 仍会增加持久记录数量。没有通过给 side 登记或历史引用随意增加 TTL 来缩短既有续聊和 fork 能力。

普通任务的 Cache 继续按「用户、账号、epoch、原始 session root、原始 prompt cache partition」确定性映射；side 使用不带 epoch 的 side 命名空间。Cache 不能使用当前 child thread：普通 root 与 child 共享 CA，side root 与自己的 child 共享 CC，两者隔离。缺少原始 session 时依次使用原始 cache key、task identity 作为 root；缺少 cache key 时使用原始 root 作为默认 partition。显式 cache key 在各自原始 root 内保留独立分区。

| 客户端拓扑 | 上游 Session | 映射后的 thread 关系 | Cache |
| --- | --- | --- | --- |
| Root：session=A、thread=A、cache=A | S | TA | CA |
| spawn_agent：session=A、thread=B、parent=A、cache=A | S | TB，parent=TA | CA，与 root 共享 |
| /side：session=C、thread=C、forked_from=A、cache=C | SC（不同于 S） | TC，forked_from=TA | CC，与 root 隔离 |
| Side child：session=C、thread=D、parent=C、cache=C | SC | TD，parent=TC | CC，与 side root 共享 |

跨普通 epoch 时，普通 session、thread 和 cache 一起换代；side session、thread、cache 和已固定的 fork 目标均保持稳定。

| 跨周期操作 | 普通 root | 已有 side C | 此时新建的 side |
| --- | --- | --- | --- |
| epoch 1：root 请求后创建 C | S1 / TA1 | SC / TC，forked_from=TA1 | — |
| epoch 2：root 尚未请求，直接创建 E | 最近已登记 TA1 | SC / TC，forked_from=TA1 | SE / TE，forked_from=TA1 |
| epoch 2：root 已请求后，再创建 side F | S2 / TA2 | SC / TC，forked_from=TA1 | SF / TF，forked_from=TA2；E 仍指向 TA1 |

Root 和 side 首次请求的 `turn_id == root_turn_id` 保持成立；child 的 `parent_turn_id`、`root_turn_id` 继续指向客户端原始 turn。重试同一个客户端 turn 不会人为产生新的 turn ID。

最终 normalization 以 staged v3 snapshot 为准，只同步载体，不再恢复客户端 UUIDv7 或调用 v2 session mapper。已有 `sandbox` 标签按最终出站 UA 对齐，`sandbox_mode` 和权限保持原样。缺少可识别用户或任务时保守使用 device 投影。

新建 OAuth 账号仍默认使用 `device`；管理员需要显式选择 `session`。本次不修改 WS、旧 `/responses/compact`、兼容消息桥接和探测的 session 策略，也不调整 Spark shadow。`off`、`device` 的原有 v2 隔离映射和存量 key 保持现有行为。

实现复用现有 Redis 和 UUID 库；原子写入与到期语义参见 [Redis SET 文档](https://redis.io/docs/latest/commands/set/)。
