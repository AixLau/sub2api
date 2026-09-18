# 公平队列推进与生命周期防饥饿专项

基线 `6935a7c7ecd98c7e19ee6c8e40b2943139ddca8d`，报告版本 `queue-progress-r1`。多凭证仍默认关闭；真实 provider/compact 未验证，不改变UNVERIFIED。范围是现有队列的推进协议和数据库资源分配，不增加业务端点/调度策略；保留PostgreSQL最终联合准入与用户→主体→实例→lease锁顺序。

## Q1：有界就绪提示

基线新增 `TestCredentialQueueProgressPausedOwner` 实际失败：先登记的owner停止轮询后，后续合法请求即使有容量仍无法在4秒内前进（`/tmp/sub2api-queue-progress/paused-owner-before.log`）。

迁移259增加offer_until/offer_instance_id，仅是调度提示，不增加occupied、不创建lease。保留现有需求水位、低份额优先、用户公平和到达年龄；最多8条提示，1秒owner响应窗口；过期提示延后1秒再竞争，保留原始年龄。锁主体下选择当前有权限/凭证/端点/绑定/容量的票据。owner尚需通过完整联合准入；其他节点不得接管请求体。取消/释放触发本机提示检查，其他节点100ms有限周期恢复；每节点把所有等待请求合成一次批量查询，只唤醒本节点相应owner。不存在第二个容量权威。

完整准入限制为每store一个事务；PostgreSQL非阻塞事务advisory lock按主体限制跨节点竞争，生命周期操作不等待这个锁。对提示扫描使用SKIP LOCKED，不对最终准入使用不完整容量视图。旧“每请求100ms完整准入”路径移除。

设计复用现有database/sql、lib/pq和PostgreSQL队列模式，无新依赖。[PostgreSQL SELECT](https://www.postgresql.org/docs/current/sql-select.html)说明SKIP LOCKED适用于队列消费者而不适合一般一致读；此处仅用于非权威提示。[Go连接管理](https://go.dev/doc/database/manage-connections)说明pool容量用尽后等待，故同时治理事务竞争和连接资源。

实际命令（backend）：`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredential(Queue|LifecycleResources|AdmissionCompeting|GatewayGlobal|GatewayLegacy)' -count=1 -v`。`progress-resources3.log`退出0：暂停owner约1.13秒后可前进；丢失提示、重复提示、错误owner、原硬绑定/无队头阻塞、全局用户/旧路径网关回归均通过。首次修改曾使不可执行旧票据阻塞新会话，测试失败后修复，失败日志`queue-after.log`保留。Wire及测试曾因磁盘耗尽/生成接线错误失败，不作为通过；使用`go clean -cache`回收可重建缓存后重跑。

迁移仅加提示列/索引；回滚先暂停新准入、取消等待、保留在途及未知账本，再回退协议代码，保留259。不得让运行新旧队列协议的节点混合承载grouped主体；原协议有已知推进缺陷，不能把回退当生产放行。

## Q2：生命周期保留资源

从每进程既有连接预算划出2条生命周期连接（测试为6+2，总共仍8）；启用grouped且预算小于4时拒绝启动。保留池覆盖终结receipt、Finish/Heartbeat/Cancel、全局用户补偿和reconciler账本操作。准入最多一个本地完整事务、一个主体的跨节点完整事务，避免仅拆pool而继续堆行锁。

同一命令中耗尽全部6条普通连接后，heartbeat、receipt持久化和Finish仍在原3秒/5秒预算内成功；3个独立pool节点、90个准入竞争者下，最大heartbeat449.78ms、Finish200.84ms，通过。非真实三OS网关进程，持续压力矩阵另列。

无schema变化。关闭功能时不创建保留池；关停reconciler后关闭其生命周期pool。Wire已重新生成。回滚保持总预算及未知占用，不把拆pool视为数据库锁隔离保证。

## Q3：可靠终结事实的独立恢复

真实handler复现：完整终结响应已写receipt，但Finish被数据库锁阻塞直到原5秒预算超时；前台计费随后成功，把receipt标RECORDED。旧恢复查询仅取PENDING，故恢复后仍剩2条占用（1条应释放+1条真正UNKNOWN），`recovery-before.log`失败。

修复查询同时读取“已可靠终结但lease仍未释放”的receipt，无论计费为PENDING/RECORDED/REVIEW_REQUIRED；先按原lease与证据幂等Finish，非PENDING只恢复容量，不重新结算或把未知usage记零。无可靠终结事实的UNKNOWN不进入此分支。

`GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -p 2 -tags=integration ./internal/repository -run '^TestCredential(OverloadRecovery|UsageCrashWindows)' -count=1 -v`通过（`recovery-after-isolated.log`）；原六窗口均通过。本专项专用GOCACHE用于避开共享cache文件缺失，并非产品依赖变化。

进一步 `-run '^TestCredentialOverloadRecoveryTerminalAndUnknown$'` 使用实际`ProvideCredentialReconciler`接线：停止新增压力后20.159秒内（定义预算25秒，默认10秒周期不变）完成PG释放与Redis用户槽释放。真正未知lease仍ORPHANED，占用1；上游仅调用1次，usage和扣费各1条，重复恢复幂等。日志`recovery-reconciler.log`。这是完整handler+receipt+reconciler+计费证据，与持续发生器不含receipt恢复的边界分开。

无schema变化；回滚必须先消化可靠receipt的待完成lease，保留UNKNOWN，不恢复旧数据库快照。


## Q4：提示owner优先进入本地数据库访问队列

75秒C200/I16冒烟发现：新到请求能排在已有有效提示的owner前面，耗尽其响应窗口；最初global try-lock失败还直接返回WAIT但未登记票据。现改为有界本地数据库访问排队（最多256项；生产HTTP已有更小body/请求数预算），就绪owner及批量推进优先于新登记请求；仍只有一个本地准入事务。跨节点try-lock竞争发生在本地登记阶段，释放DB连接后5ms再试，不持SQL事务睡眠，不对外返回没有票据的WAIT。最终许可完全不变。

取消尚未登记的请求为幂等no-op，无外部副作用。handler把既有15秒排队期限传入TryAdmit context，防止本地数据库访问排队超过入口预算。

`TestCredentialAdmissionTurnOffersPrecedeFreshAndCancel`验证就绪owner先于fresh及取消不遗留turn；`TestCredentialQueueProgressNoTicketlessWait`在真实PG持有跨节点锁时确认不伪造WAIT；`TestCredentialQueueProgressOffersRecheckRevocationAndCapacity`验证提示后缩容、撤销仍由权威准入拒绝。`offer-boundaries.log`通过。

75秒诊断`burst-durable-wait.log`（包含一次分钟barrier）通过：3545次执行全部RELEASED，Heartbeat/Finish无错误，Finish最大2.63秒、Heartbeat最大875ms，保留3秒/5秒预算。此次诊断运行期间还做过定向测试/编译，不作为独占硬件SLO对照。较高负载已主要排在本地数据库访问队列，单次准入call p95仍11.01秒；权威事务p95 38.51ms。**不能把等待搬到进程内后宣称20ms达标**，最终六组合须同时报告call和事务。


## 固定持续发生器与定向验证

E3继续使用原C10/50/200 × I3/16、C+3个稳态worker、每分钟额外C+3的明确barrier、200ms/2s/5s/12s混合mock、2分钟业务/context、Heartbeat10s/3s和Finish5s预算。唯有等待方式接入WaitAdmission，每节点pool从8改成6+2（总24不变），没有增加容量或延长预算。

增加计数覆盖未带请求trace的后台批量推进SQL，避免只把WAIT调用变少却漏算后台开销。`queue_sql_per_dispatch`为(WAIT SQL+queue/control SQL)/实际dispatch；完整全部SQL另列。`pre_transaction_wait`包含本地准入队列、跨节点非阻塞turn重试和connection取得，不能冒称纯pool等待；pool真实等待由各DB.Stats给出。`transaction`统计最后一次取得跨节点turn的权威事务，call包含全部此前等待与重试。SQL阶段时间包括SQL往返、执行、锁等待，pg活动样本不换算精确锁毫秒。

`idle_capacity_with_live_offer_sample_seconds`是50ms周期下有有效提示且主体/实例容量可用的样本估计；`idle_capacity_with_application_waiters_sample_seconds`还覆盖等待首次登记的本地队列。样本会有漏采/配置变化边界，不能作为精确墙钟保证；性能fixture保持权限/凭证/端点健康不变，无状态候选相同，才可把后者解释为可执行需求。暂停owner测试另给出确定性推进时限。

最终定向命令：`GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -p 2 -tags=integration ./internal/repository -run '^TestCredential(Queue|LifecycleResources|OverloadRecovery|UsageCrashWindows|Gateway|AdmissionTurn|AdmissionCapacity|AdmissionCompeting)|^TestPrincipalAdmission' -count=1 -timeout=5m -v`，通过，`focused-final.log`，74.997秒。

实际Wire生成及最小cleanup接线测试通过：`go generate ./cmd/server`；`GOCACHE=/tmp/sub2api-queue-progress/go-cache go test -p 2 ./cmd/server -run '^TestProvideCleanup_WithMinimalDependencies_NoPanic$' -count=1`。pool保留生命周期复用现有连接寿命clamp规则；`final-harness-check.log`验证实际provider预算与关闭默认，以及最终测量程序的1秒冒烟（不作为性能验收）。未执行全量测试/全量编译，沿用先前全套失败作为历史证据，不声称本次修复了范围外失败。


## 三个独立进程的owner暂停

新增真实子进程测试 `TestCredentialQueueThreeProcessesPausedOwner`：三个独立Go运行时/SQL pool经同一HTTP barrier同时开始，先全部登记WAIT；SIGSTOP最早owner，释放总额1的占用，两个存活owner约1.240秒内推进；SIGCONT后原owner仍自行执行。总上游开始3次、三个不同lease、三个隔离binding，峰值不超过1，最终无占用。其他节点没有接管暂停进程的body。

实际命令：`GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -p 2 -tags=integration ./internal/repository -run '^TestCredentialQueueThreeProcessesPausedOwner$' -count=1 -v`，PASS，日志`three-process-queue.log`。这是三独立准入测试进程，不冒称三个完整网关服务器；完整handler+receipt链路另有单进程联测。

测量条件披露：该进程测试的编译和约10秒运行与E3的C10/I3尾段有重叠，因此C10/I3不应描述为完全独占机器的性能测量。其余五组无这项并行工作，所有执行代码在E3期间保持冻结。没有为了文档更新重复整套压力测试。


## E3：固定六组合矩阵结果

固定命令：

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=10m go test -p 2 -tags=integration ./internal/repository -run '^TestCredentialAcceptanceEndurance$' -count=1 -timeout=85m -v
```

受测 `tested_code_sha=7ba4602131b11e51a9647f2455d650ff71e7b5e8`，报告/发生器同SHA；C10/I3尾段与三进程owner测试有时间重叠，仍在报告中披露。六组执行断言全部通过，最终 lease 均 `RELEASED`，无 `ERROR`、明确队列超时、重复或超准入：

| 场景 | ADMITTED call p95 | 权威事务 p95 | 本地/连接前等待 p95 | Finish p95 | Heartbeat p95 | dispatch/s | pool waits | 空间利用率 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| C10/I3 | 61.25ms | 25.36ms | 40.34ms | 21.73ms | 21.66ms | 9.32 | 0 | 89.29% |
| C10/I16 | 64.37ms | 26.78ms | 42.20ms | 22.29ms | 23.07ms | 9.32 | 0 | 89.23% |
| C50/I3 | 498.99ms | 29.06ms | 475.74ms | 26.67ms | 25.65ms | 23.21 | 0 | 44.47% |
| C50/I16 | 544.01ms | 32.69ms | 518.10ms | 30.81ms | 31.31ms | 21.41 | 0 | 41.04% |
| C200/I3 | 7098.51ms | 30.87ms | 7078.88ms | 33.61ms | 33.22ms | 47.07 | 0 | 22.55% |
| C200/I16 | 8120.32ms | 34.77ms | 8097.78ms | 45.07ms | 46.25ms | 43.50 | 0 | 20.85% |

E3 的生命周期资源问题已通过本矩阵预算：没有 E2 的 Finish/Heartbeat 超时和残留 `DISPATCHING`。但 C200 的端到端 call p95 仍由本地等待占主导，权威事务 p95 也高于20ms；不能把等待搬到有界应用队列后称为性能达标。`queue_sql_per_dispatch` 在C10/C50约31.7–40.1；C200没有WAIT调用，后台提示扫描和本地访问等待仍需单独看待。所有SQL、提示空闲样本、pool stats、等待事件和终态见 [admission-endurance-queue-progress-results.json](admission-endurance-queue-progress-results.json)。

与E2相比，E3将“有容量但owner不推进”转为确定的有界提示推进；E2 C50/C200 的Finish/Heartbeat错误和残留占用不再复现。E3并不证明任意生产压力、跨地域拓扑或真实provider行为，仍需保持功能关闭。


当前 HEAD 定向回归复核在 E3 后再次通过（`final-focused-current-head.log`，72.191s）：三进程暂停owner、五个提示边界、连接资源、receipt/reconciler恢复、usage六窗口、网关跨主体/旧路径用户上限、31秒heartbeat均PASS。该命令不包含重复的10分钟矩阵，矩阵结果仍以E3原始artifact为准。
