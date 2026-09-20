# 本地源码运行

在仓库根目录执行：

```bash
python3 deploy/dev-source.py start
```

需要本机安装 Go、Node.js 和 pnpm，并已执行 `pnpm --dir frontend install --frozen-lockfile`。
启动器复用现有 `sub2api-dev` 容器的环境配置和 `/app/data` 挂载目录，连接现有的
`sub2api-postgres-dev`、`sub2api-redis-dev`，不创建数据库或重置管理员密码。
依赖容器需要已经运行且可从宿主机访问；支持已映射的端口、OrbStack 的 `.orb.local` 域名和可路由的容器 IP。

- 前端：<http://127.0.0.1:8080>，Vite 热更新，修改 Vue/TypeScript 文件即时生效。
- 后端：<http://127.0.0.1:8081>，使用本机 Go 缓存编译，不构建镜像、不打包前端。
- 前端的 `/api`、`/v1`（含 WebSocket）、`/setup`、`/health` 请求转发到后端。
- 后端通过健康检查后，启动器停止 Docker 应用容器，PostgreSQL 和 Redis 继续运行。
- 配置和密钥从现有本地部署读取；进程记录、日志和本地二进制位于 Git 忽略的 `.dev/source/`。

修改 Go 代码后重新启动，编译完成前仍保留当前服务：

```bash
python3 deploy/dev-source.py restart
python3 deploy/dev-source.py status
python3 deploy/dev-source.py stop
```

`stop` 只停止源码前后端。容器名称和端口可以通过 `--help` 中的参数调整。
数据库内的账号、分组和其他数据与之前相同，原登录账号仍然有效。
