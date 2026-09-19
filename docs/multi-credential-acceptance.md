# 多凭证 HTTP 验收账本（收尾状态）

本账本逐项记录 AT-01～AT-40。组件测试通过不等于整项系统验收通过。

验收分支：`feat/multi-credential-http`。
报告版本 `at40-arbitration-r1`；最新AT-40专项产品快照 `1536849f2e714e265b584528fb51be3bf03ea094`（[专项证据](credential-acceptance/at40-arbitration.md)）；此前最新收尾对照 `924819c5d04a550801fb4e85441059389dc00caa`，冻结候选 `b06789dde6ddb7c25fc29ffb7ae81ed79cf6eef8`（实际命令见[本轮收尾](credential-acceptance/release-closure-924819.md)）；历史本地隔离专项起点 `44f88b3d55c170205b1e6d120895f8691132bb3a`，最后验收范围为仅本机。历史完整suite受测代码 `8c1bfcbff45883c730e37f6382c4c9fd70344122`；专项起点 `5effa833a988b57ec977a93481be2f75450f8edd`。历史E2持续矩阵的 `tested_code_sha` / `benchmark_code_sha` 为 `c594d92e3d6580cb9069a7c788deec1380eb22ea`；队列推进E3为 `7ba4602131b11e51a9647f2455d650ff71e7b5e8`，与报告提交分开，见 [证据清单](credential-acceptance/evidence-manifest.json)。
规格基线：`9bdb388b83f05e678e83990d9b19afbc3f088a8f`；代码对照基线：`fde7e8ec4ff9af1b2645661d6a28cec19f6b347f`。

状态定义：PASS表示本项在声明拓扑和测试契约内完整通过，不能外推为全系统PASS；PARTIAL表示已有局部证据但整项仍有缺口；BLOCKED表示缺少外部契约或前置条件；NOT_RUN表示无实际执行证据。历史证据与本轮复跑用SHA/命令区分；保留历史PASS不意味着本轮重跑了全部AT。

声明支持目标：单PostgreSQL主库、共享单Redis、至少三个同机受管网关进程；本专项持续压测是同进程三个独立SQL pool，三真实进程/fencing有其他局部证据，普通cmd/server三网关离线闭环及PG/Redis持久重启已在最终产品快照通过，测试fixture35aff321d单独复验，见[报告](credential-acceptance/full-gateway-rollout.md)。HA、跨地域双活、Redis Cluster、只读副本准入不支持，不把这些拓扑无条件追加为首版任务。

缺口分类：**I 本期内部正确性**（需补测/修复或完整支持范围演练）；**E 外部provider契约**（继续BLOCKED，mock不能替代）；**T 未支持拓扑/作用域**（明确禁止部署，不作为首版追加实现任务）；**M 设计允许人工处置**（验收UNKNOWN、证据保留和安全核对，而非承诺自动重建丢失事实）。一项可含多类；分类本身不把PARTIAL改成PASS。

| AT | 状态 | 实际证据与命令 | 未完成或限制 | 缺口类型 |
|---|---|---|---|---|
| AT-01 | BLOCKED | mock verifier 三实例测试通过：`TestCredentialImportControlAT01To04` | 真实 provider account+user verifier 不存在；真实导入保持 UNVERIFIED | E |
| AT-02 | PARTIAL | 重复 token/family 的 PostgreSQL 查重通过 | 无真实 refresh-family 契约 | E |
| AT-03 | PARTIAL | mock 不同 user 不合并通过；三用户相同 session binding 通过 | 无真实 provider 身份证明 | E |
| AT-04 | PARTIAL | 部分失败、操作重放、异 payload 均无部分激活 | 仅 mock provider | E |
| AT-05 | PARTIAL | `TestCredentialRefreshFamilyVersionIdentityAT05AT28AT29`：refresh 后 profile/generation 不变 | 真实 OAuth 轮换未验证 | E |
| AT-06 | PASS | 最终三完整gateway重启前后profile及活跃binding全行相同；配置CAS/快照检查通过 | 范围为声明单机拓扑，非HA/跨地域 | T |
| AT-07 | PARTIAL | replacement integration：新实例/新 generation，旧 profile 不变 | 完整 UI replace 流程未验收 | I |
| AT-08 | PARTIAL | 三独立测试进程抢最后槽位仅一lease；完整三gateway离线切换另已通过 | 最后槽位竞争本身仍为store进程，不能合并称完整三handler同时竞争验收 | I/T |
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
| AT-22 | PASS | E3有限提示/三进程暂停owner；本轮A锁保持时健康B独立准入、offered/fresh有界优先与取消回收通过 | 本地访问turn界限不等于任意外部锁下的墙钟保证；吞吐另列 | I |
| AT-23 | PASS | 取消/准入竞态、owner/提示故障回归；本轮真实handler未登记/已登记3请求15秒突发、late ACK/lost ACK和5阶段取消通过；grant/cancel race1000次通过 | 真实跨地域故障不在支持范围 | I/T |
| AT-24 | PASS | 相同 request/owner 恢复 RESERVED，不生成第二 lease；handler lost-commit 测试通过 | 真实网络 commit response 丢失注入仍有限 | —（限制见前列） |
| AT-25 | PASS | 双 release、旧 owner/epoch、旧 nonce 均幂等/拒绝 | 极端时钟偏移未测 | —（限制见前列） |
| AT-26 | PARTIAL | SIGKILL后ORPHANED占用不减；最终E5六组Heartbeat/Finish在原预算内、无残留；完整gateway重启保留UNKNOWN及user hold/审计 | UNKNOWN人工核对；外部告警投递未验证，HA不支持 | I/T/M |
| AT-27 | PARTIAL | partial SSE 无终结事件不释放、不重放；failure terminal 不是 success | 各 provider 流协议事件未全覆盖 | E/I/M |
| AT-28 | PARTIAL | 旧 version 401 不停用新 version；generation/version CAS 通过 | 真实 provider 401 分类未验证 | E |
| AT-29 | PARTIAL | refresh family singleflight/CAS/unknown补偿；usage六窗口验证安全结果，其中receipt前只能UNKNOWN人工核对 | 真实远端成功本地全存储失败不能自动恢复，转人工 | E/M |
| AT-30 | PARTIAL | Retry-After、共享 quota domain 主体保护、维护探测 budget 通过 | provider 归因契约未确认 | E |
| AT-31 | PARTIAL | 同幂等同内容不重复，异内容冲突；跨主体重新选择仍不重复 | 更复杂并发路由矩阵未测 | I |
| AT-32 | PASS | 六usage窗口独立handler+PG/Redis补跑通过；可靠receipt在完整gateway重启后11.014秒释放/结算，上游不重放、重复消费不重复计费 | receipt前UNKNOWN人工核对；不保证无法证明的自动重建事实 | M |
| AT-33 | PARTIAL | Redis全部丢失后grouped与旧共享用户申请fail closed；最终完整gateway验证SAVE/同实例restart保留epoch和UNKNOWN hold并恢复续期 | 持久restart不等于state loss重建；丢epoch仍阻断，Cluster/HA不支持 | T/M |
| AT-34 | PARTIAL | DB不可达拒绝、最终真实fde7旧binary fence通过；完整三gateway离线切换及单PG持久restart恢复通过 | 非HA/备份还原；正式provider迁移仍BLOCKED，不能外推任意故障 | E/T |
| AT-35 | PARTIAL | If-Match/CAS、主库权限重查、配置丢失不绕过准入测试通过 | 管理通知丢失实验未完成 | I |
| AT-36 | PARTIAL | 最终fmt/slog/zap/Gin/panic与15完整gateway日志扫描通过；四类密文PG离线轮换/提交ACK丢失恢复、稳定HMAC及权限测试通过 | 外置APM/heap/core dump未验证；首版部署scope=1，多租户不支持 | I/T |
| AT-37 | PARTIAL | 完整gateway真实Migrate保留Account ID、profile、组、倍率/extra；最终fde7历史binary fencing通过 | 真实provider verifier缺失，正式迁移BLOCKED；旧影子alias/binding人工处置 | E/M |
| AT-38 | PASS | 最终完整三gateway离线fence、两主体单独canary、receipt恢复及latest-token回滚通过；未知主体保持PAUSED并占用 | 声明的单机、可回滚单保留实例范围；多实例无证据时暂停，不自动旁路 | T/M |
| AT-39 | PASS | 三端点最终 body、RawMessage 大整数/null、重复 JSON/header 拒绝通过 | 全部嵌套 map/压缩/重复 header 金样未全量 | —（限制见前列） |
| AT-40 | PASS | a212两交错真实PG动态FAIL→最终1536849f定向PG22根9子PASS；legacy五写入入口与control共同事务、raw持久操作、三进程owner退出、ACK丢失、retired冲突/显式同op完成、history迁移回滚与key轮换均通过 | 仅受管HTTP入口、本地相同token指纹；真实provider未知关系、任意直接DB或外部执行者不在范围，默认关闭；keyless旧刷新拒绝、过期receipt不重发 | E/T |

## 历史记录与本轮证据入口

- 历史代码快照的 `go test ./... -run '^$'` 编译通过；本专项不将其冒称为最终代码全套重跑。
- `go test ./...`：历史F2受测代码 `8c1bfcbff` 实际退出1。基线 `fde7e8ec4` 与F2各自完整运行均记录 78 个失败事件、141 个父测试中断/未完成事件；F2逐测试对照见 [`full-suite-comparison-final.json`](credential-acceptance/full-suite-comparison-final.json)；原始日志 `/tmp/sub2api-acceptance-closure/head-final2-full.jsonl`，逐项复跑证据见 [`failure-comparison.md`](credential-acceptance/failure-comparison.md)。相同结果不能自动归因基线；WS/流错误代表性失败已单测复跑，仍有其他范围外失败需要单独归因。
- 历史F3受测代码 `e2a2342228c58074dca9b0d8ede5968ec3b44c6b` 实际执行 `cd backend && go test ./... -count=1 -json`，退出1，81个失败事件；与 `fde7e8ec4` 当前对照为 `FAIL_BOTH=81`、`BASELINE_ONLY=0`、`CURRENT_ONLY=0`。逐测试记录见 [`current-failure-comparison.json`](credential-acceptance/current-failure-comparison.json)，不能将两边相同改写成PASS或“全部基线问题”。
- `go test -race ./internal/service ./internal/handler ./internal/repository ./internal/handler/admin -run '^TestCredential|^TestPrincipal|^TestUpstreamPrincipal' -count=1`：历史运行通过；本专项另有race定向命令，见专项报告。
- PostgreSQL/Redis/HTTP mock 集成命令和真实 Gin handler 命令见 [`gateway.md`](credential-acceptance/gateway.md)、[`usage-recovery.md`](credential-acceptance/usage-recovery.md)。
- E0历史30秒请求预算运行有两组ownership loss，根因时间线未证明；不推断全部为发生器问题。E1已完成两分钟预算的六个并行10分钟运行，无测试观察到的超限/重复/残留，mixed call p95为0.49～9.79s（不可直接对照20ms事务目标）。它补充E0，旧“尚未重跑/两组当前失败”结论不再代表最新矩阵。
- 本专项P0/P1/P3分别测量pool和SQL阶段；P3短测虽减少SQL往返和持锁时间，但出现一次3秒heartbeat pool超时，整条命令FAIL，未掩盖。E2六组合串行10分钟的结果和SHA另见 [专项报告](credential-acceptance/admission-time-performance.md)；性能20ms目标保持。
- 历史时间正确性T2：`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^(TestCredentialGatewayTime|TestPrincipalAdmissionTime)' -count=1 -v`，PASS，实际31秒心跳锁等待及完整handler提交边界。日志 `long-wait-and-commit.log`。扩大网关/usage/容量/队列回归结果见专项P3记录，不把包含失败压测的整条命令写成PASS。
- E2最终矩阵：C10/I3 PASS（执行断言，准入p95=24.11ms）；C10/I16 PASS（23.78ms）；C50/I3 FAIL（1条DISPATCHING）；C50/I16 PASS（执行断言但967次队列超时、p95=1.62s）；C200/I3 FAIL（101条DISPATCHING、8次Heartbeat错误）；C200/I16 FAIL（90条DISPATCHING、12次Heartbeat错误）。E2命令实际退出1；这些子场景结果不能外推为系统验收PASS，完整表格和SHA见专项报告/`admission-endurance-results.json`。
- 旧 binary fence、离线迁移/canary/rollback 证据见 [`rollout.md`](credential-acceptance/rollout.md)。
- 五个扫描漏洞逐项调用路径和处置见 [`vulnerabilities.md`](credential-acceptance/vulnerabilities.md)。本轮已独立升级grpc 1.83.2/x/image 0.45.0并执行通信及图像回归；原五项扫描已消失，剩余六项仅模块级发现不等于可达漏洞；最终候选扫描另见收尾记录。

## 本轮最终结论

**代码实现状态：** 原五项依赖漏洞升级完成，SQL往返做了实测支持的最小合并，稳定密钥指纹与离线轮换、秘密panic、已登记token入口保护及完整网关运维闭环已补齐。跨主体advisory隔离、有界offered/fresh、取消名额、发送前期限检查均保留并复验。AT-40两个内部缺陷已在本轮声明HTTP范围关闭，详见专项证据；未修改WS、session/full、UA/TLS实现、既有隐私清理或原有zz_debug未跟踪文件。

**系统验收状态：PARTIAL。** 最终定向/race/Go1.27扫描通过；E5六组十分钟共114527次dispatch全部释放，无超限、重复、记录错误。完整default与unit suite仍退出1，逐项首跑与补证见[失败归因](credential-acceptance/release-failure-attribution.md)。真实身份和compact契约BLOCKED。C50/C200成功准入事务p95仍超过20ms，C10 WAIT事务也超过；C200利用率约27%–28%，不能宣称性能达标。可靠receipt恢复与无可靠事实的UNKNOWN人工处置分别验收。

**生产启用条件：** 维持本轮跨入口互斥协议及离线升级fencing、配置旧OAuth刷新所需vault key，完成剩余I类验收和失败处置，满足原20ms事务目标及吞吐要求；取得可信provider account+user和compact契约；维持完整fence清单、最新schema/key、单PG与单Redis noeviction，外部插件/APM另行审计。条件完成前保持 `gateway.multi_credential_http_enabled=false`，禁止生产启用。HA/跨地域/Cluster不受本报告支持。

本轮每类提交、实际命令/退出码/二进制摘要、23条记录和迁移261回滚说明见[收尾报告](credential-acceptance/release-closure-924819.md)、[验证清单](credential-acceptance/release-validation-results.json)。产品/发生器SHA为b06789dde，之后唯一backend变化是35aff321d的独立restart测试fixture，已实际重编复验；不使用旧测试覆盖它。


- E3公平队列/生命周期矩阵：六组合10分钟执行断言全部PASS，C50/C200无Finish/Heartbeat错误、无残留lease；但权威事务p95 25.36–34.77ms，端到端call p95最高8.12s，性能仍PARTIAL。E3结果、提示空闲样本和本地等待数据见 `queue-progress-lifecycle.md`/`admission-endurance-queue-progress-results.json`。生命周期安全问题已修复到本轮矩阵预算，不能把端到端性能声明为达标。


本地准入隔离专项口径更正：20ms来自规格21.2的权威admission事务p95首轮目标，不是任意负载下call/排队总耗时上限。E3范围内生命周期超时/残留问题已不复现；跨主体advisory重试隔离、有界公平和取消名额已局部通过，保留为本轮回归约束；当前仍需单独评价单主体吞吐及新审计的跨入口边界，不能重新列为旧生命周期饥饿未解决。


历史本地隔离专项（起点44f88b3d5）：跨主体advisory重试隔离、有界offered/fresh、取消名额/grant竞态、真实handler15秒各阶段测试通过，参见[专项报告](credential-acceptance/local-admission-fairness.md)。本机E4六组合首次退出1：五组PASS，C10/I16在并行编译/内存争用期间失败（9条DISPATCHING保留）；同一保存二进制重跑该组十分钟PASS（5537次全部释放、零错误）。保留两份记录，不能把首次矩阵改为全PASS或证明全部失败来自环境。单主体C200事务p95约39.72/40.17ms、call p95约8.21/8.51s，20ms事务目标未达到。最后指令仅本机验收，未执行原生或生产压测；生产仅做只读架构/负载检查。

## E5最终复跑（补充历史，不覆盖首次失败）

[固定六组合报告](credential-acceptance/release-endurance-E5.md)：命令退出0，3677.22秒，六组执行断言通过；ADMITTED事务p95依次18.17/18.61/23.29/25.57/21.59/22.25ms，C10 WAIT p95为21.16/21.25ms。C200 call p95约5.03/5.34秒，利用率28.00%/27.23%。保留E4初次失败及同binary补测，不以不同条件运行差额认定根因。E5期间无并行编译/其他压测。

最终完整gateway使用b067产品及35aff测试fixture，receipt恢复11.014秒、PG/Redis持久restart续期11.131秒，8次mock调用/7条usage/1条未知占用，未重放；最新token回滚、身份绑定保持和15份进程日志扫描通过。首次restart旧host-port拒绝日志保留，后续实测端口重映射后修正fixture通过；未改产品epoch/TTL。真实provider与compact仍BLOCKED。

## AT-40专项收尾（a212→1536849f）

本轮只修复两个内部凭证旁路问题，AT-40更新PASS限定在受管HTTP入口及本地token相等关系。新前向迁移262/263保留历史owner与独立UNKNOWN声明；旧账号五个写入口与受控导入/激活/刷新/迁移回滚遵守短事务仲裁；raw/manual/worker刷新有持久SENDING/UNKNOWN/SUCCEEDED和加密结果，不持网络长事务、不因退出/超时重放。三独立进程和真实HTTP mock/PG barrier、提交ACK丢失、历史别名/迟到写回/删除账号/批量冲突及密钥轮换均有实际测试。最终三完整网关和handler回归通过，E5原证据保持；本轮未改调度和性能门槛、未复跑E5。

真实provider account+user及compact继续BLOCKED；整体仍PARTIAL。扩大repository/admin/config包测试仍有两项Dashboard失败，已逐项在a212实际补跑同失败，不能计PASS。原全套其他失败和性能缺口保持。功能默认false，生产未操作；原未跟踪zz_debug未动。部署必须fence所有旧writer并配置稳定key；keyless生产旧OAuth刷新也failclosed，成功receipt到期不自动再次外呼。迁移影响、具体命令/退出码/日志及安全回滚见[AT-40报告](credential-acceptance/at40-arbitration.md)。
