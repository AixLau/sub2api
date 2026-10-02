# OpenAI Codex Ticket / Route / Semantic Hook

`openai-codex-ticket` 是对 `SUCK_MY_ASTRA` 运行时核心行为的 Sub2API 插件化复刻。它使用宿主提供的 OAuth 账号目录和出站身份，在独立采票出口获取 Codex `x-codex-turn-state`，维护跨账号的 `__cflb`/`__oailb` 路由 Cookie 池，按账号和模型保存票据，并在响应阶段观测实际 served model 与 ticket envelope 形状。

插件负责票据、路由、采集/探测、云端 Mint、降级门禁和安全状态摘要；账号选择、OAuth access token、业务代理、HTTP/SSE/WebSocket 传输、响应流、计费和用量统计仍由宿主处理。采票代理和 Relay 代理永远不会写入账号业务代理，也不会被用于业务请求。

## 能力和包布局

插件清单声明能力 `openai.oauth.codex_ticket_hook.v1`（平台 `openai`、账号类型 `oauth`）。运行时通过 `HostService` 使用以下宿主能力：

- 列出当前 capability scope 内的 OAuth 账号元数据；
- 解析指定账号的短期出站身份；
- 使用插件命名空间的 KV 保存票据和探测状态；
- 通过 `PrepareOutbound` hook 返回受限的 `x-codex-turn-state` 和路由 Cookie 信号；
- 通过响应 observer 接收 `Set-Cookie`、WebSocket metadata 和 served-model 语义；
- 使用同一插件 UI 查看票据、路由健康、观测统计和探测事件。

插件不会读取其它插件的 KV，也不会返回 ticket state、Cookie、OAuth token 或采票代理凭据到管理 UI。

标准包结构如下：

```text
manifest.json
runtimes/<goos>-<goarch>/openai-codex-ticket
ui/index.html
ui/assets/bridge-v1.js
ui/assets/app.js
ui/assets/styles.css
```

## 配置

配置通过插件管理页面保存为 JSON。插件会拒绝未知字段、非 HTTPS 的接口地址、带凭据的接口地址和不支持的代理地址；保存前会规范化模型列表和默认值。

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `enabled` | `false` | 启用后台采票和出站注入。关闭时不采票、不注入。 |
| `role` | `business` | `business` 负责注入；`probe` 只采集/观测，不改业务请求。 |
| `dry_run` | `false` | 只记录决策，不注入 ticket 或路由 Cookie。 |
| `log_decisions` | `true` | 记录采集、路由、注入和观测决策的安全摘要。 |
| `block_degraded` | `false` | served model 与请求模型不一致时，是否阻止响应。 |
| `template_length` | `292` | 用于响应形状分类的自然模板长度。 |
| `replace_length` | `312` | 用于响应形状分类的替代/受限长度。 |
| `target_length` | `780` | 支持 `292`、`332` 或 `780`；780 配置会按账号实际 envelope 动态接受个人/Business 票，780 票据还会校验 Cookie/Gateway。 |
| `ttl_seconds` | `240` | 本地缓存 TTL，范围 60 至 604800。 |
| `route_cookie_ttl_seconds` | `3900` | 路由 Cookie 池 TTL，按最早可信过期时间和最近观测裁剪。 |
| `refresh_before_seconds` | `60` | 距过期小于该值时刷新。必须小于 TTL。 |
| `harvest_probe_interval_seconds` | `180` | 后台探测周期，最小 30 秒。 |
| `harvest_cooldown_seconds` | `180` | 单账号采票失败后的冷却时间。 |
| `max_probes_per_round` | `6` | 一轮最多并行探测的账号数。 |
| `harvest_attempt_timeout_seconds` | `25` | 单次采票超时。 |
| `models` | `gpt-6-astra`, `gpt-5.6-sol` | 参与采票、注入和 served-model 观测的模型。按账号和模型保存。 |
| `harvest_proxy_url` | `""` | 仅采票使用的 `http(s)://`、`socks5://`、`socks5h://` 或 `ip-pool`。 |
| `harvest_proxy_pool` / `harvest_proxy_mode` | `[]` / `static` | 支持 static、pool、api；出口只用于采票。 |
| `harvest_proxy_api_url` | `""` | `api` 模式下获取下一采票代理的 HTTPS 接口。 |
| `fail_closed` | `false` | 没有有效票据时是否阻止目标模型调度；默认 fail-open。 |
| `target_gateway` | `unified-88` | 780 票据的目标 Gateway。 |
| `transport` | `sse` | 780 票据的传输类型：`sse` 或 `websocket`。 |
| `ticket_url` | ChatGPT Codex Responses URL | 票据请求接口，必须是无凭据 HTTPS URL。 |
| `harvest_url` | `ticket_url` | 采票接口，必须是无凭据 HTTPS URL。 |
| `cookie_validation` | `true` | 780 票据是否要求 Cookie 仍然新鲜。 |
| `gateway_validation` | `true` | 780 票据是否匹配 `target_gateway`。 |
| `team_plan_blocked` | `false` | 是否跳过 Team、Business 和 Enterprise 账号。 |
| `cloud_mint` | 关闭 | 可选 Relay：缓存票不可用时按 OAuth 身份和路由种子获取短期票，支持代理、Gateway、TTL、超时和 fail-closed。 |

典型配置：

```json
{
  "enabled": true,
  "role": "business",
  "dry_run": false,
  "log_decisions": true,
  "block_degraded": false,
  "template_length": 292,
  "replace_length": 312,
  "target_length": 780,
  "ttl_seconds": 240,
  "refresh_before_seconds": 60,
  "harvest_probe_interval_seconds": 180,
  "harvest_cooldown_seconds": 180,
  "max_probes_per_round": 6,
  "harvest_attempt_timeout_seconds": 25,
  "models": ["gpt-6-astra", "gpt-5.6-sol"],
  "harvest_proxy_url": "socks5h://proxy.example:1080",
  "harvest_proxy_mode": "static",
  "fail_closed": false,
  "target_gateway": "unified-88",
  "transport": "sse",
  "ticket_url": "https://chatgpt.com/backend-api/codex/responses",
  "harvest_url": "https://chatgpt.com/backend-api/codex/responses",
  "cookie_validation": true,
  "gateway_validation": true,
  "team_plan_blocked": false
}
```

## 票据、路由和语义生命周期

插件启动后加载规范化配置并启动后台 worker。worker 只探测宿主列出的 active OAuth 账号；暂停、禁用、shadow 或超出 capability scope 的账号不会被探测。

每个 `(account_id, model)` 独立保存一张主票据和可选备用票据。采票成功后记录长度、签发时间、有效期、身份摘要、采票出口、transport、gateway 和 Cookie 摘要；KV 中的原始 ticket state 仅供插件使用。身份摘要由 `chatgpt_account_id` 与邮箱计算，账号身份变化后旧票据自动失效。

292 票据检查 Fernet-like envelope 的长度、前缀、签发时间和 TTL。780 票据额外检查 transport、Cookie 新鲜度和 Gateway；主票失效且备用票仍有效时自动切换备用票。采票失败进入冷却并保留上一次有效票，避免瞬态错误使账号立即不可用。

响应中的 `Set-Cookie` 只解析为允许列表中的两枚路由 Cookie。插件按 gateway、TTL、最近成功/降级时间和 90 秒惩罚窗口选择路由对；请求级 fingerprint 会在 401/403、Cookie 删除、served-model 不匹配时定向撤销或降权，避免把一个坏出口扩散给所有账号。

请求发送前，宿主将账号、模型和 transport 交给插件 hook。插件可以返回 `x-codex-turn-state` 和一个结构化路由信号，宿主只合并 `__cflb`/`__oailb`，不能修改 `Authorization`、账号身份 Header、模型、代理或计费字段。插件不可用或采票失败时遵循 `fail_closed`：默认继续原有请求，显式开启后仅阻止配置中目标模型的请求。

宿主会在 SSE、非流式 JSON、原生 WebSocket、HTTP bridge 和 WebSocket metadata 中报告实际 served model。`block_degraded` 开启时，插件返回 `DEGRADED_MODEL`，宿主在首个可见输出前清空暂存并发送协议兼容的失败响应；已经向客户端提交输出后不回写第二个响应。WebSocket 的 ticket metadata 从 `codex.response.metadata`（以及等价嵌套形状）提取，不依赖旧的 101 header。

## 管理状态

插件 `Health.status_json` 提供无副作用的状态快照：

- `accounts_total`、`ready_tickets`、`blocked_tickets`；
- `models` 和按账号/模型分组的 ticket 摘要；
- 路由 Cookie 指纹、Gateway、健康时间和剩余 TTL；
- 自然/注入 ticket 长度分类、重复长度学习、served-model 匹配/降级和最近事件；
- `host_kv`、最近探测结果、决策计数器和 worker 健康信息。

`Health.status_json` 本身是只读快照；观测数据在响应 observer 路径持久化。摘要只包含长度、模型、transport、gateway、出口、剩余秒数、Cookie 数量、指纹和过期时间，不包含 state blob、Cookie 值、OAuth token、邮箱或代理认证信息。UI 每 5 秒刷新一次，保存和校验分别调用 `config.save`、`config.test`，不会调用有副作用的探测接口。

## 构建和验证

构建脚本默认生成 Linux amd64 包；可以用 `TARGETS` 指定多个运行时：

```bash
cd backend/plugins/openai-codex-ticket
TARGETS=linux-amd64,darwin-arm64 ./build.sh
```

脚本不会运行测试，也不会删除旧包。未设置签名参数时生成开发包，宿主必须显式允许 unsigned 插件。生产构建使用独立的 Ed25519 私钥：

```bash
SIGNING_KEY=/secure/path/publisher.private \
KEY_ID=sub2api-official-v1 \
TARGETS=linux-amd64 ./build.sh
```

建议在 `backend` 目录执行相关检查：

```bash
go test ./plugins/openai-codex-ticket/...
node --test plugins/openai-codex-ticket/tools/ui-config.test.cjs
TARGETS=linux-amd64 ./plugins/openai-codex-ticket/build.sh
```

将 `.s2plugin` 上传到管理员插件页面后，先绑定目标 OAuth 账号，再开启插件。生产环境建议先保持 `fail_closed: false`，确认采票摘要和真实业务请求均正常后，再按模型逐步开启严格门禁。

## 安全边界

- ticket state 只存在于插件 KV 和插件进程内存，不写入普通日志和 UI；
- 采票使用独立代理，每次采票建立独立连接，不复用业务连接；
- 宿主负责 capability scope，插件无法读取其它平台或账号类型；
- hook 返回值只允许设置/删除受保护的 Codex ticket Header，并由宿主严格合并两枚路由 Cookie；
- Relay 请求只携带短期 OAuth access token、账号标识和已验证的路由 Cookie 对；响应必须是 200、严格 JSON、匹配模型/长度/transport/Gateway 且具有可信过期时间；
- 配置 UI 和状态摘要使用 `textContent`，不把状态数据解释为 HTML；
- 签名包必须由宿主信任的发布者验证，私钥不进入源码或插件包。

## 生产验证边界

模拟上游测试可以验证 envelope 解析、TTL、身份绑定、备用票、代理隔离和 Header hook 决策，但不能证明真实上游接受特定账号、出口或 780 Cookie。部署后应使用真实 OAuth 账号分别验证采票、SSE、WebSocket 和过期刷新，并检查宿主审计日志中的 hook 决策；不要在 issue、日志或截图中暴露 ticket state、Cookie 或代理凭据。

## 与独立版管理面板的边界

独立的 `SUCK_MY_ASTRA` 还提供管理 HTTP 路由、账号目录、代理连通性检查、云端链路页面和认证目录接口。当前 Sub2API 插件 ABI 只开放配置、Health/UI、KV、OAuth capability 和出站/响应 hook，没有允许插件注册任意管理路由的接口，因此这些管理能力被收敛为 `config.*`、`Health.status_json` 和插件 UI；账号、代理、认证和业务请求仍由宿主负责。票据采集、轮换、身份绑定、路由 Cookie 池、云端 Mint、响应语义观测及降级门禁等运行时核心行为已在该边界内复刻；若要逐字暴露独立版的公开管理 API，需要先扩展宿主插件协议。
