# 多凭证 HTTP 验收账本（收尾状态）

本账本逐项记录 AT-01～AT-40。组件测试通过不等于整项系统验收通过。

验收分支：`feat/multi-credential-http`。
报告版本 `admission-time-performance-r1`。历史完整suite受测代码 `8c1bfcbff45883c730e37f6382c4c9fd70344122`；专项起点 `5effa833a988b57ec977a93481be2f75450f8edd`。历史E2持续矩阵的 `tested_code_sha` / `benchmark_code_sha` 为 `c594d92e3d6580cb9069a7c788deec1380eb22ea`；队列推进E3为 `7ba4602131b11e51a9647f2455d650ff71e7b5e8`，与报告提交分开，见 [证据清单](credential-acceptance/evidence-manifest.json)。
规格基线：`9bdb388b83f05e678e83990d9b19afbc3f088a8f`；代码对照基线：`fde7e8ec4ff9af1b2645661d6a28cec19f6b347f`。

状态定义：PASS表示本项在声明拓扑和测试契约内完整通过，不能外推为全系统PASS；PARTIAL表示已有局部证据但整项仍有缺口；BLOCKED表示缺少外部契约或前置条件；NOT_RUN表示无实际执行证据。历史证据与本轮复跑用SHA/命令区分；保留历史PASS不意味着本轮重跑了全部AT。

声明支持目标：单PostgreSQL主库、共享单Redis、至少三个同机受管网关进程；本专项持续压测是同进程三个独立SQL pool，三真实进程/fencing有其他局部证据，**尚未合成完整三网关发布演练**。HA、跨地域双活、Redis Cluster、只读副本准入不支持，不把这些拓扑无条件追加为首版任务。

缺口分类：**I 本期内部正确性**（需补测/修复或完整支持范围演练）；**E 外部provider契约**（继续BLOCKED，mock不能替代）；**T 未支持拓扑/作用域**（明确禁止部署，不作为首版追加实现任务）；**M 设计允许人工处置**（验收UNKNOWN、证据保留和安全核对，而非承诺自动重建丢失事实）。一项可含多类；分类本身不把PARTIAL改成PASS。

| AT | 状态 | 实际证据与命令 | 未完成或限制 | 缺口类型 |
|---|---|---|---|---|
| AT-01 | BLOCKED | mock verifier 三实例测试通过：`TestCredentialImportControlAT01To04` | 真实 provider account+user verifier 不存在；真实导入保持 UNVERIFIED | E |
| AT-02 | PARTIAL | 重复 token/family 的 PostgreSQL 查重通过 | 无真实 refresh-family 契约 | E |
| AT-03 | PARTIAL | mock 不同 user 不合并通过；三用户相同 session binding 通过 | 无真实 provider 身份证明 | E |
| AT-04 | PARTIAL | 部分失败、操作重放、异 payload 均无部分激活 | 仅 mock provider | E |
| AT-05 | PARTIAL | `TestCredentialRefreshFamilyVersionIdentityAT05AT28AT29`：refresh 后 profile/generation 不变 | 真实 OAuth 轮换未验证 | E |
| AT-06 | PARTIAL | profile trigger、配置 CAS 和快照版本检查通过 | 真实进程重启活跃绑定未完成 | I |
| AT-07 | PARTIAL | replacement integration：新实例/新 generation，旧 profile 不变 | 完整 UI replace 流程未验收 | I |
| AT-08 | PARTIAL | 三独立测试进程+独立 SQL pool 抢最后槽位，仅一 lease；SIGKILL 后 ORPHANED | 同主机完整离线切换演练尚未串起；运行时混用未fence旧节点不支持 | I/T |
| AT-09 | PASS | 8/2/0、总额约束 PostgreSQL ledger 测试 | 长时动态负载另见 AT-10/13 | —（限制见前列） |
| AT-10 | PARTIAL | 权重水位 `5/2/5` 单测和随机 ledger 测试 | 组件结果不能证明真实队列达到5/2/5或持续公平；本轮不新增策略 | I |
| AT-11 | PASS | C=0、hard_max=0、Redis/PG fail closed 测试 | 无生产配置验证 | —（限制见前列） |
| AT-12 | PASS | 缩容 overhang 不杀旧 lease、禁止新准入 | 控制面前端影响预览不完整 | —（限制见前列） |
| AT-13 | PARTIAL | config CAS、health capacity 字段和旧请求非抢占代码 | 健康探测完整接线/通知丢失未验收 | I |
| AT-14 | PASS | 满 binding 实例不会阻塞空闲实例新会话 | 全下游结果端到端未测 | —（限制见前列） |
| AT-15 | PARTIAL | 同 session 粘性、唯一 binding、三进程竞争通过 | 同一新 session 的完整 handler 竞态未覆盖 | I |
| AT-16 | PARTIAL | 三用户相同 session 字符串得到独立 binding；caller scope hash 含 user/API key | 完整权限结果隔离未测 | I |
| AT-17 | PASS | 活跃 lease 保护 binding TTL | tombstone 清理周期未测 | —（限制见前列） |
| AT-18 | PASS | EXPIRED/世代不匹配明确拒绝，不静默迁移 | 正式管理员迁移 API 未完整 | —（限制见前列） |
| AT-19 | PARTIAL | previous_response/状态无可信 session 时拒绝代码 | 所有工具续接形态未建立金样 | I |
| AT-20 | PARTIAL | `TestCredentialHTTPWithPostgresLedgerAndMockUpstream` 的 Responses/透传/compact 三子测试通过；Gin+PG+Redis+mock 联测 `TestCredentialGateway*` 通过 | compact 真实契约未确认，真实 provider BLOCKED | E/I |
| AT-21 | PARTIAL | HTTP guard 拒绝无快照受控账号；WS snapshot 明确拒绝；旧二进制 fencing 通过 | 完整旧路由所有旁路未穷举 | I |
| AT-22 | PASS | 无队头阻塞、ticket 不占 lease、队列预算测试通过 | 跨节点可靠通知未测 | —（限制见前列） |
| AT-23 | PARTIAL | 取消/准入竞态；本轮完整handler锁等待deadline/client cancel、已确认提交后取消均零发送且NOT_SENT（专项命令T2） | 其他长时断连矩阵未完成 | I |
| AT-24 | PASS | 相同 request/owner 恢复 RESERVED，不生成第二 lease；handler lost-commit 测试通过 | 真实网络 commit response 丢失注入仍有限 | —（限制见前列） |
| AT-25 | PASS | 双 release、旧 owner/epoch、旧 nonce 均幂等/拒绝 | 极端时钟偏移未测 | —（限制见前列） |
| AT-26 | PARTIAL | 三独立进程SIGKILL后ORPHANED占用不减；本轮心跳31秒锁等待和dispatch提交未知保留占用通过 | 声明拓扑告警联动仍缺；UNKNOWN可人工核对；HA不支持 | I/T/M |
| AT-27 | PARTIAL | partial SSE 无终结事件不释放、不重放；failure terminal 不是 success | 各 provider 流协议事件未全覆盖 | E/I/M |
| AT-28 | PARTIAL | 旧 version 401 不停用新 version；generation/version CAS 通过 | 真实 provider 401 分类未验证 | E |
| AT-29 | PARTIAL | refresh family singleflight/CAS/unknown补偿；usage六窗口验证安全结果，其中receipt前只能UNKNOWN人工核对 | 真实远端成功本地全存储失败不能自动恢复，转人工 | E/M |
| AT-30 | PARTIAL | Retry-After、共享 quota domain 主体保护、维护探测 budget 通过 | provider 归因契约未确认 | E |
| AT-31 | PARTIAL | 同幂等同内容不重复，异内容冲突；跨主体重新选择仍不重复 | 更复杂并发路由矩阵未测 | I |
| AT-32 | PASS | usage receipt/billing/audit outbox重复消费只一次；有持久receipt的五个窗口可恢复，receipt前UNKNOWN不重放 | 全部异步消费者重启演练未完成 | —（限制见前列） |
| AT-33 | PARTIAL | Redis epoch 丢失后 grouped 和旧共享用户申请均 fail closed | 单Redis受控恢复闭环仍需演练；Cluster/HA不支持，epoch丢失不会自动重建放行 | I/T/M |
| AT-34 | PARTIAL | DB断连 fail closed；旧 linux/amd64 binary Docker fence 通过 | 单主库恢复/完整旧节点离线fencing仍缺闭环；自动HA和副本准入不支持 | I/T |
| AT-35 | PARTIAL | If-Match/CAS、主库权限重查、配置丢失不绕过准入测试通过 | 管理通知丢失实验未完成 | I |
| AT-36 | PARTIAL | AES-GCM、owner隔离、秘密不回显、日志 outbox 无秘密通过 | APM/panic/debug hook扫描、密钥轮换未完成；首版仅部署级scope=1，不支持多租户 | I/T |
| AT-37 | PARTIAL | 只读迁移预览保留旧 Account 行；旧 fde7e8ec4 binary HTTP fence 通过；离线迁移工具存在 | 真实 provider verifier 缺失，正式迁移不能启用；影子 alias/旧 binding 需人工 | E/I/M |
| AT-38 | PARTIAL | Compose fence、canary、rollback（保留最新 rotated token）测试通过 | 合成/单主机拓扑；完整旧网关多节点灰度未完成 | I/M |
| AT-39 | PASS | 三端点最终 body、RawMessage 大整数/null、重复 JSON/header 拒绝通过 | 全部嵌套 map/压缩/重复 header 金样未全量 | —（限制见前列） |
| AT-40 | PARTIAL | 受控 Account DB trigger、HTTPUpstream guard、旧 API 保护通过 | 导入导出/插件/定时任务所有旁路逐项审计未完成 | I |

## 完整测试和性能证据

- 历史代码快照的 `go test ./... -run '^$'` 编译通过；本专项不将其冒称为最终代码全套重跑。
- `go test ./...`：历史F2受测代码 `8c1bfcbff` 实际退出1。基线 `fde7e8ec4` 与F2各自完整运行均记录 78 个失败事件、141 个父测试中断/未完成事件；F2逐测试对照见 [`full-suite-comparison-final.json`](credential-acceptance/full-suite-comparison-final.json)；原始日志 `/tmp/sub2api-acceptance-closure/head-final2-full.jsonl`，逐项复跑证据见 [`failure-comparison.md`](credential-acceptance/failure-comparison.md)。相同结果不能自动归因基线；WS/流错误代表性失败已单测复跑，仍有其他范围外失败需要单独归因。
- 当前专项 HEAD `e2a2342228c58074dca9b0d8ede5968ec3b44c6b` 实际执行 `cd backend && go test ./... -count=1 -json`，退出1，81个失败事件；与 `fde7e8ec4` 当前对照为 `FAIL_BOTH=81`、`BASELINE_ONLY=0`、`CURRENT_ONLY=0`。逐测试记录见 [`current-failure-comparison.json`](credential-acceptance/current-failure-comparison.json)，不能将两边相同改写成PASS或“全部基线问题”。
- `go test -race ./internal/service ./internal/handler ./internal/repository ./internal/handler/admin -run '^TestCredential|^TestPrincipal|^TestUpstreamPrincipal' -count=1`：历史运行通过；本专项另有race定向命令，见专项报告。
- PostgreSQL/Redis/HTTP mock 集成命令和真实 Gin handler 命令见 [`gateway.md`](credential-acceptance/gateway.md)、[`usage-recovery.md`](credential-acceptance/usage-recovery.md)。
- E0历史30秒请求预算运行有两组ownership loss，根因时间线未证明；不推断全部为发生器问题。E1已完成两分钟预算的六个并行10分钟运行，无测试观察到的超限/重复/残留，mixed call p95为0.49～9.79s，仍超20ms。它补充E0，旧“尚未重跑/两组当前失败”结论不再代表最新矩阵。
- 本专项P0/P1/P3分别测量pool和SQL阶段；P3短测虽减少SQL往返和持锁时间，但出现一次3秒heartbeat pool超时，整条命令FAIL，未掩盖。E2六组合串行10分钟的结果和SHA另见 [专项报告](credential-acceptance/admission-time-performance.md)；性能20ms目标保持。
- 本轮时间正确性T2：`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^(TestCredentialGatewayTime|TestPrincipalAdmissionTime)' -count=1 -v`，PASS，实际31秒心跳锁等待及完整handler提交边界。日志 `long-wait-and-commit.log`。扩大网关/usage/容量/队列回归结果见专项P3记录，不把包含失败压测的整条命令写成PASS。
- E2最终矩阵：C10/I3 PASS（执行断言，准入p95=24.11ms）；C10/I16 PASS（23.78ms）；C50/I3 FAIL（1条DISPATCHING）；C50/I16 PASS（执行断言但967次队列超时、p95=1.62s）；C200/I3 FAIL（101条DISPATCHING、8次Heartbeat错误）；C200/I16 FAIL（90条DISPATCHING、12次Heartbeat错误）。E2命令实际退出1；这些子场景结果不能外推为系统验收PASS，完整表格和SHA见专项报告/`admission-endurance-results.json`。
- 旧 binary fence、离线迁移/canary/rollback 证据见 [`rollout.md`](credential-acceptance/rollout.md)。
- 五个扫描漏洞逐项调用路径和处置见 [`vulnerabilities.md`](credential-acceptance/vulnerabilities.md)。扫描结果未被忽略；grpc/x/image 未在本任务升级，生产仍受漏洞处置门槛约束。

## 三个最终结论

**代码实现状态：** 多实例模型、PostgreSQL准入/lease、硬粘性、动态借用、HTTP三端点快照、refresh CAS/unknown、全局 Redis user hold fencing、usage receipt/billing outbox、审计、迁移预览和单主机离线 fencing/rollback 已实现并有受控测试；本专项修复了锁等待后的时间语义和发送前取消边界，并加入阶段测量、258活跃lease索引、无竞争票据短路及锁内批量计数写入。E2仍暴露高并发Finish/Heartbeat连接等待和公平队列退化，尚未修复。WS、session/full、UA/TLS 与既有隐私清理未修改。另有原有未跟踪 `zz_debug_test.go` 不在提交中。

**系统验收状态：** PARTIAL。真实身份与compact契约继续BLOCKED；声明支持拓扑的完整演练、各I类缺口、准入性能和生命周期超时、全仓失败归因、安全漏洞处置仍未闭合。E0已由后续运行补充，HA/跨地域属于不支持范围。receipt前UNKNOWN人工处置是允许的安全边界，不能承诺自动恢复。不能宣称七阶段全部完成或远端Exactly Once。

**生产启用条件：**

- provider 能提供经过验证的 account+user 身份接口，并将真实导入从 UNVERIFIED 变为 VERIFIED；
- provider 书面/可测试确认 compact 请求和响应契约；
- 升级并复验 grpc/x/image 五项漏洞，或获得明确安全豁免；
- 完成20ms准入目标及生命周期续约/释放预算的性能验收；本专项发现的pool/行锁瓶颈未关闭前，不调高门槛生产启用；
- 在声明的单PG主库、单Redis、同机三网关范围内完成旧节点离线fencing、故障恢复和灰度/回滚闭环；不得以本报告启用HA/跨地域等未支持拓扑；
- 完成全套失败测试逐项归因、日志/APM/权限旁路扫描、完整管理端 drain/revoke/replace/resolve 影响预览；
- 完成上述条件前保持 `gateway.multi_credential_http_enabled=false`，禁止生产启用。


- E3公平队列/生命周期矩阵：六组合10分钟执行断言全部PASS，C50/C200无Finish/Heartbeat错误、无残留lease；但权威事务p95 25.36–34.77ms，端到端call p95最高8.12s，性能仍PARTIAL。E3结果、提示空闲样本和本地等待数据见 `queue-progress-lifecycle.md`/`admission-endurance-queue-progress-results.json`。生命周期安全问题已修复到本轮矩阵预算，不能把端到端性能声明为达标。
