# BPS Local 桥接插件（openai-bps-local）

把 [bps-local-public](https://github.com/sunboyzoey/bps-local-public)（2026-11-06 快照 `8736c9f`）的 Responses → ChatGPT Excel / Basispoints（BPS）桥接移植为 Sub2API 的 OpenAI OAuth 出站传输插件。

- 插件 ID：`local.sub2api.openai-bps-local`，版本 `0.1.0`
- 能力：`openai.oauth.outbound_transport.v1`（platform `openai`，account_type `oauth`）
- 上游：默认 `https://bps.openai.com/basispoints/api/responses`，**默认走 WebSocket**（bps-local 行为），可切到 HTTP SSE

## 按账号启用

插件**不是全局生效**：只有在插件管理页“绑定账号”里选中的 OpenAI OAuth 账号，其请求才会交给本插件；其余账号继续走宿主原生通道。账号范围可以随时调整，宿主会原子替换绑定快照，不需要重启插件。

宿主同一时间只允许启用一个 OpenAI OAuth 出站插件，本插件与 `openai-basispoints-transport` 互斥，切换前需先停用另一个。

## 与原版 bps-local 的差异

| 项 | bps-local CLI | 本插件 |
| --- | --- | --- |
| 授权 | 独立 BPS 设备码登录，读本地 oauth.json，不刷新 | 复用宿主为绑定账号解析出的 OAuth（`Authorization` + `chatgpt-account-id`），刷新由宿主负责，插件不保存凭据 |
| 账号 | 每个进程一个账号 | 按宿主绑定的多个账号；工具回放缓存按“账号 + 客户端会话”隔离 |
| 代理 | `--proxy` 全局 | 默认使用每个账号自己的代理（http/https/socks5），可改为直连 |
| 入口 | 本地 `127.0.0.1:8790` | 宿主网关；本插件不监听端口 |
| 非 `/responses` 端点 | 不支持 | 默认原样透传到宿主原始 URL（如 `/responses/compact`），可关闭 |
| 图片中转 / 协议诊断 / 设备登录 | 有 | 未移植（图片中转需要公网托管地址；诊断与登录属于 CLI 功能） |

`internal/{basispoints,sanitize,retryafter,proxy,auth}` 是原仓库代码的快照，只改了 import 路径；删除了 CLI 专用的 `protocol_diagnostic*.go` 和需要真实账号的 `live_access_test.go`，`auth` 只保留 `auth.go`（测试用的凭据文件解析和从 JWT 取账号 ID）。唯一新增的是 `internal/proxy/plugin_api.go`，提供请求级凭据与按代理复用传输的入口。今后与上游同步时，替换这些目录后重新执行上述删除即可。

原仓库使用 Unlicense（公有领域），许可原文见 [LICENSE.bps-local](LICENSE.bps-local)。

## 配置

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `upstream_url` | BPS Responses 地址 | 只允许 https / wss，不能带凭据、查询参数或片段 |
| `upstream_transport` | `ws` | `ws` 或 `sse` |
| `ws_idle_timeout_seconds` | `120` | 等待下一条上游 WS 消息的空闲预算，5–3600 |
| `tools_version` | 空 | 写入 `metadata.bps_tools_version_id`，空表示由服务端决定 |
| `arc_text_session` | `false` | 实验：纯文本请求走 ARC 会话；开启后带客户端工具的请求会被拒绝 |
| `proxy_mode` | `account` | `account` 使用账号代理，`disabled` 直连（忽略插件进程的环境代理） |
| `native_passthrough` | `true` | 非 `POST /responses` 请求是否原样透传 |
| `extra_headers` | `{}` | 额外的 BPS 请求头；身份与协议头（Authorization、账号 ID、Origin、`X-OpenAI-*`、`X-Basispoints-*` 等）保存时直接拒绝 |

## 构建与安装

```bash
cd backend/plugins/openai-bps-local
TARGETS=linux-amd64 ./build.sh          # 产物：dist/openai-bps-local-0.1.0.s2plugin
```

可用 `SIGNING_KEY` + `KEY_ID` 签名。安装后：上传插件包 → 保存配置 → 在绑定里选择要使用的账号 → 启用。建议先用一个专用测试账号验证普通文本，再验证工具调用和多轮回放，然后再扩大账号范围。

## 已知限制

- 工具回放缓存存在插件进程内存中（上限 1024 条 / 16 MiB），插件重启或多实例之间不共享；跨实例或重启后的会话续接可能找不到原始工具调用。
- BPS 是产品内部接口，模型权限和请求格式可能变化；本地测试与打包通过不代表真实 BPS 联调通过。
- 上游返回的 HTTP 状态（401 / 429 等）会原样交给宿主，宿主据此执行账号冷却或标记。
