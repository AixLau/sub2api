# 多凭证 HTTP 实施记录

依据：用户提供 `sub2api_multi_credential_development_spec.md` v1.0，850 行，已完整阅读。
本记录区分代码已落地、实际执行的测试和未验收项目，不将组件测试等同于生产集成验收。

## 基线与范围

文档基线 `9bdb388b83f05e678e83990d9b19afbc3f088a8f`；开发起点 `fde7e8ec4`，分支 `feat/multi-credential-http`。
其间 `74af2f263` 移除自动修改 OAuth 隐私设置，`fde7e8ec4` 保留 metadata 数值及未知字段；两项保留。
已有未跟踪文件 `backend/internal/service/zz_debug_test.go` 不属本任务，不编辑、不提交。
仅 HTTP 普通 Responses、透传、显式 compact；不开发 WS、session/full 或 UA/TLS。

## 实际调用点清单

| 关注点 | 实际入口和链路 | 接入约束 |
|---|---|---|
| 管理 | routes/admin.go → admin/account_handler.go → admin_account.go 的 CreateAccount/UpdateAccount/BulkUpdateAccounts/DuplicateAccount/UpdateAccountExtra → account_repo.go | grouped 受控字段不能从旧入口改写；Account ID、组、倍率、代理、seed 不变 |
| 导入/导出 | admin/account_data.go、admin/account_codex_import.go、data_management_handler.go、crs_sync_service.go | 秘密只能写入；所有旁路必须检查实例所属 |
| 选账号 | openai_gateway_pipeline*.go → SelectAccountWithSchedulerForCapabilityAndImage → defaultOpenAIAccountScheduler.Select；openai_gateway_scheduling.go 旧选择路径 | 主体只计一次账号权重；实例只在已授权候选内选择 |
| 缓存 | scheduler_snapshot_service.go 的 ListSchedulableAccounts/GetAccount/UpdateAccountInCache、scheduler_cache.go、scheduler_outbox_repo.go | 缓存只能提示；最终准入查主库 |
| 并发 | OpenAIGatewayHandler.Responses → acquireResponsesUserSlot → gateway_helper.go；scheduler tryAcquireOpenAISelectionOrderWithBudgetDiagnostics、acquireResponsesAccountSlotForRequest → ConcurrencyService.AcquireAccountSlot/AcquireUserSlot → concurrency_cache.go | 现有用户槽位在账号选择前取得；新路径必须重排，排队不持槽；禁止重复扣 account |
| 释放 | openai_gateway_pipeline forward stage、gateway_helper.go wrapReleaseOnDone、ConcurrencyService 的 ReleaseFunc | grouped 不能在 context 取消时把未知远端执行当已结束 |
| HTTP | openai_gateway_forward.go Forward/buildUpstreamRequest；GetAccessToken → openai_token_provider.go | 身份、凭证和代理由同一快照固定；最终报文验证 |
| 透传 | openai_gateway_passthrough.go forwardOpenAIPassthrough/buildUpstreamRequestOpenAIPassthrough | 与普通 HTTP 同一容量域，无自动跨实例重放 |
| compact | content_moderation_guard.go 入口 NormalizeOpenAICompactRequestBodyForTest；Forward 端点分支；openai_compact_*；AccountTestService.testOpenAICompactConnection | 独立端点契约，未验证关闭；不注入猜测字段 |
| 身份 | openai_codex_account_identity.go、openai_codex_request_identity.go；fingerprint 的 prepareCodexFingerprintExtraForCreate/Update、resolveConvergedInstallationID | 旧 seed/override 保留；不把 Principal 当 session namespace |
| refresh | TokenRefreshService → OpenAITokenRefresher；OpenAITokenProvider → OAuthRefreshAPI.RefreshIfNeeded；admin AccountHandler.Refresh → OpenAIOAuthService.RefreshAccountToken/RefreshTokenWithClientID | 所有 grouped 刷新必须同 family 互斥、CAS、未知结果停止 |
| 探测 | AccountTestService.TestAccountConnection/RunTestBackground/testOpenAIAccountConnection/testOpenAICompactConnection；scheduled_test_service/runner；channel_monitor；openai_apikey_responses_probe.go；upstream_billing_probe*.go | 推理请求必须计入相同账本；元数据查询单独维护预算 |
| 不支持入口 | ResponsesWebSocket、ChatCompletions、Messages、Images、Embeddings、AlphaSearch、Live、后台直接 HTTPUpstream 调用 | grouped 不能走旧许可旁路；不修改协议实现 |

## 仓库冲突与安全默认

- 仓库没有 tenant 模型，管理员是部署级权限。第一版作用域固定为 1，数据库拒绝其他值；不能用组/工作区冒充 tenant，不能宣称多租户已验收。
- 全局 user cap 由 Redis 提供。按规格 15.4 保留这一权威；新增用户容量只能称主体内上限，跨存储不得声称原子。
- 当前 OAuth enrichment 包含 JWT 解析，它不构成可信主体证明。没有经确认的 provider verifier 时不激活导入。
- accounts.credentials 是 JSONB，未见认证加密；现有 AES-GCM SecretEncryptor 使用 TOTP 密钥，需评估专用密钥和 AAD，不直接把明文移进新表。
- 复用现有 database/sql、lib/pq、sqlmock、testcontainers；新控制表采用显式 SQL repository，遵循现有 outbox/idempotency 实现，不引入额外 ORM 生成层。
- PostgreSQL 短事务与固定锁顺序依据 [官方行锁文档](https://www.postgresql.org/docs/current/explicit-locking.html)；刷新未知处理遵循 [RFC 9700](https://www.rfc-editor.org/rfc/rfc9700.html) 的轮换风险边界，不据此推断特定上游行为。

## 阶段状态

### PR-01：控制模型、迁移、管理只读入口、关闭的功能开关

修改：新增 principal、instance、不可变 profile 表；状态/外键/非负上限/正有限权重约束；默认 UNVERIFIED、PAUSED、OFF。已有账号完全不迁移。
管理入口使用现有 adminAuth/audit/compliance 中间件，显式字段白名单不返回秘密或外部主体/安装标识；详情通过 repeatable-read 快照读取占用和实例。提供 overhang 与 effective hard max。
功能开关 `gateway.multi_credential_http_enabled=false`；本阶段不提供激活/写接口，不接管旧执行路径。

实际测试：
- `go generate ./cmd/server` 成功，Wire 生成文件已更新。
- `go test -tags=unit ./internal/service ./internal/handler/admin -run 'Test(PrincipalCapacityView|UpstreamPrincipalReadBoundary|RefreshSingleAccountOpenAIDoesNotModifyPrivacy|.*Fingerprint.*|ResolveConvergedInstallation.*)' -count=1 -v` 通过；管理 read boundary 随后独立运行并确认。
- `go test ./internal/config ./migrations -count=1` 通过。
- `go test ./cmd/server -run '^$'` 编译通过（没有运行 server 测试）。
- `TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestMultiCredentialControl' -count=1 -v` 两项真实 PostgreSQL 测试通过：rollback/replay 和约束。
- 初次集成测试因 Docker Hub 拉取 ryuk/redis 失败而失败，之后从镜像源拉取 Redis，禁用 ryuk 再运行；未修改产品依赖。PostgreSQL 本地 tag 的实际版本为 18.4，不能按 tag 宣称测试在 18.1 上运行。
- `git diff --check` 通过。
未验证：新执行路径、导入验证、全部 AT、实际上游、性能/HA。只读 API 尚不等同于管理前端。
迁移影响：新增表、索引、两个约束 trigger；不改写 accounts、credentials、extra、group 或计费。
回滚：关闭新功能，回退本阶段二进制，保留新增表；无 grouped 工作可创建，旧表不受影响。事务失败整体 rollback；不 DROP 审计/身份表，不用旧快照覆盖 refresh token。

### PR-02：秘密导入与受控实例创建

修改：credential import 只写接口、owner 隔离、幂等摘要、token/family HMAC 查重、专用 AES-256-GCM vault（record ID 作为 AAD）；全部验证后单事务创建 inactive Account + 实例 + 不可变 LOCAL_LOGICAL profile，默认无组权限且 routing OFF。旧 Account/group 写入通过数据库 trigger 拒绝，秘密导入 body 不入通用审计。
用户已确认没有 provider 验证契约：生产 Wire 明确传入 nil verifier，导入保持 UNVERIFIED，不能创建可执行主体。mock verifier 只在测试代码存在。
配置：`gateway.credential_vault_key` 必须由部署秘密管理提供 32 字节 hex；缺失关闭导入，不复用 TOTP 密钥，不自动生成持久密钥。

实际测试：`go test ./internal/service ./internal/handler/admin -run '^TestCredential' -count=1` 通过（当时 admin 无匹配测试，后补秘密不回显测试单独执行）；`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredentialImport' -count=1 -v` 两项通过，覆盖三实例创建、重复 token、操作重放、异 payload、不同 user、越权读取、部分失败无写入、UNVERIFIED 拒绝、旧 Account 更新拒绝。AES-GCM AAD/篡改测试通过。
未验证：真实 provider、完整前端、自动秘密过期清理、密钥轮换、跨部署/多租户（仓库无该权限模型），故 AT-36 不能宣布全部完成。生产不得启用 grouped。
迁移影响：247 新增秘密/导入/指纹/审计表和旧写入口数据库 gate；旧未分组账号不受影响。
回滚：保持主体 OFF/PAUSED；新账号无组、无明文 token、不可调度；回退代码保留 246/247 表及保护 trigger。不得解密导出到旧 Account 或恢复旧 token。

补充实际验证：`go test ./internal/service ./internal/handler/admin ./internal/config ./internal/server/middleware -run 'TestCredential|TestAudit.*|TestUpstreamPrincipal' -count=1` 通过（config/middleware 无匹配项）；错误/秘密不回显与 owner 参数来自认证上下文通过。首次 govulncheck 因自动工具链选用 1.26 而无法加载 go1.27 项目，不属于扫描通过；指定 go1.27 重试已完成，退出 3：发现现有 grpc v1.82.1 与 x/image v0.41.0 中 5 个可达漏洞（GO-2026-6443/6348/6222/5061/4961），相关模块未在本任务升级；这不属于安全扫描通过。

### PR-03：PostgreSQL 联合准入和租约

修改：248 建立逻辑请求、票据、会话绑定、三层容量、lease 与 usage event 表；锁顺序 user → principal → instance → ledger；排队无 lease；幂等内容冲突拒绝。BeginDispatch 校验 owner/epoch/generation 与授权；双释放幂等，未知执行保持 ORPHANED 占用。同 ID/owner 可恢复 RESERVED 提交结果，DISPATCHING 不可重放。
实际测试：真实 PostgreSQL + httptest mock 上游的 3 个 store 竞争最后槽位仅一条获准；8/2/0 动态借用、总额满等待、缩容不杀旧工作、未知执行保留占用、重复 release/旧 nonce、同 session 粘性、幂等冲突和发送前 API key 撤销测试通过。后补 DB 断连单元测试、恢复/零限额真实 PostgreSQL 测试均通过。
未验证：三 OS 网关进程（当前为 3 个 store 并发，不宣称三进程故障测试）、DB HA/提交网络丢包/进程 SIGKILL、全局 Redis user cap 接入、完整权限模型重查、HTTP 生产入口。当前模块尚未接管真实流量，功能保持关闭。
迁移影响：仅新增 ledger 与控制列，不回填占用。
回滚：停止新准入，保留 RESERVED/ORPHANED 账本；只有未发送预留可取消释放；不清空计数、不删除表、不恢复旧 token。尚未接管流量的当前阶段可回退代码并保留 schema。

### PR-04：硬绑定、队列、需求权重

修改：有需求水位目标；数据库事务中对可执行队列按低于目标份额、用户最近获准时间和到达时间排序；绑定实例满不会迁移；队头不具备容量/健康/权限时跳过。队列取消幂等，与已准入竞态返回 ownership 冲突由执行器处理。活跃 lease/就绪票据保护 TTL，旧状态过期明确拒绝；无状态新请求可建新 binding。
实际测试：`go test ./internal/service -run '^TestCredentialTargets' -count=1` 通过（5/2/5、1/1/1、空闲借用、零硬上限）；`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredentialQueue|^TestPrincipalAdmission' -count=1 -v` 通过，覆盖满绑定实例与空闲其他实例、无队头阻塞、取消重入、活跃 lease 保护、旧状态拒绝。
未验证：长时间多用户无饥饿性质测试、生产节点有界 body 内存、全局/用户队列条数和体积预算、目标平滑与通知丢失。当前只有主体票据上限，未声称完整队列验收。
迁移影响：249 增加调度提示字段；无占用重算。
回滚：停止新票据，取消未准入请求；已绑定工作仍按原世代处理，不能因回退算法重绑；保留 tombstone 和 ledger。

### PR-05：HTTP 快照与刷新/错误控制组件

修改：Responses handler 在旧 user 槽位前分流，新的 route query 只读取已 GROUPED 的主体；新旧账号混组拒绝。实例候选复用组/模型/Codex/channel/profit 过滤，主体只用一个代表排序。PostgreSQL 准入后非阻塞获取原 Redis user slot，失败撤销 RESERVED；不取旧 Account slot。HTTPUpstream DI 包装器拒绝未携快照的受控账号，覆盖直接 HTTP 探测旁路。普通、透传和 compact 复用现有请求构造，版本与 profile 固定；适配器内第二次发送拒绝，流式终结事件才释放。
新增 family 唯一刷新记录、版本 CAS、REFRESH_UNKNOWN 与加密结果补偿；401 按 generation/version 更新，UNKNOWN 429 保护主体，已知 quota domain 可传播阻断。刷新 provider 复用现有固定地址 OAuth client，保存 proxy 引用并使用已导入 client-id；新增管理员 refresh 入口经过 step-up。后台维护 worker 已通过同一 coordinator 处理近过期/401标记；不重放被401拒绝的原请求，旧 Account 按钮仍拒绝密文实例而不解密旁路；不调用生产 token 验证。

实际测试：三类端点末端请求的 token/安装标识/大整数、partial stream 未完成、二次发送拒绝、Retry-After、重复 JSON/header 拒绝测试通过；真实 PostgreSQL 同 family 三竞争仅一赢家，refresh 后 profile/generation 不变、旧 CAS 拒绝、未知结果保留密文与锁，旧版本401不失效新版本、共享 quota 保护测试通过。
发现并修正：现有共享 identity helper 仍在某些后续转换损失大整数；仅在 grouped HTTP 边界恢复非身份字段 RawMessage，未改变 WS/session/full。原始 profile namespace 保留，新增 provider subject 字段随 token 密文存储，导入请求不能指定验证主体。
未验证/未完成：完整 handler 端到端权限/结算验收，grouped AccountTestService 已走维护身份联合准入，其他直接HTTP旁路拒绝；手动/401/定时刷新均使用同一 coordinator 的新入口，旧按钮未做代理；完整 proxy/client-id provider mock 验证仍缺；compact 真实契约；完整前端。PR-05 目前为阶段实现，不能宣称满足合并/上线门槛。
迁移影响：250 新增 refresh/quota 证据表；未更新旧 credentials 或身份。
回滚：停止 grouped 准入，等待/核实在途，保留新 token、未知刷新及 ORPHANED 占用；不能把加密凭证导回独立旧账号旁路。

回归补充：较广 `go test -tags=unit ./internal/service ./internal/handler -run '^TestOpenAI.*(Fingerprint|HTTP|Responses|Compact)' -count=1` 失败，涉及 WS execution scope/moderation 与既有 response.failed sequence_number 断言；已建立起点 fde7e8ec4 独立 worktree 对照，三项在起点独立 worktree 均复现，日志 `/tmp/sub2api-credential-baseline-regression.log`。不能把过滤后的新测试通过替代全链回归。

### PR-06：控制版本与孤儿运维

修改：principal/instance PATCH 用 If-Match CAS；缩容返回 overhang/202；drain deadline、revoke 与 binding 失效；运行时接口从主库展示 RESERVED/DISPATCHING/RUNNING/CANCELLING/ORPHANED 和 ledger 差异。10秒 reconciler 不清空计数，已发送失联转 ORPHANED，RESERVED 通过同锁顺序释放；告警写结构化日志。人工 resolve 使用 step-up + confirm + evidence/reason，审计记录接受风险字段。准入发现 ledger/计数差异立即拒绝。
实际测试：真实 PostgreSQL `TestCredentialOperationsOrphansCASDrainAT26AT35AT38` 通过：失联不释放、运行视图、缩容 overhang、过期版本拒绝、无确认不能解孤儿、带证据处理、实例 drain。Wire 初次 cleanup 函数签名错误已修复并重新生成成功。
未验证：Prometheus 完整指标/告警联动、完整停机排空、权限热变更的完整策略矩阵；审计/计费重复消费与三测试进程 SIGKILL 已补验证。运维 API 不是完整前端。
迁移影响：使用已有 ledger，无新秘密回填。
回滚：暂停主体、取消未准入票据，保留在途/孤儿和新刷新结果；必须人工证据处理未知容量，不能把计数设零。控制 API 回退不删除记录。

### PR-07：迁移预览、安全回滚与验收账本

修改：只读迁移预览按凭证源识别影子别名，未知主体只出人工清单；不会把旧账号并发相加。安全回滚先 PAUSED，存在活跃 binding/lease/未知 refresh 时保持 GROUPED，不恢复旧槽位；只读 shadow 数据入口不创建 lease、不刷新、不发请求。新增管理预览页（只写 import、主体/实例状态、容量版本调整）、中英文与 API 契约测试。
实际测试：`TestCredentialMigrationPreviewAndRollbackAT37AT38` 验证 Account 完整行不变、ORPHANED 回滚保留占用；新增 `TestPrincipalAdmissionConcurrentDistinctUsersAndSession` 真实 PostgreSQL 三用户相同 session 字符串得到三个隔离绑定；新增测试整组通过，Go race 通过；前端 typecheck/lint 和 API+i18n 5项通过。完整结果与未验证项见 `docs/multi-credential-acceptance.md`。
未完成：正式旧账号迁移执行、旧进程能力 fencing、灰度激活工具、完整 shadow 对比事件、所有 AT/压测/故障演练、完整管理操作页面。生产 verifier 根据用户确认缺失，保留 UNVERIFIED，禁止上线；本 PR 不是“全部验收通过”的发布提交。
迁移影响：新接口不自动迁移，不触碰旧 credentials、seed、权限、计费。
回滚：PATCH PAUSED → 等待/证据处理在途 → 归档 ledger/binding → 单实例旧路径额度验证 → 切换；目前工具只落实前两步安全暂停，不自动执行未验证的旧路径恢复。保留密文新版 token 和 profile，禁止恢复旧数据库快照覆盖。

## 当前交付边界

七个阶段均有代码及独立提交，但 PR-05～PR-07 合并门槛未满足；不能把“按顺序实施”理解成规格全部完成。功能默认关闭，未部署或推送。所有剩余开发和测试以验收账本为准。

最终复查补充：251 保存 account/proxy 更新时间，BeginDispatch 拒绝快照后配置变化；凭证快照携带代理值拷贝。主库 user/group 权限在准入和发送前复核，新增撤销测试通过。后台维护使用同一 refresh coordinator，SENDING 超时转 REFRESH_UNKNOWN，未消费 import 过期清除密文。UNKNOWN usage 有持久事件；无 usage 证据不标 KNOWN=0。浏览器以 mock API 在1440×1000和390×844完成布局检查，无溢出/页面脚本错误，截图位于 `/tmp/sub2api-credential-{desktop,mobile}.png`。此检查不覆盖真实管理员认证/step-up。

补充阶段实现与验证（收敛修复）：252 新增/替换实例事务（旧实例 drain，新实例新世代；旧 profile 不动）通过；253 维护探测在同总额下单实例预算通过；254 调用者+端点幂等唯一索引，主体重选不重复执行；255 计费 outbox 复用原有计费器，重复消费只扣一次，审计 outbox 同事务投递仅一次。三独立测试进程+mock 上游 SIGKILL 测试通过：发送后死进程继续占用、无自动重放。150步随机操作账本守恒通过。新增每进程64MiB/100请求/用户10请求/单体8MiB队列预算（不替代已有更严入口限制）；跨部署全局体积限额尚未实现。

负载冒烟：本地 OrbStack 4GB、PostgreSQL实际18.4、三 worker，每组合5秒。C10/I3 p95=37.89ms，C10/I16=32.32ms，C50/I3=28.04ms，C50/I16=30.64ms，C200/I3=30.28ms，C200/I16=30.83ms；无测试发现的漏释放。**未达到20ms目标，未运行每组10分钟矩阵，不可宣称性能验收通过。** 日志 `/tmp/sub2api-credential-load-smoke.log`。数据库在Docker VM、SQL往返多且主体串行锁，需进一步按事务阶段剖析，不据此改变权威账本选型。
