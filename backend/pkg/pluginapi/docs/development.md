# 插件开发指南

## 稳定边界

当前宿主只支持 `openai.oauth.outbound_transport.v1`。插件负责建立实际上游 HTTP/TLS 连接，Sub2API 负责账号选择、OAuth Token 生命周期、下游协议、响应解析、SSE、错误映射、用量统计和计费。

插件不应修改 API Key 路径，也不应自行刷新或持久化 OAuth Token。

## 推荐结构

```text
plugin/
├── cmd/<plugin>/main.go
├── internal/config/
├── internal/transport/
├── ui/index.html
├── ui/assets/
├── tools/packager/
├── manifest.source.json
└── README.md
```

入口只调用 `pluginv1.Serve`。配置解析和传输实现放入独立包，以便不启动子进程就能单元测试。

## 运行时方法

| 方法 | 要求 |
|---|---|
| `GetInfo` | ID、版本、协议和能力必须与清单一致 |
| `Health` | 返回进程是否可以接受新请求，不执行昂贵探测 |
| `ValidateConfig` | 严格解析并返回完整规范化 JSON |
| `ApplyConfig` | 原子应用配置；失败时保留旧配置 |
| `TestConfig` | 验证当前环境和已保存配置，返回简短诊断 |
| `PrepareOutbound` | （可选）在宿主发送上游请求前返回受策略保护的 Header 增删和继续/拒绝决定 |
| `ObserveOutboundResponse` | （可选）接收上游响应元数据和响应头，用于轮换 ticket、Cookie 等插件状态 |
| `Forward` | 双向流式传输请求与原始 HTTP 响应 |

请求帧顺序：`start`、零到多个 `body_chunk`、`body_end`。响应帧顺序：`start`、零到多个 `body_chunk`、`end`。不能继续处理的错误使用 `error` 帧。

`request_sent` 必须如实表示请求是否可能已经到达上游。值为 `true` 时宿主禁止自动切换账号重放；只有能确认尚未调用上游 Transport 时才能返回 `false`。

### 出站 Header Hook

Header Hook 是与 `Forward` 并列的可选 unary RPC。它让状态型插件参与已有传输路径，避免
复制 HTTP、SSE 或 WebSocket 转发实现。

- `PrepareOutbound` 只接收请求元数据和宿主允许暴露的 Header 快照，不接收请求体。插件
  返回 `CONTINUE` 表示继续发送，返回 `REJECT` 表示拒绝当前请求；`UNSPECIFIED` 由宿主
  按 `CONTINUE` 处理。宿主必须重新校验 `headers_to_set` / `headers_to_delete`，插件
  不能改变 Authorization、Cookie、代理、模型或计费相关 Header。
- `ObserveOutboundResponse` 只接收响应状态、Header 和请求上下文，不接收响应体。插件可
  在此记录上游返回的短期 ticket 或 Set-Cookie，并通过自身的 HostService KV 做原子更新。
  返回值仅用于诊断，不改变宿主已经收到的响应。
- 老插件没有实现这两个 RPC 时会返回 gRPC `Unimplemented`；宿主应跳过该 Hook 并继续原
  有传输。插件实现也应把 Hook 视为幂等调用，因为宿主可能在连接重建或响应重试路径上
  再次发送同一请求 ID。

## 配置

- JSON 字段统一使用 `snake_case`。
- 拒绝未知字段、非法范围和受保护请求头。
- 默认配置必须完整，空对象应规范化为所有默认字段。
- 保存时由插件先验证和应用，再由宿主加密写入数据库。
- 数据库写入失败时宿主会尝试恢复旧配置，插件必须允许重复应用。

## 资源管理

- 复用 HTTP Transport 和连接池，不要为每个请求创建新连接池。
- 配置切换后关闭旧空闲连接。
- 使用 stream context 取消 DNS、连接、上传和响应读取。
- 始终关闭上游响应体。
- 不在插件内无限缓存按账号区分的客户端。

## 最低测试集

- 配置默认值、未知字段、边界值和深复制。
- 插件身份及协议版本。
- 请求体分块、无请求体、固定 Content-Length。
- 响应状态、重复请求头、流式响应和响应读取错误。
- 上下文取消、插件退出和超时。
- 代理开启与禁用。
- 包哈希、签名、路径穿越和目标平台运行时。
- UI Bridge 加载、保存、测试、错误和超时。

发布前还应使用真实构建包运行宿主的插件进程集成测试。
