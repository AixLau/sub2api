#!/usr/bin/env python3
"""Recover Codex token usage from local session JSONL files.

The script reads metadata and token_count events only. It does not export chat
messages, tool arguments, or model responses.
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import re
import sys
from dataclasses import dataclass
from datetime import datetime, time
from pathlib import Path
from typing import Any
from zoneinfo import ZoneInfo


TOKEN_KEYS = (
    "input_tokens",
    "cached_input_tokens",
    "output_tokens",
    "reasoning_output_tokens",
    "total_tokens",
)


@dataclass
class Price:
    input: float = 0.0
    cached_input: float = 0.0
    output: float = 0.0


@dataclass
class Report:
    rows: list[dict[str, Any]]
    summary: dict[str, Any]


def build_parser() -> argparse.ArgumentParser:
    repo_root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(
        description="Recover Codex token usage from ~/.codex/sessions JSONL files."
    )
    parser.add_argument("--codex-home", default="~/.codex")
    parser.add_argument("--supplier-url", default="https://aixlau.me")
    parser.add_argument("--multiplier-from", default="2026-06-08")
    parser.add_argument("--multiplier", type=float, default=1.3)
    parser.add_argument("--timezone", default="Asia/Shanghai")
    parser.add_argument(
        "--price-file",
        default=str(
            repo_root
            / "backend/resources/model-pricing/model_prices_and_context_window.json"
        ),
    )
    parser.add_argument("--since", help="Inclusive local date, e.g. 2026-06-08")
    parser.add_argument("--until", help="Exclusive local date, e.g. 2026-06-22")
    parser.add_argument("--out", help="Write request-level CSV to this path")
    parser.add_argument("--daily-out", help="Write daily summary CSV to this path")
    parser.add_argument(
        "--total-only",
        action="store_true",
        help="Print only the final USD cost.",
    )
    parser.add_argument(
        "--include-unknown-supplier",
        action="store_true",
        help="Include sessions even when current config does not match supplier.",
    )
    return parser


def load_prices(path: Path) -> dict[str, Price]:
    data = json.loads(path.read_text(encoding="utf-8"))
    prices: dict[str, Price] = {}
    for model, value in data.items():
        if not isinstance(value, dict):
            continue
        prices[normalize_model(model)] = Price(
            input=float(value.get("input_cost_per_token") or 0.0),
            cached_input=float(value.get("cache_read_input_token_cost") or 0.0),
            output=float(value.get("output_cost_per_token") or 0.0),
        )
    return prices


def normalize_model(model: str | None) -> str:
    return (model or "unknown").strip().lower()


def read_text_if_exists(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except FileNotFoundError:
        return ""


def detect_supplier(codex_home: Path, supplier_url: str) -> tuple[str, str]:
    needle = supplier_url.rstrip("/").lower()
    checked: list[str] = []
    candidates = [
        codex_home / "config.toml",
        codex_home / "browser" / "config.toml",
        Path.home() / ".cc-switch" / "config.json",
        Path.home() / ".cc-switch" / "config.toml",
        Path.home() / ".ccswitch" / "config.json",
        Path.home() / ".ccswitch" / "config.toml",
        Path.home() / "Library" / "Application Support" / "cc-switch" / "config.json",
    ]
    for path in candidates:
        text = read_text_if_exists(path)
        if not text:
            continue
        checked.append(str(path))
        lower = text.lower()
        if needle in lower:
            if path == codex_home / "config.toml" or path == codex_home / "browser" / "config.toml":
                return "direct", str(path)
            return "via_ccswitch", str(path)

    env_blob = "\n".join(
        os.environ.get(name, "")
        for name in ("OPENAI_BASE_URL", "OPENAI_API_BASE", "CODEX_HOME")
    ).lower()
    if needle in env_blob:
        return "direct_env", "environment"
    return "unknown", ",".join(checked)


def session_files(codex_home: Path) -> list[Path]:
    paths = list((codex_home / "sessions").glob("**/*.jsonl"))
    paths.extend((codex_home / "archived_sessions").glob("**/*.jsonl"))
    return sorted(set(paths))


def parse_ts(value: str | None, tz: ZoneInfo) -> datetime | None:
    if not value:
        return None
    text = value
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        return datetime.fromisoformat(text).astimezone(tz)
    except ValueError:
        return None


def local_day_start(day: str | None, tz: ZoneInfo) -> datetime | None:
    if not day:
        return None
    return datetime.combine(datetime.fromisoformat(day).date(), time.min, tz)


def extract_model_from_filename(path: Path) -> str | None:
    match = re.search(r"(gpt-[\w.-]+|codex-[\w.-]+)", path.name, re.I)
    return match.group(1) if match else None


def usage_delta(current: dict[str, Any], previous: dict[str, int]) -> dict[str, int]:
    delta: dict[str, int] = {}
    for key in TOKEN_KEYS:
        value = int(current.get(key) or 0)
        prior = int(previous.get(key) or 0)
        diff = value - prior
        delta[key] = diff if diff > 0 else 0
    return delta


def compute_cost(delta: dict[str, int], price: Price) -> float:
    cached = delta["cached_input_tokens"]
    uncached_input = max(delta["input_tokens"] - cached, 0)
    return (
        uncached_input * price.input
        + cached * price.cached_input
        + delta["output_tokens"] * price.output
    )


def parse_session_file(
    path: Path,
    prices: dict[str, Price],
    supplier_match: str,
    supplier_basis: str,
    multiplier_from: datetime,
    multiplier: float,
    tz: ZoneInfo,
    since_dt: datetime | None,
    until_dt: datetime | None,
) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    session_id = path.stem
    model = extract_model_from_filename(path)
    previous: dict[str, int] = {}
    request_index = 0

    with path.open(encoding="utf-8", errors="replace") as handle:
        for line in handle:
            if (
                '"type":"token_count"' not in line
                and '"type": "token_count"' not in line
                and "session_meta" not in line
                and "turn_context" not in line
            ):
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            payload = obj.get("payload") or {}
            obj_type = obj.get("type")
            if obj_type == "session_meta":
                session_id = payload.get("id") or session_id
                model = payload.get("model") or payload.get("model_slug") or model
                continue
            if obj_type == "turn_context":
                model = payload.get("model") or payload.get("model_slug") or model
                continue
            if obj_type != "event_msg" or payload.get("type") != "token_count":
                continue

            ts = parse_ts(obj.get("timestamp"), tz)
            if ts is None:
                continue
            if since_dt and ts < since_dt:
                continue
            if until_dt and ts >= until_dt:
                continue

            info = payload.get("info") or {}
            total_usage = info.get("total_token_usage") or {}
            if not isinstance(total_usage, dict):
                continue
            delta = usage_delta(total_usage, previous)
            previous = {key: int(total_usage.get(key) or 0) for key in TOKEN_KEYS}
            if not any(delta[key] for key in ("input_tokens", "cached_input_tokens", "output_tokens")):
                continue

            request_index += 1
            model_name = normalize_model(model)
            price = prices.get(model_name, Price())
            base_cost = compute_cost(delta, price)
            row_multiplier = multiplier if ts >= multiplier_from else 1.0
            rows.append(
                {
                    "timestamp": ts.isoformat(),
                    "date": ts.date().isoformat(),
                    "session_id": session_id,
                    "request_index": request_index,
                    "model": model_name,
                    "input_tokens": delta["input_tokens"],
                    "cached_input_tokens": delta["cached_input_tokens"],
                    "uncached_input_tokens": max(
                        delta["input_tokens"] - delta["cached_input_tokens"], 0
                    ),
                    "output_tokens": delta["output_tokens"],
                    "reasoning_output_tokens": delta["reasoning_output_tokens"],
                    "total_tokens": delta["total_tokens"],
                    "input_price": price.input,
                    "cache_read_price": price.cached_input,
                    "output_price": price.output,
                    "base_cost": base_cost,
                    "multiplier": row_multiplier,
                    "final_cost": base_cost * row_multiplier,
                    "supplier_match": supplier_match,
                    "supplier_basis": supplier_basis,
                    "source_file": str(path),
                    "unknown_model": model_name not in prices,
                }
            )
    return rows


def summarize(rows: list[dict[str, Any]]) -> dict[str, Any]:
    summary: dict[str, Any] = {
        "requests": len(rows),
        "sessions": len({row["session_id"] for row in rows}),
        "input_tokens": sum(row["input_tokens"] for row in rows),
        "cached_input_tokens": sum(row["cached_input_tokens"] for row in rows),
        "uncached_input_tokens": sum(row["uncached_input_tokens"] for row in rows),
        "output_tokens": sum(row["output_tokens"] for row in rows),
        "reasoning_output_tokens": sum(row["reasoning_output_tokens"] for row in rows),
        "total_tokens": sum(row["total_tokens"] for row in rows),
        "base_cost": sum(row["base_cost"] for row in rows),
        "final_cost": sum(row["final_cost"] for row in rows),
        "unknown_model_requests": sum(1 for row in rows if row["unknown_model"]),
        "supplier_match": rows[0]["supplier_match"] if rows else "unknown",
    }
    return summary


def daily_rows(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    grouped: dict[tuple[str, str], dict[str, Any]] = {}
    for row in rows:
        key = (row["date"], row["model"])
        item = grouped.setdefault(
            key,
            {
                "date": row["date"],
                "model": row["model"],
                "requests": 0,
                "sessions": set(),
                "input_tokens": 0,
                "cached_input_tokens": 0,
                "uncached_input_tokens": 0,
                "output_tokens": 0,
                "total_tokens": 0,
                "base_cost": 0.0,
                "final_cost": 0.0,
            },
        )
        item["requests"] += 1
        item["sessions"].add(row["session_id"])
        for key_name in (
            "input_tokens",
            "cached_input_tokens",
            "uncached_input_tokens",
            "output_tokens",
            "total_tokens",
        ):
            item[key_name] += row[key_name]
        item["base_cost"] += row["base_cost"]
        item["final_cost"] += row["final_cost"]

    result: list[dict[str, Any]] = []
    for item in grouped.values():
        item = dict(item)
        item["sessions"] = len(item["sessions"])
        result.append(item)
    return sorted(result, key=lambda x: (x["date"], x["model"]))


def write_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    if not rows:
        path.write_text("", encoding="utf-8")
        return
    with path.open("w", newline="", encoding="utf-8") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)


def run_report(
    codex_home: Path,
    price_file: Path,
    supplier_url: str,
    multiplier_from: str,
    multiplier: float,
    timezone_name: str,
    since: str | None,
    until: str | None,
    include_unknown_supplier: bool,
) -> Report:
    tz = ZoneInfo(timezone_name)
    prices = load_prices(price_file)
    supplier_match, supplier_basis = detect_supplier(codex_home, supplier_url)
    if supplier_match == "unknown" and not include_unknown_supplier:
        return Report(
            rows=[],
            summary={
                "requests": 0,
                "sessions": 0,
                "input_tokens": 0,
                "cached_input_tokens": 0,
                "uncached_input_tokens": 0,
                "output_tokens": 0,
                "reasoning_output_tokens": 0,
                "total_tokens": 0,
                "base_cost": 0.0,
                "final_cost": 0.0,
                "unknown_model_requests": 0,
                "supplier_match": "unknown",
                "supplier_basis": supplier_basis,
            },
        )

    rows: list[dict[str, Any]] = []
    cutoff = local_day_start(multiplier_from, tz)
    if cutoff is None:
        raise ValueError("--multiplier-from is required")
    since_dt = local_day_start(since, tz)
    until_dt = local_day_start(until, tz)
    for path in session_files(codex_home):
        rows.extend(
            parse_session_file(
                path,
                prices,
                supplier_match,
                supplier_basis,
                cutoff,
                multiplier,
                tz,
                since_dt,
                until_dt,
            )
        )
    rows.sort(key=lambda row: (row["timestamp"], row["session_id"], row["request_index"]))
    summary = summarize(rows)
    summary["supplier_basis"] = supplier_basis
    return Report(rows=rows, summary=summary)


def main() -> int:
    args = build_parser().parse_args()
    report = run_report(
        codex_home=Path(args.codex_home).expanduser(),
        price_file=Path(args.price_file).expanduser(),
        supplier_url=args.supplier_url,
        multiplier_from=args.multiplier_from,
        multiplier=args.multiplier,
        timezone_name=args.timezone,
        since=args.since,
        until=args.until,
        include_unknown_supplier=args.include_unknown_supplier,
    )
    if args.out:
        write_csv(Path(args.out), report.rows)
    if args.daily_out:
        write_csv(Path(args.daily_out), daily_rows(report.rows))

    if args.total_only:
        print(f"{report.summary['final_cost']:.2f}")
        return 0

    print(json.dumps(report.summary, ensure_ascii=False, indent=2))
    if report.summary.get("supplier_match") == "unknown":
        print(
            "Supplier URL was not found in Codex/ccswitch config. "
            "Use --include-unknown-supplier to force token aggregation.",
            file=sys.stderr,
        )
    if report.summary.get("unknown_model_requests"):
        print(
            f"Warning: {report.summary['unknown_model_requests']} requests used models "
            "missing from the price file; their cost is 0.",
            file=sys.stderr,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
