# Sub2API 公开渠道状态 v2 快照

公开资料接口将渠道状态 v2 的被动请求聚合转换为独立的公开 JSON。所有查询仅包含活跃、非专属分组，并继续受 v2 已启用的平台、模型和分组配置限制。

## 路由

| 方法 | 路径 | 响应 |
| --- | --- | --- |
| GET | `/.well-known/ai-transit.json` | 协议版本、系统类型、快照地址、生成时间 |
| GET | `/api/public/transit/v1/snapshot` | 公开 v2 快照 |
| GET | `/api/v1/public/transit/snapshot` | 相同快照 |

接口不要求登录。成功响应直接返回 JSON 对象，没有后台 API 的 `data` 包装，设置 `Cache-Control: public, max-age=60`。

发现接口的 `snapshot_url` 是相对于当前站点的根路径，不使用请求中的 Host 或转发头生成外部地址。

```bash
curl https://your-domain.example/.well-known/ai-transit.json
curl 'https://your-domain.example/api/public/transit/v1/snapshot?range=7d'
```

## 快照字段

| 字段 | 内容 |
| --- | --- |
| `schema_version` | `ai-transit.v1` |
| `system` | `sub2api` |
| `generated_at` | UTC RFC 3339 生成时间 |
| `monitoring` | 整体公开范围的来源、时间窗口、覆盖信息、指标、健康状态、趋势 |
| `groups` | 分组公开名称、平台、倍率、指标、健康状态、趋势 |
| `models` | 平台、模型名、指标、健康状态 |

`monitoring.source` 为 `channel-monitor-v2`。接口仅接受 `range` 参数，默认 `7d`，支持 v2 原生的 `90m`、`24h`、`7d` 和 `30d`。`group_id`、`platform`、`model`、`admin` 等参数不会改变公开范围。

`metrics` 字段包含：

- `success_rate`、`error_rate`、`cache_rate`：与 v2 相同的 0 到 1 比例，例如 `0.98` 表示 98%。
- `ttft`：首 Token 延迟的 `p50_ms`、`p90_ms`、`p95_ms` 和 `avg_ms`。
- `duration`：请求总耗时的相同延迟统计字段。

延迟单位均为毫秒；无样本的延迟为 `null`。健康状态包含 `overall`、`error_rate`、`ttft`、`cache` 和 `score`，直接沿用 v2 的健康判断。未知状态为 `unknown`，无有效得分时 `score` 为 `null`。

`monitoring.coverage` 包含请求窗口、实际覆盖开始时间、数据截止时间、计算时间、覆盖是否完整和时间桶秒数。`trend` 是每个时间桶的指标与健康状态。v2 会对齐时间桶，具体窗口应以 `requested_start` 和 `requested_end` 为准。

历史回填未完成时 `coverage_complete` 为 `false`，比例仅代表已有聚合数据。没有公开分组时，范围仍然受限，`groups`、`models` 和趋势返回空数组；不会退回全站或私有分组。没有数据时比例可能为 0，必须结合健康状态和覆盖信息解释，不能把未知数据解释为已确认故障。

## 可用性语义

本接口的成功率来自实际请求，沿用渠道状态 v2 的错误分类与忽略规则。忽略的错误可能不计入 `error_rate`，因此不要强制认为成功率和错误率之和一定为 1。

本实现没有使用旧版主动探测监控，也不生成探测 Ping、最近一次探测延迟或 15 天探测可用率。TTFT 和请求总耗时不能当作 Ping 延迟。

当前响应是 v2 监控公开投影，不是参考仓库的完整价格目录协议。它不包含旧协议中的 `station`、`billing`、分组模型价格、充值比例或 `monitoring` 数组；接收方应使用本文件描述的字段契约。旧参考页面未注册为当前前端路由，本次接口不提供 `/public/transit` 页面。

## 开关与错误

数据库设置 `public_transit_enabled` 控制三个接口。未配置时按现有行为默认开启；设置为 false 后返回 404，服务端无需重启。已被客户端或 CDN 缓存的成功响应可能保留至最多 60 秒缓存期结束。

- 公开开关关闭：404。
- 渠道状态 v2 配置关闭：快照返回 404；发现接口仍受公开开关控制。
- 不支持的 `range`：400。
- 数据源读取失败或返回不完整数据：500，响应使用通用错误信息。

错误响应设置 `Cache-Control: no-store`。快照不会把数据库错误、内部连接信息或异常详情返回给匿名调用方。

## 隐私边界

公开 DTO 独立定义，不嵌入内部 v2 DTO。新增内部字段不会自动进入公开协议。以下内容均不输出：

- 内部分组 ID、渠道 ID、账号 ID、用户 ID 和管理员 ID。
- 上游账号、Cookie、Access Token、Refresh Token、API Key、密钥和代理配置。
- 用户信息、余额、请求日志及原始错误详情。
- 请求数、Token 数、RPM、TPM、采样数和缓存比例的原始分子、分母。
- v2 内部配置、阈值、修改人和历史回填进度细节。

## 验证

```bash
cd backend
GOMAXPROCS=2 go test -p=1 ./internal/service ./internal/handler -run PublicTransit -count=1
GOMAXPROCS=2 go test -p=1 ./internal/server ./internal/server/routes
```

契约测试覆盖开关即时生效、公开路径一致性、错误与缓存头、真实 v2 Service 的公开分组范围，以及敏感字段不进入响应。
