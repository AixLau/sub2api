# WorkBuddy GPT 模型配置指南

你正在帮助用户把当前 API 分组中的 GPT 模型加入本地 WorkBuddy。请把本文档作为本次任务的操作规范，严格执行其中的步骤和输出要求。

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

## 筛选模型

只保留同时满足以下条件的模型：

- `slug` 匹配 `^gpt-6(?:\.[12])?(?:-|$)`，忽略大小写
- `visibility` 不是 `hide`
- `supported_in_api` 不是 `false`
- `slug` 不重复

这会包含 GPT-6、GPT-6.1 和 GPT-6.2 系列，不要加入其他厂商或旧系列模型。

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
请检查 WorkBuddy 模型列表中是否已出现这些 GPT 模型。若未出现，再完全退出并重新打开 WorkBuddy。
```

失败时只说明失败原因和用户可以采取的下一步。不要输出 API Key，不要输出完整配置 JSON，不要添加与本次配置无关的安全审计、供应商评价或长篇背景说明。
