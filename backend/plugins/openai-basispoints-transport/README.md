# Sub2API OpenAI OAuth Transport 插件

这是一个独立的 `.s2plugin` 插件工程。它只处理 Sub2API 已经选择并授权的 OpenAI OAuth 账号的出站 HTTP 请求：宿主通过插件协议传入账号身份，插件建立 HTTPS 连接并把原始响应流返回给宿主。

插件不会读取或保存 `refresh_token`，不会刷新 Token，也不会改变 Sub2API 的账号选择、计费、响应解析和失败切换逻辑。启用插件前，管理员仍需在 Sub2API 插件页面明确选择允许进入插件的 OpenAI OAuth 账号。

默认目标是 `https://bps.openai.com/basispoints/api`。来自 Sub2API 的 `/v1/responses`、`/backend-api/codex/responses` 等路径会映射为 Basis Points 的 `/responses` 路径；`compact` 等 Responses 子路径会保留。插件 UI 可以修改 Base URL、代理和连接参数。

## 本地构建

在仓库根目录执行：

```bash
cd backend/plugins/openai-basispoints-transport
TARGETS=linux-amd64 ./build.sh
```

默认构建未签名的开发包。开发环境需要在 Sub2API 配置中临时设置 `plugins.allow_unsigned: true`；生产包应使用受信任发布者的 Ed25519 私钥签名：

```bash
SIGNING_KEY=/secure/path/publisher.private \
KEY_ID=my-publisher-v1 \
TARGETS=linux-amd64 ./build.sh
```

签名私钥不应进入仓库、插件包或部署服务器。生产环境把对应公钥加入 `plugins.trusted_publishers` 后再安装包。

## 验证

```bash
cd backend
go test ./plugins/openai-basispoints-transport/...
```

插件的 `TestConfig` 只验证配置，不主动消耗账号额度；真实连通性会在绑定账号的实际请求中由宿主完成身份解析后验证。
