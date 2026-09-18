# 五个扫描项逐项处置

验收起点 `e00ea2cab`，依赖 grpc v1.82.1、x/image v0.41.0。复核命令：

```sh
GOTOOLCHAIN=go1.27.0 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 -json ./...
```

原始扫描 `/tmp/sub2api-acceptance-closure/govulncheck.jsonl`；`-json` 流模式退出0不等于无漏洞。可达 findings 见 [JSON](vulnerability-findings.json)。未执行恶意负载复现，未改变任何协议/TLS或无关代码。

| ID | 仓库调用路径 | 前提与处置结论 |
|---|---|---|
| [GO-2026-6443](https://pkg.go.dev/vuln/GO-2026-6443) | pkg/pluginapi/v1/runtime.go Serve → hashicorp/plugin.Serve → grpc transport HandleStreams | 官方报告需要 xDS routing。仓库本地插件使用默认 gRPC server，未查到 xDS 配置；**条件未确认触发，依赖版本仍受影响**，不能仅凭本地插件部署豁免全部使用方。要求后续升级到修复版本并验证插件；本轮按“不改无关代码”只记录。 |
| [GO-2026-6348](https://pkg.go.dev/vuln/GO-2026-6348) | internal/service/plugin_runtime.go roundTrip Send/Recv → gRPC transport/mem；pluginapi.Serve → NewServerTransport | 扫描确认客户端及服务端符号可达。插件通信通常本机也不能保证内存消耗风险不存在。**未修复，发布阻断项**；修复版本至少1.83.1，同时6443需选1.83.2或后续安全版本。 |
| [GO-2026-6222](https://pkg.go.dev/vuln/GO-2026-6222) | admin/reward_handler.go UploadSkin → image.DecodeConfig → vp8l.Decode | 需要管理员上传输入，但仍是可达解析路径。**未修复，发布阻断项**；x/image>=0.45.0且回归图像上传。 |
| [GO-2026-5061](https://pkg.go.dev/vuln/GO-2026-5061) | service/user_service.go compressInlineAvatar → image.Decode → webp.Decode；UploadSkin → image.DecodeConfig | 可处理用户/管理员图像。**未修复，发布阻断项**；x/image>=0.43.0；统一升级应覆盖6222。 |
| [GO-2026-4961](https://pkg.go.dev/vuln/GO-2026-4961) | 同头像压缩和皮肤上传 WebP 解码路径 | 官方触发条件为32位平台。本轮实际arm64宿主/amd64数据库拓扑不满足32位条件；**限定拓扑中条件不满足，依赖未修复**，不向32位部署作安全承诺。x/image>=0.42.0修复，统一升级应覆盖6222。 |

结论：五项分别核对，未做无依据忽略。要求记录/处置结论不等于本任务获得了无关插件/头像功能修改授权；依赖升级与相应回归应作为独立安全变更。生产启用仍受未修复项约束。
