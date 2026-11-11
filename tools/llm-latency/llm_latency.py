#!/usr/bin/env python3
"""测试大模型流式接口的首字延迟（TTFT）与总耗时。

支持协议：OpenAI chat/completions、Anthropic /v1/messages、OpenAI Responses。
依赖：httpx（pip install httpx）。

示例：
  export LLM_API_KEY=sk-xxx
  python llm_latency.py --base-url http://127.0.0.1:8080 --model gpt-5 -n 5 -c 2
  python llm_latency.py --base-url a=http://x:8080 --base-url b=http://y:8080 \
      --model m1,m2 --protocol anthropic -n 10 -c 5 --json
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from dataclasses import dataclass, field, asdict
from typing import Any, Dict, List, Optional, Tuple

try:
    import httpx
except ImportError:  # pragma: no cover
    sys.exit("缺少依赖 httpx，请先执行：pip install httpx")

PROTOCOLS = ("openai", "anthropic", "responses")
DEFAULT_PROMPT = "请用中文写一段约 200 字的短文，介绍一下 HTTP 流式传输（SSE）的原理。"


# --------------------------------------------------------------------------- #
# 协议适配：构造请求 / 解析单条 SSE data
# --------------------------------------------------------------------------- #
def build_url(base: str, protocol: str) -> str:
    base = base.rstrip("/")
    path = {
        "openai": "/chat/completions",
        "anthropic": "/messages",
        "responses": "/responses",
    }[protocol]
    return base + path if base.endswith("/v1") else base + "/v1" + path


def build_request(protocol: str, model: str, prompt: str, max_tokens: int,
                  api_key: str) -> Tuple[Dict[str, str], Dict[str, Any]]:
    headers = {"Content-Type": "application/json", "Accept": "text/event-stream"}
    if protocol == "anthropic":
        headers["x-api-key"] = api_key
        headers["anthropic-version"] = "2023-06-01"
        body = {
            "model": model,
            "max_tokens": max_tokens,
            "stream": True,
            "messages": [{"role": "user", "content": prompt}],
        }
    elif protocol == "responses":
        headers["Authorization"] = f"Bearer {api_key}"
        body = {
            "model": model,
            "input": prompt,
            "max_output_tokens": max_tokens,
            "stream": True,
        }
    else:
        headers["Authorization"] = f"Bearer {api_key}"
        body = {
            "model": model,
            "max_tokens": max_tokens,
            "stream": True,
            "stream_options": {"include_usage": True},
            "messages": [{"role": "user", "content": prompt}],
        }
    return headers, body


@dataclass
class Event:
    text: str = ""                       # 本事件携带的文本增量
    out_tokens: Optional[int] = None     # 上游报告的输出 token 数
    done: bool = False
    error: Optional[str] = None


def parse_event(protocol: str, data: str) -> Event:
    """解析一条 SSE data。首字 = 第一个非空文本增量（含 reasoning 增量）。"""
    if data == "[DONE]":
        return Event(done=True)
    try:
        obj = json.loads(data)
    except ValueError:
        return Event()
    if not isinstance(obj, dict):
        return Event()
    if "error" in obj and obj["error"]:
        return Event(error=json.dumps(obj["error"], ensure_ascii=False)[:300])

    ev = Event()
    if protocol == "openai":
        for ch in obj.get("choices") or []:
            delta = ch.get("delta") or {}
            ev.text += (delta.get("content") or "") + (delta.get("reasoning_content") or "")
        usage = obj.get("usage")
        if usage and usage.get("completion_tokens") is not None:
            ev.out_tokens = usage["completion_tokens"]
    elif protocol == "anthropic":
        t = obj.get("type")
        if t == "content_block_delta":
            d = obj.get("delta") or {}
            ev.text = d.get("text") or d.get("thinking") or ""
        elif t == "message_delta":
            usage = obj.get("usage") or {}
            if usage.get("output_tokens") is not None:
                ev.out_tokens = usage["output_tokens"]
        elif t == "message_stop":
            ev.done = True
        elif t == "error":
            ev.error = json.dumps(obj.get("error"), ensure_ascii=False)[:300]
    else:  # responses
        t = obj.get("type", "")
        if t in ("response.output_text.delta", "response.reasoning_summary_text.delta",
                 "response.reasoning_text.delta"):
            ev.text = obj.get("delta") or ""
        elif t in ("response.completed", "response.incomplete"):
            ev.done = True
            usage = (obj.get("response") or {}).get("usage") or {}
            if usage.get("output_tokens") is not None:
                ev.out_tokens = usage["output_tokens"]
        elif t in ("response.failed", "error"):
            ev.error = json.dumps(obj, ensure_ascii=False)[:300]
    return ev


# --------------------------------------------------------------------------- #
# 单次请求
# --------------------------------------------------------------------------- #
@dataclass
class Sample:
    ok: bool = False
    ttfb: Optional[float] = None         # 响应头到达（秒）
    ttft: Optional[float] = None         # 首字（秒）
    total: Optional[float] = None        # 总耗时（秒）
    out_tokens: Optional[int] = None
    tokens_estimated: bool = False
    chars: int = 0
    tps: Optional[float] = None          # 输出 tokens/s（不含首字等待）
    error: Optional[str] = None


async def one_request(client: "httpx.AsyncClient", url: str, protocol: str,
                      model: str, prompt: str, max_tokens: int, api_key: str) -> Sample:
    headers, body = build_request(protocol, model, prompt, max_tokens, api_key)
    s = Sample()
    last_data = ""
    t0 = time.perf_counter()
    try:
        async with client.stream("POST", url, headers=headers, json=body) as resp:
            s.ttfb = time.perf_counter() - t0
            if resp.status_code != 200:
                raw = (await resp.aread()).decode("utf-8", "replace")
                s.error = f"HTTP {resp.status_code}: {raw[:200].strip()}"
                s.total = time.perf_counter() - t0
                return s
            async for line in resp.aiter_lines():
                if not line.startswith("data:"):
                    continue
                last_data = line[5:].strip()
                ev = parse_event(protocol, last_data)
                if ev.error:
                    s.error = ev.error
                    break
                if ev.text:
                    if s.ttft is None:
                        s.ttft = time.perf_counter() - t0
                    s.chars += len(ev.text)
                if ev.out_tokens is not None:
                    s.out_tokens = ev.out_tokens
                if ev.done:
                    break
            s.total = time.perf_counter() - t0
    except httpx.TimeoutException:
        s.error = "超时"
        return s
    except httpx.HTTPError as e:
        s.error = f"{type(e).__name__}: {e}"[:200]
        return s

    if s.error is None and s.ttft is None:
        s.error = f"流结束但未收到任何文本；最后事件: {last_data[:160] or '(无)'}"
    if s.error is None:
        s.ok = True
        if s.out_tokens is None:  # 上游没给 usage：按字符粗估（约 2 字符/token）
            s.out_tokens = max(1, round(s.chars / 2))
            s.tokens_estimated = True
        gen = (s.total or 0) - (s.ttft or 0)
        if gen > 0 and s.out_tokens > 1:
            s.tps = (s.out_tokens - 1) / gen if not s.tokens_estimated else s.out_tokens / gen
    return s


# --------------------------------------------------------------------------- #
# 批量执行与统计
# --------------------------------------------------------------------------- #
@dataclass
class GroupResult:
    endpoint: str
    model: str
    samples: List[Sample] = field(default_factory=list)


async def run_group(name: str, base: str, model: str, args: argparse.Namespace) -> GroupResult:
    url = build_url(base, args.protocol)
    limits = httpx.Limits(max_connections=max(args.concurrency, 1) * 2,
                          max_keepalive_connections=max(args.concurrency, 1) * 2)
    timeout = httpx.Timeout(args.timeout, connect=min(args.timeout, 15.0))
    res = GroupResult(endpoint=name, model=model)
    async with httpx.AsyncClient(limits=limits, timeout=timeout,
                                 trust_env=not args.no_proxy, http2=False) as client:
        for _ in range(args.warmup):  # 预热：建连/TLS，不计入统计
            await one_request(client, url, args.protocol, model, args.prompt,
                              args.max_tokens, args.api_key)
        sem = asyncio.Semaphore(args.concurrency)

        async def task() -> Sample:
            async with sem:
                return await one_request(client, url, args.protocol, model, args.prompt,
                                         args.max_tokens, args.api_key)

        res.samples = await asyncio.gather(*(task() for _ in range(args.runs)))
    return res


def percentile(sorted_vals: List[float], p: float) -> float:
    if len(sorted_vals) == 1:
        return sorted_vals[0]
    k = (len(sorted_vals) - 1) * p / 100
    lo = int(k)
    hi = min(lo + 1, len(sorted_vals) - 1)
    return sorted_vals[lo] + (sorted_vals[hi] - sorted_vals[lo]) * (k - lo)


def stats(vals: List[float]) -> Optional[Dict[str, float]]:
    if not vals:
        return None
    v = sorted(vals)
    return {"min": v[0], "avg": sum(v) / len(v), "p50": percentile(v, 50),
            "p95": percentile(v, 95), "max": v[-1]}


def summarize(g: GroupResult) -> Dict[str, Any]:
    ok = [s for s in g.samples if s.ok]
    errors: Dict[str, int] = {}
    for s in g.samples:
        if s.error:
            errors[s.error] = errors.get(s.error, 0) + 1
    tps = [s.tps for s in ok if s.tps]
    return {
        "endpoint": g.endpoint,
        "model": g.model,
        "ok": len(ok),
        "total_requests": len(g.samples),
        "ttfb": stats([s.ttfb for s in ok if s.ttfb is not None]),
        "ttft": stats([s.ttft for s in ok if s.ttft is not None]),
        "total": stats([s.total for s in ok if s.total is not None]),
        "tokens_per_sec_avg": (sum(tps) / len(tps)) if tps else None,
        "tokens_estimated": any(s.tokens_estimated for s in ok),
        "errors": errors,
    }


def fmt_s(v: Optional[float]) -> str:
    return "-" if v is None else f"{v:.3f}"


def print_table(summaries: List[Dict[str, Any]]) -> None:
    cols = ["端点", "模型", "成功", "TTFT avg", "TTFT p50", "TTFT p95", "TTFT min/max",
            "总耗时 avg", "总耗时 p50", "总耗时 p95", "总耗时 min/max", "tok/s"]
    rows = []
    for s in summaries:
        t, tt = s["ttft"], s["total"]
        tps = s["tokens_per_sec_avg"]
        tps_s = "-" if tps is None else f"{tps:.1f}{'~' if s['tokens_estimated'] else ''}"
        rows.append([
            s["endpoint"], s["model"], f"{s['ok']}/{s['total_requests']}",
            fmt_s(t and t["avg"]), fmt_s(t and t["p50"]), fmt_s(t and t["p95"]),
            f"{fmt_s(t and t['min'])}/{fmt_s(t and t['max'])}",
            fmt_s(tt and tt["avg"]), fmt_s(tt and tt["p50"]), fmt_s(tt and tt["p95"]),
            f"{fmt_s(tt and tt['min'])}/{fmt_s(tt and tt['max'])}", tps_s,
        ])

    def width(text: str) -> int:  # 中文按 2 宽度对齐
        return sum(2 if ord(c) > 0x2E80 else 1 for c in text)

    widths = [max(width(c), *(width(r[i]) for r in rows)) for i, c in enumerate(cols)]

    def line(cells: List[str]) -> str:
        return "  ".join(c + " " * (widths[i] - width(c)) for i, c in enumerate(cells))

    print("单位：秒；tok/s 末尾 ~ 表示按字符数估算（上游未返回 usage）")
    print(line(cols))
    print("  ".join("-" * w for w in widths))
    for r in rows:
        print(line(r))
    for s in summaries:
        for msg, n in s["errors"].items():
            print(f"[错误] {s['endpoint']} / {s['model']} x{n}: {msg}", file=sys.stderr)


# --------------------------------------------------------------------------- #
# CLI
# --------------------------------------------------------------------------- #
def parse_endpoints(values: List[str]) -> List[Tuple[str, str]]:
    out = []
    for v in values:
        if "=" in v and not v.startswith(("http://", "https://")):
            name, url = v.split("=", 1)
        else:
            name, url = v.split("//", 1)[-1].rstrip("/"), v
        out.append((name, url))
    return out


def main() -> int:
    p = argparse.ArgumentParser(description="测试大模型流式接口的首字延迟与总耗时")
    p.add_argument("--base-url", action="append", required=True,
                   help="接口地址，可重复；可写 name=url 指定显示名")
    p.add_argument("--api-key", default=os.environ.get("LLM_API_KEY", ""),
                   help="API Key，默认读取环境变量 LLM_API_KEY")
    p.add_argument("--model", action="append", required=True,
                   help="模型名，可重复或逗号分隔")
    p.add_argument("--protocol", choices=PROTOCOLS, default="openai")
    p.add_argument("--prompt", default=DEFAULT_PROMPT)
    p.add_argument("--max-tokens", type=int, default=256)
    p.add_argument("-n", "--runs", type=int, default=5, help="每个组合的请求次数")
    p.add_argument("-c", "--concurrency", type=int, default=1, help="并发数")
    p.add_argument("--warmup", type=int, default=0, help="预热请求数（不计入统计）")
    p.add_argument("--timeout", type=float, default=120.0, help="单请求读超时（秒）")
    p.add_argument("--no-proxy", action="store_true", help="忽略系统代理环境变量")
    p.add_argument("--json", action="store_true", help="输出 JSON（含逐次明细）")
    args = p.parse_args()

    if not args.api_key:
        p.error("缺少 API Key：使用 --api-key 或环境变量 LLM_API_KEY")
    if args.runs < 1 or args.concurrency < 1:
        p.error("-n 与 -c 必须 >= 1")
    models = [m.strip() for v in args.model for m in v.split(",") if m.strip()]
    endpoints = parse_endpoints(args.base_url)

    async def run_all() -> List[GroupResult]:
        out = []
        for name, base in endpoints:
            for m in models:
                print(f"测试 {name} / {m} ...", file=sys.stderr)
                out.append(await run_group(name, base, m, args))
        return out

    groups = asyncio.run(run_all())
    summaries = [summarize(g) for g in groups]
    if args.json:
        detail = [{**summ, "samples": [asdict(s) for s in g.samples]}
                  for summ, g in zip(summaries, groups)]
        print(json.dumps(detail, ensure_ascii=False, indent=2))
    else:
        print_table(summaries)
    return 0 if all(s["ok"] > 0 for s in summaries) else 1


if __name__ == "__main__":
    sys.exit(main())
