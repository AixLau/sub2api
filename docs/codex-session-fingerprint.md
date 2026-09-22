# Codex HTTP session fingerprint

`codex_fingerprint_mode=session` 在普通 HTTP `/responses` 和 HTTP passthrough 中使用以下身份层级：

- **Device**：复用账号级稳定 `installation_id`。
- **Session**：普通任务按 OAuth 账号、下游用户、固定时间周期生成 UUIDv7；`/side` 按原始 session 单独登记持久 UUIDv7。同一用户更换自己的 API Key 不会新建 session。
- **Thread**：每个原始任务在其最终 session 内独立映射。普通 session 换代时 thread 一起换代；side 的 session 和 thread 长期稳定。同一 session 内重试保持相同 thread。
- **Turn**：保留客户端原始 `turn_id`、`parent_turn_id`、`root_turn_id` 和 `turn_started_at_unix_ms`，维持同一棵 turn 树。仅缺少 `turn_id` 时生成 UUIDv7，缺少开始时间时补当前时间；同一请求的 header 和 body 共用一个 snapshot。

下游用户优先取已认证的 `UserID`，只有缺少用户身份时才使用 API Key ID；不通过 IP 或 User-Agent 猜测使用者。任务身份优先取原始 `thread_id`，其次为原始 `session_id`，最后为 `x-client-request-id`。客户端 session 的 UUID 版本不影响最终 session。

周期长度为固定 **5～7 天**。账号 fingerprint seed 和用户 scope 决定周期长度与时间偏移，使用户的换代时刻分散。周期按固定时间轴划分，因此首次加入某个周期时只使用该周期的剩余时间。请求活跃度不会延长周期。

服务端在 OpenAI HTTP 请求进入 forwarding 流程时捕获一次 `codexIdentityObservedAt`。本次请求的排队、账号选择、重试、failover、epoch 选择与 history 排序共用这个时间；它不是客户端的 `turn_started_at_unix_ms`。同一请求跨过周期边界也不重新选择新的 epoch。不同账号仍按各自 seed 和用户 scope 计算周期，但计算的时间输入相同。

普通周期 session 的 v3 映射继续复用 Redis 原子 `SET NX`；首次创建时设置 TTL 为本周期剩余时间加 **1 小时 grace**。Redis 写入使用由 request observation 所属 epoch 算出的绝对到期时刻，不会因排队或写入延迟向后推迟；读取和并发创建失败均不续期，grace 也不推迟新周期的启用。一个旧请求延迟到所属周期 grace 已结束后，不能重新创建已过期 epoch 的身份。

HTTP session resolver 优先查「用户、OAuth 账号、原始 session」的 side 登记。已登记时，继续聊天和 spawn_agent 即使不再携带 fork 字段，也沿用 side session；未登记且原始 `forked_from_thread_id` 非空时创建 side 登记；其余请求继续使用普通周期 session。不使用 IP、UA 或 thread_source 判断 side，也不增加 side 身份策略配置。

Side 使用独立的 `v3:side-session` 命名空间，复用现有 durable store 和 `SET NX`，不带 epoch、不设置到期时间。存储读取失败会返回错误，不会将可能属于 side 的请求降回普通周期 session。缺少原始 session 的 side 请求无法可靠登记，会返回错误。

当前 Thread 按「最终 session、原始 thread」确定性生成。`v4:thread-current` 记录按用户、账号、最终 session 和原始 thread 隔离，用于确认该 thread 已在所属 session 中完成身份选择。普通记录的 TTL 为本周期剩余时间加 1 小时 grace，读取不续期；side 记录不带普通 epoch、不设置到期时间。普通请求不再用旧的全局永久 thread 记录决定当前 thread。

普通 child 的 `parent_thread_id` 必须引用当前最终 session 中已登记的 parent。周期换代后，如果先收到 child、尚未收到 root 的请求，则返回缺少当前 session parent 的错误。需要 root 先继续一次请求，才能建立新周期的 parent 身份；不能用旧周期的 parent 代替，也不能仅凭引用登记一个尚未选择过的 parent。

新建 side 的 `forked_from_thread_id` 可以引用历史 session 中的 thread。为此，`v4:thread-history` 为每个「用户、账号、原始 thread」仅保留最近一次已选择的最终 session、thread 和服务端捕获的请求时间，不积累逐 epoch 历史。更新使用原子 compare-and-swap 和请求时间排序，旧周期延迟完成的请求不能覆盖新周期的记录，也不能回退活跃时间。真实请求再次使用同一 thread，即使最终 session 和 thread 没变，也会推进 observation。这个索引只负责寻找新建 side 的 fork 来源，不决定普通请求的当前 thread。

每个 side 首次创建时通过 `SET NX` 将确切的 fork 目标固定在 `v4:side-fork` 中。假设 root 在 epoch 1 使用 TA1，side C 随后引用 TA1；epoch 2 的 root 换成 TA2 后，C 的重试仍引用 TA1，而此后新建的 side 才引用历史索引里的 TA2。如果进入 epoch 2 后 root 尚未再次请求，直接创建 side 仍引用实际登记过的 TA1，不按新周期猜测一个 TA2。同一原始 side session 后续声明不同的 fork 来源会报冲突。

已有的 `v3:thread` 记录可以作为确切的历史引用证据；已经存在的 side 也可继续采用已知的 v3 thread 投影，维持其存续期间的身份。这些记录不会用于选择新的普通当前 thread。完全没有历史记录的 parent/fork 请求仍返回明确错误，不根据当前周期、前一周期或 UUID 时间猜测。旧版本未保存的 thread 无法可靠恢复；让源线程重新请求可以建立一个新的可确认引用目标，但不等于还原旧身份，因此不保证无感迁移。`forked_from_ordinal_exclusive`、`thread_source`、`subagent_kind`、`agent_name`、`request_kind` 保持原值。

历史索引默认保留 **180 天**，配置项为 `gateway.codex_identity.history_retention_days`，环境变量为 `GATEWAY_CODEX_IDENTITY_HISTORY_RETENTION_DAYS`。允许配置 1～36500 天；默认值偏保守，调短之前需接受旧 thread 不能再直接创建新 side 的边界。过期时间从最新真实客户端 observation 计算，不从 Redis 写入或读取时间计算；fork 查询、管理查询和后台检查均不续期。延迟完成的旧请求也不会凭完成时间获得新的完整 retention。

旧版本未设置 TTL 的 `v4:thread-history` 在首次通过带 owner 的 HTTP 身份请求读取时，也会按**记录内已有的** `observed_at_ms + retention` 补齐绝对到期时间，不把 GET 当作新的 observation。如果该期限已过，移除 history 与对应索引成员并返回缺失；已经有有限 TTL 的 history 不会被 GET 延长。因此 retention 同样适用于被接管的旧 history，而不只适用于新建或重新使用的 thread。

历史过期只影响**新建** side 寻找 fork 来源；已固定的 side fork 不依赖 history 继续存在。历史过期后源线程重新请求会建立当前可确认的映射，但这不等于恢复旧周期的历史身份。一个明确的旧版本边界是：如果还存在永久 `v3:thread` 证据，原有历史恢复路径仍可能使用它；180 天 retention 约束的是 `v4:thread-history` 索引，不会偷偷销毁旧 side 可能依赖的 v3 证据。

所有 identity 数据使用 Redis 前缀 `openai_codex_session_identity:`。表内 key 为此前缀后的逻辑 key；原始 session/thread 不出现在 key 名称中。

| 数据种类 | 创建与读取者 | 最终生命周期 | 续期规则 | 丢失或清理后的影响 |
| --- | --- | --- | --- | --- |
| Period Session：v3 派生的 64 字符十六进制 key | 普通 HTTP session resolver 使用 `SET NX`；普通 root/child 读取 | 当前 epoch 剩余时间 + 1 小时 grace | 读取和失败的 `SET NX` 均不续期 | 正常到期后下个周期使用新 session；提前删除会丢失当期 UUIDv7 |
| 普通 `v4:thread-current` | 当前线程请求登记；当前 session 的 child 查 parent | 与所属 Period Session 相同的固定到期时间 | 读取不续期 | 正常到期后新周期重新登记；缺少当前 parent 时 child 明确失败 |
| `v4:thread-history` | 真正使用 raw thread 的请求用 CAS 更新；新 side 创建读取 | 每个 raw thread 最新 observation 后默认 180 天 | 仅更晚的真实 observation 推进 retention；GET 不续期，stale CAS 不推进 | 无确切历史时新 side fork 失败；已有 side 的固定 fork 不受影响 |
| `v3:side-session` | 原始请求首次携带 fork 时 `SET NX`；后续按原始 session 查找 | 不设置时间 TTL，归 account/user 所有 | 无滑动 TTL | 不能单独删；否则 continuation/child 会误归普通 period |
| `v4:side-fork` | side 创建时 `SET NX` 固定来源；side 重试读取并验证来源 | 不设置时间 TTL，归所属 side 的 account/user 所有 | 不更新已固定的 fork 目标 | 不能单独删；否则同一 side 可能丢失固定的历史来源 |
| Side 的 `v4:thread-current` | side root/child 登记；side parent 引用读取 | 不设置时间 TTL，归所属 side 的 account/user 所有 | 不受普通 epoch 换代影响 | 不能单独删；否则 side parent graph 可能缺少记录 |
| `v4:side-lifecycle` | 真实 side/side child 请求用 CAS 更新 | 不设置时间 TTL，与 side graph 一同归 account/user 清理 | 仅更晚的真实请求更新 `last_observed_at_ms`，读取不更新 | 丢失生命周期统计；不能作为拆开清理其它 side key 的依据 |
| 历史 `v3:thread` | 旧实现写入；当前仅在确切历史/既有 side 投影恢复时读取 | 保留已有生命周期，不新增普通永久 thread | 查询不续期 | 旧映射的唯一证据可能丢失；无法从 raw ID 可靠还原 |
| v2 session mapping：64 字符十六进制 key | `off`/`device` 及原有 WS/compact mapper | 原有永久映射，本阶段不改 | 原有读取不续期 | 会改变非本次 HTTP session 模式的既有身份，不能批量按新 retention 删除 |

`prompt_cache_key` 是由 namespace 确定性计算的 UUID，不单独写 Redis；side cache namespace 来自稳定的 side-session key，因此 side 的 cache 无独立 TTL 或清理任务。Turn 和 device 也没有新增 Redis identity key。

项目已有 ops cron 处理 SQL 日志/指标清理，usage cleanup 使用现有 timing wheel 执行显式清理任务；它们的 retention 不代表 Codex side 生命周期。本次 history 复用 Redis 原生到期，索引维护复用已有 `AuthCacheInvalidationWorker` 循环，每分钟做有界清理，不新增 scheduler。Side 暂不按闲置天数自动回收；仅凭 history 过期不能判断 side graph 可安全删除。

Side lifecycle 保存 `session_id`、`created_at_ms` 和 `last_observed_at_ms`。创建时间取上游映射 UUIDv7 自带的生成时间，旧 side 首次接管时也能恢复该时间；活跃时间取请求进入 forwarding 时的服务端 observation，仅 side 本身或其 child 的真实请求推进。CAS 防止旧请求回退活跃时间；后台查询、metrics 和 owner 清理不推进它。这个元数据先用于生命周期管理，不自动触发按闲置天数的 side 删除。

服务端 observation 使用应用进程时间，owner fence 使用 Redis `TIME`，数据库 owner 状态使用 PostgreSQL 事务时间。上线前必须保证 App、Redis、PostgreSQL 的主机时钟通过 NTP 正常同步；严重时钟漂移可能使跨进程的 stale 判断过早或过晚。本阶段不引入额外的 generation 协议。

身份 key 的 ownership 是存储元数据，不参与 session/thread/cache ID 的计算。account owner 使用 OAuth credential namespace 的 SHA-256，user owner 优先为 `user:<authenticated ID>`，只有无法识别用户时才是 API Key scope。相同凭据 namespace 的多个账号行共享 account owner；普通 access/refresh token 更新若未改变 namespace，不代表身份被永久替换。

Redis 为 account/user 各保留一个 `openai_codex_identity_owner:<owner>` 有序集合，并用 `openai_codex_identity_kind:<kind>` 集合记录按种类统计的索引。每个成员保存包含 physical identity key、account index、user index、kind index 的 JSON tuple，分数为数据绝对过期时间或永久标记。每个 identity 的 `:ownership` hash TTL 仍跟随数据 key；即使数据和 reverse hash 已到期，tuple 中完整的归属信息仍能用于从所有对应索引移除成员。

索引容器不设置独立 TTL，以免某个容器提前消失后，其它 owner/kind 索引中的关联成员失去清理入口。请求访问、metrics 抓取和已有 worker 的每分钟维护均进行有界过期成员清理，最后一个成员移除后 Redis 自动删除空集合。索引维护不会更新数据 TTL 或 observation；即使 owner 长期没有请求，也能通过 kind 索引清理其已到期成员。

清理边界是**整个 account credential namespace 或整个下游 user**，不会单独按闲置时间删除 side-session、side-fork 或某个 side child。清理先将 owner fence 设为 `cleaning` 和 `retired`，阻止读取与写入，再按 ownership index 分批删除数据、reverse ownership、对端 owner 和 kind 索引；全部完成后只退出 `cleaning`，不会自行解除 `retired`。持久的退休标记阻止所有请求，包括因旧认证或调度缓存而到达的、时间戳更新的请求，避免已删除 owner 重新产生身份。保留的 cutoff 同时拒绝清理前已经进入 forwarding 的延迟请求。因此这是受写入隔离保护的批量清理，不声称一个可能很大的 side graph 在单条 Redis 命令中完成删除。

数据库迁移 `247_codex_identity_owner_cleanup.sql` 复用已有 auth cache invalidation outbox 和 worker。账号软/硬删除、credential namespace 真正被替换、用户删除时，在原数据库事务中写入 owner 生命周期事件；账号/用户创建、账号/用户恢复以及替换后的新 credential namespace 也会写入事件。提交后异步执行并保留既有重试与延迟第二遍机制，Redis 暂时不可用不会丢事件。worker 在数据库 advisory lock 下检查 owner 当前是否存活：无存活 owner 时退休并清理，确认存活时先恢复可能中断的清理，再显式激活 owner。删除一个重复导入的 OAuth 账号时，仍由其他存活账号使用的 identity 会保留。

部署迁移 247 时需协调升级所有运行 auth outbox worker 的实例，不能在新事件开始产生后混跑旧、新 worker。新 schema 通过 `event_type` 区分身份清理与认证缓存事件；旧二进制不识别该字段，可能把 Codex cleanup 事件当普通 auth 事件消费并丢失清理任务。本次没有旧 worker 兼容层；单实例停旧换新的部署方式满足这个边界，多实例部署应先停止旧 worker，再启用迁移后的新 worker。

如果清理中途失败后同一 namespace 被重新导入，worker 只恢复已经开始、仍处于 fence 保护下的清理，然后在数据库确认 owner 存活后显式激活；不会对仍被存活账号使用的 namespace 新启动删除。激活会保留旧 cutoff，仍拒绝退休前已经开始的请求。恢复或复用曾退休的 namespace 需要等待 outbox 完成确认与激活，较新的请求时间戳本身不能解除退休状态。

删除 API Key 不产生 user identity 清理事件：同一用户的其它 API Key 继续复用 session/side。删除 User 才能清理其 user owner。只有 API Key fallback scope、无法关联已认证 User 的记录仍可由 account owner 清理；本次不增加 API Key owner 删除触发器。

旧版本已写入的 key 没有 owner 信息。本次仅在能够确认 account/user 的 HTTP 身份请求访问时补登记，不使用 `KEYS *` 或全 Redis keyspace 扫描反查。**升级前从未再次访问的永久 key 仍不能按 owner 精确清理，也不计入新索引的监控数量。** v2 和历史 v3 key 不会因为新 history retention 被统一加 TTL。

`GET /api/v1/admin/ops/codex-identity/metrics` 在现有管理员认证链下提供 Prometheus 指标：

- `sub2api_codex_identity_events_total{operation,result}`：period/side session、current/history、side fork、缺失 parent/history、fork 冲突、CAS 竞争、存储错误、owner 登记与清理等固定类别的计数。
- `sub2api_codex_identity_indexed_keys{kind}`：各 kind 索引中尚未到期的 key 数量，包括 period、普通/side current、history、side session/fork/lifecycle 和已接管的 legacy key。
- `sub2api_codex_identity_key_counts_available` 与 `sub2api_codex_identity_key_counts_updated_at_seconds`：判断最近一次抓取是否成功、最后成功时间；Redis 故障时不会将未知数量伪装成 0。

数量抓取只查询固定 kind 索引，不扫描 Redis keyspace，也不刷新任何身份活跃时间。数量表示索引中未到期的成员，不会逐个探测数据 key 是否被外部手工删除。事件计数为进程内计数，重启后归零；key 数量来自共享 Redis，同一 Redis 的多个实例不应相加。指标 label 与 debug 结构化日志只使用有限类别，不输出 raw session/thread、prompt 或任意 Redis/error 内容。

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
