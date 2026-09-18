# 离线迁移、主体灰度与回滚

## 支持和证据边界

本工具只支持单Docker主机、明确的Compose project/service、非privileged、非host/container网络的网关容器；一个PostgreSQL主库、一个Redis noeviction实例。所有能持有凭证/发上游请求的网关与后台任务必须属于同一受控service清单。外部客户端、别的部署、Docker管理员重新接网或未列入清单的执行者不在保证内；无法核实清单时阻断切换。

fencing采用Docker实际控制：关闭restart policy → stop（最长30秒）→ 断开全部网络 → 复核容器ID清单、停止状态、零网络。重启旧容器仍无网络；Verify拒绝运行旧节点。不会仅依赖PG占用字段阻止已缓存token的旧进程。

**停止连接不能证明远端结束。** 迁移前必须有明确的旧执行/会话排空证据；CLI将其留审计，不能把Redis计数0或timeout本身当证据。工具不自动伪造证据。真实provider verifier缺失时，UNVERIFIED在fencing前拒绝，故生产执行仍BLOCKED。

## 工具

在backend编译 `go build -o /tmp/credential-control ./cmd/credential-control`。连接/密钥只通过部署秘密环境提供：`SUB2API_CREDENTIAL_CONTROL_DSN`、`SUB2API_CREDENTIAL_VAULT_KEY`，不写命令参数、仓库或日志。

1. `--operation preview --account <id>` 只读预览。
2. 确认旧请求/旧session排空，完整清点网关容器；验证import必须经可信provider，不可改SQL字段模拟真实验证。
3. `--operation migrate --account <id> --import <ref> --operation-id <uuid> --actor <admin-id> --total <explicit-limit> --drain-evidence '<证据引用>' --compose-project <project> --gateway-service <service>`：预验证 → fence → 单事务绑定原Account ID。原credentials全量加密归档，真实token移到版本vault，旧Account不留可调用token；seed/device、组、倍率、代理不变，默认SHADOW/PAUSED。
4. 审核影子结果后，`--operation canary --principal <id> --version <v> --actor <admin-id> --compose-project <project> --gateway-service <service>`：再次fence，只切指定已VERIFIED主体。该离线工具不自行启动网关；启动支持当前schema/功能开关的新镜像，逐主体观察。未经验证的compact能力不能靠此工具开启。
5. 回滚先暂停主体，执行 `--operation rollback --principal <id> --version <v> --actor <admin-id> --compose-project <project> --gateway-service <service>`。只允许有migration记录的单保留实例；任何lease/queue/未知refresh/PENDING usage都阻断，保持PAUSED。清晰终结后归档binding，恢复原Account ID与最新rotated token，控制元数据留存。旧Account保持**不可调度**，必须复核旧用户/账号并发和旧session状态后另行启用，不能直接同时恢复多个实例。

257为前向加表和归档关联字段，保留审计/身份profile；不DROP任何ledger，不用旧DB备份覆盖刷新结果。session/full及shadow alias的正式迁移仍拒绝；没有一致旧身份时拒绝，不重新生成旧身份。

## 实际测试

`TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^TestCredentialComposeFence|^TestCredentialRolloutUnverified' -count=1 -v` 通过。日志 `/tmp/sub2api-acceptance-closure/rollout-verified.log`。

三个真实Docker网络命名空间中的合成旧进程，停止/隔离/重启后网络为空；PG迁移保留ID/extra/组/倍率；单主体canary；凭证轮换后rollback保留最新token；旧generation/profile不变；UNVERIFIED没有调用fence。

限制：旧容器是合成进程，不是fde7e8ec4完整网关镜像；mock验证器仅测试中注入。真实旧版完整部署、HA、Redis故障重建、旧sticky绑定导入与多实例自动回滚均未据此验收。对应AT-34/37/38继续PARTIAL/BLOCKED，禁止概括为生产可用。

2026-09-18 补充的 [完整网关闭环](full-gateway-rollout.md) 已实际运行普通 `cmd/server`，每轮三个进程，覆盖 legacy 请求→真实 fence→两主体迁移→单主体 canary→receipt 恢复→UNKNOWN 保留→最新 token 安全回滚，并扫描 15 份进程日志。它补足“只有合成 sleep 旧进程”的证据缺口；旧节点仍为候选二进制的 legacy 配置，真实 provider 验证、fde7e8ec4 历史发行镜像和最终候选重建仍不可由初次运行推出。详细二进制和代码快照边界见该报告。

## 时间/性能专项迁移与回滚补充

258仅新增活跃lease部分索引，不改事实表/占用/身份/凭证。正式迁移仍经原ApplyMigrations在受支持的离线窗口执行；普通CREATE INDEX期间会阻挡request_leases写入，大表必须预留停机时间，不能将其描述为无锁在线迁移。回退优化代码时保留索引即可，不回退已轮换凭证，也不删除UNKNOWN/ORPHANED记录。时间修复之前的二进制有已复现的到期判断缺陷，禁止将回退旧二进制作为生产放行方案。

第一版部署限制：单PostgreSQL主库、单共享Redis（noeviction）、同主机受控网关清单。拒绝把自动HA、只读副本准入、Redis Cluster或跨地域双活标成已支持；更换拓扑必须保持功能关闭并另行验收。现有CLI能核验受控容器/网络，不能从DSN自动证明外部数据库没有HA或副本切换，部署操作者仍须核对该限制。此报告没有新增拓扑自动探测代码，也没有重跑生产迁移。

真实compact只接受provider契约或可信provider集成验证；受控mock只证明本地适配器满足mock，不可用于批准真实端点能力。真实导入无verifier继续UNVERIFIED，迁移/canary前置检查不放宽。


当前专项 HEAD 受控复核（代码快照与专项测试相同，文档提交不改产品代码）：

```sh
TESTCONTAINERS_RYUK_DISABLED=true CI=true go test -tags=integration ./internal/repository -run '^(TestCredentialComposeFence|TestCredentialRolloutUnverified|TestCredentialMigrationPreviewAndRollback)' -count=1 -v
```

实际通过，日志 `/tmp/sub2api-admission-time-performance/rollout-fencing-current.log`。这证明合成 Compose fence、迁移预览/安全回滚和 UNVERIFIED 在 fence 前阻断；不证明真实旧完整网关镜像、可信 provider verifier 或未支持 HA 拓扑已完成正式迁移。生产迁移仍需先满足身份契约和声明拓扑限制。
