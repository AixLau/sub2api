# Sub2API 部署指南

本目录是 Sub2API 部署文件的权威入口。生产环境优先使用 Docker Compose；Apple silicon Mac 本地运行可使用 Apple `container`；需要直接管理进程时再使用 systemd 二进制安装。

当前版本只保留仓库 `frontend/` 中的 Vue 前端。首页、登录、注册、找回密码和其他公开页面均由 Sub2API 内嵌的 Vue 应用提供；React 官网及其双服务兼容代码已经移除。

## 选择部署方式

| 场景 | 入口 | 数据存储 | 说明 |
| --- | --- | --- | --- |
| 新服务器，一体化部署 | `docker-deploy.sh` + `docker-compose.local.yml` | 当前目录 | 推荐，便于备份和迁移 |
| 简单的一体化部署 | `docker-compose.yml` | Docker 命名卷 | 数据由 Docker 管理 |
| Apple silicon Mac 本地运行 | `apple-container.sh` | Apple 命名卷 | 需要 macOS 26 和 Apple `container` 1.1.0 或更高版本 |
| 已有 PostgreSQL/Redis | `docker-compose.standalone.yml` | 应用命名卷 | 只启动 Sub2API |
| 本地容器开发 | `docker-compose.dev.yml` | 当前目录 | 从本地源码构建 |
| 裸机/systemd | `install.sh` | `/opt/sub2api` | 不使用 Docker |
| 已有 Compose 实例发布自定义代码 | `remote-compose-deploy.sh` | 保持现有配置 | 本地构建 amd64 镜像，只替换应用服务 |

不要混用不同 Compose 文件。选定一种后，后续命令始终显式传递同一个 `-f` 参数。

## 文件说明

| 文件 | 用途 |
| --- | --- |
| `.env.example` | 环境变量模板，不包含真实凭据 |
| `docker-compose.local.yml` | PostgreSQL、Redis、Sub2API，使用本地目录持久化 |
| `docker-compose.yml` | PostgreSQL、Redis、Sub2API，使用命名卷 |
| `docker-compose.standalone.yml` | 连接外部 PostgreSQL 和 Redis |
| `docker-compose.dev.yml` | 本地源码构建与调试 |
| `docker-deploy.sh` | 首次部署准备，生成 `.env` 和数据目录 |
| `apple-container.sh` | Apple `container` 初始化、启动、停止和状态管理脚本 |
| `APPLE_CONTAINER.md` | Apple `container` 配置、升级、持久化、网络和限制说明 |
| `remote-compose-deploy.sh` | 本地构建并发布到已有远端 Compose 实例 |
| `build_image.sh` | 本地快速构建 `sub2api:latest` |
| `install.sh` | 二进制安装、升级和卸载 |
| `DOCKER.md` | 已发布 Docker 镜像的使用说明 |
| `DATAMANAGEMENTD_CN.md` | 宿主机数据管理进程说明 |
| `EDGE_SECURITY.md` | 反向代理、CDN/WAF、可信代理与入口安全加固说明 |
| `react-landing-production.md` | 已移除方案的迁移记录，仅供存量环境切换到 Vue 参考 |

运行数据、`.env`、备份和构建产物不得提交到 Git。

## 前端部署约定

- Vue 始终注册并提供 `/`、`/home`、`/login`、`/register`、`/forgot-password`、`/reset-password`、`/model-market`、`/services`、`/service-status` 和 `/faq`。
- React 路由构建开关已经移除，所有镜像均按 Vue 单服务模式构建。
- Docker 镜像已包含构建后的 Vue 前端，不需要在 Caddy/Nginx 中为首页或认证页面配置单独的静态站点。反向代理应将这些请求交给 Sub2API 服务。
- 首页、登录页及其他公开页面的后续修改统一在 `frontend/` 中进行。

仍在运行 React 双服务方案的环境，迁移步骤见 [React 双服务历史迁移说明](./react-landing-production.md)。

## Apple container 部署

Apple silicon Mac 在 macOS 26 上安装 Apple `container` 1.1.0 或更高版本后，可以在本机运行完整的 Sub2API、PostgreSQL 和 Redis 服务栈：

```bash
./apple-container.sh init
./apple-container.sh up
./apple-container.sh status
./apple-container.sh logs app -f
```

脚本使用 Apple 命名卷持久化数据，按依赖顺序启动服务，并执行实时就绪检查。它不提供持续运行的重启守护；宿主机重启后需要再次运行 `./apple-container.sh up`。生产环境仍推荐使用 Docker Compose。

配置、升级、持久化、网络行为和限制详见 [Apple container 部署与运维指南](./APPLE_CONTAINER.md)。

---

## 首次 Docker Compose 部署

### 前置条件

- Linux 服务器
- Docker Engine 24 或更高版本
- Docker Compose v2，即 `docker compose`
- `curl` 或 `wget`
- `openssl`

### 一键准备

在空目录中运行：

```bash
mkdir -p sub2api-deploy
cd sub2api-deploy
curl -fsSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-deploy.sh | bash
docker compose -f docker-compose.yml up -d
```

准备脚本会下载本地目录版 Compose 文件并保存为 `docker-compose.yml`，生成 `.env`，创建 `data/`、`postgres_data/`、`redis_data/`。生成的凭据只保存在当前服务器的 `.env` 中。

启动后验证：

```bash
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs --tail=100 sub2api
curl -fsS http://127.0.0.1:8080/health
```

预期健康响应：

```json
{"status":"ok"}
```

如果未在 `.env` 中设置 `ADMIN_PASSWORD`，首次启动会生成管理员密码：

```bash
docker compose -f docker-compose.yml logs sub2api | grep "admin password"
```

### 仓库内手动启动

```bash
cd deploy
cp .env.example .env
chmod 600 .env
```

至少设置以下值，不要把真实值提交到仓库：

```dotenv
POSTGRES_PASSWORD=<openssl rand -hex 32 的输出>
JWT_SECRET=<openssl rand -hex 32 的输出>
TOTP_ENCRYPTION_KEY=<openssl rand -hex 32 的输出>
```

然后启动本地目录版：

```bash
mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
curl -fsS http://127.0.0.1:8080/health
```

## 已有 Compose 实例升级

### 使用官方镜像

先备份数据库，再拉取镜像并只重建应用服务：

```bash
cd /path/to/deploy
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
docker compose -f docker-compose.local.yml ps sub2api
curl -fsS http://127.0.0.1:8080/health
```

`--no-deps` 可避免无必要地重建 PostgreSQL 和 Redis。数据库迁移是前向执行的；发布前应准备可验证的数据库备份，回滚应用镜像不等于回滚数据库。

### 发布当前仓库代码

`remote-compose-deploy.sh` 在本机完成 `linux/amd64` 镜像构建和压缩，通过 SSH 上传，在服务器执行 `docker load`，更新 Compose 中 Sub2API 的镜像标签，并只重建目标服务。

前置条件：

- 本地 Docker daemon 可用
- 本地能通过 SSH key 或 ssh-agent 登录目标服务器
- 本地和远端均安装 `rsync`
- 远端已有可工作的 Compose 部署
- Git 已提交，且被跟踪文件没有未提交改动

```bash
./deploy/remote-compose-deploy.sh \
  --remote user@example.com \
  --compose-dir /opt/sub2api/deploy \
  --compose-file docker-compose.local.yml
```

常用参数：

```text
--remote TARGET       必填，SSH 目标
--compose-dir PATH    远端 Compose 目录，默认 /opt/sub2api/deploy
--compose-file FILE   Compose 文件，默认 docker-compose.local.yml
--service NAME        应用服务和容器名，默认 sub2api
--image-repo NAME     镜像仓库名，默认 sub2api
--health-url URL      远端健康地址，默认 http://127.0.0.1:8080/health
--allow-dirty         明确允许被跟踪文件存在未提交改动
--skip-build          使用本地已存在的目标镜像
```

镜像标签格式为 `sub2api:<git-short-sha>-amd64`。传输使用压缩归档和断点续传；重复运行会从远端已有的部分文件继续。

脚本不会：

- 在远端从源码构建
- 输出远端 `.env`
- 重启 PostgreSQL 或 Redis
- 修改卷和数据目录
- 删除本地或远端归档

## Standalone 部署

外部 PostgreSQL 和 Redis 已由其他系统管理时使用：

```bash
cd deploy
cp .env.example .env
chmod 600 .env
# 设置 DATABASE_*、REDIS_*、JWT_SECRET 和 TOTP_ENCRYPTION_KEY
docker compose -f docker-compose.standalone.yml up -d
```

不要让容器通过公网明文访问数据库。优先使用私有网络，并按实际环境配置数据库 TLS 和 Redis TLS。

## 本地容器开发

```bash
cd deploy
cp .env.example .env
docker compose -f docker-compose.dev.yml up --build
```

开发配置默认只将应用端口绑定到 `127.0.0.1`。其中的代理和批量图片参数是本地开发默认值，生产环境不要直接复用。

## systemd 二进制安装

```bash
curl -fsSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/install.sh | sudo bash
```

常用操作：

```bash
sudo systemctl status sub2api
sudo journalctl -u sub2api -n 100 --no-pager
sudo systemctl restart sub2api
curl -fsS http://127.0.0.1:8080/health
```

完整参数请运行：

```bash
sudo bash deploy/install.sh --help
```

## 日常运维

以下示例以 `docker-compose.local.yml` 为准：

```bash
cd deploy

# 状态与健康
docker compose -f docker-compose.local.yml ps
curl -fsS http://127.0.0.1:8080/health

# 应用日志
docker compose -f docker-compose.local.yml logs --tail=100 sub2api
docker compose -f docker-compose.local.yml logs -f sub2api

# 只重启应用
docker compose -f docker-compose.local.yml restart sub2api

# 非破坏性更新应用镜像
docker compose -f docker-compose.local.yml pull sub2api
docker compose -f docker-compose.local.yml up -d --no-deps sub2api

# 检查依赖服务
docker compose -f docker-compose.local.yml exec postgres pg_isready
docker compose -f docker-compose.local.yml exec redis redis-cli ping
```

可选的 `UPDATE_GITHUB_TOKEN` 仅用于访问 `api.github.com` 检查版本，发布资产下载仍保持匿名。令牌应只写入服务器 `.env`，不要提交到仓库或输出到日志。

### 备份与迁移

不要把运行中的 PostgreSQL 数据目录直接打包作为唯一备份。生产环境应使用管理后台数据管理功能或 `pg_dump` 生成一致性备份，并定期做恢复演练。

迁移前至少保存：

- PostgreSQL 逻辑备份
- `.env`，通过安全渠道单独保存
- `data/` 中的应用文件
- 当前 Compose 文件和镜像标签

### 回滚

1. 确认前一镜像仍存在于服务器。
2. 将 Compose 中 Sub2API 的 `image` 改回前一标签。
3. 只重建应用并检查健康。

```bash
docker compose -f docker-compose.local.yml up -d --no-deps sub2api
docker compose -f docker-compose.local.yml ps sub2api
curl -fsS http://127.0.0.1:8080/health
```

如果新版本已经执行不兼容的数据库迁移，应按迁移说明恢复数据库备份，不能只切换旧镜像。

## 安全检查

- `.env` 权限建议为 `0600`，禁止提交或粘贴到工单、聊天和日志中。
- 固定 `JWT_SECRET` 和 `TOTP_ENCRYPTION_KEY`；变更会影响登录会话或双因素认证数据。
- 生产环境将 `BIND_HOST` 设为 `127.0.0.1`，由 Caddy/Nginx 提供 TLS，或使用严格配置的防火墙。
- 发布前备份数据库，发布后同时检查 Docker health 和 `/health`。
- 不使用 `docker compose down -v`，除非明确要销毁全部数据。
- 不直接删除 `data/`、`postgres_data/`、`redis_data/`。
- 不在远端服务器构建来源不明或未提交的代码。

## 故障排查

应用不健康时先限制检查范围：

```bash
docker inspect sub2api --format '{{.Config.Image}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}'
docker logs --tail=120 sub2api
curl -v http://127.0.0.1:8080/health
```

上传中断时，重新运行 `remote-compose-deploy.sh` 即可断点续传。不要因为上传慢而在服务器重新构建。

更多专题：

- [Docker 镜像说明](./DOCKER.md)
- [数据管理进程](./DATAMANAGEMENTD_CN.md)
- [旧双服务迁移到 Vue 的记录](./react-landing-production.md)
- [部署文档索引](../docs/deployment/README.md)
