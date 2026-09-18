# 完整网关链路验证

命令（backend/）：

```sh
TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredentialGateway' -count=1 -v
```

实际结果：2026-09-18 UTC 三项 PASS。原始日志 `/tmp/sub2api-acceptance-closure/gateway-capacity.log`。

- `TestCredentialGatewayGlobalUserLimitAndBilling`：真实 API key 鉴权、middleware、Gin Responses handler、真实 PostgreSQL admission、Redis ConcurrencyCache、HTTP mock、现有计费器。用户 cap=1，跨两个 grouped 主体，第二请求拒绝；预留撤销，旧 account slot=0，user slot=1→0，首请求 billing outbox 已结算且余额下降。
- `TestCredentialGatewayLegacyAndGroupedShareUserCapacity`：同用户旧 API-key 路径先占位，grouped 请求拒绝并撤销预留；旧 account/user 各扣一次并释放。
- `TestCredentialGatewayBeforeDispatchCompensation`：注入 BeginDispatch 数据库失败，上游开始次数=0，ledger=0，user slot=0，账单=0。

明确边界：标准模式、禁用内容审核功能（通过真实设置仓储），不绕过鉴权/计费。进程未崩溃、请求短于现有 Redis slot TTL；这三项不能证明全局 user cap 在 Redis重启、长请求超TTL或未知远端执行时仍严格保留，相关故障另列 PARTIAL。

## 全局用户占用故障修复

258迁移关联 `request_leases.global_user_slot/global_user_acquired` 与原Redis user ZSET，普通grouped请求只获取一次；未知执行不释放用户槽位，reconciler续期直到lease可靠终结。所有共享该Redis缓存的原用户槽位申请在**多凭证开关开启时**于同一Lua中校验持久化epoch；缓存全失/续期停止导致门控过期时拒绝申请，不能新建空计数继续放行。没有修改WS协议或其handler；关闭开关时脚本保持原分支。

`TestCredentialGlobalUserOrphanAndRedisLossFailClosed` 实际通过：半流保持PG占用和Redis用户hold；同用户另一个主体拒绝；模拟成员老化后reconciler续期；隔离测试Redis的FLUSHDB后grouped和旧共享用户计数申请都拒绝。原始日志 `/tmp/sub2api-acceptance-closure/global-user-final.log`。

拓扑仍限定单Redis持久实例；Redis丢失epoch后**不能自动初始化**。必须fence所有执行者、核对在途/孤儿/旧路径未知工作后做受控恢复。关闭开关不是恢复方法。Redis Cluster/自动failover未验收。
