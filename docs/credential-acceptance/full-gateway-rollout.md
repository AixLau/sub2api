# 三个完整网关进程的隔离离线迁移验收

本项运行普通 `cmd/server` 二进制及生成的 Wire 依赖图，不注入替代 handler、计费器或 reconciler。三个旧路径进程与每轮三个迁移后进程运行在同一 Docker 主机的独立网络命名空间，共享一个 PostgreSQL 主库和一个 `noeviction` Redis。Docker 网络为 `--internal`；只有 fixture 的 HTTP CONNECT mock 可以响应 `chatgpt.com:443`，它从不转发连接。请求客户端也在该隔离网络内运行，控制进程只经 `docker exec` 的 stdin/stdout 交换测试 JSON，不给网关映射外部端口。临时测试 CA 只挂载到这些进程，没有修改产品 TLS、UA 或 WS 代码。

生产配置和示例继续默认 `gateway.multi_credential_http_enabled=false`。旧路径测试进程显式为 false；迁移后的隔离 fixture 显式为 true，以便实际运行待验收路径。测试库通过已有测试 verifier 导入合成凭证；产品 `ProvideCredentialImportService` 仍传入 nil verifier。这里的 mock 身份不能证明真实 provider account/user，也不能批准真实 compact。

## 运行命令

在 backend 下顺序执行（编译与其他压测不能并行）：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p 2 -o "$ARTIFACTS/server" ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -p 2 -o "$ARTIFACTS/rollout-mock" ./internal/repository/testdata/credential-rollout-mock
SUB2API_ACCEPTANCE_SERVER_BINARY="$ARTIFACTS/server" \
SUB2API_ACCEPTANCE_MOCK_BINARY="$ARTIFACTS/rollout-mock" \
SUB2API_ACCEPTANCE_ARTIFACT_DIR="$ARTIFACTS/gateway-logs" \
TESTCONTAINERS_RYUK_DISABLED=true CI=true \
go test -p 2 -tags=integration ./internal/repository \
  -run '^TestCredentialFullGatewayOfflineRollout$' -count=1 -v
```

没有指定两个二进制路径时测试明确 SKIP，不能将默认 integration 套件中的 SKIP 记为本项通过。日志打印实际二进制 SHA-256，并在 secret 扫描通过后保存各完整网关日志及摘要。配置、私钥和 mock token 只存于临时 fixture，不提交到仓库。

## 检查内容与边界

1. 三个完整旧路径进程分别通过真实 API key 鉴权、Responses handler、代理 HTTP mock、旧 Redis 并发和计费；错误 API key 返回 401。
2. 使用真实 `CredentialRollout.Migrate` 和 Docker fence：关闭 restart policy、停止、断网、验证清单。重启旧二进制仍为零网络。原账号 ID、组、倍率和安装标识来源保持。
3. 对两个合成主体做迁移，仅对 A canary；新三个网关对 A 完成真实 grouped 请求，B 保持 SHADOW/PAUSED 且不能被请求旁路。普通 schema 迁移由原 `ApplyMigrations` 在隔离数据库及网关启动中执行；不是伪造已应用版本。
4. 终结 receipt 已持久化时，fixture SQL trigger 阻止本地 Finish；fence 所有进程，移除故障后重启完整网关，由周期 reconciler 恢复相同 lease 的释放和结算，上游调用次数保持不变。重启前后的 identity profile 与 session binding 全行一致，覆盖 AT-06 的持久身份及绑定重启边界；安装标识数量不代表 provider 实际设备数量。
5. B canary 后制造无终结事实的响应。B 的 rollback 必须失败并保持 PAUSED、ORPHANED 和占用；部分观察记录只能为 `Complete=false / REVIEW_REQUIRED / USAGE_UNKNOWN`，不能写为终结证据或零用量结算，不重放上游。再次重启完整网关，观察实际周期 reconciler 续期同一 Redis 用户 hold；人工核对所需 UNKNOWN 视图、lease 审计和管理员暂停审计继续保留，不伪造人工终结证明。
6. 对 A 通过版本化刷新存储轮换合成 token；安全 rollback 恢复最新 token，保留旧账号 ID/组/倍率/extra，原账号仍不可调度。B 的未知占用不受 A 回滚影响。
7. 扫描每个完整进程的 stdout/stderr，检查 fixture token、API key 和 panic。APM 外部接收端和完整攻击面检查由独立秘密/旁路审计报告负责，本项不扩大其结论。

本项旧节点使用候选二进制的旧路径配置，不能等同于实际运行 `fde7e8ec4` 发行镜像。正式控制转换调用和 Docker fencing 使用产品实现，未覆盖 CLI flag 解析的每个分支。测试没有停止生产、访问生产数据库或发往真实 provider；没有验证 HA、Redis Cluster、双活或跨地域。未知工作允许且必须继续人工核对。

## 实际结果

2026-09-18 初次闭环执行通过：`full-gateway-sixth.log` 退出 0，测试 43.01 秒，含 harness 总时长 50.154 秒。可靠终结 receipt 在完整进程被 fence 后重启，经真实周期 reconciler 约 11.087 秒恢复本地释放和结算。总计 8 次上游 mock 请求：3 次 legacy、4 次完整 grouped、1 次 UNKNOWN；只有前 7 次形成 usage 行，恢复没有重复请求。B 的 UNKNOWN 仍为 ORPHANED、occupied=1；A 的回滚保留最新 rotated token 且旧账号不可调度。AT-06 重启前后 profile/binding 全行一致。

五轮各三个真实 `cmd/server` 进程的 15 份日志已实际扫描 fixture access/refresh/API key/panic，均未发现命中；日志和摘要位于 `/tmp/sub2api-full-gateway-rollout/gateway-logs/`。PostgreSQL 实际报告 18.4、Redis `noeviction`，每进程数据库连接预算 8（开启时保留 2 条生命周期连接，预算未增加）。这是本机 arm64 Docker 上运行 amd64 二进制的正确性证据，不是原生性能证明。

机器可读结果见 [full-gateway-rollout-results.json](full-gateway-rollout-results.json)。server SHA-256 为 `80bdde51bd80fb88738120468dde7772fa8622af035aad921d4d6bd881df146b`，mock 为 `e00cd5f9b7f6315837fde98a134e8d5c4e3c5f461859e1b4762bdbe9bb55af02`。server 从 HEAD `7ed6277a28486470763a1dfb7be112e0b0070be3` 加当时明确记录的未提交源码构建；完整源码摘要清单在 `/tmp/sub2api-full-gateway-rollout/build-source-manifest.json`。**这不是最终 clean 候选证据**，后续安全旁路和性能源码变化须在最终冻结后重建 server 再执行本项，不能用此旧二进制覆盖。

前置适配记录均保留在 `/tmp/sub2api-full-gateway-rollout/`，不能算通过：`full-gateway-first.log` 在 Docker 平台 warning 混入 container ID 时退出 1；`full-gateway-second.log` 因并行开发中的 guard 接口暂不可编译而退出 1；`full-gateway-third.log` 在 Docker internal 网络不生成 HostPort 的环境行为下退出 1；`full-gateway-fourth.log` 已实际完成旧三进程请求、fence、迁移和新三进程 canary 请求，但测试把插入前 JSON 与持久化 JSON 比较，未计入已有 migration 175 INSERT trigger 补入的计费字段而退出 1。已改为冻结数据库实际迁移前 extra，不修改产品 trigger 或迁移逻辑。首次失败不被随后通过覆盖。

`full-gateway-fifth.log` 实际完成至两个主体的安全回滚分支，可靠终结恢复约 11.15 秒，仍因测试错误要求 UNKNOWN 没有任何 receipt 行而退出 1。实际仓储会保存部分观察但明确标成 `Complete=false / REVIEW_REQUIRED / USAGE_UNKNOWN`；修正验收为不能存在终结 receipt、不能结算为零，保留人工核对记录。本次修正没有更改产品 usage 处理。

## 迁移影响与回滚

本项只新增 opt-in 验收源码和 mock fixture，不新增产品 schema 或运行时开关。可以撤销测试代码而不触及事实表。安全回滚继续通过产品控制逻辑先 PAUSED、fence，再要求已知工作已终结且计费持久化；UNKNOWN/ORPHANED 阻断回滚，不能删除记录、清零占用或恢复旧 token 快照。
