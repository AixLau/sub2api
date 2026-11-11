"""Read OpenAI verification codes from a generic JSON/text mail API.

Registration needs a mailbox that can receive the signup code.  The importer
already supports logging in to existing accounts; this module adds a small
adapter for mailbox pools shaped like::

    email@example.com----https://mail.example/api/key=REDACTED

The implementation follows the generic-API mailbox approach used by
``turb-gpt-free-register`` (MIT): poll the URL, tolerate JSON/text/HTML,
extract a six-digit code with context scoring, filter stale messages by
timestamp when available, and wait a short settle window after the first
candidate so a cached code cannot win over a newer one.

The URL is a secret (it usually contains an API key), so it is never included
in errors or logs.  The adapter implements only the small part of
``core.base_mailbox.BaseMailbox`` used by the upstream registration engine.
"""

from __future__ import annotations

import base64
import html as html_lib
import json
import re
import time
from datetime import datetime
from types import SimpleNamespace
from typing import Callable
from urllib.parse import parse_qsl, unquote_to_bytes, urlencode, urlsplit, urlunsplit

import requests

CODE_RE = re.compile(r"(?<!\d)(\d{6})(?!\d)")
MAX_URL_LENGTH = 4096
MAX_RESPONSE_BYTES = 2 * 1024 * 1024
DEFAULT_POLL_SECONDS = 3.0
DEFAULT_SETTLE_SECONDS = 5.0
CONTEXT_WORDS = (
    "code", "otp", "verify", "verification", "security code", "one-time",
    "验证码", "校验码", "确认码", "代码",
    "認証コード", "検証コード", "確認コード", "コード",
    "인증 코드", "확인 코드",
)
CODE_KEYS = (
    "code", "otp", "verification_code", "verificationCode",
    "email_code", "emailCode", "latest_code", "latestCode",
)
TIME_KEYS = ("time", "date", "received_at", "receivedAt", "created_at", "createdAt", "timestamp")
TEMPLATE_NOISE = {"000000", "202123", "353740"}


class _TransientMailError(RuntimeError):
    """A retryable mail-API failure; the message must not contain the URL."""


def validate_mail_api_url(url: str) -> str:
    value = (url or "").strip()
    if not value or len(value) > MAX_URL_LENGTH or any(char.isspace() for char in value):
        raise ValueError("收信 API 地址无效")
    parts = urlsplit(value)
    if parts.scheme not in {"http", "https"} or not parts.netloc:
        raise ValueError("收信 API 地址必须是 http 或 https URL")
    if parts.username or parts.password:
        raise ValueError("收信 API 地址不能包含用户名或密码")
    return value


def _text(value: object) -> str:
    if value is None or isinstance(value, bool):
        return ""
    if isinstance(value, str):
        return value
    return str(value)


def _decode_data_uri(value: object) -> str:
    text = _text(value)
    if not text.startswith("data:"):
        return text
    try:
        meta, payload = text.split(",", 1)
    except ValueError:
        return text
    try:
        if ";base64" in meta.lower():
            return base64.b64decode(payload).decode("utf-8", errors="replace")
        return unquote_to_bytes(payload).decode("utf-8", errors="replace")
    except Exception:
        return text


def _strip_html(value: str) -> str:
    value = re.sub(r"<style[^>]*>.*?</style>", " ", value, flags=re.DOTALL | re.IGNORECASE)
    value = re.sub(r"<script[^>]*>.*?</script>", " ", value, flags=re.DOTALL | re.IGNORECASE)
    value = re.sub(r"#[0-9a-fA-F]{6}\b", " ", value)
    value = re.sub(r"<br\s*/?>", "\n", value, flags=re.IGNORECASE)
    value = re.sub(r"<[^>]+>", " ", value)
    value = html_lib.unescape(value)
    return re.sub(r"\s+", " ", value).strip()


def _flatten_json(value: object, prefix: str = "") -> list[tuple[str, str]]:
    items: list[tuple[str, str]] = []
    if isinstance(value, dict):
        for key, item in value.items():
            items.extend(_flatten_json(item, f"{prefix}.{key}" if prefix else str(key)))
    elif isinstance(value, list):
        for index, item in enumerate(value):
            items.extend(_flatten_json(item, f"{prefix}[{index}]"))
    elif value is not None and not isinstance(value, bool):
        items.append((prefix, _text(value)))
    return items


def _extract_structured_code(data: object) -> str:
    if not isinstance(data, dict):
        return ""
    for key in CODE_KEYS:
        raw = _text(data.get(key))
        if raw.strip().startswith(("http://", "https://")):
            continue
        match = CODE_RE.search(raw)
        if match:
            return match.group(1)
    inner = data.get("data")
    if isinstance(inner, dict):
        for key in CODE_KEYS:
            raw = _text(inner.get(key))
            if raw.strip().startswith(("http://", "https://")):
                continue
            match = CODE_RE.search(raw)
            if match:
                return match.group(1)
    return ""


def _extract_code_from_text(value: str) -> str:
    text = _strip_html(_decode_data_uri(value))
    # Tracking links and secure-login links can contain unrelated six-digit
    # numbers; only the visible mail text should be considered.
    text = re.sub(r"https?://\S+", " ", text)
    if not text:
        return ""
    candidates = [
        (match.group(1), match.start())
        for match in CODE_RE.finditer(text)
        if match.group(1) not in TEMPLATE_NOISE
    ]
    if not candidates:
        return ""
    lower = text.lower()
    best: tuple[int, int] | None = None
    for code, index in candidates:
        window = lower[max(0, index - 120): index + 126]
        score = 0
        if any(word.lower() in window for word in CONTEXT_WORDS):
            score += 40
        if re.search(r"(?:code|otp|verification|验证码|認証|コード|인증)", lower[max(0, index - 40): index + 46]):
            score += 20
        if text.strip() == code:
            score += 30
        candidate = (score, index)
        if best is None or candidate > best:
            best = candidate
    # With no contextual hit, the last six-digit value is the safest fallback:
    # OpenAI mails often put template noise first and the real code last.
    if best and best[0] > 0:
        return next(code for code, index in candidates if index == best[1])
    return candidates[-1][0]


def _extract_code(data: object, text: str = "") -> str:
    structured = _extract_structured_code(data)
    if structured:
        return structured
    if isinstance(data, (dict, list)):
        parts: list[str] = []
        for path, value in _flatten_json(data):
            if re.search(r"(?i)(url|link|href)", path):
                continue
            parts.append(value)
        code = _extract_code_from_text("\n".join(parts))
        if code:
            return code
    elif data is not None:
        raw = _text(data)
        if not raw.strip().startswith(("http://", "https://")):
            code = _extract_code_from_text(raw)
            if code:
                return code
    return _extract_code_from_text(text)


def extract_code(data: object, text: str = "") -> str:
    """Public wrapper used by checks and future adapters."""
    return _extract_code(data, text)


def _parse_timestamp(value: object) -> float | None:
    raw = _text(value).strip()
    if not raw:
        return None
    if re.fullmatch(r"\d+(?:\.\d+)?", raw):
        try:
            number = float(raw)
        except ValueError:
            return None
        return number / 1000 if number > 10_000_000_000 else number
    normalized = raw
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(normalized)
        return parsed.timestamp()
    except (ValueError, OverflowError):
        pass
    for fmt in ("%Y-%m-%d %H:%M:%S", "%Y-%m-%dT%H:%M:%S", "%Y/%m/%d %H:%M:%S", "%Y-%m-%d %H:%M"):
        try:
            return datetime.strptime(raw[:19], fmt).timestamp()
        except ValueError:
            pass
    return None


def _structured_time(data: object) -> float | None:
    if not isinstance(data, dict):
        return None
    for key in TIME_KEYS:
        parsed = _parse_timestamp(data.get(key))
        if parsed:
            return parsed
    inner = data.get("data")
    if isinstance(inner, dict):
        for key in TIME_KEYS:
            parsed = _parse_timestamp(inner.get(key))
            if parsed:
                return parsed
    return None


def _login_url(data: object) -> str:
    if not isinstance(data, dict):
        return ""
    value = data.get("login_url")
    return _text(value).strip() if value else ""


def _cache_busted_url(url: str, attempt: int) -> str:
    try:
        parts = urlsplit(url)
        query = parse_qsl(parts.query, keep_blank_values=True)
        query.append(("_otp_poll", f"{int(time.time() * 1000)}-{attempt}"))
        return urlunsplit(parts._replace(query=urlencode(query, doseq=True)))
    except Exception:
        return url


class MailAPIMailbox:
    """Minimal mailbox implementation backed by a polling generic mail API."""

    def __init__(
        self,
        email: str,
        api_url: str,
        *,
        request_timeout: float = 15,
        poll_interval: float = DEFAULT_POLL_SECONDS,
        settle_seconds: float = DEFAULT_SETTLE_SECONDS,
        proxy: str | None = None,
        http_get: Callable | None = None,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
    ) -> None:
        self.email = (email or "").strip()
        self.api_url = validate_mail_api_url(api_url)
        self.request_timeout = request_timeout
        self.poll_interval = max(float(poll_interval or 0), 0.2)
        self.settle_seconds = max(float(settle_seconds or 0), 0)
        self._clock = clock
        self._sleep = sleep
        self._seen: set[str] = set()
        self._consumed: set[str] = set()
        self._saw_login_url = False
        self._last_error = ""
        self._cache_bust = True
        self._log_fn: Callable[[str], None] | None = None
        self._session: requests.Session | None = None

        if http_get is not None:
            self._http_get = http_get
        else:
            self._session = requests.Session()
            self._session.trust_env = False
            if proxy:
                self._session.proxies.update({"http": proxy, "https": proxy})
            self._http_get = self._session.get

    def close(self) -> None:
        if self._session is not None:
            self._session.close()
            self._session = None

    def _log(self, message: str) -> None:
        if callable(self._log_fn):
            self._log_fn(message)

    def get_email(self):
        return SimpleNamespace(
            email=self.email,
            account_id=self.email,
            extra={"mail_provider": "generic_api", "mailapi_url": self.api_url},
        )

    def get_current_ids(self, account) -> set[str]:
        """Prime the adapter with any code that is already in the inbox."""
        try:
            code, _ = self._poll(attempt=0, after_ts=None)
        except Exception:
            return set()
        if code:
            self._seen.add(code)
            return {code}
        return set()

    def wait_for_code(
        self,
        account,
        keyword: str = "",
        timeout: int = 120,
        before_ids: set | None = None,
        code_pattern: str | None = None,
        otp_sent_at: float | None = None,
        **kwargs,
    ) -> str:
        deadline = self._clock() + max(int(timeout or 0), 1)
        seen = set(before_ids or ()) | self._seen
        candidate = ""
        settle_until = 0.0
        attempt = 0

        while self._clock() < deadline:
            attempt += 1
            try:
                code, _ = self._poll(attempt=attempt, after_ts=otp_sent_at)
            except _TransientMailError as exc:
                self._last_error = str(exc)
                code = ""
            else:
                self._last_error = ""
            if code and code not in seen and code not in self._consumed:
                if code != candidate:
                    candidate = code
                    settle_until = self._clock() + self.settle_seconds
                    self._log("已锁定候选验证码，等待邮箱接口稳定")
                elif self._clock() >= settle_until:
                    self._consumed.add(candidate)
                    self._seen.add(candidate)
                    self._log("已确认邮箱验证码")
                    return candidate

            now = self._clock()
            if candidate and now >= settle_until:
                self._consumed.add(candidate)
                self._seen.add(candidate)
                self._log("已确认邮箱验证码")
                return candidate
            remaining = deadline - now
            if remaining <= 0:
                break
            self._sleep(min(self.poll_interval, remaining))

        if candidate:
            self._consumed.add(candidate)
            self._seen.add(candidate)
            self._log("总等待时间结束，返回已锁定的验证码")
            return candidate

        if self._saw_login_url:
            reason = "仅收到安全登录链接，未收到 6 位验证码"
        elif self._last_error:
            reason = self._last_error
        else:
            reason = "等待邮箱验证码超时"
        raise TimeoutError(f"收信接口未返回验证码：{reason}")

    def set_account_status(self, account, status: str) -> None:
        """The fixed mailbox has no pool state to update."""

    def _poll(self, *, attempt: int, after_ts: float | None) -> tuple[str, str]:
        url = self.api_url
        if self._cache_bust:
            url = _cache_busted_url(url, attempt)
        response = self._request(url)
        if self._cache_bust and getattr(response, "status_code", 0) in {400, 403, 404, 405, 422}:
            # Some endpoints (including signed links) reject unknown query
            # parameters.  Retry the exact URL once and stop adding the buster.
            self._cache_bust = False
            response = self._request(self.api_url)

        status = getattr(response, "status_code", 0)
        if status == 401:
            raise RuntimeError("收信 API 鉴权失败，请检查收信地址")
        if not 200 <= status < 300:
            # Treat other failures as retryable: many mailbox APIs return 404
            # or 400 while the inbox is still empty.
            raise _TransientMailError(f"收信 API 暂时不可用（HTTP {status}）")

        content = getattr(response, "content", b"") or b""
        if len(content) > MAX_RESPONSE_BYTES:
            raise _TransientMailError("收信 API 响应过大")
        text = _text(getattr(response, "text", ""))[:MAX_RESPONSE_BYTES]
        data: object = None
        try:
            data = response.json()
        except (ValueError, AttributeError):
            data = None
        if data is None:
            try:
                data = json.loads(text)
            except (ValueError, TypeError):
                data = None

        if _login_url(data):
            self._saw_login_url = True

        code = _extract_code(data, text)
        if not code:
            return "", ""
        msg_ts = _structured_time(data)
        if after_ts and msg_ts and msg_ts + 2 < after_ts:
            return "", ""
        return code, _login_url(data)

    def _request(self, url: str):
        try:
            return self._http_get(
                url,
                timeout=self.request_timeout,
                headers={
                    "Accept": "application/json,text/plain,text/html,*/*",
                    "Cache-Control": "no-cache, no-store, max-age=0",
                    "Pragma": "no-cache",
                    "User-Agent": "sub2api-account-importer/1",
                },
            )
        except TypeError:
            # Tests and simple callbacks may only accept (url, timeout).
            try:
                return self._http_get(url, timeout=self.request_timeout)
            except requests.RequestException:
                raise _TransientMailError("收信 API 请求失败") from None
            except Exception:
                raise _TransientMailError("收信 API 请求失败") from None
        except requests.RequestException:
            raise _TransientMailError("收信 API 请求失败") from None
        except Exception:
            raise _TransientMailError("收信 API 请求失败") from None
