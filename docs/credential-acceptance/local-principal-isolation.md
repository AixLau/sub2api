# 本地主体隔离回归（2026-09-18）

基线：`44f88b3d55c170205b1e6d120895f8691132bb3a`。本记录只覆盖本地 gate 的跨主体隔离及有界登记，不代表六组合持续矩阵或生产拓扑已验收。最终受测代码 SHA 由专项 evidence manifest 固定，本文件不追写“当前 HEAD”。

## 已复现与修复

静态路径：一个 store 的 `TryAdmit` 持有节点 gate，并在 `beginAdmissionTurn` 内不断回滚、等待、重试主体 A 的 advisory lock；主体 B 即使无锁竞争也无法进入。

确定性回归使用另一条 PostgreSQL 连接持有 A 的 advisory lock，通过测试驱动记录至少两次 A 的实际 rollback 后再提交 B。在整个 B 调用期间都不释放 A 的锁。基线 B 的 750ms context 到期，测试实际失败；修复后同一测试 B 约 315.58ms 成功。该时间是本次环境结果，不是对任意负载的延迟承诺。

修改将同主体排队与节点数据库预算分开：每个主体只允许一个本地调用进入跨节点竞争；跨节点 try-lock 失败时先回滚事务并让出节点 gate，再保持原主体队列和调用优先级重试。节点仍最多一个新准入或队列推进事务，生命周期保留池、总连接预算及原 PostgreSQL 权威检查均未改变。队列 pump 每个主体分别申请节点 turn，不能持有一个 turn 处理整批主体。

主体登记表全局最多 257 个有效调用，保持原来一个运行者加 256 个等待者的边界；不会按主体乘上等待容量。取消者的计数可以在其清理 goroutine 尚未运行时释放，但其主体队列引用直到调用离开才回收，避免同一主体同时出现两条本地串行队列。最后一个调用离开后删除主体条目。

本地队列满返回 `REJECTED / ADMISSION_LOCAL_QUEUE_FULL`，不再冒充 PostgreSQL 不可用。等待和短期 offer 都不增加执行占用、创建 lease 或授予发送权限。

## 实际命令与证据

运行目录均为 `backend/`，日志目录为 `/tmp/sub2api-local-admission-isolation/`。

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
  go test -p 2 -tags=integration ./internal/repository \
  -run '^TestCredentialPrincipalTurnBlockedPrincipalDoesNotBlockHealthyPrincipal$' -count=1 -v
```

基线退出 1，`baseline-isolation.log` 明确记录 B `context deadline exceeded`。基线测试增加了新的测试文件及错误 sentinel 声明，未应用隔离实现。

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
  go test -p 2 -tags=integration ./internal/repository \
  -run '^(TestCredentialPrincipalTurn|TestCredentialQueueProgress|TestCredentialLifecycleResources|TestPrincipalAdmissionThreeNodesLastSlot)' \
  -count=1 -v
```

修复后退出 0，日志 `fixed-isolation.log`，包测试耗时约 23.16 秒。包括跨主体锁竞争、全局登记上限、登记清理、原 E3 暂停 owner／通知丢失重复／权限重查／无票据 WAIT 回归，以及最后一槽并发账本约束。生命周期测试的三节点 90 个竞争者下，最长 Heartbeat 330.76ms、Finish 41.40ms，仍使用原预算。

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache \
  go test -p 2 ./internal/repository -run '^TestCredentialPrincipalTurns' -count=20 -v
```

退出 0，日志 `registry-unit.log`。20 次重复覆盖总登记上限、取消与条目回收，以及取消已生效但 release defer 尚未执行时不会继续占用有效登记名额。

```sh
GOCACHE=/tmp/sub2api-queue-progress/go-cache \
  go test -race -p 2 ./internal/repository -run '^TestCredentialPrincipalTurns' -count=20 -timeout=60s -v
```

退出 0，日志 `registry-race.log`，包测试耗时约 1.27 秒，无 race 报告。

## 验收边界、迁移与回滚

本次已验证隔离与容量约束，没有证明按主体分队列能提升单主体 C200 吞吐。节点新准入预算仍为一个事务，事务平均成本、跨节点 try-lock 失败、pump 成本和持续吞吐由独立性能数据报告。20ms 对应原规格的 admission 事务 p95，不能套用于总排队时间。

当前本地环境为 arm64 Docker host；原生 Linux/amd64、明确独占资源及声明拓扑的六组合持续复测不属于本文件的已通过证据。真实 provider 身份与 compact 契约没有新增验证，功能仍默认关闭。未运行全量测试、部署或正式迁移。

该修复不新增或修改数据库迁移。回滚代码时先停止接收该功能的新请求并让可证明终结的工作按原链路处理，再重启节点；本地队列会随进程退出，持久化票据与所有 lease 仍由 PostgreSQL 记录。不得通过删除未知 lease、清空占用或恢复旧快照来回滚。回退该提交会重新引入本地跨主体阻塞，因此保持功能关闭。
