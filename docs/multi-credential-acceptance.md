# 多凭证验收账本（开发中，禁止上线）

本表不是全部验收通过报告。组件测试在本地 mock 与 PostgreSQL 上执行；没有使用生产凭证。
真实导入按用户确认保持 UNVERIFIED。默认功能关闭，无生产迁移/灰度/部署。

| AT | 当前证据 | 尚缺的完整验收 |
|---|---|---|
| 01–04 | mock verifier + PG 三实例、重复 token、不同 user、部分失败/重放通过 | 真实 verifier（用户确认暂无）、已知历史 family 导入/完整跨权限 |
| 05 | PG refresh CAS 保持安装标识/世代通过 | 进程重启/密钥轮换 |
| 06–07 | DB不可变profile、replace新实例/世代与旧身份不变测试通过 | 重启活跃绑定、完整替换UI |
| 08 | 三独立测试进程各自SQL pool竞争最后槽位+mock start/end通过 | 完整网关进程/路由全链 |
| 09–12 | 8/2/0、5/2/5、0、缩容 overhang 通过 | 长时动态负载 |
| 13 | 配置 CAS/健康容量代码 | 综合并发调整性质测试 |
| 14–15 | 满绑定与新会话、同 session 粘性通过 | 同新 session 三进程竞争 |
| 16 | PG三用户同 session 字符串产生三个独立 binding 通过 | 完整下游结果/权限隔离端到端 |
| 17–18 | 活跃 lease 保护过期、状态续接拒绝通过 | tombstone 清理/恢复窗口 |
| 19 | 有状态且无 session 准入拒绝代码 | 全种工具续接解析矩阵 |
| 20 | 三端点PG+真实HTTP mock最终token/profile/数值保真通过，维护probe共享总額通过 | 完整handler+PG+Redis+结算端到端 |
| 21 | HTTP guard 拒绝无快照受控请求、快照拒绝 WS 通过 | 所有非 HTTP dialer/插件入口覆盖 |
| 22–23 | 单票据、排队无槽、无队头阻塞、取消幂等通过 | 取消/准入并发随机序列、全局内存/用户队列预算 |
| 24 | 相同 request/owner 恢复 RESERVED 通过 | COMMIT 响应真实网络丢失注入 |
| 25 | 双释放/旧 owner 拒绝通过 | 随机旧 epoch 回包 |
| 26–27 | PG ORPHANED 不释放、半流不终结/不二次发通过 | 三测试进程SIGKILL已通过；节点时钟偏移/HA/超时矩阵未完成 |
| 28–29 | 旧版本401不失效新版本、同族互斥、CAS/未知补偿通过 | 后台/401进入 coordinator（未重放原请求），仍缺完整入口 mock；远端成功本地全存储失效恢复 |
| 30 | Retry-After、主体保护/共享 domain 阻断通过 | 多主体传播端到端、provider 归因契约 |
| 31 | 同键同内容不再执行、异内容拒绝通过 | 跨主体重选测试通过，仍缺并发主体验证 |
| 32 | 稳定lease键+定价命令outbox重复提交/消费只扣一次PG测试通过；审计投递一次通过 | 返回usage到定价命令持久化之间的进程故障仍需恢复闭环 |
| 33–34 | ledger 不依赖 Redis、DB begin 断连拒绝通过 | Redis清空、PG主从切换/旧节点 fencing 实验 |
| 35 | 主库读配置、过期 If-Match 拒绝通过 | 通知丢失真实实验 |
| 36 | AES-GCM AAD/篡改、秘密不回显、owner隔离通过 | 完整日志/APM扫描；tenant 无现有模型；密钥轮换/终结 ledger 保留清理 |
| 37 | 只读迁移预览保留完整 accounts 行通过 | 原地单账号正式迁移、旧绑定导入/排空、旧节点 gate |
| 38 | 有孤儿回滚仅 PAUSED，保持 GROUPED/占用通过 | 正式灰度、完整切换单实例旧计数路径 |
| 39 | 最终三端点大整数/未知值、重复 JSON/header 拒绝通过 | 所有 header/body 嵌入歧义、压缩/多种 map 的金样 |
| 40 | DB拒绝旧Account改写 + HTTP guard通过 | 所有后台/导入导出/插件旁路审计 |

## 实际执行命令

- `go test -race ./internal/service ./internal/handler/admin -run '^TestCredential|^TestPrincipal|^TestUpstreamPrincipal' -count=1` 通过。
- `TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestMultiCredential|^TestCredential|^TestPrincipalAdmission' -count=1 -v` 通过；日志 `/tmp/sub2api-credential-final-integration.log`。数据库实际 18.4，测试环境 OrbStack 4GB。
- `go test ./cmd/server ... -run ... '^TestProvideCleanup'` 通过（Wire cleanup 测试签名已更新）。
- `go test ./... -run '^$'` 通过：全仓后端测试编译成功。
- `go test ./...` 已实际运行，退出 1；失败项为既有 WS/流错误/模型列表/安全审计等测试，代表性 WS 与流错误失败在基线 fde7e8ec4 独立 worktree 复现；完整后端测试不能宣称通过。
- 广泛 OpenAI 回归失败：三项代表性失败在开发起点 `fde7e8ec4` 独立 worktree 复现；不能宣称全套绿色。
- `GOTOOLCHAIN=go1.27.0 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...` 完成并报告 5 个现有可达漏洞，退出 3。见实施记录。
- 前端 `typecheck`、`lint:check` 通过；API 与 i18n Vitest 5 项通过。浏览器 mock 管理 API 在 1440×1000 与 390×844 检查，通过无横向溢出/页面脚本错误；不覆盖真实鉴权。

## 发布门槛

未运行 C=10/50/200、实例3/16各10分钟压测；未达到 p95<=20ms 的可发布证据；三测试进程SIGKILL已通过，未运行完整网关/HA故障矩阵；完整 UI 仍缺新增组、替换、drain/revoke/resolve 影响预览。
不得把安装 ID 数量当设备数，不得把组件测试通过写成 AT-01～40 全部完成。

## 实测性能（未达标）

每组5秒、三worker、C=10/50/200、实例3/16，准入p95依次37.89/32.32/28.04/30.64/30.28/30.83ms。仅冒烟，无10分钟持续/突发压测与延迟分布上游模拟。20ms目标未达到。不能发布性能达标结论。
