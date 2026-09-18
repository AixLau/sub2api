# 发布候选全套失败归因

本轮固定对照为基线 `924819c5d04a550801fb4e85441059389dc00caa` 与候选 `b06789dde6ddb7c25fc29ffb7ae81ed79cf6eef8`。完整默认 suite 和 `-tags=unit` suite 已在两个 Git 导出快照中分别执行，四次均退出 1。原有未跟踪文件没有进入快照；本文件保留首次失败、中断和补充运行，不能据相同失败宣称通过。除下述一行测试越界修复外，本分析没有修改业务代码、旧协议、隐私清理或其他失败测试。

## 证据规则

`scripts/acceptance/compare_go_test_json.py` 分别保留 package、root test、subtest 的事件与计数。每个测试的 `run` 必须有自己的 `pass/fail/skip` 才算终结；包因另一测试 panic 而终止时，尚未终结的测试是 `INCOMPLETE`。未出现在某一运行中的测试是 `NOT_RUN`，不是 PASS。两个日志都未出现的测试无法仅靠日志枚举。

比较器的 `FAIL_TO_FAIL` 只是观测结果，`attribution` 默认 `UNATTRIBUTED`，`acceptance` 保持 `NOT_PASS`。只有逐项对照断言、实际值、栈和相关代码后，才标记“已有测试缺陷”“断言契约变化”“已有业务缺陷”“环境相关”或“未归因”。新增失败也不能仅因旧运行被 panic 截断就判定为新增回归。原始日志保留路径和 SHA-256；报告不复制任意运行时日志，避免将秘密写入 Git。

解析器的七项定向测试已执行：

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover \
  -s scripts/acceptance -p 'test_compare_go_test_json.py' -v
```

退出 0；覆盖 panic 后平行测试未终结、相同失败不可写成 PASS、子测试独立计数、候选独有失败不可直接归因、重复运行不得覆盖失败、空日志及构建失败不得捏造测试结果，以及运行时 JSON 即使含测试栈也不进入断言摘要。这是证据处理工具验证，不是产品验收。

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

现有 `TestApplyOpenAIFastPolicyToBody_ForcePriorityInjectsMissingTier` 明确配置 `OpenAIFastTierMissing`，分别检查匹配用户注入 priority、不匹配用户保持缺失；`TestApplyOpenAIFastPolicyToBody_LegacyAllRuleDoesNotInjectMissingTier` 和 `TestApplyOpenAIFastPolicyToBody_MissingTierIgnoresNonForceRule` 继续覆盖缺失 tier 的其他边界。它们是既有测试，不计作新开发功能。

本修复在最终候选默认完整 suite 中实际 PASS；基线仍原样 panic。历史 panic 与 141 个未终结测试记录没有覆盖。候选这 141 项各自终结 PASS，不能回写成基线已通过。

## 最终候选完整非 integration 结果

测试环境为本机 Apple M2 / macOS 26.6.2 / arm64，模块工具链 Go 1.27.0。所有 Go 命令串行执行，`GOFLAGS=-p=2`，没有与持续矩阵并行编译。命令由 `/tmp/sub2api-release-closure/run-recorded.py` 保存命令参数、工作目录、精确 SHA、开始/结束时间、退出码和 stdout/stderr SHA-256。环境清单为 `/tmp/sub2api-release-closure/environment.json`，不声称原生 Linux/amd64 验收。

各在对应快照的 `backend/` 运行：

```sh
go test -p 2 ./... -count=1 -timeout=15m -json
go test -p 2 -tags=unit ./... -count=1 -timeout=15m -json
```

| suite / 版本 | 耗时 / 退出码 | 包级 PASS / SKIP / FAIL | 根测试 PASS / SKIP / FAIL / INCOMPLETE | 子测试 PASS / FAIL |
|---|---|---|---|---|
| 默认 / 924819 | 182.47s / 1 | 54 / 72 / 3 | 5352 / 14 / 56 / 141 | 4701 / 23 |
| 默认 / b06789 | 271.11s / 1 | 55 / 72 / 3 | 7640 / 16 / 62 / 0 | 6700 / 37 |
| unit / 924819 | 147.79s / 1 | 60 / 66 / 3 | 4252 / 16 / 19 / 49 | 3625 / 19 |
| unit / b06789 | 114.59s / 1 | 61 / 66 / 3 | 4255 / 16 / 19 / 49 | 3632 / 19 |

默认 suite 的问题并集为：80 个 `FAIL_TO_FAIL` 事件、2 个 `FAIL_TO_PASS`、141 个 `INCOMPLETE_TO_PASS`、22 个 `NOT_RUN_TO_FAIL`。包级及父子事件分别计数，不能混称独立缺陷数。首次基线 82 个 FAIL 事件，候选 102 个 FAIL 事件，均不能沿用旧报告的 81。

两个 `FAIL_TO_PASS` 分别是已修复的 FastPolicy 越界和 `TestOpenAIGatewayHTTPPipelineCyberSessionBlockRecordsRiskAudit`。后者没有修复：它只等待异步日志出现，就立即断言随后另行写入的 snapshot 可用，存在测试同步窗口；本轮基线 FAIL、候选 PASS 只能记时序敏感待确认，不能豁免为环境问题或宣称修复。

unit 首次两边同有 41 个失败事件和 49 个未终结测试。`TestAdminProxyRejectsOutOfRangeExpiry` 要求非法年份在接触仓储之前拒绝，fixture 因此刻意传入 nil 仓储；现有 `CreateProxy` 缺少年份检查，直接调用 `proxyRepo.Create`，两边实际都在 `admin_proxy.go:85` panic。该缺陷未修复，不把 49 个被打断的测试计为 PASS。

逐项机器记录（包括每项断言、文件行、源码 blob 身份、结果及未归因边界）：

- [默认完整 suite 对照](release-full-failure-comparison.json)
- [unit 完整 suite 对照](release-unit-full-failure-comparison.json)

原始日志及命令 manifest 位于 `/tmp/sub2api-release-closure/final-logs/{baseline,candidate}-{full,unit-full}.{stdout,stderr,json}`；每项比较文件嵌入对应 manifest 与日志摘要，日志 SHA-256 以机器记录为准。

## 补充运行保留首次结果

默认 suite 的 22 个 `NOT_RUN_TO_FAIL` 事件属于 8 个根测试。两边使用同一精确 `-run` 表达式独立重跑（命令全文嵌入下面的比较 JSON），测试内容分别是图片 usage 持久化、Codex 图片账号测试、终结错误去重、UA 校验、两个 WS execution-scope 测试、Gemini 3.7/3.8 fallback 定价。实际均为 8 个根测试及 14 个子测试 FAIL，不是根据代表性失败推断其余项；不修改这些范围外业务以追求通过。

```sh
go test -p 2 ./internal/service -run '^(TestBillingService_Gemini37FlashThinkingTierFallbacksAreBillable|TestBillingService_Gemini38FlashThinkingTierFallbacksAreBillable|TestCanonicalCodexIdentityValidatesBeforeTrimming|TestCodexDirectImagesAccountTestAndWhitelist|TestOpenAIGatewayServiceRecordUsage_OutputImageSizeWinsBeforeBillingAndPersistence|TestOpenAIGatewayService_Forward_WSv2_ExecutionScopeUsesOriginalIdentity|TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_StateBoundToExecutionScope|TestOpenAIStreamTerminalOverloadRecordsUpstreamAfterOutput)$' -count=1 -timeout=120s -json
```

基线 14.59s、候选 12.19s，均退出 1。原完整运行仍是 `NOT_RUN_TO_FAIL`，每条追加独立补充证据后归类为“既有失败表现被独立复现”，不据此写 PASS。具体错误包括缺少 `image_cache_read_tokens`、mock 返回未被识别为图片、同一终结错误产生两条审计记录、UA 值冲突、WS scope/连接关闭、十个 Gemini 型号/档位定价未找到；各子测试保存自己的断言。

为补齐 unit 包被 panic 截断的覆盖，两边都使用以下明确跳过两个已知阻断测试的命令，保留原首次完整运行：

```sh
go test -p 2 -tags=unit ./internal/service \
  -skip '^(TestAdminProxyRejectsOutOfRangeExpiry|TestSetOpenAIFastPolicySettings_Validation)$' \
  -count=1 -timeout=15m -json
```

| 补充运行 | 耗时 / 退出码 | 根测试 PASS / SKIP / FAIL / INCOMPLETE | 子测试 PASS / FAIL |
|---|---|---|---|
| unit service / 924819 | 208.68s / 1 | 8342 / 4 / 65 / 0 | 7377 / 38 |
| unit service / b06789 | 213.74s / 1 | 8359 / 4 / 63 / 0 | 7386 / 36 |

这次补充中的问题并集为 100 个 `FAIL_TO_FAIL` 和 4 个 `FAIL_TO_PASS` 事件。后者是 `TestOpenAIWSHTTPBridgeSessionPreemptionEligibility` 及两个子测试、`TestOpenAIWSIngressSessionPreemptionIsolatesCodexThreads`；没有修改 WS，原因未归因，不能宣称修复或全部归为环境问题。首次 unit 的 49 个未终结测试，两边补充运行都各自 PASS，但首次 `INCOMPLETE` 不覆盖。跳过的代理日期缺陷继续保留，不宣称完整 suite 通过。

- [默认 8 根测试独立补证](release-default-unobserved-roots-failure-comparison.json)
- [unit service 中断后补覆盖](release-unit-service-after-known-panics-failure-comparison.json)

每项归因保留产品和测试文件的 Git blob 身份，确保“相同源码”与“实际失败”是不同证据。未定位的复杂 moderation、CN routing、Anthropic beta、图片/定价及 WS 等项仍标 `EXISTING_FAILURE_CAUSE_UNATTRIBUTED`；没有从同包其他测试或一条源码冲突推断所有根因。候选当前没有观察到基线实际 PASS 而候选 FAIL 的新失败，但这一结论限于这些实际执行命令，不能解释为全套或系统验收 PASS。

## integration 首次广泛回归的独立对照

候选保存的 repository 二进制首次执行以下过滤范围时退出 1；基线随后在独立 Testcontainers 环境使用同样测试过滤与 15 分钟预算执行，实际也退出 1。两边没有生产操作，也没有清空未知租约来使测试通过。

```sh
go test -p 2 -tags=integration ./internal/repository \
  -run '^(TestCredential|TestPrincipalAdmission|TestMultiCredentialControl)' \
  -skip '^TestCredentialFullGatewayOfflineRollout$' \
  -count=1 -timeout=15m -json
```

候选实际命令是相同范围的已保存 `repository.test -test.run=... -test.skip=... -test.count=1 -test.timeout=15m -test.v`，二进制命令和运行 SHA 原样嵌入比较 JSON。单独的完整网关 rollout 不混在这组直接仓储/handler fixture 测试里。

| 首次 broad integration | 耗时 / 退出码 | 根测试 PASS / SKIP / FAIL | 子测试 PASS / FAIL |
|---|---|---|---|
| 924819 | 65.99s / 1 | 54 / 3 / 15 | 3 / 17 |
| b06789 | 53.97s / 1 | 56 / 3 / 15 | 3 / 17 |

15 个根测试及 17 个子测试逐名称完全相同失败，失败的具体 fixture 断言均为 `credential_gateway_acceptance_test.go:74` 的 `CREDENTIAL_REDIS_STATE_LOST`，或由这些子项产生的父失败事件；另有一个包级失败事件。并非所有执行到了各自待验收的 handler、billing 或 receipt 断言。

已定位现存测试间状态依赖：较早的 `TestCredentialAuditOutboxDeliveredOnce` 通过直接仓储 `TryAdmit` 创建 `RESERVED`，测试结束没有结束这条预留；随后第一个网关 fixture 初始化 Redis epoch 时，PostgreSQL 已存在非 RELEASED lease，而 epoch 尚未初始化。安全默认要求拒绝，此失败正确地暴露 fixture 组合问题。至少这一条预留足以触发拒绝，不推断它是之前测试的唯一残留来源，也不删除安全检查。

逐项比较与原始命令：[integration 首次失败对照](release-integration-failure-comparison.json)。候选原日志为 `-test.v`，转换器先用基线 JSON 的输出重新解析并核对所有测试状态完全一致，再解析候选，并保留原 stdout 行号；这只是证据转换验证，不是额外产品测试。

随后使用同一保存二进制，在新 TestMain 创建的独立 PostgreSQL/Redis 环境中执行：

```sh
/tmp/sub2api-release-closure/candidate-binaries/repository.test \
  -test.run='^(TestCredentialGateway|TestCredentialGlobalUser|TestCredentialUsageCrash|TestCredentialOverloadRecovery)' \
  -test.count=1 -test.timeout=5m -test.v
```

实际 98.12s、退出 0；对应的 15 个根测试和 17 个子测试逐项 PASS。比较 JSON 每行追加同二进制独立运行的对应终结事件，且保留原 broad 的 FAIL；二进制 SHA-256、命令、日志摘要也一并记录。该结果证明在空 epoch 正确初始化的隔离条件下，handler 15 秒预算、跨路径用户容量、补偿、可靠 receipt 恢复及 UNKNOWN 保留的各项断言通过，同时进一步支持原 broad 是 fixture 顺序依赖；它不能改写首次 broad 失败，也不是完整三网关或生产验收。无需通过清零未知占用或改源码得到这次结果。

本文件和解析脚本不更改数据库、配置、迁移或运行行为。回滚仅回退证据工具提交；原始历史日志和失败结果继续保留。
