# 发布候选全套失败归因

本文件在候选代码冻结前记录证据处理方法和已完成的静态归因；不把旧快照的运行冒充本轮 `924819c5d04a550801fb4e85441059389dc00caa` 对照或最终候选验收。最终候选的实际命令、退出码和逐项结果待执行后追加。业务代码、旧协议及既有隐私清理没有因本分析修改。

## 证据规则

`scripts/acceptance/compare_go_test_json.py` 分别保留 package、root test、subtest 的事件与计数。每个测试的 `run` 必须有自己的 `pass/fail/skip` 才算终结；包因另一测试 panic 而终止时，尚未终结的测试是 `INCOMPLETE`。未出现在某一运行中的测试是 `NOT_RUN`，不是 PASS。两个日志都未出现的测试无法仅靠日志枚举。

比较器的 `FAIL_TO_FAIL` 只是观测结果，`attribution` 默认 `UNATTRIBUTED`，`acceptance` 保持 `NOT_PASS`。只有逐项对照断言、实际值、栈和相关代码后，才标记“已有测试缺陷”“断言契约变化”“已有业务缺陷”“环境相关”或“未归因”。新增失败也不能仅因旧运行被 panic 截断就判定为新增回归。原始日志保留路径和 SHA-256；报告不复制任意运行时日志，避免将秘密写入 Git。

解析器的六项定向测试已执行：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s scripts/acceptance -p 'test_compare_go_test_json.py' -v
```

退出 0；覆盖 panic 后平行测试未终结、相同失败不可写成 PASS、子测试独立计数、候选独有失败不可直接归因、重复运行不得覆盖失败、空日志及构建失败不得捏造测试结果。这是证据处理工具验证，不是产品验收。

## 已有历史证据的静态复核

只读解析历史 `fde7e8ec4ff9af1b2645661d6a28cec19f6b347f` 与 `e2a2342228c58074dca9b0d8ede5968ec3b44c6b` 两份完整日志，得到 81 个两边均失败事件（包含包级及父子事件）和 141 个两边均未终结测试。不能把 81 当成 81 个独立根因，也不能将未终结测试计为通过。产物为 `/tmp/sub2api-release-closure/historical-failure-audit.json`，仅供交叉检查历史账本；本次未重新执行这些旧版本测试。

| 路径 / 测试家族 | 已实际观察的断言或栈 | 静态原因及归因边界 |
|---|---|---|
| `TestSetOpenAIFastPolicySettings_Validation` | `openai_fast_policy_test.go:589`，`index out of range [2] with length 2` | 输入只有两条规则；测试先 `require.Len(...,2)`，随后读取 `got.Rules[2]`。确证测试自身越界。它解释包中其他 141 个 `INCOMPLETE` 被中断，不能证明那 141 个测试会通过。 |
| `stream_error_event_test.go` 中失败的每个 Responses SSE 测试 | 各自调用公共断言 `:53`：`synthetic event must not emit sequence_number` | `stream_error_event.go:42` 明确序列化必填 `sequence_number`，且注释说明终结事件写 0 的客户端要求。是既有断言与代码契约冲突；不修改 WS/流协议，不从本地注释推定所有真实客户端兼容。 |
| 两个 `TestRetrieve*Model*` 家族的失败子测试 | 返回整份 `object:list`，断言要求单模型对象；隐藏模型期望 404 实际 200 | 测试辅助函数给 `c.Params["model"]`，直接调用 `GatewayHandler.Models`；该方法没有读取 model 参数，所有分支写列表。确证调用与断言不一致，仍需产品路由契约决定是测试失配还是缺少行为。 |
| `TestGatewayModels_CompositeCustomModelsListFiltersAcrossConcretePlatforms` | `gateway_models_test.go:826` 缺少 `minimax-custom` | `compositeAvailableModels` 枚举平台缺少 MiniMax，但 fixture 明确包含 MiniMax 账号及 mapping。已有列举遗漏，不是数据库/容器环境错误。 |
| `TestResolveOpenAIMessagesDispatchMappedModel_CompositeCNTargetsSkipGroupMapping` | `openai_gateway_cn_dispatch_test.go:82` 对 `opencode_go` 期待空值，实际 `gpt-5.3-codex` | 实现只跳过 Grok 和 `IsCNProvider`，未涵盖 OpenCodeGo。既有映射行为与独立测试要求冲突；本轮不扩展分发功能。 |
| 两个 Dashboard native compaction 测试 | `dashboard_handler_request_type_test.go:355` 过滤值 nil；`:377` 非法布尔值返回 200 | trend/models/groups 没有读取 `native_compaction_v2`；已有 usage handler 读取同名字段。是 Dashboard 过滤链路遗漏；不是本轮显式 compact 请求契约已获确认的证据。 |
| `TestAntigravityCompatChatMixedBuiltInToolsEnableServerSideInvocations` | `antigravity_gateway_compat_test.go:320` flag 期待 true，实际 false | `enableMixedGeminiToolInvocations` 注释和实现明确同时有 function 时删除 built-in tools 和该 flag。旧测试要求保留混合工具，与已有处理契约冲突。 |
| `TestCodexContextWindow*` 中失败的各子测试 | 上游独立 maximum 期望 872000 / 512000 / 300000 或缩小为128000；实际均为272000 | `openai_codex_model_metadata.go:404–406` 把两个输出字段都设为同一 `metadata.ContextWindow`。确证丢失独立最大窗口语义，不能以 float64/int64 表象或环境为由豁免。 |
| moderation 的旧枚举断言 | `rule_only → rules_only`、`api_only → model_only`、`hybrid/candidate_only → rules_and_model` | `normalizeModerationEngineMode` 和 `content_moderation_unified_mode_test.go` 明确要求映射到新枚举。对应断言存在契约变化；不推断同包所有审计失败均由该映射造成。 |
| moderation 默认审计范围 | `content_moderation_test.go:1135` 期望 `all_context` 实际 `user_only` | 与既有 candidate-only/user-only 隐私策略相关。只记录现存断言冲突，不恢复已清理的隐私数据路径。 |
| moderation 的 reviewer 次数、fallback provider、observe action、fail-closed、语义 evidence 等其余失败 | 每项保留自身文件行、expected/actual 或失败布尔断言 | 各条需要对应路径的最终候选复核；不能从枚举变更或代表性失败外推根因。暂为未归因，仍 NOT_PASS。 |
| `TestOpenAIResponsesWebSocket_ContentModerationBlocksFirstFrame` | `openai_gateway_handler_test.go:2057` 布尔断言失败 | 既有 WS 与 moderation 边界失败；本轮不修改 WS，根因保持未归因。 |

上述文件在准备时与 `924819c5` 的对应文件没有源码差异；这只能帮助排除本轮直接编辑，不能取代本轮实际运行，也不能排除新依赖造成间接影响。

## 测试自身越界的最小修复

`TestSetOpenAIFastPolicySettings_Validation` 的有效输入、长度断言和其余字段断言均只描述 priority、ultrafast 两条规则。本轮仅移除 `got.Rules[2]` 的矛盾断言，不增加 fixture 规则，不修改业务实现、两条规则的期望或其他失败测试。修复目的是允许 service 包继续执行后续测试，不能据此宣布它们通过。

现有 `TestApplyOpenAIFastPolicyToBody_ForcePriorityInjectsMissingTier` 明确配置 `OpenAIFastTierMissing`，分别检查匹配用户注入 priority、不匹配用户保持缺失；`TestApplyOpenAIFastPolicyToBody_LegacyAllRuleDoesNotInjectMissingTier` 和 `TestApplyOpenAIFastPolicyToBody_MissingTierIgnoresNonForceRule` 继续覆盖缺失 tier 的其他边界。这里记录的是已存在的测试代码覆盖，尚未重新执行，不将其写成新增或已通过的验收证据。

本修复尚待最终候选快照的定向及完整 suite 运行。历史 panic 与 141 个未终结测试记录不会被覆盖；基线 `924819c5` 保持原样运行并单独保留 panic。

## 最终候选执行时的处理

先保留不跳过测试的完整非 integration 命令及退出码。若旧 panic 仍导致包中断，对失败与 `INCOMPLETE` 根测试并集作独立有界重跑，逐条记录命令、退出码和子测试结果；独立重跑 PASS 不能覆盖原整套 INCOMPLETE。任何修复之后重新冻结受测代码 SHA；文档变更不要求机械重跑。

对照命令示例（占位值不能写入最终证据）：

```sh
python3 scripts/acceptance/compare_go_test_json.py \
  --baseline-json BASELINE_JSONL --candidate-json CANDIDATE_JSONL \
  --baseline-sha BASELINE_SHA --candidate-sha CANDIDATE_SHA \
  --baseline-exit BASELINE_EXIT --candidate-exit CANDIDATE_EXIT \
  --baseline-command 'go test -p 2 -count=1 -json ./...' \
  --candidate-command 'go test -p 2 -count=1 -json ./...' \
  --output COMPARISON_JSON
```

本文件和解析脚本不更改数据库、配置、迁移或运行行为。回滚仅回退证据工具提交；原始历史日志和失败结果继续保留。
