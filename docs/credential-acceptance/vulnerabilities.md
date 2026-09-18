# 五个扫描项逐项处置

本轮安全依赖修复以 `924819c5d04a550801fb4e85441059389dc00caa` 的 grpc v1.82.1、x/image v0.41.0 为对照。2026-09-18 重新核对官方 Go 漏洞数据库和 gRPC 公告，选择 grpc **v1.83.2**、x/image **v0.45.0**，覆盖下列五项。采用正常 Go MVS 解析得到所需传递升级（x/net、x/sys、x/text、x/crypto、x/sync、x/term、x/mod、x/tools、OpenTelemetry、genproto），没有手工降级依赖或改变 gRPC/TLS 配置。

| ID | 实际调用路径 | 官方修复版本和触发条件 | 本次处置 |
|---|---|---|---|
| [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) | `pkg/pluginapi/v1/runtime.go Serve` → `hashicorp/plugin.Serve` → gRPC transport | 稳定分支修复版 1.82.2 / 1.83.2。需要 xDS routing，并成功建立传输连接后发送同时缺少 authority 和 Host 的请求。本仓库使用 `DefaultGRPCServer`，未发现 xDS 配置，不能把符号可达解释为当前部署必然可利用。 | grpc 1.83.2 覆盖修复；没有以“本地插件”或“没有 xDS”豁免升级。 |
| [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348) | `internal/service/plugin_runtime.go roundTrip` Send/Recv → gRPC transport/mem；插件 Serve → NewServerTransport | 1.83.1 修复。攻击者须能建立 gRPC 流并发送大量细碎 HTTP/2 DATA 帧，造成额外内存分配。官方修复使用默认启用的 receive buffer compaction；不能设置 `GRPC_GO_EXPERIMENTAL_ENABLE_RECEIVE_BUFFER_COMPACTION=false` 撤销保护。 | grpc 1.83.2 覆盖修复，回归真实独立插件进程的握手、unary 配置、双向流、HTTP mock 回包及取消。 |
| [GO-2026-6222](https://pkg.go.dev/vuln/GO-2026-6222) | 头像压缩 `compressInlineAvatar` → image.Decode → webp/vp8l；皮肤上传 `UploadSkin` → image.DecodeConfig（历史扫描路径） | 0.45.0 修复。特制 VP8L 输入包含大量未使用 Huffman tree groups 时可能过量分配。头像压缩会实际解码；皮肤入口的 DecodeConfig 是否进入完整 VP8L 解码依文件/版本而定，不能把调用图潜在可达等同每次上传都触发。 | x/image 0.45.0；有效无损 WebP 头像超过 20 KiB，确认经过解码和 JPEG 压缩；有效皮肤通过 multipart HTTP handler。 |
| [GO-2026-5061](https://pkg.go.dev/vuln/GO-2026-5061) | 同头像压缩和皮肤上传 WebP 入口 | 0.43.0 修复。VP8/VP8L 尺寸与 canvas 不一致的特制输入可导致 panic。 | x/image 0.45.0；增加 canvas 不匹配头像拒绝、无 panic、无持久化测试，并保留损坏上传拒绝测试。 |
| [GO-2026-4961](https://pkg.go.dev/vuln/GO-2026-4961) | 同头像压缩和皮肤上传 WebP 入口 | 0.42.0 修复。大尺寸无效 WEBP 引起 panic 的报告限定 32 位平台。本轮 darwin/arm64 不满足该条件。 | x/image 0.45.0 覆盖版本修复；没有在本机复现 32 位漏洞，也没有宣称所有 64 位部署都可利用。 |

[官方 gRPC 1.83.2 发布说明](https://github.com/grpc/grpc-go/releases/tag/v1.83.2)、[xDS 公告](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj)、[DATA 帧碎片公告](https://github.com/grpc/grpc-go/security/advisories/GHSA-vp52-pcj8-j9qc)与五个 `vuln.go.dev/ID/*.json` 已保存为本地审计产物。具体命令、退出码、受测快照和 SHA-256 见 [security-dependency-results.json](security-dependency-results.json)。

安全代码提交为 `6f8dc9b36e73fff4b86616a251e926eae3d52a61`。本次开发定向测试退出 0，`govulncheck` 默认文本及 verbose 两次扫描均退出 0，原五项不再出现。扫描结论是 **0 可达符号漏洞、0 导入包漏洞、6 项仅模块级发现**；不能简化成整个依赖图无漏洞。6 项分别为 crypto/ssh 的 GO-2026-6355、6354，crypto/openpgp 的 GO-2026-5932，mod/sumdb 的 GO-2026-6180、6179，以及 compress/s2 的 GO-2026-5841。扫描未发现本项目导入这些受影响包；它们的版本和后续处置线索在 JSON 中保留，不伪装成已经升级或攻击验证通过。

开发验证发生在共享工作树，期间有其他专项的未提交改动。因此这里固定安全文件提交及 go.mod/go.sum 摘要，`tested_code_sha` 诚实保留为空，不能据此宣称已有不可变最终候选的全系统 PASS。发布候选应在源码冻结后统一执行并记录测试和扫描；后续仅文档变化无需重跑。

## 回归范围与证据边界

- 自包含的 `TestPluginRuntimeGRPCSubprocess` 使用真实 go-plugin 子进程、真实 gRPC client/server 和本地 HTTP mock。覆盖 320 KiB 分帧传输、响应头和正文、取消传播及无重放。它不验证外部第三方插件包的签名或实现。
- 原 `TestPluginRuntimeIntegration` 需要 `SUB2API_TEST_PLUGIN_PACKAGE`，本轮没有提供外部签名安装包；该测试的 SKIP 单独记录，不能计为 PASS。
- 头像测试覆盖无损 WebP 压缩持久化、冲突 canvas 及截断文件拒绝；皮肤测试覆盖 PNG/JPEG/WebP multipart 成功和损坏、空、超限输入拒绝。生成 fixture 不含用户图片或秘密。
- 兼容测试不是五项 DoS 的攻击复现，也不是第三方依赖所有功能的完整验证。32 位执行、外部签名插件包和 xDS 部署未在本轮验证。
- 历史扫描 `/tmp/sub2api-acceptance-closure/govulncheck.jsonl` 与 [原始 findings](vulnerability-findings.json)继续保留。原扫描的 JSON 流模式退出 0 不代表无漏洞；本轮使用文本默认扫描结果和实际退出码判断。

## 迁移与回滚

本安全变更没有数据库迁移、配置开关或业务权限修改。发布需要重新构建网关和自有插件，更新宿主依赖不会自动重写已经分发的第三方插件二进制。旧插件包必须单独重新构建/审计；版本号升级不能代表旧二进制已修复。

如兼容问题要求撤回，优先暂停受影响插件并保持多凭证关闭，在安全修复版本上修复兼容问题；不得把降回 grpc 1.82.1 / x/image 0.41.0 视为可生产启用的安全回滚。若撤销本提交用于诊断，五项版本风险恢复，B2 必须重新标记 BLOCKED。此过程不触碰凭证身份、token、租约或 usage 事实。

## 最终候选复验

`tested_code_sha=b06789dde6ddb7c25fc29ffb7ae81ed79cf6eef8` 的追踪文件快照已重跑插件/头像/皮肤相关定向和race（命令与根/子测试记录归入发布收尾证据）。显式 `GOTOOLCHAIN=go1.27.0 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -show verbose ./...` 实际退出0，仍为0可达、0导入包、6仅模块级发现；原五项不再出现。日志 `/tmp/sub2api-release-closure/final-logs/candidate-govulncheck-go127.stdout`，SHA-256 `410dea70b554ad5de426c88e3ddaf8b9c7ada1266b73b7bd9d6ecd2778b8f71a`。

首次未固定toolchain的运行自动选Go1.26.8，无法解析项目Go1.27，退出1且未完成扫描。其日志和退出码保留在 `candidate-govulncheck.*`，不是漏洞扫描PASS；显式toolchain复验没有改源码、依赖版本或扫描规则。不能把六项模块级发现改写为整个依赖图无漏洞，也不能将宿主升级外推到已分发的第三方插件二进制。
