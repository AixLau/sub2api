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
