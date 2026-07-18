# Prompt Injection 内容审计 V2 验收报告

日期：2026-07-17

实施依据：`prompt-injection-content-audit-implementation-plan-v2.md`

## 本地验收结论

- 已实现专用 Prompt Injection reviewer、严格 JSON schema/parser、确定性 post-policy、12K effective input、primary/fallback 相同 evidence。
- 已实现完整当前 source、head/hit/tail 单 JSON evidence、多 occurrence 覆盖、secret redaction、incomplete 禁止最终 allow。
- 已实现 cache V4；完整 source HMAC、policy/model route、instructions/schema/evidence revision 和完整性参与隔离。
- 已实现 `review/unavailable/incomplete` 的 pre-block fail-closed 503；不增加 violation count、不自动封禁、不发送违规邮件。
- 已实现全局 Prompt Injection baseline、no-hit 零模型调用、HTTP/WS/selected-account receipt 和 forward admission fail-closed。
- 已实现 NFKC、零宽、全角、compact 检测、原文 span、signal family、重复 occurrence 不重复计分及 terminal shadow eligibility。
- 已实现固定低基数 metrics、管理端配置透传与只读状态、运行语义和 coverage 文档。

## 自动化证据

- 真实 5.1K fixture 与去空格 compact 变体各执行 30 个独立 cache-key 用例：最终 allow 为 `0/60`。
- focused service：`ContentModeration|PromptInjection|SemanticReview` 通过。
- handler 全包测试通过。
- `promptfilter`、`moderationcoverage`、`server/routes` 全包测试通过。
- RiskControlView、risk-control locales、静态 locale key 共 25 个前端测试通过。
- `vue-tsc --noEmit`、`git diff --check`、coverage JSON 解析通过。
- 约 2K benign no-hit benchmark：`1.63-1.89ms/op`，低于 5ms 门槛。
- 12K evidence builder benchmark：`3.78-4.91ms/op`，低于 10ms 门槛。

## 尚需生产环境完成

以下是上线/运营 Gate，不属于本地代码验收，当前不得标记为通过：

- Spark、5.4 mini、5.6 luna 各 3 次真实 smoke request。
- 2K/6K/12K 随机交错 A/B，每档至少 30 组，并记录 cold/cache state。
- 不少于 300 条扩展攻击集和 1,000 条良性集的准确率统计。
- 至少 7 天、10,000 个线上 shadow 请求的 provider error、P95、准确率和 receipt 关联率。
- `pre_block` 5% -> 25% -> 100% 灰度，每档至少 24 小时。

任一确认攻击被转发、receipt 关联率低于 99.9%、provider error 连续 15 分钟超过 1%，或 cold P95 连续 15 分钟超过 8 秒，应立即停止升级或回滚。

## 仓库并行改动说明

最终复跑 service 全包测试时，工作树中另一项订阅开发为 `UserSubscriptionRepository` 新增了 `ActivatePendingRenewal`，但其多个旧测试 stub 尚未同步，导致 service 测试包在编译阶段失败。该问题与本次内容审计改动无关；本次相关 service 测试已在该并行接口变更前通过，生产源码随后也通过 `go build ./internal/service ./internal/handler ./internal/server/routes`。
