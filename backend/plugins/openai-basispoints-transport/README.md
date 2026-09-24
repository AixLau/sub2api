# Basis Points Responses 工具桥接插件

0.2.0 是根据本次提供的 BPS 实测协议实现的独立 Sub2API 插件，仅修改插件工程。默认上游是 `https://bps.openai.com/basispoints/api/responses`，只接受 `POST /responses`。宿主负责凭据刷新、账号调度、下游协议与计费；插件复用该次请求已经携带的 OAuth Authorization 和 ChatGPT 账号 ID。

## 0.2.0 的请求与回放协议

1. 删除请求体中的 `tools`、`tool_choice`、`parallel_tool_calls`。将客户端工具目录写入 developer 消息的普通文本。
2. 描述约定：调用客户端工具时，通过 `run_officejs.arguments.code` 传递 `{"tool":"get_weather","args":{"city":"Tokyo"}}`。插件只解析 JSON，不执行 OfficeJS 或任何客户端工具。
3. 解码完整的 `run_officejs` item，识别两层 JSON，校验目标是本次声明的客户端工具，再输出标准 `function_call`。支持 namespace 内的 function，以及 custom 工具对应的 `custom_tool_call`。
4. 通过宿主 HostService KV 保存完整上游 item，包含 `id`、`call_id`、`arguments` 内外的 `summary`、`references` 和未知字段。客户端收到不透明的 `call_bps_…` 标识，按正常流程决定是否执行工具。
5. 客户端提交工具结果时，插件恢复原始调用 item 和原始 `call_id`，即使客户端只提交了工具结果也能回放。相同调用 item 不重复插入。
6. `turn_id`、`agent_iteration` 放在请求体顶层。工具往返沿用原 turn，并在上一调用迭代的基础上加一；同一请求重试不再额外递增。新的 user 消息开启新 turn。显式传入的 turn 与回放状态冲突时直接报错。

SSE 中的普通文本保持流式输出；工具 envelope 等待 `response.output_item.done` 到齐、回放状态保存成功后，才输出对应的 added / delta / arguments.done / item.done。终结响应中的工具也同步转换，usage 保留。无效 envelope、未知工具、KV 不可用或工具流截断都返回明确错误，不执行代码、不伪造结果。

## 边界与已知限制

- **尚未通过真实 BPS 账号联调验证**。测试使用用户提供的协议样例和本地模拟上游，验证 JSON、SSE、gRPC、跨进程回放与打包运行时。公开 Responses 文档不能证明 BPS 对所有请求字段、模型、工具和额度的支持。
- 需要宿主提供 HostService KV。记录按账号、上游地址及宿主隔离后的 session 标识隔离，保留 24 小时。KV 包含工具调用参数，可能含业务数据；不保存 OAuth 认证头或 refresh token。
- 启用后应开始新的客户端会话。旧 Codex 会话里的工具历史没有完整 BPS item，插件会拒绝伪造回放。切换账号或丢失 session/KV 状态后，也需要新会话。
- 仅桥接客户端 function/custom 工具。`image_generation`、`web_search` 等托管工具声明会移除，不会被转成并不存在的客户端函数；被移除的类型可在插件状态页查看。
- 上游直接返回 `update_plan`、`write_range` 等非 envelope 调用时明确报错。插件不会自动执行或伪造 Office 工具结果，也不会自动循环追加上游请求。
- 当前不支持 compact、图片专用接口或计数接口；此版本不会把这些请求伪装成普通 Responses。
- 每个 HTTP 请求 JSON 上限 64 MiB；每个回放 KV 记录上限 240 KiB；每次响应最多 128 个工具调用；每个 turn 最多 64 轮。错误发生在发出上游请求之后时，返回 `request_sent=true`，防止宿主重复执行。
- `tool_choice` 通过文本约定和返回校验表达，无法保证模型与原生 API 的行为完全一致。`max` 等 reasoning 档位不做自动降档，以免隐藏行为变化。

## 构建与验证

在仓库根目录运行：

```bash
cd backend
go test -race ./plugins/openai-basispoints-transport/... -count=1
TARGETS=linux-amd64,darwin-arm64 ./plugins/openai-basispoints-transport/build.sh
SUB2API_TEST_BPS_PACKAGE="$PWD/plugins/openai-basispoints-transport/dist/openai-basispoints-transport-0.2.0.s2plugin" \
SUB2API_TEST_BPS_RUNTIME=darwin-arm64 \
go test ./plugins/openai-basispoints-transport/internal/transport -run '^TestPackagedPluginToolReplay$' -count=1
```

`build.sh` 不执行测试，不删除旧版包。只需 Linux 部署时设 `TARGETS=linux-amd64`。生成的包位于 `dist/openai-basispoints-transport-0.2.0.s2plugin`。

不设置签名参数时输出无 `signature.json` 的开发包，适用于已明确配置 `plugins.allow_unsigned: true` 的宿主。签名构建：

```bash
SIGNING_KEY=/secure/path/publisher.private KEY_ID=my-publisher-v1 TARGETS=linux-amd64 ./build.sh
```

签名包仍要求对应公钥存在于 `plugins.trusted_publishers` 中；允许未签名包不等于信任未知签名。不要把私钥提交进源码或插件包。

## 安装与观察

先停用已安装的同 ID 插件，再上传 0.2.0，保存配置并选择账号。该版本未声明完整宿主联调通过，需要确认“未测试版本”提示。使用专用测试账号开始新会话，先验证普通文本，再验证一次工具调用及回放，最后才扩大账号范围。

配置页每 10 秒显示请求数、成功/失败数、最近 HTTP 状态、KV 连接情况及最近桥接错误码。请求数增加说明请求进入了本插件；成功数、工具调用和回放均成功才说明相应链路可用。“校验配置”仅检查配置，不调用模型。
