"""Register new ChatGPT accounts with the pinned any-auto-register engine.

The importer already uses ``any-auto-register`` for password/TOTP relogin.
The same checkout also contains a complete email-registration engine.  This
module wraps it for the importer:

* input is ``email----mail API URL`` (the generic mailbox format);
* a strong password is generated locally;
* 2FA is bound during registration;
* the result is a standard Sub2API bundle plus an encrypted-ready
  ``email----password----TOTP`` credential line.

Secrets never appear in logs or error messages.  Upstream protocol logs are
silenced while the engine runs because they may contain the password, OTP or
TOTP secret.
"""

from __future__ import annotations

import base64
import binascii
import logging
import os
import re
import secrets
import string
import sys
import time
from pathlib import Path
from types import SimpleNamespace

from local_relogin import DEFAULT_SOURCE_DIR, TIMING_LOGGER, _masked_email
from mail_api_mailbox import MailAPIMailbox, validate_mail_api_url

PASSWORD_ALPHABET = string.ascii_letters + string.digits + "!@#$"
MAX_LINE_LENGTH = 8192
DEFAULT_OTP_TIMEOUT = 180


class RegistrationIncomplete(RuntimeError):
    """The account exists but a full Sub2API bundle could not be produced.

    ``credential_line`` keeps the generated password and TOTP secret so the
    operator can log in again and import the account instead of losing it.
    """

    def __init__(
        self,
        message: str,
        *,
        credential_line: str | None = None,
        two_factor_bound: bool = False,
    ) -> None:
        super().__init__(message)
        self.credential_line = credential_line
        self.two_factor_bound = bool(two_factor_bound)


def _valid_email(email: str) -> bool:
    if not email or len(email) > 254 or email.count("@") != 1 or any(char.isspace() for char in email):
        return False
    local, _, domain = email.partition("@")
    return bool(local and domain and "." in domain)


def parse_registration_line(line: str) -> tuple[str, str]:
    """Parse ``email----mail API URL`` (also accepts ``====``)."""
    value = (line or "").strip()
    if not value or len(value) > MAX_LINE_LENGTH:
        raise ValueError("注册资料长度无效")
    if "\n" in value or "\r" in value:
        raise ValueError("每行只能填写一个注册邮箱")
    separator = "----" if "----" in value else "====" if "====" in value else ""
    if not separator:
        raise ValueError("注册资料必须是：邮箱----收信 API 地址")
    email, api_url = (part.strip() for part in value.split(separator, 1))
    if not _valid_email(email):
        raise ValueError("注册资料中的邮箱格式无效")
    return email, validate_mail_api_url(api_url)


def _generate_password(length: int = 16) -> str:
    return "".join(secrets.choice(PASSWORD_ALPHABET) for _ in range(length))


def _load_upstream(source_dir: Path):
    source_dir = source_dir.expanduser().resolve()
    if not (source_dir / "platforms/chatgpt/registration_engine.py").is_file():
        raise RuntimeError(f"未找到 any-auto-register 注册源码：{source_dir}")
    if str(source_dir) not in sys.path:
        sys.path.insert(0, str(source_dir))
    try:
        from platforms.chatgpt.registration_engine import (
            ChatGPTRegistrationEngine,
            REGISTER_FLOW_EMAIL,
            REGISTRATION_MODE_REFRESH_TOKEN,
        )
        from platforms.chatgpt.sub2api_upload import _build_sub2api_account_payload
    except ImportError as exc:
        raise RuntimeError("本地协议依赖缺失；请安装 requirements.txt") from exc

    class EmailOnlyRegistrationEngine(ChatGPTRegistrationEngine):
        """Email registration without the upstream SQLModel/SMS config store.

        The importer deliberately has no SMS provider.  The upstream engine
        calls ``_build_sms_callback`` while building the flow, which imports
        ``core.config_store`` and therefore ``sqlmodel``.  That module also
        expects a writable SQLite path, which the read-only importer container
        does not provide.  Returning ``None`` keeps the email path working and
        leaves add-phone failures as a clear upstream error.
        """

        def _build_sms_callback(self):
            return None

        def _env_overrides(self):
            overrides = super()._env_overrides()
            overrides.update({
                "AUTH_TRACE_DUMP": "0",
                "AUTH_TRACE_INCLUDE_COOKIE": "0",
                "AUTH_HTTP_TRACE": "0",
            })
            return overrides

    return (
        EmailOnlyRegistrationEngine,
        REGISTER_FLOW_EMAIL,
        REGISTRATION_MODE_REFRESH_TOKEN,
        _build_sub2api_account_payload,
    )


def _sanitize(
    value: object,
    *,
    email: str = "",
    password: str = "",
    secret: str = "",
    api_url: str = "",
) -> str:
    text = str(value or "")
    for item in (api_url, password, secret):
        if item:
            text = text.replace(item, "[REDACTED]")
    if email:
        text = text.replace(email, _masked_email(email))
    text = re.sub(r"https?://\S+", "[URL]", text)
    text = re.sub(r"\beyJ[A-Za-z0-9._-]{16,}\b", "[TOKEN]", text)
    text = re.sub(r"\b[A-Za-z0-9_-]{40,}\b", "[TOKEN]", text)
    text = re.sub(r"(?i)(totp\s*(?:密钥|secret)?\s*[:：]\s*)[A-Z2-7]{16,}", r"\1[REDACTED]", text)
    text = re.sub(r"\b[A-Z2-7]{20,}\b", "[REDACTED]", text)
    text = re.sub(r"(?<!\d)\d{6}(?!\d)", "[CODE]", text)
    return text[:400]


def _totp_secret(engine, result) -> str:
    metadata = getattr(result, "metadata", None)
    value = metadata.get("totp_secret") if isinstance(metadata, dict) else ""
    if not value:
        flow = getattr(engine, "flow", None)
        value = getattr(getattr(flow, "result", None), "totp_secret", "")
    if not value:
        value = getattr(getattr(engine, "two_factor", None), "secret", "")
    secret = "".join(str(value or "").split()).upper()
    if not secret:
        return ""
    try:
        base64.b32decode(secret + "=" * (-len(secret) % 8), casefold=True)
    except (binascii.Error, ValueError):
        return ""
    return secret


def register_payload(
    line: str,
    export_format: str = "sub2api",
    source_dir: Path = DEFAULT_SOURCE_DIR,
    proxy: str | None = None,
    group_ids: list[int] | None = None,
    otp_timeout: int = DEFAULT_OTP_TIMEOUT,
    mailbox_factory=MailAPIMailbox,
) -> tuple[dict, str | None]:
    """Register one account and return ``(sub2api bundle, credential line)``.

    ``credential_line`` is ``None`` when the flow produced no usable password
    or no valid TOTP secret; the account is still imported and monitored.
    """
    email, api_url = parse_registration_line(line)
    if export_format != "sub2api":
        raise ValueError("注册仅支持生成 Sub2API 导入数据")
    Engine, flow_email, mode_rt, make_sub2api = _load_upstream(source_dir)
    password = _generate_password()
    timeout = min(max(int(otp_timeout or DEFAULT_OTP_TIMEOUT), 30), 600)
    mailbox_proxy = os.getenv("ACCOUNT_IMPORT_MAIL_API_PROXY", "").strip() or None
    try:
        mailbox = mailbox_factory(email, api_url, proxy=mailbox_proxy)
    except TypeError:
        mailbox = mailbox_factory(email, api_url)

    def log(message: object) -> None:
        text = str(message or "")
        # AuthFlow generates the real password internally and the engine logs
        # it once.  We cannot know that value before the callback, so drop the
        # whole line instead of risking a credential leak in the timing log.
        if "密码已在 OpenAI 侧生效" in text:
            text = "密码已在 OpenAI 侧生效（已隐藏）"
        TIMING_LOGGER.info(
            "register_step email=%s message=%s",
            _masked_email(email),
            _sanitize(text, email=email, password=password, api_url=api_url),
        )

    engine = Engine(
        mailbox=mailbox,
        mode=mode_rt,
        proxy=proxy,
        email=email,
        password=password,
        extra_config={"mailbox_otp_timeout_seconds": timeout},
        log_fn=log,
        mailbox_kind="generic_api",
        register_flow=flow_email,
        bind_2fa=True,
    )

    # Upstream protocol loggers may write the password, OTP or TOTP secret.
    # Drop their records unless explicit sanitized debugging is requested.
    protocol_logger = logging.getLogger("platforms.chatgpt")
    old_propagate = protocol_logger.propagate
    old_handlers = protocol_logger.handlers[:]
    old_level = protocol_logger.level
    debug_logs = os.getenv("ACCOUNT_IMPORT_REGISTRATION_DEBUG") == "1"
    if debug_logs:
        class _SanitizedProtocolHandler(logging.Handler):
            def emit(self, record):
                try:
                    message = _sanitize(
                        record.getMessage(),
                        email=email,
                        password=password,
                        api_url=api_url,
                    )
                    if "密码" in message and "/" in message:
                        message = "密码相关日志已隐藏"
                    TIMING_LOGGER.info(
                        "register_upstream level=%s message=%s",
                        record.levelname,
                        message,
                    )
                except Exception:
                    pass

        protocol_logger.handlers = [_SanitizedProtocolHandler()]
        protocol_logger.setLevel(logging.INFO)
    else:
        protocol_logger.handlers = [logging.NullHandler()]
    protocol_logger.propagate = False
    started = time.perf_counter()
    TIMING_LOGGER.info(
        "register_start email=%s proxy_configured=%s",
        _masked_email(email),
        bool(proxy),
    )
    result = None
    try:
        result = engine.run()
    except Exception as exc:
        actual_password = str(getattr(engine, "password", "") or password)
        secret = _totp_secret(engine, None)
        credential_line = (
            f"{email}----{actual_password}----{secret}"
            if actual_password and secret and "----" not in actual_password
            else None
        )
        message = _sanitize(
            f"OpenAI 注册失败：{exc}",
            email=email,
            password=actual_password,
            api_url=api_url,
        )
        if credential_line:
            TIMING_LOGGER.info(
                "register_partial email=%s two_factor_bound=True",
                _masked_email(email),
            )
            raise RegistrationIncomplete(
                f"{message}；账号可能已创建并绑定 2FA，已保存账号资料，"
                "可用“用已保存资料导入”重新登录",
                credential_line=credential_line,
                two_factor_bound=True,
            ) from None
        raise RuntimeError(message) from None
    finally:
        protocol_logger.handlers = old_handlers
        protocol_logger.propagate = old_propagate
        protocol_logger.setLevel(old_level)
        close = getattr(mailbox, "close", None)
        if callable(close):
            close()

    if result is None:
        raise RuntimeError("OpenAI 注册失败：未返回注册结果")

    actual_email = str(getattr(result, "email", "") or email).strip()
    if actual_email and actual_email.casefold() != email.casefold():
        raise RuntimeError("注册返回的邮箱与提交邮箱不匹配")

    secret = _totp_secret(engine, result)
    result_password = str(getattr(result, "password", "") or "")
    if "----" in result_password:
        result_password = ""
    credential_line = (
        f"{email}----{result_password}----{secret}"
        if result_password and secret
        else None
    )
    two_factor_bound = bool(secret)

    if not getattr(result, "success", False):
        reason = getattr(result, "error_message", "") or "未返回注册结果"
        actual_password = str(getattr(engine, "password", "") or password)
        message = _sanitize(
            f"OpenAI 注册失败：{reason}",
            email=email,
            password=actual_password,
            api_url=api_url,
        )
        if credential_line:
            TIMING_LOGGER.info(
                "register_partial email=%s two_factor_bound=%s",
                _masked_email(email),
                two_factor_bound,
            )
            raise RegistrationIncomplete(
                f"{message}；账号可能已创建并绑定 2FA，已保存账号资料，"
                "可用“用已保存资料导入”重新登录",
                credential_line=credential_line,
                two_factor_bound=two_factor_bound,
            )
        raise RuntimeError(message)

    access_token = str(getattr(result, "access_token", "") or "")
    refresh_token = str(getattr(result, "refresh_token", "") or "")
    id_token = str(getattr(result, "id_token", "") or "")
    if not access_token or not refresh_token:
        message = "注册未同时取得 access_token 和 refresh_token；账号可能已在 OpenAI 创建"
        if credential_line:
            TIMING_LOGGER.info(
                "register_partial email=%s two_factor_bound=%s",
                _masked_email(email),
                two_factor_bound,
            )
            raise RegistrationIncomplete(
                message + "；已保存密码和 2FA，可用“用已保存资料导入”重新登录获取令牌",
                credential_line=credential_line,
                two_factor_bound=two_factor_bound,
            )
        raise RuntimeError(message + "，请核查后重试")

    account = SimpleNamespace(
        email=email,
        access_token=access_token,
        refresh_token=refresh_token,
        id_token=id_token,
    )
    payload = {"proxies": [], "accounts": [make_sub2api(account, group_ids=group_ids or [])]}
    TIMING_LOGGER.info(
        "register_done email=%s elapsed_ms=%d two_factor_bound=%s",
        _masked_email(email),
        round((time.perf_counter() - started) * 1000),
        bool(secret),
    )
    return payload, credential_line
