# 发布阻断项收敛（924819c5 对照）

`report_revision=release-closure-r1`。本轮起点实际HEAD与用户验收快照相同：`924819c5d04a550801fb4e85441059389dc00caa`，分支`feat/multi-credential-http`，`git diff 924819c5..HEAD`为空。起点工作树仅有原有未跟踪`backend/internal/service/zz_debug_test.go`，不修改、不提交；最终构建/完整suite使用仅Git追踪内容的候选快照，避免把未追踪文件纳入证据。

已有其他工作流提交（包括`f416f1a93`及迁移260）保留。已经局部通过的advisory重试隔离、有界offered/fresh、取消名额和发送前期限检查列为回归约束，不再登记为未实现。

已完整读取用户`acceptance_report_924819c5.md`（92行）、原规格v1.0（850行）、当前AT账本、证据目录全部Markdown；全部JSON已逐对象解析（含所有矩阵/失败事件/漏洞调用链），阅读清单及文件摘要保存在`/tmp/sub2api-release-closure/read-evidence-inventory.json`。报告来源和既有证据不代替本轮实际运行。

本轮仅隔离本机测试，不操作生产；不扩展HA、跨地域双活或Redis Cluster。产品默认`gateway.multi_credential_http_enabled=false`；测试显式注入mock及测试开关只用于内部验收，真实provider身份和compact契约仍需可信外部证据。

## 工作与证据账本

| 类别 | 起点状态 | 本轮处理 |
|---|---|---|
| B1 身份/compact | BLOCKED | 无新可信契约，保持UNVERIFIED与能力门控，不使用mock证明真实认证 |
| B2 grpc/x/image五个公告 | 未升级 | 官方公告重核、最小依赖闭包升级、真实插件通信/头像/皮肤回归、govulncheck；独立提交和结果稍后填入 |
| B3 单主体成本 | 5ms重试；E4事务p95超20ms | 先固定二进制做受控对照，再最小优化，保留权限/账本/锁顺序/预算 |
| B4 候选快照 | 历史非integration suite仍失败 | 最终候选build、定向、race、完整suite和逐项基线对照；所有命令记录exit与artifact |
| B5 运维与秘密 | 三完整网关闭环、密钥轮换、旁路缺口 | 完整服务隔离演练、密钥轮换及分范围审计，由独立实现与证据关闭，不把组件替代系统验收 |

所有新测试和压测共用受控执行时段：依赖验证、集成构建、完整suite、控制实验、持续矩阵顺序运行。固定矩阵期间不运行编译/其他测试；若出现其他工作流争用，记录并停止将该运行称作独占可比证据。保留首次失败与同二进制复测，不覆盖历史失败。

20ms只对照权威准入事务p95；call、跨节点/本地等待、队列、Heartbeat、Finish和恢复耗时分别报告。未知执行不会为使账本归零而释放；有可靠receipt则幂等恢复本地完成/结算，不重放上游。
