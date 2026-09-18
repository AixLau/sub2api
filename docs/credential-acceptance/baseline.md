# 验收起点

验收开始 HEAD：`e00ea2cab7a9c5375d34ea911557948d6eea278f`。分支 `feat/multi-credential-http`。

对照基线：`fde7e8ec4ff9af1b2645661d6a28cec19f6b347f`；规格基线：`9bdb388b83f05e678e83990d9b19afbc3f088a8f`。

规格 v1.0，850 行，SHA256 `5c5226eca2e90a7d33a113d8b42b1cd1abd19d852d46d298ca5584e256bede40`。已完整读取规格、实施记录和旧验收账本。

八个原始提交（按时间顺序）：

- `1fda0009220d440bcdaec3373187305c37f7f3dd` feat(credentials): add inactive principal control model and admin reads
- `9ba3d5531bf9276a044cb06f3fbc20eb191ef226` feat(credentials): add encrypted imports and verified instance creation
- `5164ee3086ac6ffa94f3ae809ae296319738121f` feat(admission): add transactional capacity ledger and fenced leases
- `ce28a66dca74c589074a7f31b9525a612bf47f47` feat(scheduler): add hard bindings and demand-aware admission queue
- `7bbd41b2df9d8027363bb2c89788f5361d78353c` feat(credentials): integrate fenced HTTP snapshots and refresh ledger
- `7261721fc333069fdaf697b7b5574b21d09294e4` feat(credentials): add versioned controls and orphan reconciliation
- `4af6dd136bd1b12854f5589e480d1c3932d5236e` feat(credentials): add migration review and close HTTP lifecycle gaps
- `e00ea2cab7a9c5375d34ea911557948d6eea278f` docs(credentials): record full test and release gate results

原有未跟踪 `backend/internal/service/zz_debug_test.go` 不编辑、不提交；对照测试使用独立提交快照排除该文件。

拓扑声明：验收目标是同一部署/管理作用域、一个 PostgreSQL 主库、一个共享 Redis、至少三个同机网关进程；HA、跨地域双活均不在已验证拓扑。身份 verifier 和真实 compact 契约仍缺失；真实导入 UNVERIFIED、默认关闭。
