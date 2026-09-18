# 准入成本受控对照

`report_revision=release-admission-cost-r1`。基线 `924819c5d04a550801fb4e85441059389dc00caa`；第一组受测代码和发生器均为 `9046c871e636aab006058eac9a8ea717b52df364` 的仅追踪文件快照，无原有 `zz_debug_test.go`。三个运行使用同一个保存二进制。详细二进制/日志摘要、命令、退出码和完整指标在 `release-admission-controlled-results.json`。本地 macOS/arm64、Docker aarch64 VM（8 vCPU/约4GiB），PostgreSQL amd64跨架构运行；不作为原生Linux性能承诺。镜像标签18.1的实际服务器报告为18.4，保留镜像ID和运行输出，不用标签替代实际版本。

未与任何编译或其他压测并行。三个store各普通6+生命周期2连接，合计24；节点准入事务预算仍为1。固定请求时长分布与2分钟业务/context期限、Heartbeat 3秒/Finish 5秒预算均保持。

## 第一组：同一二进制 A/B/A

命令：先 `GOCACHE=/tmp/sub2api-queue-progress/go-cache go test -p 2 -c -tags=integration -o /tmp/sub2api-release-closure/perf-control.test ./internal/repository`，退出0；随后通过环境 `SUB2API_CREDENTIAL_ENDURANCE=30s SUB2API_CREDENTIAL_MATRIX_CASE=C200_I16 SUB2API_CREDENTIAL_RETRY_EXPERIMENT=<policy> TESTCONTAINERS_RYUK_DISABLED=true CI=true` 执行保存二进制的 `-test.run=^TestCredentialAcceptanceEndurance$ -test.count=1 -test.timeout=3m -test.v`。

| 指标 | A1 固定5ms | B 有界抖动 | A2 固定5ms |
|---|---:|---:|---:|
| 退出码 | 0 | 0 | 0 |
| dispatch/s（含最后排空） | 41.76 | 43.63 | 41.67 |
| 上游平均容量利用率 | 19.91% | 20.82% | 19.91% |
| advisory失败次数 | 10879 | 2120 | 11027 |
| SQL总数 | 73240 | 67517 | 74073 |
| 获准事务mean(ms) | 17.81 | 16.89 | 17.64 |
| 获准事务p95(ms) | 23.14 | 21.30 | 22.84 |
| 获准call p95(ms) | 4587.89 | 6890.36 | 4557.56 |

B重试等待范围为第1次2.5–5ms、第2次5–10ms、第3次10–20ms、以后20–40ms；每次失败回滚并释放节点gate，保留主体turn和请求年龄。采用既有非阻塞advisory事务锁，通知不授予容量。三次均无超准入、重复发送、生命周期错误或残留；这只代表这三个30秒场景，不替代十分钟矩阵或真实handler预算。

**决定：不采纳B作为产品默认。** B显著减少无效锁尝试，但短测call尾延迟恶化；不能只选择SQL减少这一指标称修复。当前产品仍固定5ms，实验选择仅测试代码读取环境，非运行配置。A1/A2结果接近仍不构成长期稳定性或永久公平证明。

A1/A2中单独quota往返均mean约0.55ms，三次insert累计mean约1.49ms；用户行锁阶段mean约7.7–7.8ms（包括SQL往返，非纯锁等待）。该阶段是共享生命周期锁的成本，不能简单删锁。后台推进SQL计数为0，说明本场景等待集中在登记前本地/跨节点turn，不能把延迟归因于本次未实际运行的queue pump。

## 第二组：最小往返合并（待运行）

保持锁顺序与所有检查：共享quota EXISTS并入已持主体锁后的ledger读；LEASE_RESERVED audit写入并入同事务容量CTE，任何审计错误仍回滚所有计数和lease。最终实时deadline检查仍在写入之后、提交之前。没有增加许可、删除账本核对或修改未知占用。新增实际PostgreSQL触发器故障测试验证审计失败不能留下半批准。第二组结果待执行，不能把静态减少两次往返视为已达到20ms。

参考已核对的成熟模式：[PostgreSQL事务级锁](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)、[AWS退避与抖动说明](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)。这些资料支持机制取舍，不证明本系统性能。

无schema变更；回滚本性能变更须保持开关关闭，回退代码即可，保留所有账本、receipt、最新token和UNKNOWN/ORPHANED占用。该对照不涉及真实provider身份或compact契约。
