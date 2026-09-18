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
