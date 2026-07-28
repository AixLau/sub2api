# React 官网 + Sub2API 双服务历史迁移说明（已弃用）

> **移除通知：** React 官网目录、条件路由和构建开关均已从当前代码中移除。首页、登录、注册、找回密码及其他公开页面只由仓库 `frontend/` 中的 Vue 应用提供。新部署请使用 [`deploy/README.md`](./README.md) 中的标准一体化方案。

本文档仅保留给仍在运行旧 React 双服务架构的环境，用于识别旧配置并迁移回 Vue 一体化部署。以下 React 构建、发布、路由和回滚命令均为历史信息，不适用于新部署。

## 迁移到 Vue 一体化部署

1. 使用当前代码重新构建并发布 Sub2API 镜像。当前镜像只包含 Vue 前端，不再接受 React 路由构建参数。
2. 从 Caddy/Nginx 配置中移除 React 静态目录、`/landing-assets/*` 和 `@react_landing` 等专用处理，让 `/`、`/home`、`/login`、`/register`、`/forgot-password`、`/reset-password`、`/model-market`、`/services`、`/service-status`、`/faq` 回源到 Sub2API。
3. 保留 `/api/*`、`/v1/*` 等 API 反向代理规则，并按实际部署保留独立文档站等无关路由。
4. 校验并重载反向代理，随后检查首页、登录页、注册页和健康接口。

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsSI https://example.com/
curl -fsSI https://example.com/login
curl -fsSI https://example.com/register
```

确认 Vue 页面正常后，再按运维流程下线旧 React 静态站。不要在迁移验证完成前清理旧产物。

## 以下为历史方案

旧方案曾由独立 React 静态站接管官网和认证入口，Sub2API Vue 应用只提供控制台等其余页面。该兼容模式已从代码中移除。

## 旧方案拓扑

| 层级 | 历史约定 |
|------|----------|
| 公网域名 | `https://aixlau.me` |
| 反向代理 | Caddy，配置在服务器 `/etc/caddy/Caddyfile` |
| React 官网静态目录 | `/var/www/aixlau.me/landing` |
| React 资源前缀 | `/landing-assets/*` |
| Sub2API 服务 | Docker Compose，`sub2api` 容器监听宿主机 `127.0.0.1:8080` 或 compose 映射端口 |
| Sub2API 部署目录 | `/opt/sub2api/deploy` |
| 数据存储 | 复用生产 PostgreSQL 和 Redis；不要在测试镜像部署时新建空库替换它们 |

## 路由归属

| 路径 | 归属 |
|------|------|
| `/`, `/home` | React 官网 |
| `/model-market`, `/services`, `/service-status`, `/faq` | React 官网独立业务页面 |
| `/login`, `/register`, `/forgot-password`, `/reset-password`, `/change-password` | React 官网认证入口 |
| `/landing-assets/*` | React 官网构建资源 |
| `/dashboard`, `/keys`, `/usage`, `/profile`, `/admin/*` | Sub2API Vue 控制台 |
| `/auth/*`, `/email-verify`, `/payment/*`, `/legal/*` | Sub2API |
| `/api/*`, `/v1/*`, `/responses*`, `/chat/completions*`, `/embeddings*`, `/images/*`, `/anthropic/*`, `/claude/*`, `/gemini/*`, `/v1beta/*` | Sub2API 后端 API |

React 登录、注册、找回密码页面必须使用同源 API，例如 `/api/v1/auth/login`、`/api/v1/auth/register`、`/api/v1/auth/send-verify-code`、`/api/v1/auth/forgot-password`。登录成功后保持 Sub2API 前端已有的浏览器存储键：`auth_token`、`refresh_token`、`auth_user`、`token_expires_at`。

React 登录和注册入口还必须接入 Sub2API 后端的登录条款确认。页面加载时调用 `/api/v1/settings/public`，读取 `login_agreement_enabled`、`login_agreement_mode`、`login_agreement_updated_at`、`login_agreement_revision`、`login_agreement_documents`。当 `login_agreement_enabled=true` 且文档列表非空时，在提交登录、注册、第三方快捷登录前要求用户确认：

```text
我已阅读并同意 服务条款、使用政策、支持的国家和地区、服务特定条款
```

文档链接指向 Sub2API 已有公开路由 `/legal/{document.id}`，例如 `/legal/terms`、`/legal/usage-policy`、`/legal/supported-regions`、`/legal/service-specific-terms`。用户确认后使用与 Vue 控制台相同的 localStorage 键 `sub2api_login_agreement_consent` 保存当前 revision：

```json
{"revision":"<login_agreement_revision>","accepted_at":"<ISO timestamp>"}
```

如果后端返回的 revision 和本地保存值不一致，必须重新要求确认；未确认前应禁用或拦截账号密码登录、注册、第三方快捷登录，并给出“请先阅读并同意最新条款”的提示。若 `login_agreement_revision` 为空，可按 Vue 控制台兜底规则拼接：`login_agreement_updated_at + ":" + documents.map(doc => doc.id + ":" + doc.title).join("|")`。

## Caddy 配置模板

以下模板保留 Sub2API 原路由，不添加 `/console` 前缀，并让 React 只接管明确的官网入口。

```caddyfile
aixlau.me {
	request_body {
		max_size 256MB
	}

	header {
		Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
		X-Content-Type-Options "nosniff"
		Referrer-Policy "strict-origin-when-cross-origin"
	}

	@landing_static {
		path /landing-assets/*
	}
	header @landing_static Cache-Control "public, max-age=31536000, immutable"
	handle @landing_static {
		root * /var/www/aixlau.me/landing
		file_server
	}

	@seo_static {
		path /robots.txt /sitemap.xml /llms.txt /og-image.png
	}
	header @seo_static Cache-Control "public, max-age=3600"
	handle @seo_static {
		root * /var/www/aixlau.me/landing
		file_server
	}

	@react_landing {
		path / /home /model-market /services /service-status /faq /login /register /forgot-password /reset-password /change-password
	}
	handle @react_landing {
		root * /var/www/aixlau.me/landing
		try_files {path} /index.html
		file_server
	}

	redir /docs/install /docs/install/ 308
	handle_path /docs/install/* {
		root * /var/www/aixlau.me/docs/install
		file_server
	}

	@api {
		path /v1/*
		path /api/v1/*
		path /responses
		path /responses/*
		path /chat/completions
		path /chat/completions/*
		path /embeddings
		path /embeddings/*
		path /images/*
		path /anthropic/*
		path /claude/*
		path /gemini/*
		path /v1beta/*
	}
	handle @api {
		reverse_proxy 127.0.0.1:8080 {
			flush_interval -1
			header_up X-Real-IP {remote_host}
			header_up CF-Connecting-IP {http.request.header.CF-Connecting-IP}
			header_down X-Accel-Buffering "no"
			transport http {
				keepalive 120s
				keepalive_idle_conns 256
				read_timeout 900s
				write_timeout 900s
			}
		}
	}

	handle {
		reverse_proxy 127.0.0.1:8080
	}
}
```

修改 Caddy 后先校验再重载：

```bash
sudo caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

## 旧 React 官网发布流程

旧 React 官网项目位于本仓库 `react-frontend/`，仅供尚未完成迁移的环境临时维护。不要在其中继续开发首页或认证页面；相关改动应在 `frontend/` 中进行。旧构建产物使用 `landing-assets` 作为资源目录，以避免和 Sub2API Vue 控制台的 `/assets/*` 冲突。

> SEO 发布要求：凡修改 `react-frontend/index.html`、`react-frontend/src/lib/seo.ts` 或 `react-frontend/public/` 下的 SEO 文件，都必须重新构建并上传整个 React `dist/`。只部署 Sub2API Docker 镜像不会更新官网首页的 title、canonical、Open Graph 或官网静态资源。

```bash
cd /path/to/sub2api/react-frontend
npm test
npm run typecheck
npm run build

# 覆盖上传静态产物；这是 overlay，不会删除服务器上其他文件。
tar -C dist -czf - . | ssh sub2api-server \
  'mkdir -p /var/www/aixlau.me/landing && tar -C /var/www/aixlau.me/landing -xzf -'
```

发布后检查：

```bash
curl -fsSL https://aixlau.me/ | rg -o 'landing-assets/[^" ]+'
curl -I https://aixlau.me/landing-assets/index-*.js
curl -I https://aixlau.me/model-market
curl -I https://aixlau.me/faq
curl -I https://aixlau.me/register
```

SEO 相关发布还要检查首页 head 和抓取入口：

```bash
curl -fsSL https://aixlau.me/ | rg -n 'GPT API 中转站|canonical|application/ld\+json'
curl -fsSL https://aixlau.me/robots.txt
curl -fsSL https://aixlau.me/sitemap.xml
curl -fsSL https://aixlau.me/llms.txt
curl -fsSI https://aixlau.me/og-image.png
```

如果本次修改包含 `docs/install/index.html`，该文档由 Caddy 单独托管，也要单独覆盖上传：

```bash
tar -C docs/install -czf - . | ssh sub2api-server \
  'mkdir -p /var/www/aixlau.me/docs/install && tar -C /var/www/aixlau.me/docs/install -xzf -'
curl -fsSL https://aixlau.me/docs/install/ | rg -n 'canonical|TechArticle|FAQPage'
```

如果需要清理旧的 hashed 静态资源，只能在确认当前 HTML 引用的新资源可访问后，人工列出待删文件并按删除安全流程执行。不要用自动同步删除命令直接覆盖生产静态目录。

## 旧双服务镜像发布流程

旧双服务生产环境使用 Docker Compose，并复用现有 PostgreSQL 和 Redis 数据目录。

以下命令是已经失效的历史记录，当前代码无法再构建双站点兼容镜像：

```bash
docker buildx build \
  --platform linux/amd64 \
  --build-arg VITE_REACT_LANDING_ROUTES=true \
  -t sub2api:<commit>-amd64 \
  -f Dockerfile . \
  --load
```

```bash
ssh sub2api-server
cd /opt/sub2api/deploy

# 发布前记录当前状态。
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs --tail=80 sub2api

# 更新 compose 中 sub2api 镜像标签前先备份配置。
cp docker-compose.local.yml docker-compose.local.yml.backup-$(date +%Y%m%d-%H%M%S)

# 修改 image 标签后，只重启 sub2api，避免无谓重建数据库和 Redis。
docker compose -f docker-compose.local.yml up -d sub2api
```

发布后检查：

```bash
docker compose -f docker-compose.local.yml ps
docker compose -f docker-compose.local.yml logs --tail=120 sub2api
curl -fsS http://127.0.0.1:8080/health
curl -fsSI https://aixlau.me/api/v1/auth/login
```

## 回滚

React 官网回滚：

1. 找到上一版 React `dist/` 产物或重新构建上一版提交。
2. 使用同样的 `tar -C dist ...` 命令覆盖 `/var/www/aixlau.me/landing`。
3. 确认 `https://aixlau.me/` 的 HTML 指向回滚后的资源。

Sub2API 回滚：

```bash
ssh sub2api-server
cd /opt/sub2api/deploy
cp docker-compose.local.yml.backup-YYYYMMDD-HHMMSS docker-compose.local.yml
docker compose -f docker-compose.local.yml up -d sub2api
docker compose -f docker-compose.local.yml logs --tail=120 sub2api
```

数据库迁移是前向迁移。涉及迁移的版本回滚前，必须先确认迁移是否兼容旧代码；不兼容时应从数据库备份恢复，或编写补偿 SQL。

## Cloudflare 建议

旧 React 官网页面是纯静态 HTML。若 Cloudflare 对官网公开页或认证入口返回 `cf-cache-status: DYNAMIC`，首包时间会受回源链路和防护脚本影响。

建议：

- 对 `/landing-assets/*` 使用长期缓存，已由 Caddy 设置 `Cache-Control: public, max-age=31536000, immutable`。
- 对 Caddy `@react_landing` 中明确列出的 React 路径配置 Cloudflare Cache Rule，例如 `Cache Everything`，但必须排除 `/api/*`、`/v1/*`、`/dashboard*`、`/admin*` 等动态/API 路径。
- 官网入口如果启用了 Bot Fight Mode、Managed Challenge 或 JavaScript Detection，可能注入 `/cdn-cgi/challenge-platform/scripts/jsd/main.js`，会增加首屏请求和执行成本。可按安全策略对纯静态官网路径降低挑战强度，API 和控制台路径保持严格策略。

## 常见问题

### 前端 404

检查 Caddy 中 `@react_landing` 是否包含当前 React 路由，且 `try_files {path} /index.html` 是否存在。只把 React 拥有的入口路由加进去，不要把 `/dashboard`、`/admin` 等 Sub2API 控制台路由转给 React。

### 登录/注册接口 404 或 CORS

React 官网应与 Sub2API API 同源部署，请求路径使用 `/api/v1/...`。如果 React 使用单独域名，需要额外配置 CORS，但生产推荐同源路径转发。

### 注册提示需要邮箱验证

React 注册页要先调用 `/api/v1/auth/send-verify-code` 获取邮箱验证码，再携带 `verify_code` 调用 `/api/v1/auth/register`。

### 页面慢

先区分 origin 与边缘网络：

```bash
# 服务器上测 origin，通常应为毫秒级。
curl -sS -o /dev/null -w 'ttfb=%{time_starttransfer}s total=%{time_total}s\n' \
  -H 'Host: aixlau.me' http://127.0.0.1/

# 外部测公网。
curl -L --compressed -sS -o /dev/null -w 'ttfb=%{time_starttransfer}s total=%{time_total}s\n' \
  https://aixlau.me/
```

若 origin 很快但公网慢，优先检查 Cloudflare 缓存状态、防护脚本、线路和 DNS；不要先怀疑 PostgreSQL 或 Redis。
