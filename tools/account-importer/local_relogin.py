"""Use a local any-auto-register source checkout for OAuth relogin and export.

The imported upstream protocol handles password, TOTP, and Codex OAuth token
exchange. Its network precheck also contacts Cloudflare's trace endpoint.
"""

from __future__ import annotations

import base64
import binascii
import json
import logging
import os
import re
import sys
from pathlib import Path
from types import SimpleNamespace

DEFAULT_SOURCE_DIR = Path(os.getenv("ACCOUNT_IMPORT_AUTH_SOURCE", "/opt/any-auto-register"))
EXPORT_FORMATS = ("sub2api", "cpa", "native")


def parse_account_line(line: str) -> tuple[str, str, str]:
    parts = line.strip().split("----", 2)
    if len(parts) != 3:
        raise ValueError("账号资料必须是：邮箱----密码----2FA 密钥")
    email, password, secret = (part.strip() for part in parts)
    if email.count("@") != 1 or "." not in email.split("@", 1)[1] or " " in email:
        raise ValueError("账号资料中的邮箱格式无效")
    if not password or not secret:
        raise ValueError("账号密码和 2FA 密钥都不能为空")
    normalized = "".join(secret.split()).upper()
    try:
        base64.b32decode(normalized + "=" * (-len(normalized) % 8), casefold=True)
    except (binascii.Error, ValueError) as exc:
        raise ValueError("2FA 密钥不是有效的 Base32 格式") from exc
    return email, password, secret


def _load_upstream(source_dir: Path):
    source_dir = source_dir.expanduser().resolve()
    if not (source_dir / "platforms/chatgpt/protocol/auth_flow.py").is_file():
        raise RuntimeError(f"未找到 any-auto-register 源码：{source_dir}")
    if str(source_dir) not in sys.path:
        sys.path.insert(0, str(source_dir))
    try:
        from platforms.chatgpt.protocol import AuthFlow, Config
        from platforms.chatgpt.rt_backfill import MailboxUnavailableProvider
        from platforms.chatgpt.cpa_upload import generate_token_json
        from platforms.chatgpt.sub2api_upload import _build_sub2api_account_payload
    except ImportError as exc:
        raise RuntimeError("本地协议依赖缺失；请安装 requirements.txt") from exc
    return AuthFlow, Config, MailboxUnavailableProvider, generate_token_json, _build_sub2api_account_payload


def _filename(email: str, export_format: str) -> str:
    account = re.sub(r"[^A-Za-z0-9._-]+", "_", email)[:96] or "account"
    return f"{account}-{export_format}.json"


class _FailureCapture(logging.Handler):
    def __init__(self):
        super().__init__(level=logging.WARNING)
        self.account_disabled = False

    def emit(self, record):
        if "deleted or deactivated" in record.getMessage().lower():
            self.account_disabled = True


def _secure_write(destination: Path, payload: dict) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, ensure_ascii=False, indent=2)
            handle.write(chr(10))
    except BaseException:
        destination.unlink(missing_ok=True)
        raise


def relogin_payload(
    account_line: str,
    export_format: str = "sub2api",
    source_dir: Path = DEFAULT_SOURCE_DIR,
    proxy: str | None = None,
    group_ids: list[int] | None = None,
) -> dict:
    """Return fresh OAuth credentials in memory without writing a token file."""
    email, password, secret = parse_account_line(account_line)
    if export_format not in EXPORT_FORMATS:
        raise ValueError(f"不支持的导出格式：{export_format}")
    AuthFlow, Config, MailboxUnavailableProvider, make_cpa, make_sub2api = _load_upstream(source_dir)

    flow = AuthFlow(
        Config(proxy=proxy),
        env_overrides={
            "WEBUI_ALLOW_LOGIN": "1",
            "OAUTH_REFRESH_ONLY": "1",
            "OAUTH_CODEX_RT_EXCHANGE": "1",
            "OAUTH_CODEX_RT_BEFORE_CALLBACK": "1",
            "AUTH_TRACE_DUMP": "0",
            "AUTH_TRACE_INCLUDE_COOKIE": "0",
            "AUTH_HTTP_TRACE": "0",
        },
        account_callback=lambda _email: {"password": password, "totp_secret": secret},
    )
    flow.result.totp_secret = secret
    provider = MailboxUnavailableProvider(email, "未配置邮箱收件接口")
    logger = logging.getLogger("platforms.chatgpt")
    capture = _FailureCapture()
    old_propagate = logger.propagate
    logger.addHandler(capture)
    logger.propagate = False
    try:
        try:
            result = flow.run_protocol_login(provider, email, password)
        except Exception as exc:
            if capture.account_disabled:
                raise RuntimeError("OpenAI 在 2FA 阶段返回 403，提示账号已删除或停用") from None
            reason = str(exc).replace(password, "[REDACTED]").replace(secret, "[REDACTED]")
            raise RuntimeError(f"OpenAI 重登失败：{reason[:280]}") from None
    finally:
        logger.removeHandler(capture)
        logger.propagate = old_propagate
    if not result.access_token or not result.refresh_token:
        raise RuntimeError("未同时取得 access_token 和 refresh_token，不生成无效文件")

    account = SimpleNamespace(
        email=email,
        access_token=result.access_token,
        refresh_token=result.refresh_token,
        id_token=result.id_token,
    )
    if export_format == "sub2api":
        payload = {"proxies": [], "accounts": [make_sub2api(account, group_ids=group_ids or [])]}
    elif export_format == "cpa":
        payload = make_cpa(account)
    else:
        payload = {
            "email": email,
            "access_token": result.access_token,
            "refresh_token": result.refresh_token,
            "id_token": result.id_token,
        }
    return payload


def relogin_and_download(
    account_line: str,
    output_dir: Path,
    export_format: str = "sub2api",
    source_dir: Path = DEFAULT_SOURCE_DIR,
    proxy: str | None = None,
) -> Path:
    """Relogin via local source and export one OAuth account."""
    email, _, _ = parse_account_line(account_line)
    payload = relogin_payload(account_line, export_format, source_dir, proxy)
    destination = output_dir / _filename(email, export_format)
    _secure_write(destination, payload)
    return destination
