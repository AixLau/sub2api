# 多凭证 HTTP 验收账本（收尾状态）

本账本逐项记录 AT-01～AT-40。组件测试通过不等于整项系统验收通过。

验收分支：`feat/multi-credential-http`。
当前最终 HEAD：`8c1bfcbff`；完整最终 suite 日志 `/tmp/sub2api-acceptance-closure/head-final2-full.jsonl`；验收收尾提交包括 `5f335e11b`、`515b23bc9`、`393655edd`、`039ef1cf2`、`85c47db9f`、`b80c9b9a0`、`53164d062`、`8434b6748`。
规格基线：`9bdb388b83f05e678e83990d9b19afbc3f088a8f`；代码对照基线：`fde7e8ec4ff9af1b2645661d6a28cec19f6b347f`。

状态定义：PASS 表示本项在声明拓扑和测试契约内完整通过；PARTIAL 表示部分系统/组件证据通过但仍有缺口；BLOCKED 表示缺少外部契约或前置条件；NOT_RUN 表示本轮未执行。

| AT | 状态 | 实际证据与命令 | 未完成或限制 |
|---|---|---|---|
| AT-01 | BLOCKED | mock verifier 三实例测试通过：`TestCredentialImportControlAT01To04` | 真实 provider account+user verifier 不存在；真实导入保持 UNVERIFIED |
| AT-02 | PARTIAL | 重复 token/family 的 PostgreSQL 查重通过 | 无真实 refresh-family 契约 |
| AT-03 | PARTIAL | mock 不同 user 不合并通过；三用户相同 session binding 通过 | 无真实 provider 身份证明 |
| AT-04 | PASS | 部分失败、操作重放、异 payload 均无部分激活 | 仅 mock provider |
| AT-05 | PASS | `TestCredentialRefreshFamilyVersionIdentityAT05AT28AT29`：refresh 后 profile/generation 不变 | 真实 OAuth 轮换未验证 |
| AT-06 | PARTIAL | profile trigger、配置 CAS 和快照版本检查通过 | 真实进程重启活跃绑定未完成 |
| AT-07 | PASS | replacement integration：新实例/新 generation，旧 profile 不变 | 完整 UI replace 流程未验收 |
| AT-08 | PASS | 三独立测试进程+独立 SQL pool 抢最后槽位，仅一 lease；SIGKILL 后 ORPHANED | 完整旧/新网关混合拓扑未演练 |
| AT-09 | PASS | 8/2/0、总额约束 PostgreSQL ledger 测试 | 长时动态负载另见 AT-10/13 |
| AT-10 | PASS | 权重水位 `5/2/5` 单测和随机 ledger 测试 | 线上需求分布未测 |
| AT-11 | PASS | C=0、hard_max=0、Redis/PG fail closed 测试 | 无生产配置验证 |
| AT-12 | PASS | 缩容 overhang 不杀旧 lease、禁止新准入 | 控制面前端影响预览不完整 |
| AT-13 | PARTIAL | config CAS、health capacity 字段和旧请求非抢占代码 | 健康探测完整接线/通知丢失未验收 |
| AT-14 | PASS | 满 binding 实例不会阻塞空闲实例新会话 | 全下游结果端到端未测 |
| AT-15 | PARTIAL | 同 session 粘性、唯一 binding、三进程竞争通过 | 同一新 session 的完整 handler 竞态未覆盖 |
| AT-16 | PASS | 三用户相同 session 字符串得到独立 binding；caller scope hash 含 user/API key | 完整权限结果隔离未测 |
| AT-17 | PASS | 活跃 lease 保护 binding TTL | tombstone 清理周期未测 |
| AT-18 | PASS | EXPIRED/世代不匹配明确拒绝，不静默迁移 | 正式管理员迁移 API 未完整 |
| AT-19 | PARTIAL | previous_response/状态无可信 session 时拒绝代码 | 所有工具续接形态未建立金样 |
| AT-20 | PASS | `TestCredentialHTTPWithPostgresLedgerAndMockUpstream` 的 Responses/透传/compact 三子测试通过；Gin+PG+Redis+mock 联测 `TestCredentialGateway*` 通过 | compact 真实契约未确认，真实 provider BLOCKED |
| AT-21 | PARTIAL | HTTP guard 拒绝无快照受控账号；WS snapshot 明确拒绝；旧二进制 fencing 通过 | 完整旧路由所有旁路未穷举 |
| AT-22 | PASS | 无队头阻塞、ticket 不占 lease、队列预算测试通过 | 跨节点可靠通知未测 |
| AT-23 | PARTIAL | 取消/准入竞态和随机序列通过 | 长时客户端断连矩阵未完成 |
| AT-24 | PASS | 相同 request/owner 恢复 RESERVED，不生成第二 lease；handler lost-commit 测试通过 | 真实网络 commit response 丢失注入仍有限 |
| AT-25 | PASS | 双 release、旧 owner/epoch、旧 nonce 均幂等/拒绝 | 极端时钟偏移未测 |
| AT-26 | PASS | 三独立进程 SIGKILL，发送后变 ORPHANED 且占用不减 | 多地域 HA 未测 |
| AT-27 | PASS | partial SSE 无终结事件不释放、不重放；failure terminal 不是 success | 各 provider 流协议事件未全覆盖 |
| AT-28 | PASS | 旧 version 401 不停用新 version；generation/version CAS 通过 | 真实 provider 401 分类未验证 |
| AT-29 | PASS | refresh family singleflight/CAS/unknown 补偿；六个 usage/settlement 崩溃窗口通过 | 真实远端成功本地全存储失败不能自动恢复，转人工 |
| AT-30 | PASS | Retry-After、共享 quota domain 主体保护、维护探测 budget 通过 | provider 归因契约未确认 |
| AT-31 | PASS | 同幂等同内容不重复，异内容冲突；跨主体重新选择仍不重复 | 更复杂并发路由矩阵未测 |
| AT-32 | PASS | usage receipt + billing outbox + audit outbox 重复消费只一次；六窗口恢复测试通过 | 全部异步消费者重启演练未完成 |
| AT-33 | PARTIAL | Redis epoch 丢失后 grouped 和旧共享用户申请均 fail closed | Redis Cluster/HA 恢复未验收；不会自动“重建放行” |
| AT-34 | PARTIAL | DB断连 fail closed；旧 linux/amd64 binary Docker fence 通过 | PostgreSQL主从切换、旧节点 fencing 的真实完整部署未完成 |
| AT-35 | PASS | If-Match/CAS、主库权限重查、配置丢失不绕过准入测试通过 | 管理通知丢失实验未完成 |
| AT-36 | PARTIAL | AES-GCM、owner隔离、秘密不回显、日志 outbox 无秘密通过 | 全量 APM/panic/debug hook 扫描、tenant 模型、密钥轮换未完成 |
| AT-37 | PARTIAL | 只读迁移预览保留旧 Account 行；旧 fde7e8ec4 binary HTTP fence 通过；离线迁移工具存在 | 真实 provider verifier 缺失，正式迁移不能启用；影子 alias/旧 binding 需人工 |
| AT-38 | PARTIAL | Compose fence、canary、rollback（保留最新 rotated token）测试通过 | 合成/单主机拓扑；完整旧网关多节点灰度未完成 |
| AT-39 | PASS | 三端点最终 body、RawMessage 大整数/null、重复 JSON/header 拒绝通过 | 全部嵌套 map/压缩/重复 header 金样未全量 |
| AT-40 | PARTIAL | 受控 Account DB trigger、HTTPUpstream guard、旧 API 保护通过 | 导入导出/插件/定时任务所有旁路逐项审计未完成 |

## 完整测试和性能证据

- `go test ./... -run '^$'`：当前最终代码全仓编译通过。
- `go test ./...`：当前最终 HEAD `8c1bfcbff` 实际退出 1。基线 `fde7e8ec4` 与当前最终 HEAD 各自完整运行均记录 78 个失败事件、141 个父测试中断/未完成事件；完整最终 HEAD 逐测试对照见 [`full-suite-comparison-final.json`](credential-acceptance/full-suite-comparison-final.json)；原始日志 `/tmp/sub2api-acceptance-closure/head-final2-full.jsonl`，逐项复跑证据见 [`failure-comparison.md`](credential-acceptance/failure-comparison.md)。相同结果不能自动归因基线；WS/流错误代表性失败已单测复跑，仍有其他范围外失败需要单独归因。
- `go test -race ./internal/service ./internal/handler ./internal/repository ./internal/handler/admin -run '^TestCredential|^TestPrincipal|^TestUpstreamPrincipal' -count=1`：通过。
- PostgreSQL/Redis/HTTP mock 集成命令和真实 Gin handler 命令见 [`gateway.md`](credential-acceptance/gateway.md)、[`usage-recovery.md`](credential-acceptance/usage-recovery.md)。
- 10 分钟 endurance：C10/I3、C10/I16 通过；C50/I16 通过；C50/I3 和 C200/I3 出现 `ADMISSION_OWNERSHIP_LOST`；C200/I16 无超限但 admission p95 约 4.34s。后续 corrected 5s smoke 使用 2 分钟 request deadline，六组未作为 10 分钟证据。结论：持续压测整体 PARTIAL，未达到 p95≤20ms。
- 旧 binary fence、离线迁移/canary/rollback 证据见 [`rollout.md`](credential-acceptance/rollout.md)。
- 五个扫描漏洞逐项调用路径和处置见 [`vulnerabilities.md`](credential-acceptance/vulnerabilities.md)。扫描结果未被忽略；grpc/x/image 未在本任务升级，生产仍受漏洞处置门槛约束。

## 三个最终结论

**代码实现状态：** 多实例模型、PostgreSQL准入/lease、硬粘性、动态借用、HTTP三端点快照、refresh CAS/unknown、全局 Redis user hold fencing、usage receipt/billing outbox、审计、迁移预览和单主机离线 fencing/rollback 已实现并有受控测试。WS、session/full、UA/TLS 与既有隐私清理未修改。另有原有未跟踪 `zz_debug_test.go` 不在提交中。

**系统验收状态：** PARTIAL。AT-01/真实 verifier、compact契约、跨HA/旧节点完整拓扑、两组10分钟高并发失败、完整全仓回归失败、5个依赖漏洞均阻断完整验收。不能宣称七个PR全部通过，也不能宣称任意网络故障下Exactly Once。

**生产启用条件：**

- provider 能提供经过验证的 account+user 身份接口，并将真实导入从 UNVERIFIED 变为 VERIFIED；
- provider 书面/可测试确认 compact 请求和响应契约；
- 升级并复验 grpc/x/image 五项漏洞，或获得明确安全豁免；
- 修复/解释 C50/I3、C200/I3 的 lease ownership loss，重新跑完整 10 分钟矩阵并达到或解释 20ms目标；
- 在声明拓扑中完成真实旧网关多节点 fencing、PG/Redis HA 和完整灰度/回滚演练；
- 完成全套失败测试逐项归因、日志/APM/权限旁路扫描、完整管理端 drain/revoke/replace/resolve 影响预览；
- 完成上述条件前保持 `gateway.multi_credential_http_enabled=false`，禁止生产启用。
