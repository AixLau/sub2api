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

PR-04～PR-07 尚未交付，不宣称完成。
