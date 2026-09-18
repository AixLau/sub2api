#!/usr/bin/env python3
"""Compare recorded Go test runs without treating matching failures as passes.

This reads evidence; it never runs tests. JSON events are keyed by package and
full subtest name. A RUN without PASS/FAIL/SKIP remains INCOMPLETE, including
parallel tests interrupted by another test's panic. Unobserved tests are NOT_RUN,
not PASS. Root tests, subtests, and package failures have separate counts.
"""

import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re


TERMINAL = {"pass", "fail", "skip"}
PROBLEM = {"FAIL", "INCOMPLETE"}


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def read_run(path):
    records = {}
    invalid = []
    events = 0
    with Path(path).open(encoding="utf-8") as stream:
        for number, line in enumerate(stream, 1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                invalid.append(number)
                continue
            if not isinstance(event, dict) or "Action" not in event or "Package" not in event:
                invalid.append(number)
                continue
            events += 1
            key = (event["Package"], event.get("Test", "<package>"))
            record = records.setdefault(key, {
                "status": "INCOMPLETE", "run_events": 0, "terminal_events": [],
                "first_event_line": number, "last_event_line": number,
                "output": [], "event_times": {},
            })
            record["last_event_line"] = number
            action = event["Action"]
            if action == "run":
                record["run_events"] += 1
                record["status"] = "INCOMPLETE"
            if action in TERMINAL:
                record["status"] = action.upper()
                record["terminal_events"].append(action.upper())
            if action in {"start", "run", "pass", "fail", "skip"}:
                record["event_times"][action] = event.get("Time")
            if action == "output":
                record["output"].append(event.get("Output", ""))
    if not events:
        raise ValueError(f"No Go test JSON events in {path}; do not infer test results from an empty log")
    # A repeated -count run may pass after failing; preserve the failed run.
    for record in records.values():
        if "FAIL" in record["terminal_events"]:
            record["status"] = "FAIL"
        text = "".join(record.pop("output"))
        record["output_sha256"] = hashlib.sha256(text.encode()).hexdigest()
        record["assertion_locations"] = sorted(set(re.findall(r"[\w.-]+_test\.go:\d+", text)))
        record["panicked"] = "panic:" in text
        # Only assertion and stack evidence; do not copy arbitrary runtime logs
        # (which may contain credentials) into a tracked comparison report.
        selected = []
        for line in text.splitlines():
            if any(marker in line for marker in (
                "Error:", "Messages:", "expected:", "actual  :", "panic:",
                "Error Trace:", "_test.go:", "[build failed]", "undefined:",
            )):
                selected.append(line.strip()[:600])
        record["assertion_excerpt"] = selected[:30]
    return records, {"path": str(path), "sha256": sha256(path), "json_events": events,
                     "non_json_lines": invalid}


def level(test):
    if test == "<package>":
        return "package"
    return "subtest" if "/" in test else "root_test"


def counts(records):
    return {kind: dict(Counter(record["status"] for (_, test), record in records.items()
                               if level(test) == kind))
            for kind in ("package", "root_test", "subtest")}


def compare(baseline, candidate):
    rows = []
    for key in sorted(baseline.keys() | candidate.keys()):
        old = baseline.get(key, {"status": "NOT_RUN"})
        new = candidate.get(key, {"status": "NOT_RUN"})
        if not ({old["status"], new["status"]} & PROBLEM):
            continue
        comparison = f"{old['status']}_TO_{new['status']}"
        rows.append({"package": key[0], "test": key[1], "level": level(key[1]),
                     "comparison": comparison, "baseline": old, "candidate": new,
                     "attribution": "UNATTRIBUTED",
                     "acceptance": "NOT_PASS"})
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for revision in ("baseline", "candidate"):
        parser.add_argument(f"--{revision}-json", required=True, type=Path)
        parser.add_argument(f"--{revision}-sha", required=True)
        parser.add_argument(f"--{revision}-exit", required=True, type=int)
        parser.add_argument(f"--{revision}-command", required=True)
        parser.add_argument(f"--{revision}-stderr", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    runs = {}
    metadata = {}
    for revision in ("baseline", "candidate"):
        records, meta = read_run(getattr(args, f"{revision}_json"))
        runs[revision] = records
        meta.update({"tested_code_sha": getattr(args, f"{revision}_sha"),
                     "exit_code": getattr(args, f"{revision}_exit"),
                     "command": getattr(args, f"{revision}_command"),
                     "counts": counts(records)})
        stderr = getattr(args, f"{revision}_stderr")
        if stderr:
            meta["stderr"] = {"path": str(stderr), "sha256": sha256(stderr)}
        metadata[revision] = meta
    rows = compare(runs["baseline"], runs["candidate"])
    result = {
        "schema_version": 1,
        "method": "Recorded events only; matching failures remain NOT_PASS. Attribution requires separate review.",
        "coverage_limit": "Tests absent from both logs cannot be discovered here. Panic may also prevent unstarted tests from being observed.",
        "runs": metadata,
        "comparison_counts": dict(Counter(row["comparison"] for row in rows)),
        "problem_union": rows,
    }
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(args.output), "problem_union": len(rows),
                      "counts": result["comparison_counts"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
