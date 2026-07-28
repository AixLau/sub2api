# 部署文档索引

当前通用部署与运维流程统一维护在 [`deploy/README.md`](../../deploy/README.md)。开始新部署、升级或回滚前，请先阅读该文档。

## 当前文档

| 文档 | 状态 | 用途 |
| --- | --- | --- |
| [`deploy/README.md`](../../deploy/README.md) | 当前 | Compose、systemd、远端发布、备份和回滚 |
| [`deploy/DOCKER.md`](../../deploy/DOCKER.md) | 当前 | Docker 镜像、标签、架构和健康检查 |
| [`deploy/DATAMANAGEMENTD_CN.md`](../../deploy/DATAMANAGEMENTD_CN.md) | 当前 | 数据管理宿主机进程 |
| [`deploy/react-landing-production.md`](../../deploy/react-landing-production.md) | 历史 | 仅供存量双服务环境切换到 Vue 参考 |

当前只保留 Vue 前端。首页、登录、注册、找回密码及其他公开页面均由 `frontend/` 中的 Vue 应用提供，并随 Sub2API 镜像一起发布；React 前端目录及双服务构建开关已经移除。

本地工作区若存在早期 HaozPay 部署包说明或单次服务器操作记录，它们属于历史材料，不纳入当前仓库部署文档。历史材料中的镜像标签、服务器地址、文件路径和命令可能已经失效，不应直接用于当前生产环境。
