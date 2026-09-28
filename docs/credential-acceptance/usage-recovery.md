# Usage 与计费崩溃窗口

修复：256新增内容白名单 usage receipt，上游终结结果解析后先持久化再释放lease；receipt不含headers/token/prompt/output。后台以receipt恢复既有计费器；已定价命令仍使用255 outbox。配置/价格变化时保留REVIEW_REQUIRED，不擅自换价格。usage未观察到不能记作KNOWN=0。

命令：`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredentialUsageCrashWindowsRecoverWithoutReplay$' -count=1 -v`（backend）。六个窗口的预期安全行为在真实handler+PG+Redis+HTTP mock上通过（其中一个为UNKNOWN人工核对，五个具有持久事实的窗口可恢复）；日志 `/tmp/sub2api-acceptance-closure/usage-windows-fixed.log`。

| 窗口 | 注入方法 | 恢复事实 |
|---|---|---|
| terminal已返回、receipt提交前 | 测试装饰器panic，终止当前执行栈 | ledger保持ORPHANED/UNKNOWN，人工核对；无可用usage事实，绝不重放上游/免费当零 |
| receipt提交后、Finish前 | 测试装饰器panic | 新服务实例读取receipt，完成容量释放、结算和usage row |
| Finish提交后、定价前 | 测试装饰器panic | lease已释放，receipt仍可恢复结算 |
| priced command提交后、扣款前 | PostgreSQL trigger导致扣款事务回滚 | 原命令outbox未丢，重放只扣一次 |
| 扣款提交后、usage row前 | PostgreSQL trigger拒绝usage_logs INSERT | dedup防止第二次扣款，恢复usage row |
| usage row后、receipt ack前 | PostgreSQL trigger拒绝receipt UPDATE | 重复恢复不重复usage row/扣款 |

每项检查上游开始次数恰好1。故障通过受控事务回滚/栈中止模拟；不冒称六项均实际SIGKILL。进程SIGKILL容量测试另有证据。无法从已丢失的远端响应重建usage是外部系统边界，状态UNKNOWN需保留与核对。

迁移：仅新增256表/索引；不改原计费价格，不改旧路径。回滚：暂停grouped，处理PENDING receipt/outbox后才降级；不得删除未消费receipt，不能恢复旧数据库快照覆盖扣款。

修复过程中实际发现：鉴权缓存的Group投影没有UpdatedAt，直接比较时间戳会把所有恢复误判为配置变化；已改为对实际计费字段做摘要。重复执行的最终命令日志 `/tmp/sub2api-acceptance-closure/usage-final.log`，六窗口的安全行为及三个完整网关测试通过；不表示receipt之前丢失的usage能自动恢复。既有后台计费/通知仍在原计费器执行；恢复不修改价格规则。
