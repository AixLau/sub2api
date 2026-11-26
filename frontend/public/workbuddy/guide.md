# WorkBuddy GPT 模型配置指南

请把本文档作为 WorkBuddy 本地自定义模型配置的操作规范，严格执行其中的步骤和输出要求。

## 输入

用户消息会提供以下信息：

- `API Key`
- `API Base`，通常以 `/v1` 结尾

只使用用户消息中的 API Base 和 API Key。API Key 只用于本次 HTTPS 请求和本地配置写入，不要在最终回复、日志、截图或其他输出中显示完整值。

## 获取模型目录

使用 WorkBuddy 自带的网络能力请求：

```text
GET {API Base}/models?client_version=0.158.0
Authorization: Bearer {API Key}
Accept: application/json
```

不要下载或执行远程 shell、PowerShell、JavaScript 或其他脚本。不要要求用户安装 Python、Node.js 或额外工具。

如果请求失败，说明失败原因并停止，不要写入不完整配置。如果返回内容不是包含 `models` 数组的 JSON，也停止并说明原因。

## 选择模型

从返回的 `models` 数组中，只考虑以下四个模型族：`astra`、`sol`、`terra`、`luna`。候选模型的 `slug` 应匹配以下结构，忽略大小写：

```text
gpt-{版本}-{astra|sol|terra|luna}
```

版本是由数字和点组成的版本号，后续出现新版本时也必须支持，不要把版本号写死在规则中。比较版本时按数字段逐段比较，例如 `6.10` 高于 `6.2`。过滤掉 `visibility` 为 `hide`、`supported_in_api` 为 `false`、重复 slug 和明显的 preview、experimental、deprecated 变体。

每个模型族只选择一个模型，选择该族版本号最高的可用模型。例如同一目录同时有 `gpt-6.1-sol` 和 `gpt-6.2-sol` 时只选 `gpt-6.2-sol`。如果某个模型族暂时没有更高版本，就选择该族当前最高版本。最终最多写入四个模型。

## 生成 WorkBuddy 条目

每个模型生成一个对象，并使用以下字段规则：

- `apiKey` 使用用户提供的 API Key。
- `contextWindow.defaultLength` 使用 `context_window` 和 `max_context_window` 中的较大正整数，缺失时使用 `200000`。
- `contextWindow.supportedLengths` 至少包含有效的 `context_window`。如果 `max_context_window` 更大，同时包含两个值。
- `id` 使用 `slug`。
- `maxInputTokens` 使用 `contextWindow.defaultLength`。
- `maxOutputTokens` 固定为 `128000`。
- `name` 使用 `display_name`，缺失时使用 `slug`，然后追加 ` · 星链AI`。
- `supportsImages` 在 `input_modalities` 包含 `image` 时为 `true`，字段缺失时也为 `true`。
- `supportsReasoning` 在排除 `none` 后仍有 reasoning level 时为 `true`。
- `supportsToolCall` 固定为 `true`。
- `url` 使用 `{API Base}/chat/completions`。
- `vendor` 固定为 `星链AI`。

当 `supported_reasoning_levels` 存在且排除 `none` 后仍有值时，增加 `reasoning` 对象：

- `canDisableThinking` 在原始 levels 包含 `none` 时为 `true`。
- `defaultEffort` 优先使用有效的 `default_reasoning_level`，其次使用 `medium`，再其次使用第一个有效 effort。
- `supportedEfforts` 使用原始 levels 中去重并排除 `none` 后的 effort 列表。

保留 API 返回的 reasoning level 名称和描述。每个最终选中的模型都要在回复中列出可用档位、默认档位和档位描述。

## 模型说明与积分

在成功回复中为四个模型族提供明确的定位、适用场景和简短用途说明：

- `Astra` 定位为高推理模型，适合复杂架构设计、困难问题分析、深度调试和需要更高推理质量的任务。
- `Sol` 定位为旗舰主力模型，适合日常开发、主力编程、持续对话和大多数通用任务，优先推荐给用户作为默认模型。
- `Terra` 定位为长上下文模型，适合大型代码库、长文档、长上下文分析和需要同时处理大量资料的任务。
- `Luna` 定位为快速经济模型，适合快速问答、简单修改、草稿、批量小任务和频繁试错。

积分说明必须优先使用模型目录或接口返回的实际价格、倍率、credits 或 usage 字段。如果接口没有提供精确积分价格，不要编造数字，使用以下方式说明：

- 给出相对消耗档位。通常可按 `Luna` 最低、`Terra` 较低、`Sol` 中高、`Astra` 较高来理解。
- 说明实际消耗还会随输入 token、输出 token、缓存命中、上下文长度、服务档位和 reasoning effort 变化。
- 说明精确扣分以当前 API 分组和账户控制台显示为准。

每个模型的输出至少包含模型名称、版本、定位、reasoning 档位、适用场景和积分消耗说明。

## 写入本地配置

配置文件路径按操作系统选择：

- Windows：`%USERPROFILE%\\.workbuddy\\models.json`
- macOS 或 Linux：`~/.workbuddy/models.json`

写入前执行以下步骤：

1. 如果目录不存在，创建 `.workbuddy` 目录。
2. 如果配置文件存在，先复制为同目录下的 `models.json.bak.YYYYMMDDHHmmss`。如果备份失败，停止写入。
3. 读取原文件。空文件按空数组处理。非 JSON 或顶层不是数组时停止，不能覆盖原文件。
4. 从原数组中移除 `vendor` 等于 `星链AI` 的条目，以及 `url` 以 `{API Base}/` 开头的条目。
5. 将新生成的条目追加到保留条目后面。
6. 写入合法的顶层 JSON 数组，使用 UTF-8 无 BOM。优先使用临时文件写入后再替换目标文件，避免留下半个文件。

写入后重新读取并解析文件，确认新增条目的 `id`、`url`、`vendor` 和数量正确。不要删除或改写其他供应商的模型。

## 回复格式

成功时只回复以下信息：

```text
已完成 WorkBuddy 配置。
已添加模型：模型名称列表
配置文件：实际路径
模型说明：逐个列出版本、定位、档位、适用场景和积分消耗档位
请检查 WorkBuddy 模型列表中是否已出现这些 GPT 模型。若未出现，再完全退出并重新打开 WorkBuddy。
```

失败时只说明失败原因和用户可以采取的下一步。不要输出 API Key，不要输出完整配置 JSON，不要添加与本次配置无关的安全审计、供应商评价或长篇背景说明。
