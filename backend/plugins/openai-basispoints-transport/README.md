# Basis Points Responses 工具桥接插件

0.3.1 是根据 BPS 实测协议实现的独立 Sub2API 插件，仅修改插件工程。默认上游是 `https://bps.openai.com/basispoints/api/responses`，只接受 `POST /responses`。宿主负责凭据刷新、账号调度、下游协议与计费；插件复用该次请求已经携带的 OAuth Authorization 和 ChatGPT 账号 ID。

0.2.0 上线后的真实错误（`422: Invalid request body`）定位出：BPS 只接受 Excel 加载项的请求体词汇表，客户端 Responses 字段与顶层自定义字段都会被整体拒绝。0.3.0 起插件按已知字段白名单重建请求体，不再在客户端 body 上做删除式修补。

## 请求与回放协议

1. 请求体只发送 Excel 加载项词汇表字段：`model`（见下方模型映射）、`model_selection: "explicit"`、`stream`、`store: false`、`input`、`prompt_cache_key`、`reasoning_effort`、`context_management`、`metadata`。`tools`、`tool_choice`、`parallel_tool_calls`、`reasoning` 对象、`instructions`、`include`、`text`、`max_output_tokens`、`temperature`、`top_p`、`previous_response_id` 等一律不发往上游。
2. `task_id`、`turn_id`、`agent_iteration` 放在 `metadata` 中，值为字符串；客户端 metadata 的标量字段一并透传（保留键优先）。`agent_iteration` 随每个工具往返递增，同一请求重试不递增；新的 user 消息开启新 turn。显式传入的 turn 与回放状态冲突时直接报错。
3. `instructions` 与客户端工具目录都写入 developer 消息（带 `type: "message"`）。工具目录是普通文本，不是上游工具声明。
4. 描述约定：调用客户端工具时，通过 `run_officejs.arguments.code` 传递 `{"tool":"get_weather","args":{"city":"Tokyo"}}`。插件只解析 JSON，不执行 OfficeJS 或任何客户端工具。上游可能把该工具显示为 `functions.run_officejs`，两者都按传输信封解码。
5. 解码完整的传输信封（`run_officejs` / `functions.run_officejs`，以及模型误放进其它执行器如 `run_connector_action` 的信封），识别两层 JSON，校验目标是本次声明的客户端工具，再输出标准 `function_call`。支持 namespace 内的 function，以及 custom 工具对应的 `custom_tool_call`。
6. 通过宿主 HostService KV 保存完整上游 item，包含 `id`、`call_id`、`arguments` 内外的 `summary`、`references` 和未知字段。客户端收到不透明的 `call_bps_…` 标识，按正常流程决定是否执行工具。
7. 客户端提交工具结果时，插件恢复原始调用 item 和原始 `call_id`（即使客户端只提交了工具结果也能回放），并为结果 item 补全 `fc_…` 形式的 `id`。相同调用 item 不重复插入。
8. 非插件生成的工具历史（启用前的会话、无法解析的调用）不再拒绝：插件用客户端自己的 name/arguments 重建 `run_officejs` 传输信封继续回放。没有对应调用项的孤立工具结果仍然报错。
9. 输入项清洗：带 `encrypted_content` 的 reasoning 原样保留（仅保留加密内容），裸 reasoning 与 `item_reference` 丢弃（`store:false` 的上游拒绝它们）；`image_generation` 只保留仍带内联数据的 item，瘦身项（仅 `id`，`result: null`）丢弃——`store:false` 下上游不持久化 item，回放瘦身项会以 `Item with id 'ig_…' not found` 404 整个请求；各 item 上的 `internal_chat_message_metadata_passthrough` 会剥离。
10. 图片输入（**通常走原生通路**）：默认开启 `native_fallback` 时，含 `input_image` 的请求会路由到原生 Codex 上游（见下方「双通道路由」），本条描述的附件化上传**仅适用于 BPS 通路**——即路由关闭（`native_fallback: false`）或未触发时。用户消息里的内联 `input_image`（`data:` URL，含 `{"url":…}` 字典形态）会被解码后以 multipart 上传到与 `/responses` 同目录的 `attachments` 端点（字段名 `file`，文件名固定 `image.<ext>`），拿到 `openai_file_id` 后替换为 `{"type":"input_image","file_id":…,"detail":…}`（`detail` 缺省补 `"auto"`）。直接把 data URL 发给上游会 422。**仅支持 JPEG/PNG/GIF/WebP**：媒体类型按别名归一（`image/jpg`、`image/pjpeg`→`image/jpeg`，`image/x-png`→`image/png`），扩展名由显式映射决定（不依赖系统 MIME 表，否则会出现无扩展名或 `.jpe` 这类上游不认的后缀，报 `Expected image type … but got none`）；其它格式（HEIC/AVIF/TIFF/BMP 等）在发出主请求前明确报错，不做格式转换。已有 `file_id`、远程 `https://` 图片、assistant 消息与工具结果里的图片原样保留；同内容图片按摘要缓存复用 file id（缓存按端点+凭据隔离，失败不缓存），并发同图合并为一次上传。上传失败在发出主请求前中止（`ATTACHMENT_UPLOAD_FAILED`，`request_sent=false`），错误文本会脱敏凭据与图片字节。上传发生在 turn/task 标识计算之后，图片引用不会改变会话身份。

SSE 中的普通文本保持流式输出；工具 envelope 等待 `response.output_item.done` 到齐、回放状态保存成功后，才输出对应的 added / delta / arguments.done / item.done。终结响应中的工具也同步转换，usage 保留。无效 envelope、未知工具、KV 不可用或工具流截断都返回明确错误，不执行代码、不伪造结果。

## 0.3.1 修复与恢复边界

- 修复非法工具信封原样泄漏导致 Codex 连续报告 unsupported call: run_officejs。包括嵌套 JSON 中非法的单引号转义；客户端只会收到已校验的工具调用或明确错误。namespace custom 工具的输入增量也等待完整校验。
- BPS SSE 收到终结事件立即结束，不继续等待 HTTP 连接关闭；读取超时单独报告 UPSTREAM_RESPONSE_TIMEOUT，避免被吞成不明读取错误。该修改不表示可以消除上游真正的中断或超时。
- invalid_encrypted_content 支持 HTTP 错误和 HTTP 200 中的早期 SSE 失败。只有在文本、推理或工具输出之前，才允许清理一次并重试；SSE 探测上限为 32 个前导事件、256 KiB，普通无密文请求不探测。
- 清理发生在 Prepare 恢复 KV 原始调用之后，保持 turn_id、agent_iteration 和真实工具结果。仅移除可选 reasoning 或有完整可读副本的协议密文字段；不递归修改工具业务参数或字符串 JSON，不改写 KV 原始记录。
- 密文是工具结果或压缩历史唯一副本时，返回 TOOL_BRIDGE_ENCRYPTED_REPLAY_UNSAFE，需重新提供结果或开新会话。不会用空结果伪装恢复成功，也不会在一般 server_error 重试耗尽后盲目删密文。

## 客户端工具走原生通道（tools_via_native）

BPS 注入的执行器套件**不稳定**：有的账号/时段拿到带 `run_officejs` 的正版套件，有的只拿到通用套件（`functions.read_ranges`/`run_connector_action` 等，无 `run_officejs`），此时走私协议结构性无法工作。而客户端工具（`exec_command` 等）在**原生 Codex 通道**是模型的一等工具，不需要任何协议。

`tools_via_native`（**默认关闭**）：工具会话默认留在 BPS 桥接。打开后，携带客户端 function/custom 工具声明（顶层 `tools[]` 或 `additional_tools` 载体）的请求整体走原生通道——请求体一字不动、响应原样回传。仅当 BPS 套件缺 `run_officejs` 时才建议开启。配置页有对应复选框。

## Codex 身份头（ensureCodexIdentity）

上游按客户端身份校验（originator 与 UA 首段配对）并按身份分优先级；身份缺失的请求会被当作匿名客户端，拿到通用工具栏（`web.run`/`multi_tool_use.parallel` 一系）而非 Codex 执行器套件。插件在所有出站请求上补齐：`originator`（`codex-tui`/`codex_cli_rs`，由 UA 推导）、`version`（与 UA 版本段逐字一致，低于上游门槛 0.146.0 时一并重写 UA）、`OpenAI-Beta: responses=experimental`。非 Codex 形态 UA 采用规范身份。

## 模型映射

插件配置里的 `model_mapping`（JSON 对象）把客户端请求的模型名翻译成上游 slug：

```json
{
  "model_mapping": {
    "gpt-6-astra-basispoints": "gpt-6-astra",
    "gpt-5.6-luna-excel": "gpt-5.6-luna"
  }
}
```

- 命中映射：按映射值**原样**发给上游（映射值即最终 slug，不再做任何改写）。
- 未命中：维持现状，透传模型名并自动去掉 `-excel` 后缀。
- 键是客户端请求的原始模型名（精确匹配）；键值都必须是非空模型名，最多 64 条。配置页有对应的 JSON 编辑框。

## 双通道路由（原生 Codex 回退）

BPS 只服务 Excel 加载项词汇表能表达的请求。凡是工具桥无法服务的请求（图片、图片生成、托管工具选择、结构化输出）会绕过工具桥，**原样**转发到原生 Codex 上游（宿主最初意图的端点），响应不经任何改写直接回流。BPS 通路本身完全不变。

### 路由触发条件

满足以下任一条件即走原生通路（判定在原始请求体上按固定顺序取首个命中，命中与否只看「是否走原生」，顺序不影响结果归属）：

| 触发条件 | 判定 |
| --- | --- |
| `image_input` | `input` 中任意位置（消息 content 部分或工具结果 output 数组）出现 `input_image` 内容块。形态无关：data URL、`https://` URL、`{"url":…}` 字典、`file_id` 均算命中。 |
| `image_generation` | `tools[]` 声明了 `image_generation` 工具（含 namespace 嵌套），或历史里存在 `type: "image_generation"` 的 item、或 `id` 以 `ig_` 开头的 item。 |
| `hosted_tool_choice` | `tool_choice` 强制指定某个工具名，而该名**不**在本次请求 `tools[]` 声明的 function/custom 工具里（按 namespace 限定名比对）。例如强制 `web_search` 或 `image_generation` 走原生；强制一个本次已声明的客户端 function **不**触发。 |
| `structured_output` | `text.format.type` 或 `response_format.type` 存在且不为 `"text"`。 |

**显式非触发**：仅声明 `web_search`（Codex CLI 总是带 `external_web_access=false` 声明它）**不**路由，仍走 BPS 通路。

### 原生通路语义

- 请求体**逐字节一致**转发：不做 wire-shape 重建、不做工具桥接、不加 turn metadata、不做 `model_mapping`。
- 认证头与 BPS 通路完全相同（复用同一次请求已携带的 OAuth Authorization 与账号头）。
- 响应原样回流，SSE 保持流式，不做事件改写。
- 状态页 `native_requests` 计数记录走该通路的请求数。

### 通路无关的历史不变量

客户端工具调用使用标准 function/custom item 和不透明的 call_bps_ 别名。加密 reasoning、工具结果和压缩历史不保证跨账号或跨通路可解密；不能据此认为对话可以无损自由切换。BPS 的有限恢复规则见下方，原生通路仍原样转发。

### 配置

- `native_fallback`（默认 `true`，缺省即启用）：关闭后，命中触发条件的请求不再改走原生通路，而是留在 BPS 通路按原逻辑处理；此时下述附件化图片上传能力仍然可用。
- `native_upstream_base_url`（默认 `""`）：原生 Codex 端点覆盖。留空表示原样使用宿主传入的请求 URL（scheme/host/path/query 逐字保留）；非空时以其为 base 并保留 `/responses` 路径后缀与 query。

路由关闭（`native_fallback: false`）或未触发时，基于 `attachments` 的内联图片上传（见上方「图片输入」）继续适用于 BPS 通路。

## 边界与已知限制

- **请求体适配仍未通过真实 BPS 账号完整联调**。0.2.0 的 422 暴露了词汇表问题，0.3.0 的白名单以 Excel 加载项线上请求形态为准；测试覆盖 JSON、SSE、gRPC、跨进程回放与打包运行时，真实模型上的字段接受度仍需联调确认。
- `reasoning_effort`：`low`/`medium`/`high`/`xhigh`/`ultra` 按原值发送（`ultra` 不改档）；`max` 映射为 `xhigh`；`x-high`、`extra-high`、`extra_high` 映射为 `xhigh`；客户端省略时按 `medium` 发送。其它未知档位（如 `minimal` 或拼写错误）在插件侧明确报错，不静默改档。
- 客户端的 `max_output_tokens`、`temperature`、`top_p` 等采样/长度字段不再发往上游（不在 Excel 词汇表内）。这些约束由上游默认行为接管，属行为变化。
- 需要宿主提供 HostService KV。记录按账号、上游地址及宿主隔离后的 session 标识隔离，保留 24 小时。KV 包含工具调用参数，可能含业务数据；不保存 OAuth 认证头或 refresh token。
- 非桥接工具历史可以继续回放，但其中没有 BPS 原始 item，encrypted reasoning 的连续性只从插件接管后的新调用开始。桥接调用（`call_bps_…`）在 KV 过期或丢失后仍然报错，请开始新会话；切换账号或丢失 session/KV 状态后同理。
- 仅桥接客户端 function/custom 工具。`image_generation`、`web_search` 等托管工具声明会移除，不会被转成并不存在的客户端函数；被移除的类型可在插件状态页查看。
- 上游返回的每个工具调用都必须匹配当前客户端目录并通过参数类型校验。有效的执行器信封转换为实际客户端工具；客户端已声明的直接调用也接受校验。无效 JSON、未知工具及错误参数类型返回 TOOL_BRIDGE_CALL_INVALID，不透传 Office 执行器、不猜测或修复可执行代码。
- 当前不支持 compact、图片专用接口或计数接口；此版本不会把这些请求伪装成普通 Responses。`/images/*` 等生成类端点仍然不支持；图片支持仅限用户消息内联图的附件化。
- 工具结果（`function_call_output`）内嵌的图片不做附件化，按参考实现原样保留；宿主 `LiftResponsesToolOutputMedia` 会把工具结果里的图片抬升到后续用户消息，走附件化路径。
- 每个 HTTP 请求 JSON 上限 64 MiB；每个回放 KV 记录上限 240 KiB；每次响应最多 128 个工具调用；每个 turn 最多 512 轮工具往返（防跑飞的保险丝，不是产品限制；超出时明确报错请开新 turn）。错误发生在发出上游请求之后时，返回 `request_sent=true`，防止宿主重复执行。
- `tool_choice` 通过文本约定和返回校验表达，无法保证模型与原生 API 的行为完全一致。

## 构建与验证

在仓库根目录运行：

```bash
cd backend
go test -race ./plugins/openai-basispoints-transport/... -count=1
TARGETS=linux-amd64,darwin-arm64 ./plugins/openai-basispoints-transport/build.sh
SUB2API_TEST_BPS_PACKAGE="$PWD/plugins/openai-basispoints-transport/dist/openai-basispoints-transport-0.3.1.s2plugin" \
SUB2API_TEST_BPS_RUNTIME=darwin-arm64 \
go test ./plugins/openai-basispoints-transport/internal/transport -run '^TestPackagedPluginToolReplay$' -count=1
```

`build.sh` 不执行测试，不删除旧版包。只需 Linux 部署时设 `TARGETS=linux-amd64`。生成的包位于 `dist/openai-basispoints-transport-0.3.1.s2plugin`。

不设置签名参数时输出无 `signature.json` 的开发包，适用于已明确配置 `plugins.allow_unsigned: true` 的宿主。签名构建：

```bash
SIGNING_KEY=/secure/path/publisher.private KEY_ID=my-publisher-v1 TARGETS=linux-amd64 ./build.sh
```

签名包仍要求对应公钥存在于 `plugins.trusted_publishers` 中；允许未签名包不等于信任未知签名。不要把私钥提交进源码或插件包。

## 安装与观察

先停用已安装的同 ID 插件，再上传 0.3.1，保存配置并选择账号。该版本未声明完整宿主联调通过，需要确认“未测试版本”提示。使用专用测试账号开始新会话，先验证普通文本，再验证一次工具调用及回放，最后才扩大账号范围。

配置页每 10 秒显示请求数、成功/失败数、最近 HTTP 状态、KV 连接情况及最近桥接错误码。请求数增加说明请求进入了本插件；成功数、工具调用和回放均成功才说明相应链路可用。“校验配置”仅检查配置，不调用模型。
