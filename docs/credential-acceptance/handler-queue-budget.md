# Handler 15 秒准入预算专项

`report_revision=handler-queue-budget-r1`。任务基线为 `44f88b3d55c170205b1e6d120895f8691132bb3a`。本记录验证实际 Gin 入口、认证、路由、PostgreSQL 账本、Redis 用户容量和 HTTP mock 的有限突发及取消边界；不以 E3 的两分钟发生器 deadline 代替入口原有的 15 秒排队预算。

## 静态发现与实际复现

基线 handler 在 `TryAdmit` 失败后用原请求 context 恢复 `RESERVED`，没有重新检查排队期限；正常返回 `ADMITTED` 也没有检查确认是否已晚于期限。未登记等待在 admission context 到期后返回存储不可用，未统一清理自己的排队状态。

在 handler 产品代码尚未修改时实际执行：

```sh
cd backend
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
go test -p 2 -tags=integration ./internal/repository \
  -run '^TestCredentialGatewayQueueBudget(UnregisteredBurst|LateAdmissionAcknowledgement|CancellationPhases)$' \
  -count=1 -timeout=3m -v
```

退出码 1，日志 `/tmp/sub2api-local-admission/handler/before.log`。结果：

- 未登记的 3 请求突发：持有真实 PostgreSQL 主体 advisory lock，尚无 ticket 或 lease；约 15 秒后错误码为 `ADMISSION_STORE_UNAVAILABLE`，而非排队超时。
- 准入已提交但正常确认被延后至 15 秒期限：handler 向 mock 上游发送并返回 200，测试失败。
- 准入已提交且丢失确认被延后至期限：handler 恢复同一 `RESERVED` 后仍向 mock 发送并返回 200，测试失败。
- 未登记客户端取消：没有调用统一取消补偿；该场景尚无数据库占用，不能据此宣称执行槽泄漏。已登记、已获 offer、已获准取消三个场景原本通过。

延迟确认由测试 store wrapper 在真实准入事务提交后实施，等待 handler 实际传入的 15 秒 context 结束；没有改变产品期限，也没有伪造数据库准入。正常 HTTP mock 返回包含 usage 的终结响应，保留既有计费链路。

## 最小修改

仅修改 `backend/internal/handler/credential_http_handler.go`：

- `TryAdmit` 正常返回和同 lease 恢复后重新检查原排队期限与客户端取消；到期后不能继续取得全局用户槽或发送。
- 对已确认属于当前 owner 的 `RESERVED` 使用原有 3 秒独立补偿预算，撤销为 `RELEASED/NOT_SENT`；对尚未准入的请求取消自己的 `QUEUED`。客户端 context 已取消时仍允许有界查询同一预留，以完成未发送补偿。
- 无法恢复成自己 `RESERVED` 的未知执行不因此释放，也不跨实例重放。数据库仍决定 Cancel 的真实状态转移。
- 本地队列满使用独立 `ADMISSION_LOCAL_QUEUE_FULL` 和 HTTP 503，取消该请求可能已有的 ticket；不归类为数据库不可用。

新增测试文件 `credential_gateway_queue_deadline_integration_test.go`。有限突发为同一用户的 3 个请求，位于既有每用户 10 个等待请求和每进程 100 个请求的内存预算内。未登记与已登记两个突发均实际等待完整 15 秒；没有通过缩短计时或把 client context 设为两分钟来替代入口预算。offer/准入确认暂停是确定性的阶段屏障，不是生产吞吐测量。

## 修复后实际验证

```sh
cd backend
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
go test -p 2 -tags=integration ./internal/repository \
  -run '^TestCredentialGateway(QueueBudget|LostAdmissionResponseRecoversSameLease|BeforeDispatchCompensation)' \
  -count=1 -timeout=3m -v
```

实际退出码 0，76.194 秒，日志 `/tmp/sub2api-local-admission/handler/after-stable.log`。本轮与其他准入修复共用开发工作树和测试宿主，不作为独占环境性能数据；测试未修改实际 15 秒 handler 期限，3 秒补偿、Heartbeat 或 Finish 预算均未延长。

| 阶段/触发 | 请求数 | 实际结果 | 耗时口径 |
|---|---:|---|---|
| 未登记，主体 advisory lock 持有至请求结束 | 3 | 全部 503 `ADMISSION_QUEUE_TIMEOUT`；没有 ticket、lease、上游调用或计费 | HTTP 完整调用 15.530–15.619 秒 |
| 已登记，主体唯一槽位被测试预留持有 | 3 | 全部 503 `ADMISSION_QUEUE_TIMEOUT`；自己的 ticket 全部退出 QUEUED，不新增 lease | HTTP 完整调用 15.133–15.150 秒 |
| 已准入，成功确认到期限后才返回 | 1 | 503 排队超时，原 lease `RELEASED/NOT_SENT`，上游、receipt、计费均为零 | HTTP 完整调用 15.145 秒 |
| 已准入，丢失确认到期限后才返回 | 1 | 恢复同一 RESERVED 后只做取消，结果同上 | HTTP 完整调用 15.149 秒 |
| 客户端取消：未登记/已登记/offered/已准入/已准入且丢确认 | 各 1 | 自己的 ticket 无 QUEUED、lease 无活跃占用，上游均为零 | 从取消到补偿完成分别 9.472 / 3.168 / 3.184 / 5.507 / 6.085ms |
| 首次登记/已获 offer 后遇到本地队列满 | 各 1 | 503 `ADMISSION_LOCAL_QUEUE_FULL`，无存储故障误报，已有 ticket 被取消 | 功能验收，不用于时延目标 |

HTTP 完整耗时包含认证、路由准备、原 15 秒排队和本地补偿，不是 admission 事务耗时。保留的正常回归 `TestCredentialGatewayLostAdmissionResponseRecoversSameLease` 与 `TestCredentialGatewayBeforeDispatchCompensation` 同次通过；未到期限的确认丢失仍只恢复同一 lease，发送前失败仍可安全补偿。

初次修复后运行 `/tmp/sub2api-local-admission/handler/after.log` 未进入用例：并行编辑队列文件时 vet 无法解析刚加入的 `slices` 导入，退出 1。保留这一构建失败记录；待源码稳定后重新运行上述命令通过，没有关闭 vet、清缓存或修改期限。

为避免报告追写“当前 HEAD”，本记录固定受测文件内容与日志 SHA-256；最终合并报告可把相同文件内容映射到实际提交，不需要因文档提交重复测试。

| 产物 | SHA-256 |
|---|---|
| `backend/internal/handler/credential_http_handler.go` | `3ac73706866441fae0fbcb1f7eb8966dd8de413f70505dc70cb77ec712992616` |
| `backend/internal/repository/credential_gateway_queue_deadline_integration_test.go` | `58cea93d69628fa9b7884e95d7278c0f9efa7a4f2abbff80d9e2e16dd699d8b6` |
| `before.log` | `942a4f49f3010ba2e07987c359f7248dd242c463f6ac06ac3375c642604d8d02` |
| `after.log`（构建失败） | `e0d7345a7b9114c4ae01079931ccd8ce6cf0b12b0aa684104b65862ec38114e1` |
| `after-stable.log` | `5a903db36be62f4307cf88970812b8d57cef8103c9aebac783dd54b78ee18e4a` |

## 验证边界与回滚

这组测试不能证明 C200 下完整网关没有排队超时，也不证明所有故障下零残留；它明确验收各阶段在固定入口预算和客户端取消后的状态。E3 两分钟发生器和本组真实 handler 证据必须分别报告。20ms 原目标对应权威 admission 事务 p95，不能转写为这 15 秒排队期限中的全部调用耗时目标。

不包含 schema 迁移、配置项或连接预算变化；没有改 WS、`session/full`、UA/TLS 或原有未跟踪文件。回滚本提交只回退 HTTP 期限收尾逻辑和专项测试；需要先保持多凭证关闭，保留所有账本、receipt、最新 token 和 UNKNOWN 占用。回退会重新暴露已复现的确认晚于排队期限后发送问题，不可作为生产放行手段。
