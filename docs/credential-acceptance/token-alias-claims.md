# AT-40：历史 owner 与独立阻断声明

本子项以 `a2122fcba84f1defe65e51605cadefdf45f4edfd` 为基线，修复“已可靠持久化的 UNKNOWN 新 token 与 retired 历史 owner 碰撞时，旧入口错误地认为没有受控声明”。该碰撞由本地合成输入构造，不能据此声称真实 provider 已观察到返回相同 token。

代码提交：`e2c9445a19a536558826ed9dde2b5a2aadaaf496`；SQL 参数类型修正：`7022ca16c3710c6be4b894cd5067acd5f24cce7c`。该子项不单独关闭 AT-40：旧入口检查与写入的并发仲裁，由后续统一仲裁变更负责，必须组合验证。

## 行为和数据

- 新增迁移 `262_credential_alias_claims.sql`：`credential_instance_alias_claims` 保存实例独立别名，`credential_refresh_alias_claims` 保存每个刷新操作的独立别名。fingerprint 不唯一，允许保存多个相互冲突的 UNKNOWN 事实；历史 `credential_fingerprints` 的首个 owner 不修改。
- Begin 在同一短事务持久化输入别名声明。UNKNOWN 的返回密文与返回 token 别名同事务提交；无 TTL。操作状态 SENDING/REFRESH_RESULT_UNKNOWN 都持续阻断，不能因原 owner retired、实例停用或时间流逝自动解锁。
- Complete 检查其他非 retired 实例和其他 SENDING/UNKNOWN 操作。如果冲突返回错误，由已有 coordinator 按同一个 op 记录 UNKNOWN 补偿，不重放 provider。无其他有效声明时，可以保留 retired 历史 owner 并登记当前实例的独立别名。
- Complete 只把自身 op 改为 SUCCEEDED；其他 op 的密文、别名和状态不清理。历史声明不删除；是否阻断依据其实际持有对象状态判断。
- `hasForeignCredentialAliasClaims`、`credentialInstanceAliasFingerprints` 供统一仲裁及安全回滚使用，不授予容量或 provider 身份。调用方必须先取得统一凭证仲裁锁及排序后的注册行，再保持原 domain 锁顺序。只有只读查询不消除并发 TOCTOU。
- `backfillUnknownAliasClaims` 解封历史未知结果，补全丢失的独立声明；缺密钥、密文损坏或没有可识别的 token 时拒绝初始化，调用方必须回滚。它不改变 token、安装标识、版本或占用。

## 实际验证

本地隔离 checkout `/tmp/sub2api-at40-alias-claims` 以 e2c9445a1 加两处 SQL 参数修正运行，五个子项源文件逐字匹配 7022ca16c。另复制 root 的 baseline 测试文件作为测试 overlay，SHA-256 为 `0b19f569c32833ddfa3e29813103e790d16458249a2d32940711c2d07eb75cad`；未编辑或提交该 baseline 文件。环境为 Go 1.27.0 darwin/arm64、OrbStack、本地独立 PostgreSQL 与 Redis 测试容器。

```sh
cd /tmp/sub2api-at40-alias-claims/backend
GOCACHE=/tmp/sub2api-queue-progress/go-cache TESTCONTAINERS_RYUK_DISABLED=true CI=true \
  go test -p 2 -tags=integration ./internal/repository \
  -run '^(TestCredentialArbitrationBaselineUnknownAliasRetiredCollision|TestCredentialAliasClaims|TestCredentialRefreshFamily|TestCredentialVaultOfflineRotation)' \
  -count=1 -v
```

退出 0，包耗时 9.907 秒，六个根测试实际 PASS：基线碰撞复现、双 UNKNOWN 结果保留与十年不解锁、Complete 只结束自身操作、历史 UNKNOWN 回填失败关闭/幂等、刷新 family/version/identity、包含真实 PostgreSQL Commit 后丢 ACK 的 vault 轮换。

证据：`/tmp/sub2api-at40/alias-claims-casts-fixed.log`，SHA-256 `f6ab8e6c43d5ecdfbcd8ae6fb05979b88882fd2e83024d8946d6f38485a9dd32`；命令和源码摘要在 `/tmp/sub2api-at40/alias-claims-evidence.json`。

保留两次开发失败：`alias-claims-focused.log` 是共享工作树的其他未完成 wrapper 导致编译失败；`alias-claims-isolated.log` 是新增 UNION UUID 参数推导及测试 AAD 参数类型错误，退出 1。修正后的通过不删除首次日志。未在此子项执行全量 suite、race、六组合矩阵或真实 provider 验证；最终集成候选由 root 统一测试。

## 迁移和回滚

迁移 262 只回填历史实例别名，不可能用 SQL 从密文导出 token HMAC。必须在 vault 可用且统一仲裁事务持锁时调用历史 UNKNOWN 回填，然后才允许把空查询解释为没有冲突。不可跳过损坏密文继续启用。

部署期间功能保持关闭。本变更无上游网络动作，无容量配置变更。应用回滚应保留两个新表、所有历史 owner、UNKNOWN 密文和操作状态；不得 drop claims 或恢复旧 token 快照以让旧 guard 放行。旧版本 guard 不识别独立 UNKNOWN 声明，因此不能恢复其受控凭证旧入口写入/刷新能力后宣称安全。需要回滚时保持相关入口关闭，等待支持新声明的安全版本或具备证据的人工处置。

## 后续统一仲裁

本缺陷2的组件修复随后已接入 [AT-40统一仲裁](at40-arbitration.md)：所有凭证变更先取持久仲裁行，再保留各自domain锁序；受控导入/激活/刷新与legacy仓储写入共同检查独立UNKNOWN声明。请以该报告的最终候选测试为当前状态，本文件保留7022ca组件证据，不将其单独扩大为跨入口互斥或生产可启用。
