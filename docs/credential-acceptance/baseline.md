# 验收起点

分支 `feat/multi-credential-http`。验收收尾起点 `e00ea2cab7a9c5375d34ea911557948d6eea278f`；时间/性能专项起点 `5effa833a988b57ec977a93481be2f75450f8edd`。

证据版本分开记录：

- `tested_code_sha`：该次运行对应的产品代码提交；
- `benchmark_code_sha`：发生器/测量程序提交，若使用产品代码较早的版本则明确叠加测试文件；
- `report_revision`：报告版本号，当前为 `admission-time-performance-r1`，不等于受测代码HEAD；
- `artifacts`：原始日志、命令、环境、摘要及内容散列，见 [专项报告](admission-time-performance.md) 和 [证据清单](evidence-manifest.json)。

历史完整suite记录的代码快照为 `8c1bfcbff45883c730e37f6382c4c9fd70344122`；`8c1bfcbff..5effa833` 与 `01fbfcd6c..5effa833` 的 backend/frontend 差异均为空。因此旧文档把多个文档提交称为“当前最终HEAD”不代表测试代码改变。专项修改执行代码后的证据另列，不把历史全套结果当作本专项最终代码的全套结果。

对照基线：`fde7e8ec4ff9af1b2645661d6a28cec19f6b347f`；规格基线：`9bdb388b83f05e678e83990d9b19afbc3f088a8f`。

规格 v1.0，850 行，SHA256 `5c5226eca2e90a7d33a113d8b42b1cd1abd19d852d46d298ca5584e256bede40`。已完整读取规格、实施记录和旧验收账本。

原八个实现提交（按时间顺序，验收收尾在其后继续提交）：

- `1fda0009220d440bcdaec3373187305c37f7f3dd` feat(credentials): add inactive principal control model and admin reads
- `9ba3d5531bf9276a044cb06f3fbc20eb191ef226` feat(credentials): add encrypted imports and verified instance creation
- `5164ee3086ac6ffa94f3ae809ae296319738121f` feat(admission): add transactional capacity ledger and fenced leases
- `ce28a66dca74c589074a7f31b9525a612bf47f47` feat(scheduler): add hard bindings and demand-aware admission queue
- `7bbd41b2df9d8027363bb2c89788f5361d78353c` feat(credentials): integrate fenced HTTP snapshots and refresh ledger
- `7261721fc333069fdaf697b7b5574b21d09294e4` feat(credentials): add versioned controls and orphan reconciliation
- `4af6dd136bd1b12854f5589e480d1c3932d5236e` feat(credentials): add migration review and close HTTP lifecycle gaps
- `e00ea2cab7a9c5375d34ea911557948d6eea278f` docs(credentials): record full test and release gate results

原有未跟踪 `backend/internal/service/zz_debug_test.go` 不编辑、不提交；对照测试使用独立提交快照排除该文件。

拓扑声明：验收目标是同一部署/管理作用域、一个 PostgreSQL 主库、一个共享 Redis、至少三个同机网关进程；第一版不支持PostgreSQL自动HA/只读副本准入、Redis Cluster/HA、跨地域双活，不能把这些拓扑当作本期必须追加实现的范围。切换到未支持拓扑须单独验收，禁止沿用本报告放行。身份 verifier 和真实 compact 契约仍缺失；真实导入 UNVERIFIED、默认关闭。


验收收尾提交（不计入原八个实现提交）：

- `e00ea2cab7a9c5375d34ea911557948d6eea278f`：实施记录/发布门槛
- `952fede19c76fd92ee601d0a674bd2323f3f765d`：固定验收基线和系统验收边界
- `5f335e11be33de96bd5a11dbc5247bf3f194f476`：完整 Gin+PG+Redis+HTTP mock+计费联测
- `47a53c0093ab0e1ba0cd36da2061daf20675caf0`：失败对照和漏洞处置文档
- `515b23bc9e7fe23818dc8c7447dc554779d5acc1`：usage receipt/计费恢复
- `393655edd396e8dec31a83a1d90f531d8deeae96`：Redis user hold/epoch fencing
- `039ef1cf281836e4ab93069cacf095a63d8266f1`：Docker fencing/迁移回滚工具
- `85c47db9fc8b85bc2e304ebf2ebb4b1dd8306793`：集成容器清理
- `b80c9b9a031854f363b327cac18c2b5ac5365cb3`：最终 HEAD 失败对照
- `53164d062b3c88f6f2f4f7cb16d8483cc6392c5f`：修正持续压测发生器
- `8434b67482b89a70b7a8a310ea94bd9843e3e108`：旧二进制 fencing 测试
- `01fbfcd6c`：逐项 AT 矩阵和生产条件
