# 准入时间边界与性能专项

专项起点 `5effa833a988b57ec977a93481be2f75450f8edd`，规格 v1.0；功能保持关闭。本报告使用受测代码和压测程序提交标识，不追写“当前最终 HEAD”。原有未跟踪文件不纳入修改。

## 时间边界修复

在未修改产品代码的起点增加确定性 PostgreSQL 回归，用 `pg_blocking_pids` 确认事务已经阻塞，再让数据库墙钟跨过 deadline 后释放锁。实际运行：

```sh
cd backend
TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^(TestPrincipalAdmissionTime|TestCredentialGatewayTime)' -count=1 -v
```

起点的 `TryAdmit` deadline、`BeginDispatch` deadline 和 heartbeat 三项失败；连接池等待取消通过。日志 `/tmp/sub2api-admission-time-performance/time-before.log`。该运行的网关子测试被前面失败用例保留的 lease 阻挡在 Redis fencing 初始化，不算网关链路失败证据。另以 `-run '^TestCredentialGatewayTime'` 独立执行，deadline/client cancel 两项均未达到 `RELEASED/NOT_SENT` 补偿断言，日志 `gateway-time-before.log`；没有据此推断发生上游实际发送。

数据库修复：在锁主体后的独立语句读取墙钟；候选锁等待后再次读取；提交预留前复核业务和票据 deadline。凭证有效性、额度冷却、dispatch deadline、heartbeat、binding 续期使用实际检查/续约时间。队列只读筛选使用逐语句推进的 `statement_timestamp()`；审计 `created_at/released_at` 和到达排序时间保持事务时间。新预留显式写入实际 heartbeat，reconciler 持锁后的陈旧判断也使用墙钟。没有全局替换时间函数。

HTTP 修复：在数据库 dispatch 等待前连接执行 context 和 snapshot deadline，保留协议 context 值；提交返回后同步检查取消。只有提交明确成功、且尚未进入 transport 时，才能记录 NOT_SENT；提交结果未知或 transport 已进入仍保留 UNKNOWN，占用不因超时直接释放。

修复后上述命令通过（`time-after.log`，11.240s）：包括真实 Gin handler + PG + Redis + HTTP mock 的两项，均确认零上游发送、原预留 RELEASED/NOT_SENT、三层账本归零。`go test ./internal/service -run '^TestCredentialHTTP' -count=1` 通过，包含 detached context 在 dispatch 前及提交返回瞬间取消的回归。此证据覆盖时间专项，不代表 AT 全部系统通过。

迁移影响：无 schema 变更，无既有身份/凭证改写。回滚：保持功能关闭；若已在测试部署启用，先暂停主体，排空或保留未知 lease 后回退二进制，保留全部账本及新 token。回到专项起点会重新引入时间判断缺陷，不允许作为生产启用版本。

时间语义依据：[PostgreSQL 时间函数](https://www.postgresql.org/docs/current/functions-datetime.html#FUNCTIONS-DATETIME-CURRENT) 区分事务、语句和调用时刻；连接池观察依据：[Go 连接管理](https://go.dev/doc/database/manage-connections)。文档说明不代替上述实测。

## 性能证据

待本专项测量和持续矩阵完成后补充。20ms 目标不变，旧混合结果 p95 不代表成功准入事务 p95；不把 30 秒与两分钟请求预算的差异单独归因为发生器或实现问题。

### 优化前测量 P0（30秒诊断，不是持续验收）

产品代码 `3d9a7c124`；压测程序为后续 measurement 提交所记录的版本。命令：

```sh
TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=30s SUB2API_CREDENTIAL_MATRIX_CASE=C200_I16 go test -tags=integration ./internal/repository -run '^TestCredentialAcceptanceEndurance$' -count=1 -v
```

原始日志 `profile-before-C200_I16.log`（上述专项 /tmp 目录）。PG18.4 / Go1.27.0 / 8核 / OrbStack约4GiB；一个主库，三个独立8连接pool，203个worker，同用户同主体。此次1292次全部ADMITTED，WAIT/REJECTED/ERROR无样本，不能凭零样本判断这些结果的延迟。请求200ms/2s/5s，每第51个为12s，以覆盖真实10s心跳/3s续约超时；两分钟业务和context deadline。每分钟计划突发的机制已实现，但30秒诊断没有分钟突发。实际执行和收尾43.60秒。

- ADMITTED call p95 4364.08ms；连接取得前阶段 p95 4162.08ms；事务 p95 317.31ms；取得用户锁后持锁下界 p95 14.69ms；commit p95 0.399ms。
- 用户锁 SQL p95 289.89ms；PG采样在该语句观察到13,870个tuple-lock、634个transactionid-lock backend样本。连接前阶段含Go调度和首次建连，不能单称纯连接池等待；DB.Stats独立记录三个pool累计等待各1511.69s、1518.59s、1481.95s，确认池等待占主导。
- 每次成功准入25个SQL调用；空队列也执行3条公平计算SQL（均值合计2.30ms）。候选锁SQL均值0.964ms、票据清理0.444ms、ledger检查0.977ms。候选锁没有显示为主要阻塞，不据此改锁范围；不删除逐次账本核对。
- Dispatch/Finish/Heartbeat call p95分别3530.83/3362.95/2555.19ms。上游峰值55/200，平均利用率14.17%，吞吐29.63/s，无超限、重复或残留占用。没有WAIT，故此次不能归因100ms轮询风暴。
- 最后ledger count查询的EXPLAIN实际过滤1292条RELEASED历史记录，访问57个shared块，执行2.931ms；当前索引只按principal定位再过滤state。这支持增加活跃ledger的部分索引，不能推断所有SQL都由历史扫描主导。

此次测量说明长SQL串行临界区让24个数据库连接排在同一用户行锁上，其余调用在pool等待。持锁阶段的多轮往返与历史扫描是可测成本；先保留权限/账本/候选锁检查，缩减无队列时的计算与写入往返，再用相同发生器对照。不能把持锁14.69ms写成事务p95达标。

### P1：活跃 ledger 索引

新增前向迁移258，仅索引 `state <> RELEASED` 的principal；对账SQL/状态集合/容量不变。与P0相同30秒C200/I16命令（另加时间/借用回归过滤）运行通过，日志 `profile-index-C200_I16.log`。准入call p95 4658.77ms、事务p95 303.84ms、ledger SQL均值0.897ms；吞吐29.41/s。**未观察到调用尾延迟改善**，不能把索引当作池排队问题的解决。索引用于避免终结历史继续扩大扫描，EXPLAIN与持续矩阵另存。

迁移在声明的停机窗口执行，普通CREATE INDEX会暂时阻挡该表写入；不在活跃大表上承诺无锁在线迁移。回滚保留加法索引即可，不删除lease，不改变准入事实源。

### P2：无竞争票据时跳过公平计算

锁住主体后，在原账本核对语句中读取是否有其他QUEUED票据；没有竞争者时省去三条公平计算SQL，有票据仍执行原算法。没有按deadline/ready_at提前过滤该存在性判断，避免事务内新就绪票据被错误跳过。保留权限、实例硬上限、会话、完整ledger检查及候选实例行锁。

`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredential(AdmissionCompeting|Queue|LedgerRandom)' -count=1 -v` 通过，日志 `queue-fastpath.log`。新增排队公平回归确认：容量释放后，新来者不能抢在已有就绪票据之前；原无队头阻塞/TTL/随机账本测试通过。无schema或配置变更，回滚仅退回该优化，账本和绑定保留。

### P3：合并已锁定容量行的写入

把准入/释放各五条独立UPDATE合并为一条数据修改CTE。所有容量行仍先按原顺序加锁，每张表只更新一次，CTE不读取兄弟CTE的写入；权限、上限、租约和审计仍在原事务内。新增故障trigger分别使准入和释放中途写入失败，验证全部计数/lease/logical request回滚，解除故障后可继续且重复取消不重复减槽。无新增配置、无调度策略变化、无schema变更。

实际命令：`TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=30s SUB2API_CREDENTIAL_MATRIX_CASE=C200_I16 go test -tags=integration ./internal/repository -run '^(TestCredential(Admission|Gateway|UsageCrash|Queue|LedgerRandom|AcceptanceEndurance)|TestPrincipalAdmission)' -count=1 -v`。日志 `profile-batch-C200_I16.log`。准入/释放故障回滚、网关全局用户容量及旧路径、未发送补偿、同ID恢复、六个usage窗口、队列和随机账本、时间边界全部通过；独立旧冒烟测试因未设置其环境变量SKIP。**持续发生器的30秒诊断子测试失败：一次heartbeat在3秒内未取得连接，返回ADMISSION_STORE_UNAVAILABLE**；该次HTTP已完成，所有lease最终释放，无上游超限或重复。不能写成整条命令通过。

1620次准入均ADMITTED，每次SQL由25降到18，Finish由12降到8。准入call p95 3401.62ms（P0 4364.08ms），事务p95 222.78ms（317.31ms），持锁下界p95 10.66ms（14.69ms）；吞吐37.71/s（29.63/s），平均mock利用率17.99%（14.17%）。这些单次短运行只支持有限改善，**不满足20ms，也未解决生命周期连接等待尾部超时**。保留心跳3秒失败及安全未知处理，不提高其预算来掩盖问题。六组合持续验证将把该风险继续作为失败项记录。

回滚退回对应二进制优化，保留258索引及完整账本；不得用回滚清除未知lease或恢复旧token。数据修改CTE语义依据 [PostgreSQL WITH 文档](https://www.postgresql.org/docs/current/queries-with.html#QUERIES-WITH-MODIFYING)，一致性结论依赖上述实际故障测试。

### Dispatch 提交边界与31秒心跳锁等待

补充真实handler测试：BeginDispatch已经提交但模拟丢失提交应答时，零HTTP发送仍保守进入ORPHANED/UNKNOWN，PG与Redis占用各保留1；不冒充已知未发送。提交明确返回成功、执行context已到期且尚未进入transport时，则凭本地确定证据释放为NOT_SENT，PG和Redis同时归零。二者没有混为一类。

`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^(TestCredentialGatewayTime|TestPrincipalAdmissionTime)' -count=1 -v` 通过（`long-wait-and-commit.log`，42.378s）。心跳回归实际持锁等待31秒，跨过30秒陈旧阈值，提交后的heartbeat仍不早于释放锁的时刻；这里只在仓储测试提供长context，产品执行器3秒心跳预算不变。提交丢应答由装饰器注入，未声称真实网络丢包。

### 持续矩阵的固定口径

发生器记录ADMITTED/WAIT/REJECTED/ERROR各自call/transaction/连接取得前阶段/SQL阶段/commit分布；零样本明确count=0。连接池超时尚未进入driver.BeginTx时记`connection_acquire_failed`；SQL阶段时间包含往返和执行，不能等同纯锁等待。`lock_held_lower_bound`从用户锁SQL返回算起，仅是持锁下界，不替代事务时间。DB活动每50ms采样，以backend样本计数展示wait_event，不换算为精确锁等待毫秒。

真实HTTP mock记录开始/结束、峰值和累计service时间，报告吞吐和平均容量利用率。`arrival_to_admit`包含首次准入；`first_wait_to_admit`只统计曾WAIT的请求，自首次WAIT响应（票据已提交）至获准，不包括首次准入调用。minute barrier释放独立有限批次，`burst_arrival_jitter`单独统计计划到达到实际调用的偏差。业务和context均2分钟；心跳10秒/3秒，Finish使用与执行器一致的5秒预算。短P0/P1/P3诊断的Finish原为30秒，因此它们的成功结果不能证明5秒Finish预算已满足；最终持续矩阵不扩大预算。

`TESTCONTAINERS_RYUK_DISABLED=true CI=true SUB2API_CREDENTIAL_ENDURANCE=2s SUB2API_CREDENTIAL_MATRIX_CASE=C10_I3 go test -race -tags=integration ./internal/repository -run '^(TestCredential(GatewayTime|AdmissionCapacityWrite|AdmissionCompeting|AcceptanceEndurance))' -count=1 -v` 通过（`race-targeted.log`，17.554s）。仅证明这些回归和测量包装器未报告数据竞争；race下2秒样本不参与性能对照，分钟突发未在此运行。
