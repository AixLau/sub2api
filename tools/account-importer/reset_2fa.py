#!/usr/bin/env python3
"""并发重置 ChatGPT 账号的 TOTP 2FA。

输入文件每行：email----password----totp_secret
输出文件每行：email----password----new_totp_secret

流程：登录 -> mfa_info -> disable_in_house 移除旧 TOTP ->
enroll_totp 生成并激活新 TOTP -> 复核 mfa_info。
"""

from __future__ import annotations

import argparse
import asyncio
import logging
import os
import sys
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

IMPORTER = Path(__file__).resolve().parent
if str(IMPORTER) not in sys.path:
    sys.path.insert(0, str(IMPORTER))

from local_relogin import _load_upstream, parse_account_line


@dataclass
class Account:
    index: int
    email: str
    password: str
    secret: str
    line: str


@dataclass
class Upstream:
    AuthFlow: Any
    Config: Any
    MailboxUnavailableProvider: Any
    MFA_INFO_URL: str
    DISABLE_URL: str
    mfa_headers: Any
    enroll_totp: Any
    totp_now: Any


UPSTREAM: Upstream | None = None


def load_upstream(source_dir: Path) -> Upstream:
    AuthFlow, Config, MailboxUnavailableProvider, _make_cpa, _make_sub2api = _load_upstream(source_dir)
    from platforms.chatgpt.protocol.two_factor import MFA_INFO_URL, _mfa_headers, enroll_totp
    from platforms.chatgpt.protocol.totp import totp_now

    return Upstream(
        AuthFlow=AuthFlow,
        Config=Config,
        MailboxUnavailableProvider=MailboxUnavailableProvider,
        MFA_INFO_URL=MFA_INFO_URL,
        DISABLE_URL="https://chatgpt.com/backend-api/accounts/mfa/user/disable_in_house",
        mfa_headers=_mfa_headers,
        enroll_totp=enroll_totp,
        totp_now=totp_now,
    )


def masked_email(email: str) -> str:
    local, separator, domain = email.partition("@")
    if not separator:
        return "[REDACTED]"
    return f"{local[:2]}***@{domain}"


def read_accounts(path: Path) -> list[Account]:
    raw_lines = path.read_text(encoding="utf-8-sig").splitlines()
    accounts: list[Account] = []
    seen: set[str] = set()
    for number, raw in enumerate(raw_lines, 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if "----" not in line and "====" in line:
            line = "----".join(line.split("====", 2))
        try:
            email, password, secret = parse_account_line(line)
        except ValueError as exc:
            raise SystemExit(f"第 {number} 行无效：{exc}") from None
        key = email.casefold()
        if key in seen:
            raise SystemExit(f"第 {number} 行邮箱重复：{email}")
        seen.add(key)
        accounts.append(Account(len(accounts) + 1, email, password, secret, line))
    if not accounts:
        raise SystemExit("输入文件没有有效账号")
    return accounts


def login(account: Account, proxy: str | None, retries: int):
    assert UPSTREAM is not None
    last_error: Exception | None = None
    for attempt in range(max(1, retries + 1)):
        flow = None
        try:
            flow = UPSTREAM.AuthFlow(
                UPSTREAM.Config(proxy=proxy),
                env_overrides={
                    "WEBUI_ALLOW_LOGIN": "1",
                    "OAUTH_REFRESH_ONLY": "1",
                    "OAUTH_CODEX_RT_EXCHANGE": "1",
                    "OAUTH_CODEX_RT_BEFORE_CALLBACK": "1",
                    "OAUTH_CODEX_AFTER_CALLBACK": "1",
                    "OAUTH_CODEX_PROMPT": "",
                    "AUTH_TRACE_DUMP": "0",
                    "AUTH_TRACE_INCLUDE_COOKIE": "0",
                    "AUTH_HTTP_TRACE": "0",
                },
                account_callback=lambda _email, password=account.password, secret=account.secret: {
                    "password": password,
                    "totp_secret": secret,
                },
            )
            flow.result.totp_secret = account.secret
            provider = UPSTREAM.MailboxUnavailableProvider(account.email, "未配置邮箱收件接口")
            try:
                result = flow.run_protocol_login(provider, account.email, account.password)
                access_token = str(result.access_token or "")
            except Exception as exc:
                if "协议登录完成，但未拿到" not in str(exc):
                    raise
                access_token = str(flow.result.access_token or "")

            if not access_token:
                response = flow.session.get(
                    "https://chatgpt.com/api/auth/session",
                    headers={"Accept": "application/json"},
                    timeout=30,
                )
                if response.status_code == 200:
                    access_token = str((response.json() or {}).get("accessToken") or "")
            if not access_token:
                raise RuntimeError("登录完成但没有拿到 access token")
            return flow, access_token
        except Exception as exc:
            last_error = exc
            if attempt < retries:
                time.sleep(2)
                continue
            raise RuntimeError(f"登录失败：{type(exc).__name__}: {exc}") from None
    raise RuntimeError(f"登录失败：{last_error}")


def mfa_info(flow, access_token: str) -> dict:
    assert UPSTREAM is not None
    response = flow.session.get(
        UPSTREAM.MFA_INFO_URL,
        headers=UPSTREAM.mfa_headers(flow, UPSTREAM.MFA_INFO_URL, access_token),
        timeout=30,
    )
    if response.status_code != 200:
        raise RuntimeError(f"mfa_info HTTP {response.status_code}: {response.text[:160]}")
    try:
        data = response.json() or {}
    except Exception:
        raise RuntimeError("mfa_info 响应不是 JSON") from None
    if not isinstance(data, dict):
        raise RuntimeError("mfa_info 响应结构无效")
    return data


def totp_factors(info: dict) -> list[dict]:
    factors = info.get("factors")
    if not isinstance(factors, dict):
        return []
    items = factors.get("totp")
    if not isinstance(items, list):
        return []
    return [item for item in items if isinstance(item, dict) and item.get("id")]


def disable_factor(flow, access_token: str, factor_id: str) -> None:
    assert UPSTREAM is not None
    response = flow.session.post(
        UPSTREAM.DISABLE_URL,
        headers=UPSTREAM.mfa_headers(flow, UPSTREAM.DISABLE_URL, access_token, json_body=True),
        json={"factor_id": factor_id},
        timeout=30,
    )
    if response.status_code != 200:
        raise RuntimeError(f"disable_in_house HTTP {response.status_code}: {response.text[:200]}")


def enroll_with_retry(flow, access_token: str):
    assert UPSTREAM is not None
    result = None
    for attempt in range(3):
        result = UPSTREAM.enroll_totp(flow, access_token)
        if result.ok and result.secret:
            return result
        if result.already_bound:
            return result
        message = (result.error_message or "").lower()
        if "429" in message and attempt < 2:
            time.sleep(30)
            continue
        return result
    return result


def reset_one(account: Account, proxy: str | None, retries: int) -> str:
    assert UPSTREAM is not None
    flow, access_token = login(account, proxy, retries)
    info = mfa_info(flow, access_token)
    factors = totp_factors(info)

    disabled_any = False
    if factors:
        for factor in factors:
            factor_id = str(factor.get("id") or "")
            if not factor_id:
                continue
            try:
                disable_factor(flow, access_token, factor_id)
            except Exception as exc:
                if "401" in str(exc) or "recent_auth" in str(exc).lower():
                    flow, access_token = login(account, proxy, retries)
                    disable_factor(flow, access_token, factor_id)
                else:
                    raise
            disabled_any = True

        info = mfa_info(flow, access_token)
        remaining = totp_factors(info)
        if remaining:
            for factor in remaining:
                factor_id = str(factor.get("id") or "")
                if factor_id:
                    disable_factor(flow, access_token, factor_id)
                    disabled_any = True
            info = mfa_info(flow, access_token)
            if totp_factors(info):
                raise RuntimeError("旧 TOTP factor 未能全部移除")

    result = enroll_with_retry(flow, access_token)
    if not result.ok or not result.secret:
        message = result.error_message or "enroll 未返回新密钥"
        if disabled_any:
            raise RuntimeError(
                "旧 2FA 已禁用，但新 TOTP 未生成；请立即用密码登录并重新绑定。"
                f" 原因：{message}"
            )
        raise RuntimeError(message)

    new_secret = str(result.secret).strip()
    check = mfa_info(flow, access_token)
    if not check.get("mfa_enabled") or not totp_factors(check):
        raise RuntimeError("新 TOTP 已生成，但 mfa_info 未确认启用")
    return new_secret


def check_one(account: Account, proxy: str | None, retries: int) -> str:
    flow, access_token = login(account, proxy, retries)
    info = mfa_info(flow, access_token)
    factors = totp_factors(info)
    return (
        f"mfa_enabled={bool(info.get('mfa_enabled'))} "
        f"totp_factors={len(factors)} "
        f"default={str(info.get('native_default_factor_id') or '')[:8]}..."
    )


class OutputWriter:
    def __init__(self, output: Path, failed: Path, force: bool):
        if not force:
            for path in (output, failed):
                if path.exists():
                    raise SystemExit(f"文件已存在：{path}；换一个路径或加 --force")
        output.parent.mkdir(parents=True, exist_ok=True)
        failed.parent.mkdir(parents=True, exist_ok=True)
        self.output = output
        self.failed = failed
        self._out = os.fdopen(
            os.open(output, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600),
            "w",
            encoding="utf-8",
        )
        self._fail = os.fdopen(
            os.open(failed, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600),
            "w",
            encoding="utf-8",
        )
        self._lock = asyncio.Lock()

    async def success(self, line: str) -> None:
        async with self._lock:
            self._out.write(line.rstrip("\n") + "\n")
            self._out.flush()

    async def failure(self, account: Account, reason: str) -> None:
        clean = " ".join(str(reason).split())[:500]
        async with self._lock:
            self._fail.write(f"{account.email}----{clean}\n")
            self._fail.flush()

    def close(self) -> None:
        self._out.close()
        self._fail.close()
        try:
            self.output.chmod(0o600)
            self.failed.chmod(0o600)
        except OSError:
            pass


async def run(args: argparse.Namespace) -> int:
    global UPSTREAM
    source_dir = Path(args.source_dir).expanduser().resolve()
    UPSTREAM = load_upstream(source_dir)
    accounts = read_accounts(Path(args.input).expanduser())

    if args.check:
        semaphore = asyncio.Semaphore(max(1, args.workers))
        counter = 0
        lock = asyncio.Lock()

        async def check(account: Account) -> bool:
            nonlocal counter
            async with semaphore:
                try:
                    detail = await asyncio.to_thread(check_one, account, args.proxy, args.retries)
                    async with lock:
                        counter += 1
                        print(f"[{counter}/{len(accounts)}] {masked_email(account.email)} OK {detail}", flush=True)
                    return True
                except Exception as exc:
                    async with lock:
                        counter += 1
                        print(f"[{counter}/{len(accounts)}] {masked_email(account.email)} FAIL {str(exc)[:220]}", flush=True)
                    return False

        results = await asyncio.gather(*(check(account) for account in accounts))
        return 0 if all(results) else 1

    output = Path(args.output).expanduser()
    failed = Path(args.failed).expanduser()
    writer = OutputWriter(output, failed, args.force)
    semaphore = asyncio.Semaphore(max(1, args.workers))
    counter = 0
    lock = asyncio.Lock()
    ok_count = 0
    fail_count = 0

    async def worker(account: Account) -> None:
        nonlocal counter, ok_count, fail_count
        async with semaphore:
            try:
                new_secret = await asyncio.to_thread(reset_one, account, args.proxy, args.retries)
                line = f"{account.email}----{account.password}----{new_secret}"
                await writer.success(line)
                async with lock:
                    counter += 1
                    ok_count += 1
                    print(f"[{counter}/{len(accounts)}] {masked_email(account.email)} OK", flush=True)
            except Exception as exc:
                reason = str(exc)
                await writer.failure(account, reason)
                async with lock:
                    counter += 1
                    fail_count += 1
                    print(f"[{counter}/{len(accounts)}] {masked_email(account.email)} FAIL {reason[:220]}", flush=True)

    try:
        await asyncio.gather(*(worker(account) for account in accounts))
    finally:
        writer.close()

    print(f"DONE ok={ok_count} failed={fail_count}", flush=True)
    print(f"OUTPUT {output}", flush=True)
    print(f"FAILED {failed}", flush=True)
    return 0 if fail_count == 0 else 1


def main() -> None:
    parser = argparse.ArgumentParser(description="并发重置 ChatGPT TOTP 2FA")
    parser.add_argument("--input", required=True, help="email----password----totp_secret 文件")
    parser.add_argument("--output", help="成功结果 txt，默认 2fa_reset_<时间>.txt")
    parser.add_argument("--failed", help="失败原因 txt，默认 <output>.failed.txt")
    parser.add_argument("--workers", type=int, default=3, help="并发数，默认 3")
    parser.add_argument("--retries", type=int, default=2, help="单账号登录重试次数")
    parser.add_argument("--proxy", help="所有账号共用的 HTTP 代理")
    parser.add_argument(
        "--source-dir",
        default=os.getenv("ACCOUNT_IMPORT_AUTH_SOURCE", "/opt/any-auto-register"),
        help="any-auto-register 源码目录",
    )
    parser.add_argument("--check", action="store_true", help="只读检查 MFA，不重置")
    parser.add_argument("--force", action="store_true", help="覆盖已存在的输出文件")
    args = parser.parse_args()

    if args.workers < 1:
        parser.error("--workers 必须 >= 1")
    if args.workers > 10:
        parser.error("--workers 建议不超过 10，避免触发风控")
    if args.retries < 0:
        parser.error("--retries 不能为负")

    stamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    if not args.output:
        args.output = f"2fa_reset_{stamp}.txt"
    if not args.failed:
        args.failed = args.output + ".failed.txt"

    logging.getLogger("platforms.chatgpt").setLevel(logging.WARNING)
    raise SystemExit(asyncio.run(run(args)))


if __name__ == "__main__":
    main()
