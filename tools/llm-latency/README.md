# llm-latency

测大模型流式接口的 **首字延迟（TTFT）** 和 **总耗时**。单文件，仅依赖 `httpx`。

```bash
pip install httpx
export LLM_API_KEY=sk-xxx
python tools/llm-latency/llm_latency.py --base-url http://127.0.0.1:8080 --model gpt-5 -n 5 -c 2
```

## 指标

| 指标 | 含义 |
|---|---|
| TTFB | 响应头到达时间（仅在 `--json` 明细中） |
| TTFT | 请求发出 → 第一个非空文本增量（含 reasoning 增量） |
| 总耗时 | 请求发出 → 流结束（`[DONE]` / `message_stop` / `response.completed`） |
| tok/s | 输出 token 数 ÷ (总耗时 − TTFT)；上游无 usage 时按字符数估算，数值后带 `~` |

输出 min / avg / p50 / p95 / max，以及成功率。失败请求不计入统计，错误原因打印到 stderr。

## 常用参数

- `--protocol openai|anthropic|responses`：对应 `/v1/chat/completions`、`/v1/messages`、`/v1/responses`
- `--base-url` 可重复，支持 `name=url`；`--model` 可重复或逗号分隔 → 多端点 × 多模型逐组对比
- `-n` 每组请求数，`-c` 并发数，`--warmup` 预热（建连/TLS，不计入）
- `--max-tokens`、`--prompt`、`--timeout`、`--no-proxy`、`--json`

`--base-url` 以 `/v1` 结尾时不会重复拼接 `/v1`。

## 示例

```bash
# 对比两个网关、两个模型，Anthropic 协议，10 次、并发 5
python llm_latency.py --base-url a=http://x:8080 --base-url b=http://y:8080 \
  --model claude-sonnet-4-5,claude-haiku-4-5 --protocol anthropic -n 10 -c 5

# 导出逐次明细
python llm_latency.py --base-url http://127.0.0.1:8080 --model gpt-5 -n 20 --json > out.json
```

注意：并发较高时，TTFT 会包含网关/上游排队时间，这正是压测想观察的现象。
