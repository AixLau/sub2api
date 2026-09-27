"""Select a usable Sub2API proxy for account import and OAuth login."""

from __future__ import annotations

import secrets
from dataclasses import dataclass, field
from datetime import datetime, timezone
from urllib.parse import quote

PROXY_NAME = "5x"
SUPPORTED_PROTOCOLS = {"http", "https", "socks5", "socks5h"}


@dataclass(frozen=True, slots=True)
class ProxyChoice:
    id: int
    url: str = field(repr=False)
    key: str = field(repr=False)


def _available(proxy: dict, now: datetime) -> bool:
    if proxy.get("name") != PROXY_NAME or proxy.get("status") != "active":
        return False
    if proxy.get("protocol") not in SUPPORTED_PROTOCOLS:
        return False
    host = proxy.get("host")
    if not isinstance(host, str) or not host or not all(
        char.isascii() and (char.isalnum() or char in ".-:[]") for char in host
    ):
        return False
    if isinstance(proxy.get("port"), bool) or not isinstance(proxy.get("port"), int) or not 1 <= proxy["port"] <= 65535:
        return False
    if isinstance(proxy.get("id"), bool) or not isinstance(proxy.get("id"), int) or proxy["id"] <= 0:
        return False
    if bool(proxy.get("username")) != bool(proxy.get("password")):
        return False
    expires_at = proxy.get("expires_at")
    if expires_at:
        try:
            expiry = datetime.fromisoformat(expires_at.replace("Z", "+00:00"))
        except (AttributeError, ValueError):
            return False
        if expiry.tzinfo is None:
            expiry = expiry.replace(tzinfo=timezone.utc)
        if expiry <= now:
            return False
    return True


def select_5x_proxy(proxies: list[dict]) -> ProxyChoice:
    candidates = [proxy for proxy in proxies if isinstance(proxy, dict) and _available(proxy, datetime.now(timezone.utc))]
    if not candidates:
        raise ValueError("没有可用的 5x 代理，请先在 Sub2API 添加或启用名称为 5x 的代理")
    proxy = secrets.choice(candidates)
    host = proxy["host"]
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"
    auth = ""
    if proxy.get("username") and proxy.get("password"):
        auth = f"{quote(proxy['username'], safe='')}:{quote(proxy['password'], safe='')}@"
    url = f"{proxy['protocol']}://{auth}{host}:{proxy['port']}"
    key = "|".join(str(proxy.get(name) or "").strip() for name in ("protocol", "host", "port", "username", "password"))
    return ProxyChoice(id=proxy["id"], url=url, key=key)
