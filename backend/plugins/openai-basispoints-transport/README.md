# Basis Points Responses 工具桥接插件

0.4.3 是根据 BPS 实测请求词汇表实现的独立 Sub2API 插件。默认上游是 `https://bps.openai.com/basispoints/api/responses`，只接受 `POST /responses`。宿主负责凭据刷新、账号调度、下游协议与计费；插件复用该次请求已经携带的 OAuth Authorization 和 ChatGPT 账号 ID。0.4.3 在结构化运行日志中增加失败请求原始正文；仍需使用已包含 0.3.2 插件语义错误分类的宿主，才能避免协议错误触发账号换号。

0.2.0 上线后的真实错误（`422: Invalid request body`）定位出：BPS 只接受 Excel 加载项的请求体词汇表，客户端 Responses 字段与顶层自定义字段都会被整体拒绝。0.3.0 起插件按已知字段白名单重建请求体，不再在客户端 body 上做删除式修补。

## 请求与回放协议

1. 请求体只发送 Excel 加载项词汇表字段：`model`（见下方模型映射）、`model_selection: "explicit"`、`stream`、`store: false`、`input`、`prompt_cache_key`、`reasoning_effort`、`context_management`、`metadata`。`tools`、`tool_choice`、`parallel_tool_calls`、`reasoning` 对象、`instructions`、`include`、`text`、`max_output_tokens`、`temperature`、`top_p`、`previous_response_id` 等一律不发往上游。
2. `task_id`、`turn_id`、`agent_iteration` 放在 `metadata` 中，值为字符串；客户端 metadata 的标量字段一并透传（保留键优先）。`agent_iteration` 随每个工具往返递增，同一请求重试不递增；新的 user 消息或非空明文 agent_message 开启新 turn；同一正文发给不同子 agent 时保留独立身份。显式传入的 turn 与回放状态冲突时直接报错。
3. `instructions` 与客户端工具目录都写入 developer 消息（带 `type: "message"`）。工具目录是普通文本，不是上游工具声明。
4. 工具传输协议 v3：外层 `references` 必须是只含一个完整客户端工具名的数组，例如 `["functions.exec"]`。`code` 只承载该工具的载荷：function 为参数 JSON 对象的文本，例如 `{"city":"Tokyo"}`；custom/freeform 为未经包装的原始输入，只序列化外层执行器参数。`summary` 仅供显示，不参与路由。插件不执行 OfficeJS 或任何客户端工具。
5. 仅 `run_officejs` / `functions.run_officejs` 承载协议（也支持独立的 `namespace: "functions"`）。先按 references 精确查询本次客户端目录，再由目录类型决定是否解析 code；custom 中 JSON 外观的内容也保持原样。验证参数类型后输出标准 function_call 或 custom_tool_call，保留 namespace。不从正文猜测工具名、不修补坏 JSON、不接受 connector 代替传输执行器。
6. 通过宿主 HostService KV 保存完整上游 item，包含 `id`、`call_id`、`arguments` 内外的 `summary`、`references` 和未知字段。客户端收到不透明的 `call_bps_…` 标识，按正常流程决定是否执行工具。
7. 客户端提交工具结果时，插件恢复原始调用 item 和原始 `call_id`（即使客户端只提交了工具结果也能回放），并为结果 item 补全 `fc_…` 形式的 `id`。相同调用 item 不重复插入。
8. 非插件生成但结构有效的客户端工具历史，使用客户端自己的 name/arguments 按 v3 重建 `run_officejs` 调用继续回放，保留参数原始 JSON 和数字精度。无效参数或没有对应调用项的孤立工具结果明确报错。
9. 输入项清洗：带 `encrypted_content` 的 reasoning 原样保留（仅保留加密内容），裸 reasoning 与 `item_reference` 丢弃（`store:false` 的上游拒绝它们）；`image_generation` 只保留仍带内联数据的 item，瘦身项（仅 `id`，`result: null`）丢弃——`store:false` 下上游不持久化 item，回放瘦身项会以 `Item with id 'ig_…' not found` 404 整个请求；各 item 上的 `internal_chat_message_metadata_passthrough` 会剥离。
10. 图片输入（**通常走原生通路**）：默认开启 `native_fallback` 时，含 `input_image` 的请求会路由到原生 Codex 上游（见下方「双通道路由」），本条描述的附件化上传**仅适用于 BPS 通路**——即路由关闭（`native_fallback: false`）或未触发时。用户消息里的内联 `input_image`（`data:` URL，含 `{"url":…}` 字典形态）会被解码后以 multipart 上传到与 `/responses` 同目录的 `attachments` 端点（字段名 `file`，文件名固定 `image.<ext>`），拿到 `openai_file_id` 后替换为 `{"type":"input_image","file_id":…,"detail":…}`（`detail` 缺省补 `"auto"`）。直接把 data URL 发给上游会 422。**仅支持 JPEG/PNG/GIF/WebP**：媒体类型按别名归一（`image/jpg`、`image/pjpeg`→`image/jpeg`，`image/x-png`→`image/png`），扩展名由显式映射决定（不依赖系统 MIME 表，否则会出现无扩展名或 `.jpe` 这类上游不认的后缀，报 `Expected image type … but got none`）；其它格式（HEIC/AVIF/TIFF/BMP 等）在发出主请求前明确报错，不做格式转换。已有 `file_id`、远程 `https://` 图片、assistant 消息与工具结果里的图片原样保留；同内容图片按摘要缓存复用 file id（缓存按端点+凭据隔离，失败不缓存），并发同图合并为一次上传。上传失败在发出主请求前中止（`ATTACHMENT_UPLOAD_FAILED`，`request_sent=false`），错误文本会脱敏凭据与图片字节。上传发生在 turn/task 标识计算之后，图片引用不会改变会话身份。

SSE 中的普通文本保持流式输出；工具调用等待 `response.output_item.done` 到齐、回放状态保存成功后，才输出对应的 added / delta / arguments.done / item.done。终结响应中的工具也同步转换，usage 保留。无效载荷、未知工具、KV 不可用或工具流截断都返回明确错误，不执行代码、不伪造结果。

## 0.4.3 失败请求正文

请求失败时，默认记录插件收到、尚未进行 BPS 改写的原始 body。保留提示词、对话历史、工具 schema、code/input/arguments、数字精度和空白，也保留无法解析的 JSON；宿主在调用插件之前做过的改写不属于本插件的捕获范围。成功请求和最终恢复成功的内部重试不记录正文。

- 事件为 WARN 级别的 `bps.failed_request_body`，每次 Forward 最多记录一份，使用 `request_id` 和独立 `trace_id` 关联故障、路由和上游尝试。HTTP 4xx/5xx、工具桥接错误、请求解析错误、连接/读取错误，以及 BPS/native 的语义失败均进入现有失败诊断路径。
- 正文按原始字节最多 8 KiB 分段，UTF-8 在字符边界切分，避免 JSON 转义后超过 go-plugin 的 64 KiB 单行限制。每段包含 `request_body`、`part`、`parts`、`request_body_bytes`、`request_body_sha256`、`request_body_complete` 和 `request_body_encoding`。
- 按同一 `trace_id` 检索 `bps.failed_request_body`，按 `part` 升序拼接 `request_body`，确认段数、字节数和 SHA-256 一致。极少数非 UTF-8 请求使用 `base64`，应先逐段解码再拼接。宿主采样、日志级别、队列丢弃与保留策略仍可能影响留存；缺段不能视为完整请求。
- 配置页只保留每条失败正文的前 8 KiB 预览，最多 50 条故障；可查看 `available`、`complete`、`preview_truncated`、总字节数和 SHA-256。完整正文只写入宿主日志，不在 Health 中积累。未读取到正文的早期拒绝标记 `available: false`；读取中断只保留已接收部分，`complete: false`。
- native 响应观察器仅识别错误状态，不改写转发字节。JSON 响应和单个 SSE data 事件的检查上限为 256 KiB；超大事件独立跳过，不阻止识别后续错误事件，显式 `event: response.failed/error/response.incomplete` 不受 data 大小影响。
- **正文包含用户提交的完整内容，不进行自动脱敏。** 不额外采集 HTTP Authorization/Cookie、OAuth 凭据、代理 URL 或上游响应正文；若用户把凭据写入 body，该内容也会按原文记录。客户端错误响应仍使用既有的脱敏结构，不附加日志正文。

完整日志要求宿主已包含 0.4.2 配套的 `plugin_logger.go` / `plugin_runtime.go` 改动；旧宿主的空 logger 会丢弃日志，只能查看配置页中的有界预览。

## 0.4.2 排障日志基础

以下结构化诊断自 0.4.2 引入；0.4.3 的失败正文策略以上一节为准。

默认开启脱敏结构化日志，无需打开 debug。使用已有的 go-hclog / go-plugin 日志协议，通过宿主 slog 接入现有控制台、日志文件及 Ops 系统日志管线；不会写入承载 RPC 握手的 stdout。

- 请求关联：入口 request_id、每次插件 Forward 独立 trace_id、账号 ID、模型、插件版本、BPS/native 路由及原因、上游尝试次数、流式标记、HTTP 状态、上游 X-Request-ID 和累计耗时。
- 关键事件：bps.route_selected、bps.upstream_response、bps.upstream_retry、bps.tool_rejected、bps.request_failed、bps.request_finished。工具拒绝使用 WARN；一般成功流程使用 INFO，不逐个记录 token/delta。
- 工具拒绝：记录源 SSE 事件（或 json_response）、已知的响应 ID、实际 name/namespace/qualified_name、客户端工具数量、校验阶段与固定原因、arguments/references/code 的类型及长度、references 数量、已声明的目标工具，以及 JSON 出错字段和字节偏移。未知 references 的值不记入日志。code_json_type 和 legacy_envelope 只描述结构，不记录正文，也不改变校验或恢复旧信封协议。
- 元数据边界：身份字段限制为不超过 128 字节的 ASCII 字母、数字、下划线、连字符和点；不符合要求整体脱敏。不额外采集认证头、代理 URL、上游响应正文或任意原始异常文本。0.4.3 失败请求正文按上一节单独记录。
- 流式语义失败即使正常结束 RPC，也会生成一条工具拒绝诊断；日志与已有失败响应分别处理。日志观察器不改写参数、重放状态或返回结果。
- 插件配置页提供“最近故障诊断与请求正文”，每 10 秒刷新，显示最近 50 条故障，最新在前，可复制 JSON。数据来自 Health.status_json.recent_diagnostics，是有界内存快照，插件进程重启后清空；长期留存遵循宿主的日志保留策略。

**查看方式与升级要求：**

1. 只升级 0.4.3 插件，即可在插件配置页查看最近故障；不会为了记录诊断访问外部接口或额外写数据库。
2. 要在宿主日志或 Ops 系统日志中检索 bps.tool_rejected，需要同时包含 0.4.2 配套的宿主 plugin_runtime.go / plugin_logger.go 改动。原宿主使用空 logger，会丢弃插件标准错误日志；只升级插件无法改变宿主行为。新宿主同时把入口 request_id 传给插件，关联错误详情中的请求 ID；缺少 HTTP 请求上下文时使用独立生成的 ID。旧宿主下可按故障时间、账号和可用的上游响应 ID 对照配置页诊断。
3. 在系统日志中按组件 plugin、事件 bps.tool_rejected 和 request_id 过滤；继续查看相同 trace_id 的路由、上游尝试及结束记录。Ops 展示仍遵循宿主当前日志级别、采样与留存配置。
4. stage=upstream_tool_identity 表示执行器/工具身份不匹配；upstream_tool_arguments 表示外层或直接工具参数问题；upstream_tool_references 表示路由目标问题；upstream_tool_code 表示 code 类型或 JSON 语法问题；upstream_tool_choice 表示工具选择约束不一致。应先按阶段定位，不能仅凭 HTTP 400 推断为余额或 API Key 问题。

## 0.4.1 工具身份查找与拒绝诊断

- 排查 #478069 时发现，原实现无条件拼接 namespace 与 name；当 name 已包含相同命名空间时，会得到 `functions.functions.run_officejs` 并误拒绝。改为仅在名称尚未限定时添加前缀；客户端直接调用、传输执行器、历史重建和 tool_choice 使用同一查找规则。原始 item 和命名空间仍完整保留在 KV 回放记录中。
- #478069 的线上记录没有保存被拒绝的 name/namespace，不能确认上述可复现缺陷就是该次错误的实际原因；也没有证据证明应将某个未知 Office/connector 工具放行。本版不放宽未知执行器校验、不猜测工具意图、不恢复旧信封协议。
- 工具身份拒绝的 `error.diagnostics` 包含 `stage: upstream_tool_identity`、call_type、name、namespace、qualified_name 和 catalog_tools 数量。标识符仅允许有界的 ASCII 字母、数字、下划线、连字符与点；其它内容整体替换为 `<redacted>`。不输出参数、code、input、summary、references、调用 ID 或完整目录。
- JSON 与 SSE 均传递这份诊断；无效调用仍只产生失败事件，不交付可执行输出。回归覆盖带 namespace 的短名/完整名、直接 function/custom、流式快照、原始回放、强制工具选择及 HTTP/gRPC 错误脱敏。尚未完成真实 BPS 模型的线上复现。

## 0.4.0 工具路由与载荷分离

- 错误 #478021 的 `TOOL_BRIDGE_CALL_INVALID` 来自插件对上游工具调用的解析，不是余额或认证校验。线上记录只保留转换后的失败事件，未保存原始工具参数，不能据此断言某一个字符或某一种上游生成格式就是该次错误的根因。
- 统一使用 `references: [完整工具名]` 路由，`code` 仅含参数 JSON 或 custom 原文，减少工具信封的嵌套序列化。删除旧 tool/args、name/arguments 信封解包、summary 标记及旧摘要回退解析；新调用不再接受这些旧协议，也不把工具信封挪进 connector。
- 目录提示、客户端历史重建、JSON 响应和 SSE 转换遵守同一协议。KV 中已交付调用的原始 item 仍作为真实历史原样回放，这不是旧协议的新调用入口；提示明确要求不要模仿历史旧格式。
- function 参数和历史回放使用 JSON 原始字节，避免大整数或精确小数经过浮点解码后失真。无效历史参数明确拒绝，不再静默替换成空对象。
- 错误区分外层 arguments、references 路由、code 类型和 JSON 语法；语法错误仅提供固定字段名和字节偏移，不包含工具参数、代码片段或认证信息。坏调用仍不会交付客户端执行。
- 本地回归覆盖流式/非流式、仅终结快照、custom 原文及 JSON 外观输入、数字精度、回放、非法 references、旧格式拒绝和参数脱敏；这些验证不代表真实 BPS 模型上的调用成功率已得到验证。

2026-09-26 对照的参考版本：

- [JaxsonWang/cpa-plugin-oai-basispoints@1b9359a](https://github.com/JaxsonWang/cpa-plugin-oai-basispoints/blob/1b9359ae1eda41b0eee556af8ed6edd059d7f8e7/internal/basispoints/protocol.go)：采用其路由字段与载荷分离的模式，保留本插件完整原始 item 的 KV 回放机制。
- [zhu961212/sub2api-oai-basispoints@5b4afc1](https://github.com/zhu961212/sub2api-oai-basispoints/blob/5b4afc1c01267190277e62eb4c3253ae2ed28015/internal/protocol/protocol.go) 和 [Nonary/ghcp_proxy@dfb758b](https://github.com/Nonary/ghcp_proxy/blob/dfb758b181e5caa6c52183ef957232140c384dcb/excel_upstream.py)：对照了旧嵌套信封、工具结果回放和格式恢复路径；本版未引入它们的多格式兼容或猜测性解包。

## 0.3.6 加密 agent 历史路由修复

- 含非空 encrypted_content 的 agent_message 不进入 BPS 重写；插件将原始请求逐字节交给 native Codex 通道，由原生端保留并解密历史。
- 只有明确由 input_text/text 构成的 agent_message 才进入 BPS，并按顺序转换为普通 user message。native_fallback: false 时仍安全返回 TOOL_BRIDGE_REQUEST_INVALID，不猜测密文。
- 当时为旧摘要增加的 custom 回退解析已在 0.4.0 移除；当前协议见上方 v3 定义。
- 本版本针对错误 #477711 的 input[434].content[1] 路径增加了 native 原样转发测试，并针对 #477994 增加旧 custom 摘要回放测试。

## 0.3.3 多 agent 与 custom 工具修复

- function_call 默认显式返回 encrypted_function_args: []。缺省或 null 会使 Codex 根据工具 schema 推断加密参数；显式空数组才能说明本次 message 是明文。spawn_agent、send_message、followup_task 的 JSON 响应、SSE added/done 和终结快照使用相同声明。
- 已声明工具的直接调用若携带真实 encrypted_function_args 列表，原样保留；Office 执行器包装层的加密字段名不会移植到客户端业务参数。KV 仍保存完整上游原始调用，回放不篡改加密元数据。
- agent_message 只接受显式明文字符串或 input_text/text 内容段，按原顺序无损连接任务信封与正文并转换为 user message。首轮任务与后续通信都建立新 turn，工具往返沿用所属 turn；重试不增加迭代次数，空消息不重置 turn。
- 既有 agent_message 的 encrypted_content 一律不猜测、不删除、不当作明文；即使看起来可读也不能证明未加密。请求在发送上游前返回 TOOL_BRIDGE_REQUEST_INVALID：非流式 HTTP 400，流式一个 response.failed 并正常结束 RPC。诊断仅包含索引路径和固定原因，不包含任务正文。已污染的会话需要重新创建并派发任务。
- 当时使用的 summary 显式标记已在 0.4.0 被 references 路由替代；custom 的原文保留约定和禁止猜测、修补可执行代码的边界不变。
- 测试覆盖 JSON/SSE 的明文加密声明、父会话工具回放、子会话首轮与后续消息、原文 custom 输入及回放，以及不安全任务在网络请求前失败。继承的 reasoning/compaction 密文保持原样；本修复不代表它们已经能够跨账号或跨通路解密。

0.3.3 当时参考的版本（0.4.0 的参考版本见上方）：

- [ranxi2001/sub2api 的多 agent 回归](https://github.com/ranxi2001/sub2api/blob/3e345632fd66aea7724c5929e9415a828a4ae83b/backend/internal/service/basispoints/agent_message_test.go)：显式空加密字段列表、拒绝凭外观识别密文；其 [custom 传输](https://github.com/ranxi2001/sub2api/blob/3e345632fd66aea7724c5929e9415a828a4ae83b/backend/internal/service/basispoints/custom_transport.go) 曾用于 0.3.3 的显式标记设计，当前已由 v3 取代。
- [Nonary/ghcp_proxy](https://github.com/Nonary/ghcp_proxy/blob/1a73157d579dcdaa08d8ecbcd166e80ca48c9e66/excel_upstream.py) 与 [JaxsonWang/cpa-plugin-oai-basispoints](https://github.com/JaxsonWang/cpa-plugin-oai-basispoints/blob/0b47e11cecc3213774927044e3518ea4a770cd24/internal/basispoints/protocol.go) 用于请求体和工具协议对照；所检视版本未提供对应的 agent_message 专用修复，不能据此宣称多 agent 已恢复。

## 0.3.2 修复与恢复边界

- 修复非法工具信封原样泄漏导致 Codex 连续报告 unsupported call: run_officejs。包括嵌套 JSON 中非法的单引号转义；客户端只会收到已校验的工具调用或明确错误。namespace custom 工具的输入增量也等待完整校验。
- BPS SSE 收到终结事件立即结束，不继续等待 HTTP 连接关闭；读取超时单独报告 UPSTREAM_RESPONSE_TIMEOUT，避免被吞成不明读取错误。该修改不表示可以消除上游真正的中断或超时。
- invalid_encrypted_content 支持 HTTP 错误和 HTTP 200 中的早期 SSE 失败。只有在文本、推理或工具输出之前，才允许清理一次并重试；SSE 探测上限为 32 个前导事件、256 KiB，普通无密文请求不探测。
- 清理发生在 Prepare 恢复 KV 原始调用之后，保持 turn_id、agent_iteration 和真实工具结果。仅移除可选 reasoning 或有完整可读副本的协议密文字段；不递归修改工具业务参数或字符串 JSON，不改写 KV 原始记录。
- 无法确认完整副本时，不在 BPS 通道继续重试，也不改写或删除原始历史；插件立即把原始请求体交给原生 Codex 通道，原生通道失败时才返回原生错误。插件内部仍保留结构化诊断（input 索引路径、白名单类型和原因），不包含密文、调用 ID 或工具正文。显式文本（包括空字符串）和已验证的内联媒体可作为 BPS 重试副本；null/空串密文元数据本身不构成加密历史。
- 工具校验失败和加密回放拒绝按语义错误交付：流式发送一个 response.failed 并正常结束 RPC；非流式返回 HTTP 400 的结构化错误。不会返回可执行的坏工具调用；流式序号、响应 ID 和已有文本保留。无效 JSON 的通用诊断不再一概归因于单引号转义。
- 宿主按结构化错误码区分协议失败与断流；工具协议错误不换号、不触发代理隔离，已交付终端错误后不追加通用错误。真正的网络断流仍执行原有熔断策略。上游 invalid_encrypted_content 在 BPS 一次安全清理失败后转原生，不通过换号重复发送。

## 按模型选择 BPS 通道

配置页提供“全部模型”和“仅指定模型”。例如以下配置只允许插件收到的 model-a 请求进入 BPS 适配，其余模型直接走原生端点：

```json
{
  "bps_model_mode": "selected",
  "bps_models": ["model-a"]
}
```

- bps_model_mode 默认 all；selected 按 bps_models 名单匹配，名单为空时全部走原生。
- 名单最多 64 个，区分大小写、精确匹配，不支持通配符；使用插件收到的模型名，在插件 model_mapping 和去除 -excel 后缀之前判定。宿主若已有模型映射，应填写宿主实际交给插件的名字。
- 名单外请求不受 native_fallback 开关影响，模型名、工具定义和请求体均原样转发。名单内仍遵循图片/结构化输出路由和 tools_via_native 设置。
- 不内置“哪些模型支持 BPS”的猜测名单。切换通道不会解密既有历史；带不透明历史的会话可能仍需重新提供结果或新建会话。

## 客户端工具走原生通道（tools_via_native）

BPS 注入的执行器套件**不稳定**：有的账号/时段拿到带 `run_officejs` 的正版套件，有的只拿到通用套件（`functions.read_ranges`/`run_connector_action` 等，无 `run_officejs`），此时走私协议结构性无法工作。而客户端工具（`exec_command` 等）在**原生 Codex 通道**是模型的一等工具，不需要任何协议。

`tools_via_native`（**默认关闭**）：工具会话默认留在 BPS 桥接。打开后，携带客户端 function/custom 工具声明（顶层 `tools[]` 或 `additional_tools` 载体）的请求整体走原生通道——请求体一字不动、响应原样回传。仅当 BPS 套件缺 `run_officejs` 时才建议开启。配置页有对应复选框。

## Codex 身份头（ensureCodexIdentity）

上游按客户端身份校验（originator 与 UA 首段配对）并按身份分优先级；身份缺失的请求会被当作匿名客户端，拿到通用工具栏（`web.run`/`multi_tool_use.parallel` 一系）而非 Codex 执行器套件。插件在所有出站请求上补齐：`originator`（`codex-tui`/`codex_cli_rs`，由 UA 推导）、`version`（与 UA 版本段逐字一致，低于上游门槛 0.146.0 时一并重写 UA）、`OpenAI-Beta: responses=experimental`。非 Codex 形态 UA 采用规范身份。

## 模型映射

插件配置里的 `model_mapping`（JSON 对象）仅在 BPS 通道把收到的请求模型名翻译成上游 slug；原生通道不改写模型名：

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
- 上游返回的每个工具调用都必须匹配当前客户端目录并通过参数类型校验。有效的执行器载荷转换为实际客户端工具；客户端已声明的直接调用也接受校验。function 的无效 JSON、未知工具及错误参数类型返回 TOOL_BRIDGE_CALL_INVALID；custom 原文不解析为 JSON。不透传 Office 执行器、不猜测或修复可执行代码。
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
SUB2API_TEST_BPS_PACKAGE="$PWD/plugins/openai-basispoints-transport/dist/openai-basispoints-transport-0.4.3.s2plugin" \
SUB2API_TEST_BPS_RUNTIME=darwin-arm64 \
go test ./plugins/openai-basispoints-transport/internal/transport -run '^TestPackagedPluginToolReplay$' -count=1
```

`build.sh` 不执行测试，不删除旧版包。只需 Linux 部署时设 `TARGETS=linux-amd64`。生成的包位于 `dist/openai-basispoints-transport-0.4.3.s2plugin`。配置页测试使用仓库已有的 frontend jsdom 开发依赖，在 backend 目录运行 `node --test plugins/openai-basispoints-transport/tools/ui-config.test.cjs`。

不设置签名参数时输出无 `signature.json` 的开发包，适用于已明确配置 `plugins.allow_unsigned: true` 的宿主。签名构建：

```bash
SIGNING_KEY=/secure/path/publisher.private KEY_ID=my-publisher-v1 TARGETS=linux-amd64 ./build.sh
```

签名包仍要求对应公钥存在于 `plugins.trusted_publishers` 中；允许未签名包不等于信任未知签名。不要把私钥提交进源码或插件包。

## 安装与观察

先确认宿主已包含 0.3.2 引入的插件语义错误分类，再停用已安装的同 ID 插件，上传 0.4.3，保存配置并选择账号和 BPS 模型范围。当前工具传输协议不接受旧格式的新调用。使用专用测试账号开始新会话，先验证普通文本，再验证 function/custom 工具调用及回放，随后验证子 agent 首轮、send_message 和 followup_task，再考虑扩大账号范围。若仍遇到工具身份拒绝，可通过 error.diagnostics 区分真实的未知执行器与名称问题。该版本未声明完整线上联调通过，需要确认“未测试版本”提示；本地测试和打包验证不等于真实 BPS 联调通过。

配置页每 10 秒显示请求数、成功/失败数、最近 HTTP 状态、KV 连接情况及最近桥接错误码。请求数增加说明请求进入了本插件；成功数、工具调用和回放均成功才说明相应链路可用。“校验配置”仅检查配置，不调用模型。
